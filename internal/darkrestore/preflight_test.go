package darkrestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureDDL = `
 CREATE TABLE restore_subscriptions(
  id TEXT PRIMARY KEY,public_token TEXT UNIQUE NOT NULL,legacy_url TEXT NOT NULL,
  legacy_host TEXT NOT NULL,legacy_path TEXT NOT NULL,legacy_query TEXT NOT NULL,
  core_email TEXT NOT NULL,inbound_ids TEXT NOT NULL,node_ids TEXT NOT NULL,
  legacy_upload INTEGER NOT NULL,legacy_download INTEGER NOT NULL,
  legacy_total INTEGER NOT NULL,legacy_expire INTEGER NOT NULL,
  scan_status TEXT NOT NULL,enabled INTEGER NOT NULL,
  first_seen REAL NOT NULL,last_seen REAL NOT NULL,
  group_id TEXT NOT NULL,promoted_at REAL NOT NULL,promoted_owner TEXT NOT NULL,
  deleted_at REAL NOT NULL,deleted_reason TEXT NOT NULL,
  node_mode TEXT NOT NULL,include_local INTEGER NOT NULL
 );
 CREATE TABLE restore_groups(id TEXT PRIMARY KEY,name TEXT,name_key TEXT);
 CREATE TABLE restore_usage(restore_id TEXT,scope TEXT,up INTEGER,down INTEGER,PRIMARY KEY(restore_id,scope));
 CREATE TABLE restore_events(restore_id TEXT,event TEXT,at REAL);
 CREATE TABLE restore_domains(domain TEXT);
 CREATE TABLE restore_safety(restore_id TEXT PRIMARY KEY,metadata_state TEXT,external_disabled INTEGER NOT NULL DEFAULT 0,expected_enable INTEGER NOT NULL DEFAULT 1);
 CREATE TABLE restore_group_targets(group_id TEXT PRIMARY KEY,body TEXT);
 CREATE TABLE core_clients(email TEXT PRIMARY KEY);
`

