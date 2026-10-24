package business

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func initialPromoted(t *testing.T) *promotionFixture {
	t.Helper()
	f := newPromotionFixture(t, nil)
	ctx := context.Background()
	if _, err := f.s.BootstrapOwner(ctx, "initial-owner", "synthetic-initial-health-password"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableInitialApplyHealth(ctx); err != nil {
		t.Fatal("initial startup", err)
	}
	if _, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, f.reviewed(t)); err != nil {
		t.Fatal(err)
	}
	return f
}

func initialPrepare(t *testing.T, f *promotionFixture) InitialApplyHealthPreparation {
	t.Helper()
	p, err := f.s.PrepareInitialApplyHealth(context.Background(), f.key, "owner", 501, InitialApplyHealthPreparationInput{EventID: "initial-preparation-0001", PromotionEventID: f.input.EventID})
	if err != nil {
		t.Fatal("prepare", err)
	}
	return p
}

func initialApplied(t *testing.T) (*promotionFixture, uc.Epoch) {
	t.Helper()
	f := initialPromoted(t)
	initialPrepare(t, f)
	return f, initialActivate(t, f, 1)
}

func initialActivate(t *testing.T, f *promotionFixture, commandBase int) uc.Epoch {
	t.Helper()
	ctx := context.Background()
	stage := meteredQueueRequest(f.poll, commandBase, "stage", "")
	stage.Drafts = []RouteDraftRef{{InboundID: 501, InboundRevision: 2, DraftRevision: 1}}
	if _, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, stage); err != nil {
		t.Fatal(err)
	}
	d := meteredQueuePoll(t, f.s, f.key, &f.poll, true)
	if err := f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, meteredQueueAck(d.Command, f.poll.Status.Observed, "staged", *f.poll.Status.Usage, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	f.poll.Status.Serial, f.poll.Status.StagedID = d.Command.Serial, d.Command.StageID
	meteredQueuePoll(t, f.s, f.key, &f.poll, false)
	if _, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, meteredQueueRequest(f.poll, commandBase+1, "apply", d.Command.StageID)); err != nil {
		t.Fatal(err)
	}
	d = meteredQueuePoll(t, f.s, f.key, &f.poll, true)
	b, err := mx.SignBirthReport(string(routeTestToken), d.Command.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 13456, StartTicks: 79})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := f.s.MeteredRouteAuthority(ctx, f.key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command, Birth: &b})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1}
	for _, b := range epoch.Bindings {
		zero.Users = append(zero.Users, uc.Usage{ClientID: b.ClientID})
	}
	zero.SHA = meteredHash(zero)
	if _, err = f.s.usage.Accept(ctx, usageledger.Principal{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	u := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("e", 64)}
	c := d.Command
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, meteredQueueAck(c, o, "applied", u, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	f.poll.Status.Serial, f.poll.Status.CurrentStageID, f.poll.Status.StagedID, f.poll.Status.Observed, f.poll.Status.Usage = c.Serial, c.StageID, "", o, &u
	meteredQueuePoll(t, f.s, f.key, &f.poll, false)
	return epoch
}

func initialProof(q mx.ProtocolHealthRequest) mx.ProtocolHealthReceipt {
	h := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	p := mx.ProtocolHealthReceipt{Version: q.Version, RequestSHA: meteredHash(q), ResponseSHA: hex.EncodeToString(h[:]), Completed: time.Now().UnixNano()}
	raw, _ := json.Marshal(p)
	m := hmac.New(sha256.New, routeTestToken)
	m.Write([]byte(fmt.Sprintf("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v%d\x00", q.Version)))
	m.Write(raw)
	p.Signature = hex.EncodeToString(m.Sum(nil))
	return p
}

func initialCompleteRound(t *testing.T, f *promotionFixture, n int) (InitialApplyHealthRound, mx.ProtocolHealthRequest, mx.ProtocolHealthReceipt) {
	t.Helper()
	ctx := context.Background()
	state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 501, healthEndpoint())
	if err != nil || !state.Eligible || !state.Prepared || state.CanPrepare || state.InitialApplySHA == "" {
		t.Fatal("current first apply", state, err)
	}
	round, err := f.s.QueueInitialApplyHealthRound(ctx, f.key, "owner", 501, InitialApplyHealthRoundInput{ID: routeTestID(n), InitialApplySHA: state.InitialApplySHA}, healthEndpoint())
	if err != nil || round.Current || round.State != "pending" || len(round.Members) != 1 {
		t.Fatal("queue first round", round, err)
	}
	q, found, err := f.s.PollInitialApplyHealth(ctx, f.key, routeTestToken, f.node.NodeID, f.node.InstallationID, healthEndpoint())
	if err != nil || !found || q.Version != mx.InitialApplyProtocolHealthVersion || q.HandoverID != "" || q.InitialApplySHA != state.InitialApplySHA || q.Expires != round.Expires {
		t.Fatal("poll first round", found, err)
	}
	p := initialProof(q)
	if _, err = f.s.ConsumeInitialApplyHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p); err != nil {
		t.Fatal("consume first proof", err)
	}
	round, err = f.s.InitialApplyHealthRound(ctx, f.key, "owner", 501, round.ID, healthEndpoint())
	if err != nil || !round.Current || round.State != "complete" {
		t.Fatal("completed first proof", round, err)
	}
	return round, q, p
}

