//go:build linux

package business

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// This fixture uses the actual supported v16 backup schema, unlike the v15
// native handover delivery fixture. No installed LAB data or archive is changed.
func TestDarkRestoreNativeAttestationOfflineBackupAndRecovery(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	if err := f.s.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableDarkRestoreCutoverV1(ctx, f.owner.Token); err != nil {
		t.Fatal(err)
	}
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "native-attestation-backup-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	source := intent.Review.Clients[0].SourceID
	sha := intent.Review.SourceSHA256
	proof := darkrestore.LocalSourceSettlement{Scope: "single_local_source_epoch", SourceSHA: sha, SourceID: source, AuthoritySHA: strings.Repeat("b", 64), JournalSHA: strings.Repeat("c", 64),
		Terminal:   uc.Receipt{EpochID: strings.Repeat("d", 32), Sequence: 1, SHA: strings.Repeat("e", 64)},
		PriorBytes: 7, Up: 3, Down: 4, Historical: 14, Remaining: 999986}
	handoff, pin, sourceKey := signFixtureHandoffWithKey(t, proof, nil)
	if _, err := f.s.AttachSignedLocalSettlementV1(ctx, f.owner.Token, handoff); err != nil {
		t.Fatal(err)
	}
	before, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil {
		t.Fatal(err)
	}
	destination, attribution := strings.Repeat("1", 64), strings.Repeat("2", 64)
	native, nativePins := businessSignedNativeBaseline(t, before, destination)
	prepared, err := f.s.PrepareDarkRestoreNativeBaselineV1(ctx, f.owner.Token, sha, source, native, nativePins)
	if err != nil {
		t.Fatal(err)
	}
	projected := prepared.ProjectedSnapshot
	inventory := darkrestore.LocalHandoffInventoryV1{Version: 1, SourceSHA: sha, SourceID: source, Sources: []darkrestore.LocalHandoffSourcePinV1{pin}}
	inventorySHA, err := darkrestore.DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	retiredAt := time.Now().UnixNano()
	retirements := []darkrestore.VerifiedSourceRetirementV1{businessSignedRetirement(t, inventory, inventorySHA, destination, pin, sourceKey, retiredAt)}
	retirementCoverage, err := darkrestore.CheckLocalHandoffRetirementsV1(inventory, inventorySHA, []darkrestore.VerifiedLocalHandoff{handoff}, retirements, destination)
	if err != nil {
		t.Fatal(err)
	}
	approval := businessSignedCoverage(t, darkrestore.HandoffCoverageApprovalV1{
		Version: 1, Scope: darkrestore.HandoffCoverageScopeV1, Statement: darkrestore.HandoffCoverageStatementV1,
		InventorySHA: inventorySHA, SourceSHA: sha, SourceID: source, DestinationSHA: destination, ImportReceiptSHA: before.ImportReceiptSHA,
		DestinationStateSHA: projected.StateSHA, NativeBaselineSHA: projected.NativeBaselineSHA,
		BillingAttributionSHA: attribution, BillingAttribution: "disjoint_source_epoch_deltas",
		DeclaredSources: 1, EpochIDs: []string{pin.EpochID}, InventoryCompleteAsserted: true, DeclaredSourcesRetired: true,
		RetirementBundleSHA: retirementCoverage.BundleSHA, ApprovedUnixNano: retiredAt + 3})
	balances := ownershipBalances(t, f.s)
	evidence, err := f.s.OpenDarkRestoreCutoverV1(ctx, f.owner.Token, inventory, inventorySHA, []darkrestore.VerifiedLocalHandoff{handoff}, retirements, approval, destination, attribution, native, nativePins)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateOfflineBackup(ctx, f.path)
	if err != nil || validated.SchemaVersion != 16 {
		t.Fatal("supported v16 offline backup rejected native evidence", validated, err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.OpenDarkRestoreCutoverV1(ctx, f.owner.Token, inventory, inventorySHA, []darkrestore.VerifiedLocalHandoff{handoff}, retirements, approval, destination, attribution, native, nativePins)
	if err != nil || !recovered.Replayed || recovered.TransitionSHA != evidence.TransitionSHA || !reflect.DeepEqual(recovered.NativeDocument, evidence.NativeDocument) {
		t.Fatal("native evidence changed after recovery", err)
	}
	if !reflect.DeepEqual(balances, ownershipBalances(t, reopened)) {
		t.Fatal("recovery changed representative balances")
	}
	restoredAccount, err := reopened.ClientUsage(ctx, evidence.ClientID)
	if err != nil || restoredAccount.ReservedHistoricalBytes != 14 || restoredAccount.Total != 0 || !restoredAccount.RemainingNativeBudgetKnown || restoredAccount.RemainingNativeBudgetBytes != 999986 {
		t.Fatal("backup lost enforced native budget", restoredAccount, err)
	}
	readiness, err := reopened.DarkRestoreCutoverReadinessV1(ctx, sha, source)
	if err != nil || readiness.ActivationAllowed || readiness.SubscriptionDelivered || !readiness.NativeBaselineComplete {
		t.Fatal("backup/reopen fabricated delivery", readiness, err)
	}
}
