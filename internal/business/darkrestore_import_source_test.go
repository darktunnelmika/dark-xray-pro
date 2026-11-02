package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	restorefixture "github.com/darktunnelmika/dark-xray-pro/testdata/darkrestore"
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

	if _, err = db.Exec(restorefixture.SQL); err != nil {
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
	receipt, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, path, make([]byte, 32), req)
	if err != nil || receipt.Count != 1 {
		t.Fatal("verified snapshot import failed", receipt, err)
	}
	if receipt.LegacyEvents != 1 {
		t.Fatal("source event missing")
	}
	key := make([]byte, 32)
	history, err := f.s.DarkRestoreEventHistory(ctx, f.owner.Token, receipt.EventID, key, 0, 20)
	if err != nil || len(history.Events) != 1 {
		t.Fatal("history not recoverable", err)
	}
	event := history.Events[0]
	if event.ID == nil || *event.ID != 9 || event.Kind != "group.changed" || event.At != 1720000000.125 || event.Detail == nil || *event.Detail != `{"from":"synthetic-old-group","to":"synthetic-group"}` {
		t.Fatal("source event changed")
	}
	var sealed []byte
	if err = f.s.db.QueryRow("SELECT sealed_history FROM darkrestore_event_archives_v1").Scan(&sealed); err != nil || bytes.Contains(sealed, []byte("synthetic-old-group")) {
		t.Fatal("private history exposed", err)
	}
	if _, err = f.s.DarkRestoreEventHistory(ctx, f.rep.Token, receipt.EventID, key, 0, 20); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("history exposed to representative", err)
	}
	if _, err = f.s.DarkRestoreEventHistory(ctx, f.owner.Token, receipt.EventID, bytes.Repeat([]byte{1}, 32), 0, 20); err == nil {
		t.Fatal("incorrect key accepted")
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("sealed archive invalid", err)
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
