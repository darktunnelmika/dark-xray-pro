package business

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func importRequest(t *testing.T, f ownershipFixture, intent DarkRestoreImportIntentV1, event string) DarkRestoreImportCommitV1 {
	t.Helper()
	q, err := f.s.QuoteDarkRestoreImportV1(context.Background(), f.owner.Token, intent)
	if err != nil {
		t.Fatal(err)
	}
	return DarkRestoreImportCommitV1{DarkRestoreImportIntentV1: intent, EventID: event, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}
}
func importSetup(t *testing.T) ownershipFixture {
	t.Helper()
	f := ownershipSetup(t)
	if err := f.s.EnableDarkRestoreImportV1(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f
}
func TestDarkRestoreCommitAtomicReplayPreservesIdentityHistoryAndCredit(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(3)
	intent.Review.Clients[0].Status = "archive_history_only"
	intent.Review.Clients[0].ReviewReasons = []string{"archived_history_must_not_be_recreated_active"}
	intent.Review.Groups[0].Archived = 1
	intent.Review.Clients[1].Status = "skip_already_promoted"
	intent.Review.Clients[1].ReviewReasons = []string{"promoted_identity_must_not_be_imported_twice"}
	intent.Review.Groups[1].Promoted = 1
	before := ownershipBalances(t, f.s)
	req := importRequest(t, f, intent, "darkrestore-import-test-0001")
	out, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || out.Count != 2 || out.Archived != 1 || out.Disabled != 2 || out.SkippedPromoted != 1 {
		t.Fatal(out, err)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("import debited representative")
	}
	for _, i := range []int{0, 2} {
		source := intent.Review.Clients[i]
		id := darkRestoreClientID(intent.Review.SourceSHA256, source.SourceID)
		c, err := f.s.Client(ctx, id)
		if err != nil || c.UUID != source.UUID || c.ExpiresUnix != source.ExpiryUnix || c.DeploymentState != "disabled" || c.Archived != (i == 0) || c.ChargeVolumeBytes != 0 {
			t.Fatal("identity changed", c, err)
		}
		a, err := f.s.ClientUsage(ctx, id)
		if err != nil || a.Known || a.Eligible || a.Total != 0 {
			t.Fatal("unverified cutover accepted", a, err)
		}
	}
	replay, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || !replay.Replayed || replay.Count != 2 {
		t.Fatal("lost-response replay failed", replay, err)
	}
	req.EventID = "darkrestore-import-test-0002"
	if _, err = f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); !errors.Is(err, ErrConflict) {
		t.Fatal("same file reimport accepted", err)
	}
	receipt, err := f.s.DarkRestoreImportReceiptV1(ctx, f.owner.Token, out.EventID)
	if err != nil || receipt.Count != 2 || receipt.Replayed {
		t.Fatal("receipt lookup failed", receipt, err)
	}
	if _, err = f.s.DarkRestoreImportReceiptV1(ctx, f.rep.Token, out.EventID); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("private receipt leaked", err)
	}
	var count int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM clients").Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate client", count, err)
	}
	var historical int
	if err = f.s.db.QueryRow("SELECT SUM(CAST(historical_bytes AS INTEGER)) FROM darkrestore_group_history_v1").Scan(&historical); err != nil || historical != 21 {
		t.Fatal("archive/promoted history lost", historical, err)
	}
}
func TestDarkRestoreCommitFailureRollsBackAllWrites(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(2), "darkrestore-rollback-test-0001")
	if _, err := f.s.db.Exec("CREATE TRIGGER test_import_failure BEFORE INSERT ON darkrestore_import_entries_v1 BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err == nil {
		t.Fatal("injected failure ignored")
	}
	for _, table := range []string{"clients", "client_groups", "client_group_members", "darkrestore_import_receipts_v1", "darkrestore_import_entries_v1", "darkrestore_group_history_v1", "pro_usage_accounts"} {
		var n int
		if err := f.s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial import survived", table, n, err)
		}
	}
	if _, err := f.s.db.Exec("DROP TRIGGER test_import_failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal("retry after rollback failed", err)
	}
}
func TestDarkRestoreCommitRejectsUnverifiedSourceAndUnsafeBatch(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "darkrestore-source-test-0001")
	if _, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, "/nonexistent-private-snapshot", make([]byte, 32), req); !errors.Is(err, ErrInvalid) {
		t.Fatal("unverified snapshot accepted", err)
	}
	intent.Review.Clients[0].UUID = ""
	intent.Review.Clients[0].ReviewReasons = []string{"uuid_not_verified"}
	req = importRequest(t, f, intent, "darkrestore-blocked-test-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); !errors.Is(err, ErrConflict) {
		t.Fatal("unsafe batch accepted", err)
	}
}

