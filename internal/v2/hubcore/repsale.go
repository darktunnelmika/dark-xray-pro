package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Selling reseller panels from the owner's Telegram bot. The owner defines
// packages of volume credit; a customer buys a panel (a new reseller with a
// login and that credit), a connected reseller buys a top-up. Payment is a
// card-to-card receipt the owner approves; nothing is created before that.

const repSaleSchema = `CREATE TABLE IF NOT EXISTS rep_orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  chat_id INTEGER NOT NULL,
  buyer_name TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,            -- new | topup
  rep_id TEXT NOT NULL,          -- the wanted username (new) or the reseller (topup)
  package TEXT NOT NULL,
  gb INTEGER NOT NULL,
  price INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'await_receipt', -- await_receipt | pending | approved | rejected | cancelled
  receipt TEXT NOT NULL DEFAULT '',
  decided_unix INTEGER NOT NULL DEFAULT 0,
  decided_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS rep_orders_chat ON rep_orders(chat_id, id);`

func (s *Store) ensureRepSaleSchema() error {
	if _, err := s.db.Exec(repSaleSchema); err != nil {
		return err
	}
	// The package as it was when ordered: later edits do not change a paid order.
	return s.addColumn("rep_orders", "terms", "TEXT NOT NULL DEFAULT ''")
}

// RepPackage is one thing on sale: GB of volume credit for a price in toman,
// together with the settings a panel bought with it gets. The owner sets
// them once, on the package; approving an order needs no further setup.
type RepPackage struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	GB    int64  `json:"gb"`
	Price int64  `json:"price"`
	// The new reseller's policy.
	AllowUnlimited bool     `json:"allow_unlimited"` // may sell unlimited plans (off: volume only)
	Units          int64    `json:"units"`           // unlimited units that come with the package
	Inbounds       []string `json:"inbounds"`        // empty: every inbound
	MaxClients     int64    `json:"max_clients"`     // 0: no cap
	MaxDays        int64    `json:"max_days"`
	MaxClientGB    int64    `json:"max_client_gb"`
	CanDelete      bool     `json:"can_delete"`
	RefundUnused   bool     `json:"refund_unused"`
}

// Policy is the reseller policy a panel bought with this package starts with.
func (p RepPackage) Policy() RepPolicy {
	return RepPolicy{MaxClients: p.MaxClients, MaxDays: p.MaxDays, MaxClientGB: p.MaxClientGB, AllowUnlimited: p.AllowUnlimited,
		CanDelete: p.CanDelete, RefundUnused: p.RefundUnused, Inbounds: p.Inbounds}
}

// RepSale is the owner's setting; the sale is off until turned on.
type RepSale struct {
	Enabled  bool         `json:"enabled"`
	Packages []RepPackage `json:"packages"`
	// WholesaleTB is what a reseller selling these panels in its own bot
	// pays per terabyte (resale.go); 0 = resale is off.
	WholesaleTB int64 `json:"wholesale_tb"`
}

func (s *Store) RepSale(ctx context.Context) RepSale {
	var v RepSale
	_ = json.Unmarshal([]byte(s.Setting(ctx, "rep_sale")), &v)
	if v.Packages == nil {
		v.Packages = []RepPackage{}
	}
	return v
}

