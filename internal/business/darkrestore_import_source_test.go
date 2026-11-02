package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const importSourceDDL = `
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

func TestDarkRestoreRealOfflineSnapshotToClients(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-offline.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{importSourceDDL,
		"ALTER TABLE core_clients ADD COLUMN body TEXT",
		"INSERT INTO restore_groups VALUES('synthetic-group','Synthetic source','synthetic-source')",
		"INSERT INTO restore_group_targets VALUES('synthetic-group','{}')",
		`INSERT INTO core_clients VALUES('synthetic-core','{"id":"01234567-89ab-4cde-8fab-0123456789ab"}')`,
		`INSERT INTO restore_subscriptions VALUES('synthetic-source','synthetic-token','https://example.invalid/sub','example.invalid','/sub','','synthetic-core','[1]','[]',100,200,5000,1893456000,'verified',1,0,0,'synthetic-group',0,'',0,'','selected',1)`,
		"INSERT INTO restore_safety(restore_id,metadata_state) VALUES('synthetic-source','verified')",
		"INSERT INTO restore_usage VALUES('synthetic-source','local',3,7)",
		"INSERT INTO restore_events VALUES('synthetic-source','synthetic-created',1720000000)",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	review, err := darkrestore.BuildReview(ctx, path, hex.EncodeToString(hash[:]), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	intent := DarkRestoreImportIntentV1{Review: review, Mappings: []DarkRestoreGroupMappingV1{{SourceGroupID: "synthetic-group", RepresentativeID: "rep-target"}}}
	req := importRequest(t, f, intent, "darkrestore-real-source-0001")
	receipt, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, path, req)
	if err != nil || receipt.Count != 1 {
		t.Fatal("verified snapshot import failed", receipt, err)
	}
	id := darkRestoreClientID(review.SourceSHA256, "synthetic-source")
	projection, err := f.s.DarkRestoreUsageV1(ctx, id)
	if err != nil || projection.HistoricalBytes != "310" || projection.TotalBytes != "310" || projection.CutoverVerified {
		t.Fatal("historical baseline lost", projection, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(after) != hash {
		t.Fatal("source changed", err)
	}
}
