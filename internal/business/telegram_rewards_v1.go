package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Referral rewards are promotional XTR credit, not cash or a second wallet.
// Terms are frozen when an invitation is accepted. Payments, fulfillment and
// verified native delivery are separate facts; only all three release credit.
type TelegramReferralPolicyV1 struct {
	Enabled             bool   `json:"enabled"`
	Mode                string `json:"mode"`
	FirstPurchase       int64  `json:"first_purchase"`
	RepeatPurchase      int64  `json:"repeat_purchase"`
	MaxPerOrderStars    int64  `json:"max_per_order_stars"`
	PerReferrerCapStars int64  `json:"per_referrer_cap_stars"`
	AttributionDays     int64  `json:"attribution_days"`
	HoldHours           int64  `json:"hold_hours"`
	ExpiresUnix         int64  `json:"expires_unix"`
	Revision            int64  `json:"revision"`
}

type TelegramReferralPolicyChangeV1 struct {
	EventID          string                   `json:"event_id"`
	ExpectedRevision int64                    `json:"expected_revision"`
	Policy           TelegramReferralPolicyV1 `json:"policy"`
}

type TelegramReferralAttributionV1 struct {
	StoreID            string                   `json:"store_id"`
	ReferredCustomerID string                   `json:"referred_customer_id"`
	ReferrerCustomerID string                   `json:"referrer_customer_id"`
	Policy             TelegramReferralPolicyV1 `json:"policy"`
	CreatedUnix        int64                    `json:"created_unix"`
	ExpiresUnix        int64                    `json:"expires_unix"`
}

type TelegramReferralSummaryV1 struct {
	Code               string                   `json:"code"`
	StartPayload       string                   `json:"start_payload"`
	Policy             TelegramReferralPolicyV1 `json:"policy"`
	InvitedCustomers   int64                    `json:"invited_customers"`
	QualifiedPurchases int64                    `json:"qualified_purchases"`
	PendingStars       int64                    `json:"pending_stars"`
	ReleasedStars      int64                    `json:"released_stars"`
	ReversedStars      int64                    `json:"reversed_stars"`
	Recent30DayStars   int64                    `json:"recent_30_day_stars"`
}

type TelegramReferralRewardV1 struct {
	OrderID            string `json:"order_id"`
	StoreID            string `json:"store_id"`
	ReferrerCustomerID string `json:"referrer_customer_id"`
	ReferredCustomerID string `json:"referred_customer_id"`
	AmountStars        int64  `json:"amount_stars"`
	State              string `json:"state"`
	ReleaseAfterUnix   int64  `json:"release_after_unix"`
	CreatedUnix        int64  `json:"created_unix"`
	ReleasedUnix       int64  `json:"released_unix,omitempty"`
	ReversedUnix       int64  `json:"reversed_unix,omitempty"`
}

