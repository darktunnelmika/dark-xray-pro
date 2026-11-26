package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Panel resale: a reseller the owner allows sells panels (sub-sellers under
// itself) in its own Telegram bot, from the owner's packages. Its customer
// pays the reseller; the reseller pays the owner the wholesale price, which
// is a price per terabyte. It is paid from the reseller's toman wallet at the
// main panel or, when the owner allows it, through one bill due in 24 hours.
// A bill left unpaid past its due time stops the reseller spending credit
// (new clients, renewals, more volume) until it is paid; clients already made
// keep working.

const resaleSchema = `CREATE TABLE IF NOT EXISTS rep_resale (
  rep_id TEXT PRIMARY KEY REFERENCES reps(id) ON DELETE CASCADE,
  allowed INTEGER NOT NULL DEFAULT 0,
  credit INTEGER NOT NULL DEFAULT 0,
  price_tb INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  prices TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS rep_wallet (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  rep_id TEXT NOT NULL,
  delta INTEGER NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS rep_wallet_rep ON rep_wallet(rep_id, id);
CREATE TABLE IF NOT EXISTS rep_bills (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  rep_id TEXT NOT NULL,
  order_id INTEGER NOT NULL DEFAULT 0,
  amount INTEGER NOT NULL,
  due_unix INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'open', -- open | paid | cancelled
  paid_unix INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
  reminded INTEGER NOT NULL DEFAULT 0,  -- 1: 12 hours left, 2: 2 hours left, 3: overdue
  owner_told INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS rep_bills_rep ON rep_bills(rep_id, status);`

// BillTerm is how long a reseller has to pay a bill.
const BillTerm = 24 * 3600

// ErrFunds means the reseller's wallet does not cover a wholesale price and
// no bill may be opened for it.
var ErrFunds = errors.New("the reseller wallet does not cover the wholesale price")

func (s *Store) ensureResaleSchema() error {
	if _, err := s.db.Exec(resaleSchema); err != nil {
		return err
	}
	for _, c := range [][2]string{{"seller", "TEXT NOT NULL DEFAULT ''"}, {"cost", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := s.addColumn("rep_orders", c[0], c[1]); err != nil {
			return err
		}
	}
	return nil
}

// Resale is one reseller's panel-sale setting. The owner decides Allowed,
// Credit and PriceTB; the reseller decides Enabled and its Prices.
type Resale struct {
	Allowed bool             `json:"allowed"`
	Credit  bool             `json:"credit"`   // may owe through a 24-hour bill
	PriceTB int64            `json:"price_tb"` // its own wholesale per TB, 0 = the general one
	Enabled bool             `json:"enabled"`
	Prices  map[string]int64 `json:"prices"` // package id → its selling price
}

func (s *Store) ResaleOf(ctx context.Context, rep string) Resale {
	v := Resale{Enabled: true, Prices: map[string]int64{}}
	var prices string
	if s.ro.QueryRowContext(ctx, `SELECT allowed,credit,price_tb,enabled,prices FROM rep_resale WHERE rep_id=?`, rep).
		Scan(&v.Allowed, &v.Credit, &v.PriceTB, &v.Enabled, &prices) == nil {
		_ = json.Unmarshal([]byte(prices), &v.Prices)
		if v.Prices == nil {
			v.Prices = map[string]int64{}
		}
	}
	return v
}

// PutResaleOwner is the owner's part: whether the reseller may sell panels,
// may owe a bill, and its own wholesale price per TB (0 = the general one).
func (s *Store) PutResaleOwner(ctx context.Context, rep string, allowed, credit bool, priceTB int64) error {
	if priceTB < 0 || priceTB > 10_000_000_000 {
		return fmt.Errorf("%w: the price is out of range", ErrInvalid)
	}
	var n int
	if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, rep).Scan(&n) != nil || n != 1 {
		return ErrNotFound
	}
	if allowed && s.IsSubSeller(ctx, rep) {
		return fmt.Errorf("%w: sub-sellers cannot sell panels", ErrPolicy)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO rep_resale(rep_id,allowed,credit,price_tb) VALUES(?,?,?,?)
ON CONFLICT(rep_id) DO UPDATE SET allowed=excluded.allowed, credit=excluded.credit, price_tb=excluded.price_tb`, rep, allowed, credit, priceTB)
	return err
}

// PutResaleRep is the reseller's part: showing the sale in its bot and its
// prices. A price below the wholesale price is refused; 0 means the owner's
// own price for that package.
func (s *Store) PutResaleRep(ctx context.Context, rep string, enabled bool, prices map[string]int64) error {
	rs := s.ResaleOf(ctx, rep)
	if !rs.Allowed {
		return fmt.Errorf("%w: panel sale is not allowed for this reseller", ErrPolicy)
	}
	keep := map[string]int64{}
	for _, p := range s.RepSale(ctx).Packages {
		v, ok := prices[p.ID]
		if !ok || v == 0 {
			continue
		}
		if v < 0 || v > 100_000_000_000 {
			return fmt.Errorf("%w: a price is out of range", ErrInvalid)
		}
		if cost := s.Wholesale(ctx, rep, p); v < cost {
			return fmt.Errorf("%w: the price of %s cannot be below the wholesale price %d", ErrInvalid, p.Name, cost)
		}
		keep[p.ID] = v
	}
	b, _ := json.Marshal(keep)
	_, err := s.db.ExecContext(ctx, `UPDATE rep_resale SET enabled=?, prices=? WHERE rep_id=?`, enabled, string(b), rep)
	return err
}

// WholesaleTB is what the reseller pays the owner per terabyte.
func (s *Store) WholesaleTB(ctx context.Context, rep string) int64 {
	if v := s.ResaleOf(ctx, rep).PriceTB; v > 0 {
		return v
	}
	return s.RepSale(ctx).WholesaleTB
}

// Wholesale is the reseller's price of a package, rounded up to 1000 toman.
func (s *Store) Wholesale(ctx context.Context, rep string, p RepPackage) int64 {
	v := (p.GB*s.WholesaleTB(ctx, rep) + 1023) / 1024
	return (v + 999) / 1000 * 1000
}

// Retail is what the reseller's customer pays for a package: its own price,
// else the owner's price, never below the wholesale price.
func (s *Store) Retail(ctx context.Context, rep string, p RepPackage) int64 {
	cost := s.Wholesale(ctx, rep, p)
	if v := s.ResaleOf(ctx, rep).Prices[p.ID]; v >= cost {
		return v
	}
	return max(p.Price, cost)
}

// ResaleOn reports whether the reseller's bot sells panels now.
func (s *Store) ResaleOn(ctx context.Context, rep string) bool {
	if rep == "" {
		return false
	}
	rs := s.ResaleOf(ctx, rep)
	if !rs.Allowed || !rs.Enabled || s.IsSubSeller(ctx, rep) || len(s.RepSale(ctx).Packages) == 0 || s.WholesaleTB(ctx, rep) <= 0 {
		return false
	}
	var on int
	_ = s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, rep).Scan(&on)
	return on == 1 && s.CardFor(ctx, rep).Number != ""
}

// ResalePackages are the owner's packages at the reseller's prices.
func (s *Store) ResalePackages(ctx context.Context, rep string) []RepPackage {
	out := []RepPackage{}
	for _, p := range s.RepSale(ctx).Packages {
		p.Price = s.Retail(ctx, rep, p)
		out = append(out, p)
	}
	return out
}

// ---- the reseller's toman wallet at the main panel ----

// RepWallet is the reseller's balance in toman.
func (s *Store) RepWallet(ctx context.Context, rep string) int64 {
	var v int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM rep_wallet WHERE rep_id=?`, rep).Scan(&v)
	return v
}

// RepWalletEntry is one movement of a reseller's wallet.
type RepWalletEntry struct {
	Unix  int64  `json:"unix"`
	Delta int64  `json:"delta"`
	Note  string `json:"note"`
	Actor string `json:"actor"`
}

func (s *Store) RepWalletHistory(ctx context.Context, rep string, limit int) ([]RepWalletEntry, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT unix,delta,note,actor FROM rep_wallet WHERE rep_id=? ORDER BY id DESC LIMIT ?`, rep, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepWalletEntry{}
	for rows.Next() {
		var e RepWalletEntry
		if err := rows.Scan(&e.Unix, &e.Delta, &e.Note, &e.Actor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func walletTx(ctx context.Context, tx *sql.Tx, rep string) int64 {
	var v int64
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM rep_wallet WHERE rep_id=?`, rep).Scan(&v)
	return v
}

