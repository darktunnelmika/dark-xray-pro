package hubcore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Telegram commerce beyond manual orders, carried over from the old panel:
//
//   - Telegram Stars (XTR) payments: the bot sends an invoice, Telegram
//     charges the customer, and the order is fulfilled on the spot;
//   - a Stars wallet (store credit): referral rewards and owner gifts land
//     there, and customers can pay with it;
//   - referrals: every customer has an invite link; when an invited
//     customer buys, the inviter earns Stars credit;
//   - one free trial per Telegram account.
//
// The wallet is an append-only ledger; the balance is its sum.

const commerceSchema = `
CREATE TABLE IF NOT EXISTS tg_wallet (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id INTEGER NOT NULL,
  unix INTEGER NOT NULL,
  delta INTEGER NOT NULL,
  kind TEXT NOT NULL,
  order_id INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS tg_wallet_chat ON tg_wallet(chat_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS tg_wallet_reward_once ON tg_wallet(order_id, kind) WHERE kind='reward';
CREATE TRIGGER IF NOT EXISTS tg_wallet_no_update BEFORE UPDATE ON tg_wallet BEGIN SELECT RAISE(ABORT,'wallet is append-only'); END;
CREATE TRIGGER IF NOT EXISTS tg_wallet_no_delete BEFORE DELETE ON tg_wallet BEGIN SELECT RAISE(ABORT,'wallet is append-only'); END;
CREATE TABLE IF NOT EXISTS tg_trials (
  chat_id INTEGER PRIMARY KEY,
  email TEXT NOT NULL,
  unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tg_referrals (
  chat_id INTEGER PRIMARY KEY,
  referrer INTEGER NOT NULL,
  unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tg_ref_codes (
  chat_id INTEGER PRIMARY KEY,
  code TEXT NOT NULL UNIQUE
);
`

func (s *Store) ensureCommerceSchema() error {
	if _, err := s.db.Exec(commerceSchema); err != nil {
		return err
	}
	for _, c := range []struct{ t, col, def string }{
		{"products", "price_stars", "INTEGER NOT NULL DEFAULT 0"},
		{"orders", "method", "TEXT NOT NULL DEFAULT 'manual'"},
		{"orders", "stars", "INTEGER NOT NULL DEFAULT 0"},
		{"orders", "charge_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumn(c.t, c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

// ---- policies (owner settings) ----

type TrialPolicy struct {
	Enabled  bool     `json:"enabled"`
	GB       int64    `json:"gb"`    // 0 = unlimited
	Hours    int64    `json:"hours"` // from creation
	MaxIPs   int      `json:"max_ips"`
	Inbounds []string `json:"inbounds"`
}

type ReferralPolicy struct {
	Enabled   bool  `json:"enabled"`
	Percent   int64 `json:"percent"`    // of the order's Stars price
	Fixed     int64 `json:"fixed"`      // Stars per qualifying order
	FirstOnly bool  `json:"first_only"` // only the invited customer's first purchase
	MaxReward int64 `json:"max_reward"` // per order, 0 = no cap
}

func (s *Store) TrialPolicy(ctx context.Context) TrialPolicy {
	p := TrialPolicy{GB: 1, Hours: 24, MaxIPs: 1}
	_ = json.Unmarshal([]byte(s.Setting(ctx, "trial_policy")), &p)
	return p
}

func (s *Store) PutTrialPolicy(ctx context.Context, p TrialPolicy) error {
	if p.GB < 0 || p.GB > 1000 || p.Hours < 1 || p.Hours > 24*30 || p.MaxIPs < 0 {
		return fmt.Errorf("%w: trial is 0-1000 GB for 1-720 hours", ErrInvalid)
	}
	if p.Enabled && len(p.Inbounds) == 0 {
		return fmt.Errorf("%w: choose the inbounds a trial uses", ErrInvalid)
	}
	for _, t := range p.Inbounds {
		var n int
		if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, t).Scan(&n) != nil || n == 0 {
			return fmt.Errorf("%w: unknown inbound %q", ErrInvalid, t)
		}
	}
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, "trial_policy", string(b))
}

func (s *Store) ReferralPolicy(ctx context.Context) ReferralPolicy {
	p := ReferralPolicy{Percent: 10, FirstOnly: false}
	_ = json.Unmarshal([]byte(s.Setting(ctx, "referral_policy")), &p)
	return p
}

func (s *Store) PutReferralPolicy(ctx context.Context, p ReferralPolicy) error {
	if p.Percent < 0 || p.Percent > 100 || p.Fixed < 0 || p.Fixed > 100000 || p.MaxReward < 0 {
		return fmt.Errorf("%w: percent 0-100, fixed 0-100000 Stars", ErrInvalid)
	}
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, "referral_policy", string(b))
}

