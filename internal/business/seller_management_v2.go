package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrSellerPermissionDenied = errors.New("seller management permission denied")

type SellerPermissionV2 struct {
	RepresentativeID string `json:"representative_id"`
	ManageSellers    bool   `json:"manage_sellers"`
	Revision         int64  `json:"revision"`
}

type SellerPermissionChangeV2 struct {
	EventID          string `json:"event_id"`
	RepresentativeID string `json:"representative_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	ManageSellers    bool   `json:"manage_sellers"`
	Reason           string `json:"reason"`
}

type SellerPermissionReceiptV2 struct {
	SellerPermissionV2
	Replayed        bool `json:"replayed"`
	SessionsRevoked bool `json:"sessions_revoked"`
}

type SellerStateChangeV2 struct {
	EventID          string `json:"event_id"`
	RepresentativeID string `json:"representative_id"`
	SellerID         string `json:"seller_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
	Reason           string `json:"reason"`
}

type SellerStateReceiptV2 struct {
	Seller   SellerQuotaV2 `json:"seller"`
	Replayed bool          `json:"replayed"`
}

type sellerQueryV2 interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// This schema is created only inside the explicit seller migration. It does
// not rewrite V9 permissions, historical fingerprints or quota receipts.
func ensureSellerManagementV2(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS seller_management_access_v2_meta(
 id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
		`CREATE TABLE IF NOT EXISTS representative_seller_permissions_v2(
 representative_id TEXT PRIMARY KEY REFERENCES representatives(id),
 manage_sellers INTEGER NOT NULL CHECK(manage_sellers IN(0,1)),
 revision INTEGER NOT NULL CHECK(revision>=1),updated_unix INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS representative_seller_permission_events_v2(
 event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,
 representative_id TEXT NOT NULL REFERENCES representatives(id),
 actor_user_id TEXT NOT NULL REFERENCES auth_users(id),
 previous_allowed INTEGER NOT NULL CHECK(previous_allowed IN(0,1)),
 new_allowed INTEGER NOT NULL CHECK(new_allowed IN(0,1)),
 previous_revision INTEGER NOT NULL,new_revision INTEGER NOT NULL,
 receipt_json TEXT NOT NULL,reason TEXT NOT NULL,created_unix INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS seller_state_revisions_v2(
 representative_id TEXT NOT NULL,seller_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),
 PRIMARY KEY(representative_id,seller_id),
 FOREIGN KEY(representative_id,seller_id) REFERENCES representative_sellers_v2(representative_id,id))`,
		`CREATE TABLE IF NOT EXISTS seller_state_events_v2(
 event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,
 representative_id TEXT NOT NULL,seller_id TEXT NOT NULL,
 actor_user_id TEXT NOT NULL REFERENCES auth_users(id),
 previous_enabled INTEGER NOT NULL CHECK(previous_enabled IN(0,1)),
 new_enabled INTEGER NOT NULL CHECK(new_enabled IN(0,1)),
 previous_revision INTEGER NOT NULL,new_revision INTEGER NOT NULL,
 receipt_json TEXT NOT NULL,reason TEXT NOT NULL,created_unix INTEGER NOT NULL,
 FOREIGN KEY(representative_id,seller_id) REFERENCES representative_sellers_v2(representative_id,id))`,
		`CREATE TABLE IF NOT EXISTS seller_quota_actors_v2(
 event_id TEXT PRIMARY KEY REFERENCES seller_quota_events_v2(event_id),
 actor_user_id TEXT NOT NULL REFERENCES auth_users(id))`,
		`CREATE INDEX IF NOT EXISTS seller_permission_history_v2 ON
 representative_seller_permission_events_v2(representative_id,created_unix,event_id)`,
		`CREATE INDEX IF NOT EXISTS seller_state_history_v2 ON
 seller_state_events_v2(representative_id,seller_id,created_unix,event_id)`,
		`INSERT OR IGNORE INTO seller_management_access_v2_meta(id,version) VALUES(1,1)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, table := range []string{"representative_seller_permission_events_v2", "seller_state_events_v2", "seller_quota_actors_v2"} {
		for _, action := range []string{"UPDATE", "DELETE"} {
			if _, err := tx.ExecContext(ctx, "CREATE TRIGGER IF NOT EXISTS "+table+"_no_"+action+
				" BEFORE "+action+" ON "+table+" BEGIN SELECT RAISE(ABORT,'seller management audit immutable'); END"); err != nil {
				return err
			}
		}
	}
	return sellerManagementConfiguredV2(ctx, tx)
}

func sellerManagementConfiguredV2(ctx context.Context, q sellerQueryV2) error {
	var quotaVersion, accessVersion int
	if err := q.QueryRowContext(ctx, `SELECT q.version,a.version FROM seller_quota_v2_meta q
 CROSS JOIN seller_management_access_v2_meta a WHERE q.id=1 AND a.id=1`).Scan(&quotaVersion, &accessVersion); err != nil || quotaVersion != 1 || accessVersion != 1 {
		return ErrUnavailable
	}
	return nil
}

// Absence of the explicit schema leaves old logins unchanged and the new
// permission denied. Never infer seller administration from V9 permissions.
func sessionSellerPermissionV2(ctx context.Context, q sellerQueryV2, representativeID string) (bool, error) {
	var configured int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
 WHERE type='table' AND name='seller_management_access_v2_meta'`).Scan(&configured); err != nil {
		return false, err
	}
	if configured == 0 {
		return false, nil
	}
	if err := sellerManagementConfiguredV2(ctx, q); err != nil {
		return false, err
	}
	var allowed int
	err := q.QueryRowContext(ctx, `SELECT manage_sellers FROM representative_seller_permissions_v2
 WHERE representative_id=?`, representativeID).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return allowed == 1 && err == nil, err
}

