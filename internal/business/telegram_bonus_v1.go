package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Bonus discounts are optional promotional store credit, not another payment
// rail. Every discounted order still has at least one real external XTR.
type TelegramBonusPolicyV1 struct {
	StoreID  string `json:"store_id"`
	Enabled  bool   `json:"enabled"`
	Revision int64  `json:"revision"`
}

type TelegramBonusPolicyChangeV1 struct {
	EventID          string `json:"event_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
}

type TelegramBonusQuoteV1 struct {
	OrderID       string `json:"order_id"`
	GrossStars    int64  `json:"gross_stars"`
	DiscountStars int64  `json:"discount_stars"`
	NetStars      int64  `json:"net_stars"`
	ExpiresUnix   int64  `json:"expires_unix"`
	State         string `json:"state"`
}

const (
	telegramBonusReserveReason = "Telegram bonus quote reservation"
	telegramBonusReleaseReason = "Telegram bonus quote released"
	telegramBonusLateReason    = "verified late Telegram bonus payment"
	telegramBonusRefundReason  = "verified Telegram bonus order refunded"
)

// Independently versioned: the published commerce DDL, old quotes and old
// payment/receipt JSON remain unchanged when the discount is zero.
var telegramBonusSchemaV1 = []string{
	`CREATE TABLE telegram_bonus_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE telegram_bonus_policies(store_id TEXT PRIMARY KEY REFERENCES telegram_commerce_stores(store_id),enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),revision INTEGER NOT NULL CHECK(revision>0))`,
	`CREATE TABLE telegram_bonus_policy_versions(store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),revision INTEGER NOT NULL CHECK(revision>0),enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),event_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_events(event_id),PRIMARY KEY(store_id,revision))`,
	`CREATE TABLE telegram_bonus_quotes(order_id TEXT PRIMARY KEY REFERENCES telegram_commerce_orders(order_id),store_id TEXT NOT NULL,customer_id TEXT NOT NULL,gross_stars INTEGER NOT NULL CHECK(typeof(gross_stars)='integer' AND gross_stars BETWEEN 2 AND 1000000000),discount_stars INTEGER NOT NULL CHECK(typeof(discount_stars)='integer' AND discount_stars>0),net_stars INTEGER NOT NULL CHECK(typeof(net_stars)='integer' AND net_stars>0),policy_revision INTEGER NOT NULL CHECK(policy_revision>0),reserve_event_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_wallet_ledger(event_id),reward_rule TEXT NOT NULL CHECK(reward_rule='net_paid_cap_v1'),created_unix INTEGER NOT NULL CHECK(created_unix>0),expires_unix INTEGER NOT NULL CHECK(expires_unix=created_unix+900),CHECK(gross_stars=discount_stars+net_stars),FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id),FOREIGN KEY(store_id,policy_revision) REFERENCES telegram_bonus_policy_versions(store_id,revision))`,
	`CREATE INDEX telegram_bonus_quote_scope ON telegram_bonus_quotes(store_id,customer_id,expires_unix,order_id)`,
	`CREATE TABLE telegram_bonus_releases(order_id TEXT PRIMARY KEY REFERENCES telegram_bonus_quotes(order_id),wallet_event_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_wallet_ledger(event_id),reason TEXT NOT NULL CHECK(reason IN('expired','cancelled')),created_unix INTEGER NOT NULL CHECK(created_unix>0))`,
	`CREATE TABLE telegram_bonus_settlements(order_id TEXT PRIMARY KEY REFERENCES telegram_bonus_quotes(order_id),payment_bot_id INTEGER NOT NULL,charge_id TEXT NOT NULL,wallet_event_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_wallet_ledger(event_id),late INTEGER NOT NULL CHECK(late IN(0,1)),created_unix INTEGER NOT NULL CHECK(created_unix>0),FOREIGN KEY(payment_bot_id,charge_id) REFERENCES telegram_commerce_payments(bot_id,charge_id))`,
	`CREATE TABLE telegram_bonus_refunds(order_id TEXT PRIMARY KEY REFERENCES telegram_bonus_quotes(order_id),wallet_event_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_wallet_ledger(event_id),created_unix INTEGER NOT NULL CHECK(created_unix>0),FOREIGN KEY(order_id) REFERENCES telegram_commerce_payment_refunds(order_id))`,
}

func init() {
	for _, table := range []string{"meta", "policy_versions", "quotes", "releases", "settlements", "refunds"} {
		for _, operation := range []string{"UPDATE", "DELETE"} {
			telegramBonusSchemaV1 = append(telegramBonusSchemaV1, `CREATE TRIGGER telegram_bonus_`+table+`_no_`+strings.ToLower(operation)+` BEFORE `+operation+` ON telegram_bonus_`+table+` BEGIN SELECT RAISE(ABORT,'bonus redemption history is immutable'); END`)
		}
	}
}

func (s *Store) EnableTelegramBonusV1(ctx context.Context) error {
	return s.enableTelegramExtension(ctx, "telegram_bonus_", telegramBonusSchemaV1)
}

func telegramBonusReadyTx(ctx context.Context, tx *sql.Tx) error {
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil || !present {
		return ErrUnavailable
	}
	return nil
}

func telegramBonusPolicyTx(ctx context.Context, tx *sql.Tx, store string) (TelegramBonusPolicyV1, error) {
	out := TelegramBonusPolicyV1{StoreID: store}
	err := tx.QueryRowContext(ctx, `SELECT enabled,revision FROM telegram_bonus_policies WHERE store_id=?`, store).Scan(&out.Enabled, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func (s *Store) GetTelegramBonusPolicyV1(ctx context.Context, a TelegramActor) (TelegramBonusPolicyV1, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramBonusPolicyV1{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramBonusPolicyV1{}, err
	}
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil || !present {
		return TelegramBonusPolicyV1{StoreID: access.Store.ID}, err
	}
	return telegramBonusPolicyTx(ctx, tx, access.Store.ID)
}

func (s *Store) SetTelegramBonusPolicyV1(ctx context.Context, a TelegramActor, in TelegramBonusPolicyChangeV1) (TelegramBonusPolicyV1, error) {
	var out TelegramBonusPolicyV1
	if !eventRE.MatchString(in.EventID) || in.ExpectedRevision < 0 {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_store")
	if err != nil {
		return out, err
	}
	if !telegramHasPermission(access.Permissions, "finance") {
		return out, ErrTelegramDenied
	}
	if err = telegramBonusReadyTx(ctx, tx); err != nil {
		return out, err
	}
	hash := fingerprint("telegram-bonus-policy-v1", struct {
		Actor   TelegramActor
		Request TelegramBonusPolicyChangeV1
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "bonus_policy", &out); e != nil || replay {
		return out, e
	}
	prior, err := telegramBonusPolicyTx(ctx, tx, access.Store.ID)
	if err != nil {
		return out, err
	}
	if prior.Revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	out = TelegramBonusPolicyV1{StoreID: access.Store.ID, Enabled: in.Enabled, Revision: prior.Revision + 1}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "bonus_policy", out); err != nil {
		return TelegramBonusPolicyV1{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_bonus_policies(store_id,enabled,revision) VALUES(?,?,?) ON CONFLICT(store_id) DO UPDATE SET enabled=excluded.enabled,revision=excluded.revision`, out.StoreID, out.Enabled, out.Revision); err != nil {
		return TelegramBonusPolicyV1{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_bonus_policy_versions(store_id,revision,enabled,event_id) VALUES(?,?,?,?)`, out.StoreID, out.Revision, out.Enabled, in.EventID); err != nil {
		return TelegramBonusPolicyV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramBonusPolicyV1{}, err
	}
	return out, nil
}

type telegramBonusRecordV1 struct {
	Quote                                                TelegramBonusQuoteV1
	StoreID, CustomerID, ReserveEvent, RewardRule        string
	PolicyRevision, CreatedUnix                          int64
	ReleaseEvent, ReleaseReason, SettlementEvent, Charge string
	ReleaseUnix, PaymentBotID, SettledUnix               int64
	Late                                                 bool
	RefundEvent                                          string
	RefundUnix                                           int64
}

func readTelegramBonusRecordTx(ctx context.Context, tx *sql.Tx, id string) (telegramBonusRecordV1, error) {
	var r telegramBonusRecordV1
	err := tx.QueryRowContext(ctx, `SELECT q.order_id,q.store_id,q.customer_id,q.gross_stars,q.discount_stars,q.net_stars,q.policy_revision,q.reserve_event_id,q.reward_rule,q.created_unix,q.expires_unix,COALESCE(l.wallet_event_id,''),COALESCE(l.reason,''),COALESCE(l.created_unix,0),COALESCE(s.wallet_event_id,''),COALESCE(s.payment_bot_id,0),COALESCE(s.charge_id,''),COALESCE(s.late,0),COALESCE(s.created_unix,0),COALESCE(f.wallet_event_id,''),COALESCE(f.created_unix,0) FROM telegram_bonus_quotes q LEFT JOIN telegram_bonus_releases l ON l.order_id=q.order_id LEFT JOIN telegram_bonus_settlements s ON s.order_id=q.order_id LEFT JOIN telegram_bonus_refunds f ON f.order_id=q.order_id WHERE q.order_id=?`, id).Scan(&r.Quote.OrderID, &r.StoreID, &r.CustomerID, &r.Quote.GrossStars, &r.Quote.DiscountStars, &r.Quote.NetStars, &r.PolicyRevision, &r.ReserveEvent, &r.RewardRule, &r.CreatedUnix, &r.Quote.ExpiresUnix, &r.ReleaseEvent, &r.ReleaseReason, &r.ReleaseUnix, &r.SettlementEvent, &r.PaymentBotID, &r.Charge, &r.Late, &r.SettledUnix, &r.RefundEvent, &r.RefundUnix)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.Quote.State = "reserved"
	if r.ReleaseEvent != "" {
		r.Quote.State = "released"
	}
	if r.SettlementEvent != "" {
		r.Quote.State = "consumed"
	}
	if r.RefundEvent != "" {
		r.Quote.State = "refunded"
	}
	return r, nil
}

// Reservation is a debit in the existing bonus ledger. There is no independently
// writable available balance, and ordinary reservations cannot create debt.
func reserveTelegramBonusTx(ctx context.Context, tx *sql.Tx, access TelegramAccess, order TelegramOrder) error {
	if order.BonusDiscountStars == 0 {
		return nil
	}
	if err := telegramBonusReadyTx(ctx, tx); err != nil {
		return err
	}
	policy, err := telegramBonusPolicyTx(ctx, tx, access.Store.ID)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return ErrTelegramDenied
	}
	if _, err = releaseExpiredTelegramBonusTx(ctx, tx, access.Store.ID, access.CustomerID, time.Now().UTC().Unix(), 200); err != nil {
		return err
	}
	event := "tg-bonus-reserve:" + order.ID
	if err = appendTelegramWalletTx(ctx, tx, event, order.StoreID, order.CustomerID, "bonus", "XTR", -order.BonusDiscountStars, telegramBonusReserveReason); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_bonus_quotes(order_id,store_id,customer_id,gross_stars,discount_stars,net_stars,policy_revision,reserve_event_id,reward_rule,created_unix,expires_unix) VALUES(?,?,?,?,?,?,?,?,'net_paid_cap_v1',?,?)`, order.ID, order.StoreID, order.CustomerID, order.AmountStars+order.BonusDiscountStars, order.BonusDiscountStars, order.AmountStars, policy.Revision, event, order.CreatedUnix, order.ExpiresUnix)
	return err
}

