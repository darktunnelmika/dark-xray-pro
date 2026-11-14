package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Store: products and orders. A product is a ready-made plan (volume or
// unlimited, days, IP limit, inbounds, price) sold by the owner or by one
// representative. An order is a request for a product (a new client, or a
// renewal of an existing one); fulfilling it creates or extends the client
// in one transaction. Orders sold by a representative are paid from that
// representative's credit exactly like a hand-made client, so the shop can
// never mint service. Payment collection itself is manual (or a bot later):
// the panel records the amount, it does not move money.

const shopSchema = `
CREATE TABLE IF NOT EXISTS products (
  id TEXT PRIMARY KEY,
  seller TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  plan TEXT NOT NULL CHECK(plan IN ('volume','unlimited')),
  gb INTEGER NOT NULL DEFAULT 0,
  days INTEGER NOT NULL,
  max_ips INTEGER NOT NULL DEFAULT 0,
  inbounds TEXT NOT NULL DEFAULT '',
  price INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','archived')),
  first_use INTEGER NOT NULL DEFAULT 1,
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  product_id TEXT NOT NULL,
  seller TEXT NOT NULL DEFAULT '',
  buyer TEXT NOT NULL DEFAULT '',
  client_email TEXT NOT NULL DEFAULT '',
  renew INTEGER NOT NULL DEFAULT 0,
  amount INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','fulfilled','cancelled')),
  note TEXT NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL,
  done_unix INTEGER NOT NULL DEFAULT 0,
  done_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS orders_seller ON orders(seller, id);
`

// Product is a plan for sale.
type Product struct {
	ID       string   `json:"id"`
	Seller   string   `json:"seller"` // "" = owner
	Name     string   `json:"name"`
	Plan     string   `json:"plan"`
	GB       int64    `json:"gb"`
	Days     int64    `json:"days"`
	MaxIPs   int      `json:"max_ips"`
	Inbounds []string `json:"inbounds"`
	Price    int64    `json:"price"`
	Status   string   `json:"status"`
	FirstUse bool     `json:"first_use"`
}

// Order is a purchase request.
type Order struct {
	ID          int64  `json:"id"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Seller      string `json:"seller"`
	Buyer       string `json:"buyer"`
	ClientEmail string `json:"client_email"`
	Renew       bool   `json:"renew"`
	Amount      int64  `json:"amount"`
	Status      string `json:"status"`
	Note        string `json:"note"`
	CreatedUnix int64  `json:"created_unix"`
	DoneUnix    int64  `json:"done_unix"`
	DoneBy      string `json:"done_by"`
}

func (s *Store) ensureShopSchema() error {
	if _, err := s.db.Exec(shopSchema); err != nil {
		return err
	}
	return s.addColumn("reps", "manage_products", "INTEGER NOT NULL DEFAULT 0")
}

// canSell: the owner sells anything; a representative only its own items.
func canSell(p Principal, seller string) bool {
	return p.Role == "owner" || (p.Role == "rep" && p.RepID != "" && p.RepID == seller)
}

// PutProduct creates or edits a product. Owners may create products for any
// representative; a representative needs the manage_products grant and its
// product must fit its policy.
func (s *Store) PutProduct(ctx context.Context, by Principal, p Product) error {
	if !idRE.MatchString(p.ID) || p.Name == "" || len(p.Name) > 120 || p.Days <= 0 || p.Days > 3650 || p.MaxIPs < 0 || p.Price < 0 {
		return ErrInvalid
	}
	if p.Plan != "volume" && p.Plan != "unlimited" {
		return ErrInvalid
	}
	if p.Plan == "volume" && p.GB <= 0 {
		return fmt.Errorf("%w: a volume product needs GB", ErrInvalid)
	}
	if p.Plan == "unlimited" {
		p.GB = 0
	}
	switch p.Status {
	case "":
		p.Status = "draft"
	case "draft", "published", "archived":
	default:
		return ErrInvalid
	}
	if by.Role == "rep" {
		p.Seller = by.RepID
	}
	if p.Seller != "" {
		pol, err := s.RepPolicyOf(ctx, p.Seller)
		if err != nil {
			return fmt.Errorf("%w: unknown seller", ErrInvalid)
		}
		if by.Role == "rep" {
			var manage bool
			if err := s.ro.QueryRowContext(ctx, `SELECT manage_products FROM reps WHERE id=?`, by.RepID).Scan(&manage); err != nil || !manage {
				return fmt.Errorf("%w: managing products is not allowed", ErrPolicy)
			}
		}
		if p.Plan == "unlimited" && !pol.AllowUnlimited {
			return fmt.Errorf("%w: unlimited plans are not allowed for this seller", ErrPolicy)
		}
		if pol.MaxDays > 0 && p.Days > pol.MaxDays {
			return fmt.Errorf("%w: the longest term allowed is %d days", ErrPolicy, pol.MaxDays)
		}
		if pol.MaxClientGB > 0 && p.GB > pol.MaxClientGB {
			return fmt.Errorf("%w: the largest volume allowed is %d GB", ErrPolicy, pol.MaxClientGB)
		}
		if len(pol.Inbounds) > 0 {
			ok := map[string]bool{}
			for _, t := range pol.Inbounds {
				ok[t] = true
			}
			for _, t := range p.Inbounds {
				if !ok[t] {
					return fmt.Errorf("%w: inbound %q is not allowed", ErrPolicy, t)
				}
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur, curSeller string
	err = tx.QueryRowContext(ctx, `SELECT status,seller FROM products WHERE id=?`, p.ID).Scan(&cur, &curSeller)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case !canSell(by, curSeller):
		return fmt.Errorf("%w: that ID is taken", ErrInvalid)
	case cur == "archived":
		return fmt.Errorf("%w: archived products cannot be changed", ErrInvalid)
	}
	for _, t := range p.Inbounds {
		var x int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM inbounds WHERE tag=?`, t).Scan(&x); err != nil {
			return fmt.Errorf("%w: unknown inbound %q", ErrInvalid, t)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO products(id,seller,name,plan,gb,days,max_ips,inbounds,price,status,first_use,created_unix)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET seller=excluded.seller, name=excluded.name, plan=excluded.plan,
gb=excluded.gb, days=excluded.days, max_ips=excluded.max_ips, inbounds=excluded.inbounds, price=excluded.price,
status=excluded.status, first_use=excluded.first_use`,
		p.ID, p.Seller, p.Name, p.Plan, p.GB, p.Days, p.MaxIPs, strings.Join(p.Inbounds, ","), p.Price, p.Status, p.FirstUse, s.now().Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}

const productCols = `id,seller,name,plan,gb,days,max_ips,inbounds,price,status,first_use`

func scanProduct(row scanner) (Product, error) {
	var p Product
	var tags string
	err := row.Scan(&p.ID, &p.Seller, &p.Name, &p.Plan, &p.GB, &p.Days, &p.MaxIPs, &tags, &p.Price, &p.Status, &p.FirstUse)
	p.Inbounds = splitTags(tags)
	return p, err
}

// Products lists what by may see: owners everything, a representative its
// own products. publishedOnly is for storefronts (bots).
func (s *Store) Products(ctx context.Context, by Principal, publishedOnly bool) ([]Product, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+productCols+` FROM products
WHERE (?='owner' OR seller=?) AND (?=0 OR status='published') ORDER BY seller,price,id`, by.Role, by.RepID, publishedOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		if by.Role != "owner" && p.Seller == "" {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateOrder records a request for a published product. clientEmail empty
// means a new client; otherwise it renews that client, which must belong to
// the product's seller.
func (s *Store) CreateOrder(ctx context.Context, by Principal, productID, buyer, clientEmail, note string) (int64, error) {
	p, err := scanProduct(s.ro.QueryRowContext(ctx, `SELECT `+productCols+` FROM products WHERE id=?`, productID))
	if err != nil {
		return 0, ErrNotFound
	}
	if !canSell(by, p.Seller) {
		return 0, ErrNotFound
	}
	if p.Status != "published" {
		return 0, fmt.Errorf("%w: product is not published", ErrInvalid)
	}
	if len(buyer) > 200 || len(note) > 1000 {
		return 0, ErrInvalid
	}
	renew := clientEmail != ""
	if renew {
		var owner string
		if err := s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, clientEmail).Scan(&owner); err != nil || owner != p.Seller {
			return 0, fmt.Errorf("%w: unknown client for this seller", ErrInvalid)
		}
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO orders(product_id,seller,buyer,client_email,renew,amount,note,created_unix) VALUES(?,?,?,?,?,?,?,?)`,
		p.ID, p.Seller, buyer, clientEmail, renew, p.Price, note, s.now().Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// Orders lists orders newest first within by's scope.
func (s *Store) Orders(ctx context.Context, by Principal, status string, limit int) ([]Order, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT o.id,o.product_id,COALESCE(p.name,''),o.seller,o.buyer,o.client_email,o.renew,o.amount,o.status,o.note,
o.created_unix,o.done_unix,o.done_by FROM orders o LEFT JOIN products p ON p.id=o.product_id
WHERE (?='owner' OR (o.seller=? AND o.seller<>'')) AND (?='' OR o.status=?) ORDER BY o.id DESC LIMIT ?`, by.Role, by.RepID, status, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ProductID, &o.ProductName, &o.Seller, &o.Buyer, &o.ClientEmail, &o.Renew, &o.Amount, &o.Status, &o.Note,
			&o.CreatedUnix, &o.DoneUnix, &o.DoneBy); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// FulfillOrder turns a pending order into a client (or a renewal) and marks
// it fulfilled. For a representative's order the client is charged to that
// representative's credit; insufficient credit leaves the order pending.
func (s *Store) FulfillOrder(ctx context.Context, by Principal, id int64) (string, error) {
	var o Order
	err := s.ro.QueryRowContext(ctx, `SELECT product_id,seller,client_email,renew,status FROM orders WHERE id=?`, id).
		Scan(&o.ProductID, &o.Seller, &o.ClientEmail, &o.Renew, &o.Status)
	if err != nil || !canSell(by, o.Seller) {
		return "", ErrNotFound
	}
	if o.Status != "pending" {
		return "", fmt.Errorf("%w: order is %s", ErrInvalid, o.Status)
	}
	p, err := scanProduct(s.ro.QueryRowContext(ctx, `SELECT `+productCols+` FROM products WHERE id=?`, o.ProductID))
	if err != nil {
		return "", fmt.Errorf("%w: product is gone", ErrInvalid)
	}
	now := s.now().Unix()
	var c Client
	email := o.ClientEmail
	if o.Renew {
		var old Client
		err := s.ro.QueryRowContext(ctx, `SELECT email,enabled,volume_bytes,expires_unix,duration_seconds,max_ips FROM clients WHERE email=?`, email).
			Scan(&old.Email, &old.Enabled, &old.VolumeBytes, &old.ExpiresUnix, &old.DurationSeconds, &old.MaxIPs)
		if err != nil {
			return "", fmt.Errorf("%w: the client to renew is gone", ErrInvalid)
		}
		old.Inbounds, _ = s.clientInbounds(ctx, email)
		c = old
		add := p.Days * 86400
		switch {
		case c.ExpiresUnix > now:
			c.ExpiresUnix += add
		case c.ExpiresUnix > 0:
			c.ExpiresUnix = now + add
		default:
			c.DurationSeconds += add
		}
		if p.Plan == "unlimited" {
			c.VolumeBytes = -1
		} else if c.VolumeBytes >= 0 {
			c.VolumeBytes += p.GB << 30
		} else {
			c.VolumeBytes = p.GB << 30
		}
		c.Enabled = true
	} else {
		if email == "" {
			email = fmt.Sprintf("%s-%d", p.ID, id)
		}
		c = Client{Email: email, Enabled: true, MaxIPs: p.MaxIPs, Inbounds: p.Inbounds, VolumeBytes: -1}
		if p.Plan == "volume" {
			c.VolumeBytes = p.GB << 30
		}
		if p.FirstUse {
			c.DurationSeconds = p.Days * 86400
		} else {
			c.ExpiresUnix = now + p.Days*86400
		}
	}
	actor := by.User
	if o.Seller != "" {
		err = s.RepPutClient(ctx, actor, o.Seller, c, p.Plan)
	} else {
		err = s.PutClient(ctx, c)
	}
	if err != nil {
		return "", err
	}
	r, err := s.db.ExecContext(ctx, `UPDATE orders SET status='fulfilled', client_email=?, done_unix=?, done_by=? WHERE id=? AND status='pending'`, email, now, actor, id)
	if err != nil {
		return "", err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return "", fmt.Errorf("%w: order changed meanwhile", ErrInvalid)
	}
	return email, nil
}

// CancelOrder closes a pending order without touching any client or credit.
func (s *Store) CancelOrder(ctx context.Context, by Principal, id int64) error {
	var seller string
	if err := s.ro.QueryRowContext(ctx, `SELECT seller FROM orders WHERE id=?`, id).Scan(&seller); err != nil || !canSell(by, seller) {
		return ErrNotFound
	}
	r, err := s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', done_unix=?, done_by=? WHERE id=? AND status='pending'`, s.now().Unix(), by.User, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return fmt.Errorf("%w: only pending orders can be cancelled", ErrInvalid)
	}
	return nil
}

func (s *Store) clientInbounds(ctx context.Context, email string) ([]string, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT tag FROM client_inbounds WHERE email=? ORDER BY tag`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetManageProducts grants or revokes a representative's product management.
func (s *Store) SetManageProducts(ctx context.Context, repID string, on bool) error {
	r, err := s.db.ExecContext(ctx, `UPDATE reps SET manage_products=? WHERE id=?`, on, repID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