// Authoritative source checks run in the SAME transaction as each protected
// read/write, including replays. Only the opaque verified browser token crosses
// the API boundary; role, actor ID and tenant are loaded from the database.
func authorizeSellerAccessV2(ctx context.Context, tx *sql.Tx, token, representativeID string, requireManage, ownerOnly bool) (string, error) {
	if err := sellerManagementConfiguredV2(ctx, tx); err != nil {
		return "", err
	}
	if len(token) != 64 {
		return "", ErrUnauthenticated
	}
	var actorID, role, parent string
	var expiry int64
	var disabled, parentEnabled int
	err := tx.QueryRowContext(ctx, `SELECT a.id,a.role,a.representative_id,ss.expires_unix,
 CASE WHEN a.role='representative' AND c.representative_id IS NOT NULL
 THEN c.login_suspended ELSE a.disabled END,r.enabled
 FROM auth_sessions ss JOIN auth_users a ON a.id=ss.user_id
 JOIN representatives r ON r.id=a.representative_id
 LEFT JOIN representative_controls c ON c.representative_id=a.representative_id
 WHERE ss.token_sha256=?`, digestSession(token)).Scan(&actorID, &role, &parent, &expiry, &disabled, &parentEnabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (expiry <= time.Now().UTC().Unix() || disabled != 0 || parentEnabled != 1) {
		return "", ErrUnauthenticated
	}
	if err != nil {
		return "", ErrUnavailable
	}
	if role == "owner" && parent == "owner" {
		return actorID, nil
	}
	if ownerOnly || role != "representative" || parent != representativeID {
		return "", ErrSellerPermissionDenied
	}
	if requireManage {
		allowed, err := sessionSellerPermissionV2(ctx, tx, representativeID)
		if err != nil {
			return "", err
		}
		if !allowed {
			return "", ErrSellerPermissionDenied
		}
	}
	return actorID, nil
}

func readSellerPermissionV2(ctx context.Context, tx *sql.Tx, id string) (SellerPermissionV2, error) {
	out := SellerPermissionV2{RepresentativeID: id}
	rep, err := readRep(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, ErrUnavailable
	}
	if rep.Kind != "representative" {
		return out, ErrInvalid
	}
	var allowed int
	err = tx.QueryRowContext(ctx, `SELECT manage_sellers,revision FROM representative_seller_permissions_v2
 WHERE representative_id=?`, id).Scan(&allowed, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	out.ManageSellers = allowed == 1
	return out, err
}

func (s *Store) SellerPermissionV2(ctx context.Context, token, representativeID string) (SellerPermissionV2, error) {
	if !idRE.MatchString(representativeID) {
		return SellerPermissionV2{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SellerPermissionV2{}, ErrUnavailable
	}
	defer tx.Rollback()
	if _, err = authorizeSellerAccessV2(ctx, tx, token, representativeID, false, false); err != nil {
		return SellerPermissionV2{}, err
	}
	return readSellerPermissionV2(ctx, tx, representativeID)
}

func sellerManagementEventConflictV2(ctx context.Context, tx *sql.Tx, eventID, ownTable string) error {
	if err := rejectSellerOrderEventV1(ctx, tx, eventID); err != nil {
		return err
	}
	for _, table := range []string{"seller_quota_events_v2", "representative_seller_permission_events_v2", "seller_state_events_v2"} {
		if table == ownTable {
			continue
		}
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE event_id=?", eventID).Scan(&found); err != nil {
			return ErrUnavailable
		}
		if found != 0 {
			return ErrConflict
		}
	}
	return nil
}

func (s *Store) UpdateSellerPermissionV2(ctx context.Context, token string, input SellerPermissionChangeV2) (SellerPermissionReceiptV2, error) {
	var out SellerPermissionReceiptV2
	if !eventRE.MatchString(input.EventID) || !idRE.MatchString(input.RepresentativeID) || input.RepresentativeID == "owner" ||
		input.ExpectedRevision < 0 || input.ExpectedRevision >= 1_000_000_000 || !validName(input.Reason, 200) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, ErrUnavailable
	}
	defer tx.Rollback()
	actorID, err := authorizeSellerAccessV2(ctx, tx, token, input.RepresentativeID, false, true)
	if err != nil {
		return out, err
	}
	fp := fingerprint("seller-permission-v2", input)
	var previousFP, raw string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,receipt_json FROM representative_seller_permission_events_v2
 WHERE event_id=?`, input.EventID).Scan(&previousFP, &raw)
	if err == nil {
		if previousFP != fp {
			return out, ErrConflict
		}
		if json.Unmarshal([]byte(raw), &out) != nil || out.RepresentativeID != input.RepresentativeID {
			return SellerPermissionReceiptV2{}, ErrUnavailable
		}
		out.Replayed = true
		out.SessionsRevoked = false
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, ErrUnavailable
	}
	if err = sellerManagementEventConflictV2(ctx, tx, input.EventID, "representative_seller_permission_events_v2"); err != nil {
		return out, err
	}
	current, err := readSellerPermissionV2(ctx, tx, input.RepresentativeID)
	if err != nil {
		return out, err
	}
	if current.Revision != input.ExpectedRevision {
		return out, ErrConflict
	}
	if current.ManageSellers == input.ManageSellers {
		return out, ErrInvalid
	}
	out.SellerPermissionV2 = SellerPermissionV2{RepresentativeID: input.RepresentativeID, ManageSellers: input.ManageSellers, Revision: current.Revision + 1}
	out.SessionsRevoked = true
	now := time.Now().UTC().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO representative_seller_permissions_v2(representative_id,manage_sellers,revision,updated_unix)
 VALUES(?,?,?,?) ON CONFLICT(representative_id) DO UPDATE SET manage_sellers=excluded.manage_sellers,
 revision=excluded.revision,updated_unix=excluded.updated_unix`, input.RepresentativeID, input.ManageSellers, out.Revision, now); err != nil {
		return SellerPermissionReceiptV2{}, ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id IN
 (SELECT id FROM auth_users WHERE role='representative' AND representative_id=?)`, input.RepresentativeID); err != nil {
		return SellerPermissionReceiptV2{}, ErrUnavailable
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return SellerPermissionReceiptV2{}, ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO representative_seller_permission_events_v2
 (event_id,fingerprint,representative_id,actor_user_id,previous_allowed,new_allowed,previous_revision,new_revision,receipt_json,reason,created_unix)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, input.EventID, fp, input.RepresentativeID, actorID, current.ManageSellers, input.ManageSellers,
		current.Revision, out.Revision, string(encoded), input.Reason, now); err != nil {
		return SellerPermissionReceiptV2{}, ErrUnavailable
	}
	if err = tx.Commit(); err != nil {
		return SellerPermissionReceiptV2{}, ErrUnavailable
	}
	return out, nil
}

func readSellerRevisionV2(ctx context.Context, q sellerQueryV2, seller *SellerQuotaV2) error {
	err := q.QueryRowContext(ctx, `SELECT revision FROM seller_state_revisions_v2
 WHERE representative_id=? AND seller_id=?`, seller.RepresentativeID, seller.ID).Scan(&seller.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		seller.Revision = 0
		return nil
	}
	return err
}

func (s *Store) UpdateSellerStateV2(ctx context.Context, token string, input SellerStateChangeV2) (SellerStateReceiptV2, error) {
	var out SellerStateReceiptV2
	if !eventRE.MatchString(input.EventID) || !idRE.MatchString(input.RepresentativeID) || input.RepresentativeID == "owner" ||
		!idRE.MatchString(input.SellerID) || input.ExpectedRevision < 0 || input.ExpectedRevision >= 1_000_000_000 || !validName(input.Reason, 200) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, ErrUnavailable
	}
	defer tx.Rollback()
	actorID, err := authorizeSellerAccessV2(ctx, tx, token, input.RepresentativeID, true, false)
	if err != nil {
		return out, err
	}
	fp := fingerprint("seller-state-v2", input)
	var previousFP, raw string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,receipt_json FROM seller_state_events_v2 WHERE event_id=?`, input.EventID).Scan(&previousFP, &raw)
	if err == nil {
		if previousFP != fp {
			return out, ErrConflict
		}
		if json.Unmarshal([]byte(raw), &out) != nil || out.Seller.RepresentativeID != input.RepresentativeID || out.Seller.ID != input.SellerID {
			return SellerStateReceiptV2{}, ErrUnavailable
		}
		out.Replayed = true
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, ErrUnavailable
	}
	if err = sellerManagementEventConflictV2(ctx, tx, input.EventID, "seller_state_events_v2"); err != nil {
		return out, err
	}
	rep, seller, err := sellerCurrentV2(ctx, tx, input.RepresentativeID, input.SellerID)
	if err != nil {
		return out, err
	}
	if rep.Kind != "representative" || seller.Revision != input.ExpectedRevision || input.Enabled && !rep.Enabled {
		return out, ErrConflict
	}
	if seller.Enabled == input.Enabled {
		return out, ErrInvalid
	}
	out.Seller = seller
	out.Seller.Enabled = input.Enabled
	out.Seller.Revision++
	if _, err = tx.ExecContext(ctx, `UPDATE representative_sellers_v2 SET enabled=? WHERE representative_id=? AND id=?`,
		input.Enabled, input.RepresentativeID, input.SellerID); err != nil {
		return SellerStateReceiptV2{}, ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO seller_state_revisions_v2(representative_id,seller_id,revision)
 VALUES(?,?,?) ON CONFLICT(representative_id,seller_id) DO UPDATE SET revision=excluded.revision`,
		input.RepresentativeID, input.SellerID, out.Seller.Revision); err != nil {
		return SellerStateReceiptV2{}, ErrUnavailable
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return SellerStateReceiptV2{}, ErrUnavailable
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO seller_state_events_v2
 (event_id,fingerprint,representative_id,seller_id,actor_user_id,previous_enabled,new_enabled,previous_revision,new_revision,receipt_json,reason,created_unix)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, input.EventID, fp, input.RepresentativeID, input.SellerID, actorID, seller.Enabled, input.Enabled,
		seller.Revision, out.Seller.Revision, string(encoded), input.Reason, time.Now().UTC().Unix()); err != nil {
		return SellerStateReceiptV2{}, ErrUnavailable
	}
	if err = tx.Commit(); err != nil {
		return SellerStateReceiptV2{}, ErrUnavailable
	}
	return out, nil
}

func recordSellerActorV2(ctx context.Context, tx *sql.Tx, eventID, actorID string) error {
	if actorID == "" { // Existing local operator methods retain their contract.
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO seller_quota_actors_v2(event_id,actor_user_id) VALUES(?,?)`, eventID, actorID)
	return err
}

