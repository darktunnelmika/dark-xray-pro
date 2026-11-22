package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Discount codes and card-to-card receipts.
//
// A customer enters a code; once it checks out it is held for that buyer
// (discount_pending) and the next order the buyer opens — bot, Mini App,
// Stars or wallet — takes it. Percent codes lower both the toman price and
// the Stars price; amount codes lower the toman price only. A code is used
// up when the order is opened and given back if the order is cancelled.

const discountSchema = `
CREATE TABLE IF NOT EXISTS discounts (
  code TEXT PRIMARY KEY,
  percent INTEGER NOT NULL DEFAULT 0,
  amount INTEGER NOT NULL DEFAULT 0,
  max_uses INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  expires_unix INTEGER NOT NULL DEFAULT 0,
  product_id TEXT NOT NULL DEFAULT '',
  once_per_buyer INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  note TEXT NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS discount_pending (
  buyer TEXT PRIMARY KEY,
  code TEXT NOT NULL,
  unix INTEGER NOT NULL
);
`

// Discount is one code.
type Discount struct {
	Code         string `json:"code"`
	Percent      int64  `json:"percent"` // 1..100
	Amount       int64  `json:"amount"`  // toman off (when Percent is 0)
	MaxUses      int64  `json:"max_uses"`
	Used         int64  `json:"used"`
	ExpiresUnix  int64  `json:"expires_unix"`
	ProductID    string `json:"product_id"` // "" = every product
	OncePerBuyer bool   `json:"once_per_buyer"`
	Enabled      bool   `json:"enabled"`
	Note         string `json:"note"`
	CreatedUnix  int64  `json:"created_unix"`
}

var codeRE = regexp.MustCompile(`^[A-Z0-9_-]{3,32}$`)

func normCode(c string) string { return strings.ToUpper(strings.TrimSpace(c)) }

func (s *Store) ensureDiscountSchema() error {
	if _, err := s.db.Exec(discountSchema); err != nil {
		return err
	}
	for _, c := range [][2]string{{"discount_code", "TEXT NOT NULL DEFAULT ''"}, {"discount_percent", "INTEGER NOT NULL DEFAULT 0"},
		{"discount", "INTEGER NOT NULL DEFAULT 0"}, {"receipt", "TEXT NOT NULL DEFAULT ''"}, {"receipt_unix", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := s.addColumn("orders", c[0], c[1]); err != nil {
			return err
		}
	}
	return nil
}

const discountCols = `code,percent,amount,max_uses,used,expires_unix,product_id,once_per_buyer,enabled,note,created_unix`

func scanDiscount(r scanner) (Discount, error) {
	var d Discount
	err := r.Scan(&d.Code, &d.Percent, &d.Amount, &d.MaxUses, &d.Used, &d.ExpiresUnix, &d.ProductID, &d.OncePerBuyer, &d.Enabled, &d.Note, &d.CreatedUnix)
	return d, err
}

// Discounts lists every code, newest first.
func (s *Store) Discounts(ctx context.Context) ([]Discount, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+discountCols+` FROM discounts ORDER BY created_unix DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Discount{}
	for rows.Next() {
		d, err := scanDiscount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutDiscount creates or edits a code (the use count is kept).
func (s *Store) PutDiscount(ctx context.Context, d Discount) error {
	d.Code = normCode(d.Code)
	if !codeRE.MatchString(d.Code) {
		return fmt.Errorf("%w: code: 3 to 32 letters, digits, - or _", ErrInvalid)
	}
	if (d.Percent <= 0) == (d.Amount <= 0) || d.Percent > 100 || d.Amount < 0 || d.MaxUses < 0 || len(d.Note) > 200 {
		return fmt.Errorf("%w: give either a percent (1-100) or an amount", ErrInvalid)
	}
	if d.ProductID != "" {
		var x int
		if err := s.ro.QueryRowContext(ctx, `SELECT 1 FROM products WHERE id=?`, d.ProductID).Scan(&x); err != nil {
			return fmt.Errorf("%w: unknown product", ErrInvalid)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO discounts(code,percent,amount,max_uses,expires_unix,product_id,once_per_buyer,enabled,note,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(code) DO UPDATE SET percent=excluded.percent, amount=excluded.amount, max_uses=excluded.max_uses, expires_unix=excluded.expires_unix,
product_id=excluded.product_id, once_per_buyer=excluded.once_per_buyer, enabled=excluded.enabled, note=excluded.note`,
		d.Code, d.Percent, d.Amount, d.MaxUses, d.ExpiresUnix, d.ProductID, d.OncePerBuyer, d.Enabled, d.Note, s.now().Unix())
	return err
}