func TestDarkRestoreImportedOwnershipRenewalAndHistoryProjection(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "darkrestore-management-test-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	id := darkRestoreClientID(intent.Review.SourceSHA256, intent.Review.Clients[0].SourceID)
	history, err := f.s.DarkRestoreUsageV1(ctx, id)
	if err != nil || history.HistoricalBytes != "7" || history.PostMigrationBytes != "0" || history.TotalBytes != "7" || history.CutoverVerified {
		t.Fatal("history projection inaccurate", history, err)
	}
	account, err := f.s.ClientUsage(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	account.Known = true // A native baseline is not a source-retirement receipt.
	if projection, err := f.s.DarkRestoreUsageForAccount(ctx, account); err != nil || projection.CutoverVerified {
		t.Fatal("native baseline incorrectly asserted cutover", projection, err)
	}
	move := ClientOwnershipIntent{ClientIDs: []string{id}, RepresentativeID: "rep-third", Reason: "Synthetic imported customer assignment"}
	before := ownershipBalances(t, f.s)
	ownershipMove(t, f, "darkrestore-move-test-0001", move)
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("assignment charged credit")
	}
	renew := ExtendClient{EventID: "darkrestore-renew-test-0001", ClientID: id, AdditionalVolumeBytes: 20, Reason: "Current representative pays renewal"}
	if _, err = f.s.ExtendClientScoped(ctx, renew, "rep-target"); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("old owner retained management", err)
	}
	if _, err = f.s.ExtendClientScoped(ctx, renew, "rep-third"); err != nil {
		t.Fatal("new owner cannot renew", err)
	}
	after := ownershipBalances(t, f.s)
	for i, r := range before {
		want := r.VolumeCreditBytes
		if r.ID == "rep-third" {
			want -= 20
		}
		if after[i].VolumeCreditBytes != want {
			t.Fatal("wrong renewal payer", r.ID)
		}
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("renewal invalidated immutable import", err)
	}
	if _, err = f.s.AdjustCredits(ctx, CreditChange{EventID: req.EventID, RepresentativeID: "rep-target", VolumeDeltaBytes: 1, Reason: "Illegal reused import nonce"}); !errors.Is(err, ErrConflict) {
		t.Fatal("import nonce reused for credit", err)
	}
}
func TestDarkRestoreReopenAndOfflineBackupValidation(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(2), "darkrestore-reopen-test-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	if err := validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("backup rejects imported history", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal("recovery failed", err)
	}
	defer reopened.Close()
	receipt, err := reopened.DarkRestoreImportReceiptV1(ctx, f.owner.Token, req.EventID)
	if err != nil || receipt.Count != 2 {
		t.Fatal("receipt lost on reopen", receipt, err)
	}
	replay, err := reopened.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || !replay.Replayed {
		t.Fatal("restart duplicated import", replay, err)
	}
}

