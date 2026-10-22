package business

import (
	"context"
	"strings"
	"sync"
	"testing"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type handoverFixture struct {
	s              *Store
	path           string
	key            []byte
	client         Client
	source, target mx.TransactionPoll
	epoch          uc.Epoch
	in             MeteredHandoverRequest
}

func newHandoverFixture(t *testing.T, prepare ...func(*Store, []byte, mx.TransactionPoll, mx.TransactionPoll)) *handoverFixture {
	t.Helper()
	ctx := context.Background()
	s, path, key, client, source := meteredQueueFixture(t)
	target := source
	target.NodeID, target.InstallationID, target.Status.Instance = "meter-target", strings.Repeat("b", 32), strings.Repeat("c", 32)
	if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 10, NodeID: target.NodeID, InstallationID: target.InstallationID, ListenIP: "127.0.0.1", DirectPort: 19590}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNativeRouteDraft(ctx, key, NativeRouteDraftInput{EventID: "handover-target-draft-0001", InboundID: 10, NodeID: target.NodeID, InstallationID: target.InstallationID, ExpectedInboundRevision: 1, DirectPort: 19590, Reason: "Same customers on replacement LAB target"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimMeteredRouteChannel(ctx, target.NodeID, target.InstallationID, target.BinarySHA); err != nil {
		t.Fatal(err)
	}
	meteredQueuePoll(t, s, key, &target, false)
	for _, fn := range prepare {
		fn(s, key, source, target)
	}
	review := func(p mx.TransactionPoll) []RouteDraftRef {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		refs, err := meteredRouteRefs(ctx, tx, p.NodeID, p.InstallationID)
		tx.Rollback()
		if err != nil {
			t.Fatal(err)
		}
		return refs
	}
	d := meteredQueuedApply(t, s, key, &source, review(source))
	c := d.Command
	b, err := mx.SignBirthReport(string(routeTestToken), c.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12345, StartTicks: 77})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: c, Birth: &b})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1}
	for _, b := range epoch.Bindings {
		zero.Users = append(zero.Users, uc.Usage{ClientID: b.ClientID})
	}
	zero.SHA = meteredHash(zero)
	if _, err = s.usage.Accept(ctx, usageledger.Principal{NodeID: source.NodeID, InstallationID: source.InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	u := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("d", 64)}
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, meteredQueueAck(c, o, "applied", u, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	source.Status.Serial, source.Status.CurrentStageID, source.Status.StagedID = 2, c.StageID, ""
	source.Status.Usage, source.Status.Observed = &u, o
	meteredQueuePoll(t, s, key, &source, false)
	stage := meteredQueueRequest(target, 31, "stage", "")
	stage.Drafts = review(target)
	if _, err = s.QueueMeteredRouteCommand(ctx, key, target.NodeID, stage); err != nil {
		t.Fatal(err)
	}
	d = meteredQueuePoll(t, s, key, &target, true)
	if err = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, meteredQueueAck(d.Command, target.Status.Observed, "staged", *target.Status.Usage, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	target.Status.Serial, target.Status.StagedID = 1, d.Command.StageID
	meteredQueuePoll(t, s, key, &target, false)
	return &handoverFixture{s: s, path: path, key: key, client: client, source: source, target: target, epoch: epoch, in: MeteredHandoverRequest{ID: routeTestID(90), SourceNode: source.NodeID, TargetNode: target.NodeID, SourceStageID: source.Status.CurrentStageID, TargetStageID: target.Status.StagedID, SourceUsage: u}}
}

func (f *handoverFixture) create(t *testing.T) MeteredHandoverView {
	t.Helper()
	v, err := f.s.CreateMeteredHandover(context.Background(), f.key, f.in)
	if err != nil {
		t.Fatal("create", err)
	}
	return v
}

func (f *handoverFixture) drain(t *testing.T) mx.TransactionDelivery {
	t.Helper()
	in := meteredQueueRequest(f.source, 3, "drain", f.in.SourceStageID)
	if _, err := f.s.QueueMeteredHandoverCommand(context.Background(), f.key, f.in.ID, in); err != nil {
		t.Fatal("drain", err)
	}
	return meteredQueuePoll(t, f.s, f.key, &f.source, true)
}

func (f *handoverFixture) seal(t *testing.T, d mx.TransactionDelivery) mx.TransactionReceipt {
	t.Helper()
	_, binding, _ := uc.CanonicalEpoch(f.epoch)
	final := uc.Report{Version: 1, EpochID: f.epoch.ID, BindingSHA: binding, Sequence: 2, Quiesced: true}
	for _, b := range f.epoch.Bindings {
		u := uc.Usage{ClientID: b.ClientID}
		if b.ClientID == f.client.ID {
			u.Uplink, u.Downlink = 111, 222
		}
		final.Users = append(final.Users, u)
	}
	final.SHA = meteredHash(final)
	receipt, err := f.s.usage.Accept(context.Background(), usageledger.Principal{NodeID: f.source.NodeID, InstallationID: f.source.InstallationID}, final)
	if err != nil {
		t.Fatal(err)
	}
	u := f.in.SourceUsage
	u.Settlement = receipt
	o := f.source.Status.Observed
	o.ProcessReady, o.ActiveSequence, o.ActiveSHA, o.LeaseExpires = false, 0, "", 0
	o.Usage = &mx.UsageObservationStatus{Required: true, Settled: true}
	ack := meteredQueueAck(d.Command, o, "drained", u, receipt)
	f.source.Status.Serial, f.source.Status.Observed, f.source.Status.Usage = d.Command.Serial, o, &u
	return ack
}

func TestMeteredHandoverRequiresLedgerAndDurableDrainACK(t *testing.T) {
	ctx := context.Background()
	f := newHandoverFixture(t)
	running := f.source.Status.Observed
	f.create(t)
	apply := meteredQueueRequest(f.target, 32, "apply", f.in.TargetStageID)
	if _, err := f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, apply); err == nil {
		t.Fatal("apply before drain")
	}
	v, _ := f.s.MeteredHandover(ctx, f.in.ID)
	if v.ApplyCommandID != "" {
		t.Fatal("rejected step committed link")
	}
	d := f.drain(t)
	// A valid signature with an invented final HUB receipt must not release the barrier.
	u := f.in.SourceUsage
	u.Settlement = uc.Receipt{EpochID: f.epoch.ID, Sequence: 2, SHA: strings.Repeat("f", 64)}
	o := f.source.Status.Observed
	o.ProcessReady, o.ActiveSequence, o.ActiveSHA, o.LeaseExpires = false, 0, "", 0
	o.Usage = &mx.UsageObservationStatus{Required: true, Settled: true}
	if err := f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, meteredQueueAck(d.Command, o, "drained", u, u.Settlement)); err == nil {
		t.Fatal("fabricated HUB final seal")
	}
	ack := f.seal(t, d)
	meteredQueuePoll(t, f.s, f.key, &f.source, true) // Agent stopped, but drain ACK was lost.
	if _, err := f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, apply); err == nil {
		t.Fatal("poll substituted for exact command ACK")
	}
	if _, err := f.s.CancelMeteredHandover(ctx, f.in.ID); err == nil {
		t.Fatal("cancel released an uncertain drain")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	defer s.Close()
	if _, err = s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, apply); err == nil {
		t.Fatal("restart lost barrier")
	}
	if err = s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, ack); err != nil {
		t.Fatal("replayed final ACK", err)
	}
	meteredQueuePoll(t, s, f.key, &f.source, false)
	v, err = s.MeteredHandover(ctx, f.in.ID)
	if err != nil || v.Phase != "source_drained" || v.CanPublish {
		t.Fatal("false completion", v.Phase, err)
	}
	queued, err := s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, apply)
	if err != nil {
		t.Fatal("apply after durable seal", err)
	}
	again, err := s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, apply)
	if err != nil || queued.ID != again.ID || queued.Serial != again.Serial {
		t.Fatal("lost owner reply allocated a new command", err)
	}
	if _, err = s.QueueMeteredRouteCommand(ctx, f.key, f.target.NodeID, apply); err == nil {
		t.Fatal("ordinary request adopted handover command")
	}
	d = meteredQueuePoll(t, s, f.key, &f.target, true)
	if _, err = s.MeteredRouteAuthority(ctx, f.key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command}); err != nil {
		t.Fatal("reviewed delivery authority", err)
	}
	usage, err := s.ClientUsage(ctx, f.client.ID)
	if err != nil || usage.Total != 333 {
		t.Fatal("traffic lost or billed twice", usage.Total, err)
	}
	// Every new delivery/authority check rechecks the source barrier.
	f.source.Status.Observed = running
	meteredQueuePoll(t, s, f.key, &f.source, false)
	if _, err = s.MeteredRouteAuthority(ctx, f.key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command}); err == nil {
		t.Fatal("source resumed but target authority remained valid")
	}
}

