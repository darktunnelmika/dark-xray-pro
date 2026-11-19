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
CREATE TABLE IF NOT EXISTS trial_claims (
  seller TEXT NOT NULL,
  chat_id INTEGER NOT NULL,
  email TEXT NOT NULL,
  unix INTEGER NOT NULL,
  PRIMARY KEY (seller, chat_id)
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
		{"clients", "trial", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumn(c.t, c.col, c.def); err != nil {
			return err
		}
	}
	// What became of a trial: did its owner ever connect, were they reminded.
	for _, c := range [][2]string{{"connected", "INTEGER NOT NULL DEFAULT 0"}, {"reminded", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := s.addColumn("trial_claims", c[0], c[1]); err != nil {
			return err
		}
	}
	// Trials taken before each seller had its own count belong to the owner.
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO trial_claims(seller,chat_id,email,unix) SELECT '',chat_id,email,unix FROM tg_trials`); err != nil {
		return err
	}
	return nil
}

// ---- policies (owner settings) ----

type TrialPolicy struct {
	Enabled  bool     `json:"enabled"`
	GB       int64    `json:"gb"`    // 0 = unlimited (unless MB is set)
	MB       int64    `json:"mb"`    // a volume under one GB, e.g. 50; replaces GB when set
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

// Bytes is the trial's volume; -1 means unlimited.
func (p TrialPolicy) Bytes() int64 {
	switch {
	case p.MB > 0:
		return p.MB << 20
	case p.GB > 0:
		return p.GB << 30
	}
	return -1
}

func trialKey(seller string) string {
	if seller == "" {
		return "trial_policy"
	}
	return "trial_policy:" + seller
}

// TrialPolicy is the owner's free trial; TrialPolicyFor is a seller's ("" =
// the owner). A trial is off until its seller turns it on.
func (s *Store) TrialPolicy(ctx context.Context) TrialPolicy { return s.TrialPolicyFor(ctx, "") }

func (s *Store) TrialPolicyFor(ctx context.Context, seller string) TrialPolicy {
	p := TrialPolicy{GB: 1, Hours: 24, MaxIPs: 1}
	_ = json.Unmarshal([]byte(s.Setting(ctx, trialKey(seller))), &p)
	return p
}

func (s *Store) PutTrialPolicy(ctx context.Context, p TrialPolicy) error {
	return s.PutTrialPolicyFor(ctx, "", p)
}

// PutTrialPolicyFor saves a seller's trial. A reseller's trial is a volume
// client charged to the reseller's credit, so it has a volume and whole days;
// sub-sellers sell only their reseller's plans and have no trial.
func (s *Store) PutTrialPolicyFor(ctx context.Context, seller string, p TrialPolicy) error {
	if p.MB > 0 {
		p.GB = 0 // one or the other
	}
	if p.GB < 0 || p.GB > 1000 || p.MB < 0 || p.MB > 1000*1024 || p.Hours < 1 || p.Hours > 24*30 || p.MaxIPs < 0 {
		return fmt.Errorf("%w: trial is 0-1000 GB for 1-720 hours", ErrInvalid)
	}
	if seller != "" {
		if s.IsSubSeller(ctx, seller) {
			return fmt.Errorf("%w: sub-sellers have no free trial", ErrPolicy)
		}
		if p.Bytes() < 0 {
			return fmt.Errorf("%w: a reseller's trial needs a volume", ErrInvalid)
		}
	}
	if p.Enabled && len(p.Inbounds) == 0 {
		p.Inbounds = s.SellerInbounds(ctx, seller)
		if len(p.Inbounds) == 0 {
			return fmt.Errorf("%w: choose the inbounds a trial uses", ErrInvalid)
		}
	}
	for _, t := range p.Inbounds {
		var n int
		if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, t).Scan(&n) != nil || n == 0 {
			return fmt.Errorf("%w: unknown inbound %q", ErrInvalid, t)
		}
	}
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, trialKey(seller), string(b))
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
	// EarnedToman is what invitations brought into the toman wallet.
	EarnedToman int64 `json:"earned_toman"`
}

func (s *Store) ReferralSummary(ctx context.Context, chat int64) (ReferralSummary, error) {
	code, err := s.RefCode(ctx, chat)
	if err != nil {
		return ReferralSummary{}, err
	}
	r := ReferralSummary{Code: code, Balance: s.WalletBalance(ctx, chat)}
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_referrals WHERE referrer=?`, chat).Scan(&r.Invited)
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM tg_wallet WHERE chat_id=? AND kind='reward'`, chat).Scan(&r.Earned)
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM toman_wallet WHERE seller='' AND chat_id=? AND kind='gift' AND note='invited customer bought'`, chat).Scan(&r.EarnedToman)
	return r, nil
}

