package darkrestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ReviewVersion = 1
const MaxReviewClients = 50000

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// A private, read-only operator review. It contains names/UUIDs but NO tokens,
// subscription URLs, Xray keys, emails, hosts, DNS records, or raw JSON bodies.
// Do not serve this object via any public or representative endpoint.
type Review struct {
	Version         int            `json:"version"`
	Mode            string         `json:"mode"`
	SourceSHA256    string         `json:"source_sha256"`
	SourceSizeBytes int64          `json:"source_size_bytes"`
	AsOfUnix        int64          `json:"as_of_unix"`
	Status          string         `json:"status"`
	ImportEnabled   bool           `json:"import_enabled"`
	Summary         Report         `json:"summary"`
	Groups          []ReviewGroup  `json:"groups"`
	Clients         []ReviewClient `json:"clients"`
}
type ReviewGroup struct {
	SourceGroupID   string `json:"source_group_id"`
	Name            string `json:"name"`
	Count           int64  `json:"count"`
	Archived        int64  `json:"archived"`
	Promoted        int64  `json:"promoted"`
	HistoricalBytes string `json:"historical_bytes"`
}
type ReviewClient struct {
	SourceID        string   `json:"source_id"`
	SourceGroupID   string   `json:"source_group_id"`
	Label           string   `json:"label"`
	UUID            string   `json:"uuid,omitempty"`
	QuotaKind       string   `json:"quota_kind"`
	QuotaBytes      string   `json:"quota_bytes"`
	HistoricalBytes string   `json:"historical_bytes"`
	ExpiryUnix      int64    `json:"expiry_unix"`
	Status          string   `json:"status"`
	ReviewReasons   []string `json:"review_reasons"`
}

