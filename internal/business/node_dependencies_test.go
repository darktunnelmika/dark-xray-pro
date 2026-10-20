package business

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func dependencyCount(t *testing.T, d NodeDependencies, name string, want int64) {
	t.Helper()
	n, ok := d.Counts[name]
	if !ok || n == nil || *n != want {
		t.Fatalf("%s: %v want %d", name, n, want)
	}
}

func TestNodeDependenciesReadOnlySwapAndHistoricalInstallation(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	var changesBefore, generationBefore int64
	if err := s.db.QueryRow(`SELECT total_changes()`).Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	d, err := s.InspectNodeDependencies(ctx, key, "nl", strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	dependencyCount(t, d, "inbound_targets", 1)
	dependencyCount(t, d, "swap_upstream_references", 1)
	dependencyCount(t, d, "route_drafts", 0)
	if d.Counts["usage_epochs"] != nil || d.UnknownReasons["usage_epochs"] != "usage_ledger_disabled" {
		t.Fatal("invented usage zero")
	}
	d, err = s.InspectNodeDependencies(ctx, key, "de", strings.Repeat("e", 32))
	if err != nil {
		t.Fatal(err)
	}
	dependencyCount(t, d, "inbound_targets", 2)
	dependencyCount(t, d, "route_drafts", 1)
	dependencyCount(t, d, "draft_events", 1)
	dependencyCount(t, d, "foreign_installation_rows", 3)
	var changesAfter, generationAfter int64
	if err := s.db.QueryRow(`SELECT total_changes()`).Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generationAfter); err != nil {
		t.Fatal(err)
	}
	if changesAfter != changesBefore || generationAfter != generationBefore {
		t.Fatal("inspection mutated database")
	}
	var ledgerTables int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='pro_usage_epochs'`).Scan(&ledgerTables); err != nil || ledgerTables != 0 {
		t.Fatal("inspection initialized ledger", err)
	}
}

func TestNodeDependenciesMissingKeyCorruptionAndCancellation(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	d, err := s.InspectNodeDependencies(ctx, nil, "nl", strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	if d.Counts["swap_upstream_references"] != nil || d.UnknownReasons["swap_upstream_references"] != "inbound_key_unavailable" {
		t.Fatal("missing key treated as zero")
	}
	if _, err := s.InspectNodeDependencies(ctx, bytes.Repeat([]byte{7}, 32), "nl", strings.Repeat("a", 32)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("wrong key not refused", err)
	}
	if _, err := s.db.Exec(`UPDATE native_route_drafts SET payload_enc=x'00'`); err != nil {
		t.Fatal(err)
	}
	if d, err := s.InspectNodeDependencies(ctx, key, "nl", strings.Repeat("a", 32)); !errors.Is(err, ErrUnavailable) || d.Counts != nil {
		t.Fatal("corruption exposed partial trustworthy counts", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.InspectNodeDependencies(cancelled, key, "nl", strings.Repeat("a", 32)); err == nil {
		t.Fatal("cancelled inspection succeeded")
	}
	if _, err := s.InspectNodeDependencies(ctx, key, "bad/id", strings.Repeat("a", 32)); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestNodeDependenciesUsageDisabledVersusVerifiedEmptyAndConcurrentReads(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{1}, 32)
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := s.InspectNodeDependencies(ctx, key, "empty-node", strings.Repeat("a", 32))
			if err == nil && (d.Counts["usage_epochs"] == nil || *d.Counts["usage_epochs"] != 0 || len(d.UnknownReasons) != 0) {
				err = errors.New("empty enabled ledger not distinguished")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`DROP TABLE pro_usage_holds`); err != nil {
		t.Fatal(err)
	}
	if d, err := s.InspectNodeDependencies(ctx, key, "empty-node", strings.Repeat("a", 32)); err == nil || d.Counts != nil {
		t.Fatal("missing table invented zero")
	}
}

func TestNodeDependenciesCountsRetainedUsageAndCommandsWithoutReset(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{1}, 32)
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	c := usageClient(t, s, "inspection-client", 1000)
	// Synthetic SQLite rows exercise read-only aggregation, not traffic or
	// ledger acceptance. Process lifecycle acceptance remains in dedicated CI.
	for i, state := range []string{"open", "sealed", "uncertain"} {
		id := []string{"epoch-open", "epoch-sealed", "epoch-uncertain"}[i]
		_, err := s.db.Exec(`INSERT INTO pro_usage_epochs(epoch_id,node_id,installation_id,kernel_boot_id,pid,start_ticks,config_sha,binding_sha,epoch_json,cursor_json,state,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, "read-node", strings.Repeat("b", 32), "boot", i+1, "1", "sha", "sha", []byte(`{}`), []byte(`{}`), state, 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO pro_usage_holds VALUES('epoch-uncertain','inspection-client','unknown-tail',1)`,
		`INSERT INTO pro_usage_receipts VALUES('epoch-sealed','1','sha',1,1)`,
		`INSERT INTO pro_usage_deltas VALUES('epoch-sealed','1','inspection-client',17,23)`,
		`UPDATE pro_usage_accounts SET uplink=17,downlink=23,holds=1 WHERE client_id='inspection-client'`,
		`INSERT INTO native_route_channels VALUES('read-node','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','sha',2,0,'')`,
		`INSERT INTO native_route_commands VALUES('command-1','read-node','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'apply','stage','fp',1,'queued',x'00','')`,
		`INSERT INTO native_route_commands VALUES('command-2','read-node','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',2,'apply','stage','fp',1,'abandoned_unknown',x'00','')`,
		`INSERT INTO native_metered_route_channels VALUES('read-node','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','sha',1,0,'')`,
		`INSERT INTO native_metered_route_commands VALUES('metered-1','read-node','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'apply','stage','fp',1,'delivered',x'00','')`,
		`INSERT INTO native_metered_route_births VALUES('metered-1',x'7b7d')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.ClientUsage(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.InspectNodeDependencies(ctx, key, "read-node", strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int64{"usage_epochs": 3, "open_usage_epochs": 1, "sealed_usage_epochs": 1, "uncertain_usage_epochs": 1, "usage_holds": 1, "usage_receipts": 1, "usage_deltas": 1, "foreign_usage_epochs": 3, "route_commands": 2, "pending_route_commands": 1, "unknown_route_commands": 1, "metered_commands": 1, "pending_metered_commands": 1, "metered_births": 1, "foreign_installation_rows": 5} {
		dependencyCount(t, d, name, want)
	}
	after, err := s.ClientUsage(ctx, c.ID)
	if err != nil || after.Total != before.Total || after.Total != 40 || after.Eligible != before.Eligible {
		t.Fatal("inspection reset usage or eligibility", err)
	}
}

func TestNodeDependenciesDraftScanLimitIsUnknown(t *testing.T) {
	s, key, _ := routeDraftFixture(t)
	_, err := s.db.Exec(`WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<4097)
 INSERT INTO native_route_drafts(inbound_id,node_id,installation_id,revision,inbound_revision,listen_ip,original_direct_port,original_swap_port,direct_port,tunnel_port,swap_node_port,last_event_id,payload_enc,created_unix,updated_unix)
 SELECT 1185,'bounded-'||n,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,1,'127.0.0.1',20000,0,20000,0,0,'bounded-event-'||n,x'00',1,1 FROM seq`)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.InspectNodeDependencies(context.Background(), key, "nl", strings.Repeat("a", 32))
	if err != nil || d.Counts["swap_upstream_references"] != nil || d.UnknownReasons["swap_upstream_references"] != "draft_scan_limit" {
		t.Fatal("truncated scan claimed complete", err)
	}
	dependencyCount(t, d, "inbound_targets", 1)
}

func TestNodeDependenciesArchivedTargetsKeepDistinctClientReferences(t *testing.T) {
	s, key, _ := routeDraftFixture(t)
	ctx := context.Background()
	c := usageClient(t, s, "archived-reference", 1000)
	for _, id := range []uint64{1185, 2000} {
		if err := s.BindInboundClients(ctx, id, []string{c.ID}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE native_inbounds SET enabled=0,archived=1`); err != nil {
		t.Fatal(err)
	}
	d, err := s.InspectNodeDependencies(ctx, key, "de", strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	dependencyCount(t, d, "inbound_targets", 2)
	dependencyCount(t, d, "active_inbound_targets", 0)
	dependencyCount(t, d, "assigned_clients", 1)
	if _, err := s.Client(ctx, c.ID); err != nil {
		t.Fatal("inspection removed client", err)
	}
}
