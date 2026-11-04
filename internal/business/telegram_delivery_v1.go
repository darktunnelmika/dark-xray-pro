package business

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// TelegramDelivery is a short-lived projection of the native subscription.
// Content contains credentials: callers must never log it or put it in a
// financial receipt. Only the private, bounded transport outbox may retain it.
type TelegramDelivery struct {
	ClientID        string
	Content         string
	ContentType     string
	SHA256          string
	TermsSHA256     string
	Generation      uint64
	ExpiresUnixNano int64
}

func telegramClientTerms(c Client) string {
	return fingerprint("telegram-delivery-client-terms-v1", struct {
		ID, RepresentativeID, UUID, PlanKind                                                     string
		IPCount, VolumeBytes, DurationDays, ChargeVolumeBytes, ChargeUnlimitedUnits, ExpiresUnix int64
		Archived                                                                                 bool
	}{c.ID, c.RepresentativeID, c.UUID, c.PlanKind, c.IPCount, c.VolumeBytes, c.DurationDays, c.ChargeVolumeBytes, c.ChargeUnlimitedUnits, c.ExpiresUnix, c.Archived})
}

func telegramServiceDeliveryTx(ctx context.Context, tx *sql.Tx, key []byte, a TelegramActor, clientID, format string) (TelegramDelivery, TelegramAccess, Client, error) {
	var empty TelegramDelivery
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return empty, access, Client{}, err
	}
	if err = telegramCustomerOwnsClientTx(ctx, tx, access, clientID); err != nil {
		return empty, access, Client{}, err
	}
	c, err := readClient(ctx, tx, clientID)
	if err != nil {
		return empty, access, c, err
	}
	if c.Archived || c.ExpiresUnix <= time.Now().UTC().Unix() {
		return empty, access, c, ErrConflict
	}
	routes, err := executionSubscriptionRoutes(ctx, tx, key, clientID)
	if err != nil {
		return empty, access, c, err
	}
	var generation uint64
	seen := map[uint64]bool{}
	for _, route := range routes {
		if !route.verified {
			return empty, access, c, ErrConflict
		}
		if route.view.Revision > generation {
			generation = route.view.Revision
		}
		if seen[route.view.InboundID] {
			continue
		}
		seen[route.view.InboundID] = true
		// Compare applied membership, identity and aggregate expiry with the
		// current native projection. Individual quota/time is enforced by the
		// HUB ledger; it need not change a shared Xray config. The separate
		// terms fingerprint below binds each handoff to that exact entitlement.
		old, e := readExecutionSnapshot(ctx, tx, key, route.view.InboundID, route.view.Revision)
		if e != nil {
			return empty, access, c, e
		}
		current, _, e := populateExecutionUsers(ctx, tx, old, true)
		if e != nil {
			return empty, access, c, e
		}
		if ix.Digest(current.EffectiveClientIDs) != ix.Digest(old.EffectiveClientIDs) || ix.Digest(current.Profile.Users) != ix.Digest(old.Profile.Users) || ix.Digest(current.Principals) != ix.Digest(old.Principals) || current.ExpiresUnix != old.ExpiresUnix || current.Dormant != old.Dormant {
			return empty, access, c, ErrConflict
		}
	}
	projection, err := renderExecutionSubscription(clientID, format, routes)
	if err != nil {
		return empty, access, c, err
	}
	if !projection.PathVerified || projection.ExpiresUnixNano <= time.Now().UnixNano() || generation == 0 || len(projection.Content) == 0 || len(projection.Content) > 32768 {
		return empty, access, c, ErrConflict
	}
	return TelegramDelivery{ClientID: clientID, Content: string(projection.Content), ContentType: projection.ContentType, SHA256: projection.SHA256, TermsSHA256: telegramClientTerms(c), Generation: generation, ExpiresUnixNano: projection.ExpiresUnixNano}, access, c, nil
}

// TelegramServiceDelivery returns only the requesting customer's current,
// verified native routes. No Telegram or browser caller supplies credentials,
// a readiness flag, an arbitrary URL, or another customer's actor identity.
func (s *Store) TelegramServiceDelivery(ctx context.Context, key []byte, a TelegramActor, clientID, format string) (TelegramDelivery, error) {
	if ctx == nil || !validTelegramActor(a) || !idRE.MatchString(clientID) {
		return TelegramDelivery{}, ErrTelegramDenied
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return TelegramDelivery{}, err
	}
	defer tx.Rollback()
	out, _, _, err := telegramServiceDeliveryTx(ctx, tx, key, a, clientID, format)
	return out, err
}