func releaseTelegramBonusTx(ctx context.Context, tx *sql.Tx, r telegramBonusRecordV1, reason string, now int64) error {
	if r.ReleaseEvent != "" {
		return nil
	}
	if r.SettlementEvent != "" || r.RefundEvent != "" || reason != "expired" && reason != "cancelled" || reason == "expired" && now < r.Quote.ExpiresUnix {
		return ErrConflict
	}
	var payments int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=?`, r.Quote.OrderID).Scan(&payments); err != nil {
		return err
	}
	if payments != 0 {
		return ErrConflict
	}
	event := "tg-bonus-release:" + r.Quote.OrderID
	if err := appendTelegramWalletTx(ctx, tx, event, r.StoreID, r.CustomerID, "bonus", "XTR", r.Quote.DiscountStars, telegramBonusReleaseReason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO telegram_bonus_releases(order_id,wallet_event_id,reason,created_unix) VALUES(?,?,?,?)`, r.Quote.OrderID, event, reason, now)
	return err
}

func releaseExpiredTelegramBonusTx(ctx context.Context, tx *sql.Tx, store, customer string, now int64, limit int) (int, error) {
	query := `SELECT q.order_id FROM telegram_bonus_quotes q WHERE q.store_id=? AND q.expires_unix<=? AND NOT EXISTS(SELECT 1 FROM telegram_bonus_releases r WHERE r.order_id=q.order_id) AND NOT EXISTS(SELECT 1 FROM telegram_commerce_payments p WHERE p.order_id=q.order_id)`
	args := []any{store, now}
	if customer != "" {
		query += ` AND q.customer_id=?`
		args = append(args, customer)
	}
	query += ` ORDER BY q.expires_unix,q.order_id LIMIT ?`
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		order, _, err := readTelegramOrderTx(ctx, tx, id)
		if err != nil || order.Status != "quoted" {
			return 0, ErrUnavailable
		}
		r, err := readTelegramBonusRecordTx(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if err = releaseTelegramBonusTx(ctx, tx, r, "expired", now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// Trusted maintenance releases only expired, unpaid reservations. It may run
// for a disabled store: releasing a hold grants no sale or messaging authority.
func (s *Store) ReleaseExpiredTelegramBonusQuotesV1(ctx context.Context, botID int64, limit int) (int, error) {
	if botID <= 0 || limit < 1 || limit > 200 {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil || !present {
		return 0, err
	}
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return 0, err
	}
	store, err := telegramStoreByBotTx(ctx, tx, botID)
	if err != nil {
		return 0, err
	}
	n, err := releaseExpiredTelegramBonusTx(ctx, tx, store.ID, "", time.Now().UTC().Unix(), limit)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// Authenticated wallet refresh can release this customer's expired holds
// without waiting behind another customer's maintenance backlog.
func (s *Store) ReleaseExpiredTelegramBonusQuotesAsV1(ctx context.Context, a TelegramActor, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return 0, err
	}
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil || !present {
		return 0, err
	}
	n, err := releaseExpiredTelegramBonusTx(ctx, tx, access.Store.ID, access.CustomerID, time.Now().UTC().Unix(), limit)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) TelegramBonusQuoteV1(ctx context.Context, a TelegramActor, id string) (TelegramBonusQuoteV1, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramBonusQuoteV1{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramBonusQuoteV1{}, err
	}
	order, err := readTelegramCustomerOrderTx(ctx, tx, access, id)
	if err != nil {
		return TelegramBonusQuoteV1{}, err
	}
	if order.BonusDiscountStars == 0 {
		return TelegramBonusQuoteV1{OrderID: id, GrossStars: order.AmountStars, NetStars: order.AmountStars, ExpiresUnix: order.ExpiresUnix, State: "none"}, nil
	}
	r, err := readTelegramBonusRecordTx(ctx, tx, id)
	return r.Quote, err
}

// This cancels the reservation, not a Telegram payment. A later authenticated
// payment still consumes the original agreed discount and may create bonus debt.
func (s *Store) CancelTelegramBonusQuoteV1(ctx context.Context, a TelegramActor, id, eventID string) (TelegramBonusQuoteV1, error) {
	var out TelegramBonusQuoteV1
	if !eventRE.MatchString(eventID) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return out, err
	}
	order, err := readTelegramCustomerOrderTx(ctx, tx, access, id)
	if err != nil {
		return out, err
	}
	if order.BonusDiscountStars == 0 {
		return out, ErrInvalid
	}
	hash := fingerprint("telegram-bonus-cancel-v1", struct {
		Actor TelegramActor
		Order string
	}{a, id})
	if replay, e := telegramEventReplayTx(ctx, tx, eventID, hash, "bonus_cancel", &out); e != nil || replay {
		if e != nil {
			return TelegramBonusQuoteV1{}, e
		}
		// Keep the historical cancellation receipt immutable, but render the
		// current funding state if a captured payment has since consumed it.
		current, e := readTelegramBonusRecordTx(ctx, tx, id)
		return current.Quote, e
	}
	if order.Status != "quoted" {
		return out, ErrConflict
	}
	r, err := readTelegramBonusRecordTx(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if err = releaseTelegramBonusTx(ctx, tx, r, "cancelled", time.Now().UTC().Unix()); err != nil {
		return out, err
	}
	out = r.Quote
	out.State = "released"
	if err = appendTelegramEventTx(ctx, tx, eventID, hash, access.Store.ID, a.UserID, "bonus_cancel", out); err != nil {
		return TelegramBonusQuoteV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramBonusQuoteV1{}, err
	}
	return out, nil
}

func consumeTelegramBonusPaymentTx(ctx context.Context, tx *sql.Tx, order TelegramOrder, charge string, paidUnix int64) error {
	if order.BonusDiscountStars == 0 {
		return nil
	}
	r, err := readTelegramBonusRecordTx(ctx, tx, order.ID)
	if err != nil {
		return err
	}
	if r.SettlementEvent != "" || r.RefundEvent != "" {
		return ErrUnavailable
	}
	event := r.ReserveEvent
	late := r.ReleaseEvent != ""
	if late {
		// The exact authenticated payment row is inserted by the caller in this
		// same transaction. Released/reallocated bonus cannot erase that fact.
		var valid int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=? AND bot_id=? AND charge_id=? AND customer_id=? AND currency='XTR' AND amount_stars=?`, order.ID, order.BotID, charge, order.CustomerID, order.AmountStars).Scan(&valid); err != nil {
			return err
		}
		if valid != 1 {
			return ErrTelegramPayment
		}
		event = "tg-bonus-late:" + order.ID
		// The existing debt marker preserves old wallet fingerprints. This
		// exception is private and is backed by payment/settlement provenance.
		if err = appendTelegramWalletEntryTx(ctx, tx, event, r.StoreID, r.CustomerID, "bonus", "XTR", -r.Quote.DiscountStars, telegramBonusLateReason, true); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_bonus_settlements(order_id,payment_bot_id,charge_id,wallet_event_id,late,created_unix) VALUES(?,?,?,?,?,?)`, order.ID, order.BotID, charge, event, late, paidUnix)
	return err
}

func restoreTelegramBonusRefundTx(ctx context.Context, tx *sql.Tx, order TelegramOrder) error {
	if order.BonusDiscountStars == 0 {
		return nil
	}
	r, err := readTelegramBonusRecordTx(ctx, tx, order.ID)
	if err != nil {
		return err
	}
	if r.RefundEvent != "" || r.SettlementEvent == "" {
		return ErrUnavailable
	}
	var created int64
	if err = tx.QueryRowContext(ctx, `SELECT f.created_unix FROM telegram_commerce_payment_refunds f JOIN telegram_commerce_payments p ON p.order_id=f.order_id AND p.bot_id=f.bot_id AND p.charge_id=f.charge_id WHERE f.order_id=? AND p.bot_id=? AND p.charge_id=? AND p.customer_id=? AND p.amount_stars=?`, order.ID, r.PaymentBotID, r.Charge, r.CustomerID, r.Quote.NetStars).Scan(&created); err != nil {
		return err
	}
	event := "tg-bonus-refund:" + order.ID
	if err = appendTelegramWalletTx(ctx, tx, event, r.StoreID, r.CustomerID, "bonus", "XTR", r.Quote.DiscountStars, telegramBonusRefundReason); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_bonus_refunds(order_id,wallet_event_id,created_unix) VALUES(?,?,?)`, order.ID, event, created)
	return err
}

func telegramBonusWalletTx(ctx context.Context, tx *sql.Tx, access TelegramAccess, wallet *TelegramWallet) error {
	present, err := telegramExtensionState(ctx, tx, "telegram_bonus_", telegramBonusSchemaV1)
	if err != nil || !present {
		return err
	}
	policy, err := telegramBonusPolicyTx(ctx, tx, access.Store.ID)
	if err != nil {
		return err
	}
	wallet.CheckoutRedemptionEnabled = policy.Enabled
	return tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(q.discount_stars),0) FROM telegram_bonus_quotes q WHERE q.store_id=? AND q.customer_id=? AND NOT EXISTS(SELECT 1 FROM telegram_bonus_releases r WHERE r.order_id=q.order_id) AND NOT EXISTS(SELECT 1 FROM telegram_bonus_settlements s WHERE s.order_id=q.order_id)`, access.Store.ID, access.CustomerID).Scan(&wallet.ReservedBonusStars)
}

func (s *Store) VerifyTelegramBonusV1(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return validateTelegramBonusRowsV1(ctx, tx)
}

// Referenced by the historical policy validator without changing legacy event
// receipts or the published commerce schema.
func readTelegramBonusPolicyReceipt(raw []byte) (TelegramBonusPolicyV1, error) {
	var p TelegramBonusPolicyV1
	if len(raw) > 8192 || json.Unmarshal(raw, &p) != nil || !idRE.MatchString(p.StoreID) || p.Revision < 1 {
		return p, ErrUnavailable
	}
	return p, nil
}