// ---- wallet ----

// WalletBalance is a chat's Stars credit.
func (s *Store) WalletBalance(ctx context.Context, chat int64) int64 {
	var b int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM tg_wallet WHERE chat_id=?`, chat).Scan(&b)
	return b
}

type WalletEntry struct {
	Unix    int64  `json:"unix"`
	Delta   int64  `json:"delta"`
	Kind    string `json:"kind"`
	OrderID int64  `json:"order_id"`
	Note    string `json:"note"`
}

func (s *Store) WalletHistory(ctx context.Context, chat int64, limit int) ([]WalletEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT unix,delta,kind,order_id,note FROM tg_wallet WHERE chat_id=? ORDER BY id DESC LIMIT ?`, chat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WalletEntry{}
	for rows.Next() {
		var e WalletEntry
		if err := rows.Scan(&e.Unix, &e.Delta, &e.Kind, &e.OrderID, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WalletAdjust adds (gift) or removes (correction) credit; never below zero.
func (s *Store) WalletAdjust(ctx context.Context, chat, delta int64, note string) (int64, error) {
	if delta == 0 || delta > 1_000_000 || delta < -1_000_000 || len(note) > 300 {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM tg_wallet WHERE chat_id=?`, chat).Scan(&bal); err != nil {
		return 0, err
	}
	if bal+delta < 0 {
		return 0, fmt.Errorf("%w: the balance is only %d Stars", ErrInvalid, bal)
	}
	kind := "gift"
	if delta < 0 {
		kind = "correction"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tg_wallet(chat_id,unix,delta,kind,note) VALUES(?,?,?,?,?)`, chat, s.now().Unix(), delta, kind, note); err != nil {
		return 0, err
	}
	return bal + delta, tx.Commit()
}

// ---- Stars checkout ----

var ErrNotForStars = errors.New("this product is not sold for Stars")

// StarsCheckout opens a Stars order; the bot sends an invoice for it.
func (s *Store) StarsCheckout(ctx context.Context, chat int64, productID, renew string) (id, stars int64, title string, err error) {
	var priceStars int64
	var name, seller, status string
	if err := s.ro.QueryRowContext(ctx, `SELECT price_stars,name,seller,status FROM products WHERE id=?`, productID).Scan(&priceStars, &name, &seller, &status); err != nil {
		return 0, 0, "", ErrNotFound
	}
	if seller != "" || status != "published" {
		return 0, 0, "", ErrNotFound
	}
	if priceStars <= 0 {
		return 0, 0, "", ErrNotForStars
	}
	// Reuse an open invoice for the same thing (a double tap is harmless),
	// unless the buyer now holds a discount code for it.
	var open int64
	if _, held := s.HeldDiscount(ctx, TgBuyer(chat), productID); held {
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note=note||' · replaced' WHERE buyer=? AND product_id=? AND client_email=? AND status='pending' AND method='stars'`,
			TgBuyer(chat), productID, renew)
	}
	_ = s.ro.QueryRowContext(ctx, `SELECT id FROM orders WHERE buyer=? AND product_id=? AND client_email=? AND status='pending' AND method='stars' ORDER BY id DESC LIMIT 1`,
		TgBuyer(chat), productID, renew).Scan(&open)
	if open > 0 {
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET stars=?, created_unix=? WHERE id=?`, priceStars, s.now().Unix(), open)
		return open, priceStars, name, nil
	}
	id, err = s.CreateOrder(ctx, Principal{User: "telegram", Role: "owner"}, productID, TgBuyer(chat), renew, "Telegram Stars")
	if err != nil {
		return 0, 0, "", err
	}
	if pct := s.orderStarsPercent(ctx, id); pct > 0 {
		priceStars = max(1, priceStars*(100-pct)/100)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE orders SET method='stars', stars=? WHERE id=?`, priceStars, id); err != nil {
		return 0, 0, "", err
	}
	return id, priceStars, name, nil
}

// StarsPrecheck validates a pre-checkout query before Telegram charges.
func (s *Store) StarsPrecheck(ctx context.Context, orderID, chat, amount int64) error {
	var buyer, status, method, productStatus string
	var stars int64
	err := s.ro.QueryRowContext(ctx, `SELECT o.buyer,o.status,o.method,o.stars,COALESCE(p.status,'') FROM orders o LEFT JOIN products p ON p.id=o.product_id WHERE o.id=?`, orderID).
		Scan(&buyer, &status, &method, &stars, &productStatus)
	switch {
	case err != nil:
		return errors.New("order not found")
	case buyer != TgBuyer(chat):
		return errors.New("this invoice belongs to someone else")
	case status != "pending" || method != "stars":
		return errors.New("this order is closed — open the shop again")
	case stars != amount:
		return errors.New("the price changed — open the shop again")
	case productStatus != "published":
		return errors.New("this product is no longer sold")
	}
	return nil
}

// StarsPaid records a successful payment and fulfills the order. The
// charge id makes a repeated delivery of the same payment harmless.
func (s *Store) StarsPaid(ctx context.Context, orderID, chat, amount int64, chargeID string) (string, error) {
	var buyer, status, prev, email string
	var stars int64
	if err := s.ro.QueryRowContext(ctx, `SELECT buyer,status,stars,charge_id,client_email FROM orders WHERE id=?`, orderID).Scan(&buyer, &status, &stars, &prev, &email); err != nil {
		return "", ErrNotFound
	}
	if prev == chargeID && status == "fulfilled" {
		return email, nil
	}
	if buyer != TgBuyer(chat) || stars != amount {
		return "", fmt.Errorf("%w: payment does not match order %d", ErrInvalid, orderID)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE orders SET charge_id=?, note=note||? WHERE id=?`, chargeID, " · paid "+fmt.Sprint(amount)+"⭐", orderID); err != nil {
		return "", err
	}
	return s.FulfillOrder(ctx, Principal{User: "telegram-stars", Role: "owner"}, orderID)
}

// StarsRefundInfo returns what the bot needs to refund a Stars order.
func (s *Store) StarsRefundInfo(ctx context.Context, orderID int64) (chat int64, chargeID string, err error) {
	var buyer, method, status string
	if err := s.ro.QueryRowContext(ctx, `SELECT buyer,method,status,charge_id FROM orders WHERE id=?`, orderID).Scan(&buyer, &method, &status, &chargeID); err != nil {
		return 0, "", ErrNotFound
	}
	c, ok := TgChatOfBuyer(buyer)
	if method != "stars" || chargeID == "" || status != "fulfilled" || !ok {
		return 0, "", fmt.Errorf("%w: only delivered Stars orders can be refunded", ErrInvalid)
	}
	return c, chargeID, nil
}

// StarsRefunded closes a refunded order; a client it created is disabled.
func (s *Store) StarsRefunded(ctx context.Context, orderID int64) error {
	var email string
	var renew bool
	if err := s.ro.QueryRowContext(ctx, `SELECT client_email,renew FROM orders WHERE id=?`, orderID).Scan(&email, &renew); err != nil {
		return ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note=note||' · refunded' WHERE id=?`, orderID); err != nil {
		return err
	}
	if !renew && email != "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE clients SET enabled=0 WHERE email=?`, email)
	}
	return nil
}

// WalletPay buys a product with wallet credit and fulfills it at once.
func (s *Store) WalletPay(ctx context.Context, chat int64, productID, renew string) (string, int64, error) {
	var priceStars int64
	var seller, status string
	if err := s.ro.QueryRowContext(ctx, `SELECT price_stars,seller,status FROM products WHERE id=?`, productID).Scan(&priceStars, &seller, &status); err != nil || seller != "" || status != "published" {
		return "", 0, ErrNotFound
	}
	if priceStars <= 0 {
		return "", 0, ErrNotForStars
	}
	id, err := s.CreateOrder(ctx, Principal{User: "telegram", Role: "owner"}, productID, TgBuyer(chat), renew, "wallet")
	if err != nil {
		return "", 0, err
	}
	if pct := s.orderStarsPercent(ctx, id); pct > 0 {
		priceStars = max(1, priceStars*(100-pct)/100)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM tg_wallet WHERE chat_id=?`, chat).Scan(&bal); err != nil {
		return "", 0, err
	}
	if bal < priceStars {
		_ = tx.Rollback() // the store has one writer connection: release it first
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note='wallet: not enough credit' WHERE id=?`, id)
		s.releaseDiscount(ctx, id)
		return "", bal, fmt.Errorf("%w: not enough credit (%d of %d Stars)", ErrInvalid, bal, priceStars)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tg_wallet(chat_id,unix,delta,kind,order_id,note) VALUES(?,?,?,?,?,?)`, chat, s.now().Unix(), -priceStars, "spend", id, ""); err != nil {
		return "", 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET method='wallet', stars=? WHERE id=?`, priceStars, id); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	email, err := s.FulfillOrder(ctx, Principal{User: "telegram-wallet", Role: "owner"}, id)
	if err != nil {
		// Give the credit back: the order could not be delivered.
		_, _ = s.db.ExecContext(ctx, `INSERT INTO tg_wallet(chat_id,unix,delta,kind,order_id,note) VALUES(?,?,?,?,?,?)`, chat, s.now().Unix(), priceStars, "refund", id, "delivery failed")
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled' WHERE id=? AND status='pending'`, id)
		return "", bal, err
	}
	return email, bal - priceStars, nil
}

