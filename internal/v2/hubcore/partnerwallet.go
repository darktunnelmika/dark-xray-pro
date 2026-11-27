package hubcore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// A partner key's billing. "cap" (the default) is the credit limit alone:
// the owner and the partner settle money outside the panel. "wallet" is a
// toman wallet the owner tops up; everything the key hands out is paid from
// it at the key's prices, before it happens:
//
//   - credit: price_gb per GB and price_unit per unlimited unit;
//   - a term: price_month per 30 days (or part of it);
//   - a package: its price less package_discount percent (its credit and
//     its days included).
//
// Credit taken back or left in a deleted panel is not paid back in money
// (the owner can, by hand); the credit limit, if set, still applies. The
// wallet is an append-only ledger; the balance is its sum.

const partnerWalletSchema = `
CREATE TABLE IF NOT EXISTS partner_wallet (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key_id TEXT NOT NULL,
  unix INTEGER NOT NULL,
  amount INTEGER NOT NULL,
  kind TEXT NOT NULL,
  rep_id TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS partner_wallet_key ON partner_wallet(key_id, id);
CREATE TRIGGER IF NOT EXISTS partner_wallet_no_update BEFORE UPDATE ON partner_wallet BEGIN SELECT RAISE(ABORT,'the wallet is append-only'); END;
CREATE TRIGGER IF NOT EXISTS partner_wallet_no_delete BEFORE DELETE ON partner_wallet BEGIN SELECT RAISE(ABORT,'the wallet is append-only'); END;
`

