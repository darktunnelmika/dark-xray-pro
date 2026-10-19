package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// RepresentativePermissions are native HUB access controls. They do not
// change customer entitlements, Xray processes, quota/traffic or billing.
type RepresentativePermissions struct {
	ViewClients   bool `json:"view_clients"`
	CreateClients bool `json:"create_clients"`
	RenewClients  bool `json:"renew_clients"`
	EditClients   bool `json:"edit_clients"`
	ManageClients bool `json:"manage_clients"`
	ManageGroups  bool `json:"manage_groups"`
	ViewFinance   bool `json:"view_finance"`
}

func DefaultRepresentativePermissions() RepresentativePermissions {
	return RepresentativePermissions{true, true, true, true, true, true, true}
}
func (p RepresentativePermissions) AllEnabled() bool {
	return p.ViewClients && p.CreateClients && p.RenewClients && p.EditClients && p.ManageClients && p.ManageGroups && p.ViewFinance
}

type RepresentativeControl struct {
	Representative Representative            `json:"representative"`
	Permissions    RepresentativePermissions `json:"permissions"`
	LoginSuspended bool                      `json:"login_suspended"`
	Revision       int64                     `json:"revision"`
	ActiveSessions int64                     `json:"active_sessions"`
}
type RepresentativeControlChange struct {
	EventID          string                    `json:"event_id"`
	RepresentativeID string                    `json:"representative_id"`
	ExpectedRevision int64                     `json:"expected_revision"`
	DisplayName      string                    `json:"display_name"`
	LoginSuspended   bool                      `json:"login_suspended"`
	Permissions      RepresentativePermissions `json:"permissions"`
	Reason           string                    `json:"reason"`
}
type RepresentativeControlReceipt struct {
	Control                RepresentativeControl `json:"control"`
	Replayed               bool                  `json:"replayed"`
	SessionsRevoked        bool                  `json:"sessions_revoked"`
	BalanceChanged         bool                  `json:"balance_changed"`
	ClientsDeleted         bool                  `json:"clients_deleted"`
	CustomerTrafficChanged bool                  `json:"customer_traffic_changed"`
}
type RepresentativeControlEvent struct {
	EventID                string                    `json:"event_id"`
	RepresentativeID       string                    `json:"representative_id"`
	PreviousName           string                    `json:"previous_name"`
	NewName                string                    `json:"new_name"`
	PreviousLoginSuspended bool                      `json:"previous_login_suspended"`
	NewLoginSuspended      bool                      `json:"new_login_suspended"`
	PreviousPermissions    RepresentativePermissions `json:"previous_permissions"`
	NewPermissions         RepresentativePermissions `json:"new_permissions"`
	Reason                 string                    `json:"reason"`
	CreatedUnix            int64                     `json:"created_unix"`
}
type RepresentativeActivity struct {
	RepresentativeID          string `json:"representative_id"`
	SavedClients              int64  `json:"saved_clients"`
	ArchivedClients           int64  `json:"archived_clients"`
	RecordedClientCreations   int64  `json:"recorded_client_creations"`
	RecordedClientRenewals    int64  `json:"recorded_client_renewals"`
	RecordedProfileEdits      int64  `json:"recorded_profile_edits"`
	RecordedCreditAdjustments int64  `json:"recorded_credit_adjustments"`
	AvailableVolumeBytes      string `json:"available_volume_bytes"`
	AvailableUnlimitedUnits   string `json:"available_unlimited_units"`
	MetricScope               string `json:"metric_scope"`
}

