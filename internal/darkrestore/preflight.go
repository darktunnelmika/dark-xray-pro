// Package darkrestore performs only read-only, offline source inventory.
package darkrestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

const maxExpireUnix int64 = 253402300799

var ErrUnsafeSource = errors.New("a checkpointed standalone OFFLINE SQLite copy is required")
var ErrInspection = errors.New("offline Dark Restore snapshot inspection failed")

type Finding struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

// Only aggregate counts/bytes. No ID, token, email, URL, host, path
// or credential is ever serialized to a report.
type Report struct {
	Mode                  string    `json:"mode"`
	Status                string    `json:"status"`
	ImportEnabled         bool      `json:"import_enabled"`
	SourceSchemaVersion   int64     `json:"source_schema_version"`
	Subscriptions         int64     `json:"subscriptions"`
	Active                int64     `json:"active"`
	Archived              int64     `json:"archived"`
	Promoted              int64     `json:"promoted"`
	Expired               int64     `json:"expired"`
	UnlimitedVolume       int64     `json:"unlimited_volume"`
	LimitedVolume         int64     `json:"limited_volume"`
	Groups                int64     `json:"groups"`
	UsageScopes           int64     `json:"usage_scopes"`
	Events                int64     `json:"events"`
	Domains               int64     `json:"domains"`
	SafetyRows            int64     `json:"safety_rows"`
	GroupMappings         int64     `json:"group_mappings"`
	LegacyUploadBytes     string    `json:"legacy_upload_bytes"`
	LegacyDownloadBytes   string    `json:"legacy_download_bytes"`
	DarkUploadBytes       string    `json:"dark_upload_bytes"`
	DarkDownloadBytes     string    `json:"dark_download_bytes"`
	TotalConsumedBytes    string    `json:"total_consumed_bytes"`
	ArchivedConsumedBytes string    `json:"archived_consumed_bytes"`
	Findings              []Finding `json:"findings"`
}

func zeroReport() Report {
	return Report{Mode: "offline_read_only", Status: "blocked", ImportEnabled: false,
		LegacyUploadBytes: "0", LegacyDownloadBytes: "0", DarkUploadBytes: "0",
		DarkDownloadBytes: "0", TotalConsumedBytes: "0", ArchivedConsumedBytes: "0",
		Findings: []Finding{}}
}
func sortedFindings(m map[string]int64) []Finding {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]Finding, 0, len(keys))
	for _, k := range keys {
		out = append(out, Finding{Code: k, Count: m[k]})
	}
	return out
}
func addBytes(b *big.Int, n int64) {
	if n >= 0 {
		b.Add(b, big.NewInt(n))
	}
}
func sourceURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}).String()
}
func safeSource(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrUnsafeSource
	}
	clean := filepath.Clean(path)
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil || canonical != clean {
		return "", ErrUnsafeSource
	}
	st, err := os.Lstat(clean)
	if err != nil || !st.Mode().IsRegular() || st.Size() < 16 {
		return "", ErrUnsafeSource
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_, e := os.Lstat(clean + suffix)
		if e == nil || !errors.Is(e, os.ErrNotExist) {
			return "", ErrUnsafeSource
		}
	}
	file, err := os.Open(clean)
	if err != nil {
		return "", ErrUnsafeSource
	}
	defer file.Close()
	var magic [16]byte
	if _, err = io.ReadFull(file, magic[:]); err != nil || string(magic[:]) != "SQLite format 3\x00" {
		return "", ErrUnsafeSource
	}
	return clean, nil
}

// Required fields are sourced from the OLD main repository's dark_restore.py,
// restore_groups.py, restore_targets.py and restore_safety.py.
var requiredColumns = map[string][]string{
	"restore_subscriptions": {"id", "public_token", "legacy_url", "legacy_host",
		"legacy_path", "legacy_query", "core_email", "inbound_ids", "node_ids",
		"legacy_upload", "legacy_download", "legacy_total", "legacy_expire",
		"scan_status", "enabled", "first_seen", "last_seen", "group_id",
		"promoted_at", "promoted_owner", "deleted_at", "deleted_reason",
		"node_mode", "include_local"},
	"restore_groups":        {"id", "name", "name_key"},
	"restore_usage":         {"restore_id", "scope", "up", "down"},
	"restore_events":        {"restore_id", "event", "at"},
	"restore_domains":       {"domain"},
	"restore_safety":        {"restore_id", "metadata_state", "external_disabled", "expected_enable"},
	"restore_group_targets": {"group_id", "body"},
	"core_clients":          {"email"},
}