func (s *Store) ensurePartnerWalletSchema() error {
	for _, c := range []struct{ col, def string }{
		{"billing", "TEXT NOT NULL DEFAULT 'cap'"},
		{"price_gb", "INTEGER NOT NULL DEFAULT 0"},
		{"price_unit", "INTEGER NOT NULL DEFAULT 0"},
		{"price_month", "INTEGER NOT NULL DEFAULT 0"},
		{"package_discount", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumn("partner_keys", c.col, c.def); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(partnerWalletSchema)
	return err
}

// errPartnerFunds means a partner's wallet does not cover a cost.
var errPartnerFunds = errors.New("insufficient funds")

// PartnerBilling is how a key pays, with its prices (toman).
type PartnerBilling struct {
	Billing         string `json:"billing"`          // cap | wallet
	PriceGB         int64  `json:"price_gb"`         // per GB of credit
	PriceUnit       int64  `json:"price_unit"`       // per unlimited unit
	PriceMonth      int64  `json:"price_month"`      // per 30 days of a panel's term
	PackageDiscount int    `json:"package_discount"` // percent off package prices
}

func (s *Store) partnerBilling(ctx context.Context, keyID string) PartnerBilling {
	var b PartnerBilling
	_ = s.ro.QueryRowContext(ctx, `SELECT billing,price_gb,price_unit,price_month,package_discount FROM partner_keys WHERE id=?`, keyID).
		Scan(&b.Billing, &b.PriceGB, &b.PriceUnit, &b.PriceMonth, &b.PackageDiscount)
	if b.Billing == "" {
		b.Billing = "cap"
	}
	return b
}

func checkBilling(b PartnerBilling) error {
	if b.Billing != "cap" && b.Billing != "wallet" {
		return fmt.Errorf("%w: billing is cap or wallet", ErrInvalid)
	}
	if b.PriceGB < 0 || b.PriceUnit < 0 || b.PriceMonth < 0 || b.PriceGB > 1e10 || b.PriceUnit > 1e10 || b.PriceMonth > 1e10 {
		return fmt.Errorf("%w: prices cannot be negative", ErrInvalid)
	}
	if b.PackageDiscount < 0 || b.PackageDiscount > 100 {
		return fmt.Errorf("%w: package_discount is 0 to 100", ErrInvalid)
	}
	return nil
}

// Costs at a key's prices (toman), rounded up.
func (b PartnerBilling) creditCost(gb float64, units int64) int64 {
	return int64(math.Ceil(gb*float64(b.PriceGB)-1e-9)) + units*b.PriceUnit
}

func (b PartnerBilling) termCost(days int64) int64 {
	if days <= 0 {
		return 0
	}
	return b.PriceMonth * ((days + 29) / 30)
}

func (b PartnerBilling) packageCost(p RepPackage) int64 {
	return p.Price * int64(100-b.PackageDiscount) / 100
}

// PartnerBalance is a key's wallet balance.
func (s *Store) PartnerBalance(ctx context.Context, keyID string) int64 {
	var bal int64
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM partner_wallet WHERE key_id=?`, keyID).Scan(&bal)
	return bal
}

// partnerCharge takes cost from a wallet key's balance (nothing for a cap
// key or a cost of 0) and returns what it took, for partnerRefund.
func (s *Store) partnerCharge(ctx context.Context, keyID string, cost int64, rep, note, actor string) (int64, error) {
	if cost <= 0 || s.partnerBilling(ctx, keyID).Billing != "wallet" {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var bal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM partner_wallet WHERE key_id=?`, keyID).Scan(&bal); err != nil {
		return 0, err
	}
	if bal < cost {
		return 0, fmt.Errorf("%w: this costs %d toman and the wallet has %d", errPartnerFunds, cost, bal)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO partner_wallet(key_id,unix,amount,kind,rep_id,note,actor) VALUES(?,?,?,?,?,?,?)`,
		keyID, s.now().Unix(), -cost, "charge", rep, note, actor); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.emitAPIEvent(ctx, partnerTarget(keyID), "wallet.charged", map[string]any{"amount": cost, "balance": bal - cost, "panel": rep, "note": note})
	return cost, nil
}

// partnerRefund gives back a charge whose action then failed.
func (s *Store) partnerRefund(ctx context.Context, keyID string, amount int64, rep, note string) {
	if amount <= 0 {
		return
	}
	if _, err := s.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO partner_wallet(key_id,unix,amount,kind,rep_id,note,actor) VALUES(?,?,?,?,?,?,?)`,
		keyID, s.now().Unix(), amount, "refund", rep, note, "system"); err == nil {
		s.emitAPIEvent(ctx, partnerTarget(keyID), "wallet.refunded", map[string]any{"amount": amount, "balance": s.PartnerBalance(ctx, keyID), "panel": rep, "note": note})
	}
}

// PartnerWalletAdjust adds money (top-up) or, negative, takes it (owner).
func (s *Store) PartnerWalletAdjust(ctx context.Context, actor, keyID string, amount int64, note string) (int64, error) {
	if amount == 0 || amount > 1e12 || amount < -1e12 {
		return 0, fmt.Errorf("%w: amount: a non-zero number of toman", ErrInvalid)
	}
	if _, err := s.partnerKey(ctx, keyID); err != nil {
		return 0, err
	}
	kind := "topup"
	if amount < 0 {
		kind = "adjust"
		if s.PartnerBalance(ctx, keyID)+amount < 0 {
			return 0, fmt.Errorf("%w: the wallet has less than that", errPartnerFunds)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO partner_wallet(key_id,unix,amount,kind,note,actor) VALUES(?,?,?,?,?,?)`,
		keyID, s.now().Unix(), amount, kind, strings.TrimSpace(note), actor); err != nil {
		return 0, err
	}
	bal := s.PartnerBalance(ctx, keyID)
	if amount > 0 {
		s.emitAPIEvent(ctx, partnerTarget(keyID), "wallet.topped_up", map[string]any{"amount": amount, "balance": bal, "note": note})
	}
	return bal, nil
}

// WalletRow is one movement of a partner's wallet.
type WalletRow struct {
	ID     int64  `json:"id"`
	Unix   int64  `json:"unix"`
	Amount int64  `json:"amount"` // toman: + in, - out
	Kind   string `json:"kind"`   // topup | charge | refund | adjust
	Panel  string `json:"panel,omitempty"`
	Note   string `json:"note,omitempty"`
}

// PartnerWallet is a key's balance, prices and history (newest first).
type PartnerWallet struct {
	PartnerBilling
	Balance    int64       `json:"balance"`
	Items      []WalletRow `json:"items"`
	NextBefore int64       `json:"next_before"`
}

func (s *Store) PartnerWalletOf(ctx context.Context, keyID string, before int64, limit int) (PartnerWallet, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if before <= 0 {
		before = 1 << 62
	}
	w := PartnerWallet{PartnerBilling: s.partnerBilling(ctx, keyID), Balance: s.PartnerBalance(ctx, keyID), Items: []WalletRow{}}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,amount,kind,rep_id,note FROM partner_wallet WHERE key_id=? AND id<? ORDER BY id DESC LIMIT ?`, keyID, before, limit)
	if err != nil {
		return w, err
	}
	defer rows.Close()
	for rows.Next() {
		var r WalletRow
		if err := rows.Scan(&r.ID, &r.Unix, &r.Amount, &r.Kind, &r.Panel, &r.Note); err != nil {
			return w, err
		}
		w.Items = append(w.Items, r)
		w.NextBefore = r.ID
	}
	return w, rows.Err()
}

// PartnerQuote is what an action would cost a key.
type PartnerQuote struct {
	Billing      string  `json:"billing"`
	Cost         int64   `json:"cost"` // toman; 0 for a cap key
	Balance      int64   `json:"balance"`
	BalanceAfter int64   `json:"balance_after"`
	CreditGB     float64 `json:"credit_gb"` // what it hands out
	Units        int64   `json:"units"`
	TermDays     int64   `json:"term_days"`
	Allowed      bool    `json:"allowed"`
	Code         string  `json:"code,omitempty"`
	Reason       string  `json:"reason,omitempty"`
}

// PartnerQuoteIn asks for a price.
type PartnerQuoteIn struct {
	Action   string  `json:"action"` // create | credit | renew
	Package  string  `json:"package"`
	CreditGB float64 `json:"credit_gb"`
	Units    int64   `json:"units"`
	Days     int64   `json:"days"` // create: term_days; renew: days
}

// partnerCost prices an action for a key: the money, and the credit and
// days it hands out.
func (s *Store) partnerCost(ctx context.Context, k PartnerKey, in PartnerQuoteIn) (cost int64, gb float64, units, days int64, err error) {
	b := s.partnerBilling(ctx, k.ID)
	var pkg *RepPackage
	if in.Package != "" {
		for _, p := range s.partnerPackages(ctx, k) {
			if p.ID == in.Package {
				p := p
				pkg = &p
			}
		}
		if pkg == nil {
			return 0, 0, 0, 0, fmt.Errorf("%w: unknown package %q", ErrInvalid, in.Package)
		}
	}
	switch in.Action {
	case "create", "":
		if pkg != nil {
			days = in.Days
			if days == 0 {
				days = pkg.Days
			}
			cost = b.packageCost(*pkg)
			if extra := days - pkg.Days; extra > 0 { // a longer term than the package's
				cost += b.termCost(extra)
			}
			return cost, float64(pkg.GB), pkg.Units, days, nil
		}
		return b.creditCost(in.CreditGB, in.Units) + b.termCost(in.Days), in.CreditGB, in.Units, in.Days, nil
	case "credit":
		return b.creditCost(in.CreditGB, in.Units), in.CreditGB, in.Units, 0, nil
	case "renew":
		if pkg != nil {
			days = in.Days
			if days == 0 {
				days = pkg.Days
			}
			cost = b.packageCost(*pkg)
			if extra := days - pkg.Days; extra > 0 {
				cost += b.termCost(extra)
			}
			return cost, float64(pkg.GB), pkg.Units, days, nil
		}
		return b.termCost(in.Days), 0, 0, in.Days, nil
	}
	return 0, 0, 0, 0, fmt.Errorf("%w: action is create, credit or renew", ErrInvalid)
}

func (s *Store) PartnerQuoteFor(ctx context.Context, k PartnerKey, in PartnerQuoteIn) (PartnerQuote, error) {
	b := s.partnerBilling(ctx, k.ID)
	q := PartnerQuote{Billing: b.Billing, Balance: s.PartnerBalance(ctx, k.ID)}
	cost, gb, units, days, err := s.partnerCost(ctx, k, in)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			q.Code, q.Reason = codeValidation, errDetail(err)
			return q, nil
		}
		return q, err
	}
	if b.Billing != "wallet" {
		cost = 0
	}
	q.Cost, q.CreditGB, q.Units, q.TermDays = cost, gb, units, days
	q.BalanceAfter = q.Balance - cost
	switch {
	case b.Billing == "wallet" && q.BalanceAfter < 0:
		q.Code, q.Reason = codeFunds, "the wallet does not cover it"
	case k.MaxGB > 0 && k.GivenGB+gb > k.MaxGB+1e-9, k.MaxUnits > 0 && k.GivenUnits+units > k.MaxUnits:
		q.Code, q.Reason = codeCreditLimit, "this key's credit limit is used up"
	default:
		q.Allowed = true
	}
	return q, nil
}

const codeFunds = "insufficient_funds"

func (s *Store) partnerWalletAdminRoutes(mux routeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v2/admin/partner-keys/{id}/wallet", guard(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.partnerKey(r.Context(), r.PathValue("id")); err != nil {
			reply(w, nil, err)
			return
		}
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		wl, err := s.PartnerWalletOf(r.Context(), r.PathValue("id"), before, 100)
		reply(w, wl, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/partner-keys/{id}/wallet", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Amount int64  `json:"amount"`
			Note   string `json:"note"`
		}
		if !decode(w, r, &in) {
			return
		}
		bal, err := s.PartnerWalletAdjust(r.Context(), who(r).User, r.PathValue("id"), in.Amount, in.Note)
		if errors.Is(err, errPartnerFunds) {
			err = fmt.Errorf("%w: %s", ErrInvalid, errDetail(err))
		}
		reply(w, map[string]int64{"balance": bal}, err)
	}))
}