func TestDarkRestorePartialSafeImportKeepsBlockedHistoryAndRejectsUUIDCollision(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(2)
	intent.ImportSafeOnly = true
	intent.Review.Clients[0].UUID = ""
	intent.Review.Clients[0].ReviewReasons = []string{"uuid_not_verified"}
	req := importRequest(t, f, intent, "darkrestore-partial-test-0001")
	receipt, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || receipt.Count != 1 || receipt.Blocked != 1 {
		t.Fatal("safe subset failed", receipt, err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("subset history invalid", err)
	}
	var total int
	if err = f.s.db.QueryRow("SELECT SUM(CAST(historical_bytes AS INTEGER)) FROM darkrestore_group_history_v1").Scan(&total); err != nil || total != 14 {
		t.Fatal("blocked history discarded", total, err)
	}
	changed := intent
	changed.Review.SourceSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err = f.s.QuoteDarkRestoreImportV1(ctx, f.owner.Token, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("UUID collision across snapshots accepted", err)
	}
}
func TestDarkRestoreCorruptedHistoryFailsClosedOnReopen(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(1), "darkrestore-corrupt-test-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec("UPDATE darkrestore_group_history_v1 SET historical_bytes='0'"); err == nil {
		t.Fatal("history mutable")
	}
	// Simulate corruption by an external maintenance tool bypassing guards.
	if _, err := f.s.db.Exec("DROP TRIGGER darkrestore_group_history_v1_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec("UPDATE darkrestore_group_history_v1 SET historical_bytes='0'"); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range darkRestoreImportSchemaV1 {
		if strings.HasPrefix(ddl, "CREATE TRIGGER darkrestore_group_history_v1_no_update ") {
			if _, err := f.s.db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := validateDarkRestoreImportData(ctx, f.s.db); err == nil {
		t.Fatal("contradictory history accepted")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(f.path); err == nil {
		reopened.Close()
		t.Fatal("corrupt import reopened")
	}
	if _, err := ValidateOfflineBackup(ctx, f.path); err == nil {
		t.Fatal("corrupt import backup accepted")
	}
}

// Synthetic tests exercise the database transaction directly. The public
// operator path always rebuilds the review and reads actual source events.
func (s *Store) commitVerifiedDarkRestore(ctx context.Context, token string, req DarkRestoreImportCommitV1) (DarkRestoreImportReceiptV1, error) {
	return s.commitDarkRestoreTx(ctx, token, req, nil, nil)
}

func TestDarkRestoreIndependentWritersShareOneImport(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(2), "restore-multistore-event-0001")
	other, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	type result struct {
		receipt DarkRestoreImportReceiptV1
		err     error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	var writers sync.WaitGroup
	for i := 0; i < 8; i++ {
		s := f.s
		if i%2 == 1 {
			s = other
		}
		writers.Add(1)
		go func(s *Store) {
			defer writers.Done()
			<-start
			var r DarkRestoreImportReceiptV1
			err := refundRetrySQLiteBusy(func() error { var e error; r, e = s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); return e })
			results <- result{r, err}
		}(s)
	}
	close(start)
	writers.Wait()
	close(results)
	applied := 0
	for res := range results {
		if res.err != nil || res.receipt.Count != 2 {
			t.Fatal("writer failed", res.err)
		}
		if !res.receipt.Replayed {
			applied++
		}
	}
	if applied != 1 {
		t.Fatal("wrong number of original receipts", applied)
	}
	if err = validateDarkRestoreImportData(ctx, other.db); err != nil {
		t.Fatal(err)
	}
	id := darkRestoreClientID(req.Review.SourceSHA256, req.Review.Clients[0].SourceID)
	intent := ClientOwnershipIntent{ClientIDs: []string{id}, RepresentativeID: "rep-third", Reason: "reused import event must conflict"}
	collision := ownershipRequest(t, f.s, f.owner.Token, req.EventID, intent)
	if _, err = f.s.CommitClientOwnership(ctx, f.owner.Token, collision); !errors.Is(err, ErrConflict) {
		t.Fatal("ownership accepted import nonce", err)
	}
}

func TestDarkRestoreQuarantineBlocksEnableAndSubscriptions(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(1), "restore-hold-test-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	id := darkRestoreClientID(req.Review.SourceSHA256, req.Review.Clients[0].SourceID)
	readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, req.Review.SourceSHA256, req.Review.Clients[0].SourceID)
	if err != nil || readiness.ClientID != id || readiness.SourceEpochSettled || readiness.GlobalSourceCustodyVerified ||
		readiness.NativeBaselineComplete || readiness.SubscriptionDelivered || readiness.ActivationAllowed || readiness.CutoverVerified ||
		readiness.FiniteQuotaRemainingBytes != "0" ||
		strings.Join(readiness.ActivationBlockers, ",") != "source_epoch_not_settled,global_source_custody_not_proven,finite_quota_exhausted" ||
		strings.Join(readiness.VerificationBlockers, ",") != "native_baseline_incomplete,subscription_not_delivered" {
		t.Fatal("unverified import reported cutover readiness", readiness, err)
	}
	if _, err := f.s.SetClientStateScoped(ctx, SetClientState{EventID: "restore-hold-enable-0001", ClientID: id, Action: "enable", Reason: "Synthetic attempt"}, "rep-target"); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("single enable", err)
	}
	if _, err := f.s.BulkSetClientState(ctx, BulkClientState{EventID: "restore-hold-bulk-00001", ClientIDs: []string{id}, Action: "enable", Reason: "Synthetic attempt"}, "rep-target"); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("bulk enable", err)
	}
	if _, err := f.s.db.Exec("UPDATE clients SET deployment_state='pending_node_apply' WHERE id=?", id); err == nil {
		t.Fatal("quarantine bypassed")
	}
	for _, enable := range []func(context.Context) error{f.s.EnableSystemProbeAccounting, f.s.EnableMeteredHealthChallenges, f.s.EnableMeteredHealthQueue, f.s.EnableMeteredHealthRounds, f.s.EnableLabSubscriptions} {
		if err := enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mutation := SubscriptionTokenMutation{EventID: "restore-hold-token-0001", Action: "issue"}
	if _, err := f.s.MutateLabSubscription(ctx, make([]byte, 32), "owner", id, mutation); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("legacy subscription issue", err)
	}
	// Managed inbound subscriptions start from the native v8 family. The
	// legacy health/subscription database has a separate schema lineage.
	managed := importSetup(t)
	if err := managed.s.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	request := importRequest(t, managed, syntheticImportIntent(1), "restore-managed-hold-0001")
	if _, err := managed.s.commitVerifiedDarkRestore(ctx, managed.owner.Token, request); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.s.MutateInboundExecutionSubscription(ctx, make([]byte, 32), "owner", id, mutation); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("managed subscription issue", err)
	}
}

