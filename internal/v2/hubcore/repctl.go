package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Representative policy, activity and finance.
//
// A policy caps what a representative may sell: how many clients, how long a
// term, how much volume per client, which inbounds, whether unlimited plans
// and deletes are allowed, and whether deleting a client returns its unused
// volume. Refunds happen synchronously inside the delete transaction (one
// ledger row, no background worker), so they can never stall anything else.

// RepPolicy caps a representative. Zero means "no cap" for the numeric fields.
type RepPolicy struct {
	MaxClients     int64
	MaxDays        int64
	MaxClientGB    int64
	AllowUnlimited bool
	CanDelete      bool
	RefundUnused   bool
	Inbounds       []string // empty: every inbound
	PriceGB        int64    // sale price per GB, for the finance page
	PriceUnit      int64    // sale price per unlimited unit
}

// DefaultRepPolicy is what a new representative gets.
var DefaultRepPolicy = RepPolicy{AllowUnlimited: true, CanDelete: true}

func (s *Store) ensureRepCtlSchema() error {
	for _, c := range []struct{ table, col, def string }{
		{"reps", "max_clients", "INTEGER NOT NULL DEFAULT 0"},
		{"reps", "max_days", "INTEGER NOT NULL DEFAULT 0"},
		{"reps", "max_client_gb", "INTEGER NOT NULL DEFAULT 0"},
		{"reps", "allow_unlimited", "INTEGER NOT NULL DEFAULT 1"},
		{"reps", "can_delete", "INTEGER NOT NULL DEFAULT 1"},
		{"reps", "refund_unused", "INTEGER NOT NULL DEFAULT 0"},
		{"reps", "inbounds", "TEXT NOT NULL DEFAULT ''"},
		{"reps", "price_gb", "INTEGER NOT NULL DEFAULT 0"},
		{"reps", "price_unit", "INTEGER NOT NULL DEFAULT 0"},
		{"credit_ledger", "amount", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumn(c.table, c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

const policyCols = `max_clients,max_days,max_client_gb,allow_unlimited,can_delete,refund_unused,inbounds,price_gb,price_unit`

type scanner interface{ Scan(...any) error }

func scanPolicy(row scanner, extra ...any) (RepPolicy, error) {
	var p RepPolicy
	var tags string
	err := row.Scan(append(extra, &p.MaxClients, &p.MaxDays, &p.MaxClientGB, &p.AllowUnlimited, &p.CanDelete, &p.RefundUnused, &tags, &p.PriceGB, &p.PriceUnit)...)
	p.Inbounds = splitTags(tags)
	return p, err
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// SetRepPolicy replaces a representative's policy. Owner only.
func (s *Store) SetRepPolicy(ctx context.Context, repID string, p RepPolicy) error {
	if p.MaxClients < 0 || p.MaxDays < 0 || p.MaxClientGB < 0 || p.PriceGB < 0 || p.PriceUnit < 0 {
		return ErrInvalid
	}
	for _, t := range p.Inbounds {
		if !idRE.MatchString(t) {
			return ErrInvalid
		}
	}
	r, err := s.db.ExecContext(ctx, `UPDATE reps SET max_clients=?,max_days=?,max_client_gb=?,allow_unlimited=?,can_delete=?,
refund_unused=?,inbounds=?,price_gb=?,price_unit=? WHERE id=?`,
		p.MaxClients, p.MaxDays, p.MaxClientGB, p.AllowUnlimited, p.CanDelete, p.RefundUnused, strings.Join(p.Inbounds, ","), p.PriceGB, p.PriceUnit, repID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// RepPolicyOf reads one representative's policy.
func (s *Store) RepPolicyOf(ctx context.Context, repID string) (RepPolicy, error) {
	p, err := scanPolicy(s.ro.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=?`, repID))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// checkPolicy runs inside RepPutClient's transaction.
func checkPolicy(ctx context.Context, tx *sql.Tx, repID string, c Client, plan string, isNew bool, now int64) error {
	p, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=?`, repID))
	if err != nil {
		return err
	}
	if plan == "unlimited" && !p.AllowUnlimited {
		return fmt.Errorf("%w: unlimited plans are not allowed for this representative", ErrPolicy)
	}
	if p.MaxDays > 0 && termDays(c, now) > p.MaxDays {
		return fmt.Errorf("%w: the longest term allowed is %d days", ErrPolicy, p.MaxDays)
	}
	if p.MaxClientGB > 0 && plan == "volume" && c.VolumeBytes > p.MaxClientGB<<30 {
		return fmt.Errorf("%w: the largest volume allowed is %d GB", ErrPolicy, p.MaxClientGB)
	}
	if isNew && p.MaxClients > 0 {
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE rep_id=?`, repID).Scan(&n); err != nil {
			return err
		}
		if n >= p.MaxClients {
			return fmt.Errorf("%w: the client limit (%d) is reached", ErrPolicy, p.MaxClients)
		}
	}
	if len(p.Inbounds) > 0 {
		allowed := map[string]bool{}
		for _, t := range p.Inbounds {
			allowed[t] = true
		}
		for _, t := range c.Inbounds {
			if !allowed[t] {
				return fmt.Errorf("%w: inbound %q is not allowed", ErrPolicy, t)
			}
		}
	}
	return nil
}

// ErrPolicy means a representative's policy forbids a change.
var ErrPolicy = errors.New("not allowed by policy")

// RepDeleteClient deletes a representative's client and, when the policy
// says so, returns the unused part in the same transaction: unused volume for
// a volume plan, whole unused 30-day units for an unlimited plan.
func (s *Store) RepDeleteClient(ctx context.Context, actor, repID, email string) (refundBytes, refundUnits int64, err error) {
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	p, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=? AND enabled=1`, repID))
	if err != nil {
		return 0, 0, ErrAuth
	}
	if !p.CanDelete {
		return 0, 0, fmt.Errorf("%w: deleting clients is not allowed", ErrPolicy)
	}
	var owner, plan string
	var c Client
	var used, first int64
	err = tx.QueryRowContext(ctx, `SELECT c.rep_id,c.plan,c.volume_bytes,c.expires_unix,c.duration_seconds,
COALESCE(u.up+u.down,0),COALESCE(u.first_seen_unix,0) FROM clients c LEFT JOIN client_usage u ON u.email=c.email WHERE c.email=?`, email).
		Scan(&owner, &plan, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &used, &first)
	if err != nil || owner != repID {
		return 0, 0, ErrNotFound
	}
	if p.RefundUnused {
		expired := c.ExpiresUnix > 0 && c.ExpiresUnix <= now
		switch {
		case expired:
		case plan == "volume" && c.VolumeBytes > 0:
			refundBytes = max(0, c.VolumeBytes-used)
		case plan == "unlimited":
			var left int64
			if c.ExpiresUnix > now {
				left = (c.ExpiresUnix - now) / 86400
			} else if c.DurationSeconds > 0 && first == 0 {
				left = c.DurationSeconds / 86400 // never connected: the whole term
			}
			refundUnits = left / unitDays
		}
	}
	if refundBytes > 0 || refundUnits > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+?, unlimited_units=unlimited_units+? WHERE id=?`, refundBytes, refundUnits, repID); err != nil {
			return 0, 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,units_delta) VALUES(?,?,?,?,?,?,?)`,
			now, repID, actor, "refund", email, refundBytes, refundUnits); err != nil {
			return 0, 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email) VALUES(?,?,?,?,?)`, now, repID, actor, "delete", email); err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE email=?`, email); err != nil {
		return 0, 0, err
	}
	return refundBytes, refundUnits, tx.Commit()
}

// LedgerEntry is one row of a representative's activity.
type LedgerEntry struct {
	ID          int64
	Unix        int64
	RepID       string
	Actor       string
	Kind        string // adjust | charge | refund | delete | payment
	ClientEmail string
	VolumeDelta int64
	UnitsDelta  int64
	Amount      int64
	Note        string
}

// Ledger lists activity newest first; repID "" means every representative.
// before pages backwards by id (0 = from the newest).
func (s *Store) Ledger(ctx context.Context, repID string, before int64, limit int) ([]LedgerEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,rep_id,actor,kind,client_email,volume_delta,units_delta,amount,note
FROM credit_ledger WHERE (?='' OR rep_id=?) AND id<? ORDER BY id DESC LIMIT ?`, repID, repID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.Unix, &e.RepID, &e.Actor, &e.Kind, &e.ClientEmail, &e.VolumeDelta, &e.UnitsDelta, &e.Amount, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RecordPayment logs money a representative paid. It changes no credit.
func (s *Store) RecordPayment(ctx context.Context, actor, repID string, amount int64, note string) error {
	if amount == 0 {
		return ErrInvalid
	}
	var x int
	if err := s.ro.QueryRowContext(ctx, `SELECT 1 FROM reps WHERE id=?`, repID).Scan(&x); err != nil {
		return ErrNotFound
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,amount,note) VALUES(?,?,?,?,?,?)`,
		s.now().Unix(), repID, actor, "payment", amount, note)
	return err
}

// FinanceRow sums one representative's ledger over a period.
type FinanceRow struct {
	RepID        string
	Name         string
	Clients      int64
	SoldBytes    int64
	SoldUnits    int64
	RefundBytes  int64
	RefundUnits  int64
	GrantedBytes int64
	GrantedUnits int64
	Paid         int64
	Revenue      int64 // net sold volume and units at the representative's prices
	Debt         int64 // revenue minus paid
	VolumeCredit int64
	Units        int64
}

// Finance summarizes every representative since sinceUnix (0 = all time).
func (s *Store) Finance(ctx context.Context, sinceUnix int64) ([]FinanceRow, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT r.id,r.name,r.price_gb,r.price_unit,r.volume_credit,r.unlimited_units,
(SELECT COUNT(*) FROM clients c WHERE c.rep_id=r.id),
COALESCE(SUM(CASE WHEN l.kind='charge' THEN -l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.kind='charge' THEN -l.units_delta END),0),
COALESCE(SUM(CASE WHEN l.kind='refund' THEN l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.kind='refund' THEN l.units_delta END),0),
COALESCE(SUM(CASE WHEN l.kind='adjust' THEN l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.kind='adjust' THEN l.units_delta END),0),
COALESCE(SUM(CASE WHEN l.kind='payment' THEN l.amount END),0)
FROM reps r LEFT JOIN credit_ledger l ON l.rep_id=r.id AND l.unix>=?
GROUP BY r.id ORDER BY r.id`, sinceUnix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FinanceRow{}
	for rows.Next() {
		var f FinanceRow
		var priceGB, priceUnit int64
		if err := rows.Scan(&f.RepID, &f.Name, &priceGB, &priceUnit, &f.VolumeCredit, &f.Units, &f.Clients,
			&f.SoldBytes, &f.SoldUnits, &f.RefundBytes, &f.RefundUnits, &f.GrantedBytes, &f.GrantedUnits, &f.Paid); err != nil {
			return nil, err
		}
		netGB := float64(f.SoldBytes-f.RefundBytes) / (1 << 30)
		f.Revenue = int64(netGB*float64(priceGB)+0.5) + (f.SoldUnits-f.RefundUnits)*priceUnit
		f.Debt = f.Revenue - f.Paid
		out = append(out, f)
	}
	return out, rows.Err()
}

// ClientDaily is one day of a client's traffic across nodes.
type ClientDaily struct {
	Day      string
	Up, Down int64
}

// ClientDailyUsage returns the last days of traffic, oldest first, with
// missing days filled in as zero.
func (s *Store) ClientDailyUsage(ctx context.Context, email string, days int) ([]ClientDaily, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	today := s.now().UTC()
	from := today.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT day,SUM(up),SUM(down) FROM usage_daily WHERE email=? AND day>=? GROUP BY day`, email, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]ClientDaily{}
	for rows.Next() {
		var d ClientDaily
		if err := rows.Scan(&d.Day, &d.Up, &d.Down); err != nil {
			return nil, err
		}
		got[d.Day] = d
	}
	out := make([]ClientDaily, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		d := got[day]
		d.Day = day
		out = append(out, d)
	}
	return out, rows.Err()
}