// The source SHA is an explicit commitment, never a direction to connect to
// the old HUB. Verify it twice: a snapshot cannot be replaced mid-inspection.
func hashSource(path string) (string, int64, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", 0, ErrUnsafeSource
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, (8<<30)+1))
	if e != nil || n > 8<<30 {
		return "", 0, ErrUnsafeSource
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func parseCoreUUID(body sql.NullString) string {
	if !body.Valid || len(body.String) == 0 || len(body.String) > 65536 {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body.String), &obj) != nil {
		return ""
	}
	var id string
	if json.Unmarshal(obj["id"], &id) != nil {
		return ""
	}
	id = strings.ToLower(id)
	if uuidPattern.MatchString(id) {
		return id
	}
	return ""
}
func safeLabel(label string) bool {
	if strings.TrimSpace(label) == "" || len([]rune(label)) > 128 || len(label) > 1024 {
		return false
	}
	for _, r := range label {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func readReviewGroups(ctx context.Context, tx *sql.Tx) ([]ReviewGroup, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,name FROM restore_groups ORDER BY id`)
	if e != nil {
		return nil, ErrInspection
	}
	defer rows.Close()
	out := []ReviewGroup{}
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) != nil || !safeLabel(name) || !safeLabel(id) || len(out) >= MaxReviewClients {
			return nil, ErrInspection
		}
		out = append(out, ReviewGroup{SourceGroupID: id, Name: name, HistoricalBytes: "0"})
	}
	if rows.Err() != nil {
		return nil, ErrInspection
	}
	return out, nil
}
func totalHistorical(ctx context.Context, tx *sql.Tx, id string, legacyUp, legacyDown int64) (*big.Int, error) {
	if legacyUp < 0 || legacyDown < 0 {
		return nil, ErrInspection
	}
	total := new(big.Int).Add(big.NewInt(legacyUp), big.NewInt(legacyDown))
	rows, e := tx.QueryContext(ctx, `SELECT up,down FROM restore_usage WHERE restore_id=? ORDER BY scope`, id)
	if e != nil {
		return nil, ErrInspection
	}
	defer rows.Close()
	for rows.Next() {
		var up, down int64
		if rows.Scan(&up, &down) != nil || up < 0 || down < 0 {
			return nil, ErrInspection
		}
		total.Add(total, big.NewInt(up))
		total.Add(total, big.NewInt(down))
	}
	if rows.Err() != nil {
		return nil, ErrInspection
	}
	return total, nil
}

// BuildReview refuses drift, off-host data, partial metadata and unsafe SQLite.
// Only a checkpointed independent copy of the pinned old-HUB schema is read.
// It NEVER writes to the source, a PRO database, a Node or a billing ledger.
func BuildReview(ctx context.Context, path, expectedSHA string, asOf time.Time) (Review, error) {
	result := Review{Version: ReviewVersion, Mode: "offline_operator_review_only", Status: "blocked",
		Groups: []ReviewGroup{}, Clients: []ReviewClient{}, AsOfUnix: asOf.Unix()}
	if ctx == nil || ctx.Err() != nil || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(expectedSHA) || asOf.Unix() <= 0 {
		return result, ErrUnsafeSource
	}
	safe, e := safeSource(path)
	if e != nil {
		return result, e
	}
	before, e := os.Lstat(safe)
	if e != nil || before.Mode().Perm()&0022 != 0 {
		return result, ErrUnsafeSource
	}
	sha, size, e := hashSource(safe)
	if e != nil || sha != expectedSHA {
		return result, ErrUnsafeSource
	}
	result.SourceSHA256, result.SourceSizeBytes = sha, size
	report, e := Analyze(ctx, safe, asOf)
	if e != nil {
		return result, e
	}
	result.Summary = report
	if report.Status != "ready_for_manual_review" {
		return result, nil
	}
	if report.Subscriptions > MaxReviewClients {
		result.Status = "blocked"
		result.Summary.Findings = append(result.Summary.Findings, Finding{Code: "preview_capacity_exceeded_no_partial_import", Count: report.Subscriptions})
		return result, nil
	}
	db, e := sql.Open("sqlite", sourceURI(safe))
	if e != nil {
		return result, ErrInspection
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return result, ErrInspection
	}
	defer tx.Rollback()
	groups, e := readReviewGroups(ctx, tx)
	if e != nil {
		return result, e
	}
	groupIndex := map[string]int{}
	groupTotals := make([]*big.Int, len(groups))
	for i := range groups {
		groupIndex[groups[i].SourceGroupID] = i
		groupTotals[i] = big.NewInt(0)
	}
	// Core body may be absent on migrated/archived records; never invent UUID.
	// The read uses a fixed table/column allowlist verified from sqlite_master.
	var bodyExists int
	columns, e := tx.QueryContext(ctx, `PRAGMA table_info(core_clients)`)
	if e != nil {
		return result, ErrInspection
	}
	for columns.Next() {
		var n, t string
		var ord, notNull, pk int
		var def any
		if columns.Scan(&ord, &n, &t, &notNull, &def, &pk) != nil {
			columns.Close()
			return result, ErrInspection
		}
		if n == "body" {
			bodyExists++
		}
	}
	if columns.Err() != nil {
		columns.Close()
		return result, ErrInspection
	}
	columns.Close()
	bodyExpr := "NULL"
	if bodyExists == 1 {
		bodyExpr = "c.body"
	}
	// No secrets leave this query: only historical source IDs, group metadata,
	// usage, expiry and a parsed UUID; all original URL/token/email fields excluded.
	rows, e := tx.QueryContext(ctx, `SELECT r.id,r.group_id,r.legacy_upload,r.legacy_download,
 r.legacy_total,r.legacy_expire,r.deleted_at,r.promoted_at,r.enabled,
 r.scan_status,sf.metadata_state,sf.external_disabled,sf.expected_enable,`+bodyExpr+`
 FROM restore_subscriptions r LEFT JOIN restore_safety sf ON sf.restore_id=r.id
 LEFT JOIN core_clients c ON c.email=r.core_email ORDER BY r.id`)
	if e != nil {
		return result, ErrInspection
	}
	out := []ReviewClient{}
	seenUUID := map[string]bool{}
	for rows.Next() {
		var id, grp, scan string
		var up, down, quota, expiry, enabled int64
		var deleted, promoted float64
		var meta sql.NullString
		var external, expected sql.NullInt64
		var body sql.NullString
		if rows.Scan(&id, &grp, &up, &down, &quota, &expiry, &deleted, &promoted, &enabled, &scan, &meta, &external, &expected, &body) != nil {
			rows.Close()
			return result, ErrInspection
		}
		gi, ok := groupIndex[grp]
		if !ok || !safeLabel(id) || quota < 0 || expiry < 0 || expiry > maxExpireUnix {
			rows.Close()
			return result, ErrInspection
		}
		amount, e := totalHistorical(ctx, tx, id, up, down)
		if e != nil {
			rows.Close()
			return result, e
		}
		groupTotals[gi].Add(groupTotals[gi], amount)
		g := &groups[gi]
		g.Count++
		reasons := []string{}
		status := "requires_manual_review"
		if deleted > 0 {
			status = "archive_history_only"
			g.Archived++
		}
		if promoted > 0 {
			status = "skip_already_promoted"
			g.Promoted++
		}
		if enabled != 1 || external.Valid && external.Int64 == 1 || expected.Valid && expected.Int64 == 0 {
			reasons = append(reasons, "source_disabled_do_not_reactivate")
		}
		if !meta.Valid || meta.String != "verified" || scan != "verified" {
			reasons = append(reasons, "source_verification_not_proven")
		}
		uuid := parseCoreUUID(body)
		if uuid == "" {
			reasons = append(reasons, "uuid_not_verified")
		}
		if uuid != "" && seenUUID[uuid] {
			reasons = append(reasons, "duplicate_core_uuid")
		}
		if uuid != "" {
			seenUUID[uuid] = true
		}
		if quota == 0 {
			reasons = append(reasons, "legacy_unlimited_requires_plan_policy")
		}
		if expiry == 0 {
			reasons = append(reasons, "no_finite_expiry")
		}
		if expiry > 0 && expiry <= asOf.Unix() {
			reasons = append(reasons, "expired_requires_explicit_renewal")
		}
		if deleted > 0 {
			reasons = append(reasons, "archived_history_must_not_be_recreated_active")
		}
		if promoted > 0 {
			reasons = append(reasons, "promoted_identity_must_not_be_imported_twice")
		}
		if amount.Cmp(big.NewInt(math.MaxInt64)) > 0 {
			reasons = append(reasons, "historical_counter_exceeds_signed_64bit")
		}
		sort.Strings(reasons)
		out = append(out, ReviewClient{SourceID: id, SourceGroupID: grp, Label: id, UUID: uuid,
			QuotaKind: func() string {
				if quota == 0 {
					return "legacy_unlimited"
				}
				return "volume"
			}(),
			QuotaBytes: big.NewInt(quota).String(), HistoricalBytes: amount.String(), ExpiryUnix: expiry, Status: status,
			ReviewReasons: reasons})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, ErrInspection
	}
	if len(out) != int(report.Subscriptions) {
		return result, ErrInspection
	}
	for i := range groups {
		groups[i].HistoricalBytes = groupTotals[i].String()
	}
	if e = tx.Commit(); e != nil {
		return result, ErrInspection
	}
	after, e := os.Lstat(safe)
	if e != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return result, ErrUnsafeSource
	}
	digest, afterSize, e := hashSource(safe)
	if e != nil || digest != sha || afterSize != size {
		return result, ErrUnsafeSource
	}
	result.Groups, result.Clients = groups, out
	result.Status = "ready_for_review"
	result.ImportEnabled = false
	return result, nil
}

// ReadPrivateReview refuses arbitrary structure or status claims. The private
// plan is not an executable import command and cannot authorize a PRO mutation.
func ReadPrivateReview(raw []byte) (Review, error) {
	var p Review
	if len(raw) == 0 || len(raw) > 100<<20 || json.Unmarshal(raw, &p) != nil {
		return p, ErrInspection
	}
	if p.Version != 1 || p.Mode != "offline_operator_review_only" || p.ImportEnabled ||
		(p.Status != "ready_for_review" && p.Status != "blocked") || len(p.Clients) > MaxReviewClients ||
		len(p.SourceSHA256) != 64 || p.SourceSizeBytes < 16 || len(p.Groups) > MaxReviewClients {
		return Review{}, ErrInspection
	}
	return p, nil
}