func (s *Store) PutRepSale(ctx context.Context, v RepSale) error {
	seen := map[string]bool{}
	for i := range v.Packages {
		p := &v.Packages[i]
		p.Name = strings.TrimSpace(p.Name)
		if p.GB < 1 || p.GB > 1_000_000 || p.Price < 0 || p.Price > 100_000_000_000 {
			return fmt.Errorf("%w: a package is 1 GB or more with a price", ErrInvalid)
		}
		if !p.AllowUnlimited {
			p.Units = 0
		}
		if p.Units < 0 || p.Units > 100_000 || p.MaxClients < 0 || p.MaxDays < 0 || p.MaxClientGB < 0 {
			return fmt.Errorf("%w: limits cannot be negative", ErrInvalid)
		}
		for _, t := range p.Inbounds {
			var n int
			if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, t).Scan(&n) != nil || n == 0 {
				return fmt.Errorf("%w: unknown inbound %q", ErrInvalid, t)
			}
		}
		if p.ID == "" {
			p.ID = "p" + randomHex(3)
		}
		if p.Name == "" {
			p.Name = fmt.Sprintf("%d GB", p.GB)
		}
		if seen[p.ID] {
			return fmt.Errorf("%w: duplicate package", ErrInvalid)
		}
		seen[p.ID] = true
	}
	if v.WholesaleTB < 0 || v.WholesaleTB > 10_000_000_000 {
		return fmt.Errorf("%w: the wholesale price is out of range", ErrInvalid)
	}
	if len(v.Packages) > 20 {
		return fmt.Errorf("%w: at most 20 packages", ErrInvalid)
	}
	if v.Enabled && len(v.Packages) == 0 {
		return fmt.Errorf("%w: define a package first", ErrInvalid)
	}
	if v.Enabled && s.Card(ctx).Number == "" {
		return fmt.Errorf("%w: set the payment card first", ErrInvalid)
	}
	b, _ := json.Marshal(v)
	return s.SetSetting(ctx, "rep_sale", string(b))
}

// RepOrder is one purchase of a panel or a top-up.
type RepOrder struct {
	ID        int64      `json:"id"`
	Unix      int64      `json:"unix"`
	Chat      int64      `json:"chat"`
	BuyerName string     `json:"buyer_name"`
	Kind      string     `json:"kind"`
	RepID     string     `json:"rep_id"`
	Package   string     `json:"package"`
	GB        int64      `json:"gb"`
	Price     int64      `json:"price"`
	Status    string     `json:"status"`
	Receipt   string     `json:"receipt,omitempty"`
	Terms     RepPackage `json:"terms"`  // the package as ordered
	Seller    string     `json:"seller"` // "" = sold by the owner; else the reseller whose bot sold it
	Cost      int64      `json:"cost"`   // what that reseller pays the owner
}

const repOrderCols = `id,unix,chat_id,buyer_name,kind,rep_id,package,gb,price,status,receipt,terms,seller,cost`

func scanRepOrder(r interface{ Scan(...any) error }) (RepOrder, error) {
	var o RepOrder
	var terms string
	err := r.Scan(&o.ID, &o.Unix, &o.Chat, &o.BuyerName, &o.Kind, &o.RepID, &o.Package, &o.GB, &o.Price, &o.Status, &o.Receipt, &terms, &o.Seller, &o.Cost)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if o.Kind == "wallet" {
		return o, err
	}
	if json.Unmarshal([]byte(terms), &o.Terms) != nil || o.Terms.GB == 0 {
		// An order from before packages carried settings: the old defaults.
		o.Terms = RepPackage{Name: o.Package, GB: o.GB, Price: o.Price, AllowUnlimited: DefaultRepPolicy.AllowUnlimited, CanDelete: DefaultRepPolicy.CanDelete}
	}
	return o, err
}

func (s *Store) RepOrderByID(ctx context.Context, id int64) (RepOrder, error) {
	return scanRepOrder(s.ro.QueryRowContext(ctx, `SELECT `+repOrderCols+` FROM rep_orders WHERE id=?`, id))
}

