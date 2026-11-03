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
	baselineSnapshot, err := f.s.CompleteDarkRestoreZeroBaselineV1(
		ctx, owner.Token, intent.Review.SourceSHA256, source.SourceID,
	)
	if err != nil || !baselineSnapshot.NativeBaselineComplete ||
		baselineSnapshot.NativeUplink == nil || *baselineSnapshot.NativeUplink != 0 ||
		baselineSnapshot.NativeDownlink == nil || *baselineSnapshot.NativeDownlink != 0 ||
		baselineSnapshot.NativeHolds == nil || *baselineSnapshot.NativeHolds != 0 {
		t.Fatal("zero native baseline failed", baselineSnapshot, err)
	}
	inventory := darkrestore.LocalHandoffInventoryV1{
		Version: 1, SourceSHA: intent.Review.SourceSHA256,
		SourceID: source.SourceID, Sources: []darkrestore.LocalHandoffSourcePinV1{sourcePin},
	}
	inventoryPin, err := darkrestore.DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	destinationSHA := strings.Repeat("1", 64)
	billingAttributionSHA := strings.Repeat("2", 64)
	approvalValue := darkrestore.HandoffCoverageApprovalV1{
		DestinationStateSHA: baselineSnapshot.StateSHA,
		NativeBaselineSHA:   baselineSnapshot.NativeBaselineSHA,
		Version:             1, InventorySHA: inventoryPin, SourceSHA: inventory.SourceSHA,
		SourceID: inventory.SourceID, Scope: darkrestore.HandoffCoverageScopeV1,
		Statement: darkrestore.HandoffCoverageStatementV1, DeclaredSources: 1,
		EpochIDs:                  []string{sourcePin.EpochID},
		InventoryCompleteAsserted: true, DeclaredSourcesRetired: true,
		ApprovedUnixNano:      time.Now().Add(-time.Second).UnixNano(),
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
		destinationSHA, billingAttributionSHA,
	); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale native baseline approval was accepted", err)
	}
	approval := businessSignedCoverage(t, approvalValue)
	evidence, err := f.s.OpenDarkRestoreCutoverV1(
		ctx, owner.Token, inventory, inventoryPin,
		[]darkrestore.VerifiedLocalHandoff{handoff}, approval,
		destinationSHA, billingAttributionSHA,
	)
	if err != nil || evidence.SettledHistoricalBytes != 14 ||
		evidence.RemainingBudgetBytes != 999986 || evidence.Replayed ||
		evidence.DestinationStateSHA != baselineSnapshot.StateSHA ||
		evidence.NativeBaselineSHA != baselineSnapshot.NativeBaselineSHA ||
		evidence.ImportReceiptSHA != baselineSnapshot.ImportReceiptSHA {
		t.Fatal("M5 evidence boundary failed", evidence, err)
	}
	replay, err := f.s.OpenDarkRestoreCutoverV1(
		ctx, owner.Token, inventory, inventoryPin,
		[]darkrestore.VerifiedLocalHandoff{handoff}, approval,
		destinationSHA, billingAttributionSHA,
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
	readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, inventory.SourceSHA, inventory.SourceID)
	if err != nil || !readiness.DeclaredInventoryCoverageVerified ||
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
}