// RepWalletAdjust adds (or takes, if negative) toman; the balance never goes
// below zero. Open bills the new balance covers are paid right away; their
// ids come back.
func (s *Store) RepWalletAdjust(ctx context.Context, actor, rep string, delta int64, note string) (int64, []int64, error) {
	if delta == 0 || delta > 100_000_000_000 || delta < -100_000_000_000 {
		return 0, nil, ErrInvalid
	}
	var n int
	if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, rep).Scan(&n) != nil || n != 1 {
		return 0, nil, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	if walletTx(ctx, tx, rep)+delta < 0 {
		return 0, nil, ErrCredit
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO rep_wallet(unix,rep_id,delta,note,actor) VALUES(?,?,?,?,?)`, s.now().Unix(), rep, delta, note, actor); err != nil {
		return 0, nil, err
	}
	paid, err := s.settleTx(ctx, tx, rep)
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return s.RepWallet(ctx, rep), paid, nil
}

// settleTx pays open bills, oldest first, while the wallet covers them.
func (s *Store) settleTx(ctx context.Context, tx *sql.Tx, rep string) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,amount FROM rep_bills WHERE rep_id=? AND status='open' ORDER BY id`, rep)
	if err != nil {
		return nil, err
	}
	type bill struct{ id, amount int64 }
	var open []bill
	for rows.Next() {
		var b bill
		if err := rows.Scan(&b.id, &b.amount); err != nil {
			rows.Close()
			return nil, err
		}
		open = append(open, b)
	}
	rows.Close()
	var paid []int64
	now := s.now().Unix()
	for _, b := range open {
		if walletTx(ctx, tx, rep) < b.amount {
			break
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO rep_wallet(unix,rep_id,delta,note,actor) VALUES(?,?,?,?,?)`, now, rep, -b.amount, fmt.Sprintf("bill #%d", b.id), "system"); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE rep_bills SET status='paid', paid_unix=? WHERE id=?`, now, b.id); err != nil {
			return nil, err
		}
		paid = append(paid, b.id)
	}
	return paid, nil
}