// RepNameFree reports whether a panel username can still be taken: no
// reseller and no login has it.
func (s *Store) RepNameFree(ctx context.Context, name string) bool {
	if !idRE.MatchString(name) {
		return false
	}
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM reps WHERE id=? COLLATE NOCASE)+(SELECT COUNT(*) FROM admins WHERE username=? COLLATE NOCASE)`, name, name).Scan(&n)
	return n == 0
}

// CreateRepOrder opens an order that waits for its receipt. repID "" means a
// new panel under the username name; otherwise a top-up of that reseller.
// An older order of the same chat that never got a receipt is dropped.
// seller "" is the owner's sale; otherwise the reseller whose bot sells the
// panel, as a sub-seller of its own, at its price.
func (s *Store) CreateRepOrder(ctx context.Context, seller string, chat int64, buyerName, repID, name, pkgID string) (RepOrder, error) {
	return s.createRepOrder(ctx, seller, chat, buyerName, repID, name, pkgID, false)
}

// createRepOrder: manual is a reseller selling from its panel (resale.go
// SellPanel): no bot, no card, no Telegram account; the order is paid.
func (s *Store) createRepOrder(ctx context.Context, seller string, chat int64, buyerName, repID, name, pkgID string, manual bool) (RepOrder, error) {
	if r := []rune(strings.TrimSpace(buyerName)); len(r) > 64 {
		buyerName = string(r[:64])
	}
	sale := s.RepSale(ctx)
	switch {
	case manual:
		if seller == "" || !s.ResaleOf(ctx, seller).Allowed || s.IsSubSeller(ctx, seller) || s.WholesaleTB(ctx, seller) <= 0 {
			return RepOrder{}, fmt.Errorf("%w: panel sale is not allowed for this reseller", ErrPolicy)
		}
	case seller == "" && !sale.Enabled, seller != "" && !s.ResaleOn(ctx, seller):
		return RepOrder{}, ErrNotFound
	}
	var pkg *RepPackage
	for i := range sale.Packages {
		if sale.Packages[i].ID == pkgID {
			pkg = &sale.Packages[i]
		}
	}
	var cost int64
	if pkg != nil && seller != "" {
		cost = s.Wholesale(ctx, seller, *pkg)
	}
	if pkg != nil && manual && seller != "" {
		pkg.Price = s.Retail(ctx, seller, *pkg) // the reseller's own sale: no banner discount
	} else if pkg != nil {
		pkg.Price, _ = s.PanelPrice(ctx, seller, *pkg) // with the seller's discount, if any
	}
	if pkg == nil {
		return RepOrder{}, fmt.Errorf("%w: unknown package", ErrInvalid)
	}
	kind := "topup"
	if repID == "" {
		kind, repID = "new", strings.ToLower(strings.TrimSpace(name))
		if !s.RepNameFree(ctx, repID) {
			return RepOrder{}, fmt.Errorf("%w: this username is taken or not valid", ErrInvalid)
		}
		var held int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_orders WHERE kind='new' AND rep_id=? AND status IN ('await_receipt','pending') AND chat_id<>?`, repID, chat).Scan(&held)
		if held != 0 {
			return RepOrder{}, fmt.Errorf("%w: this username is taken or not valid", ErrInvalid)
		}
	} else {
		if seller != "" && !s.IsSellerOf(ctx, seller, repID) {
			return RepOrder{}, ErrNotFound
		}
		if seller == "" && s.IsSubSeller(ctx, repID) {
			return RepOrder{}, fmt.Errorf("%w: sub-sellers are charged by their reseller", ErrPolicy)
		}
		var n int
		if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=? AND enabled=1`, repID).Scan(&n) != nil || n != 1 {
			return RepOrder{}, ErrNotFound
		}
	}
	now := s.now().Unix()
	status := "await_receipt"
	if manual {
		status = "pending"
	} else if _, err := s.db.ExecContext(ctx, `UPDATE rep_orders SET status='cancelled', decided_unix=? WHERE chat_id=? AND status='await_receipt'`, now, chat); err != nil {
		return RepOrder{}, err
	}
	terms, _ := json.Marshal(pkg)
	r, err := s.db.ExecContext(ctx, `INSERT INTO rep_orders(unix,chat_id,buyer_name,kind,rep_id,package,gb,price,terms,seller,cost,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		now, chat, buyerName, kind, repID, pkg.Name, pkg.GB, pkg.Price, string(terms), seller, cost, status)
	if err != nil {
		return RepOrder{}, err
	}
	id, _ := r.LastInsertId()
	return s.RepOrderByID(ctx, id)
}

