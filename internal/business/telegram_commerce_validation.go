package business

import (
	"context"
	"database/sql"
	"encoding/json"
)

// validateTelegramCommerceData is read-only and safe for offline snapshots.
// It verifies captured money, native issuance provenance and immutable customer
// bindings without contacting Telegram or inferring runtime delivery.
func validateTelegramCommerceData(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return err
	}
	for _, query := range []string{
		"SELECT COUNT(*) FROM telegram_commerce_stores s JOIN representatives r ON r.id=s.representative_id LEFT JOIN telegram_commerce_members m ON m.store_id=s.store_id AND m.telegram_user_id=s.manager_user_id WHERE r.kind NOT IN('owner','representative') OR (r.kind='owner' AND r.id<>'owner') OR m.role<>r.kind OR m.enabled<>1 OR m.telegram_user_id IS NULL",
		"SELECT COUNT(*) FROM telegram_commerce_prices p JOIN telegram_commerce_stores s ON s.store_id=p.store_id JOIN catalog_products c ON c.id=p.product_id WHERE c.representative_id<>s.representative_id",
		"SELECT COUNT(*) FROM telegram_commerce_payments p JOIN telegram_commerce_orders o ON o.order_id=p.order_id JOIN telegram_commerce_stores s ON s.store_id=o.store_id WHERE p.bot_id<>s.bot_id OR p.customer_id<>o.customer_id OR p.currency<>o.currency OR p.amount_stars<>o.amount_stars",
		"SELECT COUNT(*) FROM telegram_commerce_payment_refunds r JOIN telegram_commerce_payments p ON p.bot_id=r.bot_id AND p.charge_id=r.charge_id WHERE r.order_id<>p.order_id",
		"SELECT COUNT(*) FROM (SELECT SUM(delta) AS balance FROM telegram_commerce_wallet_ledger WHERE bucket='paid' GROUP BY store_id,customer_id,currency HAVING balance<0)",
		"SELECT COUNT(*) FROM telegram_commerce_trials t LEFT JOIN telegram_commerce_customer_clients b ON b.client_id=t.client_id AND b.store_id=t.store_id AND b.customer_id=t.customer_id WHERE b.client_id IS NULL",
		"SELECT COUNT(*) FROM telegram_commerce_customer_clients b WHERE NOT EXISTS(SELECT 1 FROM telegram_commerce_orders o JOIN telegram_commerce_order_receipts r ON r.order_id=o.order_id WHERE o.client_id=b.client_id AND o.store_id=b.store_id AND o.customer_id=b.customer_id AND o.action='create') AND NOT EXISTS(SELECT 1 FROM telegram_commerce_trials t WHERE t.client_id=b.client_id AND t.store_id=b.store_id AND t.customer_id=b.customer_id)",
	} {
		var count int64
		if tx.QueryRowContext(ctx, query).Scan(&count) != nil || count != 0 {
			return ErrUnavailable
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT role,permissions_json FROM telegram_commerce_members")
	if err != nil {
		return ErrUnavailable
	}
	for rows.Next() {
		var role, raw string
		var permissions []string
		if rows.Scan(&role, &raw) != nil || json.Unmarshal([]byte(raw), &permissions) != nil {
			rows.Close()
			return ErrUnavailable
		}
		if _, e := telegramPermissions(permissions, role == "admin"); e != nil {
			rows.Close()
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	// Close each bounded page before loading its receipt and native ledger facts.
	cursor := ""
	for {
		rows, err = tx.QueryContext(ctx, "SELECT order_id FROM telegram_commerce_orders WHERE order_id>? ORDER BY order_id LIMIT 200", cursor)
		if err != nil {
			return ErrUnavailable
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if rows.Scan(&id) != nil {
				rows.Close()
				return ErrUnavailable
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrUnavailable
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if err = validateTelegramOrderDataTx(ctx, tx, id); err != nil {
				return err
			}
		}
		cursor = ids[len(ids)-1]
	}
	rows, err = tx.QueryContext(ctx, "SELECT event_id,fingerprint,store_id,customer_id,bucket,currency,delta,reason FROM telegram_commerce_wallet_ledger")
	if err != nil {
		return ErrUnavailable
	}
	for rows.Next() {
		var event, hash, store, customer, bucket, currency, reason string
		var delta int64
		if rows.Scan(&event, &hash, &store, &customer, &bucket, &currency, &delta, &reason) != nil || !eventRE.MatchString(event) || !idRE.MatchString(store) || !idRE.MatchString(customer) || delta == 0 || !validName(reason, 200) {
			rows.Close()
			return ErrUnavailable
		}
		intent := struct {
			Store, Customer, Bucket, Currency, Reason string
			Delta                                     int64
			Reversal                                  bool
		}{store, customer, bucket, currency, reason, delta, false}
		if hash != fingerprint("telegram-wallet", intent) {
			if bucket != "bonus" || delta >= 0 {
				rows.Close()
				return ErrUnavailable
			}
			intent.Reversal = true
			if hash != fingerprint("telegram-wallet", intent) {
				rows.Close()
				return ErrUnavailable
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	if err = validateTelegramTrialDataTx(ctx, tx); err != nil {
		return err
	}
	if err = validateTelegramDeliveryDataTx(ctx, tx); err != nil {
		return err
	}
	return nil
}

func validateTelegramOrderDataTx(ctx context.Context, tx *sql.Tx, id string) error {
	order, intent, err := readTelegramOrderTx(ctx, tx, id)
	if err != nil {
		return ErrUnavailable
	}
	if order.CreatedUnix <= 0 || order.ExpiresUnix != order.CreatedUnix+900 || intent.Product.PriceStars <= 0 || intent.Product.PriceRevision < 1 || intent.Product.Product.Status != "published" {
		return ErrUnavailable
	}
	var payment, receiptCount, refund int
	for query, target := range map[string]*int{
		"SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=?":        &payment,
		"SELECT COUNT(*) FROM telegram_commerce_order_receipts WHERE order_id=?":  &receiptCount,
		"SELECT COUNT(*) FROM telegram_commerce_payment_refunds WHERE order_id=?": &refund,
	} {
		if tx.QueryRowContext(ctx, query, id).Scan(target) != nil {
			return ErrUnavailable
		}
	}
	switch order.Status {
	case "quoted":
		if payment != 0 || receiptCount != 0 || refund != 0 {
			return ErrUnavailable
		}
	case "paid_pending_fulfillment":
		if payment != 1 || receiptCount != 0 || refund != 0 {
			return ErrUnavailable
		}
	case "paid_pending_provisioning":
		if payment != 1 || receiptCount != 1 || refund != 0 {
			return ErrUnavailable
		}
	case "refunded":
		if payment != 1 || refund != 1 || receiptCount > 1 {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	if receiptCount == 0 {
		return nil
	}
	var raw []byte
	var created int64
	var receipt TelegramOrderReceipt
	if tx.QueryRowContext(ctx, "SELECT receipt_json,created_unix FROM telegram_commerce_order_receipts WHERE order_id=?", id).Scan(&raw, &created) != nil || json.Unmarshal(raw, &receipt) != nil {
		return ErrUnavailable
	}
	expected := order
	expected.Status = "paid_pending_provisioning"
	if receipt.Order != expected || receipt.CreatedUnix != created || created <= 0 || receipt.Replayed || receipt.Client.ID != order.ClientID || receipt.Client.RepresentativeID != order.Product.RepresentativeID || receipt.Client.UUID == "" {
		return ErrUnavailable
	}
	var clientUUID, store, customer string
	if tx.QueryRowContext(ctx, "SELECT c.uuid,b.store_id,b.customer_id FROM clients c JOIN telegram_commerce_customer_clients b ON b.client_id=c.id WHERE c.id=?", order.ClientID).Scan(&clientUUID, &store, &customer) != nil || clientUUID != receipt.Client.UUID || store != order.StoreID || customer != order.CustomerID {
		return ErrUnavailable
	}
	event := "tg-order-" + id
	if order.Action == "create" {
		var kind, rep, object string
		var bytes, units int64
		if tx.QueryRowContext(ctx, "SELECT kind,representative_id,object_id,volume_delta_bytes,unlimited_delta_units FROM events WHERE event_id=?", event).Scan(&kind, &rep, &object, &bytes, &units) != nil || kind != "client_created" || rep != order.Product.RepresentativeID || object != order.ClientID {
			return ErrUnavailable
		}
		wantBytes, wantUnits := int64(0), int64(0)
		if rep != "owner" {
			if order.Product.PlanKind == "volume" {
				wantBytes = -order.Product.VolumeBytes
			} else {
				wantUnits = -(order.Product.DurationDays*order.Product.IPCount + 29) / 30
			}
		}
		if bytes != wantBytes || units != wantUnits || receipt.Client.PlanKind != order.Product.PlanKind || receipt.Client.IPCount != order.Product.IPCount || receipt.Client.DurationDays != order.Product.DurationDays || receipt.Client.VolumeBytes != order.Product.VolumeBytes {
			return ErrUnavailable
		}
	} else {
		var client, rep, kind string
		var days, volume int64
		if tx.QueryRowContext(ctx, "SELECT client_id,representative_id,kind,added_days,added_volume_bytes FROM client_operations WHERE event_id=?", event).Scan(&client, &rep, &kind, &days, &volume) != nil || client != order.ClientID || rep != order.Product.RepresentativeID || kind != "extend" || days != order.Product.DurationDays || volume != order.Product.VolumeBytes || len(intent.ExpectedClientHash) != 64 {
			return ErrUnavailable
		}
	}
	return nil
}

func (s *Store) VerifyTelegramCommerceData(ctx context.Context) error {
	return validateTelegramCommerceData(ctx, s.db)
}

func validateTelegramTrialDataTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT t.store_id,t.customer_id,t.client_id,t.event_id,t.receipt_json,t.created_unix,c.uuid,s.representative_id,e.kind,e.representative_id,e.object_id,e.volume_delta_bytes,e.unlimited_delta_units FROM telegram_commerce_trials t JOIN clients c ON c.id=t.client_id JOIN telegram_commerce_stores s ON s.store_id=t.store_id LEFT JOIN events e ON e.event_id=t.event_id")
	if err != nil {
		return ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var store, customer, client, event, uuid, rep, kind, eventRep, object string
		var raw []byte
		var created, bytes, units int64
		var service TelegramService
		if rows.Scan(&store, &customer, &client, &event, &raw, &created, &uuid, &rep, &kind, &eventRep, &object, &bytes, &units) != nil || json.Unmarshal(raw, &service) != nil {
			return ErrUnavailable
		}
		c := service.Client
		if c.ID != client || c.UUID != uuid || c.RepresentativeID != rep || c.DurationDays != 1 || c.IPCount != 1 || c.PlanKind != "volume" || c.VolumeBytes <= 0 || c.ExpiresUnix-c.CreatedUnix != 86400 || c.CreatedUnix != created || kind != "client_created" || eventRep != rep || object != client || units != 0 || service.State != "pending_node_apply" || service.UsageAvailable {
			return ErrUnavailable
		}
		want := int64(0)
		if rep != "owner" {
			want = -c.VolumeBytes
		}
		if bytes != want || !eventRE.MatchString(event) || !idRE.MatchString(store) || !idRE.MatchString(customer) {
			return ErrUnavailable
		}
	}
	return rows.Err()
}
