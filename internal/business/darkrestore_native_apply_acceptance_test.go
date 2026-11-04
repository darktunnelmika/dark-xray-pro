package business

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func preparedNativeApplyAcceptanceFixture(t *testing.T) (preparedPromotionFixture, DarkRestorePreparedPromotionReceiptV1, preparedApplyResult, DarkRestoreNativeApplyEvidenceV1, DarkRestoreNativeApplyPinsV1) {
	t.Helper()
	f := newPreparedPromotionFixture(t)
	preview, input := f.reviewed(t)
	transition, err := f.s.CommitDarkRestorePreparedPromotionV1(context.Background(), f.key, f.owner.Token, preview.ReviewSHA256, input)
	if err != nil {
		t.Fatal(err)
	}
	result := completePreparedNativeApply(t, &f)
	evidence, pins := preparedNativeApplyEvidence(t, &f, transition, result)
	return f, transition, result, evidence, pins
}

func TestDarkRestoreNativeApplyAcceptanceRecordReplayRecoveryAndClosedIssuer(t *testing.T) {
	ctx := context.Background()
	f, _, result, evidence, pins := preparedNativeApplyAcceptanceFixture(t)
	defer f.s.Close()
	beforeBalances := ownershipBalances(t, f.s)
	clientBefore, _ := f.s.Client(ctx, f.evidence.ClientID)
	in := DarkRestoreNativeApplyAcceptanceInputV1{EventID: "m5-native-apply-acceptance-0001", ClientID: f.evidence.ClientID, InboundID: 802, RoundID: result.round.ID, Pins: pins}
	materialize := InitialApplySubscriptionMaterialization{EventID: "m5-native-apply-delivery-0001", ClientID: in.ClientID, RoundID: result.round.ID, InitialApplySHA: result.state.InitialApplySHA}
	if _, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 802, materialize, healthEndpoint()); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("initial subscription materialized before Apply acceptance", err)
	}
	receipt, err := f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, in)
	if err != nil || receipt.Replayed || !receipt.NativeApplyRecorded || receipt.SubscriptionDelivered || receipt.SubscriptionActivated || receipt.AuthorizesFurtherMutation || receipt.EvidenceSHA != pins.DocumentSHA || !reflect.DeepEqual(receipt.Evidence, evidence) {
		t.Fatal("record exact native apply acceptance", receipt, err)
	}
	replay, err := f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, in)
	if err != nil || !replay.Replayed {
		t.Fatal("idempotent native apply acceptance replay", replay, err)
	}
	if _, err = f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, DarkRestoreNativeApplyAcceptanceInputV1{EventID: "m5-native-apply-acceptance-conflict-0001", ClientID: in.ClientID, InboundID: in.InboundID, RoundID: in.RoundID, Pins: pins}); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting replay accepted", err)
	}
	if _, err = f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.rep.Token, in); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative recorded owner acceptance", err)
	}
	snapshot, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 802, materialize, healthEndpoint())
	if err != nil || !snapshot.Current || snapshot.Scope != InitialApplyHealthScope || snapshot.DeliveryRevision != 1 || snapshot.CreatedUnixNano <= receipt.RecordedUnixNano {
		t.Fatal("accepted Apply did not produce exact initial delivery", snapshot, err)
	}
	tx, err := f.s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	row, err := readInitialSubscriptionSnapshot(ctx, tx, "id", snapshot.ID)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	raw, err := openInitialSubscriptionSnapshot(f.key, row)
	tx.Rollback()
	if err != nil || subscriptionBodySHA(raw) != evidence.PreparedContentSHA {
		t.Fatal("initial delivery did not preserve prepared bytes", err)
	}
	readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, f.evidence.SourceSHA256, f.evidence.SourceID)
	if err != nil || !readiness.NativeApplyRecorded || !readiness.InitialApplySubscriptionDelivered || !readiness.SubscriptionDelivered || readiness.ActivationAllowed || !strings.Contains(strings.Join(readiness.ActivationBlockers, ","), "native_activation_not_authorized") {
		t.Fatal("readiness did not separate Apply, delivery and activation", readiness, err)
	}
	if _, err = f.s.db.Exec(`UPDATE darkrestore_native_apply_acceptances_v1 SET actor_user_id=actor_user_id WHERE client_id=?`, in.ClientID); err == nil {
		t.Fatal("native apply acceptance was mutable")
	}
	if _, err = f.s.db.Exec(`DELETE FROM darkrestore_native_apply_acceptances_v1 WHERE client_id=?`, in.ClientID); err == nil {
		t.Fatal("native apply acceptance was deletable")
	}
	clientAfter, _ := f.s.Client(ctx, f.evidence.ClientID)
	if !reflect.DeepEqual(clientBefore, clientAfter) || !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
		t.Fatal("acceptance changed client or financial state")
	}
	if err = requireDarkRestoreFinalCutoverTx(ctx, f.s.db, in.ClientID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("accepted delivery opened activation without a separate transition", err)
	}
	if _, err = f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", in.ClientID, SubscriptionTokenMutation{EventID: "m5-native-acceptance-issuer-0001", Action: "issue"}); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("acceptance opened imported credential issuer", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if validated, backupErr := ValidateOfflineBackup(ctx, f.path); backupErr != nil || validated.TableCounts["darkrestore_native_apply_acceptances_v1"] != 1 {
		t.Fatal("offline backup rejected acceptance", validated, backupErr)
	}
	if err = ValidateOfflineBackupAssets(ctx, f.path, f.key); err != nil {
		t.Fatal("encrypted backup assets rejected acceptance", err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal("reopen rejected acceptance", err)
	}
	recovered, err := readDarkRestoreNativeApplyAcceptance(ctx, f.s.db, in.ClientID)
	if err != nil || recovered == nil || recovered.Replayed || recovered.EvidenceSHA != receipt.EvidenceSHA || !reflect.DeepEqual(recovered.Evidence, receipt.Evidence) {
		t.Fatal("reopen changed acceptance", recovered, err)
	}
}