// rewardReferral credits the inviter after an invited customer's order is
// delivered. Returns the inviter and the reward (0 when none applies).
func (s *Store) rewardReferral(ctx context.Context, orderID int64) (int64, int64) {
	pol := s.ReferralPolicy(ctx)
	if !pol.Enabled {
		return 0, 0
	}
	var buyer, seller string
	var stars int64
	if err := s.ro.QueryRowContext(ctx, `SELECT buyer,stars,seller FROM orders WHERE id=? AND status='fulfilled'`, orderID).Scan(&buyer, &stars, &seller); err != nil {
		return 0, 0
	}
	if seller != "" {
		return 0, 0 // invitations belong to the owner's shop; resellers' sales reward nobody
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

// rewardReferralToman is the same reward for an order paid in toman (card or
// toman wallet): the percentage of what was paid goes to the inviter's toman
// wallet in the owner's shop. Once per order.
func (s *Store) rewardReferralToman(ctx context.Context, orderID int64) (int64, int64) {
	pol := s.ReferralPolicy(ctx)
	if !pol.Enabled || pol.Percent <= 0 {
		return 0, 0
	}
	var buyer, seller string
	var stars, amount int64
	if err := s.ro.QueryRowContext(ctx, `SELECT buyer,stars,seller,amount FROM orders WHERE id=? AND status='fulfilled'`, orderID).Scan(&buyer, &stars, &seller, &amount); err != nil {
		return 0, 0
	}
	if seller != "" || stars > 0 || amount <= 0 {
		return 0, 0
	}
	chat, ok := TgChatOfBuyer(buyer)
	if !ok {
		return 0, 0
	}
	var referrer int64
	if err := s.ro.QueryRowContext(ctx, `SELECT referrer FROM tg_referrals WHERE chat_id=?`, chat).Scan(&referrer); err != nil || referrer == 0 {
		return 0, 0
	}
	if pol.FirstOnly {
		var earlier int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE buyer=? AND status='fulfilled' AND id<?`, buyer, orderID).Scan(&earlier)
		if earlier > 0 {
			return 0, 0
		}
	}
	reward := amount * pol.Percent / 100
	if reward <= 0 {
		return 0, 0
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,order_id,note)
SELECT '',?,?,?,'gift',?,'invited customer bought' WHERE NOT EXISTS (SELECT 1 FROM toman_wallet WHERE seller='' AND chat_id=? AND order_id=? AND kind='gift')`,
		referrer, s.now().Unix(), reward, orderID, referrer, orderID)
	if err != nil {
		return 0, 0
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return 0, 0
	}
	return referrer, reward
}

// ---- trial ----

var ErrTrialUsed = errors.New("the free trial was already used")

// ClaimTrial creates the one free trial a Telegram account gets from a
// seller ("" = the owner). A reseller's trial is that reseller's own client,
// charged to their credit like any client they create.
func (s *Store) ClaimTrial(ctx context.Context, seller string, chat int64) (string, error) {
	p := s.TrialPolicyFor(ctx, seller)
	if !p.Enabled {
		return "", ErrNotFound
	}
	var used string
	if err := s.ro.QueryRowContext(ctx, `SELECT email FROM trial_claims WHERE seller=? AND chat_id=?`, seller, chat).Scan(&used); err == nil {
		return used, ErrTrialUsed
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	email := fmt.Sprintf("trial-%d", chat)
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=? COLLATE NOCASE`, email).Scan(&n)
	if n != 0 {
		email += "-" + randomHex(2)
	}
	now := s.now().Unix()
	c := Client{Email: email, Enabled: true, VolumeBytes: p.Bytes(), MaxIPs: p.MaxIPs, Inbounds: p.Inbounds, ExpiresUnix: now + p.Hours*3600}
	mark := func(q interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	}) error {
		if _, err := q.ExecContext(ctx, `UPDATE clients SET trial=1, note='free trial' WHERE email=?`, email); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO tg_links(chat_id,email) VALUES(?,?)`, chat, email)
		return err
	}
	if seller != "" {
		// The claim goes in first so two taps cannot charge the reseller twice.
		if _, err := s.db.ExecContext(ctx, `INSERT INTO trial_claims(seller,chat_id,email,unix) VALUES(?,?,?,?)`, seller, chat, email, now); err != nil {
			return "", ErrTrialUsed
		}
		if err := s.RepPutClient(ctx, "trial", seller, c, "volume"); err != nil {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM trial_claims WHERE seller=? AND chat_id=?`, seller, chat)
			return "", err
		}
		return email, mark(s.db)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO trial_claims(seller,chat_id,email,unix) VALUES('',?,?,?)`, chat, email, now); err != nil {
		return "", ErrTrialUsed
	}
	if err := putClientTx(ctx, tx, c, now); err != nil {
		return "", err
	}
	if err := mark(tx); err != nil {
		return "", err
	}
	return email, tx.Commit()
}

// TrialUsed reports whether a chat already took this seller's trial.
func (s *Store) TrialUsed(ctx context.Context, seller string, chat int64) bool {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM trial_claims WHERE seller=? AND chat_id=?`, seller, chat).Scan(&n)
	return n > 0
}

// TrialStats is how a seller's trial campaign is going.
type TrialStats struct {
	Taken     int `json:"taken"`     // accounts that took a trial
	Connected int `json:"connected"` // of those, how many ever used it
	Bought    int `json:"bought"`    // of those, how many bought or renewed afterwards
	Running   int `json:"running"`   // trials still valid now
}

// noteTrialUse records which trials were ever used, before the clients go.
func (s *Store) noteTrialUse(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE trial_claims SET connected=1 WHERE connected=0 AND email IN
(SELECT email FROM client_usage WHERE up+down>0)`)
}

func (s *Store) TrialStats(ctx context.Context, seller string) TrialStats {
	s.noteTrialUse(ctx)
	var st TrialStats
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(connected),0) FROM trial_claims WHERE seller=?`, seller).Scan(&st.Taken, &st.Connected)
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM trial_claims t WHERE t.seller=? AND EXISTS
(SELECT 1 FROM orders o WHERE o.buyer='tg:'||t.chat_id AND o.seller=t.seller AND o.status='fulfilled' AND o.created_unix>=t.unix)`, seller).Scan(&st.Bought)
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM trial_claims t JOIN clients c ON c.email=t.email WHERE t.seller=? AND c.trial=1 AND c.enabled=1 AND c.expires_unix>?`, seller, s.now().Unix()).Scan(&st.Running)
	return st
}

// TrialEnding is a trial about to end whose owner has not been reminded.
type TrialEnding struct {
	Chat    int64
	Email   string
	Expires int64
}

// TrialsEnding lists a seller's trials that end within the next day and
// were not reminded yet.
func (s *Store) TrialsEnding(ctx context.Context, seller string) []TrialEnding {
	now := s.now().Unix()
	rows, err := s.ro.QueryContext(ctx, `SELECT t.chat_id,t.email,c.expires_unix FROM trial_claims t JOIN clients c ON c.email=t.email