func (s *Store) DeleteDiscount(ctx context.Context, code string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM discounts WHERE code=?`, normCode(code))
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Reasons a code is refused (the bot shows them in its own words).
var (
	ErrCodeUnknown  = errors.New("unknown code")
	ErrCodeExpired  = errors.New("code expired")
	ErrCodeUsedUp   = errors.New("code used up")
	ErrCodeUsed     = errors.New("code already used by this buyer")
	ErrCodeProduct  = errors.New("code is for another product")
	ErrCodeDisabled = errors.New("code is off")
)

type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// checkDiscount tells whether buyer may use code on productID now.
func (s *Store) checkDiscount(ctx context.Context, q queryer, code, productID, buyer string) (Discount, error) {
	d, err := scanDiscount(q.QueryRowContext(ctx, `SELECT `+discountCols+` FROM discounts WHERE code=?`, normCode(code)))
	switch {
	case err != nil:
		return d, ErrCodeUnknown
	case !d.Enabled:
		return d, ErrCodeDisabled
	case d.ExpiresUnix > 0 && d.ExpiresUnix <= s.now().Unix():
		return d, ErrCodeExpired
	case d.MaxUses > 0 && d.Used >= d.MaxUses:
		return d, ErrCodeUsedUp
	case d.ProductID != "" && productID != "" && d.ProductID != productID:
		return d, ErrCodeProduct
	}
	if d.OncePerBuyer && buyer != "" {
		var n int
		_ = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE buyer=? AND discount_code=? AND status<>'cancelled'`, buyer, d.Code).Scan(&n)
		if n > 0 {
			return d, ErrCodeUsed
		}
	}
	return d, nil
}

// HoldDiscount checks a code for a buyer and keeps it for their next order.
func (s *Store) HoldDiscount(ctx context.Context, buyer, code, productID string) (Discount, error) {
	d, err := s.checkDiscount(ctx, s.ro, code, productID, buyer)
	if err != nil {
		return d, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO discount_pending(buyer,code,unix) VALUES(?,?,?) ON CONFLICT(buyer) DO UPDATE SET code=excluded.code, unix=excluded.unix`, buyer, d.Code, s.now().Unix())
	return d, err
}

// HeldDiscount is the code a buyer holds for productID, if it still works.
func (s *Store) HeldDiscount(ctx context.Context, buyer, productID string) (Discount, bool) {
	var code string
	if s.ro.QueryRowContext(ctx, `SELECT code FROM discount_pending WHERE buyer=? AND unix>?`, buyer, s.now().Unix()-3600).Scan(&code) == nil {
		if d, err := s.checkDiscount(ctx, s.ro, code, productID, buyer); err == nil {
			return d, true // a code the customer typed wins over the invite gift
		}
	}
	return s.inviteDiscount(ctx, buyer, productID)
}

// Discounted is the price after a code: toman and Stars.
func (d Discount) Discounted(price, stars int64) (int64, int64) {
	if d.Percent > 0 {
		return price * (100 - d.Percent) / 100, stars * (100 - d.Percent) / 100
	}
	p := price - d.Amount
	if p < 0 {
		p = 0
	}
	return p, stars
}

// applyHeldDiscount moves a held code onto a freshly opened order.
func (s *Store) applyHeldDiscount(ctx context.Context, orderID int64, buyer, productID string, price int64) {
	d, ok := s.HeldDiscount(ctx, buyer, productID)
	if !ok {
		return
	}
	after, _ := d.Discounted(price, 0)
	if d.Code == InviteCode {
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET discount_code=?, discount_percent=?, discount=?, amount=? WHERE id=?`, d.Code, d.Percent, price-after, after, orderID)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	if _, err := s.checkDiscount(ctx, tx, d.Code, productID, buyer); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET discount_code=?, discount_percent=?, discount=?, amount=? WHERE id=?`, d.Code, d.Percent, price-after, after, orderID); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE discounts SET used=used+1 WHERE code=?`, d.Code); err != nil {
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM discount_pending WHERE buyer=?`, buyer); err != nil {
		return
	}
	_ = tx.Commit()
}

// releaseDiscount gives a cancelled order's code back.
func (s *Store) releaseDiscount(ctx context.Context, orderID int64) {
	var code string
	if s.ro.QueryRowContext(ctx, `SELECT discount_code FROM orders WHERE id=?`, orderID).Scan(&code) == nil && code != "" {
		_, _ = s.db.ExecContext(ctx, `UPDATE discounts SET used=MAX(0,used-1) WHERE code=?`, code)
	}
}

// orderStarsPercent is the percent a code took off an order's Stars price.
func (s *Store) orderStarsPercent(ctx context.Context, orderID int64) int64 {
	var p int64
	_ = s.ro.QueryRowContext(ctx, `SELECT discount_percent FROM orders WHERE id=?`, orderID).Scan(&p)
	return p
}

// ---- card-to-card receipts ----

// AwaitingReceipt is the buyer's newest open card order without a receipt.
func (s *Store) AwaitingReceipt(ctx context.Context, buyer string) (int64, error) {
	var id int64
	err := s.ro.QueryRowContext(ctx, `SELECT id FROM orders WHERE buyer=? AND status='pending' AND method='manual' AND receipt='' ORDER BY id DESC LIMIT 1`, buyer).Scan(&id)
	if err != nil {
		return 0, ErrNotFound
	}
	return id, nil
}

// AttachReceipt stores a payment receipt (a Telegram photo id) on an order
// the buyer opened; the seller then approves or refuses it.
func (s *Store) AttachReceipt(ctx context.Context, orderID int64, buyer, fileID string) (Order, error) {
	if fileID == "" || len(fileID) > 300 {
		return Order{}, ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE orders SET receipt=?, receipt_unix=? WHERE id=? AND buyer=? AND status='pending' AND method='manual'`, fileID, s.now().Unix(), orderID, buyer)
	if err != nil {
		return Order{}, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return Order{}, ErrNotFound
	}
	return s.OrderByID(ctx, orderID)
}