func TestInitialApplyPreparationReviewedAtomicAndStageGate(t *testing.T) {
	f := initialPromoted(t)
	ctx := context.Background()
	before := promotionCiphertexts(t, f)
	clients, _ := f.s.ListClients(ctx, "", 200, 0)
	state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 501, healthEndpoint())
	if err != nil || state.Prepared || !state.CanPrepare || state.Eligible || state.DirectPort != 19589 || state.InboundRevision != 2 || state.DraftRevision != 1 {
		t.Fatal("preparation discovery", state, err)
	}
	controls, err := f.s.MeteredRouteControlChannel(ctx, f.key, f.node.NodeID)
	if err != nil || controls.Controls.Stage {
		t.Fatal("Stage advertised before preparation", err)
	}
	in := meteredQueueRequest(f.poll, 1, "stage", "")
	in.Drafts = []RouteDraftRef{{InboundID: 501, InboundRevision: 2, DraftRevision: 1}}
	if _, err = f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, in); !errors.Is(err, ErrConflict) {
		t.Fatal("Stage bypassed preparation", err)
	}
	if _, err = f.s.PrepareInitialApplyHealth(ctx, f.key, "owner", 501, InitialApplyHealthPreparationInput{EventID: "initial-preparation-0001", PromotionEventID: "different-promotion-0001"}); err == nil {
		t.Fatal("wrong promotion reviewed")
	}
	if _, err = f.s.db.Exec(`CREATE TRIGGER test_initial_abort BEFORE INSERT ON pro_initial_preparations BEGIN SELECT RAISE(ABORT,'late preparation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PrepareInitialApplyHealth(ctx, f.key, "owner", 501, InitialApplyHealthPreparationInput{EventID: "initial-preparation-0001", PromotionEventID: f.input.EventID}); err == nil {
		t.Fatal("late failure committed")
	}
	if planCount(t, f.s, "pro_probe_accounts") != 0 || planCount(t, f.s, "pro_initial_preparations") != 0 {
		t.Fatal("partial preparation/probe survived")
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER test_initial_abort`); err != nil {
		t.Fatal(err)
	}
	p := initialPrepare(t, f)
	replay := initialPrepare(t, f)
	if !replay.Replayed {
		t.Fatal("lost response was not replayed")
	}
	replay.Replayed = false
	if p != replay {
		t.Fatal("preparation retry changed identity")
	}
	controls, err = f.s.MeteredRouteControlChannel(ctx, f.key, f.node.NodeID)
	if err != nil || !controls.Controls.Stage {
		t.Fatal("prepared Stage unavailable", err)
	}
	after, _ := f.s.ListClients(ctx, "", 200, 0)
	if !reflect.DeepEqual(before, promotionCiphertexts(t, f)) || !reflect.DeepEqual(clients, after) || planCount(t, f.s, "native_metered_route_commands") != 0 || planCount(t, f.s, "pro_usage_epochs") != 0 {
		t.Fatal("preparation changed authoring/client/runtime history")
	}
}

