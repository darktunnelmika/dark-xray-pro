package business

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type preparedPromotionFixture struct {
	ownershipFixture
	evidence DarkRestoreCutoverEvidenceReceiptV1
	key      []byte
	node     MeteredRouteNode
	poll     mx.TransactionPoll
	input    DarkRestorePreparedPromotionInputV1
}

func newPreparedPromotionFixture(t *testing.T) preparedPromotionFixture {
	t.Helper()
	return preparedPromotionWithFiles(t, nil)
}

func PreparedPromotionHTTPFixture(t *testing.T) (*Store, string, []byte, PreparedHandoffFiles, MeteredRouteNode, string) {
	var files PreparedHandoffFiles
	f := preparedPromotionWithFiles(t, &files)
	return f.s, f.evidence.ClientID, f.key, files, f.node, ownershipPassword
}

func preparedPromotionWithFiles(t *testing.T, files *PreparedHandoffFiles) preparedPromotionFixture {
	t.Helper()
	f, evidence, key, _ := preparedCutoverWithFilesAndInitialApply(t, files, true)
	ctx := context.Background()
	node := MeteredRouteNode{NodeID: "m5-prepared-promotion-node", InstallationID: strings.Repeat("8", 32), BinarySHA: strings.Repeat("9", 64)}
	profile := nativeInput()
	profile.ID = 802
	plan := NativeInboundPlanInput{EventID: "m5-prepared-promotion-plan-0001", Profile: profile, Reason: "Synthetic exact prepared promotion profile",
		Nodes: []NativeInboundPlanNode{{NodeID: node.NodeID, InstallationID: node.InstallationID, ListenIP: "127.0.0.1", DirectPort: 21186, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}}}
	if _, err := f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
		t.Fatal("save promotable plan", err)
	}
	if err := f.s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{node}); err != nil {
		t.Fatal("reserve promotable node", err)
	}
	poll := mx.TransactionPoll{NodeID: node.NodeID, InstallationID: node.InstallationID, BinarySHA: node.BinarySHA, At: time.Now().UnixNano(),
		Status: mx.TransactionStatus{Instance: routeTestInstance, Usage: &mx.MeteredCondition{}, Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}}
	poll = mx.SignMeteredTransactionPoll(routeTestToken, poll)
	if _, found, err := f.s.PollMeteredRouteCommand(ctx, key, routeTestToken, poll); err != nil || found {
		t.Fatal("empty authenticated poll", found, err)
	}
	delivery := DarkRestorePreparedDeliveryInputV1{EventID: "m5-prepared-promotion-delivery-0001", InboundID: 802, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}
	if _, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, evidence.ClientID, delivery); err != nil {
		t.Fatal("deliver promotable prepared subscription", err)
	}
	input := DarkRestorePreparedPromotionInputV1{EventID: "m5-prepared-promotion-transition-0001", ClientID: evidence.ClientID, InboundID: 802,
		Promotion: NativeInboundPlanPromotionInput{EventID: "m5-prepared-promotion-native-0001", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{evidence.ClientID}, Reason: "Owner-reviewed synthetic quarantine to native promotion"}}
	return preparedPromotionFixture{ownershipFixture: f, evidence: evidence, key: key, node: node, poll: poll, input: input}
}

func (f *preparedPromotionFixture) reviewed(t *testing.T) (DarkRestorePreparedPromotionPreviewV1, DarkRestorePreparedPromotionInputV1) {
	t.Helper()
	preview, err := f.s.PreviewDarkRestorePreparedPromotionV1(context.Background(), f.key, f.owner.Token, f.input)
	if err != nil || !preview.Eligible || len(preview.Blockers) != 0 || !routeSHA.MatchString(preview.ReviewSHA256) || !routeSHA.MatchString(preview.Promotion.ReviewSHA256) || preview.AuthorizesMutation {
		t.Fatal("prepared promotion preview", preview, err)
	}
	in := f.input
	in.Promotion.ReviewSHA256 = preview.Promotion.ReviewSHA256
	return preview, in
}