// OrderByID reads one order (no scope check: callers check).
func (s *Store) OrderByID(ctx context.Context, id int64) (Order, error) {
	var o Order
	err := s.ro.QueryRowContext(ctx, `SELECT o.id,o.product_id,COALESCE(p.name,''),o.seller,o.buyer,o.client_email,o.renew,o.amount,o.status,o.note,
o.created_unix,o.done_unix,o.done_by,o.method,o.stars,o.discount_code,o.discount,o.receipt,o.receipt_unix FROM orders o LEFT JOIN products p ON p.id=o.product_id WHERE o.id=?`, id).
		Scan(&o.ID, &o.ProductID, &o.ProductName, &o.Seller, &o.Buyer, &o.ClientEmail, &o.Renew, &o.Amount, &o.Status, &o.Note,
			&o.CreatedUnix, &o.DoneUnix, &o.DoneBy, &o.Method, &o.Stars, &o.DiscountCode, &o.Discount, &o.Receipt, &o.ReceiptUnix)
	if err != nil {
		return o, ErrNotFound
	}
	o.HasReceipt = o.Receipt != ""
	return o, nil
}

// CancelOwnOrder lets a buyer drop an order nobody has handled yet.
func (s *Store) CancelOwnOrder(ctx context.Context, buyer string, id int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note=note||' · cancelled by buyer', done_unix=? WHERE id=? AND buyer=? AND status='pending'`, s.now().Unix(), id, buyer)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.releaseDiscount(ctx, id)
	_, _ = s.db.ExecContext(ctx, `UPDATE orders SET tg_buyer_notified=1 WHERE id=?`, id)
	return nil
}

// CardInfo is the card customers pay to (settings).
type CardInfo struct {
	Number string `json:"number"`
	Holder string `json:"holder"`
	Bank   string `json:"bank"`
}

func (s *Store) Card(ctx context.Context) CardInfo {
	return CardInfo{Number: s.Setting(ctx, "pay_card_number"), Holder: s.Setting(ctx, "pay_card_holder"), Bank: s.Setting(ctx, "pay_card_bank")}
}

func (s *Store) PutCard(ctx context.Context, c CardInfo) error {
	num := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r >= '۰' && r <= '۹' {
			return '0' + (r - '۰')
		}
		return -1
	}, c.Number)
	if num != "" && (len(num) < 16 || len(num) > 26) {
		return fmt.Errorf("%w: card number: 16 digits (or a 24-digit IBAN)", ErrInvalid)
	}
	if len(c.Holder) > 80 || len(c.Bank) > 60 {
		return ErrInvalid
	}
	for k, v := range map[string]string{"pay_card_number": num, "pay_card_holder": strings.TrimSpace(c.Holder), "pay_card_bank": strings.TrimSpace(c.Bank)} {
		if err := s.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}