WHERE t.seller=? AND t.reminded=0 AND c.trial=1 AND c.enabled=1 AND c.expires_unix>? AND c.expires_unix<=?`, seller, now, now+86400)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []TrialEnding
	for rows.Next() {
		var e TrialEnding
		if rows.Scan(&e.Chat, &e.Email, &e.Expires) == nil {
			out = append(out, e)
		}
	}
	return out
}

// TrialReminded marks a trial's reminder as sent.
func (s *Store) TrialReminded(ctx context.Context, seller string, chat int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE trial_claims SET reminded=1 WHERE seller=? AND chat_id=?`, seller, chat)
}

// trialKeepDays is how long an ended trial stays listed before it is removed.
const trialKeepDays = 3

// PurgeTrials deletes trial clients that ended more than trialKeepDays ago.
// The claim stays, so the same Telegram account cannot take another one.
func (s *Store) PurgeTrials(ctx context.Context) int {
	s.noteTrialUse(ctx) // before the clients and their usage are gone
	rows, err := s.ro.QueryContext(ctx, `SELECT email, rep_id FROM clients WHERE trial=1 AND expires_unix>0 AND expires_unix<?`, s.now().Unix()-trialKeepDays*86400)
	if err != nil {
		return 0
	}
	var gone [][2]string
	for rows.Next() {
		var e, r string
		if rows.Scan(&e, &r) == nil {
			gone = append(gone, [2]string{e, r})
		}
	}
	rows.Close()
	n := 0
	for _, g := range gone {
		var err error
		if g[1] != "" {
			_, _, err = s.RepDeleteClient(ctx, "trial", g[1], g[0])
		}
		if g[1] == "" || err != nil {
			err = s.DeleteClient(ctx, g[0])
		}
		if err == nil {
			n++
		}
	}
	return n
}

// ExpireStarsInvoices closes unpaid Stars invoices older than a day, quietly.
func (s *Store) ExpireStarsInvoices(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note=note||' · invoice expired', tg_owner_notified=1, tg_buyer_notified=1
WHERE status='pending' AND method='stars' AND created_unix<?`, s.now().Unix()-86400)
}