// AwaitingRepReceipt is the chat's order in this seller's bot that still
// needs its receipt.
func (s *Store) AwaitingRepReceipt(ctx context.Context, seller string, chat int64) (int64, error) {
	var id int64
	err := s.ro.QueryRowContext(ctx, `SELECT id FROM rep_orders WHERE chat_id=? AND seller=? AND status='await_receipt' ORDER BY id DESC LIMIT 1`, chat, seller).Scan(&id)
	if err != nil {
		return 0, ErrNotFound
	}
	return id, nil
}

// AttachRepReceipt stores the receipt photo; the order then waits for the owner.
func (s *Store) AttachRepReceipt(ctx context.Context, id, chat int64, fileID string) (RepOrder, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE rep_orders SET receipt=?, status='pending' WHERE id=? AND chat_id=? AND status='await_receipt'`, fileID, id, chat)
	if err != nil {
		return RepOrder{}, err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return RepOrder{}, ErrNotFound
	}
	return s.RepOrderByID(ctx, id)
}

// RepOrderResult is what an approval produced. Password is set only for a
// new panel and is shown once.
type RepOrderResult struct {
	Order    RepOrder
	Password string
	Units    int64   // unlimited units that were added
	Bill     int64   // a resale paid by a bill: its id (0 = from the wallet)
	Wallet   int64   // a wallet top-up: the balance after it
	Settled  []int64 // a wallet top-up: bills it paid
}

// ApproveRepOrder delivers a paid order once: a new reseller with a login,
// this Telegram account and the credit, or the credit added to the reseller.
// seller is the bot approving it; an order of another seller is refused.
func (s *Store) ApproveRepOrder(ctx context.Context, actor, seller string, id int64) (RepOrderResult, error) {
	// Claim the order first, so two taps cannot deliver it twice.
	r, err := s.db.ExecContext(ctx, `UPDATE rep_orders SET status='approved', decided_unix=?, decided_by=? WHERE id=? AND status='pending' AND seller=?`, s.now().Unix(), actor, id, seller)
	if err != nil {
		return RepOrderResult{}, err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return RepOrderResult{}, fmt.Errorf("%w: this order was already decided", ErrInvalid)
	}
	o, err := s.RepOrderByID(ctx, id)
	if err != nil {
		return RepOrderResult{}, err
	}
	undo := func(e error) (RepOrderResult, error) {
		_, _ = s.db.ExecContext(ctx, `UPDATE rep_orders SET status='pending', decided_unix=0, decided_by='' WHERE id=?`, id)
		return RepOrderResult{}, e
	}
	res := RepOrderResult{Order: o}
	if o.Seller != "" && !s.ResaleOf(ctx, o.Seller).Allowed {
		return undo(fmt.Errorf("%w: panel sale is no longer allowed for this reseller", ErrPolicy))
	}
	if o.Kind == "wallet" {
		bal, paid, err := s.RepWalletAdjust(ctx, actor, o.RepID, o.Price, fmt.Sprintf("top-up #%d", o.ID))
		if err != nil {
			return undo(err)
		}
		res.Wallet, res.Settled = bal, paid
		return res, nil
	}
	note := fmt.Sprintf("telegram order #%d: %s", o.ID, o.Package)
	if o.Seller != "" {
		// The reseller pays the owner before anything is made.
		bill, err := s.chargeResale(ctx, o.Seller, o.ID, o.Cost)
		if err != nil {
			return undo(err)
		}
		res.Bill = bill
		undo = func(e error) (RepOrderResult, error) {
			s.refundResale(ctx, o.Seller, o.ID, o.Cost, bill)
			_, _ = s.db.ExecContext(ctx, `UPDATE rep_orders SET status='pending', decided_unix=0, decided_by='' WHERE id=?`, id)
			return RepOrderResult{}, e
		}
		note = fmt.Sprintf("panel order #%d at %s's bot: %s", o.ID, o.Seller, o.Package)
	}
	if o.Kind == "new" {
		if !s.RepNameFree(ctx, o.RepID) {
			return undo(fmt.Errorf("%w: the username %s is taken now", ErrInvalid, o.RepID))
		}
		name := o.BuyerName
		if name == "" {
			name = o.RepID
		}
		var err error
		if o.Seller != "" {
			err = s.PutSeller(ctx, o.Seller, Rep{ID: o.RepID, Name: name, Enabled: true}, "", "")
		} else {
			err = s.PutRep(ctx, Rep{ID: o.RepID, Name: name, Enabled: true})
		}
		if err != nil {
			return undo(err)
		}
		res.Password = randomHex(6)
		if err := s.SetRepLogin(ctx, o.RepID, res.Password, o.RepID); err != nil {
			_, _ = s.DeleteRep(ctx, o.RepID, false)
			return undo(err)
		}
		// This Telegram account is the reseller's from now on, unless it
		// already runs a panel: the bot stays on that one, and the new
		// panel is used from the web panel with its own login.
		if o.Chat != 0 && s.TgRep(ctx, o.Chat) == "" {
			if _, err := s.db.ExecContext(ctx, `UPDATE reps SET tg_chat=0 WHERE tg_chat=?`, o.Chat); err != nil {
				return undo(err)
			}
			if _, err := s.db.ExecContext(ctx, `DELETE FROM rep_tg_owners WHERE chat_id=?`, o.Chat); err != nil {
				return undo(err)
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE reps SET tg_chat=? WHERE id=?`, o.Chat, o.RepID); err != nil {
				return undo(err)
			}
		}
	}
	units := o.Terms.Units
	if o.Kind == "new" {
		// The package's settings are the new panel's settings (under a
		// reseller: never looser than the reseller's own).
		var err error
		if o.Seller != "" {
			err = s.SetSellerPolicy(ctx, o.Seller, o.RepID, o.Terms.Policy())
		} else {
			err = s.SetRepPolicy(ctx, o.RepID, o.Terms.Policy())
		}
		if err != nil {
			return res, fmt.Errorf("the panel %s was created but its settings were not applied: %w", o.RepID, err)
		}
		if pol, err := s.RepPolicyOf(ctx, o.RepID); err == nil && !pol.AllowUnlimited {
			units = 0
		}
	} else if pol, err := s.RepPolicyOf(ctx, o.RepID); err != nil || !pol.AllowUnlimited {
		units = 0 // a top-up never changes what a panel may sell
	}
	res.Units = units
	if err := s.AdjustCredit(ctx, actor, o.RepID, o.GB<<30, units, note); err != nil {
		if o.Kind == "new" {
			return res, fmt.Errorf("the panel %s was created but its credit was not added: %w", o.RepID, err)
		}
		return undo(err)
	}
	return res, nil
}