func sourceFixture(t *testing.T, records int) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "authorized_offline_copy.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(fixtureDDL); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO restore_groups VALUES('grp_one','Synthetic Group','synthetic_group')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO restore_group_targets VALUES('grp_one','{}')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < records; i++ {
		id := fmt.Sprintf("super_private_restore_client_%04d", i)
		core := fmt.Sprintf("super_private_core_email_%04d", i)
		token := fmt.Sprintf("super_private_token_%04d", i)
		url := fmt.Sprintf("https://private-legacy.example/secret/%s?token=%s", id, token)
		total := int64(0)
		if i%3 != 0 {
			total = 5000
		}
		deleted := float64(0)
		if i%10 == 0 {
			deleted = 1720000000
		}
		promoted := float64(0)
		if i%29 == 0 && deleted == 0 {
			promoted = 1720000500
		}
		if _, err = tx.Exec(`INSERT INTO restore_subscriptions(
   id,public_token,legacy_url,legacy_host,legacy_path,legacy_query,
   core_email,inbound_ids,node_ids,legacy_upload,legacy_download,
   legacy_total,legacy_expire,scan_status,enabled,first_seen,last_seen,
   group_id,promoted_at,promoted_owner,deleted_at,deleted_reason,node_mode,include_local
  )VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, token, url, "private-legacy.example", "/secret/"+id, "token="+token,
			core, "[1]", "[]", 100, 200, total, 1893456000,
			"verified", 1, 0, 0, "grp_one", promoted, "", deleted, "", "selected", 1); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO core_clients VALUES(?)`, core); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO restore_safety(restore_id,metadata_state) VALUES(?,'verified')`, id); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		for _, scope := range []string{"local", "node:synthetic"} {
			up, down := int64(3), int64(7)
			if _, err = tx.Exec(`INSERT INTO restore_usage VALUES(?,?,?,?)`, id, scope, up, down); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if _, err = tx.Exec(`INSERT INTO restore_events VALUES(?,'synthetic-import',1720000000)`, id); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Close/reopen before inspecting; the detector must NEVER read live WAL.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	return path, nil
}
func hasFinding(r Report, code string) bool {
	for _, x := range r.Findings {
		if x.Code == code {
			return true
		}
	}
	return false
}
func TestOfflineDarkRestoreReadOnly517RecordsAndNoSecretLeak(t *testing.T) {
	path, _ := sourceFixture(t, 517)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	result, err := Analyze(context.Background(), path, time.Unix(1780000000, 0))
	if err != nil || result.Status != "ready_for_manual_review" || result.ImportEnabled {
		t.Fatalf("report wasn't safe %+v %v", result, err)
	}
	archived := int64(0)
	promoted := int64(0)
	unlimited := int64(0)
	for i := 0; i < 517; i++ {
		if i%10 == 0 {
			archived++
		}
		if i%29 == 0 && i%10 != 0 {
			promoted++
		}
		if i%3 == 0 {
			unlimited++
		}
	}
	if result.Subscriptions != 517 || result.Active != 517-archived || result.Archived != archived ||
		result.Promoted != promoted || result.UnlimitedVolume != unlimited ||
		result.LimitedVolume != 517-unlimited || result.UsageScopes != 1034 ||
		result.Events != 517 || result.Groups != 1 || result.GroupMappings != 1 || result.SafetyRows != 517 {
		t.Fatalf("incorrect Restore source counts %+v", result)
	}
	if result.LegacyUploadBytes != "51700" || result.LegacyDownloadBytes != "103400" ||
		result.DarkUploadBytes != "3102" || result.DarkDownloadBytes != "7238" ||
		result.TotalConsumedBytes != "165440" {
		t.Fatalf("incorrect archived-inclusive source counter totals %+v", result)
	}
	if result.ArchivedConsumedBytes != fmt.Sprintf("%d", archived*320) {
		t.Fatalf("archived users lost cumulative history: %+v", result)
	}
	result2, err := Analyze(context.Background(), path, time.Unix(1780000000, 0))
	if err != nil || result2.TotalConsumedBytes != result.TotalConsumedBytes || result2.Subscriptions != result.Subscriptions {
		t.Fatal("second read wasn't idempotent", err)
	}
	data, err := result.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"super_private_restore_client", "super_private_core_email", "super_private_token", "private-legacy.example",
		"restore_user_0000", "/secret/", "token=",
	} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("private source data leaked: %s", secret)
		}
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(now) != hash {
		t.Fatal("read-only source inspection mutated SQLite snapshot")
	}
	if matches, _ := filepath.Glob(path + "-*"); len(matches) != 0 {
		t.Fatal("source inspector wrote SQLite temporary files", matches)
	}
}
func TestOfflineDarkRestoreArchiveMustNeverSubtractGroupTraffic(t *testing.T) {
	path, _ := sourceFixture(t, 8)
	before, err := Analyze(context.Background(), path, time.Unix(1780000000, 0))
	if err != nil || before.Archived != 1 {
		t.Fatal(before, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE restore_subscriptions SET deleted_at=1720000000 WHERE id='super_private_restore_client_0001'`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := Analyze(context.Background(), path, time.Unix(1780000000, 0))
	if err != nil || after.Archived != 2 || after.Active != 6 {
		t.Fatal("archived vs active counts invalid", after, err)
	}
	if after.TotalConsumedBytes != before.TotalConsumedBytes ||
		after.DarkUploadBytes != before.DarkUploadBytes ||
		after.DarkDownloadBytes != before.DarkDownloadBytes {
		t.Fatal("archive changed historical consumed group/global totals", before, after)
	}
	if after.ArchivedConsumedBytes == "0" || after.ArchivedConsumedBytes == before.ArchivedConsumedBytes {
		t.Fatal("archived historical accounting not preserved")
	}
}
func TestOfflineDarkRestoreSchemaDriftOrNegativeCountersFailClosed(t *testing.T) {
	path, _ := sourceFixture(t, 2)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE restore_subscriptions SET legacy_upload=-10,scan_status='scan_failed' WHERE id='super_private_restore_client_0001'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE restore_subscriptions SET group_id='deleted_group' WHERE id='super_private_restore_client_0000'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO restore_usage VALUES('missing_source','local',10,20)`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := Analyze(context.Background(), path, time.Unix(1780000000, 0))
	if err != nil || result.Status != "blocked" || result.ImportEnabled {
		t.Fatal(result, err)
	}
	for _, code := range []string{"negative_legacy_counter", "source_metadata_review_needed",
		"orphan_group_assignment", "orphan_restore_usage"} {
		if !hasFinding(result, code) {
			t.Fatalf("missing violation %s in %+v", code, result.Findings)
		}
	}
}
func TestOfflineDarkRestoreUnsafePathWalSymlinkAndMissingSchema(t *testing.T) {
	path, _ := sourceFixture(t, 1)
	if _, err := Analyze(context.Background(), "relative.sqlite", time.Now()); !errors.Is(err, ErrUnsafeSource) {
		t.Fatal("relative path accepted", err)
	}
	link := filepath.Join(t.TempDir(), "linked.sqlite")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(context.Background(), link, time.Now()); !errors.Is(err, ErrUnsafeSource) {
		t.Fatal("linked copy accepted", err)
	}
	wal := path + "-wal"
	if err := os.WriteFile(wal, []byte("uncheckpointed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(context.Background(), path, time.Now()); !errors.Is(err, ErrUnsafeSource) {
		t.Fatal("active WAL accepted", err)
	}
	if err := os.Remove(wal); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE restore_safety`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(context.Background(), path, time.Now())
	if err != nil || report.Status != "blocked" || !hasFinding(report, "missing_table_restore_safety") ||
		report.ImportEnabled {
		t.Fatal("unsafe schema silently accepted", report, err)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "super_private") {
		t.Fatal("report contained private identity")
	}
}
