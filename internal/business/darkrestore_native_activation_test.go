package business

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type preparedNativeActivationFixture struct {
	preparedPromotionFixture
	acceptance         DarkRestoreNativeApplyAcceptanceReceiptV1
	snapshot           LabSubscriptionSnapshot
	activationEvidence DarkRestoreNativeActivationEvidenceV1
	pins               DarkRestoreNativeActivationPinsV1
}

func newPreparedNativeActivationFixture(t *testing.T) preparedNativeActivationFixture {
	t.Helper()
	ctx := context.Background()
	f, _, result, _, applyPins := preparedNativeApplyAcceptanceFixture(t)
	acceptance, err := f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, DarkRestoreNativeApplyAcceptanceInputV1{
		EventID: "m5-native-activation-apply-0001", ClientID: f.evidence.ClientID, InboundID: 802, RoundID: result.round.ID, Pins: applyPins,
	})
	if err != nil {
		f.s.Close()
		t.Fatal(err)
	}
	snapshot, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 802, InitialApplySubscriptionMaterialization{
		EventID: "m5-native-activation-delivery-0001", ClientID: f.evidence.ClientID, RoundID: result.round.ID, InitialApplySHA: result.state.InitialApplySHA,
	}, healthEndpoint())
	if err != nil {
		f.s.Close()
		t.Fatal(err)
	}
	tx, err := f.s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		f.s.Close()
		t.Fatal(err)
	}
	evidence, err := projectDarkRestoreNativeActivation(ctx, tx, f.evidence.ClientID)
	tx.Rollback()
	if err != nil {
		f.s.Close()
		t.Fatal(err)
	}
	return preparedNativeActivationFixture{
		preparedPromotionFixture: f, acceptance: acceptance, snapshot: snapshot,
		activationEvidence: evidence, pins: DarkRestoreNativeActivationPinsV1(evidence),
	}
}

