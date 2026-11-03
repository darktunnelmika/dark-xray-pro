//go:build linux

package business

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func businessSignedHandoff(t *testing.T, proof darkrestore.LocalSourceSettlement) (darkrestore.VerifiedLocalHandoff, darkrestore.LocalHandoffSourcePinV1) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	messageRaw, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	message := append([]byte("DARK-PRO-DARKRESTORE-LOCAL-HANDOFF-v1\x00"), messageRaw...)
	signed := darkrestore.SignedLocalHandoffV1{
		Version: 1, Settlement: proof, Signature: ed25519.Sign(private, message),
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "handoff.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fileSum := sha256.Sum256(raw)
	publicSum := sha256.Sum256(public)
	filePin := hex.EncodeToString(fileSum[:])
	publicPin := hex.EncodeToString(publicSum[:])
	verified, err := darkrestore.VerifySignedLocalHandoff(path, filePin, public)
	if err != nil {
		t.Fatal(err)
	}
	return verified, darkrestore.LocalHandoffSourcePinV1{
		EpochID: proof.Terminal.EpochID, Sequence: proof.Terminal.Sequence,
		ReceiptSHA: proof.Terminal.SHA, AuthoritySHA: proof.AuthoritySHA,
		JournalSHA: proof.JournalSHA, HandoffSHA: filePin,
		SignerPublicSHA: publicPin,
	}
}