// ---- bills ----

// RepBill is what a reseller owes for a panel sold on credit.
type RepBill struct {
	ID       int64  `json:"id"`
	Unix     int64  `json:"unix"`
	RepID    string `json:"rep_id"`
	OrderID  int64  `json:"order_id"`
	Amount   int64  `json:"amount"`
	DueUnix  int64  `json:"due_unix"`
	Status   string `json:"status"`
	PaidUnix int64  `json:"paid_unix"`
	Note     string `json:"note"`
	Overdue  bool   `json:"overdue"`
}

const billCols = `id,unix,rep_id,order_id,amount,due_unix,status,paid_unix,note`

func (s *Store) scanBills(ctx context.Context, q string, args ...any) ([]RepBill, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+billCols+` FROM rep_bills `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := s.now().Unix()
	out := []RepBill{}
	for rows.Next() {
		var b RepBill
		if err := rows.Scan(&b.ID, &b.Unix, &b.RepID, &b.OrderID, &b.Amount, &b.DueUnix, &b.Status, &b.PaidUnix, &b.Note); err != nil {
			return nil, err
		}
		b.Overdue = b.Status == "open" && b.DueUnix <= now
		out = append(out, b)
	}
	return out, rows.Err()
}

// RepBills lists a reseller's bills, newest first ("" = every reseller).
func (s *Store) RepBills(ctx context.Context, rep string, onlyOpen bool, limit int) ([]RepBill, error) {
	q, args := `WHERE 1=1`, []any{}
	if rep != "" {
		q += ` AND rep_id=?`
		args = append(args, rep)
	}
	if onlyOpen {
		q += ` AND status='open'`
	}
	return s.scanBills(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
}

func (s *Store) RepBillByID(ctx context.Context, id int64) (RepBill, error) {
	l, err := s.scanBills(ctx, `WHERE id=?`, id)
	if err != nil {
		return RepBill{}, err
	}
	if len(l) == 0 {
		return RepBill{}, ErrNotFound
	}
	return l[0], nil
}

// RepLocked reports whether the reseller has a bill past its due time.
func (s *Store) RepLocked(ctx context.Context, rep string) bool {
	return repLockedQ(ctx, s.ro, rep, s.now().Unix())
}

func repLockedQ(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, rep string, now int64) bool {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_bills WHERE rep_id=? AND status='open' AND due_unix<=?`, rep, now).Scan(&n)
	return n > 0
}

// PayRepBill pays an open bill from the wallet.
func (s *Store) PayRepBill(ctx context.Context, rep string, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var amount int64
	if tx.QueryRowContext(ctx, `SELECT amount FROM rep_bills WHERE id=? AND rep_id=? AND status='open'`, id, rep).Scan(&amount) != nil {
		return ErrNotFound
	}
	if walletTx(ctx, tx, rep) < amount {
		return ErrCredit
	}
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO rep_wallet(unix,rep_id,delta,note,actor) VALUES(?,?,?,?,?)`, now, rep, -amount, fmt.Sprintf("bill #%d", id), rep); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE rep_bills SET status='paid', paid_unix=? WHERE id=?`, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CloseRepBill is the owner closing a bill by hand: paid outside the panel
// (paid=true) or forgiven.
func (s *Store) CloseRepBill(ctx context.Context, actor string, id int64, paid bool) error {
	st, note := "cancelled", "cancelled by "+actor
	if paid {
		st, note = "paid", "marked paid by "+actor
	}
	r, err := s.db.ExecContext(ctx, `UPDATE rep_bills SET status=?, paid_unix=?, note=? WHERE id=? AND status='open'`, st, s.now().Unix(), note, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// BillNotice is a reminder that is due: Stage 1 at 12 hours left, 2 at two
// hours left, 3 once overdue.
type BillNotice struct {
	Bill  RepBill
	Stage int
}

// BillNotices lists the reminders due now; each must be claimed before it
// is sent.
func (s *Store) BillNotices(ctx context.Context) []BillNotice {
	open, err := s.scanBills(ctx, `WHERE status='open' ORDER BY id`)
	if err != nil {
		return nil
	}
	now := s.now().Unix()
	var reminded = map[int64]int{}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,reminded FROM rep_bills WHERE status='open'`)
	if err == nil {
		for rows.Next() {
			var id int64
			var r int
			if rows.Scan(&id, &r) == nil {
				reminded[id] = r
			}
		}
		rows.Close()
	}
	var out []BillNotice
	for _, b := range open {
		left, stage := b.DueUnix-now, 0
		switch {
		case left <= 0:
			stage = 3
		case left <= 2*3600:
			stage = 2
		case left <= 12*3600:
			stage = 1
		}
		if stage > reminded[b.ID] {
			out = append(out, BillNotice{Bill: b, Stage: stage})
		}
	}
	return out
}

