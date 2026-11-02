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

func TestDarkRestoreRealOfflineSnapshotToClients(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-offline.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../../testdata/darkrestore/single-customer.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(fixture)); err != nil {
		t.Fatal(err)
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