func TestMeteredHandoverReservationCancellationAndAtomicQueue(t *testing.T) {
	ctx := context.Background()
	f := newHandoverFixture(t)
	v := f.create(t)
	again, err := f.s.CreateMeteredHandover(ctx, f.key, f.in)
	if err != nil || again.Created != v.Created {
		t.Fatal("create replay", err)
	}
	changed := f.in
	changed.TargetStageID = routeTestID(80)
	if _, err = f.s.CreateMeteredHandover(ctx, f.key, changed); err == nil {
		t.Fatal("same id changed review")
	}
	changed = f.in
	changed.ID = routeTestID(91)
	if _, err = f.s.CreateMeteredHandover(ctx, f.key, changed); err == nil {
		t.Fatal("overlapping nodes")
	}
	for _, p := range []mx.TransactionPoll{f.source, f.target} {
		if _, err = f.s.QueueMeteredRouteCommand(ctx, f.key, p.NodeID, meteredQueueRequest(p, 70, "stage", "")); err == nil {
			t.Fatal("independent stage bypassed reservation", p.NodeID)
		}
	}
	// An insertion failure must roll back the operation link and channel serial.
	if _, err = f.s.db.Exec(`CREATE TRIGGER reject_handover_test BEFORE INSERT ON native_metered_route_commands BEGIN SELECT RAISE(ABORT,'fault injection'); END`); err != nil {
		t.Fatal(err)
	}
	in := meteredQueueRequest(f.source, 3, "drain", f.in.SourceStageID)
	if _, err = f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, in); err == nil {
		t.Fatal("injected insert failure ignored")
	}
	v, err = f.s.MeteredHandover(ctx, f.in.ID)
	if err != nil || v.DrainCommandID != "" {
		t.Fatal("failed command left dangling step", err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER reject_handover_test`); err != nil {
		t.Fatal(err)
	}
	v, err = f.s.CancelMeteredHandover(ctx, f.in.ID)
	if err != nil || !v.Cancelled || v.CanPublish {
		t.Fatal("cancel", err)
	}
	if _, err = f.s.CancelMeteredHandover(ctx, f.in.ID); err != nil {
		t.Fatal("cancel replay", err)
	}
	if _, err = f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, in); err == nil {
		t.Fatal("cancelled step queued")
	}
	changed = f.in
	changed.ID = routeTestID(92)
	if _, err = f.s.CreateMeteredHandover(ctx, f.key, changed); err != nil {
		t.Fatal("safe cancellation kept reservation", err)
	}
}

func TestMeteredHandoverConcurrentCreateHasOneWinner(t *testing.T) {
	f := newHandoverFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{routeTestID(93), routeTestID(94)} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			in := f.in
			in.ID = id
			_, err := f.s.CreateMeteredHandover(ctx, f.key, in)
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatal("concurrent overlapping handovers", wins)
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM native_metered_handover_nodes`).Scan(&n); err != nil || n != 2 {
		t.Fatal("partial reservation", n, err)
	}
}