var telegramRewardsSchemaV1 = []string{
	`CREATE TABLE telegram_rewards_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE telegram_rewards_policies(store_id TEXT PRIMARY KEY REFERENCES telegram_commerce_stores(store_id),policy_json BLOB NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),updated_unix INTEGER NOT NULL)`,
	`CREATE TABLE telegram_rewards_codes(code TEXT PRIMARY KEY,store_id TEXT NOT NULL,customer_id TEXT NOT NULL,created_unix INTEGER NOT NULL,UNIQUE(store_id,customer_id),FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))`,
	`CREATE TABLE telegram_rewards_attributions(store_id TEXT NOT NULL,referred_customer_id TEXT PRIMARY KEY,referrer_customer_id TEXT NOT NULL,code TEXT NOT NULL REFERENCES telegram_rewards_codes(code),policy_json BLOB NOT NULL,created_unix INTEGER NOT NULL,expires_unix INTEGER NOT NULL,CHECK(referred_customer_id!=referrer_customer_id),FOREIGN KEY(store_id,referred_customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id),FOREIGN KEY(store_id,referrer_customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))`,
	`CREATE INDEX telegram_rewards_referrer ON telegram_rewards_attributions(store_id,referrer_customer_id,created_unix)`,
	`CREATE TABLE telegram_rewards_grants(order_id TEXT PRIMARY KEY REFERENCES telegram_commerce_orders(order_id),store_id TEXT NOT NULL,referrer_customer_id TEXT NOT NULL,referred_customer_id TEXT NOT NULL,amount_stars INTEGER NOT NULL CHECK(typeof(amount_stars)='integer' AND amount_stars BETWEEN 0 AND 1000000000),policy_json BLOB NOT NULL,payment_bot_id INTEGER NOT NULL,charge_id TEXT NOT NULL,receipt_sha256 TEXT NOT NULL CHECK(length(receipt_sha256)=64),release_after_unix INTEGER NOT NULL,created_unix INTEGER NOT NULL,FOREIGN KEY(store_id,referrer_customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id),FOREIGN KEY(store_id,referred_customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id),FOREIGN KEY(payment_bot_id,charge_id) REFERENCES telegram_commerce_payments(bot_id,charge_id))`,
	`CREATE INDEX telegram_rewards_grants_referrer ON telegram_rewards_grants(store_id,referrer_customer_id,created_unix DESC,order_id)`,
	`CREATE TABLE telegram_rewards_releases(order_id TEXT PRIMARY KEY REFERENCES telegram_rewards_grants(order_id),wallet_event_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_wallet_ledger(event_id),created_unix INTEGER NOT NULL)`,
	`CREATE TABLE telegram_rewards_reversals(order_id TEXT PRIMARY KEY REFERENCES telegram_rewards_grants(order_id),refund_order_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_payment_refunds(order_id),amount_stars INTEGER NOT NULL CHECK(amount_stars>=0),released INTEGER NOT NULL CHECK(released IN(0,1)),wallet_event_id TEXT REFERENCES telegram_commerce_wallet_ledger(event_id),created_unix INTEGER NOT NULL,CHECK(order_id=refund_order_id))`,
	`CREATE TABLE telegram_rewards_events(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),kind TEXT NOT NULL,store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),actor_user_id INTEGER NOT NULL CHECK(actor_user_id>0),receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)`,
}

func init() {
	for _, table := range []string{"codes", "attributions", "grants", "releases", "reversals", "events", "meta"} {
		for _, operation := range []string{"UPDATE", "DELETE"} {
			telegramRewardsSchemaV1 = append(telegramRewardsSchemaV1, `CREATE TRIGGER telegram_rewards_`+table+`_no_`+strings.ToLower(operation)+` BEFORE `+operation+` ON telegram_rewards_`+table+` BEGIN SELECT RAISE(ABORT,'referral history is immutable'); END`)
		}
	}
}

func (s *Store) EnableTelegramRewardsV1(ctx context.Context) error {
	return s.enableTelegramExtension(ctx, "telegram_rewards_", telegramRewardsSchemaV1)
}

func (s *Store) VerifyTelegramRewardsV1(ctx context.Context) error {
	return s.verifyTelegramExtension(ctx, "telegram_rewards_", telegramRewardsSchemaV1, validateTelegramRewardsRowsV1)
}

func telegramValidReferralPolicy(p TelegramReferralPolicyV1) bool {
	if p.Mode != "percent_bps" && p.Mode != "fixed_stars" || p.FirstPurchase < 0 || p.RepeatPurchase < 0 || p.MaxPerOrderStars < 0 || p.MaxPerOrderStars > 1_000_000_000 || p.PerReferrerCapStars < 0 || p.PerReferrerCapStars > 1_000_000_000 || p.AttributionDays < 1 || p.AttributionDays > 3650 || p.HoldHours < 0 || p.HoldHours > 720 || p.ExpiresUnix < 0 || p.Revision < 0 {
		return false
	}
	if p.Mode == "percent_bps" && (p.FirstPurchase > 10000 || p.RepeatPurchase > 10000) || p.Mode == "fixed_stars" && (p.FirstPurchase > 1_000_000_000 || p.RepeatPurchase > 1_000_000_000) {
		return false
	}
	if p.Enabled && (p.MaxPerOrderStars < 1 || p.PerReferrerCapStars < 1 || p.FirstPurchase == 0 && p.RepeatPurchase == 0) {
		return false
	}
	return true
}

