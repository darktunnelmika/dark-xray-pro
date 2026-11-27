package hubcore

import (
	"context"
	"crypto/rand"
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
	UsedBytes      int64 // lifetime: current clients plus deleted ones (and the old panel's)
	UsedHere       int64 // on this panel only: since the reseller was made (or moved) here
	Policy         RepPolicy
	ParentID       string
	ManageProducts bool
}

func (s *Store) PutRep(ctx context.Context, r Rep) error {
	if !idRE.MatchString(r.ID) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,created_unix,manage_products) VALUES(?,?,?,?,1)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, enabled=excluded.enabled`, r.ID, r.Name, r.Enabled, s.now().Unix())
	return err
}

func (s *Store) Reps(ctx context.Context) ([]Rep, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT r.id,r.name,r.enabled,r.volume_credit,r.unlimited_units,
(SELECT COUNT(*) FROM clients c WHERE c.rep_id=r.id),
r.used_base+COALESCE((SELECT SUM(u.up+u.down) FROM client_usage u JOIN clients c ON c.email=u.email WHERE c.rep_id=r.id),0),r.used_here,r.parent_id,r.manage_products,`+policyCols+` FROM reps r ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rep
	for rows.Next() {
		var r Rep
		p, err := scanPolicy(rows, &r.ID, &r.Name, &r.Enabled, &r.VolumeCredit, &r.UnlimitedUnits, &r.Clients, &r.UsedBytes, &r.UsedHere, &r.ParentID, &r.ManageProducts)
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
	if err := tx.Commit(); err != nil {
		return err
	}
	s.creditEvent(ctx, repID, volume, units, note)
	return nil
}

// SetRepLogin creates or resets a representative's panel login.
func (s *Store) SetRepLogin(ctx context.Context, user, pass, repID string) error {
	// Never take over somebody else's login (the owner's, another reseller's).
	var role, rid string
	if err := s.ro.QueryRowContext(ctx, `SELECT role,rep_id FROM admins WHERE username=?`, user).Scan(&role, &rid); err == nil && (role != "rep" || rid != repID) {
		return fmt.Errorf("%w: this sign-in name is already used by another account", ErrInvalid)
	}
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

// maxTermSeconds bounds a first-connection term (100 years): beyond it the
// day arithmetic would overflow into "free" and "never ends".
const maxTermSeconds = 100 * 365 * 86400

// checkTermBounds refuses a term no real plan has.
func checkTermBounds(c Client) error {
	if c.DurationSeconds < 0 || c.DurationSeconds > maxTermSeconds || c.ExpiresUnix < 0 || c.ExpiresUnix > pgMaxUnix {
		return fmt.Errorf("%w: the term is out of range", ErrInvalid)
	}
	return nil
}

// termEnd is when a client's term ends: its expiry, or for a
// first-connection term the first connection plus the term (from now if it
// was never used). 0 means no term.
func termEnd(c Client, firstSeen, now int64) int64 {
	switch {
	case c.ExpiresUnix > 0:
		return c.ExpiresUnix
	case c.DurationSeconds > 0 && firstSeen > 0:
		return firstSeen + c.DurationSeconds
	case c.DurationSeconds > 0:
		return now + c.DurationSeconds
	}
	return 0
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
// difference in the same transaction. Lowering a volume package returns its
// unused paid part only as part of a renewal (the term moves later too: how
// Mirza renews, by resetting and then setting the new package) or when the
// reseller may get unused volume back on delete; otherwise the owner
// adjusts credit by hand.
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
	if s.IsLegacy(ctx, c.Email) {
		return s.repPutLegacy(ctx, repID, c, plan)
	}
	if err := checkTermBounds(c); err != nil {
		return err
	}
	// One term: the node serves the expiry when both are set, so the
	// first-connection term would only price and police the wrong one.
	if c.ExpiresUnix > 0 {
		c.DurationSeconds = 0
	}
	if c.ExpiresUnix <= 0 && c.DurationSeconds <= 0 {
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
	err = tx.QueryRowContext(ctx, `SELECT rep_id,plan,volume_bytes,expires_unix,duration_seconds,max_ips,enabled FROM clients WHERE email=?`, c.Email).
		Scan(&existsRep, &oldPlan, &old.VolumeBytes, &old.ExpiresUnix, &old.DurationSeconds, &old.MaxIPs, &old.Enabled)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return err
	}
	if !isNew && existsRep != repID {
		return ErrNotFound // another seller's (or the owner's) client is invisible
	}
	var firstSeen int64
	_ = tx.QueryRowContext(ctx, `SELECT first_seen_unix FROM client_usage WHERE email=?`, c.Email).Scan(&firstSeen)
	// longer: the change moves the end of the term past what is still paid
	// (a used first-connection term counts from its first connection).
	longer := !isNew && termEnd(c, firstSeen, now) > max(termEnd(old, firstSeen, now), now)
	if isNew && termDays(c, now) == 0 {
		return fmt.Errorf("%w: representatives sell a fixed term", ErrInvalid)
	}
	if err := checkPolicy(ctx, tx, repID, c, plan, isNew, now); err != nil {
		return err
	}
	if isNew {
		if err := s.subSellerPlan(ctx, repID, plan, c.VolumeBytes, termDays(c, now), c.MaxIPs, false); err != nil {
			return err
		}
	} else if s.IsSubSeller(ctx, repID) && (plan != oldPlan || c.VolumeBytes != old.VolumeBytes || longer || c.MaxIPs > old.MaxIPs) {
		return fmt.Errorf("%w: sub-sellers renew with a plan their reseller defined", ErrPolicy)
	}
	if !isNew {
		pol, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=?`, repID))
		if err != nil {
			return err
		}
		var oldTags []string
		if rows, err := tx.QueryContext(ctx, `SELECT tag FROM client_inbounds WHERE email=?`, c.Email); err == nil {
			for rows.Next() {
				var t string
				if rows.Scan(&t) == nil {
					oldTags = append(oldTags, t)
				}
			}
			rows.Close()
		}
		renewed := plan != oldPlan || c.VolumeBytes > old.VolumeBytes || longer
		// DenyEdit covers the IP limit, the inbounds and on/off (see RepPolicy).
		edited := c.MaxIPs != old.MaxIPs || c.Enabled != old.Enabled || !sameSet(oldTags, c.Inbounds)
		if renewed && pol.DenyRenew {
			return fmt.Errorf("%w: renewing clients is not allowed for this representative", ErrPolicy)
		}
		if edited && pol.DenyEdit {
			return fmt.Errorf("%w: changing client settings is not allowed for this representative", ErrPolicy)
		}
	}
	var volCharge, unitCharge int64
	switch {
	case plan == "volume" && (isNew || oldPlan != "volume"):
		volCharge = c.VolumeBytes
	case plan == "volume":
		volCharge = max(0, c.VolumeBytes-old.VolumeBytes)
	}
	if plan == "unlimited" {
		if isNew || oldPlan != "unlimited" {
			unitCharge = unitsFor(termDays(c, now))
		} else {
			// Only the days added past what is still paid are charged.
			// Rounding the old and the new term up each would make any
			// top-up before the end free (30 days left -> 30 days again).
			added := termEnd(c, firstSeen, now) - max(termEnd(old, firstSeen, now), now)
			unitCharge = unitsFor((added + 86399) / 86400)
		}
	}
	if err := repStopQ(ctx, tx, repID, now); err != nil && (volCharge > 0 || unitCharge > 0) {
		// A panel bill past its due time (resale.go) or an ended term
		// (repterm.go): no spending until paid or renewed.
		return err
	}
	if !isNew && plan == "volume" && oldPlan == "volume" && old.VolumeBytes > 0 && c.VolumeBytes < old.VolumeBytes {
		var used, gift int64
		var refundUnused bool
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT up+down FROM client_usage WHERE email=?),0),gift_bytes FROM clients WHERE email=?`, c.Email, c.Email).Scan(&used, &gift)
		_ = tx.QueryRowContext(ctx, `SELECT refund_unused FROM reps WHERE id=?`, repID).Scan(&refundUnused)
		// A renewal: the term moves later, or the usage was just reset
		// through the API (Mirza resets, then sets the new package).
		var reset int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_resets WHERE email=? AND reason='api' AND unix>=?`, c.Email, now-600).Scan(&reset)
		if back := max(0, old.VolumeBytes-gift-max(c.VolumeBytes, used)); back > 0 && (longer || reset > 0 || refundUnused) {
			if _, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+? WHERE id=?`, back, repID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,note) VALUES(?,?,?,?,?,?,?)`,
				now, repID, actor, "refund", c.Email, back, "smaller package: unused volume"); err != nil {
				return err
			}
		}
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
	// A gift (the owner's next plan) shrinks with the package it is part of,
	// so volume or days paid for on top of it stay refundable; a new plan
	// (volume <-> unlimited) is paid in full and ends it.
	if !isNew && plan != oldPlan {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET gift_bytes=0, gift_units=0, gift_until=0 WHERE email=?`, c.Email); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE clients SET gift_bytes=MIN(gift_bytes,MAX(volume_bytes,0)), gift_units=MIN(gift_units,?) WHERE email=?`,
		unitsFor(termDays(c, now)), c.Email); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if isNew {
		days := termDays(c, now)
		s.emitAPIEvent(ctx, repTarget(repID), "client.created", map[string]any{"name": c.Email, "plan": plan, "volume_gb": eventGB(c.VolumeBytes),
			"term_days": days, "expires_unix": c.ExpiresUnix, "charged_gb": eventGB(volCharge), "charged_units": unitCharge, "by": actor})
	} else {
		s.emitAPIEvent(ctx, repTarget(repID), "client.updated", map[string]any{"name": c.Email, "charged_gb": eventGB(volCharge), "charged_units": unitCharge, "by": actor})
	}
	return nil
}