// Only additive, append-only schema. Existing V8 tables keep same user_version
// and can still be opened on rollback. Restricted accounts carry an
// auth_users.disabled fail-closed flag honored by V8, while V9 uses the
// finer-grained permissions once explicit control rows exist.
func ensureRepresentativeControls(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS representative_controls (
    representative_id TEXT PRIMARY KEY REFERENCES representatives(id),
    revision INTEGER NOT NULL CHECK(revision>=1),
    login_suspended INTEGER NOT NULL CHECK(login_suspended IN (0,1)),
    view_clients INTEGER NOT NULL CHECK(view_clients IN (0,1)),
    create_clients INTEGER NOT NULL CHECK(create_clients IN (0,1)),
    renew_clients INTEGER NOT NULL CHECK(renew_clients IN (0,1)),
    edit_clients INTEGER NOT NULL CHECK(edit_clients IN (0,1)),
    manage_clients INTEGER NOT NULL CHECK(manage_clients IN (0,1)),
    manage_groups INTEGER NOT NULL CHECK(manage_groups IN (0,1)),
    view_finance INTEGER NOT NULL CHECK(view_finance IN (0,1)),
    updated_unix INTEGER NOT NULL
  )`,
		`CREATE TABLE IF NOT EXISTS representative_control_events (
    event_id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    representative_id TEXT NOT NULL REFERENCES representatives(id),
    previous_name TEXT NOT NULL, new_name TEXT NOT NULL,
    previous_login_suspended INTEGER NOT NULL CHECK(previous_login_suspended IN (0,1)),
    new_login_suspended INTEGER NOT NULL CHECK(new_login_suspended IN (0,1)),
    previous_permissions_json TEXT NOT NULL,
    new_permissions_json TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_unix INTEGER NOT NULL
  )`,
		`CREATE INDEX IF NOT EXISTS representative_control_events_owner_history
    ON representative_control_events(representative_id,created_unix DESC,event_id)`,
		`CREATE INDEX IF NOT EXISTS client_profile_edits_rep_time_v9 ON client_profile_edits(representative_id,created_unix DESC,event_id)`,
		`CREATE TRIGGER IF NOT EXISTS representative_control_events_immutable_update
    BEFORE UPDATE ON representative_control_events BEGIN SELECT RAISE(ABORT,'representative control audits are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS representative_control_events_immutable_delete
    BEFORE DELETE ON representative_control_events BEGIN SELECT RAISE(ABORT,'representative control audits are append-only'); END`,
	}
	for _, q := range statements {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func readRepresentativeControl(ctx context.Context, tx *sql.Tx, id string) (RepresentativeControl, error) {
	var out RepresentativeControl
	rep, err := readRep(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.Representative = rep
	out.Permissions = DefaultRepresentativePermissions()
	var flags [8]int
	err = tx.QueryRowContext(ctx, `SELECT revision,login_suspended,
   view_clients,create_clients,renew_clients,edit_clients,manage_clients,manage_groups,view_finance
   FROM representative_controls WHERE representative_id=?`, id).Scan(
		&out.Revision, &flags[0], &flags[1], &flags[2], &flags[3], &flags[4], &flags[5], &flags[6], &flags[7])
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return RepresentativeControl{}, err
	}
	if err == nil {
		out.LoginSuspended = flags[0] == 1
		out.Permissions = RepresentativePermissions{
			ViewClients: flags[1] == 1, CreateClients: flags[2] == 1, RenewClients: flags[3] == 1,
			EditClients: flags[4] == 1, ManageClients: flags[5] == 1, ManageGroups: flags[6] == 1, ViewFinance: flags[7] == 1,
		}
	}
	// Count only unexpired saved login sessions; no activity fake-online metric.
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_sessions
     WHERE user_id=? AND expires_unix>?`, id, time.Now().UTC().Unix()).Scan(&out.ActiveSessions); err != nil {
		return RepresentativeControl{}, err
	}
	return out, nil
}
func (s *Store) RepresentativeControl(ctx context.Context, id string) (RepresentativeControl, error) {
	if !idRE.MatchString(id) {
		return RepresentativeControl{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RepresentativeControl{}, err
	}
	defer tx.Rollback()
	c, err := readRepresentativeControl(ctx, tx, id)
	if err != nil {
		return RepresentativeControl{}, err
	}
	if err = tx.Commit(); err != nil {
		return RepresentativeControl{}, err
	}
	return c, nil
}

// For a legacy rep with no controls row, all existing functionality stays
// enabled. Owner permissions are independent of the reseller toggle set.
func (s *Store) RepresentativePermissions(ctx context.Context, id string) (RepresentativePermissions, error) {
	if !idRE.MatchString(id) {
		return RepresentativePermissions{}, ErrInvalid
	}
	var p RepresentativePermissions
	var flags [7]int
	err := s.db.QueryRowContext(ctx, `SELECT view_clients,create_clients,renew_clients,
  edit_clients,manage_clients,manage_groups,view_finance
  FROM representative_controls WHERE representative_id=?`, id).Scan(
		&flags[0], &flags[1], &flags[2], &flags[3], &flags[4], &flags[5], &flags[6])
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultRepresentativePermissions(), nil
	}
	if err != nil {
		return p, err
	}
	p = RepresentativePermissions{
		ViewClients: flags[0] == 1, CreateClients: flags[1] == 1, RenewClients: flags[2] == 1,
		EditClients: flags[3] == 1, ManageClients: flags[4] == 1, ManageGroups: flags[5] == 1,
		ViewFinance: flags[6] == 1,
	}
	return p, nil
}
func (s *Store) UpdateRepresentativeControl(ctx context.Context, input RepresentativeControlChange) (RepresentativeControlReceipt, error) {
	var out RepresentativeControlReceipt
	input.DisplayName = normalizeRepresentativeDisplayName(input.DisplayName)
	input.Reason = normalizeRepresentativeDisplayName(input.Reason)
	if !eventRE.MatchString(input.EventID) || !idRE.MatchString(input.RepresentativeID) ||
		input.RepresentativeID == "owner" || input.ExpectedRevision < 0 || input.ExpectedRevision > 1_000_000_000 ||
		!validName(input.DisplayName, 128) || !validName(input.Reason, 200) {
		return out, ErrInvalid
	}
	normalized := input
	fp := fingerprint("representative-control-v9", normalized)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// Same event nonce across credit, client/renewal, profile and control tables.
	var knownFP string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint FROM representative_control_events
  WHERE event_id=?`, input.EventID).Scan(&knownFP)
	if err == nil {
		if knownFP != fp {
			return out, ErrConflict
		}
		c, e := readRepresentativeControl(ctx, tx, input.RepresentativeID)
		if e != nil {
			return out, e
		}
		if e = tx.Commit(); e != nil {
			return out, e
		}
		return RepresentativeControlReceipt{Control: c, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	for _, table := range []string{"events", "client_operations", "client_profile_edits", "catalog_events","catalog_policy_events"} {
		var found int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE event_id=?", input.EventID).Scan(&found); err != nil {
			return out, err
		}
		if found != 0 {
			return out, ErrConflict
		}
	}
	current, err := readRepresentativeControl(ctx, tx, input.RepresentativeID)
	if err != nil {
		return out, err
	}
	if current.Representative.Kind != "representative" || current.Revision != input.ExpectedRevision {
		return out, ErrConflict
	}
	if current.Representative.DisplayName == input.DisplayName && current.LoginSuspended == input.LoginSuspended && current.Permissions == input.Permissions {
		return out, ErrInvalid
	}
	newVersion := current.Revision + 1
	now := time.Now().UTC().Unix()
	if _, err = tx.ExecContext(ctx, `UPDATE representatives SET display_name=? WHERE id=?`, input.DisplayName, input.RepresentativeID); err != nil {
		return out, err
	}
	nextBool := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO representative_controls(
  representative_id,revision,login_suspended,view_clients,create_clients,renew_clients,edit_clients,
  manage_clients,manage_groups,view_finance,updated_unix)
  VALUES(?,?,?,?,?,?,?,?,?,?,?)
  ON CONFLICT(representative_id) DO UPDATE SET
   revision=excluded.revision,login_suspended=excluded.login_suspended,
   view_clients=excluded.view_clients,create_clients=excluded.create_clients,
   renew_clients=excluded.renew_clients,edit_clients=excluded.edit_clients,
   manage_clients=excluded.manage_clients,manage_groups=excluded.manage_groups,
   view_finance=excluded.view_finance,updated_unix=excluded.updated_unix`,
		input.RepresentativeID, newVersion, nextBool(input.LoginSuspended),
		nextBool(input.Permissions.ViewClients), nextBool(input.Permissions.CreateClients),
		nextBool(input.Permissions.RenewClients), nextBool(input.Permissions.EditClients),
		nextBool(input.Permissions.ManageClients), nextBool(input.Permissions.ManageGroups),
		nextBool(input.Permissions.ViewFinance), now); err != nil {
		return out, err
	}
	// Rollback safeguard: V8 knows ONLY auth_users.disabled. Set it when
	// V9 denies login OR when V9 has any restriction that V8 cannot enforce.
	// V9 treats explicit control as the authoritative login-suspend field.
	fallbackDisabled := input.LoginSuspended || !input.Permissions.AllEnabled()
	if _, err = tx.ExecContext(ctx, `UPDATE auth_users SET disabled=? WHERE representative_id=? AND role='representative'`,
		nextBool(fallbackDisabled), input.RepresentativeID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id IN (
  SELECT id FROM auth_users WHERE representative_id=? AND role='representative'
 )`, input.RepresentativeID); err != nil {
		return out, err
	}
	previousJSON, err := json.Marshal(current.Permissions)
	if err != nil {
		return out, err
	}
	newJSON, err := json.Marshal(input.Permissions)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO representative_control_events(
  event_id,fingerprint,representative_id,previous_name,new_name,
  previous_login_suspended,new_login_suspended,previous_permissions_json,
  new_permissions_json,reason,created_unix)
  VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		input.EventID, fp, input.RepresentativeID, current.Representative.DisplayName, input.DisplayName,
		nextBool(current.LoginSuspended), nextBool(input.LoginSuspended),
		string(previousJSON), string(newJSON), input.Reason, now); err != nil {
		return out, err
	}
	after, err := readRepresentativeControl(ctx, tx, input.RepresentativeID)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return RepresentativeControlReceipt{Control: after, SessionsRevoked: true}, nil
}
func normalizeRepresentativeDisplayName(s string) string { return strings.TrimSpace(s) }
func (s *Store) ListRepresentativeControlEvents(ctx context.Context, id string, limit, offset int) ([]RepresentativeControlEvent, error) {
	if !idRE.MatchString(id) || limit < 1 || limit > 50 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	r, err := s.Representative(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.Kind != "representative" {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,representative_id,previous_name,new_name,
  previous_login_suspended,new_login_suspended,previous_permissions_json,
  new_permissions_json,reason,created_unix FROM representative_control_events
  WHERE representative_id=? ORDER BY created_unix DESC,event_id DESC LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RepresentativeControlEvent, 0)
	for rows.Next() {
		var e RepresentativeControlEvent
		var oldDisabled, newDisabled int
		var oldJSON, newJSON string
		if err = rows.Scan(&e.EventID, &e.RepresentativeID, &e.PreviousName, &e.NewName,
			&oldDisabled, &newDisabled, &oldJSON, &newJSON, &e.Reason, &e.CreatedUnix); err != nil {
			return nil, err
		}
		e.PreviousLoginSuspended = oldDisabled == 1
		e.NewLoginSuspended = newDisabled == 1
		if err = json.Unmarshal([]byte(oldJSON), &e.PreviousPermissions); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(newJSON), &e.NewPermissions); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) RepresentativeActivity(ctx context.Context, id string) (RepresentativeActivity, error) {
	var out RepresentativeActivity
	if !idRE.MatchString(id) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RepresentativeActivity{}, err
	}
	defer tx.Rollback()
	rep, err := readRep(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.RepresentativeID = id
	out.AvailableVolumeBytes = strconv.FormatInt(rep.VolumeCreditBytes, 10)
	out.AvailableUnlimitedUnits = strconv.FormatInt(rep.UnlimitedCreditUnits, 10)
	out.MetricScope = "saved_records_and_immutable_events_not_live_xray_traffic"
	values := []struct {
		sql string
		dst *int64
	}{
		{`SELECT COUNT(*) FROM clients WHERE representative_id=?`, &out.SavedClients},
		{`SELECT COUNT(*) FROM clients WHERE representative_id=? AND archived=1`, &out.ArchivedClients},
		{`SELECT COUNT(*) FROM events WHERE representative_id=? AND kind='client_created'`, &out.RecordedClientCreations},
		{`SELECT COUNT(*) FROM client_operations WHERE representative_id=? AND kind='extend'`, &out.RecordedClientRenewals},
		{`SELECT COUNT(*) FROM client_profile_edits WHERE representative_id=?`, &out.RecordedProfileEdits},
		{`SELECT COUNT(*) FROM events WHERE representative_id=? AND kind='credit_adjustment'`, &out.RecordedCreditAdjustments},
	}
	for _, item := range values {
		if err = tx.QueryRowContext(ctx, item.sql, id).Scan(item.dst); err != nil {
			return RepresentativeActivity{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return RepresentativeActivity{}, err
	}
	return out, nil
}