func TestDarkRestoreNativeActivationRecordReplayReopenAndFinalGate(t *testing.T) {
	ctx := context.Background()
	f := newPreparedNativeActivationFixture(t)
	defer f.s.Close()
	clientBefore, _ := f.s.Client(ctx, f.evidence.ClientID)
	balancesBefore := ownershipBalances(t, f.s)
	if err := requireDarkRestoreFinalCutoverTx(ctx, f.s.db, f.preparedPromotionFixture.evidence.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("initial delivery opened final gate", err)
	}
	in := DarkRestoreNativeActivationInputV1{EventID: "m5-native-activation-0001", ClientID: f.preparedPromotionFixture.evidence.ClientID, Pins: f.pins}
	receipt, err := f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, in)
	if err != nil || receipt.Replayed || !receipt.AuthorizesFinalCutover || receipt.ActivationSHA == "" || !reflect.DeepEqual(receipt.Evidence, f.activationEvidence) {
		t.Fatal("record exact activation", receipt, err)
	}
	replay, err := f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, in)
	if err != nil || !replay.Replayed {
		t.Fatal("idempotent activation replay", replay, err)
	}
	conflict := in
	conflict.EventID = "m5-native-activation-conflict-0001"
	if _, err = f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting activation replay accepted", err)
	}
	if _, err = f.s.RecordDarkRestoreNativeActivationV1(ctx, f.rep.Token, in); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative recorded owner activation", err)
	}
	if err = requireDarkRestoreFinalCutoverTx(ctx, f.s.db, f.preparedPromotionFixture.evidence.ClientID); err != nil {
		t.Fatal("exact activation did not open final gate", err)
	}
	readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, f.preparedPromotionFixture.evidence.SourceSHA256, f.preparedPromotionFixture.evidence.SourceID)
	if err != nil || !readiness.NativeApplyRecorded || !readiness.InitialApplySubscriptionDelivered || !readiness.NativeActivationAuthorized || !readiness.ActivationAllowed || !readiness.CutoverVerified || strings.Contains(strings.Join(readiness.ActivationBlockers, ","), "native_activation_not_authorized") {
		t.Fatal("readiness did not expose exact activation", readiness, err)
	}
	issued, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.preparedPromotionFixture.evidence.ClientID, SubscriptionTokenMutation{EventID: "m5-native-activation-issuer-0001", Action: "issue"})
	if err != nil || issued.Generation != 1 || issued.State != "active" {
		t.Fatal("final gate did not open existing issuer", issued, err)
	}
	clientAfter, _ := f.s.Client(ctx, f.preparedPromotionFixture.evidence.ClientID)
	if !reflect.DeepEqual(clientBefore, clientAfter) || !reflect.DeepEqual(balancesBefore, ownershipBalances(t, f.s)) {
		t.Fatal("activation changed client or financial state")
	}
	if planCount(t, f.s, "pro_initial_snapshots") != 1 || planCount(t, f.s, "pro_delivery_events") != 1 {
		t.Fatal("activation created another runtime delivery")
	}
	if _, err = f.s.db.Exec(`UPDATE darkrestore_native_activations_v1 SET actor_user_id=actor_user_id WHERE client_id=?`, in.ClientID); err == nil {
		t.Fatal("activation was mutable")
	}
	if _, err = f.s.db.Exec(`DELETE FROM darkrestore_native_activations_v1 WHERE client_id=?`, in.ClientID); err == nil {
		t.Fatal("activation was deletable")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	validated, backupErr := ValidateOfflineBackup(ctx, f.path)
	if backupErr != nil || validated.TableCounts["darkrestore_native_activations_v1"] != 1 {
		t.Fatal("offline backup rejected activation", validated, backupErr)
	}
	if err = ValidateOfflineBackupAssets(ctx, f.path, f.key); err != nil {
		t.Fatal("encrypted assets rejected activation", err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal("reopen rejected activation", err)
	}
	recovered, err := readDarkRestoreNativeActivation(ctx, f.s.db, in.ClientID)
	if err != nil || recovered == nil || recovered.Replayed || recovered.ActivationSHA != receipt.ActivationSHA || !reflect.DeepEqual(recovered.Evidence, receipt.Evidence) {
		t.Fatal("reopen changed activation", recovered, err)
	}
}

func TestDarkRestoreNativeActivationRollbackStalePinsAndState(t *testing.T) {
	ctx := context.Background()
	f := newPreparedNativeActivationFixture(t)
	defer f.s.Close()
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureDarkRestoreNativeActivationSchema(ctx, tx); err != nil || tx.Commit() != nil {
		t.Fatal("test schema", err)
	}
	if _, err = f.s.db.Exec(`CREATE TRIGGER synthetic_native_activation_failure BEFORE INSERT ON darkrestore_native_activations_v1 BEGIN SELECT RAISE(ABORT,'synthetic activation failure'); END`); err != nil {
		t.Fatal(err)
	}
	in := DarkRestoreNativeActivationInputV1{EventID: "m5-native-activation-rollback-0001", ClientID: f.preparedPromotionFixture.evidence.ClientID, Pins: f.pins}
	if _, err = f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, in); err == nil {
		t.Fatal("synthetic insert failure committed activation")
	}
	if planCount(t, f.s, "darkrestore_native_activations_v1") != 0 {
		t.Fatal("failed activation left a receipt")
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER synthetic_native_activation_failure`); err != nil {
		t.Fatal(err)
	}
	bad := in
	bad.EventID = "m5-native-activation-stale-pin-0001"
	bad.Pins.BillingAttributionSHA = strings.Repeat("0", 64)
	if _, err = f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, bad); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale billing-attribution pin accepted", err)
	}
	client, err := f.s.Client(ctx, in.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: "m5-native-activation-renewal-0001", ClientID: in.ClientID, AdditionalVolumeBytes: 17, Reason: "Synthetic current-payer renewal invalidates activation"}, client.RepresentativeID); err != nil {
		t.Fatal(err)
	}
	in.EventID = "m5-native-activation-stale-state-0001"
	if _, err = f.s.RecordDarkRestoreNativeActivationV1(ctx, f.owner.Token, in); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale entitlement accepted", err)
	}
	if planCount(t, f.s, "darkrestore_native_activations_v1") != 0 || requireDarkRestoreFinalCutoverTx(ctx, f.s.db, in.ClientID) == nil {
		t.Fatal("stale activation opened final gate")
	}
}