func checkSchema(ctx context.Context, tx *sql.Tx, issues map[string]int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		return false, err
	}
	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return false, err
		}
		present[name] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	if err = rows.Close(); err != nil {
		return false, err
	}
	names := make([]string, 0, len(requiredColumns))
	for name := range requiredColumns {
		names = append(names, name)
	}
	sort.Strings(names)
	usable := true
	for _, name := range names {
		if !present[name] {
			issues["missing_table_"+name]++
			usable = false
			continue
		}
		// Never interpolate source-supplied table or column names into SQL.
		columns, err := tx.QueryContext(ctx, "PRAGMA table_info("+name+")")
		if err != nil {
			return false, err
		}
		found := map[string]bool{}
		for columns.Next() {
			var index, notNull, primary int
			var column, columnType string
			var defaultValue any
			if err = columns.Scan(&index, &column, &columnType, &notNull, &defaultValue, &primary); err != nil {
				columns.Close()
				return false, err
			}
			found[column] = true
		}
		if err = columns.Err(); err != nil {
			columns.Close()
			return false, err
		}
		if err = columns.Close(); err != nil {
			return false, err
		}
		for _, col := range requiredColumns[name] {
			if !found[col] {
				issues["missing_column_"+name+"_"+col]++
				usable = false
			}
		}
	}
	return usable, nil
}
func tableCount(ctx context.Context, tx *sql.Tx, table string) (int64, error) {
	var n int64
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n)
	return n, err
}

type sourceRow struct {
	id                                                                    string
	legacyUp, legacyDown, legacyTotal, legacyExpire                       int64
	deleted, promoted, firstSeen, lastSeen                                float64
	enabled                                                               int64
	scanStatus                                                            string
	metadataState                                                         sql.NullString
	externalDisabled, expectedEnable                                      sql.NullInt64
	groupFound, safetyFound, coreFound                                    int64
	idLength, tokenLength, urlLength, hostLength, pathLength, emailLength int64
	scope                                                                 sql.NullString
	darkUp, darkDown                                                      sql.NullInt64
}

