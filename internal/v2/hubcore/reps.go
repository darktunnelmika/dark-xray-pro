package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Representatives resell access. Each one holds two independent credits:
// volume credit in bytes for volume plans, and unlimited units where one unit
// is one user for 30 days (an unlimited client for N days costs ceil(N/30)
// units). A representative only sees and edits its own clients; every
// charge and adjustment is an append-only ledger row written in the same
// transaction as the change it pays for.

const repSchema = `
CREATE TABLE IF NOT EXISTS reps (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  volume_credit INTEGER NOT NULL DEFAULT 0 CHECK(volume_credit >= 0),
  unlimited_units INTEGER NOT NULL DEFAULT 0 CHECK(unlimited_units >= 0),
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS credit_ledger (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  rep_id TEXT NOT NULL,
  actor TEXT NOT NULL,
  kind TEXT NOT NULL,
  client_email TEXT NOT NULL DEFAULT '',
  volume_delta INTEGER NOT NULL DEFAULT 0,
  units_delta INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS credit_ledger_rep ON credit_ledger(rep_id, id);
CREATE TRIGGER IF NOT EXISTS credit_ledger_no_update BEFORE UPDATE ON credit_ledger BEGIN SELECT RAISE(ABORT,'ledger is append-only'); END;
CREATE TRIGGER IF NOT EXISTS credit_ledger_no_delete BEFORE DELETE ON credit_ledger BEGIN SELECT RAISE(ABORT,'ledger is append-only'); END;
`

// ErrCredit means the representative's credit does not cover a change.
var ErrCredit = errors.New("insufficient credit")

const unitDays = 30

func (s *Store) ensureRepSchema() error {
	if _, err := s.db.Exec(repSchema); err != nil {
		return err
	}
	for _, c := range []struct{ table, col, def string }{
		{"admins", "role", "TEXT NOT NULL DEFAULT 'owner'"},
		{"admins", "rep_id", "TEXT NOT NULL DEFAULT ''"},
		{"clients", "rep_id", "TEXT NOT NULL DEFAULT ''"},
		{"clients", "plan", "TEXT NOT NULL DEFAULT 'volume'"},
	} {
		if err := s.addColumn(c.table, c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) addColumn(table, col, def string) error {
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return err
		}
		if n == col {
			return nil
		}
	}
	rows.Close()
	_, err = s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, col, def))
	return err
}

// Rep is a representative with its balances.
type Rep struct {
	ID             string
	Name           string
	Enabled        bool
	VolumeCredit   int64
	UnlimitedUnits int64
	Clients        int
	Policy         RepPolicy
}

func (s *Store) PutRep(ctx context.Context, r Rep) error {
	if !idRE.MatchString(r.ID) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,created_unix) VALUES(?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, enabled=excluded.enabled`, r.ID, r.Name, r.Enabled, s.now().Unix())
	return err
}

func (s *Store) Reps(ctx context.Context) ([]Rep, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT r.id,r.name,r.enabled,r.volume_credit,r.unlimited_units,
(SELECT COUNT(*) FROM clients c WHERE c.rep_id=r.id),`+policyCols+` FROM reps r ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rep
	for rows.Next() {
		var r Rep
		p, err := scanPolicy(rows, &r.ID, &r.Name, &r.Enabled, &r.VolumeCredit, &r.UnlimitedUnits, &r.Clients)
		if err != nil {
			return nil, err
		}
		r.Policy = p
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdjustCredit adds (or with negative values removes) credit. Owner only.
func (s *Store) AdjustCredit(ctx context.Context, actor, repID string, volume, units int64, note string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+?, unlimited_units=unlimited_units+? WHERE id=?`, volume, units, repID)
	if err != nil {
		if strings.Contains(err.Error(), "CHECK") {
			return ErrCredit
		}
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,?,?)`,
		s.now().Unix(), repID, actor, "adjust", volume, units, note); err != nil {
		return err
	}
	return tx.Commit()
}

// SetRepLogin creates or resets a representative's panel login.
func (s *Store) SetRepLogin(ctx context.Context, user, pass, repID string) error {
	if err := s.SetAdmin(ctx, user, pass); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE admins SET role='rep', rep_id=? WHERE username=?`, repID, user)
	return err
}