func TestMeteredHandoverAppliedStillNeedsHealthAndPreservesReservations(t *testing.T) {
	ctx := context.Background()
	f := newHandoverFixture(t)
	f.create(t)
	d := f.drain(t)
	ack := f.seal(t, d)
	if err := f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, ack); err != nil {
		t.Fatal(err)
	}
	meteredQueuePoll(t, f.s, f.key, &f.source, false)
	apply := meteredQueueRequest(f.target, 32, "apply", f.in.TargetStageID)
	if _, err := f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, apply); err != nil {
		t.Fatal(err)
	}
	d = meteredQueuePoll(t, f.s, f.key, &f.target, true)
	b, err := mx.SignBirthReport(string(routeTestToken), d.Command.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12346, StartTicks: 78})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := f.s.MeteredRouteAuthority(ctx, f.key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command, Birth: &b})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1, Users: []uc.Usage{{ClientID: f.client.ID}}}
	zero.SHA = meteredHash(zero)
	if _, err = f.s.usage.Accept(ctx, usageledger.Principal{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	u := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("e", 64)}
	c := d.Command
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, meteredQueueAck(c, o, "applied", u, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	f.target.Status.Serial, f.target.Status.StagedID, f.target.Status.CurrentStageID, f.target.Status.Observed, f.target.Status.Usage = c.Serial, "", c.StageID, o, &u
	meteredQueuePoll(t, f.s, f.key, &f.target, false)
	v, err := f.s.MeteredHandover(ctx, f.in.ID)
	if err != nil || v.Phase != "target_applied_health_required" || v.CanPublish {
		t.Fatal("runtime ACK invented protocol health/subscription", v.Phase, err)
	}
	if _, err = f.s.CancelMeteredHandover(ctx, f.in.ID); err == nil {
		t.Fatal("running target released source reservation")
	}
	for _, p := range []mx.TransactionPoll{f.source, f.target} {
		view, err := f.s.MeteredRouteControlChannel(ctx, f.key, p.NodeID)
		if err != nil || view.HandoverID != f.in.ID || view.Controls.Stage || view.Controls.Apply != nil || view.Controls.Rollback != nil || view.Controls.Renew != nil {
			t.Fatal("ordinary controls bypassed reservation", err)
		}
	}
	if _, err = f.s.QueueMeteredRouteCommand(ctx, f.key, f.source.NodeID, meteredQueueRequest(f.source, 4, "rollback", f.in.SourceStageID)); err == nil {
		t.Fatal("source restarted while target applied")
	}
	// Emergency stop of target is allowed without clearing the handover fence.
	if _, err = f.s.QueueMeteredRouteCommand(ctx, f.key, f.target.NodeID, meteredQueueRequest(f.target, 33, "drain", f.in.TargetStageID)); err != nil {
		t.Fatal("emergency stop blocked", err)
	}
}

func TestMeteredHandoverUnknownDrainRemainsUnknownAfterLateACK(t *testing.T) {
	ctx := context.Background()
	f := newHandoverFixture(t)
	f.create(t)
	d := f.drain(t)
	if err := f.s.AbandonMeteredRouteCommand(ctx, f.source.NodeID, d.Command.ID); err != nil {
		t.Fatal(err)
	}
	ack := f.seal(t, d)
	if err := f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, ack); err != nil {
		t.Fatal(err)
	}
	meteredQueuePoll(t, f.s, f.key, &f.source, false)
	if _, err := f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, meteredQueueRequest(f.target, 32, "apply", f.in.TargetStageID)); err == nil {
		t.Fatal("abandoned unknown became execution authority")
	}
	v, err := f.s.MeteredHandover(ctx, f.in.ID)
	if err != nil || v.Phase != "recovery_required" || v.CanPublish {
		t.Fatal("unknown state hidden", v.Phase, err)
	}
	if _, err = f.s.CancelMeteredHandover(ctx, f.in.ID); err == nil {
		t.Fatal("unknown operation released fence")
	}
}

func TestMeteredHandoverSchemaUpgradePreservesExistingState(t *testing.T) {
	f := newHandoverFixture(t)
	f.create(t)
	// Simulate the previous v7 marker; all existing data must survive the
	// additive v8 migration, including any already-written reservation.
	if _, err := f.s.db.Exec(`PRAGMA user_version=7`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err = s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 8 {
		t.Fatal("downgrade guard missing", version, err)
	}
	v, err := s.MeteredHandover(context.Background(), f.in.ID)
	if err != nil || v.ID != f.in.ID || v.CanPublish {
		t.Fatal("journal lost on upgrade", err)
	}
	c, err := s.Client(context.Background(), f.client.ID)
	if err != nil || c.UUID != f.client.UUID {
		t.Fatal("customer identity changed", err)
	}
}