func telegramReferralPolicyTx(ctx context.Context, tx *sql.Tx, store string) (TelegramReferralPolicyV1, error) {
	p := TelegramReferralPolicyV1{Mode: "percent_bps", AttributionDays: 30}
	var raw []byte
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT policy_json,revision FROM telegram_rewards_policies WHERE store_id=?`, store).Scan(&raw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if len(raw) > 8192 || json.Unmarshal(raw, &p) != nil || !telegramValidReferralPolicy(p) || p.Revision != revision {
		return TelegramReferralPolicyV1{}, ErrUnavailable
	}
	return p, nil
}

// GetTelegramReferralPolicyV1 is a read-only manager settings query. It does
// not create a referral code or change customer attribution/statistics.
func (s *Store) GetTelegramReferralPolicyV1(ctx context.Context, actor TelegramActor) (TelegramReferralPolicyV1, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramReferralPolicyV1{}, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return TelegramReferralPolicyV1{}, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "manage_store")
	if err != nil {
		return TelegramReferralPolicyV1{}, err
	}
	return telegramReferralPolicyTx(ctx, tx, a.Store.ID)
}

func (s *Store) TelegramSetReferralPolicyV1(ctx context.Context, actor TelegramActor, req TelegramReferralPolicyChangeV1) (TelegramReferralPolicyV1, error) {
	var out TelegramReferralPolicyV1
	if !eventRE.MatchString(req.EventID) || req.ExpectedRevision < 0 || req.Policy.Revision != 0 || !telegramValidReferralPolicy(req.Policy) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return out, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "manage_store")
	if err != nil {
		return out, err
	}
	if !telegramAccessHasPermissionV1(a, "finance") {
		return out, ErrTelegramDenied
	}
	fp := fingerprint("telegram-referral-policy", struct {
		Actor   TelegramActor
		Request TelegramReferralPolicyChangeV1
	}{actor, req})
	var prior string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,receipt_json FROM telegram_rewards_events WHERE event_id=? AND kind='policy'`, req.EventID).Scan(&prior, &raw)
	if err == nil {
		if prior != fp || len(raw) > 8192 || json.Unmarshal(raw, &out) != nil {
			return out, ErrConflict
		}
		want := req.Policy
		want.Revision = req.ExpectedRevision + 1
		if out != want {
			return TelegramReferralPolicyV1{}, ErrUnavailable
		}
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	current, err := telegramReferralPolicyTx(ctx, tx, a.Store.ID)
	if err != nil {
		return out, err
	}
	if current.Revision != req.ExpectedRevision {
		return out, ErrConflict
	}
	out = req.Policy
	out.Revision = current.Revision + 1
	raw, err = json.Marshal(out)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_policies(store_id,policy_json,revision,updated_unix) VALUES(?,?,?,?) ON CONFLICT(store_id) DO UPDATE SET policy_json=excluded.policy_json,revision=excluded.revision,updated_unix=excluded.updated_unix`, a.Store.ID, raw, out.Revision, now); err != nil {
		return TelegramReferralPolicyV1{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_events(event_id,fingerprint,kind,store_id,actor_user_id,receipt_json,created_unix) VALUES(?,?,'policy',?,?,?,?)`, req.EventID, fp, a.Store.ID, actor.UserID, raw, now); err != nil {
		return TelegramReferralPolicyV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramReferralPolicyV1{}, err
	}
	return out, nil
}