// ClaimBillNotice marks a reminder sent; only the caller that gets true sends it.
func (s *Store) ClaimBillNotice(ctx context.Context, id int64, stage int) bool {
	r, err := s.db.ExecContext(ctx, `UPDATE rep_bills SET reminded=? WHERE id=? AND reminded<? AND status='open'`, stage, id, stage)
	if err != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n == 1
}

// OverdueToTell lists overdue bills the owner was not told about yet, each
// claimed by this call.
func (s *Store) OverdueToTell(ctx context.Context) []RepBill {
	l, err := s.scanBills(ctx, `WHERE status='open' AND due_unix<=? AND owner_told=0 ORDER BY id`, s.now().Unix())
	if err != nil {
		return nil
	}
	var out []RepBill
	for _, b := range l {
		if r, err := s.db.ExecContext(ctx, `UPDATE rep_bills SET owner_told=1 WHERE id=? AND owner_told=0`, b.ID); err == nil {
			if n, _ := r.RowsAffected(); n == 1 {
				out = append(out, b)
			}
		}
	}
	return out
}

// chargeResale takes the wholesale price of a sold panel from the reseller:
// from its wallet, else as a bill when it may owe one and has none open.
// It returns the bill id (0 = paid from the wallet).
func (s *Store) chargeResale(ctx context.Context, rep string, order, cost int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.now().Unix()
	if repLockedQ(ctx, tx, rep, now) {
		return 0, fmt.Errorf("%w: an overdue bill is unpaid", ErrPolicy)
	}
	var bill int64
	if walletTx(ctx, tx, rep) >= cost {
		if _, err := tx.ExecContext(ctx, `INSERT INTO rep_wallet(unix,rep_id,delta,note,actor) VALUES(?,?,?,?,?)`, now, rep, -cost, fmt.Sprintf("panel order #%d", order), "system"); err != nil {
			return 0, err
		}
	} else {
		var credit, open int
		_ = tx.QueryRowContext(ctx, `SELECT credit FROM rep_resale WHERE rep_id=?`, rep).Scan(&credit)
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_bills WHERE rep_id=? AND status='open'`, rep).Scan(&open)
		if credit != 1 || open > 0 {
			return 0, ErrFunds
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO rep_bills(unix,rep_id,order_id,amount,due_unix,note) VALUES(?,?,?,?,?,?)`, now, rep, order, cost, now+BillTerm, fmt.Sprintf("panel order #%d", order))
		if err != nil {
			return 0, err
		}
		bill, _ = r.LastInsertId()
	}
	return bill, tx.Commit()
}