func TestDarkRestoreNativeApplyAcceptanceStateDriftBlocksInitialDelivery(t *testing.T) {
	ctx := context.Background()
	f, _, result, _, pins := preparedNativeApplyAcceptanceFixture(t)
	defer f.s.Close()
	in := DarkRestoreNativeApplyAcceptanceInputV1{EventID: "m5-native-apply-acceptance-drift-0001", ClientID: f.evidence.ClientID, InboundID: 802, RoundID: result.round.ID, Pins: pins}
	if _, err := f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, in); err != nil {
		t.Fatal(err)
	}
	client, err := f.s.Client(ctx, in.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: "m5-native-apply-acceptance-renewal-0001", ClientID: in.ClientID, AdditionalVolumeBytes: 17, Reason: "Synthetic current-payer renewal invalidates acceptance"}, client.RepresentativeID); err != nil {
		t.Fatal(err)
	}
	materialize := InitialApplySubscriptionMaterialization{EventID: "m5-native-apply-delivery-drift-0001", ClientID: in.ClientID, RoundID: result.round.ID, InitialApplySHA: result.state.InitialApplySHA}
	if _, err = f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 802, materialize, healthEndpoint()); err == nil {
		t.Fatal("stale entitlement delivered initial subscription", err)
	}
	if planCount(t, f.s, "pro_initial_snapshots") != 0 || planCount(t, f.s, "pro_delivery_events") != 0 {
		t.Fatal("stale delivery left a partial journal row")
	}
}

func requireDarkRestoreFinalCutoverTx(ctx context.Context, db *sql.DB, client string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return requireDarkRestoreFinalCutover(ctx, tx, client)
}

func TestDarkRestoreNativeApplyAcceptanceRollbackAndStaleEvidence(t *testing.T) {
	ctx := context.Background()
	f, _, result, _, pins := preparedNativeApplyAcceptanceFixture(t)
	defer f.s.Close()
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureDarkRestoreNativeApplyAcceptanceSchema(ctx, tx); err != nil || tx.Commit() != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`CREATE TRIGGER synthetic_native_apply_acceptance_failure BEFORE INSERT ON darkrestore_native_apply_acceptances_v1 BEGIN SELECT RAISE(ABORT,'synthetic acceptance failure'); END`); err != nil {
		t.Fatal(err)
	}
	in := DarkRestoreNativeApplyAcceptanceInputV1{EventID: "m5-native-apply-acceptance-rollback-0001", ClientID: f.evidence.ClientID, InboundID: 802, RoundID: result.round.ID, Pins: pins}
	if _, err = f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, in); err == nil {
		t.Fatal("synthetic insert failure committed acceptance")
	}
	var count int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM darkrestore_native_apply_acceptances_v1`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER synthetic_native_apply_acceptance_failure`); err != nil {
		t.Fatal(err)
	}
	bad := in
	bad.EventID = "m5-native-apply-acceptance-stale-0001"
	bad.Pins.DocumentSHA = strings.Repeat("0", 64)
	if _, err = f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, bad); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale document pin accepted", err)
	}
	ownershipMove(t, f.ownershipFixture, "m5-native-acceptance-payer-drift-0001", ClientOwnershipIntent{ClientIDs: []string{f.evidence.ClientID}, RepresentativeID: "rep-third", Reason: "Synthetic payer drift before acceptance"})
	in.EventID = "m5-native-apply-acceptance-payer-drift-0001"
	if _, err = f.s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, f.key, f.owner.Token, in); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale payer accepted", err)
	}
}
