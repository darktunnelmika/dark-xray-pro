package business

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Returns the immutable ledger row sequence so compensation ordering is checked
// independently of equal-second timestamps or later wall-clock corrections.
func telegramBonusLedgerProofTx(ctx context.Context, tx *sql.Tx, event, store, customer string, delta int64, reason string, debt bool) (int64, error) {
	var sequence, gotDelta, created int64
	var gotStore, gotCustomer, bucket, currency, gotReason, hash string
	err := tx.QueryRowContext(ctx, `SELECT rowid,store_id,customer_id,bucket,currency,delta,reason,fingerprint,created_unix FROM telegram_commerce_wallet_ledger WHERE event_id=?`, event).Scan(&sequence, &gotStore, &gotCustomer, &bucket, &currency, &gotDelta, &gotReason, &hash, &created)
	if err != nil {
		return 0, ErrUnavailable
	}
	intent := struct {
		Store, Customer, Bucket, Currency, Reason string
		Delta                                     int64
		Reversal                                  bool
	}{store, customer, "bonus", "XTR", reason, delta, debt}
	if sequence <= 0 || created <= 0 || gotStore != store || gotCustomer != customer || bucket != "bonus" || currency != "XTR" || gotDelta != delta || gotReason != reason || hash != fingerprint("telegram-wallet", intent) {
		return 0, ErrUnavailable
	}
	return sequence, nil
}