func TestDarkRestorePreparedPromotionAtomicReplayReopenAndNoApply(t *testing.T) {
	ctx := context.Background()
	f := newPreparedPromotionFixture(t)
	defer f.s.Close()
	beforeBalances := ownershipBalances(t, f.s)
	beforeClient, _ := f.s.Client(ctx, f.evidence.ClientID)
	if _, err := f.s.PreviewDarkRestorePreparedPromotionV1(ctx, f.key, f.rep.Token, f.input); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative reviewed private transition", err)
	}
	preview, input := f.reviewed(t)
	stillDisabled, _ := f.s.Client(ctx, f.evidence.ClientID)
	if stillDisabled.DeploymentState != "disabled" || planCount(t, f.s, "native_inbound_plan_promotions") != 0 {
		t.Fatal("preview mutated quarantine or native plan")
	}
	receipt, err := f.s.CommitDarkRestorePreparedPromotionV1(ctx, f.key, f.owner.Token, preview.ReviewSHA256, input)
	if err != nil || receipt.Replayed || !receipt.NativePromotionRecorded || receipt.NativeApplyRecorded || receipt.SubscriptionActivated || receipt.AuthorizesFurtherMutation || receipt.PreStateSHA != preview.PreStateSHA || receipt.PostStateSHA != preview.PostStateSHA {
		t.Fatal("atomic prepared promotion", receipt, err)
	}
	afterClient, err := f.s.Client(ctx, f.evidence.ClientID)
	if err != nil || afterClient.DeploymentState != "pending_node_apply" || afterClient.UUID != beforeClient.UUID || afterClient.ExpiresUnix != beforeClient.ExpiresUnix || afterClient.RepresentativeID != beforeClient.RepresentativeID {
		t.Fatal("promotion changed identity or payer", afterClient, err)
	}
	account, err := f.s.ClientUsage(ctx, f.evidence.ClientID)
	if err != nil || !account.Eligible || account.Total != 0 || account.ReservedHistoricalBytes != 14 || !account.RemainingNativeBudgetKnown || account.RemainingNativeBudgetBytes != 999986 {
		t.Fatal("promotion changed financial baseline", account, err)
	}
	if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
		t.Fatal("promotion charged representative")
	}
	for _, table := range []string{"native_runtime_delivery", "native_node_revisions", "pro_usage_epochs", "pro_usage_receipts", "native_metered_route_commands"} {
		if planCount(t, f.s, table) != 0 {
			t.Fatal("promotion claimed native apply", table)
		}
	}
	replay, err := f.s.CommitDarkRestorePreparedPromotionV1(ctx, f.key, f.owner.Token, preview.ReviewSHA256, input)
	if err != nil || !replay.Replayed {
		t.Fatal("lost transition response not replayable", err)
	}
	replay.Replayed = false
	if !reflect.DeepEqual(replay, receipt) {
		t.Fatal("replay changed immutable receipt")
	}
	if _, err = f.s.db.Exec(`UPDATE darkrestore_prepared_promotions_v1 SET actor_user_id=actor_user_id WHERE client_id=?`, f.evidence.ClientID); err == nil {
		t.Fatal("prepared promotion receipt was mutable")
	}
	if _, err = f.s.db.Exec(`DELETE FROM darkrestore_prepared_promotions_v1 WHERE client_id=?`, f.evidence.ClientID); err == nil {
		t.Fatal("prepared promotion receipt was deletable")
	}
	if _, err = f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, f.key, f.owner.Token, f.evidence.ClientID, 802, routeTestID(1802), DarkRestoreNativeApplyPinsV1{}); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("promotion invented apply/health evidence", err)
	}
	path := f.path
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	validated, err := ValidateOfflineBackup(ctx, path)
	if err != nil || validated.SchemaVersion != 16 {
		t.Fatal("v16 offline backup rejected prepared promotion", validated, err)
	}
	if err = ValidateOfflineBackupAssets(ctx, path, f.key); err != nil {
		t.Fatal("offline backup lost promoted plan ciphertext", err)
	}
	f.s, err = Open(path)
	if err != nil {
		t.Fatal("startup rejected coherent prepared promotion", err)
	}
	kept, err := f.s.Client(ctx, f.evidence.ClientID)
	if err != nil || kept.DeploymentState != "pending_node_apply" || kept.UUID != beforeClient.UUID {
		t.Fatal("reopen lost prepared promotion", kept, err)
	}
}

func TestDarkRestorePreparedPromotionRollbackAndStaleReview(t *testing.T) {
	ctx := context.Background()
	f := newPreparedPromotionFixture(t)
	defer f.s.Close()
	preview, input := f.reviewed(t)
	if _, err := f.s.db.Exec("UPDATE clients SET expires_unix=expires_unix+1 WHERE id=?", f.evidence.ClientID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CommitDarkRestorePreparedPromotionV1(ctx, f.key, f.owner.Token, preview.ReviewSHA256, input); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale entitlement state promoted", err)
	}
	client, _ := f.s.Client(ctx, f.evidence.ClientID)
	if client.DeploymentState != "disabled" || planCount(t, f.s, "native_inbound_plan_promotions") != 0 {
		t.Fatal("failed stale transition partially committed")
	}
}