func scanRow(rows *sql.Rows) (sourceRow, error) {
	var r sourceRow
	err := rows.Scan(&r.id, &r.legacyUp, &r.legacyDown, &r.legacyTotal, &r.legacyExpire,
		&r.deleted, &r.promoted, &r.firstSeen, &r.lastSeen, &r.enabled, &r.scanStatus,
		&r.groupFound, &r.safetyFound, &r.coreFound,
		&r.idLength, &r.tokenLength, &r.urlLength, &r.hostLength, &r.pathLength, &r.emailLength,
		&r.metadataState, &r.externalDisabled, &r.expectedEnable,
		&r.scope, &r.darkUp, &r.darkDown)
	return r, err
}
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Analyze ONLY an independently checkpointed, local old-HUB SQLite copy.
// "ready_for_manual_review" is not permission to import, publish services,
// charge credit or re-use OLD Node/Bot credentials.
func Analyze(ctx context.Context, path string, asOf time.Time) (Report, error) {
	result := zeroReport()
	safe, err := safeSource(path)
	if err != nil {
		return result, err
	}
	db, err := sql.Open("sqlite", sourceURI(safe))
	if err != nil {
		return result, ErrInspection
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, ErrInspection
	}
	defer tx.Rollback()
	issues := map[string]int64{}
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.SourceSchemaVersion); err != nil {
		return result, ErrInspection
	}
	var check string
	if err = tx.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&check); err != nil || check != "ok" {
		issues["sqlite_quick_check_failed"]++
		result.Findings = sortedFindings(issues)
		return result, nil
	}
	usable, err := checkSchema(ctx, tx, issues)
	if err != nil {
		return result, ErrInspection
	}
	if !usable {
		result.Findings = sortedFindings(issues)
		return result, nil
	}
	tables := []struct {
		name string
		dest *int64
	}{
		{"restore_subscriptions", &result.Subscriptions},
		{"restore_groups", &result.Groups},
		{"restore_usage", &result.UsageScopes},
		{"restore_events", &result.Events},
		{"restore_domains", &result.Domains},
		{"restore_safety", &result.SafetyRows},
		{"restore_group_targets", &result.GroupMappings},
	}
	for _, table := range tables {
		*table.dest, err = tableCount(ctx, tx, table.name)
		if err != nil {
			return result, ErrInspection
		}
	}
	// LEFT JOIN preserves zero-usage subscriptions, including archived ones.
	// The SQLite cursor streams one row at a time; no secrets enter the report.
	rows, err := tx.QueryContext(ctx, `SELECT
  r.id,r.legacy_upload,r.legacy_download,r.legacy_total,r.legacy_expire,
  r.deleted_at,r.promoted_at,r.first_seen,r.last_seen,r.enabled,r.scan_status,
  CASE WHEN g.id IS NULL THEN 0 ELSE 1 END,
  CASE WHEN sf.restore_id IS NULL THEN 0 ELSE 1 END,
  CASE WHEN c.email IS NULL THEN 0 ELSE 1 END,
  length(r.id),length(r.public_token),length(r.legacy_url),length(r.legacy_host),
  length(r.legacy_path),length(r.core_email),
  sf.metadata_state,sf.external_disabled,sf.expected_enable,
  u.scope,u.up,u.down
  FROM restore_subscriptions r
  LEFT JOIN restore_groups g ON g.id=r.group_id
  LEFT JOIN restore_safety sf ON sf.restore_id=r.id
  LEFT JOIN core_clients c ON c.email=r.core_email
  LEFT JOIN restore_usage u ON u.restore_id=r.id
  ORDER BY r.id,u.scope`)
	if err != nil {
		return result, ErrInspection
	}
	var (
		previousID                                                               string
		havePrevious                                                             bool
		previous                                                                 sourceRow
		darkForClient                                                            big.Int
		legacyUpload, legacyDownload, darkUpload, darkDownload, archivedConsumed big.Int
		joinedUsageRows                                                          int64
		distinctClients                                                          int64
	)
	finish := func() {
		if !havePrevious {
			return
		}
		distinctClients++
		if previous.deleted > 0 {
			result.Archived++
		} else {
			result.Active++
		}
		if previous.promoted > 0 {
			result.Promoted++
		}
		if previous.legacyTotal == 0 {
			result.UnlimitedVolume++
		} else {
			result.LimitedVolume++
		}
		if previous.legacyExpire > 0 && previous.legacyExpire <= asOf.Unix() {
			result.Expired++
		}
		if previous.deleted > 0 {
			addBytes(&archivedConsumed, previous.legacyUp)
			addBytes(&archivedConsumed, previous.legacyDown)
			archivedConsumed.Add(&archivedConsumed, &darkForClient)
		}
		if previous.legacyUp < 0 || previous.legacyDown < 0 || previous.legacyTotal < 0 {
			issues["negative_legacy_counter"]++
		}
		if previous.legacyExpire < 0 || previous.legacyExpire > maxExpireUnix {
			issues["invalid_expiry_seconds"]++
		}
		if previous.deleted < 0 || previous.promoted < 0 || previous.firstSeen < 0 || previous.lastSeen < 0 ||
			(previous.firstSeen > 0 && previous.lastSeen > 0 && previous.lastSeen < previous.firstSeen) {
			issues["invalid_history_timestamp"]++
		}
		if previous.promoted > 0 && previous.deleted > 0 {
			issues["promoted_and_archived"]++
		}
		if previous.enabled != 0 && previous.enabled != 1 {
			issues["invalid_enabled_value"]++
		}
		if previous.idLength == 0 || previous.tokenLength == 0 ||
			previous.urlLength == 0 || previous.hostLength == 0 || previous.pathLength == 0 ||
			previous.emailLength == 0 {
			issues["missing_public_identity"]++
		}
		if previous.groupFound == 0 {
			issues["orphan_group_assignment"]++
		}
		if previous.safetyFound == 0 {
			issues["missing_safety_metadata"]++
		}
		if previous.coreFound == 0 && previous.deleted == 0 && previous.promoted == 0 {
			issues["missing_active_core_identity"]++
		}
		if previous.scanStatus != "verified" ||
			!previous.metadataState.Valid ||
			(previous.metadataState.String != "verified" && previous.metadataState.String != "manual") {
			issues["source_metadata_review_needed"]++
		}
		if previous.externalDisabled.Valid && previous.externalDisabled.Int64 != 0 && previous.externalDisabled.Int64 != 1 {
			issues["invalid_source_external_disabled"]++
		}
		if previous.expectedEnable.Valid && previous.expectedEnable.Int64 != 0 && previous.expectedEnable.Int64 != 1 {
			issues["invalid_source_expected_enable"]++
		}
	}
	for rows.Next() {
		r, e := scanRow(rows)
		if e != nil {
			rows.Close()
			return result, ErrInspection
		}
		if !havePrevious || previousID != r.id {
			finish()
			havePrevious = true
			previousID = r.id
			previous = r
			darkForClient.SetInt64(0)
			addBytes(&legacyUpload, r.legacyUp)
			addBytes(&legacyDownload, r.legacyDown)
		}
		if r.scope.Valid {
			joinedUsageRows++
			if r.scope.String == "" {
				issues["empty_usage_scope"]++
			}
			if !r.darkUp.Valid || !r.darkDown.Valid || r.darkUp.Int64 < 0 || r.darkDown.Int64 < 0 {
				issues["negative_or_null_dark_counter"]++
				continue
			}
			addBytes(&darkUpload, r.darkUp.Int64)
			addBytes(&darkDownload, r.darkDown.Int64)
			darkForClient.Add(&darkForClient, big.NewInt(r.darkUp.Int64))
			darkForClient.Add(&darkForClient, big.NewInt(r.darkDown.Int64))
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, ErrInspection
	}
	if err = rows.Close(); err != nil {
		return result, ErrInspection
	}
	finish()
	if distinctClients != result.Subscriptions {
		issues["subscription_row_mismatch"]++
	}
	if joinedUsageRows < result.UsageScopes {
		issues["orphan_restore_usage"] += result.UsageScopes - joinedUsageRows
	} else if joinedUsageRows > result.UsageScopes {
		issues["duplicate_usage_join"] += joinedUsageRows - result.UsageScopes
	}
	for _, probe := range []struct{ sql, code string }{
		{`SELECT COUNT(*) FROM restore_events e LEFT JOIN restore_subscriptions r
     ON r.id=e.restore_id WHERE r.id IS NULL`, "orphan_restore_event"},
		{`SELECT COUNT(*) FROM restore_safety sf LEFT JOIN restore_subscriptions r
     ON r.id=sf.restore_id WHERE r.id IS NULL`, "orphan_restore_safety"},
		{`SELECT COUNT(*) FROM restore_group_targets t LEFT JOIN restore_groups g
     ON g.id=t.group_id WHERE g.id IS NULL`, "orphan_group_mapping"},
	} {
		var n int64
		if err = tx.QueryRowContext(ctx, probe.sql).Scan(&n); err != nil {
			return result, ErrInspection
		}
		issues[probe.code] += n
	}
	if err = tx.Commit(); err != nil {
		return result, ErrInspection
	}
	result.LegacyUploadBytes = legacyUpload.String()
	result.LegacyDownloadBytes = legacyDownload.String()
	result.DarkUploadBytes = darkUpload.String()
	result.DarkDownloadBytes = darkDownload.String()
	result.ArchivedConsumedBytes = archivedConsumed.String()
	total := new(big.Int).Add(&legacyUpload, &legacyDownload)
	total.Add(total, &darkUpload)
	total.Add(total, &darkDownload)
	result.TotalConsumedBytes = total.String()
	result.Findings = sortedFindings(issues)
	if len(result.Findings) == 0 {
		result.Status = "ready_for_manual_review"
	}
	return result, nil
}