// Called for every read of an order. A discount cannot be inferred from a low
// invoice amount: its original reservation and every compensation need proof.
func validateTelegramBonusOrderTx(ctx context.Context, tx *sql.Tx, order TelegramOrder, intent telegramCheckoutIntent) error {
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil {
		return err
	}
	if !present {
		if order.BonusDiscountStars != 0 {
			return ErrUnavailable
		}
		return nil
	}
	if order.BonusDiscountStars == 0 {
		var count int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_bonus_quotes WHERE order_id=?`, order.ID).Scan(&count) != nil || count != 0 {
			return ErrUnavailable
		}
		return nil
	}
	r, err := readTelegramBonusRecordTx(ctx, tx, order.ID)
	if err != nil {
		return ErrUnavailable
	}
	b := order.BonusDiscountStars
	if b < 1 || b >= r.Quote.GrossStars || r.Quote.GrossStars > 1_000_000_000 || r.Quote.GrossStars != intent.Product.PriceStars || r.Quote.GrossStars != order.AmountStars+b || r.Quote.DiscountStars != b || intent.Request.BonusStars != b || r.Quote.NetStars != order.AmountStars || order.AmountStars < 1 || r.StoreID != order.StoreID || r.CustomerID != order.CustomerID || r.CreatedUnix != order.CreatedUnix || r.CreatedUnix <= 0 || r.Quote.ExpiresUnix != order.ExpiresUnix || r.Quote.ExpiresUnix != r.CreatedUnix+900 || r.RewardRule != "net_paid_cap_v1" || r.ReserveEvent != "tg-bonus-reserve:"+order.ID {
		return ErrUnavailable
	}
	var authorized int
	if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_bonus_policy_versions v JOIN telegram_bonus_policies p ON p.store_id=v.store_id AND p.revision>=v.revision WHERE v.store_id=? AND v.revision=? AND v.enabled=1`, r.StoreID, r.PolicyRevision).Scan(&authorized) != nil || authorized != 1 {
		return ErrUnavailable
	}
	reserveSequence, err := telegramBonusLedgerProofTx(ctx, tx, r.ReserveEvent, r.StoreID, r.CustomerID, -b, telegramBonusReserveReason, false)
	if err != nil {
		return err
	}
	var releaseSequence int64
	if r.ReleaseEvent != "" {
		if r.ReleaseEvent != "tg-bonus-release:"+order.ID || r.ReleaseUnix < r.CreatedUnix || r.ReleaseReason != "expired" && r.ReleaseReason != "cancelled" || r.ReleaseReason == "expired" && r.ReleaseUnix < r.Quote.ExpiresUnix {
			return ErrUnavailable
		}
		releaseSequence, err = telegramBonusLedgerProofTx(ctx, tx, r.ReleaseEvent, r.StoreID, r.CustomerID, b, telegramBonusReleaseReason, false)
		if err != nil || releaseSequence <= reserveSequence {
			return ErrUnavailable
		}
		if r.ReleaseReason == "cancelled" {
			want := fingerprint("telegram-bonus-cancel-v1", struct {
				Actor TelegramActor
				Order string
			}{TelegramActor{BotID: order.BotID, UserID: order.UserID, ChatID: order.UserID}, order.ID})
			var audited int
			if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_events WHERE action='bonus_cancel' AND store_id=? AND actor_user_id=? AND fingerprint=? AND json_extract(receipt_json,'$.order_id')=?`, order.StoreID, order.UserID, want, order.ID).Scan(&audited) != nil || audited < 1 {
				return ErrUnavailable
			}
		}
	}
	var payments, refunds int
	if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=?`, order.ID).Scan(&payments) != nil || tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payment_refunds WHERE order_id=?`, order.ID).Scan(&refunds) != nil {
		return ErrUnavailable
	}
	switch order.Status {
	case "quoted":
		if payments != 0 || refunds != 0 || r.SettlementEvent != "" || r.RefundEvent != "" {
			return ErrUnavailable
		}
	case "paid_pending_fulfillment", "paid_pending_provisioning":
		if payments != 1 || refunds != 0 || r.SettlementEvent == "" || r.RefundEvent != "" {
			return ErrUnavailable
		}
	case "refunded":
		if payments != 1 || refunds != 1 || r.SettlementEvent == "" || r.RefundEvent == "" {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	consumedSequence := reserveSequence
	if r.SettlementEvent != "" {
		var valid int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=? AND bot_id=? AND charge_id=? AND customer_id=? AND currency='XTR' AND amount_stars=? AND created_unix=?`, order.ID, r.PaymentBotID, r.Charge, r.CustomerID, r.Quote.NetStars, r.SettledUnix).Scan(&valid) != nil || valid != 1 || r.PaymentBotID != order.BotID || r.SettledUnix <= 0 {
			return ErrUnavailable
		}
		if r.Late {
			if r.ReleaseEvent == "" || r.SettlementEvent != "tg-bonus-late:"+order.ID {
				return ErrUnavailable
			}
			consumedSequence, err = telegramBonusLedgerProofTx(ctx, tx, r.SettlementEvent, r.StoreID, r.CustomerID, -b, telegramBonusLateReason, true)
			if err != nil || consumedSequence <= releaseSequence {
				return ErrUnavailable
			}
		} else if r.ReleaseEvent != "" || r.SettlementEvent != r.ReserveEvent {
			return ErrUnavailable
		}
	}
	if r.RefundEvent != "" {
		if r.RefundEvent != "tg-bonus-refund:"+order.ID || r.SettlementEvent == "" {
			return ErrUnavailable
		}
		var valid int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payment_refunds WHERE order_id=? AND bot_id=? AND charge_id=? AND created_unix=?`, order.ID, r.PaymentBotID, r.Charge, r.RefundUnix).Scan(&valid) != nil || valid != 1 || r.RefundUnix <= 0 {
			return ErrUnavailable
		}
		sequence, e := telegramBonusLedgerProofTx(ctx, tx, r.RefundEvent, r.StoreID, r.CustomerID, b, telegramBonusRefundReason, false)
		if e != nil || sequence <= consumedSequence {
			return ErrUnavailable
		}
	}
	// Exactly one net discount is consumed for a paid, unrefunded order. An
	// expired/cancelled unpaid quote and a refunded order have zero net cost.
	want := -b
	if r.ReleaseEvent != "" && r.SettlementEvent == "" || r.RefundEvent != "" {
		want = 0
	}
	var sum int64
	if tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM telegram_commerce_wallet_ledger WHERE event_id IN(?,?,?,?)`, "tg-bonus-reserve:"+order.ID, "tg-bonus-release:"+order.ID, "tg-bonus-late:"+order.ID, "tg-bonus-refund:"+order.ID).Scan(&sum) != nil || sum != want {
		return ErrUnavailable
	}
	return nil
}