func TestDarkRestorePreparedPromotionInsertFailureRollsBackWholeTransition(t *testing.T) {
	ctx := context.Background()
	f := newPreparedPromotionFixture(t)
	defer f.s.Close()
	preview, input := f.reviewed(t)
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureDarkRestorePreparedPromotionSchema(ctx, tx); err != nil || tx.Commit() != nil {
		t.Fatal("test schema", err)
	}
	if _, err = f.s.db.Exec("CREATE TRIGGER synthetic_prepared_promotion_failure BEFORE INSERT ON darkrestore_prepared_promotions_v1 BEGIN SELECT RAISE(ABORT,'synthetic prepared promotion failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CommitDarkRestorePreparedPromotionV1(ctx, f.key, f.owner.Token, preview.ReviewSHA256, input); err == nil {
		t.Fatal("synthetic receipt failure committed transition")
	}
	client, _ := f.s.Client(ctx, f.evidence.ClientID)
	if client.DeploymentState != "disabled" || planCount(t, f.s, "native_inbound_plan_promotions") != 0 || planCount(t, f.s, "darkrestore_prepared_promotions_v1") != 0 {
		t.Fatal("receipt failure left partial native state")
	}
}

type preparedApplyResult struct {
	state InitialApplyHealthState
	round InitialApplyHealthRound
	proof mx.ProtocolHealthReceipt
	apply mx.TransactionReceipt
}

func completePreparedNativeApply(t *testing.T, f *preparedPromotionFixture) preparedApplyResult {
	t.Helper()
	ctx := context.Background()
	if _, err := f.s.PrepareInitialApplyHealth(ctx, f.key, "owner", 802, InitialApplyHealthPreparationInput{EventID: "m5-prepared-apply-preparation-0001", PromotionEventID: f.input.Promotion.EventID}); err != nil {
		t.Fatal("prepare initial apply", err)
	}
	stage := meteredQueueRequest(f.poll, 1810, "stage", "")
	stage.Drafts = []RouteDraftRef{{InboundID: 802, InboundRevision: 2, DraftRevision: 1}}
	if _, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, stage); err != nil {
		t.Fatal("queue stage", err)
	}
	delivery := meteredQueuePoll(t, f.s, f.key, &f.poll, true)
	stageAck := meteredQueueAck(delivery.Command, f.poll.Status.Observed, "staged", *f.poll.Status.Usage, uc.Receipt{})
	if err := f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, stageAck); err != nil {
		t.Fatal("accept stage", err)
	}
	f.poll.Status.Serial, f.poll.Status.StagedID = delivery.Command.Serial, delivery.Command.StageID
	meteredQueuePoll(t, f.s, f.key, &f.poll, false)
	if _, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, meteredQueueRequest(f.poll, 1811, "apply", delivery.Command.StageID)); err != nil {
		t.Fatal("queue apply", err)
	}
	delivery = meteredQueuePoll(t, f.s, f.key, &f.poll, true)
	birth, err := mx.SignBirthReport(string(routeTestToken), delivery.Command.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 24680, StartTicks: 91})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := f.s.MeteredRouteAuthority(ctx, f.key, routeTestToken, mx.MeteredAuthorityRequest{Command: delivery.Command, Birth: &birth})
	if err != nil {
		t.Fatal("register exact promoted runtime", err)
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1}
	for _, b := range epoch.Bindings {
		zero.Users = append(zero.Users, uc.Usage{ClientID: b.ClientID})
	}
	zero.SHA = meteredHash(zero)
	if _, err = f.s.usage.Accept(ctx, usageledger.Principal{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID}, zero); err != nil {
		t.Fatal("record initial zero", err)
	}
	usage := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("e", 64)}
	command := delivery.Command
	observed := mx.Snapshot{Phase: "ready_observed_admission", Highwater: command.RuntimeSequence, SavedSequence: command.RuntimeSequence, SavedSHA: command.Lease.ConfigSHA, ActiveSequence: command.RuntimeSequence, ActiveSHA: command.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: command.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	applyAck := meteredQueueAck(command, observed, "applied", usage, uc.Receipt{})
	if err = f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, applyAck); err != nil {
		t.Fatal("accept apply", err)
	}
	f.poll.Status.Serial, f.poll.Status.CurrentStageID, f.poll.Status.StagedID, f.poll.Status.Observed, f.poll.Status.Usage = command.Serial, command.StageID, "", observed, &usage
	meteredQueuePoll(t, f.s, f.key, &f.poll, false)
	state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 802, healthEndpoint())
	if err != nil || !state.Eligible || !state.Prepared || state.InitialApplySHA == "" {
		t.Fatal("current prepared first apply", state, err)
	}
	round, err := f.s.QueueInitialApplyHealthRound(ctx, f.key, "owner", 802, InitialApplyHealthRoundInput{ID: routeTestID(1812), InitialApplySHA: state.InitialApplySHA}, healthEndpoint())
	if err != nil {
		t.Fatal("queue prepared health round", err)
	}
	challenge, found, err := f.s.PollInitialApplyHealth(ctx, f.key, routeTestToken, f.node.NodeID, f.node.InstallationID, healthEndpoint())
	if err != nil || !found {
		t.Fatal("poll prepared health", found, err)
	}
	proof := initialProof(challenge)
	if _, err = f.s.ConsumeInitialApplyHealthProof(ctx, f.key, routeTestToken, challenge.ID, healthEndpoint(), proof); err != nil {
		t.Fatal("consume prepared health", err)
	}
	round, err = f.s.InitialApplyHealthRound(ctx, f.key, "owner", 802, round.ID, healthEndpoint())
	if err != nil || !round.Current || round.State != "complete" || len(round.Members) != 1 || round.Members[0].State != "accepted" {
		t.Fatal("complete prepared health", round, err)
	}
	return preparedApplyResult{state: state, round: round, proof: proof, apply: applyAck}
}