// ---- referrals ----

const refAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// RefCode is a chat's invite code (made on first use).
func (s *Store) RefCode(ctx context.Context, chat int64) (string, error) {
	var code string
	if err := s.ro.QueryRowContext(ctx, `SELECT code FROM tg_ref_codes WHERE chat_id=?`, chat).Scan(&code); err == nil {
		return code, nil
	}
	for i := 0; i < 5; i++ {
		var b strings.Builder
		for j := 0; j < 7; j++ {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(refAlphabet))))
			b.WriteByte(refAlphabet[n.Int64()])
		}
		code = b.String()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO tg_ref_codes(chat_id,code) VALUES(?,?)`, chat, code); err == nil {
			return code, nil
		}
		// a code or chat clash: read again (another request may have won)
		if err := s.ro.QueryRowContext(ctx, `SELECT code FROM tg_ref_codes WHERE chat_id=?`, chat).Scan(&code); err == nil {
			return code, nil
		}
	}
	return "", errors.New("could not make an invite code")
}

// AttributeReferral records who invited a chat. Only new customers (no
// orders yet, not already invited) count, and nobody invites themselves.
func (s *Store) AttributeReferral(ctx context.Context, chat int64, code string) (int64, error) {
	if !s.ReferralPolicy(ctx).Enabled {
		return 0, ErrNotFound
	}
	var referrer int64
	if err := s.ro.QueryRowContext(ctx, `SELECT chat_id FROM tg_ref_codes WHERE code=?`, strings.ToLower(strings.TrimSpace(code))).Scan(&referrer); err != nil {
		return 0, ErrNotFound
	}
	if referrer == chat {
		return 0, fmt.Errorf("%w: own invite link", ErrInvalid)
	}
	var orders int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE buyer=?`, TgBuyer(chat)).Scan(&orders)
	if orders > 0 {
		return 0, fmt.Errorf("%w: only new customers can be invited", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tg_referrals(chat_id,referrer,unix) VALUES(?,?,?)`, chat, referrer, s.now().Unix())
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, fmt.Errorf("%w: already invited", ErrInvalid)
	}
	return referrer, nil
}

type ReferralSummary struct {
	Code    string `json:"code"`
	Invited int    `json:"invited"`
	Earned  int64  `json:"earned"`
	Balance int64  `json:"balance"`
}

func (s *Store) ReferralSummary(ctx context.Context, chat int64) (ReferralSummary, error) {
	code, err := s.RefCode(ctx, chat)
	if err != nil {
		return ReferralSummary{}, err
	}
	r := ReferralSummary{Code: code, Balance: s.WalletBalance(ctx, chat)}
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_referrals WHERE referrer=?`, chat).Scan(&r.Invited)
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM tg_wallet WHERE chat_id=? AND kind='reward'`, chat).Scan(&r.Earned)
	return r, nil
}

// rewardReferral credits the inviter after an invited customer's order is
// delivered. Returns the inviter and the reward (0 when none applies).
func (s *Store) rewardReferral(ctx context.Context, orderID int64) (int64, int64) {
	pol := s.ReferralPolicy(ctx)
	if !pol.Enabled {
		return 0, 0
	}
	var buyer string
	var stars int64
	if err := s.ro.QueryRowContext(ctx, `SELECT buyer,stars FROM orders WHERE id=? AND status='fulfilled'`, orderID).Scan(&buyer, &stars); err != nil {
		return 0, 0
	}
	chat, ok := TgChatOfBuyer(buyer)
	if !ok {
		return 0, 0
	}
	var referrer int64
	if err := s.ro.QueryRowContext(ctx, `SELECT referrer FROM tg_referrals WHERE chat_id=?`, chat).Scan(&referrer); err != nil {
		return 0, 0
	}
	if pol.FirstOnly {
		var earlier int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE buyer=? AND status='fulfilled' AND id<?`, buyer, orderID).Scan(&earlier)
		if earlier > 0 {
			return 0, 0
		}
	}
	reward := pol.Fixed + stars*pol.Percent/100
	if pol.MaxReward > 0 && reward > pol.MaxReward {
		reward = pol.MaxReward
	}
	if reward <= 0 {
		return 0, 0
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tg_wallet(chat_id,unix,delta,kind,order_id,note) VALUES(?,?,?,?,?,?)`,
		referrer, s.now().Unix(), reward, "reward", orderID, "invited customer bought")
	if err != nil {
		return 0, 0 // the unique index makes a second reward for the same order impossible
	}
	return referrer, reward
}

// ---- trial ----

var ErrTrialUsed = errors.New("the free trial was already used")

// ClaimTrial creates the one free trial of a Telegram account.
func (s *Store) ClaimTrial(ctx context.Context, chat int64) (string, error) {
	p := s.TrialPolicy(ctx)
	if !p.Enabled {
		return "", ErrNotFound
	}
	var used string
	if err := s.ro.QueryRowContext(ctx, `SELECT email FROM tg_trials WHERE chat_id=?`, chat).Scan(&used); err == nil {
		return used, ErrTrialUsed
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	email := fmt.Sprintf("trial-%d", chat)
	now := s.now().Unix()
	c := Client{Email: email, Enabled: true, VolumeBytes: -1, MaxIPs: p.MaxIPs, Inbounds: p.Inbounds, ExpiresUnix: now + p.Hours*3600}
	if p.GB > 0 {
		c.VolumeBytes = p.GB << 30
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO tg_trials(chat_id,email,unix) VALUES(?,?,?)`, chat, email, now); err != nil {
		return "", ErrTrialUsed
	}
	if err := putClientTx(ctx, tx, c, now); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET note='free trial' WHERE email=?`, email); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tg_links(chat_id,email) VALUES(?,?)`, chat, email); err != nil {
		return "", err
	}
	return email, tx.Commit()
}

// TrialUsed reports whether a chat already took its trial.
func (s *Store) TrialUsed(ctx context.Context, chat int64) bool {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_trials WHERE chat_id=?`, chat).Scan(&n)
	return n > 0
}

// ExpireStarsInvoices closes unpaid Stars invoices older than a day, quietly.
func (s *Store) ExpireStarsInvoices(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note=note||' · invoice expired', tg_owner_notified=1, tg_buyer_notified=1
WHERE status='pending' AND method='stars' AND created_unix<?`, s.now().Unix()-86400)
}