// refundResale gives back a charge whose panel could not be made.
func (s *Store) refundResale(ctx context.Context, rep string, order, cost, bill int64) {
	if bill != 0 {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM rep_bills WHERE id=?`, bill)
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO rep_wallet(unix,rep_id,delta,note,actor) VALUES(?,?,?,?,?)`, s.now().Unix(), rep, cost, fmt.Sprintf("refund of panel order #%d", order), "system")
}

// CreateRepWalletOrder opens a top-up of a reseller's wallet, paid by card to
// the owner and approved like a panel order.
func (s *Store) CreateRepWalletOrder(ctx context.Context, chat int64, buyerName, rep string, amount int64) (RepOrder, error) {
	if amount < 10_000 || amount > 10_000_000_000 {
		return RepOrder{}, fmt.Errorf("%w: the amount is out of range", ErrInvalid)
	}
	if s.Card(ctx).Number == "" {
		return RepOrder{}, fmt.Errorf("%w: the owner has no card set", ErrPolicy)
	}
	if !s.ResaleOf(ctx, rep).Allowed {
		return RepOrder{}, fmt.Errorf("%w: panel sale is not allowed for this reseller", ErrPolicy)
	}
	now := s.now().Unix()
	if _, err := s.db.ExecContext(ctx, `UPDATE rep_orders SET status='cancelled', decided_unix=? WHERE chat_id=? AND status='await_receipt'`, now, chat); err != nil {
		return RepOrder{}, err
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO rep_orders(unix,chat_id,buyer_name,kind,rep_id,package,gb,price,terms) VALUES(?,?,?,?,?,?,?,?,?)`,
		now, chat, buyerName, "wallet", rep, "wallet", 0, amount, "{}")
	if err != nil {
		return RepOrder{}, err
	}
	id, _ := r.LastInsertId()
	return s.RepOrderByID(ctx, id)
}

// ResaleView is everything the panel shows about one reseller's resale.
type ResaleView struct {
	Resale      Resale           `json:"resale"`
	WholesaleTB int64            `json:"wholesale_tb"`
	On          bool             `json:"on"`
	HasCard     bool             `json:"has_card"`
	Wallet      int64            `json:"wallet"`
	Locked      bool             `json:"locked"`
	Packages    []ResalePkg      `json:"packages"`
	Bills       []RepBill        `json:"bills"`
	History     []RepWalletEntry `json:"history"`
	MainBot     string           `json:"main_bot"`
}

// ResalePkg is one package as the reseller sees it.
type ResalePkg struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	GB    int64  `json:"gb"`
	Cost  int64  `json:"cost"`  // wholesale
	Price int64  `json:"price"` // what its customers pay
	Own   int64  `json:"own"`   // its own price, 0 = the owner's
	Owner int64  `json:"owner"` // the owner's price
}

func (s *Store) ResaleViewOf(ctx context.Context, rep string) ResaleView {
	rs := s.ResaleOf(ctx, rep)
	v := ResaleView{Resale: rs, WholesaleTB: s.WholesaleTB(ctx, rep), On: s.ResaleOn(ctx, rep), HasCard: s.CardFor(ctx, rep).Number != "",
		Wallet: s.RepWallet(ctx, rep), Locked: s.RepLocked(ctx, rep), Packages: []ResalePkg{}, MainBot: s.BotUsernameFor(ctx, "")}
	for _, p := range s.RepSale(ctx).Packages {
		v.Packages = append(v.Packages, ResalePkg{ID: p.ID, Name: p.Name, GB: p.GB, Cost: s.Wholesale(ctx, rep, p), Price: s.Retail(ctx, rep, p), Own: rs.Prices[p.ID], Owner: p.Price})
	}
	v.Bills, _ = s.RepBills(ctx, rep, false, 20)
	v.History, _ = s.RepWalletHistory(ctx, rep, 20)
	return v
}

func resaleRoutes(s *Store, mux *docMux, guard, member func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/resale", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.ResaleViewOf(r.Context(), r.PathValue("id")), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/resale", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Allowed bool  `json:"allowed"`
			Credit  bool  `json:"credit"`
			PriceTB int64 `json:"price_tb"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.PutResaleOwner(r.Context(), r.PathValue("id"), in.Allowed, in.Credit, in.PriceTB)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/reps/{id}/wallet", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Delta int64  `json:"delta"`
			Note  string `json:"note"`
		}
		if !decode(w, r, &in) {
			return
		}
		bal, paid, err := s.RepWalletAdjust(r.Context(), who(r).User, r.PathValue("id"), in.Delta, in.Note)
		reply(w, map[string]any{"wallet": bal, "settled": paid}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/rep-bills/{bid}/close", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paid bool `json:"paid"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("bid"), 10, 64)
		err := s.CloseRepBill(r.Context(), who(r).User, id, in.Paid)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/rep/resale", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role != "rep" || s.IsSubSeller(r.Context(), p.RepID) {
			http.NotFound(w, r)
			return
		}
		reply(w, s.ResaleViewOf(r.Context(), p.RepID), nil)
	}))
	mux.HandleFunc("PUT /api/v2/rep/resale", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role != "rep" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Enabled bool             `json:"enabled"`
			Prices  map[string]int64 `json:"prices"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.PutResaleRep(r.Context(), p.RepID, in.Enabled, in.Prices)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/resale/bills/{bid}/pay", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role != "rep" {
			http.NotFound(w, r)
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("bid"), 10, 64)
		err := s.PayRepBill(r.Context(), p.RepID, id)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
}

// ---- the reseller's panel-sale page ----

// SellPanel is a reseller selling from its panel (no bot): a new panel
// (target "" and name) or a top-up of one of its panels, paid like a bot
// sale from its wallet or a bill.
func (s *Store) SellPanel(ctx context.Context, actor, rep, target, name, buyer, pkgID string) (RepOrderResult, error) {
	o, err := s.createRepOrder(ctx, rep, 0, buyer, target, name, pkgID, true)
	if err != nil {
		return RepOrderResult{}, err
	}
	res, err := s.ApproveRepOrder(ctx, actor, rep, o.ID)
	if err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE rep_orders SET status='cancelled', decided_unix=? WHERE id=?`, s.now().Unix(), o.ID)
		return RepOrderResult{}, err
	}
	return res, nil
}