func preparedNativeApplyEvidence(t *testing.T, f *preparedPromotionFixture, transition DarkRestorePreparedPromotionReceiptV1, result preparedApplyResult) (DarkRestoreNativeApplyEvidenceV1, DarkRestoreNativeApplyPinsV1) {
	t.Helper()
	ctx := context.Background()
	final, err := readCutoverReceipt(ctx, f.s.db, f.evidence.ClientID)
	if err != nil || final == nil {
		t.Fatal(err)
	}
	current, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, f.evidence.SourceSHA256, f.evidence.SourceID)
	if err != nil || current.NativeUplink == nil || current.NativeDownlink == nil {
		t.Fatal(err)
	}
	expected := DarkRestoreNativeApplyEvidenceV1{
		Scope: DarkRestoreNativeApplyEvidenceScopeV1, ClientID: f.evidence.ClientID, TransitionSHA: final.TransitionSHA,
		PreparedContentSHA: final.Prepared.ContentSHA, PreparedAuthoritySHA: final.Prepared.AuthoritySHA, InboundID: 802,
		PlanRevision: transition.PlanRevision, PreparedInboundRevision: transition.PreparedInboundRevision, PromotedInboundRevision: transition.PromotedInboundRevision,
		PromotionEventID: transition.PromotionEventID, PromotionReceiptSHA: transition.PromotionReceiptSHA,
		InitialApplySHA: result.state.InitialApplySHA, ApplyCommandID: result.state.ApplyCommandID, StageID: result.state.StageID,
		HealthRoundID: result.round.ID, HealthReceiptSHA: meteredHash(result.proof), PreparedUnixNano: final.SubscriptionDeliveredNano,
		RepresentativeID: current.RepresentativeID, ImportReceiptSHA: current.ImportReceiptSHA, NativeBaselineSHA: current.NativeBaselineSHA,
		CurrentFiniteEntitlement: current.CurrentEntitlement, SettledHistoricalBytes: f.evidence.SettledHistoricalBytes,
		NativePostMigrationBytes: *current.NativeUplink + *current.NativeDownlink, RemainingBudgetBytes: final.RemainingBudgetBytes,
		PromotedUnixNano: transition.CreatedUnixNano, AppliedUnixNano: result.apply.ObservedAt, HealthAcceptedUnixNano: result.round.Members[0].Accepted,
	}
	pins := DarkRestoreNativeApplyPinsV1{ClientID: expected.ClientID, TransitionSHA: expected.TransitionSHA, PreparedContentSHA: expected.PreparedContentSHA,
		PreparedAuthoritySHA: expected.PreparedAuthoritySHA, InboundID: expected.InboundID, PlanRevision: expected.PlanRevision,
		PreparedInboundRevision: expected.PreparedInboundRevision, PromotionEventID: expected.PromotionEventID, PromotionReceiptSHA: expected.PromotionReceiptSHA,
		InitialApplySHA: expected.InitialApplySHA, ApplyCommandID: expected.ApplyCommandID, StageID: expected.StageID,
		HealthRoundID: expected.HealthRoundID, HealthReceiptSHA: expected.HealthReceiptSHA, RepresentativeID: expected.RepresentativeID,
		ImportReceiptSHA: expected.ImportReceiptSHA, NativeBaselineSHA: expected.NativeBaselineSHA,
		CurrentFiniteEntitlement: expected.CurrentFiniteEntitlement, SettledHistoricalBytes: expected.SettledHistoricalBytes,
		NativePostMigrationBytes: expected.NativePostMigrationBytes, RemainingBudgetBytes: expected.RemainingBudgetBytes}
	pins.DocumentSHA, err = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", expected)
	if err != nil {
		t.Fatal(err)
	}
	return expected, pins
}

