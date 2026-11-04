package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	restorefixture "github.com/darktunnelmika/dark-xray-pro/testdata/darkrestore"
)

// This test is deliberately synthetic and offline. It exercises a representative
// directory size without using or modifying any production snapshot. The zz file
// ordering keeps this expensive acceptance fixture after short-lease unit tests on
// constrained runners; it does not alter product behavior or test assertions.
func TestDarkRestore5000OfflineCustomersImportReplayAndRecovery(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "synthetic-5000.sqlite")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(restorefixture.SQL); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, table := range []string{"restore_events", "restore_usage", "restore_safety", "restore_subscriptions", "core_clients", "restore_group_targets", "restore_groups"} {
		if _, err = tx.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}

	const count, groupCount = 5000, 20
	const membersPerGroup = count / groupCount
	expiry := time.Now().Add(45 * 24 * time.Hour).Unix()
	for group := 0; group < groupCount; group++ {
		id := fmt.Sprintf("production-scale-group-%02d", group)
		if _, err = tx.Exec("INSERT INTO restore_groups VALUES(?,?,?)", id, fmt.Sprintf("Synthetic production-scale group %02d", group), id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_group_targets VALUES(?,?)", id, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("production-scale-source-%05d", i)
		core := fmt.Sprintf("production-scale-core-%05d", i)
		group := fmt.Sprintf("production-scale-group-%02d", i/membersPerGroup)
		uuid := fmt.Sprintf("87654321-4321-4abc-8def-%012d", i)
		body, marshalErr := json.Marshal(map[string]string{"id": uuid})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = tx.Exec("INSERT INTO core_clients VALUES(?,?)", core, string(body)); err != nil {
			t.Fatal(err)
		}
		up, down := int64(i*19+101), int64(i*37+203)
		if _, err = tx.Exec("INSERT INTO restore_subscriptions VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			id, "token-"+id, "https://example.invalid/sub", "example.invalid", "/sub", "", core, "[1]", "[]",
			up, down, int64(4<<30), expiry, "verified", 1, 0, 0, group, 0, "", 0, "", "selected", 1); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_safety(restore_id,metadata_state) VALUES(?,?)", id, "verified"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_usage VALUES(?,?,?,?)", id, "local", 5, 11); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_events(restore_id,event,detail,at) VALUES(?,?,?,?)", id, "group.changed", "{\"from\":\"synthetic-old\",\"to\":\"synthetic-new\"}", 1730000000.25+float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(source, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	review, err := darkrestore.BuildReview(ctx, source, hex.EncodeToString(sum[:]), time.Now())
	if err != nil || len(review.Clients) != count || len(review.Groups) != groupCount {
		t.Fatalf("offline review: %v, clients=%d groups=%d", err, len(review.Clients), len(review.Groups))
	}

	intent := DarkRestoreImportIntentV1{Review: review}
	reps := []string{"rep-source", "rep-target", "rep-third"}
	expected := map[string]int64{}
	for i, group := range review.Groups {
		rep := reps[i%len(reps)]
		intent.Mappings = append(intent.Mappings, DarkRestoreGroupMappingV1{SourceGroupID: group.SourceGroupID, RepresentativeID: rep})
		expected[rep] += membersPerGroup
	}
	before := ownershipBalances(t, f.s)
	req := importRequest(t, f, intent, "production-scale-offline-import-0001")
	key := []byte(strings.Repeat("p", 32))
	started := time.Now()
	receipt, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, source, key, req)
	importDuration := time.Since(started)
	if err != nil || receipt.Count != count || receipt.Disabled != count || receipt.Archived != 0 || receipt.LegacyEvents != count {
		t.Fatalf("import: %+v %v", receipt, err)
	}
	replay, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, source, key, req)
	if err != nil || !replay.Replayed || replay.Count != count {
		t.Fatalf("same-source retry: %+v %v", replay, err)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("import or replay changed representative credit")
	}

	lookup := make(map[string]darkrestore.ReviewClient, count)
	for _, client := range review.Clients {
		lookup[darkRestoreClientID(review.SourceSHA256, client.SourceID)] = client
	}
	assertDirectory := func(store *Store, representative string, want int64) {
		t.Helper()
		seen := make(map[string]bool, want)
		for offset := 0; ; offset += 100 {
			page, searchErr := store.SearchClients(ctx, ClientSearch{RepresentativeID: representative, Origin: "dark_restore", Sort: "name", Limit: 100, Offset: offset})
			if searchErr != nil || page.Total != want {
				t.Fatalf("scoped imported page: rep=%s total=%d want=%d err=%v", representative, page.Total, want, searchErr)
			}
			for _, client := range page.Clients {
				original, ok := lookup[client.ID]
				if !ok || seen[client.ID] || client.RepresentativeID != representative || client.UUID != original.UUID || client.ExpiresUnix != original.ExpiryUnix || client.DeploymentState != "disabled" {
					t.Fatalf("pagination/identity/quarantine changed: %+v", client)
				}
				seen[client.ID] = true
			}
			if !page.HasMore {
				break
			}
		}
		if int64(len(seen)) != want {
			t.Fatalf("incomplete or duplicate pagination: got=%d want=%d", len(seen), want)
		}
	}
	for _, rep := range reps {
		assertDirectory(f.s, rep, expected[rep])
	}

	sampleID := darkRestoreClientID(review.SourceSHA256, review.Clients[count-1].SourceID)
	usage, err := f.s.DarkRestoreUsageV1(ctx, sampleID)
	if err != nil || usage.HistoricalBytes == "0" || usage.PostMigrationBytes != "0" || usage.TotalBytes != usage.HistoricalBytes || usage.CutoverVerified {
		t.Fatalf("historical/post-migration split changed: %+v %v", usage, err)
	}
	var unsafe int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM clients WHERE deployment_state <> 'disabled'").Scan(&unsafe); err != nil || unsafe != 0 {
		t.Fatalf("import escaped quarantine: count=%d err=%v", unsafe, err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("offline backup validation", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal("restart", err)
	}
	defer reopened.Close()
	for _, rep := range reps {
		assertDirectory(reopened, rep, expected[rep])
	}
	saved, err := reopened.DarkRestoreImportReceiptV1(ctx, f.owner.Token, req.EventID)
	if err != nil || saved.Count != count || saved.Disabled != count || saved.LegacyEvents != count {
		t.Fatalf("import receipt lost: %+v %v", saved, err)
	}
	events, err := reopened.DarkRestoreEventHistory(ctx, f.owner.Token, req.EventID, key, count-5, 10)
	if err != nil || len(events.Events) != 5 || events.Events[4].Kind != "group.changed" {
		t.Fatalf("sealed event archive lost: %+v %v", events, err)
	}
	recovered, err := reopened.CommitDarkRestoreImportV1(ctx, f.owner.Token, source, key, req)
	if err != nil || !recovered.Replayed || recovered.Count != count {
		t.Fatalf("restart duplicated import: %+v %v", recovered, err)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, reopened)) {
		t.Fatal("restart or replay changed representative credit")
	}
	unchanged, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(unchanged) != sum {
		t.Fatal("offline source was modified", err)
	}
	t.Logf("5000 synthetic private offline rows, 20 groups, 3 representatives; import=%s; idempotent replay, quarantine, accounting split and restart recovery verified", importDuration)
}