// SoldPanel is one panel a reseller sold, with what it holds.
type SoldPanel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Credit  int64  `json:"credit"` // bytes
	Units   int64  `json:"units"`
	Clients int    `json:"clients"`
	Active  int    `json:"active"`
	Used    int64  `json:"used"`
	Sold    int64  `json:"sold"`    // unix of the sale
	TopUps  int    `json:"top_ups"` // packages bought after
	Paid    int64  `json:"paid"`    // all its customer paid
	Cost    int64  `json:"cost"`    // all the reseller paid the owner for it
	Login   string `json:"login"`
	LastUse int64  `json:"last_use"`
}

// ResaleStats sums a reseller's (or, rep "", everyone's) panel sales.
type ResaleStats struct {
	Panels     int   `json:"panels"`
	Active     int   `json:"active"`
	Credit     int64 `json:"credit"`
	MonthSales int   `json:"month_sales"`
	MonthPaid  int64 `json:"month_paid"`
	MonthCost  int64 `json:"month_cost"`
}

// SoldPanels lists the panels a reseller sold (in its bot or its panel).
func (s *Store) SoldPanels(ctx context.Context, rep string) ([]SoldPanel, ResaleStats, error) {
	var st ResaleStats
	rows, err := s.ro.QueryContext(ctx, `SELECT o.rep_id, MIN(CASE WHEN o.kind='new' THEN o.decided_unix END), SUM(o.kind='topup'), SUM(o.price), SUM(o.cost)
FROM rep_orders o WHERE o.seller=? AND o.status='approved' AND o.kind IN ('new','topup')
GROUP BY o.rep_id HAVING SUM(o.kind='new')>0`, rep)
	if err != nil {
		return nil, st, err
	}
	sold := map[string]*SoldPanel{}
	for rows.Next() {
		var p SoldPanel
		var at sql.NullInt64
		if err := rows.Scan(&p.ID, &at, &p.TopUps, &p.Paid, &p.Cost); err != nil {
			rows.Close()
			return nil, st, err
		}
		p.Sold = at.Int64
		sold[p.ID] = &p
	}
	rows.Close()
	subs, err := s.Sellers(ctx, rep)
	if err != nil {
		return nil, st, err
	}
	out := []SoldPanel{}
	for _, r := range subs {
		p, ok := sold[r.ID]
		if !ok {
			continue
		}
		p.Name, p.Enabled, p.Credit, p.Units, p.Clients, p.Used = r.Name, r.Enabled, r.VolumeCredit, r.UnlimitedUnits, r.Clients, r.UsedBytes
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(u.last_seen_unix),0) FROM clients c LEFT JOIN client_usage u ON u.email=c.email WHERE c.rep_id=? AND c.enabled=1`, r.ID).Scan(&p.Active, &p.LastUse)
		if l := s.RepLogins(ctx, r.ID); len(l) > 0 {
			p.Login = l[0]
		}
		st.Panels++
		if p.Enabled {
			st.Active++
		}
		st.Credit += p.Credit
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sold > out[j].Sold })
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(price),0),COALESCE(SUM(cost),0) FROM rep_orders WHERE seller=? AND status='approved' AND kind IN ('new','topup') AND decided_unix>=?`,
		rep, monthStart(s.now())).Scan(&st.MonthSales, &st.MonthPaid, &st.MonthCost)
	return out, st, nil
}