func TestDarkRestoreImportPreservesExistingSubscriptionProof(t *testing.T) {
	ctx := context.Background()
	f, round, secret := labSubscriptionFixture(t, true)
	if err := f.s.EnableClientOwnershipV1(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.Login(ctx, "owner-lab", "synthetic-subscription-password")
	if err != nil {
		t.Fatal(err)
	}
	materializeSubscription(t, f, round)
	before, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	generation := func() int64 {
		t.Helper()
		var n int64
		if err := f.s.db.QueryRow("SELECT value FROM native_config_generation WHERE id=1").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	start := generation()
	intent := syntheticImportIntent(2)
	for i := range intent.Mappings {
		intent.Mappings[i].RepresentativeID = f.client.RepresentativeID
	}
	q, err := f.s.QuoteDarkRestoreImportV1(ctx, owner.Token, intent)
	if err != nil {
		t.Fatal(err)
	}
	req := DarkRestoreImportCommitV1{DarkRestoreImportIntentV1: intent, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix, EventID: "restore-preserve-proof-0001"}
	if _, err = f.s.commitVerifiedDarkRestore(ctx, owner.Token, req); err != nil {
		t.Fatal(err)
	}
	if start != generation() {
		t.Fatal("quarantined import invalidated live generation")
	}
	after, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || !after.Snapshot.Current || after.Snapshot.ID != before.Snapshot.ID || string(after.Raw) != string(before.Raw) {
		t.Fatal("existing subscription lost its accepted proof", err)
	}
	_, err = f.s.CreateClient(ctx, NewClient{EventID: "restore-native-after-0001", ClientID: "new-native-after", RepresentativeID: "owner", Name: "Native client", VolumeBytes: 100, DurationDays: 30})
	if err != nil || generation() <= start {
		t.Fatal("ordinary creation no longer invalidates native delivery", err)
	}

}

func TestDarkRestoreArchivedGroupHistorySurvivesRenameAndAssignment(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	intent.Review.Clients[0].Status = "archive_history_only"
	intent.Review.Clients[0].ReviewReasons = []string{"archived_history_must_not_be_recreated_active"}
	intent.Review.Groups[0].Archived = 1
	req := importRequest(t, f, intent, "darkrestore-archived-group-history-0001")
	original, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || original.Count != 1 || original.Archived != 1 || original.Groups[0].HistoricalBytes != "7" {
		t.Fatal("archived import changed source history", original, err)
	}
	id := darkRestoreClientID(intent.Review.SourceSHA256, intent.Review.Clients[0].SourceID)
	group := original.Groups[0]
	if _, err := f.s.UpdateGroup(ctx, group.TargetGroupID, "rep-target", "Renamed imported group", "#123ABC"); err != nil {
		t.Fatal("imported group cannot be renamed", err)
	}
	ownershipMove(t, f, "darkrestore-archived-history-move-0001",
		ClientOwnershipIntent{ClientIDs: []string{id}, RepresentativeID: "rep-third", Reason: "Synthetic archived ownership transfer"})
	var historical string
	if err := f.s.db.QueryRowContext(ctx,
		"SELECT historical_bytes FROM darkrestore_group_history_v1 WHERE source_sha256=? AND source_group_id=?",
		intent.Review.SourceSHA256, group.SourceGroupID).Scan(&historical); err != nil || historical != "7" {
		t.Fatal("historical group total changed after rename or assignment", historical, err)
	}
	usage, err := f.s.DarkRestoreUsageV1(ctx, id)
	if err != nil || usage.HistoricalBytes != "7" || usage.PostMigrationBytes != "0" || usage.TotalBytes != "7" || usage.CutoverVerified {
		t.Fatal("archived client usage changed", usage, err)
	}
	groups, err := f.s.GroupHistory(ctx, group.TargetGroupID)
	if err != nil || len(groups) != 1 || groups[0].SnapshotBytes != "7" || groups[0].ClosedEpochBytes != "0" || groups[0].HistoricalBytes != "7" || groups[0].ArchivedCount != 1 {
		t.Fatal("persisted source group history changed", groups, err)
	}
	receipt, err := f.s.DarkRestoreImportReceiptV1(ctx, f.owner.Token, original.EventID)
	if err != nil || receipt.Groups[0].HistoricalBytes != "7" {
		t.Fatal("source history receipt changed", receipt, err)
	}
	if err := validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("renamed or assigned imported client fails offline validation", err)
	}
}