func (s *Store) TelegramReferralV1(ctx context.Context, actor TelegramActor) (TelegramReferralSummaryV1, error) {
	var out TelegramReferralSummaryV1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return out, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	out.Policy, err = telegramReferralPolicyTx(ctx, tx, a.Store.ID)
	if err != nil {
		return out, err
	}
	err = tx.QueryRowContext(ctx, `SELECT code FROM telegram_rewards_codes WHERE store_id=? AND customer_id=?`, a.Store.ID, a.CustomerID).Scan(&out.Code)
	if errors.Is(err, sql.ErrNoRows) {
		out.Code, err = telegramOpaqueID()
		if err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_codes(code,store_id,customer_id,created_unix) VALUES(?,?,?,?)`, out.Code, a.Store.ID, a.CustomerID, time.Now().UTC().Unix()); err != nil {
			return TelegramReferralSummaryV1{}, err
		}
	} else if err != nil {
		return out, err
	}
	out.StartPayload = "ref_" + out.Code
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_rewards_attributions WHERE store_id=? AND referrer_customer_id=?`, a.Store.ID, a.CustomerID).Scan(&out.InvitedCustomers); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_rewards_grants g WHERE g.store_id=? AND g.referrer_customer_id=? AND g.amount_stars>0 AND NOT EXISTS(SELECT 1 FROM telegram_rewards_reversals r WHERE r.order_id=g.order_id)`, a.Store.ID, a.CustomerID).Scan(&out.QualifiedPurchases); err != nil {
		return out, err
	}
	queries := []struct {
		sql  string
		out  *int64
		args []any
	}{
		{`SELECT COALESCE(SUM(g.amount_stars),0) FROM telegram_rewards_grants g WHERE g.store_id=? AND g.referrer_customer_id=? AND NOT EXISTS(SELECT 1 FROM telegram_rewards_releases l WHERE l.order_id=g.order_id) AND NOT EXISTS(SELECT 1 FROM telegram_rewards_reversals r WHERE r.order_id=g.order_id)`, &out.PendingStars, []any{a.Store.ID, a.CustomerID}},
		{`SELECT COALESCE(SUM(g.amount_stars),0) FROM telegram_rewards_grants g JOIN telegram_rewards_releases l ON l.order_id=g.order_id WHERE g.store_id=? AND g.referrer_customer_id=? AND NOT EXISTS(SELECT 1 FROM telegram_rewards_reversals r WHERE r.order_id=g.order_id)`, &out.ReleasedStars, []any{a.Store.ID, a.CustomerID}},
		{`SELECT COALESCE(SUM(g.amount_stars),0) FROM telegram_rewards_grants g JOIN telegram_rewards_reversals r ON r.order_id=g.order_id WHERE g.store_id=? AND g.referrer_customer_id=? AND r.released=1`, &out.ReversedStars, []any{a.Store.ID, a.CustomerID}},
		{`SELECT COALESCE(SUM(g.amount_stars),0) FROM telegram_rewards_grants g JOIN telegram_rewards_releases l ON l.order_id=g.order_id WHERE g.store_id=? AND g.referrer_customer_id=? AND l.created_unix>=? AND NOT EXISTS(SELECT 1 FROM telegram_rewards_reversals r WHERE r.order_id=g.order_id)`, &out.Recent30DayStars, []any{a.Store.ID, a.CustomerID, time.Now().UTC().Add(-30 * 24 * time.Hour).Unix()}},
	}
	for _, q := range queries {
		if err = tx.QueryRowContext(ctx, q.sql, q.args...).Scan(q.out); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(); err != nil {
		return TelegramReferralSummaryV1{}, err
	}
	return out, nil
}

func telegramReadAttributionTx(ctx context.Context, tx *sql.Tx, store, customer string) (TelegramReferralAttributionV1, error) {
	var out TelegramReferralAttributionV1
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT store_id,referred_customer_id,referrer_customer_id,policy_json,created_unix,expires_unix FROM telegram_rewards_attributions WHERE store_id=? AND referred_customer_id=?`, store, customer).Scan(&out.StoreID, &out.ReferredCustomerID, &out.ReferrerCustomerID, &raw, &out.CreatedUnix, &out.ExpiresUnix)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if len(raw) > 8192 || json.Unmarshal(raw, &out.Policy) != nil || !telegramValidReferralPolicy(out.Policy) || !out.Policy.Enabled || out.Policy.Revision < 1 {
		return TelegramReferralAttributionV1{}, ErrUnavailable
	}
	return out, nil
}

func (s *Store) TelegramAttributeReferralV1(ctx context.Context, actor TelegramActor, code string) (TelegramReferralAttributionV1, error) {
	var out TelegramReferralAttributionV1
	code = strings.TrimPrefix(code, "ref_")
	decoded, err := hex.DecodeString(code)
	if err != nil || len(decoded) != 16 || len(code) != 32 {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return out, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	var referrer string
	if err = tx.QueryRowContext(ctx, `SELECT customer_id FROM telegram_rewards_codes WHERE code=? AND store_id=?`, code, a.Store.ID).Scan(&referrer); errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	} else if err != nil {
		return out, err
	}
	if referrer == a.CustomerID {
		return out, ErrConflict
	}
	prior, err := telegramReadAttributionTx(ctx, tx, a.Store.ID, a.CustomerID)
	if err == nil {
		if prior.ReferrerCustomerID != referrer {
			return out, ErrConflict
		}
		return prior, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	var paid int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments p JOIN telegram_commerce_orders o ON o.order_id=p.order_id WHERE o.store_id=? AND o.customer_id=?`, a.Store.ID, a.CustomerID).Scan(&paid); err != nil {
		return out, err
	}
	if paid != 0 {
		return out, ErrConflict
	}
	// Referrals are one immutable parent edge. Refuse self-referral, reciprocal
	// loops and chains exceeding the bounded inspection depth.
	current := referrer
	for depth := 0; depth < 64; depth++ {
		if current == a.CustomerID {
			return out, ErrConflict
		}
		var next string
		err = tx.QueryRowContext(ctx, `SELECT referrer_customer_id FROM telegram_rewards_attributions WHERE store_id=? AND referred_customer_id=?`, a.Store.ID, current).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return out, err
		}
		if depth == 63 {
			return out, ErrConflict
		}
		current = next
	}
	policy, err := telegramReferralPolicyTx(ctx, tx, a.Store.ID)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC().Unix()
	if !policy.Enabled || policy.ExpiresUnix != 0 && policy.ExpiresUnix <= now {
		return out, ErrConflict
	}
	out = TelegramReferralAttributionV1{StoreID: a.Store.ID, ReferredCustomerID: a.CustomerID, ReferrerCustomerID: referrer, Policy: policy, CreatedUnix: now, ExpiresUnix: now + policy.AttributionDays*86400}
	if policy.ExpiresUnix != 0 && policy.ExpiresUnix < out.ExpiresUnix {
		out.ExpiresUnix = policy.ExpiresUnix
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_attributions(store_id,referred_customer_id,referrer_customer_id,code,policy_json,created_unix,expires_unix) VALUES(?,?,?,?,?,?,?)`, out.StoreID, out.ReferredCustomerID, out.ReferrerCustomerID, code, raw, out.CreatedUnix, out.ExpiresUnix); err != nil {
		return TelegramReferralAttributionV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramReferralAttributionV1{}, err
	}
	return out, nil
}

