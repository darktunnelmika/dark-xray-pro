package business

import (
	"context"
	"errors"
	"reflect"
	"strings"
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
	if _, err := f.s.CommitDarkRestoreImportV1(ctx, f.owner.Token, "/nonexistent-private-snapshot", req); !errors.Is(err, ErrInvalid) {
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