const sellerWithRevisionColumnsV2 = `s.id,s.representative_id,s.name,s.telegram_user_id,s.enabled,s.reserved_bytes,s.reserved_units,COALESCE(v.revision,0)`
const sellerWithRevisionJoinV2 = ` FROM representative_sellers_v2 s LEFT JOIN seller_state_revisions_v2 v
 ON v.representative_id=s.representative_id AND v.seller_id=s.id`

func sellerWithRevisionScanV2(row interface{ Scan(...any) error }) (SellerQuotaV2, error) {
	var seller SellerQuotaV2
	var enabled int
	err := row.Scan(&seller.ID, &seller.RepresentativeID, &seller.Name, &seller.TelegramUserID, &enabled,
		&seller.ReservedBytes, &seller.ReservedUnits, &seller.Revision)
	seller.Enabled = enabled == 1
	return seller, err
}

func (s *Store) SellerV2As(ctx context.Context, token, representativeID, sellerID string) (SellerQuotaV2, error) {
	if !idRE.MatchString(representativeID) || !idRE.MatchString(sellerID) {
		return SellerQuotaV2{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SellerQuotaV2{}, ErrUnavailable
	}
	defer tx.Rollback()
	if _, err = authorizeSellerAccessV2(ctx, tx, token, representativeID, true, false); err != nil {
		return SellerQuotaV2{}, err
	}
	_, seller, err := sellerCurrentV2(ctx, tx, representativeID, sellerID)
	return seller, err
}

func (s *Store) ListSellersV2As(ctx context.Context, token, representativeID string, limit, offset int) ([]SellerQuotaV2, error) {
	if !idRE.MatchString(representativeID) || limit < 1 || limit > 100 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()
	if _, err = authorizeSellerAccessV2(ctx, tx, token, representativeID, true, false); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+sellerWithRevisionColumnsV2+sellerWithRevisionJoinV2+
		" WHERE s.representative_id=? ORDER BY s.id LIMIT ? OFFSET ?", representativeID, limit, offset)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	list := make([]SellerQuotaV2, 0)
	for rows.Next() {
		seller, err := sellerWithRevisionScanV2(rows)
		if err != nil {
			return nil, ErrUnavailable
		}
		list = append(list, seller)
	}
	return list, rows.Err()
}