type telegramRewardOrderV1 struct {
	OrderID, StoreID, CustomerID, ClientID, Currency, Status, ChargeID, ReceiptSHA string
	BotID, AmountStars, PaidUnix, PaymentSequence                                  int64
	Receipt                                                                        TelegramOrderReceipt
}

func telegramRewardOrderTx(ctx context.Context, tx *sql.Tx, orderID string) (telegramRewardOrderV1, error) {
	var o telegramRewardOrderV1
	var raw []byte
	var paymentCustomer, paymentCurrency string
	var paymentAmount int64
	err := tx.QueryRowContext(ctx, `SELECT o.order_id,o.store_id,o.customer_id,o.client_id,o.currency,o.status,o.amount_stars,p.bot_id,p.charge_id,p.customer_id,p.currency,p.amount_stars,p.created_unix,p.rowid,r.receipt_json FROM telegram_commerce_orders o JOIN telegram_commerce_payments p ON p.order_id=o.order_id JOIN telegram_commerce_order_receipts r ON r.order_id=o.order_id WHERE o.order_id=?`, orderID).Scan(&o.OrderID, &o.StoreID, &o.CustomerID, &o.ClientID, &o.Currency, &o.Status, &o.AmountStars, &o.BotID, &o.ChargeID, &paymentCustomer, &paymentCurrency, &paymentAmount, &o.PaidUnix, &o.PaymentSequence, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	if o.Currency != "XTR" || paymentCurrency != o.Currency || paymentCustomer != o.CustomerID || paymentAmount != o.AmountStars || o.AmountStars < 1 || o.AmountStars > 1_000_000_000 || len(raw) > 128<<10 || json.Unmarshal(raw, &o.Receipt) != nil {
		return telegramRewardOrderV1{}, ErrUnavailable
	}
	r := o.Receipt
	if r.Order.ID != o.OrderID || r.Order.StoreID != o.StoreID || r.Order.CustomerID != o.CustomerID || r.Order.ClientID != o.ClientID || r.Client.ID != o.ClientID || r.Order.Currency != o.Currency || r.Order.AmountStars != o.AmountStars {
		return telegramRewardOrderV1{}, ErrUnavailable
	}
	var binding int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_customer_clients b JOIN telegram_commerce_stores s ON s.store_id=b.store_id WHERE b.store_id=? AND b.customer_id=? AND b.client_id=? AND s.bot_id=?`, o.StoreID, o.CustomerID, o.ClientID, o.BotID).Scan(&binding); err != nil || binding != 1 {
		return telegramRewardOrderV1{}, ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	o.ReceiptSHA = hex.EncodeToString(digest[:])
	return o, nil
}

func telegramReadRewardTx(ctx context.Context, tx *sql.Tx, orderID string) (TelegramReferralRewardV1, error) {
	var out TelegramReferralRewardV1
	var released, reversed int64
	err := tx.QueryRowContext(ctx, `SELECT g.order_id,g.store_id,g.referrer_customer_id,g.referred_customer_id,g.amount_stars,g.release_after_unix,g.created_unix,COALESCE(l.created_unix,0),COALESCE(r.created_unix,0) FROM telegram_rewards_grants g LEFT JOIN telegram_rewards_releases l ON l.order_id=g.order_id LEFT JOIN telegram_rewards_reversals r ON r.order_id=g.order_id WHERE g.order_id=?`, orderID).Scan(&out.OrderID, &out.StoreID, &out.ReferrerCustomerID, &out.ReferredCustomerID, &out.AmountStars, &out.ReleaseAfterUnix, &out.CreatedUnix, &released, &reversed)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.State = "pending_delivery"
	if out.AmountStars == 0 {
		out.State = "ineligible"
	}
	if released > 0 {
		out.State = "released"
		out.ReleasedUnix = released
	}
	if reversed > 0 {
		out.State = "reversed"
		out.ReversedUnix = reversed
	}
	return out, nil
}

// TelegramSettleReferralRewardV1 is safe to call again after fulfillment,
// delivery or a refund. Amounts are derived solely from verified shared rows.
func (s *Store) TelegramSettleReferralRewardV1(ctx context.Context, actor TelegramActor, orderID string) (TelegramReferralRewardV1, error) {
	var out TelegramReferralRewardV1
	if !idRE.MatchString(orderID) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return out, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	o, err := telegramRewardOrderTx(ctx, tx, orderID)
	if err != nil {
		return out, err
	}
	if o.StoreID != a.Store.ID || o.BotID != actor.BotID {
		return out, ErrNotFound
	}
	attr, err := telegramReadAttributionTx(ctx, tx, o.StoreID, o.CustomerID)
	if err != nil {
		return out, err
	}
	if a.CustomerID != o.CustomerID && a.CustomerID != attr.ReferrerCustomerID && !telegramAccessHasPermissionV1(a, "finance") {
		return out, ErrNotFound
	}
	out, err = telegramSettleReferralRewardTx(ctx, tx, o, attr)
	if err != nil {
		return TelegramReferralRewardV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramReferralRewardV1{}, err
	}
	return out, nil
}

func telegramSettleReferralRewardTx(ctx context.Context, tx *sql.Tx, o telegramRewardOrderV1, attr TelegramReferralAttributionV1) (TelegramReferralRewardV1, error) {
	var out TelegramReferralRewardV1
	orderID := o.OrderID
	if o.StoreID != attr.StoreID || o.CustomerID != attr.ReferredCustomerID {
		return out, ErrUnavailable
	}
	var refund int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payment_refunds WHERE order_id=? AND bot_id=? AND charge_id=?`, orderID, o.BotID, o.ChargeID).Scan(&refund); err != nil {
		return out, err
	}
	prior, err := telegramReadRewardTx(ctx, tx, orderID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if refund != 0 {
		if errors.Is(err, ErrNotFound) {
			return TelegramReferralRewardV1{OrderID: orderID, StoreID: o.StoreID, ReferrerCustomerID: attr.ReferrerCustomerID, ReferredCustomerID: o.CustomerID, State: "refunded"}, nil
		}
		if err = reconcileTelegramReferralRefundTx(ctx, tx, orderID); err != nil {
			return out, err
		}
		out, err = telegramReadRewardTx(ctx, tx, orderID)
		if err != nil {
			return out, err
		}
		return out, nil
	}
	if o.Status != "paid_pending_provisioning" {
		return out, ErrConflict
	}
	if errors.Is(err, ErrNotFound) {
		p := attr.Policy
		amount := int64(0)
		if o.PaidUnix >= attr.CreatedUnix && o.PaidUnix < attr.ExpiresUnix {
			var earlier int
			// The immutable payment row order breaks equal-second timestamps by
			// actual capture order, not by the random invoice identifier. Refunds
			// never erase that purchase history and cannot earn a second "first".
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments p JOIN telegram_commerce_orders o ON o.order_id=p.order_id WHERE o.store_id=? AND o.customer_id=? AND p.rowid<?`, o.StoreID, o.CustomerID, o.PaymentSequence).Scan(&earlier); err != nil {
				return out, err
			}
			rate := p.FirstPurchase
			if earlier > 0 {
				rate = p.RepeatPurchase
			}
			amount = rate
			if p.Mode == "percent_bps" {
				amount = o.AmountStars * rate / 10000
			}
			if amount > p.MaxPerOrderStars {
				amount = p.MaxPerOrderStars
			}
			var committed int64
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(g.amount_stars),0) FROM telegram_rewards_grants g WHERE g.store_id=? AND g.referrer_customer_id=? AND NOT EXISTS(SELECT 1 FROM telegram_rewards_reversals r WHERE r.order_id=g.order_id)`, o.StoreID, attr.ReferrerCustomerID).Scan(&committed); err != nil {
				return out, err
			}
			available := p.PerReferrerCapStars - committed
			if available < 0 {
				available = 0
			}
			if amount > available {
				amount = available
			}
		}
		policyRaw, err := json.Marshal(p)
		if err != nil {
			return out, err
		}
		now := time.Now().UTC().Unix()
		if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_grants(order_id,store_id,referrer_customer_id,referred_customer_id,amount_stars,policy_json,payment_bot_id,charge_id,receipt_sha256,release_after_unix,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, o.OrderID, o.StoreID, attr.ReferrerCustomerID, o.CustomerID, amount, policyRaw, o.BotID, o.ChargeID, o.ReceiptSHA, o.PaidUnix+p.HoldHours*3600, now); err != nil {
			return out, err
		}
		prior, err = telegramReadRewardTx(ctx, tx, orderID)
		if err != nil {
			return out, err
		}
	} else {
		var receiptSHA string
		if err = tx.QueryRowContext(ctx, `SELECT receipt_sha256 FROM telegram_rewards_grants WHERE order_id=?`, orderID).Scan(&receiptSHA); err != nil || receiptSHA != o.ReceiptSHA {
			return out, ErrUnavailable
		}
	}
	if prior.State == "pending_delivery" && prior.AmountStars > 0 && prior.ReleaseAfterUnix <= time.Now().UTC().Unix() {
		verified, e := telegramOrderDeliveryVerifiedTx(ctx, tx, orderID)
		if e != nil {
			return out, e
		}
		if verified {
			eventID := "tg-referral-grant:" + orderID
			if err = appendTelegramWalletTx(ctx, tx, eventID, o.StoreID, attr.ReferrerCustomerID, "bonus", "XTR", prior.AmountStars, "verified referral purchase"); err != nil {
				return out, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_releases(order_id,wallet_event_id,created_unix) VALUES(?,?,?)`, orderID, eventID, time.Now().UTC().Unix()); err != nil {
				return out, err
			}
		}
	}
	out, err = telegramReadRewardTx(ctx, tx, orderID)
	if err != nil {
		return out, err
	}
	if out.State == "pending_delivery" && out.ReleaseAfterUnix > time.Now().UTC().Unix() {
		out.State = "holding"
	}
	return out, nil
}

// TelegramSettleDueRewardsV1 is a bounded trusted worker operation. It never
// constructs a customer TelegramActor or browser session: the worker's bot
// identity selects a currently enabled native store, and every candidate must
// have immutable payment, fulfillment and verified delivery evidence. Frozen
// invitation terms determine the liability, including any hold period.
func (s *Store) TelegramSettleDueRewardsV1(ctx context.Context, botID int64, limit int) error {
	if ctx == nil || botID <= 0 || limit < 1 || limit > 100 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return err
	}
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return err
	}
	store, err := telegramStoreByBotTx(ctx, tx, botID)
	if err != nil {
		return err
	}
	if !store.Enabled {
		return ErrTelegramDenied
	}
	rep, err := readRep(ctx, tx, store.RepresentativeID)
	if err != nil || !rep.Enabled {
		return ErrTelegramDenied
	}
	control, err := readRepresentativeControl(ctx, tx, rep.ID)
	if err != nil || control.LoginSuspended {
		return ErrTelegramDenied
	}
	rows, err := tx.QueryContext(ctx, `SELECT o.order_id FROM telegram_commerce_orders o JOIN telegram_commerce_payments p ON p.order_id=o.order_id AND p.bot_id=? JOIN telegram_commerce_order_receipts r ON r.order_id=o.order_id JOIN telegram_commerce_order_deliveries d ON d.order_id=o.order_id JOIN telegram_rewards_attributions a ON a.store_id=o.store_id AND a.referred_customer_id=o.customer_id LEFT JOIN telegram_rewards_grants g ON g.order_id=o.order_id WHERE o.store_id=? AND o.status='paid_pending_provisioning' AND NOT EXISTS(SELECT 1 FROM telegram_commerce_payment_refunds f WHERE f.order_id=o.order_id) AND NOT EXISTS(SELECT 1 FROM telegram_rewards_releases l WHERE l.order_id=o.order_id) AND NOT EXISTS(SELECT 1 FROM telegram_rewards_reversals v WHERE v.order_id=o.order_id) AND (g.order_id IS NULL OR (g.amount_stars>0 AND g.release_after_unix<=?)) ORDER BY p.rowid LIMIT ?`, botID, store.ID, time.Now().UTC().Unix(), limit)
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
	for _, id := range ids {
		verified, e := telegramOrderDeliveryVerifiedTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if !verified {
			return ErrUnavailable
		}
		o, e := telegramRewardOrderTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if o.StoreID != store.ID || o.BotID != botID {
			return ErrUnavailable
		}
		a, e := telegramReadAttributionTx(ctx, tx, store.ID, o.CustomerID)
		if e != nil {
			return e
		}
		if _, e = telegramSettleReferralRewardTx(ctx, tx, o, a); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Called inside the verified native payment-refund transaction. It never
// accepts a customer-provided refund assertion. A released reward may create
// negative bonus credit (debt) if already consumed; paid credit is unaffected.
func reconcileTelegramReferralRefundTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	present, err := telegramExtensionState(ctx, tx, "telegram_rewards_", telegramRewardsSchemaV1)
	if err != nil || !present {
		return err
	}
	reward, err := telegramReadRewardTx(ctx, tx, orderID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if reward.ReversedUnix > 0 {
		return nil
	}
	var verified int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payment_refunds r JOIN telegram_commerce_payments p ON p.order_id=r.order_id AND p.bot_id=r.bot_id AND p.charge_id=r.charge_id JOIN telegram_rewards_grants g ON g.order_id=r.order_id AND g.payment_bot_id=p.bot_id AND g.charge_id=p.charge_id WHERE r.order_id=?`, orderID).Scan(&verified)
	if err != nil {
		return err
	}
	if verified != 1 {
		return ErrTelegramPayment
	}
	var walletEvent any
	if reward.ReleasedUnix > 0 && reward.AmountStars > 0 {
		eventID := "tg-referral-reverse:" + orderID
		if err = appendTelegramBonusReversalTx(ctx, tx, eventID, reward.StoreID, reward.ReferrerCustomerID, reward.AmountStars, "verified referral purchase refunded"); err != nil {
			return err
		}
		walletEvent = eventID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_rewards_reversals(order_id,refund_order_id,amount_stars,released,wallet_event_id,created_unix) VALUES(?,?,?,?,?,?)`, orderID, orderID, reward.AmountStars, reward.ReleasedUnix > 0, walletEvent, time.Now().UTC().Unix())
	return err
}

func (s *Store) TelegramRewardHistoryV1(ctx context.Context, actor TelegramActor, limit, offset int) ([]TelegramReferralRewardV1, error) {
	limit, offset, err := telegramPageV1(limit, offset)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_rewards_"); err != nil {
		return nil, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT g.order_id,g.store_id,g.referrer_customer_id,g.referred_customer_id,g.amount_stars,g.release_after_unix,g.created_unix,COALESCE(l.created_unix,0),COALESCE(r.created_unix,0) FROM telegram_rewards_grants g LEFT JOIN telegram_rewards_releases l ON l.order_id=g.order_id LEFT JOIN telegram_rewards_reversals r ON r.order_id=g.order_id WHERE g.store_id=? AND g.referrer_customer_id=? ORDER BY g.created_unix DESC,g.order_id LIMIT ? OFFSET ?`, a.Store.ID, a.CustomerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TelegramReferralRewardV1, 0)
	for rows.Next() {
		var r TelegramReferralRewardV1
		if err = rows.Scan(&r.OrderID, &r.StoreID, &r.ReferrerCustomerID, &r.ReferredCustomerID, &r.AmountStars, &r.ReleaseAfterUnix, &r.CreatedUnix, &r.ReleasedUnix, &r.ReversedUnix); err != nil {
			return nil, err
		}
		r.State = "pending_delivery"
		if r.AmountStars == 0 {
			r.State = "ineligible"
		}
		if r.AmountStars > 0 && r.ReleaseAfterUnix > time.Now().UTC().Unix() {
			r.State = "holding"
		}
		if r.ReleasedUnix > 0 {
			r.State = "released"
		}
		if r.ReversedUnix > 0 {
			r.State = "reversed"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
