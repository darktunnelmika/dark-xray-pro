package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// V10a is deliberately a CATALOG, not a checkout or payment processor.
// No operation here writes to clients, credits, Node/Agent, Xray or usage.
const MaxCatalogPriceToman int64 = 1_000_000_000_000
const MaxCatalogVolume int64 = 9_000_000_000_000_000 // browser-safe integer

type CatalogSpec struct {
	ID               string `json:"id"`
	RepresentativeID string `json:"representative_id"`
	Name             string `json:"name"`
	PlanKind         string `json:"plan_kind"`
	DurationDays     int64  `json:"duration_days"`
	IPCount          int64  `json:"ip_count"`
	VolumeBytes      int64  `json:"volume_bytes"`
	PriceToman       int64  `json:"price_toman"`
}
type CatalogProduct struct {
	CatalogSpec
	Status      string `json:"status"`
	Revision    int64  `json:"revision"`
	CreatedUnix int64  `json:"created_unix"`
	UpdatedUnix int64  `json:"updated_unix"`
}
type CatalogCreate struct {
	CatalogSpec
	EventID string `json:"event_id"`
	Reason  string `json:"reason"`
}
type CatalogUpdate struct {
	CatalogSpec
	EventID          string `json:"event_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
}
type CatalogAudit struct {
	EventID          string `json:"event_id"`
	ProductID        string `json:"product_id"`
	RepresentativeID string `json:"representative_id"`
	Action           string `json:"action"`
	BeforeJSON       string `json:"before_json"`
	AfterJSON        string `json:"after_json"`
	Reason           string `json:"reason"`
	CreatedUnix      int64  `json:"created_unix"`
}
type CatalogSearch struct {
	RepresentativeID string
	State            string
	Query            string
	Limit            int
	Offset           int
}
type CatalogPage struct {
	Products         []CatalogProduct `json:"products"`
	Total            int64            `json:"total"`
	Limit            int              `json:"limit"`
	Offset           int              `json:"offset"`
	HasMore          bool             `json:"has_more"`
	Query            string           `json:"q"`
	State            string           `json:"state"`
	RepresentativeID string           `json:"representative_id"`
}

func ensureCatalog(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS catalog_products (
   id TEXT PRIMARY KEY,
   representative_id TEXT NOT NULL REFERENCES representatives(id),
   display_name TEXT NOT NULL,
   plan_kind TEXT NOT NULL CHECK(plan_kind IN ('volume','unlimited')),
   duration_days INTEGER NOT NULL CHECK(duration_days BETWEEN 1 AND 3650),
   ip_count INTEGER NOT NULL CHECK(ip_count BETWEEN 0 AND 1000),
   volume_bytes INTEGER NOT NULL CHECK(volume_bytes>=0 AND volume_bytes<=9000000000000000),
   price_toman INTEGER NOT NULL CHECK(price_toman>=0 AND price_toman<=1000000000000),
   status TEXT NOT NULL CHECK(status IN ('draft','published','archived')),
   revision INTEGER NOT NULL CHECK(revision>=1),
   created_unix INTEGER NOT NULL,
   updated_unix INTEGER NOT NULL
  )`,
		`CREATE INDEX IF NOT EXISTS catalog_products_scope_status
    ON catalog_products(representative_id,status,created_unix DESC,id)`,
		`CREATE TABLE IF NOT EXISTS catalog_events (
    event_id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    product_id TEXT NOT NULL REFERENCES catalog_products(id),
    representative_id TEXT NOT NULL REFERENCES representatives(id),
    action TEXT NOT NULL CHECK(action IN ('create','update')),
    before_json TEXT NOT NULL, after_json TEXT NOT NULL,
    reason TEXT NOT NULL, created_unix INTEGER NOT NULL
  )`,
		`CREATE INDEX IF NOT EXISTS catalog_events_product
    ON catalog_events(product_id,created_unix DESC,event_id)`,
		`CREATE TRIGGER IF NOT EXISTS catalog_events_immutable_update
    BEFORE UPDATE ON catalog_events BEGIN SELECT RAISE(ABORT,'catalog audit is append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS catalog_events_immutable_delete
    BEFORE DELETE ON catalog_events BEGIN SELECT RAISE(ABORT,'catalog audit is append-only'); END`,
	} {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func validateCatalogSpec(s CatalogSpec) (CatalogSpec, error) {
	s.Name = strings.TrimSpace(s.Name)
	if !idRE.MatchString(s.ID) || !idRE.MatchString(s.RepresentativeID) ||
		s.RepresentativeID == "owner" || !validName(s.Name, 128) ||
		s.DurationDays < 1 || s.DurationDays > MaxDays ||
		s.IPCount < 0 || s.IPCount > MaxIPLimit ||
		s.PriceToman < 0 || s.PriceToman > MaxCatalogPriceToman ||
		s.VolumeBytes < 0 || s.VolumeBytes > MaxCatalogVolume {
		return CatalogSpec{}, ErrInvalid
	}
	switch s.PlanKind {
	case "volume":
		if s.VolumeBytes <= 0 {
			return CatalogSpec{}, ErrInvalid
		}
	case "unlimited":
		if s.IPCount <= 0 || s.VolumeBytes != 0 {
			return CatalogSpec{}, ErrInvalid
		}
	default:
		return CatalogSpec{}, ErrInvalid
	}
	return s, nil
}
func catalogScan(row interface{ Scan(...any) error }) (CatalogProduct, error) {
	var c CatalogProduct
	err := row.Scan(&c.ID, &c.RepresentativeID, &c.Name, &c.PlanKind,
		&c.DurationDays, &c.IPCount, &c.VolumeBytes, &c.PriceToman,
		&c.Status, &c.Revision, &c.CreatedUnix, &c.UpdatedUnix)
	return c, err
}

const catalogColumns = "id,representative_id,display_name,plan_kind,duration_days,ip_count,volume_bytes,price_toman,status,revision,created_unix,updated_unix"

func readCatalog(ctx context.Context, tx *sql.Tx, id string) (CatalogProduct, error) {
	return catalogScan(tx.QueryRowContext(ctx, "SELECT "+catalogColumns+" FROM catalog_products WHERE id=?", id))
}
func catalogEventReplayed(ctx context.Context, tx *sql.Tx, id, fp, action string) (bool, error) {
	var existingFP, existingAction string
	err := tx.QueryRowContext(ctx, "SELECT fingerprint,action FROM catalog_events WHERE event_id=?", id).Scan(&existingFP, &existingAction)
	if err == nil {
		if existingFP == fp && existingAction == action {
			return true, nil
		}
		return false, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	for _, name := range []string{"events", "client_operations", "client_profile_edits", "representative_control_events", "catalog_policy_events"} {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name+" WHERE event_id=?", id).Scan(&count); err != nil {
			return false, err
		}
		if count != 0 {
			return false, ErrConflict
		}
	}
	return false, nil
}
func (s *Store) CreateCatalogProduct(ctx context.Context, req CatalogCreate) (CatalogProduct, error) {
	return s.createCatalogProduct(ctx, req, "")
}
func (s *Store) CreateCatalogProductScoped(ctx context.Context, req CatalogCreate, scope string) (CatalogProduct, error) {
	if !idRE.MatchString(scope) || scope == "owner" {
		return CatalogProduct{}, ErrInvalid
	}
	return s.createCatalogProduct(ctx, req, scope)
}
func (s *Store) createCatalogProduct(ctx context.Context, req CatalogCreate, scope string) (CatalogProduct, error) {
	var out CatalogProduct
	spec, err := validateCatalogSpec(req.CatalogSpec)
	if err != nil || !eventRE.MatchString(req.EventID) || !validName(req.Reason, 200) {
		return out, ErrInvalid
	}
	req.CatalogSpec = spec
	req.Reason = strings.TrimSpace(req.Reason)
	if scope != "" && spec.RepresentativeID != scope {
		return out, ErrConflict
	}
	fp := fingerprint("catalog-v10-create", req)
	if scope != "" {
		fp = fingerprint("catalog-v10b-seller-create", struct {
			Scope   string
			Request CatalogCreate
		}{scope, req})
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if scope != "" {
		pol, e := catalogPolicyTx(ctx, tx, scope)
		if e != nil {
			return out, e
		}
		rep, e := readRep(ctx, tx, scope)
		if e != nil {
			return out, e
		}
		if rep.Kind != "representative" || !rep.Enabled || !policyPermitsProduct(pol, spec, false) {
			return out, ErrConflict
		}
	}
	replay, err := catalogEventReplayed(ctx, tx, req.EventID, fp, "create")
	if err != nil {
		return out, err
	}
	if replay {
		out, err = readCatalog(ctx, tx, spec.ID)
		if err != nil {
			return CatalogProduct{}, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return CatalogProduct{}, err
		}
		return out, nil
	}
	rep, err := readRep(ctx, tx, spec.RepresentativeID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if rep.Kind != "representative" {
		return out, ErrConflict
	}
	if _, err = readCatalog(ctx, tx, spec.ID); err == nil {
		return out, ErrConflict
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	now := time.Now().UTC().Unix()
	out = CatalogProduct{CatalogSpec: spec, Status: "draft", Revision: 1, CreatedUnix: now, UpdatedUnix: now}
	if _, err = tx.ExecContext(ctx, "INSERT INTO catalog_products("+catalogColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
		spec.ID, spec.RepresentativeID, spec.Name, spec.PlanKind, spec.DurationDays, spec.IPCount,
		spec.VolumeBytes, spec.PriceToman, out.Status, out.Revision, now, now); err != nil {
		return CatalogProduct{}, err
	}
	afterJSON, err := json.Marshal(out)
	if err != nil {
		return CatalogProduct{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO catalog_events(event_id,fingerprint,product_id,representative_id,action,before_json,after_json,reason,created_unix) VALUES(?,?,?,?,'create','',?,?,?)",
		req.EventID, fp, spec.ID, spec.RepresentativeID, string(afterJSON), req.Reason, now); err != nil {
		return CatalogProduct{}, err
	}
	if err = tx.Commit(); err != nil {
		return CatalogProduct{}, err
	}
	return out, nil
}
func (s *Store) UpdateCatalogProduct(ctx context.Context, req CatalogUpdate) (CatalogProduct, error) {
	return s.updateCatalogProduct(ctx, req, "")
}
func (s *Store) UpdateCatalogProductScoped(ctx context.Context, req CatalogUpdate, scope string) (CatalogProduct, error) {
	if !idRE.MatchString(scope) || scope == "owner" {
		return CatalogProduct{}, ErrInvalid
	}
	return s.updateCatalogProduct(ctx, req, scope)
}
func (s *Store) updateCatalogProduct(ctx context.Context, req CatalogUpdate, scope string) (CatalogProduct, error) {
	var out CatalogProduct
	spec, err := validateCatalogSpec(req.CatalogSpec)
	if err != nil || !eventRE.MatchString(req.EventID) || !validName(req.Reason, 200) ||
		req.ExpectedRevision < 1 || req.ExpectedRevision > 1_000_000_000 ||
		(req.Status != "draft" && req.Status != "published" && req.Status != "archived") {
		return out, ErrInvalid
	}
	if req.Status == "published" && spec.PriceToman == 0 {
		return out, ErrInvalid
	}
	req.CatalogSpec = spec
	req.Reason = strings.TrimSpace(req.Reason)
	if scope != "" && spec.RepresentativeID != scope {
		return out, ErrConflict
	}
	fp := fingerprint("catalog-v10-update", req)
	if scope != "" {
		fp = fingerprint("catalog-v10b-seller-update", struct {
			Scope   string
			Request CatalogUpdate
		}{scope, req})
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if scope != "" {
		pol, e := catalogPolicyTx(ctx, tx, scope)
		if e != nil {
			return out, e
		}
		rep, e := readRep(ctx, tx, scope)
		if e != nil {
			return out, e
		}
		if rep.Kind != "representative" || !rep.Enabled || !pol.ManageProducts {
			return out, ErrConflict
		}
		if req.Status != "archived" && !policyPermitsProduct(pol, spec, req.Status == "published") {
			return out, ErrConflict
		}
	}
	replay, err := catalogEventReplayed(ctx, tx, req.EventID, fp, "update")
	if err != nil {
		return out, err
	}
	before, err := readCatalog(ctx, tx, spec.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if replay {
		if err = tx.Commit(); err != nil {
			return CatalogProduct{}, err
		}
		return before, nil
	}
	if before.Revision != req.ExpectedRevision || before.Status == "archived" ||
		before.RepresentativeID != spec.RepresentativeID {
		return out, ErrConflict
	}
	// Revocation of a published product is safe even if its original price
	// is now beneath a newly raised floor: archive WITHOUT changing terms.
	if scope != "" && req.Status == "archived" && before.CatalogSpec != spec {
		return out, ErrConflict
	}
	if before.Status == req.Status && before.CatalogSpec == spec {
		return out, ErrInvalid
	}
	rep, err := readRep(ctx, tx, spec.RepresentativeID)
	if err != nil {
		return out, err
	}
	if rep.Kind != "representative" || (req.Status == "published" && !rep.Enabled) {
		return out, ErrConflict
	}
	now := time.Now().UTC().Unix()
	out = CatalogProduct{CatalogSpec: spec, Status: req.Status, Revision: before.Revision + 1,
		CreatedUnix: before.CreatedUnix, UpdatedUnix: now}
	if _, err = tx.ExecContext(ctx, "UPDATE catalog_products SET display_name=?,plan_kind=?,duration_days=?,ip_count=?,volume_bytes=?,price_toman=?,status=?,revision=?,updated_unix=? WHERE id=? AND revision=?",
		spec.Name, spec.PlanKind, spec.DurationDays, spec.IPCount, spec.VolumeBytes, spec.PriceToman,
		out.Status, out.Revision, now, spec.ID, before.Revision); err != nil {
		return CatalogProduct{}, err
	}
	// Store stable audit fingerprints only. No credit/order or source secrets.
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return CatalogProduct{}, err
	}
	afterJSON, err := json.Marshal(out)
	if err != nil {
		return CatalogProduct{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO catalog_events(event_id,fingerprint,product_id,representative_id,action,before_json,after_json,reason,created_unix) VALUES(?,?,?,?,'update',?,?,?,?)",
		req.EventID, fp, spec.ID, spec.RepresentativeID, string(beforeJSON), string(afterJSON), req.Reason, now); err != nil {
		return CatalogProduct{}, err
	}
	if err = tx.Commit(); err != nil {
		return CatalogProduct{}, err
	}
	return out, nil
}
func (s *Store) CatalogProduct(ctx context.Context, id, scope string) (CatalogProduct, error) {
	var out CatalogProduct
	if !idRE.MatchString(id) || (scope != "" && !idRE.MatchString(scope)) {
		return out, ErrInvalid
	}
	out, err := catalogScan(s.db.QueryRowContext(ctx, "SELECT "+catalogColumns+" FROM catalog_products WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if scope != "" && (out.RepresentativeID != scope || out.Status != "published") {
		return CatalogProduct{}, ErrConflict
	}
	return out, nil
}
func (s *Store) SearchCatalog(ctx context.Context, q CatalogSearch, scope string) (CatalogPage, error) {
	return s.searchCatalog(ctx, q, scope, false)
}

// A second authenticated read route exposes own drafts ONLY to a rep with
// an explicit catalog management policy; normal storefront stays published.
func (s *Store) SearchCatalogManaged(ctx context.Context, q CatalogSearch, scope string) (CatalogPage, error) {
	if scope == "" || scope == "owner" || !idRE.MatchString(scope) {
		return CatalogPage{}, ErrInvalid
	}
	return s.searchCatalog(ctx, q, scope, true)
}
func (s *Store) searchCatalog(ctx context.Context, q CatalogSearch, scope string, manage bool) (CatalogPage, error) {
	var out CatalogPage
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 1_000_000 ||
		(q.RepresentativeID != "" && !idRE.MatchString(q.RepresentativeID)) ||
		(scope != "" && !idRE.MatchString(scope)) || !utf8.ValidString(q.Query) ||
		!validCatalogQuery(q.Query) || len([]rune(q.Query)) > 128 {
		return out, ErrInvalid
	}
	switch q.State {
	case "", "all", "draft", "published", "archived":
	default:
		return out, ErrInvalid
	}
	if scope != "" {
		if q.RepresentativeID != "" && q.RepresentativeID != scope {
			return out, ErrConflict
		}
		if !manage && q.State != "" && q.State != "published" && q.State != "all" {
			return out, ErrConflict
		}
		q.RepresentativeID = scope
		if !manage {
			q.State = "published"
		}
	}
	q.Query = strings.TrimSpace(q.Query)
	args := []any{}
	filters := []string{}
	if q.RepresentativeID != "" {
		filters = append(filters, "representative_id=?")
		args = append(args, q.RepresentativeID)
	}
	if q.State != "" && q.State != "all" {
		filters = append(filters, "status=?")
		args = append(args, q.State)
	}
	if q.Query != "" {
		filters = append(filters, "(instr(lower(id),lower(?))>0 OR instr(lower(display_name),lower(?))>0)")
		args = append(args, q.Query, q.Query)
	}
	where := ""
	if len(filters) > 0 {
		where = " WHERE " + strings.Join(filters, " AND ")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if manage {
		pol, e := catalogPolicyTx(ctx, tx, scope)
		if e != nil {
			return CatalogPage{}, e
		}
		rep, e := readRep(ctx, tx, scope)
		if e != nil {
			return CatalogPage{}, e
		}
		if !pol.ManageProducts || !rep.Enabled {
			return CatalogPage{}, ErrConflict
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_products"+where, args...).Scan(&out.Total); err != nil {
		return CatalogPage{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+catalogColumns+" FROM catalog_products"+where+
		" ORDER BY created_unix DESC,id ASC LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
	if err != nil {
		return CatalogPage{}, err
	}
	out.Products = make([]CatalogProduct, 0, q.Limit)
	for rows.Next() {
		c, e := catalogScan(rows)
		if e != nil {
			rows.Close()
			return CatalogPage{}, e
		}
		out.Products = append(out.Products, c)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return CatalogPage{}, err
	}
	if err = rows.Close(); err != nil {
		return CatalogPage{}, err
	}
	if err = tx.Commit(); err != nil {
		return CatalogPage{}, err
	}
	out.Limit = q.Limit
	out.Offset = q.Offset
	out.HasMore = int64(q.Offset+len(out.Products)) < out.Total
	out.Query = q.Query
	out.State = q.State
	out.RepresentativeID = q.RepresentativeID
	return out, nil
}
func validCatalogQuery(q string) bool {
	for _, r := range q {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func (s *Store) CatalogAudit(ctx context.Context, id string, limit, offset int) ([]CatalogAudit, error) {
	if !idRE.MatchString(id) || limit < 1 || limit > 100 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	c, err := s.CatalogProduct(ctx, id, "")
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT event_id,product_id,representative_id,action,before_json,after_json,reason,created_unix FROM catalog_events WHERE product_id=? ORDER BY created_unix DESC,rowid DESC LIMIT ? OFFSET ?", c.ID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CatalogAudit, 0)
	for rows.Next() {
		var e CatalogAudit
		if err = rows.Scan(&e.EventID, &e.ProductID, &e.RepresentativeID, &e.Action, &e.BeforeJSON, &e.AfterJSON, &e.Reason, &e.CreatedUnix); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