// TelegramConfirmServiceDelivery is called only after a definite Telegram send
// receipt. It revalidates current native evidence, ownership and receipt terms
// in the same transaction as the immutable handoff record. An uncertain send
// or an expired proof cannot unlock referral credit.
func (s *Store) TelegramConfirmServiceDelivery(ctx context.Context, key []byte, a TelegramActor, clientID, sha, preparedTerms string, expiresNano, messageID int64) error {
	if ctx == nil || !validTelegramActor(a) || !idRE.MatchString(clientID) || len(sha) != 64 || len(preparedTerms) != 64 || messageID <= 0 {
		return ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	out, access, c, err := telegramServiceDeliveryTx(ctx, tx, key, a, clientID, "uri")
	if err != nil {
		return err
	}
	if out.SHA256 != sha || out.TermsSHA256 != preparedTerms || expiresNano <= time.Now().UnixNano() || expiresNano > out.ExpiresUnixNano {
		return ErrConflict
	}
	terms := telegramClientTerms(c)
	rows, err := tx.QueryContext(ctx, `SELECT o.order_id,r.receipt_json,r.created_unix FROM telegram_commerce_orders o JOIN telegram_commerce_order_receipts r ON r.order_id=o.order_id WHERE o.store_id=? AND o.customer_id=? AND o.client_id=? AND o.status='paid_pending_provisioning' ORDER BY r.created_unix,o.order_id LIMIT 1001`, access.Store.ID, access.CustomerID, clientID)
	if err != nil {
		return err
	}
	type candidate struct {
		id      string
		raw     []byte
		created int64
	}
	list := []candidate{}
	for rows.Next() {
		var item candidate
		if e := rows.Scan(&item.id, &item.raw, &item.created); e != nil {
			rows.Close()
			return e
		}
		list = append(list, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(list) > 1000 {
		return ErrUnavailable
	}
	now := time.Now().UTC().Unix()
	for _, item := range list {
		var receipt TelegramOrderReceipt
		if json.Unmarshal(item.raw, &receipt) != nil || receipt.Order.ID != item.id || receipt.Client.ID != clientID {
			return ErrUnavailable
		}
		if telegramClientTerms(receipt.Client) != terms {
			continue
		}
		if item.created > now {
			return ErrUnavailable
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO telegram_commerce_order_deliveries(order_id,store_id,customer_id,client_id,content_sha256,client_terms_sha256,generation,valid_until_unix_nano,delivered_unix,telegram_message_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, item.id, access.Store.ID, access.CustomerID, clientID, sha, terms, out.Generation, expiresNano, now, messageID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Historical handoff proof remains valid after its short native lease expires.
// The only writer above checked that lease at the definite send receipt. This
// function binds the proof to the exact immutable order terms, not to a manual
// "delivered" checkbox or the presence of a payment alone.
func telegramOrderDeliveryVerifiedTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error) {
	return telegramHistoricalDeliveryValidTx(ctx, tx, orderID, false)
}

func telegramHistoricalDeliveryValidTx(ctx context.Context, tx *sql.Tx, orderID string, allowRefund bool) (bool, error) {
	var raw []byte
	var store, customer, client, terms, status, contentSHA string
	var delivered, validUntil, messageID, created, generation int64
	err := tx.QueryRowContext(ctx, `SELECT d.store_id,d.customer_id,d.client_id,d.client_terms_sha256,d.generation,d.valid_until_unix_nano,d.delivered_unix,d.telegram_message_id,o.status,r.receipt_json,r.created_unix,d.content_sha256 FROM telegram_commerce_order_deliveries d JOIN telegram_commerce_orders o ON o.order_id=d.order_id AND o.store_id=d.store_id AND o.customer_id=d.customer_id AND o.client_id=d.client_id JOIN telegram_commerce_order_receipts r ON r.order_id=o.order_id JOIN telegram_commerce_payments p ON p.order_id=o.order_id AND p.customer_id=o.customer_id WHERE d.order_id=?`, orderID).Scan(&store, &customer, &client, &terms, &generation, &validUntil, &delivered, &messageID, &status, &raw, &created, &contentSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status != "paid_pending_provisioning" && !(allowRefund && status == "refunded") || generation <= 0 || messageID <= 0 || created <= 0 || delivered < created || delivered > time.Now().UTC().Unix()+300 || validUntil <= delivered*int64(time.Second) {
		return false, nil
	}
	if hash, err := hex.DecodeString(contentSHA); err != nil || len(hash) != 32 {
		return false, ErrUnavailable
	}
	var receipt TelegramOrderReceipt
	if json.Unmarshal(raw, &receipt) != nil {
		return false, ErrUnavailable
	}
	if receipt.Order.ID != orderID || receipt.Order.StoreID != store || receipt.Order.CustomerID != customer || receipt.Client.ID != client || telegramClientTerms(receipt.Client) != terms {
		return false, ErrUnavailable
	}
	return true, nil
}

// Inspect every historical handoff, even if no referral release ever consumed
// it. A later verified refund preserves the evidence but forbids new rewards.
func validateTelegramDeliveryDataTx(ctx context.Context, tx *sql.Tx) error {
	cursor := ""
	for {
		rows, err := tx.QueryContext(ctx, "SELECT order_id FROM telegram_commerce_order_deliveries WHERE order_id>? ORDER BY order_id LIMIT 200", cursor)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
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
			valid, err := telegramHistoricalDeliveryValidTx(ctx, tx, id, true)
			if err != nil || !valid {
				return ErrUnavailable
			}
		}
		cursor = ids[len(ids)-1]
	}
}