func TestInitialApplyProofSubscriptionAccountingAndRestart(t *testing.T) {
	f, epoch := initialApplied(t)
	ctx := context.Background()
	client := f.clients[0]
	before := promotionCiphertexts(t, f)
	if _, err := f.s.MutateLabSubscription(ctx, f.key, "owner", client.ID, SubscriptionTokenMutation{EventID: "initial-token-issue-0001", Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secret, err := f.s.RevealLabSubscription(ctx, f.key, "owner", client.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	round, q, p := initialCompleteRound(t, f, 990)
	in := InitialApplySubscriptionMaterialization{EventID: "initial-snapshot-event-0001", ClientID: client.ID, RoundID: round.ID, InitialApplySHA: round.InitialApplySHA}
	snapshot, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, in, healthEndpoint())
	if err != nil || !snapshot.Current || snapshot.Revision != 1 || snapshot.DeliveryRevision != 1 || snapshot.HandoverID != "" || snapshot.Scope != InitialApplyHealthScope || snapshot.ExpiresUnixNano != round.Expires {
		t.Fatal("initial snapshot", snapshot, err)
	}
	body, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || body.SourceNodeID != "" || body.Snapshot.DeliveryRevision != 1 || bytes.Count(body.Raw, []byte(client.UUID)) != 1 || bytes.Contains(body.Raw, []byte("privateKey")) || bytes.Contains(body.Raw, []byte(q.SystemProbeID)) {
		t.Fatal("initial bounded delivery", err)
	}
	view, err := f.s.LabSubscription(ctx, f.key, "owner", client.ID, healthEndpoint())
	if err != nil || view.DeliveryScope != InitialApplyHealthScope || view.DeliveryRevision != 1 || view.InitialApplyRevision != 1 || view.HandoverRevision != 0 || view.Snapshot == nil || !view.Snapshot.Current {
		t.Fatal("typed subscription view", view, err)
	}
	if planCount(t, f.s, "native_metered_handovers") != 0 || planCount(t, f.s, "pro_health_rounds") != 0 || planCount(t, f.s, "pro_subscription_snapshots") != 0 {
		t.Fatal("initial proof fabricated a handover")
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	report := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 2}
	for _, b := range epoch.Bindings {
		u := uc.Usage{ClientID: b.ClientID}
		if b.Role == uc.SystemProbeRole {
			if b.Billable {
				t.Fatal("billable system probe")
			}
			u.Uplink, u.Downlink = 123, 456
		}
		report.Users = append(report.Users, u)
	}
	report.SHA = meteredHash(report)
	generation := planGeneration(t, f.s)
	if _, err = f.s.usage.Accept(ctx, usageledger.Principal{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID}, report); err != nil {
		t.Fatal(err)
	}
	probe, err := f.s.usage.ProbeUsage(ctx, q.SystemProbeID)
	if err != nil || probe.Total != 579 {
		t.Fatal("probe accounting", probe, err)
	}
	for _, c := range f.clients {
		usage, e := f.s.ClientUsage(ctx, c.ID)
		if e != nil || usage.Total != 0 || !usage.Eligible {
			t.Fatal("probe debited customer", e)
		}
	}
	if planGeneration(t, f.s) != generation || !reflect.DeepEqual(before, promotionCiphertexts(t, f)) {
		t.Fatal("probe changed plan/generation")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal("v15 restart", err)
	}
	t.Cleanup(func() { f.s.Close() })
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("schema restoration silently enabled initial delivery")
	}
	if err = f.s.EnableInitialApplyHealth(ctx); err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, in, healthEndpoint())
	if err != nil || !replay.Replayed || replay.ID != snapshot.ID || replay.ExpiresUnixNano != snapshot.ExpiresUnixNano || replay.DeliveryRevision != 1 {
		t.Fatal("snapshot replay after restart", replay, err)
	}
	evidence, err := f.s.ConsumeInitialApplyHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p)
	if err != nil || !evidence.Replayed {
		t.Fatal("receipt lost response replay", err)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err != nil {
		t.Fatal("restart delivery", err)
	}
	tx, err := f.s.initialHealthTx(ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	row, err := readInitialSubscriptionSnapshot(ctx, tx, "id", snapshot.ID)
	if err == nil {
		_, err = f.s.currentInitialSubscriptionSnapshot(ctx, tx, f.key, row, nil, row.CreatedUnixNano-1)
	}
	tx.Rollback()
	if !errors.Is(err, ErrConflict) {
		t.Fatal("snapshot clock regression accepted", err)
	}
	controls, err := f.s.MeteredRouteControlChannel(ctx, f.key, f.node.NodeID)
	if err != nil || controls.Controls.Drain == nil {
		t.Fatal("health removed Drain", err)
	}
	if _, err = f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, meteredQueueRequest(f.poll, 991, "drain", f.poll.Status.CurrentStageID)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("pending Drain retained subscription authority")
	}
}