func businessSignedCoverage(t *testing.T, approval darkrestore.HandoffCoverageApprovalV1) darkrestore.VerifiedHandoffCoverageApprovalV1 {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	approvalRaw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	message := append([]byte("DARK-PRO-DARKRESTORE-HANDOFF-COVERAGE-v1\x00"), approvalRaw...)
	signed := darkrestore.SignedHandoffCoverageApprovalV1{
		Version: 1, Approval: approval, Signature: ed25519.Sign(private, message),
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	documentSum := sha256.Sum256(raw)
	publicSum := sha256.Sum256(public)
	verified, err := darkrestore.VerifySignedHandoffCoverageApprovalV1(
		raw, hex.EncodeToString(documentSum[:]), public, hex.EncodeToString(publicSum[:]),
	)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func installSyntheticM5Delivery(t *testing.T, f *handoverFixture, round MeteredHealthRound, clientID string, after int64) LabSubscriptionSnapshot {
	t.Helper()
	tx, err := f.s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	created := time.Now().UnixNano()
	if created <= after {
		created = after + 1
	}
	prior, err := readSubscriptionDelivery(context.Background(), tx, clientID)
	if err != nil {
		t.Fatal("read prior delivery", err)
	}
	row := LabSubscriptionSnapshot{
		ID: routeTestID(952), EventID: routeTestID(953), ClientID: clientID,
		HandoverID: f.in.ID, RoundID: round.ID, NodeID: round.NodeID,
		InstallationID: round.InstallationID, Revision: 1,
		CreatedUnixNano: created, ExpiresUnixNano: created + int64(time.Second),
		RouteCount: 1,
	}
	digest := strings.Repeat("f", 64)
	if _, err = tx.Exec(`INSERT INTO pro_subscription_snapshots(`+subscriptionSnapshotColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, row.ClientID, row.HandoverID, row.RoundID, row.NodeID,
		row.InstallationID, row.Revision, row.CreatedUnixNano, row.ExpiresUnixNano,
		row.RouteCount, row.EventID, "owner", digest, round.EndpointSHA,
		round.ManifestSHA, digest, digest, []byte("synthetic-m5-delivery")); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO pro_subscription_snapshot_heads(client_id,revision,snapshot_id) VALUES(?,?,?)`, clientID, 1, row.ID); err != nil {
		t.Fatal(err)
	}
	row.DeliveryRevision, err = appendSubscriptionDelivery(context.Background(), tx, prior, HandoverSubscriptionScope, row)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestDarkRestoreM5ControlledSubscriptionCutover(t *testing.T) {
	ctx := context.Background()
	f, round, _ := labSubscriptionFixture(t, true)
	if err := f.s.EnableInitialApplyHealth(ctx); err != nil {
		t.Fatal(err)
	}
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
	creditBefore := ownershipBalances(t, f.s)
	intent := syntheticImportIntent(1)
	intent.Mappings[0].RepresentativeID = f.client.RepresentativeID
	quote, err := f.s.QuoteDarkRestoreImportV1(ctx, owner.Token, intent)
	if err != nil {
		t.Fatal(err)
	}
	request := DarkRestoreImportCommitV1{
		DarkRestoreImportIntentV1: intent, EventID: "darkrestore-m5-cutover-0001",
		QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix,
	}
	if _, err = f.s.commitVerifiedDarkRestore(ctx, owner.Token, request); err != nil {
		t.Fatal(err)
	}
	source := intent.Review.Clients[0]
	clientID := darkRestoreClientID(intent.Review.SourceSHA256, source.SourceID)

	mutation := SubscriptionTokenMutation{
		EventID: routeTestID(951), Action: "issue", ExpectedGeneration: 0,
	}
	if _, err = f.s.MutateLabSubscription(ctx, f.key, "owner", clientID, mutation); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("subscription escaped quarantine before M5 evidence", err)
	}
	var predecessorView string
	if err = f.s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='pro_usage_eligibility'`).Scan(&predecessorView); err != nil {
		t.Fatal(err)
	}
	if err = f.s.EnableDarkRestoreCutoverV1(ctx, owner.Token); err != nil {
		t.Fatal(err)
	}

	proof := darkrestore.LocalSourceSettlement{
		Scope: "single_local_source_epoch", SourceSHA: intent.Review.SourceSHA256,
		SourceID: source.SourceID, AuthoritySHA: strings.Repeat("b", 64),
		JournalSHA: strings.Repeat("c", 64),
		Terminal: uc.Receipt{
			EpochID: strings.Repeat("d", 32), Sequence: 1,
			SHA: strings.Repeat("e", 64),
		},
		PriorBytes: 7, Up: 3, Down: 4, Historical: 14, Remaining: 999986,
	}
	handoff, sourcePin := businessSignedHandoff(t, proof)
	if _, err = f.s.AttachSignedLocalSettlementV1(ctx, owner.Token, handoff); err != nil {
		t.Fatal(err)
	}

	destinationSHA := strings.Repeat("1", 64)
	beforeNative, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, intent.Review.SourceSHA256, source.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	native, nativePins := businessSignedNativeBaseline(t, beforeNative, destinationSHA)
	if _, err = f.s.CompleteDarkRestoreZeroBaselineV1(ctx, owner.Token, intent.Review.SourceSHA256, source.SourceID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("absence of activity fabricated coverage", err)
	}
	preparation, err := f.s.PrepareDarkRestoreNativeBaselineV1(ctx, owner.Token, intent.Review.SourceSHA256, source.SourceID, native, nativePins)
	baselineSnapshot := preparation.ProjectedSnapshot
	if err != nil || !baselineSnapshot.NativeBaselineComplete || preparation.BeforeStateSHA != beforeNative.StateSHA {
		t.Fatal("attested baseline projection failed", preparation, err)
	}
	stillUnknown, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, intent.Review.SourceSHA256, source.SourceID)
	if err != nil || stillUnknown.NativeBaselineComplete || stillUnknown.StateSHA != beforeNative.StateSHA {
		t.Fatal("preparation changed baseline", err)
	}

	inventory := darkrestore.LocalHandoffInventoryV1{
		Version: 1, SourceSHA: intent.Review.SourceSHA256,
		SourceID: source.SourceID, Sources: []darkrestore.LocalHandoffSourcePinV1{sourcePin},
	}
	inventoryPin, err := darkrestore.DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	billingAttributionSHA := strings.Repeat("2", 64)
	approvalValue := darkrestore.HandoffCoverageApprovalV1{
		DestinationStateSHA: baselineSnapshot.StateSHA,
		NativeBaselineSHA:   baselineSnapshot.NativeBaselineSHA,
		Version:             1, InventorySHA: inventoryPin, SourceSHA: inventory.SourceSHA,
		SourceID: inventory.SourceID, Scope: darkrestore.HandoffCoverageScopeV1,
		Statement: darkrestore.HandoffCoverageStatementV1, DeclaredSources: 1,
		EpochIDs:                  []string{sourcePin.EpochID},
		InventoryCompleteAsserted: true, DeclaredSourcesRetired: true,
		ApprovedUnixNano:      time.Now().UnixNano(),
		DestinationSHA:        destinationSHA,
		ImportReceiptSHA:      baselineSnapshot.ImportReceiptSHA,
		BillingAttributionSHA: billingAttributionSHA,
		BillingAttribution:    "disjoint_source_epoch_deltas",
	}
	staleApprovalValue := approvalValue
	staleApprovalValue.NativeBaselineSHA = strings.Repeat("3", 64)
	staleApproval := businessSignedCoverage(t, staleApprovalValue)
	if _, err = f.s.OpenDarkRestoreCutoverV1(
		ctx, owner.Token, inventory, inventoryPin,
		[]darkrestore.VerifiedLocalHandoff{handoff}, staleApproval,
		destinationSHA, billingAttributionSHA, native, nativePins,
	); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale native baseline approval was accepted", err)
	}
	nativeClaim, _, _, _, err := native.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	prematureValue := approvalValue
	prematureValue.ApprovedUnixNano = nativeClaim.MeasuredThroughNano - 1
	premature := businessSignedCoverage(t, prematureValue)
	if _, err = f.s.OpenDarkRestoreCutoverV1(ctx, owner.Token, inventory, inventoryPin, []darkrestore.VerifiedLocalHandoff{handoff}, premature, destinationSHA, billingAttributionSHA, native, nativePins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("approval before measurement accepted", err)
	}
	approval := businessSignedCoverage(t, approvalValue)
	// Inject a failure between receipt insertion and baseline completion.
	if _, err = f.s.db.Exec(`CREATE TRIGGER synthetic_m5_baseline_failure BEFORE UPDATE OF baseline_complete ON pro_usage_accounts BEGIN SELECT RAISE(ABORT,'synthetic baseline failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.OpenDarkRestoreCutoverV1(ctx, owner.Token, inventory, inventoryPin, []darkrestore.VerifiedLocalHandoff{handoff}, approval, destinationSHA, billingAttributionSHA, native, nativePins); err == nil {
		t.Fatal("injected baseline failure ignored")
	}
	var evidenceCount int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM darkrestore_cutover_evidence_v1`).Scan(&evidenceCount); err != nil || evidenceCount != 0 {
		t.Fatal("partial evidence survived rollback", err)
	}
	afterFailure, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, inventory.SourceSHA, inventory.SourceID)
	if err != nil || afterFailure.NativeBaselineComplete || afterFailure.StateSHA != beforeNative.StateSHA {
		t.Fatal("failed transaction changed baseline", err)
	}
	var reservationCount int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM pro_usage_historical_budgets`).Scan(&reservationCount); err != nil || reservationCount != 0 {
		t.Fatal("partial budget reservation survived rollback", err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER synthetic_m5_baseline_failure`); err != nil {
		t.Fatal(err)
	}
	evidence, err := f.s.OpenDarkRestoreCutoverV1(
		ctx, owner.Token, inventory, inventoryPin,
		[]darkrestore.VerifiedLocalHandoff{handoff}, approval,
		destinationSHA, billingAttributionSHA, native, nativePins,
	)
	if err != nil || evidence.SettledHistoricalBytes != 14 ||
		evidence.RemainingBudgetBytes != 999986 || evidence.Replayed ||
		evidence.DestinationStateSHA != baselineSnapshot.StateSHA ||
		evidence.NativeBaselineSHA != baselineSnapshot.NativeBaselineSHA ||
		evidence.ImportReceiptSHA != baselineSnapshot.ImportReceiptSHA {
		t.Fatal("M5 evidence boundary failed", evidence, err)
	}
	changedBudgetEvidence := evidence
	changedBudgetEvidence.SettledHistoricalBytes++
	if err = verifyCutoverHistoricalBudget(ctx, f.s.db, &changedBudgetEvidence); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("budget reservation accepted foreign receipt history", err)
	}
	replay, err := f.s.OpenDarkRestoreCutoverV1(
		ctx, owner.Token, inventory, inventoryPin,
		[]darkrestore.VerifiedLocalHandoff{handoff}, approval,
		destinationSHA, billingAttributionSHA, native, nativePins,
	)
	if err != nil || !replay.Replayed ||
		replay.TransitionOpenedUnixNano != evidence.TransitionOpenedUnixNano {
		t.Fatal("M5 evidence replay changed boundary", replay, err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("M5 evidence failed reopen validation", err)
	}
	if _, err = f.s.db.Exec(`UPDATE darkrestore_cutover_evidence_v1 SET settled_historical_bytes='0' WHERE client_id=?`, clientID); err == nil {
		t.Fatal("immutable M5 evidence was mutable")
	}
	if _, err = f.s.db.Exec(`UPDATE clients SET expires_unix=expires_unix+1 WHERE id=?`, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.MutateLabSubscription(ctx, f.key, "owner", clientID, mutation); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("state changed after approval but subscription issue was accepted", err)
	}
	if _, err = f.s.db.Exec(`UPDATE clients SET expires_unix=expires_unix-1 WHERE id=?`, clientID); err != nil {
		t.Fatal(err)
	}

	issued, err := f.s.MutateLabSubscription(ctx, f.key, "owner", clientID, mutation)
	if err != nil || issued.Generation != 1 || issued.State != "active" {
		t.Fatal("controlled subscription issue failed", issued, err)
	}
	if _, err = f.s.FinalizeDarkRestoreCutoverV1(ctx, owner.Token, clientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("cutover finalized before subscription delivery", err)
	}
	// This synthetic row exercises the authoritative delivery journal contract.
	// It is not evidence of a real production subscription or source retirement.
	snapshot := installSyntheticM5Delivery(t, f, round, clientID, evidence.TransitionOpenedUnixNano)
	if snapshot.DeliveryRevision == 0 || snapshot.CreatedUnixNano <= evidence.TransitionOpenedUnixNano {
		t.Fatal("delivery did not follow evidence boundary", snapshot)
	}
	final, err := f.s.FinalizeDarkRestoreCutoverV1(ctx, owner.Token, clientID)
	if err != nil || final.SubscriptionRevision != snapshot.DeliveryRevision ||
		final.RemainingBudgetBytes != 999986 || final.TransitionSHA != evidence.TransitionSHA ||
		final.Replayed {
		t.Fatal("cutover finalization failed", final, err)
	}
	finalReplay, err := f.s.FinalizeDarkRestoreCutoverV1(ctx, owner.Token, clientID)
	if err != nil || !finalReplay.Replayed {
		t.Fatal("cutover finalization replay failed", finalReplay, err)
	}
	enabled, err := f.s.SetClientStateScoped(ctx, SetClientState{
		EventID: "darkrestore-m5-enable-0001", ClientID: clientID,
		Action: "enable", Reason: "synthetic accepted cutover",
	}, f.client.RepresentativeID)
	if err != nil || enabled.DeploymentState != "pending_node_apply" {
		t.Fatal("accepted cutover remained blocked", enabled, err)
	}
	account, err := f.s.ClientUsage(ctx, clientID)
	if err != nil || account.ReservedHistoricalBytes != 14 || account.Total != 0 || account.Limit != 1000000 || !account.RemainingNativeBudgetKnown || account.RemainingNativeBudgetBytes != 999986 {
		t.Fatal("actual native budget ignored settled history", account, err)
	}
	readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, inventory.SourceSHA, inventory.SourceID)
	if err != nil || !readiness.SettledHistoryBudgetReserved || !readiness.DeclaredInventoryCoverageVerified ||
		!readiness.IndependentCoverageAssertionVerified ||
		readiness.GlobalSourceCustodyVerified ||
		!readiness.NativeBaselineComplete || !readiness.SubscriptionDelivered ||
		!readiness.CutoverReceiptRecorded || !readiness.ActivationAllowed ||
		!readiness.CutoverVerified || readiness.FiniteQuotaRemainingBytes != "999986" {
		t.Fatal("M5 readiness misrepresented evidence scope", readiness, err)
	}
	usage, err := f.s.DarkRestoreUsageV1(ctx, clientID)
	if err != nil || usage.HistoricalBytes != "14" ||
		usage.PostMigrationBytes != "0" || usage.TotalBytes != "14" ||
		!usage.SourceEpochSettled || !usage.CutoverVerified {
		t.Fatal("M5 usage projection incorrect", usage, err)
	}
	if _, err = f.s.db.Exec(`DELETE FROM darkrestore_cutover_receipts_v1 WHERE client_id=?`, clientID); err == nil {
		t.Fatal("immutable M5 final receipt was deletable")
	}
	if !reflect.DeepEqual(creditBefore, ownershipBalances(t, f.s)) {
		t.Fatal("M5 transition changed representative credit")
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("final M5 state failed backup/reopen validation", err)
	}
	// Reopen retains the exact signed native bundle and
	// immutable financial evidence. No installed LAB archive is restored here.
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal("native attestation reopen failed", err)
	}
	defer reopened.Close()
	recovered, err := reopened.OpenDarkRestoreCutoverV1(ctx, owner.Token, inventory, inventoryPin, []darkrestore.VerifiedLocalHandoff{handoff}, approval, destinationSHA, billingAttributionSHA, native, nativePins)
	if err != nil || !recovered.Replayed || recovered.TransitionSHA != evidence.TransitionSHA || !reflect.DeepEqual(recovered.NativeDocument, evidence.NativeDocument) {
		t.Fatal("late replay lost immutable native proof", err)
	}
	// Historical pre-attestation receipts remain decodable, never authoritative.
	legacy := evidence
	legacy.NativeDocument = nil
	legacy.NativePublicKey = nil
	legacy.NativePins = darkrestore.NativeBaselinePinsV1{}
	legacy.TransitionSHA, err = darkRestoreTransitionSHA(legacy.ClientID, legacy.InventorySHA256, legacy.CoverageApprovalSHA256, legacy.DestinationStateSHA, legacy.NativeBaselineSHA, "", legacy.TransitionOpenedUnixNano)
	if err != nil {
		t.Fatal(err)
	}
	legacyRaw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacyRaw), "native_document") || strings.Contains(string(legacyRaw), "native_pins") {
		t.Fatal("legacy encoding was rewritten")
	}
	var legacyDecoded DarkRestoreCutoverEvidenceReceiptV1
	if err = decodeCutoverEvidence(legacyRaw, &legacyDecoded); err != nil {
		t.Fatal("legacy historical receipt rejected", err)
	}
	if _, _, err = cutoverSnapshotMatchesEvidence(ctx, reopened.db, &legacyDecoded); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("legacy receipt authorized a mutation", err)
	}
	// Simulate a predecessor schema only inside this disposable fixture. A serving
	// startup must refuse it, while the immutable M5 financial evidence is retained.
	var historicalReceipt []byte
	if err = reopened.db.QueryRow(`SELECT receipt_json FROM darkrestore_cutover_evidence_v1 WHERE client_id=?`, clientID).Scan(&historicalReceipt); err != nil {
		t.Fatal(err)
	}
	tx, err := reopened.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{`DROP VIEW pro_usage_eligibility`, `DROP TABLE pro_usage_historical_budgets`, predecessorView} {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = reopened.EnableDarkRestoreCutoverV1(ctx, owner.Token); !errors.Is(err, ErrConflict) {
		t.Fatal("predecessor evidence was silently migrated", err)
	}
	var unchangedReceipt []byte
	if err = reopened.db.QueryRow(`SELECT receipt_json FROM darkrestore_cutover_evidence_v1 WHERE client_id=?`, clientID).Scan(&unchangedReceipt); err != nil || !reflect.DeepEqual(historicalReceipt, unchangedReceipt) {
		t.Fatal("refused upgrade changed history", err)
	}
	if err = validateDarkRestoreImportData(ctx, reopened.db); err != nil {
		t.Fatal("historical evidence was lost", err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if unsafe, e := Open(f.path); !errors.Is(e, ErrUnavailable) {
		if unsafe != nil {
			unsafe.Close()
		}
		t.Fatal("active predecessor reopened without historical budget enforcement", e)
	}

}

// All inventory, measurement and fence digests here are SYNTHETIC test anchors.
// They are not real LAB/production native-supervisor evidence.
func businessSignedNativeBaseline(t *testing.T, before DarkRestoreCutoverSnapshotV1, destinationSHA string, mutations ...func(*darkrestore.NativeBaselineV1)) (darkrestore.VerifiedNativeBaselineV1, darkrestore.NativeBaselinePinsV1) {
	t.Helper()
	now := time.Now().UnixNano()
	proof := darkrestore.NativeBaselineV1{Version: 1, Scope: "destination_identity_lifetime_with_active_fence",
		DestinationSHA: destinationSHA, SourceSHA: before.SourceSHA, SourceID: before.SourceID,
		ClientID: before.ClientID, ClientUUID: before.ClientUUID, ImportReceiptSHA: before.ImportReceiptSHA,
		BeforeStateSHA: before.StateSHA, BeforeNativeSHA: before.NativeBaselineSHA,
		InventorySHA: strings.Repeat("7", 64), MeasurementSHA: strings.Repeat("8", 64), FenceSHA: strings.Repeat("9", 64),
		CoverageFromNano: before.ClientCreatedUnix * 1_000_000_000, MeasuredThroughNano: now, FenceUntilNano: now + int64(2*time.Minute),
		Uplink: *before.NativeUplink, Downlink: *before.NativeDownlink, Complete: true, Fenced: true}
	for _, change := range mutations {
		change(&proof)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	message := append([]byte("DARK-PRO-NATIVE-BASELINE-ATTESTATION-v1\x00"), body...)
	raw, err := json.Marshal(darkrestore.SignedNativeBaselineV1{Version: 1, Baseline: proof, Signature: ed25519.Sign(private, message)})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	d, k := sha256.Sum256(raw), sha256.Sum256(public)
	pins := darkrestore.NativeBaselinePinsV1{DocumentSHA: hex.EncodeToString(d[:]), PublicKeySHA: hex.EncodeToString(k[:]), DestinationSHA: destinationSHA, InventorySHA: proof.InventorySHA, MeasurementSHA: proof.MeasurementSHA, FenceSHA: proof.FenceSHA}
	verified, err := darkrestore.VerifySignedNativeBaselineV1(raw, public, pins)
	if err != nil {
		t.Fatal(err)
	}
	return verified, pins
}
