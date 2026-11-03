package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	restorefixture "github.com/darktunnelmika/dark-xray-pro/testdata/darkrestore"
)

// Exercise the public SHA-pinned SQLite path, without bypassing source verification.
func TestDarkRestore750OfflineCustomersScopedFiltersAndOwnershipRecovery(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "synthetic-750.sqlite")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
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
	const count, groupCount = 750, 6
	expiry := time.Now().Add(30 * 24 * time.Hour).Unix()
	groupHistory := make(map[string]int64)
	for group := 0; group < groupCount; group++ {
		id := fmt.Sprintf("scale-group-%d", group)
		if _, err = tx.Exec("INSERT INTO restore_groups VALUES(?,?,?)", id, fmt.Sprintf("Synthetic group %d", group), id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_group_targets VALUES(?,?)", id, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < count; i++ {
		id, core := fmt.Sprintf("scale-source-%04d", i), fmt.Sprintf("scale-core-%04d", i)
		group := fmt.Sprintf("scale-group-%d", i/(count/groupCount))
		uuid := fmt.Sprintf("12345678-1234-4abc-8def-%012d", i)
		body, _ := json.Marshal(map[string]string{"id": uuid})
		if _, err = tx.Exec("INSERT INTO core_clients VALUES(?,?)", core, string(body)); err != nil {
			t.Fatal(err)
		}
		up, down := int64(i*17+100), int64(i*31+200)
		if _, err = tx.Exec("INSERT INTO restore_subscriptions VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			id, "token-"+id, "https://example.invalid/sub", "example.invalid", "/sub", "", core, "[1]", "[]",
			up, down, int64(2<<30), expiry, "verified", 1, 0, 0, group, 0, "", 0, "", "selected", 1); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_safety(restore_id,metadata_state) VALUES(?,?)", id, "verified"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_usage VALUES(?,?,?,?)", id, "local", 3, 7); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO restore_events(restore_id,event,detail,at) VALUES(?,?,?,?)", id, "group.changed", "{\"from\":\"synthetic-old\",\"to\":\"synthetic-new\"}", 1720000000.125+float64(i)); err != nil {
			t.Fatal(err)
		}
		groupHistory[group] += up + down + 10
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
	for i, g := range review.Groups {
		rep := "rep-target"
		if i%2 != 0 {
			rep = "rep-source"
		}
		intent.Mappings = append(intent.Mappings, DarkRestoreGroupMappingV1{SourceGroupID: g.SourceGroupID, RepresentativeID: rep})
	}
	for _, rep := range []string{"rep-source", "rep-target", "rep-third"} {
		if _, err = f.s.CreateClient(ctx, NewClient{EventID: "scale-native-" + rep, ClientID: "native-" + rep, RepresentativeID: rep, Name: "Native control", DurationDays: 30, VolumeBytes: 100}); err != nil {
			t.Fatal(err)
		}
	}
	before := ownershipBalances(t, f.s)
	req := importRequest(t, f, intent, "scale-offline-import-0001")
	key := []byte(strings.Repeat("k", 32))
	started := time.Now()
	receipt, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, source, key, req)
	importDuration := time.Since(started)
	if err != nil || receipt.Count != count || receipt.Disabled != count || receipt.LegacyEvents != count {
		t.Fatalf("import: %+v %v", receipt, err)
	}
	replay, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, source, key, req)
	if err != nil || !replay.Replayed || replay.Count != count {
		t.Fatalf("same-source retry: %+v %v", replay, err)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("import changed credit")
	}
	lookup := make(map[string]darkrestore.ReviewClient, count)
	for _, c := range review.Clients {
		lookup[darkRestoreClientID(review.SourceSHA256, c.SourceID)] = c
	}
	assertDirectory := func(store *Store, representative string, want int64) {
		t.Helper()
		seen := map[string]bool{}
		for offset := 0; ; offset += 50 {
			page, e := store.SearchClients(ctx, ClientSearch{RepresentativeID: representative, Origin: "dark_restore", Sort: "name", Limit: 50, Offset: offset})
			if e != nil || page.Total != want {
				t.Fatalf("scoped imported page: rep=%s count=%d err=%v", representative, page.Total, e)
			}
			for _, c := range page.Clients {
				original, ok := lookup[c.ID]
				if !ok || seen[c.ID] || c.RepresentativeID != representative || c.UUID != original.UUID || c.ExpiresUnix != original.ExpiryUnix || c.DeploymentState != "disabled" {
					t.Fatal("pagination/identity/scope changed", c.ID)
				}
				seen[c.ID] = true
			}
			if !page.HasMore {
				break
			}
		}
		if int64(len(seen)) != want {
			t.Fatal("incomplete or duplicate pagination")
		}
		native, e := store.SearchClients(ctx, ClientSearch{RepresentativeID: representative, Origin: "native", Limit: 50})
		if e != nil || native.Total != 1 || native.Clients[0].ID != "native-"+representative {
			t.Fatalf("native origin mixed with imports: %+v %v", native, e)
		}
	}
	assertDirectory(f.s, "rep-source", 375)
	assertDirectory(f.s, "rep-target", 375)
	assertDirectory(f.s, "rep-third", 0)
	first := receipt.Groups[0]
	groupPage, err := f.s.SearchClients(ctx, ClientSearch{GroupID: first.TargetGroupID, Origin: "dark_restore", Limit: 200})
	if err != nil || groupPage.Total != 125 || len(groupPage.Clients) != 125 {
		t.Fatalf("group filter: %+v %v", groupPage, err)
	}
	foreign, err := f.s.SearchClients(ctx, ClientSearch{RepresentativeID: "rep-source", GroupID: first.TargetGroupID, Origin: "dark_restore", Limit: 200})
	if err != nil || foreign.Total != 0 {
		t.Fatalf("group bypassed representative scope: %+v %v", foreign, err)
	}
	if _, err = f.s.CreateGroup(ctx, NewGroup{ID: "scale-destination", RepresentativeID: "rep-third", Name: "Assigned synthetic customers"}); err != nil {
		t.Fatal(err)
	}
	move := ClientOwnershipIntent{SourceGroupID: first.TargetGroupID, RepresentativeID: "rep-third", GroupID: "scale-destination", Reason: "Assign one complete synthetic import group"}
	assignment := ownershipMove(t, f, "scale-group-assignment-0001", move)
	if assignment.Count != 125 || !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("group assignment changed credit or lost members")
	}
	assertDirectory(f.s, "rep-target", 250)
	assertDirectory(f.s, "rep-third", 125)
	history, err := f.s.GroupHistory(ctx, first.TargetGroupID)
	if err != nil || len(history) != 1 || history[0].HistoricalBytes != strconv.FormatInt(groupHistory[first.SourceGroupID], 10) || history[0].SourceCount != 125 {
		t.Fatalf("original group history changed: %+v %v", history, err)
	}
	empty, err := f.s.SearchClients(ctx, ClientSearch{GroupID: first.TargetGroupID, Origin: "dark_restore", Limit: 50})
	if err != nil || empty.Total != 0 {
		t.Fatal("old current membership persisted", err)
	}
	movedID := assignment.Clients[0].ClientID
	renewal := ExtendClient{EventID: "scale-current-payer-renewal-0001", ClientID: movedID, AdditionalVolumeBytes: 4096, Reason: "New manager pays one renewal"}
	if _, err = f.s.ExtendClientScoped(ctx, renewal, "rep-target"); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("previous manager retained renewal access", err)
	}
	for i := 0; i < 2; i++ {
		if _, err = f.s.ExtendClientScoped(ctx, renewal, "rep-third"); err != nil {
			t.Fatal("current manager/replay", err)
		}
	}
	after := ownershipBalances(t, f.s)
	for i, r := range before {
		if r.ID == "rep-third" {
			r.VolumeCreditBytes -= 4096
		}
		if r != after[i] {
			t.Fatal("renewal charged the wrong payer or charged twice")
		}
	}
	usage, err := f.s.DarkRestoreUsageV1(ctx, movedID)
	if err != nil || usage.PostMigrationBytes != "0" || usage.CutoverVerified || usage.HistoricalBytes != lookup[movedID].HistoricalBytes {
		t.Fatal("assignment/renewal reset history or claimed cutover", usage, err)
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
	assertDirectory(reopened, "rep-source", 375)
	assertDirectory(reopened, "rep-target", 250)
	assertDirectory(reopened, "rep-third", 125)
	saved, err := reopened.DarkRestoreImportReceiptV1(ctx, f.owner.Token, req.EventID)
	if err != nil || saved.Count != count || saved.LegacyEvents != count {
		t.Fatal("import receipt lost", err)
	}
	events, err := reopened.DarkRestoreEventHistory(ctx, f.owner.Token, req.EventID, key, 740, 20)
	if err != nil || len(events.Events) != 10 || events.Events[9].Kind != "group.changed" {
		t.Fatal("sealed event archive lost", err)
	}
	if _, err = reopened.DarkRestoreEventHistory(ctx, f.rep.Token, req.EventID, key, 0, 20); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("source event archive leaked")
	}
	recovered, err := reopened.CommitDarkRestoreImportV1(ctx, f.owner.Token, source, key, req)
	if err != nil || !recovered.Replayed {
		t.Fatal("restart duplicated import", err)
	}
	if !reflect.DeepEqual(after, ownershipBalances(t, reopened)) {
		t.Fatal("restart/replay changed credit")
	}
	unchanged, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(unchanged) != sum {
		t.Fatal("offline source was modified", err)
	}
	t.Logf("750 private offline source rows, 6 groups, 3 representatives; import=%s; 125-client ownership transfer, single-charge renewal and restart recovery verified", importDuration)
}