// RejectRepOrder closes an order without delivering anything.
func (s *Store) RejectRepOrder(ctx context.Context, actor string, id int64) (RepOrder, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE rep_orders SET status='rejected', decided_unix=?, decided_by=? WHERE id=? AND status IN ('pending','await_receipt')`, s.now().Unix(), actor, id)
	if err != nil {
		return RepOrder{}, err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return RepOrder{}, fmt.Errorf("%w: this order was already decided", ErrInvalid)
	}
	return s.RepOrderByID(ctx, id)
}

// RepCredit is a reseller's volume credit in bytes.
func (s *Store) RepCredit(ctx context.Context, repID string) int64 {
	var v int64
	_ = s.ro.QueryRowContext(ctx, `SELECT volume_credit FROM reps WHERE id=?`, repID).Scan(&v)
	return v
}

// ChatPanels lists the panels a Telegram account owns: the one its bot
// runs on first, then every other panel it bought, oldest first.
func (s *Store) ChatPanels(ctx context.Context, chat int64) []string {
	if chat == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	add(s.TgRep(ctx, chat))
	rows, err := s.ro.QueryContext(ctx, `SELECT o.rep_id FROM rep_orders o JOIN reps r ON r.id=o.rep_id
WHERE o.chat_id=? AND o.kind='new' AND o.status='approved' AND r.enabled=1 ORDER BY o.id`, chat)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			add(id)
		}
	}
	return out
}