// SetSoldPanelEnabled switches one of the reseller's panels on or off.
func (s *Store) SetSoldPanelEnabled(ctx context.Context, rep, id string, on bool) error {
	if !s.IsSellerOf(ctx, rep, id) {
		return ErrNotFound
	}
	_, err := s.db.ExecContext(ctx, `UPDATE reps SET enabled=? WHERE id=?`, on, id)
	return err
}

// ResaleRepRow is one reseller on the owner's panel-sale page.
type ResaleRepRow struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Allowed  bool        `json:"allowed"`
	On       bool        `json:"on"`
	Credit   bool        `json:"credit"`
	PriceTB  int64       `json:"price_tb"`
	Wallet   int64       `json:"wallet"`
	OpenBill int64       `json:"open_bill"`
	Overdue  bool        `json:"overdue"`
	Stats    ResaleStats `json:"stats"`
}

// ResaleOverview lists the resellers allowed to sell panels.
func (s *Store) ResaleOverview(ctx context.Context) ([]ResaleRepRow, error) {
	reps, err := s.Reps(ctx)
	if err != nil {
		return nil, err
	}
	out := []ResaleRepRow{}
	for _, r := range reps {
		rs := s.ResaleOf(ctx, r.ID)
		if !rs.Allowed || r.ParentID != "" {
			continue
		}
		row := ResaleRepRow{ID: r.ID, Name: r.Name, Allowed: true, On: s.ResaleOn(ctx, r.ID), Credit: rs.Credit, PriceTB: rs.PriceTB, Wallet: s.RepWallet(ctx, r.ID), Overdue: s.RepLocked(ctx, r.ID)}
		_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM rep_bills WHERE rep_id=? AND status='open'`, r.ID).Scan(&row.OpenBill)
		_, row.Stats, _ = s.SoldPanels(ctx, r.ID)
		out = append(out, row)
	}
	return out, nil
}

// PendingWalletOrders are top-ups waiting for the owner.
func (s *Store) PendingWalletOrders(ctx context.Context) ([]RepOrder, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+repOrderCols+` FROM rep_orders WHERE kind='wallet' AND status='pending' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepOrder{}
	for rows.Next() {
		o, err := scanRepOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func resalePageRoutes(s *Store, mux *docMux, guard, member func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v2/admin/resale", guard(func(w http.ResponseWriter, r *http.Request) {
		reps, err := s.ResaleOverview(r.Context())
		pend, _ := s.PendingWalletOrders(r.Context())
		reply(w, map[string]any{"sale": s.RepSale(r.Context()), "reps": reps, "pending": pend}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/panels", guard(func(w http.ResponseWriter, r *http.Request) {
		list, st, err := s.SoldPanels(r.Context(), r.PathValue("id"))
		reply(w, map[string]any{"panels": list, "stats": st}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/rep-orders/{oid}/{act}", guard(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("oid"), 10, 64)
		o, err := s.RepOrderByID(r.Context(), id)
		if err != nil || o.Kind != "wallet" {
			reply(w, nil, ErrNotFound)
			return
		}
		switch r.PathValue("act") {
		case "approve":
			res, err := s.ApproveRepOrder(r.Context(), who(r).User, "", id)
			if err == nil {
				s.tell(r.Context(), o.Chat, fmt.Sprintf("✅ کیف پول شما %s تومان شارژ شد. موجودی: %s تومان.", thousands(o.Price), thousands(res.Wallet)))
			}
			reply(w, map[string]any{"ok": err == nil, "wallet": res.Wallet}, err)
		case "reject":
			_, err := s.RejectRepOrder(r.Context(), who(r).User, id)
			if err == nil {
				s.tell(r.Context(), o.Chat, fmt.Sprintf("❌ رسید شارژ کیف پول #%d تأیید نشد. اگر اشتباهی شده، با پشتیبانی در تماس باشید.", id))
			}
			reply(w, map[string]bool{"ok": err == nil}, err)
		default:
			http.NotFound(w, r)
		}
	}))
	rep := func(f func(w http.ResponseWriter, r *http.Request, rep string)) http.HandlerFunc {
		return member(func(w http.ResponseWriter, r *http.Request) {
			p := who(r)
			if p.Role != "rep" || s.IsSubSeller(r.Context(), p.RepID) || !s.ResaleOf(r.Context(), p.RepID).Allowed {
				http.NotFound(w, r)
				return
			}
			f(w, r, p.RepID)
		})
	}
	mux.HandleFunc("GET /api/v2/rep/resale/panels", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		list, st, err := s.SoldPanels(r.Context(), id)
		reply(w, map[string]any{"panels": list, "stats": st}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/resale/sell", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		var in struct {
			Target  string `json:"target"`
			Name    string `json:"name"`
			Buyer   string `json:"buyer"`
			Package string `json:"package"`
		}
		if !decode(w, r, &in) {
			return
		}
		res, err := s.SellPanel(r.Context(), who(r).User, id, in.Target, in.Name, in.Buyer, in.Package)
		reply(w, map[string]any{"ok": err == nil, "username": res.Order.RepID, "password": res.Password, "bill": res.Bill, "cost": res.Order.Cost, "address": miniBase}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/resale/panels/{pid}/enabled", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		var in struct {
			Enabled bool `json:"enabled"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetSoldPanelEnabled(r.Context(), id, r.PathValue("pid"), in.Enabled)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/resale/panels/{pid}/password", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		pid := r.PathValue("pid")
		if !s.IsSellerOf(r.Context(), id, pid) {
			reply(w, nil, ErrNotFound)
			return
		}
		user := pid
		if l := s.RepLogins(r.Context(), pid); len(l) > 0 {
			user = l[0]
		}
		pass := randomHex(6)
		err := s.SetRepLogin(r.Context(), user, pass, pid)
		reply(w, map[string]any{"ok": err == nil, "username": user, "password": pass}, err)
	}))
}

func thousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	d := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range d {
		if i > 0 && (len(d)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
