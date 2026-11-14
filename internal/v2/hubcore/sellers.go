package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Sub-sellers: a representative can open sellers under itself and move part
// of its own credit to them. One level only (a sub-seller cannot open
// sellers). A sub-seller starts with its parent's limits and can never be
// given looser ones by the parent. Every move is two ledger rows written in
// one transaction, so credit is never created or lost.

func (s *Store) ensureSellerSchema() error {
	return s.addColumn("reps", "parent_id", "TEXT NOT NULL DEFAULT ''")
}

func (s *Store) parentOf(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, repID string) (string, error) {
	var p string
	err := q.QueryRowContext(ctx, `SELECT parent_id FROM reps WHERE id=?`, repID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return p, err
}

// IsSellerOf reports whether child is a sub-seller of parent.
func (s *Store) IsSellerOf(ctx context.Context, parent, child string) bool {
	p, err := s.parentOf(ctx, s.ro, child)
	return err == nil && parent != "" && p == parent
}

// PutSeller creates or edits a sub-seller of parent, with an optional login.
func (s *Store) PutSeller(ctx context.Context, parent string, r Rep, user, pass string) error {
	if !idRE.MatchString(r.ID) || r.ID == parent {
		return ErrInvalid
	}
	pp, err := s.parentOf(ctx, s.ro, parent)
	if err != nil {
		return ErrAuth
	}
	if pp != "" {
		return fmt.Errorf("%w: sub-sellers cannot open sellers", ErrPolicy)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing, err := s.parentOf(ctx, tx, r.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		pol, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=?`, parent))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,created_unix,parent_id,max_clients,max_days,max_client_gb,
allow_unlimited,can_delete,refund_unused,inbounds) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.Name, r.Enabled, s.now().Unix(), parent, pol.MaxClients, pol.MaxDays, pol.MaxClientGB,
			pol.AllowUnlimited, pol.CanDelete, pol.RefundUnused, strings.Join(pol.Inbounds, ",")); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("%w: that ID is taken", ErrInvalid)
			}
			return err
		}
	case err != nil:
		return err
	case existing != parent:
		return fmt.Errorf("%w: that ID is taken", ErrInvalid) // never reveal or touch someone else's seller
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE reps SET name=?, enabled=? WHERE id=?`, r.Name, r.Enabled, r.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if user != "" && pass != "" {
		var role, rep string
		err := s.ro.QueryRowContext(ctx, `SELECT role,rep_id FROM admins WHERE username=?`, user).Scan(&role, &rep)
		if err == nil && (role != "rep" || rep != r.ID) {
			return fmt.Errorf("%w: that username is taken", ErrInvalid)
		}
		return s.SetRepLogin(ctx, user, pass, r.ID)
	}
	return nil
}

// Sellers lists parent's sub-sellers.
func (s *Store) Sellers(ctx context.Context, parent string) ([]Rep, error) {
	all, err := s.Reps(ctx)
	if err != nil {
		return nil, err
	}
	out := []Rep{}
	for _, r := range all {
		if r.ParentID == parent && parent != "" {
			out = append(out, r)
		}
	}
	return out, nil
}

// TransferCredit moves credit from parent to its seller (negative values
// take unused credit back).
func (s *Store) TransferCredit(ctx context.Context, actor, parent, child string, volume, units int64, note string) error {
	if volume == 0 && units == 0 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := s.parentOf(ctx, tx, child)
	if err != nil || p != parent || parent == "" {
		return ErrNotFound
	}
	move := func(id string, v, u int64) error {
		r, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+?, unlimited_units=unlimited_units+?
WHERE id=? AND volume_credit+?>=0 AND unlimited_units+?>=0`, v, u, id, v, u)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return ErrCredit
		}
		return nil
	}
	if err := move(parent, -volume, -units); err != nil {
		return err
	}
	if err := move(child, volume, units); err != nil {
		return err
	}
	now := s.now().Unix()
	for _, row := range []struct {
		rep, other string
		v, u       int64
	}{{parent, child, -volume, -units}, {child, parent, volume, units}} {
		n := row.other
		if note != "" {
			n += " — " + note
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,?,?)`,
			now, row.rep, actor, "transfer", row.v, row.u, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetSellerPolicy lets a parent tighten its seller's limits; anything looser
// than the parent's own limits is clamped to them.
func (s *Store) SetSellerPolicy(ctx context.Context, parent, child string, p RepPolicy) error {
	if !s.IsSellerOf(ctx, parent, child) {
		return ErrNotFound
	}
	pp, err := s.RepPolicyOf(ctx, parent)
	if err != nil {
		return err
	}
	clamp := func(v, limit int64) int64 {
		if limit > 0 && (v == 0 || v > limit) {
			return limit
		}
		return v
	}
	p.MaxClients = clamp(p.MaxClients, pp.MaxClients)
	p.MaxDays = clamp(p.MaxDays, pp.MaxDays)
	p.MaxClientGB = clamp(p.MaxClientGB, pp.MaxClientGB)
	p.AllowUnlimited = p.AllowUnlimited && pp.AllowUnlimited
	p.CanDelete = p.CanDelete && pp.CanDelete
	p.RefundUnused = p.RefundUnused && pp.RefundUnused
	if len(pp.Inbounds) > 0 {
		ok := map[string]bool{}
		for _, t := range pp.Inbounds {
			ok[t] = true
		}
		kept := []string{}
		for _, t := range p.Inbounds {
			if ok[t] {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			kept = pp.Inbounds
		}
		p.Inbounds = kept
	}
	p.PriceGB, p.PriceUnit = 0, 0
	return s.SetRepPolicy(ctx, child, p)
}