func TestDarkRestoreNativeApplyStoreAdapterPositiveExactChainAndStalePayer(t *testing.T) {
	ctx := context.Background()
	f := newPreparedPromotionFixture(t)
	defer f.s.Close()
	preview, input := f.reviewed(t)
	transition, err := f.s.CommitDarkRestorePreparedPromotionV1(ctx, f.key, f.owner.Token, preview.ReviewSHA256, input)
	if err != nil {
		t.Fatal(err)
	}
	result := completePreparedNativeApply(t, &f)
	expected, pins := preparedNativeApplyEvidence(t, &f, transition, result)
	projected, err := f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, f.key, f.owner.Token, f.evidence.ClientID, 802, result.round.ID, pins)
	if err != nil || !reflect.DeepEqual(projected, expected) {
		t.Fatal("actual prepared promotion/apply/health chain not projected", projected, err)
	}
	if _, err = f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.evidence.ClientID, SubscriptionTokenMutation{EventID: "m5-prepared-post-apply-issuer-0001", Action: "issue"}); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("apply evidence bypassed imported subscription issuer gate", err)
	}
	if _, err = f.s.ProjectInboundExecutionSubscription(ctx, f.key, "owner", f.evidence.ClientID, "uri"); err == nil {
		t.Fatal("apply evidence made a subscription publishable")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if validated, backupErr := ValidateOfflineBackup(ctx, f.path); backupErr != nil || validated.SchemaVersion != 16 {
		t.Fatal("applied v16 evidence failed offline backup validation", validated, backupErr)
	}
	if err = ValidateOfflineBackupAssets(ctx, f.path, f.key); err != nil {
		t.Fatal("applied v16 evidence lost encrypted assets", err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal("applied v16 evidence failed reopen", err)
	}
	if err = f.s.EnableInitialApplyHealth(ctx); err != nil {
		t.Fatal("v16 base-v15 health capability failed explicit restore", err)
	}
	recovered, err := f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, f.key, f.owner.Token, f.evidence.ClientID, 802, result.round.ID, pins)
	if err != nil || !reflect.DeepEqual(recovered, expected) {
		t.Fatal("reopen changed exact native apply projection", recovered, err)
	}
	ownershipMove(t, f.ownershipFixture, "m5-prepared-post-apply-payer-change-0001", ClientOwnershipIntent{ClientIDs: []string{f.evidence.ClientID}, RepresentativeID: "rep-third", Reason: "Synthetic current payer drift after native apply"})
	if _, err = f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, f.key, f.owner.Token, f.evidence.ClientID, 802, result.round.ID, pins); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("stale payer reused native apply evidence", err)
	}
}

func TestPreparedDarkRestorePromotionReceiptOwnerRecovery(t *testing.T) {
	ctx := context.Background()
	f := newPreparedPromotionFixture(t)
	defer f.s.Close()
	if _, err := f.s.DarkRestorePreparedPromotionReceipt(ctx, f.owner.Token, f.evidence.ClientID); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing receipt", err)
	}
	preview, in := f.reviewed(t)
	committed, err := f.s.CommitDarkRestorePreparedPromotionV1(ctx, f.key, f.owner.Token, preview.ReviewSHA256, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.s.DarkRestorePreparedPromotionReceipt(ctx, f.owner.Token, in.ClientID)
	if err != nil || !reflect.DeepEqual(got, committed) {
		t.Fatal("recovery changed receipt", err)
	}
	if _, err = f.s.DarkRestorePreparedPromotionReceipt(ctx, f.rep.Token, in.ClientID); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative read owner receipt", err)
	}
	if got.NativeApplyRecorded || got.SubscriptionActivated || got.AuthorizesFurtherMutation {
		t.Fatal("recovery granted further authority")
	}
}