func TestInitialApplyPreparationRejectsExistingStageAndMissingZero(t *testing.T) {
	t.Run("staged without preparation", func(t *testing.T) {
		f := newPromotionFixture(t, nil)
		ctx := context.Background()
		if _, err := f.s.BootstrapOwner(ctx, "initial-owner", "synthetic-initial-health-password"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, f.reviewed(t)); err != nil {
			t.Fatal(err)
		}
		in := meteredQueueRequest(f.poll, 1, "stage", "")
		in.Drafts = []RouteDraftRef{{InboundID: 501, InboundRevision: 2, DraftRevision: 1}}
		if _, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, in); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EnableInitialApplyHealth(ctx); err != nil {
			t.Fatal(err)
		}
		state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 501, healthEndpoint())
		if err != nil || state.Prepared || state.CanPrepare || state.Eligible {
			t.Fatal("retrofit advertised", state, err)
		}
		if _, err = f.s.PrepareInitialApplyHealth(ctx, f.key, "owner", 501, InitialApplyHealthPreparationInput{EventID: "initial-preparation-0001", PromotionEventID: f.input.EventID}); err == nil {
			t.Fatal("staged candidate retrofitted")
		}
		if planCount(t, f.s, "pro_probe_accounts") != 0 {
			t.Fatal("retrofit probe created")
		}
	})
	t.Run("durable zero receipt mandatory", func(t *testing.T) {
		f, _ := initialApplied(t)
		ctx := context.Background()
		// Corrupt only the durable initial-receipt relation after a valid ACK;
		// the health loader must recheck the HUB ledger, not trust that ACK.
		var guard string
		if err := f.s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='pro_usage_receipts_no_delete'`).Scan(&guard); err != nil {
			t.Fatal(err)
		}
		for _, sql := range []string{`PRAGMA foreign_keys=OFF`, `DROP TRIGGER pro_usage_receipts_no_delete`, `DELETE FROM pro_usage_receipts WHERE sequence='1'`, guard, `PRAGMA foreign_keys=ON`} {
			if _, err := f.s.db.Exec(sql); err != nil {
				t.Fatal(err)
			}
		}
		state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 501, healthEndpoint())
		if err != nil || state.Eligible {
			t.Fatal("missing zero receipt accepted", state, err)
		}
	})
}

func TestInitialApplyConcurrentRoundReceiptAndDeliveryCAS(t *testing.T) {
	f, _ := initialApplied(t)
	ctx := context.Background()
	state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 501, healthEndpoint())
	if err != nil || !state.Eligible {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan InitialApplyHealthRound, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.s.QueueInitialApplyHealthRound(ctx, f.key, "owner", 501, InitialApplyHealthRoundInput{ID: routeTestID(992), InitialApplySHA: state.InitialApplySHA}, healthEndpoint())
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var deadline int64
	for r := range results {
		if deadline != 0 && deadline != r.Expires {
			t.Fatal("retry extended round")
		}
		deadline = r.Expires
	}
	q, found, err := f.s.PollInitialApplyHealth(ctx, f.key, routeTestToken, f.node.NodeID, f.node.InstallationID, healthEndpoint())
	if err != nil || !found {
		t.Fatal(err)
	}
	q2, found, err := f.s.PollInitialApplyHealth(ctx, f.key, routeTestToken, f.node.NodeID, f.node.InstallationID, healthEndpoint())
	if err != nil || !found || q2 != q {
		t.Fatal("poll lost response replaced challenge", err)
	}
	if _, err = f.s.ConsumeInitialApplyHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), initialProof(q)); err != nil {
		t.Fatal(err)
	}
	client := f.clients[0].ID
	in := InitialApplySubscriptionMaterialization{EventID: "initial-cas-snapshot-0001", ClientID: client, RoundID: routeTestID(992), InitialApplySHA: state.InitialApplySHA}
	if _, err = f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, in, healthEndpoint()); err != nil {
		t.Fatal(err)
	}
	in.EventID = "initial-cas-snapshot-0002"
	in.ExpectedRevision = 1
	if _, err = f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, in, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("global CAS omitted/stale accepted", err)
	}
	in.ExpectedDeliveryRevision = 1
	if _, err = f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, in, healthEndpoint()); err != nil {
		t.Fatal("second same-origin snapshot", err)
	}
	if planCount(t, f.s, "pro_delivery_events") != 2 {
		t.Fatal("delivery revision journal mismatch")
	}
}
