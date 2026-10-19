package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// CatalogPolicy is independently owner-managed. Without an explicit policy
// a representative has ZERO catalog write or publish privileges.
type CatalogPolicy struct {
	RepresentativeID string `json:"representative_id"`
	ManageProducts   bool   `json:"manage_products"`
	CanPublish       bool   `json:"can_publish"`
	AllowVolume      bool   `json:"allow_volume"`
	AllowUnlimited   bool   `json:"allow_unlimited"`
	MinPriceToman    int64  `json:"min_price_toman"`
	MaxPriceToman    int64  `json:"max_price_toman"`
	MaxDurationDays  int64  `json:"max_duration_days"`
	MaxIPCount       int64  `json:"max_ip_count"`
	MaxVolumeBytes   int64  `json:"max_volume_bytes"`
	Revision         int64  `json:"revision"`
	UpdatedUnix      int64  `json:"updated_unix"`
}
type CatalogPolicyChange struct {
	CatalogPolicy
	EventID          string `json:"event_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
}
type CatalogPolicyReceipt struct {
	Policy          CatalogPolicy `json:"policy"`
	Replayed        bool          `json:"replayed"`
	SessionsRevoked bool          `json:"sessions_revoked"`
	CreditChanged   bool          `json:"credit_changed"`
	ClientsChanged  bool          `json:"clients_changed"`
	AppliedToXray   bool          `json:"applied_to_xray"`
}
type CatalogPolicyAudit struct {
	EventID          string        `json:"event_id"`
	RepresentativeID string        `json:"representative_id"`
	Previous         CatalogPolicy `json:"previous"`
	Current          CatalogPolicy `json:"current"`
	Reason           string        `json:"reason"`
	CreatedUnix      int64         `json:"created_unix"`
}

func ensureCatalogPolicy(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS catalog_rep_policies (
   representative_id TEXT PRIMARY KEY REFERENCES representatives(id),
   manage_products INTEGER NOT NULL CHECK(manage_products IN (0,1)),
   can_publish INTEGER NOT NULL CHECK(can_publish IN (0,1)),
   allow_volume INTEGER NOT NULL CHECK(allow_volume IN (0,1)),
   allow_unlimited INTEGER NOT NULL CHECK(allow_unlimited IN (0,1)),
   min_price_toman INTEGER NOT NULL CHECK(min_price_toman BETWEEN 1 AND 1000000000000),
   max_price_toman INTEGER NOT NULL CHECK(max_price_toman BETWEEN 1 AND 1000000000000),
   max_duration_days INTEGER NOT NULL CHECK(max_duration_days BETWEEN 1 AND 3650),
   max_ip_count INTEGER NOT NULL CHECK(max_ip_count BETWEEN 1 AND 1000),
   max_volume_bytes INTEGER NOT NULL CHECK(max_volume_bytes BETWEEN 1 AND 9000000000000000),
   revision INTEGER NOT NULL CHECK(revision>=1),
   updated_unix INTEGER NOT NULL
  )`,
		`CREATE TABLE IF NOT EXISTS catalog_policy_events (
   event_id TEXT PRIMARY KEY,
   fingerprint TEXT NOT NULL,
   representative_id TEXT NOT NULL REFERENCES representatives(id),
   previous_json TEXT NOT NULL,
   current_json TEXT NOT NULL,
   reason TEXT NOT NULL,
   created_unix INTEGER NOT NULL
  )`,
		`CREATE INDEX IF NOT EXISTS catalog_policy_events_rep_time
   ON catalog_policy_events(representative_id,created_unix DESC,event_id)`,
		`CREATE TRIGGER IF NOT EXISTS catalog_policy_events_immutable_update
   BEFORE UPDATE ON catalog_policy_events BEGIN SELECT RAISE(ABORT,'catalog policy audit is append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS catalog_policy_events_immutable_delete
   BEFORE DELETE ON catalog_policy_events BEGIN SELECT RAISE(ABORT,'catalog policy audit is append-only'); END`,
	} {
		if _, err = tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func defaultCatalogPolicy(id string) CatalogPolicy {
	return CatalogPolicy{
		RepresentativeID: id, ManageProducts: false, CanPublish: false,
		AllowVolume: true, AllowUnlimited: false,
		MinPriceToman: 1, MaxPriceToman: MaxCatalogPriceToman,
		MaxDurationDays: MaxDays, MaxIPCount: MaxIPLimit,
		MaxVolumeBytes: MaxCatalogVolume,
	}
}
func catalogPolicyTx(ctx context.Context, tx *sql.Tx, id string) (CatalogPolicy, error) {
	out := defaultCatalogPolicy(id)
	rep, err := readRep(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return CatalogPolicy{}, ErrNotFound
	}
	if err != nil {
		return CatalogPolicy{}, err
	}
	if rep.Kind != "representative" {
		return CatalogPolicy{}, ErrInvalid
	}
	var flags [4]int
	err = tx.QueryRowContext(ctx, `SELECT manage_products,can_publish,allow_volume,allow_unlimited,
  min_price_toman,max_price_toman,max_duration_days,max_ip_count,max_volume_bytes,revision,updated_unix
  FROM catalog_rep_policies WHERE representative_id=?`, id).Scan(
		&flags[0], &flags[1], &flags[2], &flags[3],
		&out.MinPriceToman, &out.MaxPriceToman, &out.MaxDurationDays, &out.MaxIPCount,
		&out.MaxVolumeBytes, &out.Revision, &out.UpdatedUnix)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return CatalogPolicy{}, err
	}
	out.ManageProducts = flags[0] == 1
	out.CanPublish = flags[1] == 1
	out.AllowVolume = flags[2] == 1
	out.AllowUnlimited = flags[3] == 1
	return out, nil
}
func (s *Store) CatalogPolicy(ctx context.Context, id string) (CatalogPolicy, error) {
	if !idRE.MatchString(id) || id == "owner" {
		return CatalogPolicy{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CatalogPolicy{}, err
	}
	defer tx.Rollback()
	out, err := catalogPolicyTx(ctx, tx, id)
	if err != nil {
		return CatalogPolicy{}, err
	}
	if err = tx.Commit(); err != nil {
		return CatalogPolicy{}, err
	}
	return out, nil
}
func validateCatalogPolicyChange(c CatalogPolicyChange) error {
	p := c.CatalogPolicy
	if !idRE.MatchString(p.RepresentativeID) || p.RepresentativeID == "owner" ||
		!eventRE.MatchString(c.EventID) || !validName(strings.TrimSpace(c.Reason), 200) ||
		c.ExpectedRevision < 0 || c.ExpectedRevision > 1_000_000_000 ||
		p.MinPriceToman < 1 || p.MinPriceToman > MaxCatalogPriceToman ||
		p.MaxPriceToman < p.MinPriceToman || p.MaxPriceToman > MaxCatalogPriceToman ||
		p.MaxDurationDays < 1 || p.MaxDurationDays > MaxDays ||
		p.MaxIPCount < 1 || p.MaxIPCount > MaxIPLimit ||
		p.MaxVolumeBytes < 1 || p.MaxVolumeBytes > MaxCatalogVolume ||
		(p.ManageProducts && !p.AllowVolume && !p.AllowUnlimited) ||
		(p.CanPublish && !p.ManageProducts) {
		return ErrInvalid
	}
	// Revision/UpdatedUnix are server-owned fields, never caller-controlled.
	if p.Revision != 0 || p.UpdatedUnix != 0 {
		return ErrInvalid
	}
	return nil
}
func policyPermitsProduct(p CatalogPolicy, spec CatalogSpec, publish bool) bool {
	return p.ManageProducts &&
		(!publish || p.CanPublish) &&
		spec.PriceToman >= p.MinPriceToman && spec.PriceToman <= p.MaxPriceToman &&
		spec.DurationDays >= 1 && spec.DurationDays <= p.MaxDurationDays &&
		spec.IPCount <= p.MaxIPCount &&
		((spec.PlanKind == "volume" && p.AllowVolume && spec.VolumeBytes <= p.MaxVolumeBytes) ||
			(spec.PlanKind == "unlimited" && p.AllowUnlimited && spec.VolumeBytes == 0))
}
func catalogPolicyNonceTx(ctx context.Context, tx *sql.Tx, id, fingerprint string) (bool, error) {
	var previous string
	err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM catalog_policy_events WHERE event_id=?`, id).Scan(&previous)
	if err == nil {
		if previous != fingerprint {
			return false, ErrConflict
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	for _, table := range []string{"events", "client_operations", "client_profile_edits",
		"representative_control_events", "catalog_events"} {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE event_id=?", id).Scan(&count); err != nil {
			return false, err
		}
		if count > 0 {
			return false, ErrConflict
		}
	}
	return false, nil
}

// UpdateCatalogPolicy changes ONLY the seller's catalog write authority.
// It revokes old sessions so every new browser session sees updated policy.
// No flags used by customer lease/traffic accounting are touched.
func (s *Store) UpdateCatalogPolicy(ctx context.Context, c CatalogPolicyChange) (CatalogPolicyReceipt, error) {
	var out CatalogPolicyReceipt
	c.Reason = strings.TrimSpace(c.Reason)
	if err := validateCatalogPolicyChange(c); err != nil {
		return out, err
	}
	fp := fingerprint("catalog-policy-v10b", c)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	replay, err := catalogPolicyNonceTx(ctx, tx, c.EventID, fp)
	if err != nil {
		return out, err
	}
	existing, err := catalogPolicyTx(ctx, tx, c.RepresentativeID)
	if err != nil {
		return out, err
	}
	if replay {
		if err = tx.Commit(); err != nil {
			return CatalogPolicyReceipt{}, err
		}
		return CatalogPolicyReceipt{Policy: existing, Replayed: true}, nil
	}
	if c.ExpectedRevision != existing.Revision {
		return out, ErrConflict
	}
	next := c.CatalogPolicy
	next.Revision = existing.Revision + 1
	next.UpdatedUnix = time.Now().UTC().Unix()
	next.RepresentativeID = existing.RepresentativeID
	if next.ManageProducts == existing.ManageProducts &&
		next.CanPublish == existing.CanPublish &&
		next.AllowVolume == existing.AllowVolume &&
		next.AllowUnlimited == existing.AllowUnlimited &&
		next.MinPriceToman == existing.MinPriceToman &&
		next.MaxPriceToman == existing.MaxPriceToman &&
		next.MaxDurationDays == existing.MaxDurationDays &&
		next.MaxIPCount == existing.MaxIPCount &&
		next.MaxVolumeBytes == existing.MaxVolumeBytes {
		return out, ErrInvalid
	}
	n := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO catalog_rep_policies(
  representative_id,manage_products,can_publish,allow_volume,allow_unlimited,
  min_price_toman,max_price_toman,max_duration_days,max_ip_count,max_volume_bytes,revision,updated_unix
  )VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
  ON CONFLICT(representative_id) DO UPDATE SET
  manage_products=excluded.manage_products,can_publish=excluded.can_publish,
  allow_volume=excluded.allow_volume,allow_unlimited=excluded.allow_unlimited,
  min_price_toman=excluded.min_price_toman,max_price_toman=excluded.max_price_toman,
  max_duration_days=excluded.max_duration_days,max_ip_count=excluded.max_ip_count,
  max_volume_bytes=excluded.max_volume_bytes,revision=excluded.revision,
  updated_unix=excluded.updated_unix`,
		next.RepresentativeID, n(next.ManageProducts), n(next.CanPublish), n(next.AllowVolume),
		n(next.AllowUnlimited), next.MinPriceToman, next.MaxPriceToman, next.MaxDurationDays,
		next.MaxIPCount, next.MaxVolumeBytes, next.Revision, next.UpdatedUnix)
	if err != nil {
		return out, err
	}
	// Do not resurrect sessions after owner revocation.
	if _, err = tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id IN
   (SELECT id FROM auth_users WHERE representative_id=? AND role='representative')`, c.RepresentativeID); err != nil {
		return out, err
	}
	priorJSON, err := json.Marshal(existing)
	if err != nil {
		return out, err
	}
	currentJSON, err := json.Marshal(next)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO catalog_policy_events(event_id,fingerprint,representative_id,previous_json,current_json,reason,created_unix)
  VALUES(?,?,?,?,?,?,?)`, c.EventID, fp, c.RepresentativeID, string(priorJSON), string(currentJSON), c.Reason, next.UpdatedUnix); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return CatalogPolicyReceipt{Policy: next, SessionsRevoked: true}, nil
}
func (s *Store) CatalogPolicyEvents(ctx context.Context, id string, limit, offset int) ([]CatalogPolicyAudit, error) {
	if !idRE.MatchString(id) || limit < 1 || limit > 50 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	if _, err := s.Representative(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,representative_id,previous_json,current_json,reason,created_unix
  FROM catalog_policy_events WHERE representative_id=? ORDER BY created_unix DESC,rowid DESC LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CatalogPolicyAudit, 0, limit)
	for rows.Next() {
		var a CatalogPolicyAudit
		var before, after string
		if err = rows.Scan(&a.EventID, &a.RepresentativeID, &before, &after, &a.Reason, &a.CreatedUnix); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(before), &a.Previous); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(after), &a.Current); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