func validateTelegramBonusRowsV1(ctx context.Context, tx *sql.Tx) error {
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil {
		return err
	}
	if !present {
		var walletTable int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='telegram_commerce_wallet_ledger'`).Scan(&walletTable) != nil {
			return ErrUnavailable
		}
		if walletTable == 0 {
			return nil
		}
		var orphan int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_wallet_ledger WHERE event_id GLOB 'tg-bonus-reserve:*' OR event_id GLOB 'tg-bonus-release:*' OR event_id GLOB 'tg-bonus-late:*' OR event_id GLOB 'tg-bonus-refund:*'`).Scan(&orphan) != nil || orphan != 0 {
			return ErrUnavailable
		}
		// Removing the whole optional family cannot turn a discounted invoice
		// into an ordinary full-price one. Scan only the immutable request here;
		// an old database still needs no optional migration to open.
		rows, e := tx.QueryContext(ctx, `SELECT intent_json FROM telegram_commerce_orders`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var intent telegramCheckoutIntent
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &intent) != nil || intent.Request.BonusStars != 0 {
				return ErrUnavailable
			}
		}
		return rows.Err()
	}
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return err
	}
	// Reciprocal checks matter: checking quote -> wallet alone would accept an
	// orphan or transplanted positive compensation that invented spendable credit.
	for _, query := range []string{
		`SELECT COUNT(*) FROM telegram_bonus_policies p LEFT JOIN telegram_commerce_stores s ON s.store_id=p.store_id WHERE s.store_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_bonus_policy_versions v LEFT JOIN telegram_commerce_stores s ON s.store_id=v.store_id WHERE s.store_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_bonus_quotes q LEFT JOIN telegram_commerce_orders o ON o.order_id=q.order_id LEFT JOIN telegram_commerce_customers c ON c.store_id=q.store_id AND c.customer_id=q.customer_id WHERE o.order_id IS NULL OR c.customer_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_bonus_releases r LEFT JOIN telegram_bonus_quotes q ON q.order_id=r.order_id WHERE q.order_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_bonus_releases r WHERE r.reason='cancelled' AND NOT EXISTS(SELECT 1 FROM telegram_commerce_events e WHERE e.action='bonus_cancel' AND json_extract(e.receipt_json,'$.order_id')=r.order_id)`,
		`SELECT COUNT(*) FROM telegram_bonus_settlements s LEFT JOIN telegram_bonus_quotes q ON q.order_id=s.order_id WHERE q.order_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_bonus_refunds f LEFT JOIN telegram_bonus_quotes q ON q.order_id=f.order_id WHERE q.order_id IS NULL`,
		`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger w WHERE w.event_id GLOB 'tg-bonus-reserve:*' AND NOT EXISTS(SELECT 1 FROM telegram_bonus_quotes q WHERE q.reserve_event_id=w.event_id)`,
		`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger w WHERE w.event_id GLOB 'tg-bonus-release:*' AND NOT EXISTS(SELECT 1 FROM telegram_bonus_releases r WHERE r.wallet_event_id=w.event_id)`,
		`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger w WHERE w.event_id GLOB 'tg-bonus-late:*' AND NOT EXISTS(SELECT 1 FROM telegram_bonus_settlements s WHERE s.wallet_event_id=w.event_id AND s.late=1)`,
		`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger w WHERE w.event_id GLOB 'tg-bonus-refund:*' AND NOT EXISTS(SELECT 1 FROM telegram_bonus_refunds r WHERE r.wallet_event_id=w.event_id)`,
		`SELECT COUNT(*) FROM telegram_bonus_policies p LEFT JOIN telegram_bonus_policy_versions v ON v.store_id=p.store_id AND v.revision=p.revision WHERE v.store_id IS NULL OR p.enabled<>v.enabled OR p.revision<>(SELECT MAX(x.revision) FROM telegram_bonus_policy_versions x WHERE x.store_id=p.store_id)`,
		`SELECT COUNT(*) FROM telegram_bonus_policy_versions v LEFT JOIN telegram_bonus_policies p ON p.store_id=v.store_id WHERE p.store_id IS NULL`,
		`SELECT COUNT(*) FROM (SELECT store_id FROM telegram_bonus_policy_versions GROUP BY store_id HAVING MIN(revision)<>1 OR COUNT(*)<>MAX(revision))`,
		`SELECT COUNT(*) FROM telegram_commerce_events e WHERE e.action='bonus_policy' AND NOT EXISTS(SELECT 1 FROM telegram_bonus_policy_versions v WHERE v.event_id=e.event_id)`,
	} {
		if err = telegramExtensionRowsValid(ctx, tx, query); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT v.store_id,v.revision,v.enabled,v.event_id,e.action,e.actor_user_id,e.receipt_json,e.fingerprint,s.bot_id FROM telegram_bonus_policy_versions v LEFT JOIN telegram_commerce_events e ON e.event_id=v.event_id AND e.store_id=v.store_id JOIN telegram_commerce_stores s ON s.store_id=v.store_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var store, event, action, hash string
		var revision, user, bot int64
		var enabled bool
		var raw []byte
		if rows.Scan(&store, &revision, &enabled, &event, &action, &user, &raw, &hash, &bot) != nil {
			rows.Close()
			return ErrUnavailable
		}
		p, e := readTelegramBonusPolicyReceipt(raw)
		want := fingerprint("telegram-bonus-policy-v1", struct {
			Actor   TelegramActor
			Request TelegramBonusPolicyChangeV1
		}{TelegramActor{BotID: bot, UserID: user, ChatID: user}, TelegramBonusPolicyChangeV1{EventID: event, ExpectedRevision: revision - 1, Enabled: enabled}})
		if e != nil || p.StoreID != store || p.Revision != revision || p.Enabled != enabled || action != "bonus_policy" || user <= 0 || hash != want {
			rows.Close()
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Cancellation authority is a historical authenticated audit event. Its
	// receipt stays released even after late capture; current state is derived
	// from immutable settlement/refund rows instead of rewriting that event.
	type cancellation struct {
		Event, Store, Hash string
		User, Bot          int64
		Quote              TelegramBonusQuoteV1
	}
	cursor := ""
	for {
		rows, err = tx.QueryContext(ctx, `SELECT e.event_id,e.store_id,e.actor_user_id,e.fingerprint,e.receipt_json,s.bot_id FROM telegram_commerce_events e LEFT JOIN telegram_commerce_stores s ON s.store_id=e.store_id WHERE e.action='bonus_cancel' AND e.event_id>? ORDER BY e.event_id LIMIT 200`, cursor)
		if err != nil {
			return err
		}
		cancelled := []cancellation{}
		for rows.Next() {
			var c cancellation
			var raw []byte
			if rows.Scan(&c.Event, &c.Store, &c.User, &c.Hash, &raw, &c.Bot) != nil || len(raw) > 8192 || json.Unmarshal(raw, &c.Quote) != nil {
				rows.Close()
				return ErrUnavailable
			}
			cancelled = append(cancelled, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(cancelled) == 0 {
			break
		}
		for _, c := range cancelled {
			r, e := readTelegramBonusRecordTx(ctx, tx, c.Quote.OrderID)
			if e != nil || r.StoreID != c.Store || r.ReleaseEvent == "" || c.User <= 0 || c.Bot <= 0 {
				return ErrUnavailable
			}
			var user int64
			if tx.QueryRowContext(ctx, `SELECT telegram_user_id FROM telegram_commerce_customers WHERE store_id=? AND customer_id=?`, r.StoreID, r.CustomerID).Scan(&user) != nil || user != c.User {
				return ErrUnavailable
			}
			expected := r.Quote
			expected.State = "released"
			want := fingerprint("telegram-bonus-cancel-v1", struct {
				Actor TelegramActor
				Order string
			}{TelegramActor{BotID: c.Bot, UserID: c.User, ChatID: c.User}, c.Quote.OrderID})
			if c.Quote != expected || c.Hash != want {
				return ErrUnavailable
			}
		}
		cursor = cancelled[len(cancelled)-1].Event
	}
	cursor = ""
	for {
		rows, err = tx.QueryContext(ctx, `SELECT order_id FROM telegram_commerce_orders WHERE order_id>? ORDER BY order_id LIMIT 200`, cursor)
		if err != nil {
			return err
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
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if err = validateTelegramOrderDataTx(ctx, tx, id); err != nil {
				return err
			}
		}
		cursor = ids[len(ids)-1]
	}
}