// Principal is who is calling the panel API.
type Principal struct {
	User  string
	Role  string // owner | rep
	RepID string
	Level string // "" = full owner; admin | viewer = staff with the owner's scope (staff.go)
}

func (s *Store) principal(ctx context.Context, user string) (Principal, error) {
	p := Principal{User: user}
	err := s.ro.QueryRowContext(ctx, `SELECT role,rep_id FROM admins WHERE username=?`, user).Scan(&p.Role, &p.RepID)
	if p.Role == "admin" || p.Role == "viewer" {
		p.Level, p.Role, p.RepID = p.Role, "owner", ""
	}
	return p, err
}

// ChangePassword checks the current password first.
func (s *Store) ChangePassword(ctx context.Context, user, oldPass, newPass string) error {
	if _, _, err := s.verifyPassword(ctx, user, oldPass); err != nil {
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

// RepLogins lists the sign-in names of a reseller's panel.
func (s *Store) RepLogins(ctx context.Context, repID string) []string {
	rows, err := s.ro.QueryContext(ctx, `SELECT username FROM admins WHERE role='rep' AND rep_id=? ORDER BY created_unix`, repID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			out = append(out, u)
		}
	}
	return out
}

// RepCreate is everything a new reseller needs, saved in one go.
type RepCreate struct {
	ID       string
	Name     string
	Username string // sign-in name; empty = the id
	Password string
	Policy   *RepPolicy // nil = the default policy
	// ManageProducts: may define own products (nil = yes).
	ManageProducts *bool
	CreditGB       float64
	Units          int64
	TgOwners       []int64 // numeric Telegram ids that manage this panel in the bot
}

// CreateRep makes a reseller with its login, limits, credit and Telegram
// ids. It is all or nothing: when a later step fails, nothing is left of it.
func (s *Store) CreateRep(ctx context.Context, actor string, in RepCreate) error {
	in.ID = strings.ToLower(strings.TrimSpace(in.ID))
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.Password = strings.TrimSpace(in.Password)
	if in.Username == "" {
		in.Username = in.ID
	}
	if !idRE.MatchString(in.ID) {
		return fmt.Errorf("%w: the id may only have lowercase letters, digits, dot, dash and underscore", ErrInvalid)
	}
	if !s.RepNameFree(ctx, in.ID) {
		return fmt.Errorf("%w: a reseller or a sign-in name %q already exists", ErrInvalid, in.ID)
	}
	if !idRE.MatchString(in.Username) {
		return fmt.Errorf("%w: the sign-in name may only have lowercase letters, digits, dot, dash and underscore", ErrInvalid)
	}
	var n int
	if _ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE username=?`, in.Username).Scan(&n); n != 0 {
		return fmt.Errorf("%w: this sign-in name is already used by another account", ErrInvalid)
	}
	if len(in.Password) < 10 {
		return fmt.Errorf("%w: the password needs at least 10 characters", ErrInvalid)
	}
	if in.CreditGB < 0 || in.Units < 0 {
		return fmt.Errorf("%w: credit cannot be negative", ErrInvalid)
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.ID
	}
	if err := s.PutRep(ctx, Rep{ID: in.ID, Name: strings.TrimSpace(in.Name), Enabled: true}); err != nil {
		return err
	}
	undo := func(err error) error {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM admins WHERE role='rep' AND rep_id=?`, in.ID)
		_, _ = s.DeleteRep(ctx, in.ID, false)
		return err
	}
	if err := s.SetRepLogin(ctx, in.Username, in.Password, in.ID); err != nil {
		return undo(err)
	}
	pol := DefaultRepPolicy
	if in.Policy != nil {
		pol = *in.Policy
	}
	if err := s.SetRepPolicy(ctx, in.ID, pol); err != nil {
		return undo(err)
	}
	if in.ManageProducts != nil && !*in.ManageProducts {
		if err := s.SetManageProducts(ctx, in.ID, false); err != nil {
			return undo(err)
		}
	}
	if in.CreditGB > 0 || in.Units > 0 {
		if err := s.AdjustCredit(ctx, actor, in.ID, int64(in.CreditGB*(1<<30)), in.Units, "first credit"); err != nil {
			return undo(err)
		}
	}
	if len(in.TgOwners) > 0 {
		if err := s.SetTgRepOwners(ctx, in.ID, in.TgOwners); err != nil {
			return undo(err)
		}
	}
	return nil
}

// RandomPassword returns a 14-character password without look-alike
// characters (no 0/O, 1/l/I), easy to read out and to type.
func RandomPassword() string {
	const abc = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 14)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return string(b)
}