func TestClientOriginBeforeImportSchemaAndCorruptMetadata(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	if _, err := s.CreateClient(ctx, NewClient{EventID: "origin-native-create-0001", ClientID: "origin-native", RepresentativeID: "owner", Name: "Native customer", DurationDays: 30, VolumeBytes: 100}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		origin string
		total  int64
	}{{"native", 1}, {"dark_restore", 0}} {
		page, err := s.SearchClients(ctx, ClientSearch{Origin: tc.origin, Limit: 25})
		if err != nil || page.Total != tc.total {
			t.Fatalf("pre-import origin %s: %+v %v", tc.origin, page, err)
		}
	}
	for _, q := range []ClientSearch{{Origin: "unknown", Limit: 25}, {GroupID: "x' OR 1=1", Limit: 25}} {
		if _, err := s.SearchClients(ctx, q); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe filter accepted", err)
		}
	}
	f := importSetup(t)
	req := importRequest(t, f, syntheticImportIntent(1), "origin-corrupt-import-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec("DROP INDEX darkrestore_import_entries_rep_v1"); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"native", "dark_restore"} {
		if _, err := f.s.SearchClients(ctx, ClientSearch{Origin: origin, Limit: 25}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("corrupt import metadata silently reclassified clients", origin, err)
		}
	}
}