// unitsFor is the unlimited-plan price of a term in days.
func unitsFor(days int64) int64 {
	if days <= 0 {
		return 0
	}
	return (days + unitDays - 1) / unitDays
}

func termDays(c Client, now int64) int64 {
	if c.DurationSeconds > 0 {
		return (c.DurationSeconds + 86399) / 86400
	}
	if c.ExpiresUnix > now {
		return (c.ExpiresUnix - now + 86399) / 86400
	}
	return 0
}

// RepPutClient creates or edits a representative's client and charges the
// difference in the same transaction. Lowering a client never refunds
// automatically; the owner can adjust credit by hand.
func (s *Store) RepPutClient(ctx context.Context, actor, repID string, c Client, plan string) error {
	if plan != "volume" && plan != "unlimited" {
		return ErrInvalid
	}
	now := s.now().Unix()
	if plan == "unlimited" {
		c.VolumeBytes = -1
	} else if c.VolumeBytes < 0 {
		return fmt.Errorf("%w: a volume plan needs a volume", ErrInvalid)
	}
	if termDays(c, now) == 0 {
		return fmt.Errorf("%w: representatives sell a fixed term", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, repID).Scan(&enabled); err != nil || !enabled {
		return ErrAuth
	}
	var existsRep, oldPlan string
	var old Client
	err = tx.QueryRowContext(ctx, `SELECT rep_id,plan,volume_bytes,expires_unix,duration_seconds FROM clients WHERE email=?`, c.Email).
		Scan(&existsRep, &oldPlan, &old.VolumeBytes, &old.ExpiresUnix, &old.DurationSeconds)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return err
	}
	if !isNew && existsRep != repID {
		return ErrNotFound // another seller's (or the owner's) client is invisible
	}
	if err := checkPolicy(ctx, tx, repID, c, plan, isNew, now); err != nil {
		return err
	}
	var volCharge, unitCharge int64
	switch {
	case plan == "volume" && (isNew || oldPlan != "volume"):
		volCharge = c.VolumeBytes
	case plan == "volume":
		volCharge = max(0, c.VolumeBytes-old.VolumeBytes)
	}
	if plan == "unlimited" {
		newUnits := unitsFor(termDays(c, now))
		oldUnits := int64(0)
		if !isNew && oldPlan == "unlimited" {
			oldUnits = unitsFor(termDays(old, now))
		}
		unitCharge = max(0, newUnits-oldUnits)
	}
	if volCharge > 0 || unitCharge > 0 {
		r, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit-?, unlimited_units=unlimited_units-?
WHERE id=? AND volume_credit>=? AND unlimited_units>=?`, volCharge, unitCharge, repID, volCharge, unitCharge)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return ErrCredit
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,units_delta) VALUES(?,?,?,?,?,?,?)`,
			now, repID, actor, "charge", c.Email, -volCharge, -unitCharge); err != nil {
			return err
		}
	}
	if err := putClientTx(ctx, tx, c, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=?, plan=? WHERE email=?`, repID, plan, c.Email); err != nil {
		return err
	}
	return tx.Commit()
}

// Principal is who is calling the panel API.
type Principal struct {
	User  string
	Role  string // owner | rep
	RepID string
}

func (s *Store) principal(ctx context.Context, user string) (Principal, error) {
	p := Principal{User: user}
	err := s.ro.QueryRowContext(ctx, `SELECT role,rep_id FROM admins WHERE username=?`, user).Scan(&p.Role, &p.RepID)
	return p, err
}

// ChangePassword checks the current password first.
func (s *Store) ChangePassword(ctx context.Context, user, oldPass, newPass string) error {
	if _, err := s.Login(ctx, user, oldPass); err != nil {
		return ErrAuth
	}
	var role, rep string
	if err := s.ro.QueryRowContext(ctx, `SELECT role,rep_id FROM admins WHERE username=?`, user).Scan(&role, &rep); err != nil {
		return err
	}
	if err := s.SetAdmin(ctx, user, newPass); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE admins SET role=?, rep_id=? WHERE username=?`, role, rep, user)
	return err
}
