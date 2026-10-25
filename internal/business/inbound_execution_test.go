package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

type executionFixture struct {
	s      *Store
	key    []byte
	path   string
	plan   NativeInboundPlanInput
	client Client
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	s, path := opened(t)
	ctx := context.Background()
	if err := s.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BootstrapOwner(ctx, "execution-owner", "synthetic-execution-test-password"); err != nil {
		t.Fatal(err)
	}
	c := usageClient(t, s, "exec-client-a", 1<<20)
	in := NativeInboundPlanInput{EventID: "execution-create-plan-0001", Profile: InboundInput{ID: 501, Name: "Managed VLESS", Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Settings: map[string]any{"decryption": "none"}}}, Nodes: []NativeInboundPlanNode{{NodeID: "remote-a", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10", DirectPort: 21185, DirectHosts: []string{"a.example.org"}, TunnelHosts: []string{}}}, Reason: "Managed execution test fixture"}
	f := &executionFixture{s: s, key: bytes.Repeat([]byte{0x61}, 32), path: path, plan: in, client: c}
	if _, err := s.SaveNativeInboundPlan(ctx, f.key, in); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *executionFixture) command(t *testing.T, event, action string) ix.Operation {
	t.Helper()
	ctx := context.Background()
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil {
		t.Fatal(err)
	}
	in := ix.CommandInput{EventID: event, Action: action, ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Reviewed managed operation"}
	p, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 501, in)
	if err != nil || !p.Valid {
		t.Fatalf("preview %s: %v %#v", action, err, p.Blockers)
	}
	in.ReviewSHA = p.ReviewSHA
	op, err := f.s.QueueInboundExecution(ctx, f.key, "owner", 501, in)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.QueueInboundExecution(ctx, f.key, "owner", 501, in)
	if err != nil || !replay.Replayed || replay.ID != op.ID {
		t.Fatal("operation replay", err)
	}
	return op
}
func (f *executionFixture) bundle(t *testing.T) ix.Bundle {
	t.Helper()
	b, ok, e := f.s.InboundExecutionBundle(context.Background(), f.key, "remote-a", strings.Repeat("a", 32))
	if e != nil || !ok {
		t.Fatal(e)
	}
	return b
}
func (f *executionFixture) birth(t *testing.T, b ix.Bundle, id string) ix.EpochInput {
	t.Helper()
	e := executionEpoch(b, id)
	if _, err := f.s.RegisterInboundExecutionEpoch(context.Background(), f.key, e); err != nil {
		t.Fatal(err)
	}
	return e
}
func executionEpoch(b ix.Bundle, id string) ix.EpochInput {
	e := ix.EpochInput{EpochID: id, NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: b.Generation, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, BinarySHAs: map[string]string{}}
	group := map[string]string{}
	add := func(name, engine string) {
		e.BinarySHAs[engine] = strings.Repeat("b", 64)
		raw, _ := json.Marshal(ix.ProcessMember{BootID: "00000000-0000-0000-0000-000000000001", PID: 1, StartTicks: 1, BinarySHA: e.BinarySHAs[engine], ConfigSHA: ix.BytesSHA([]byte(id + "/" + name))})
		group[name] = string(raw)
	}
	if len(b.NativeConfig) > 0 {
		add("xray", "xray")
	}
	for _, c := range b.Components {
		add(c.Engine+"/"+c.Tag, c.Engine)
	}
	raw, _ := json.Marshal(group)
	e.ProcessIdentity = string(raw)
	return e
}
func executionReport(b ix.Bundle, e ix.EpochInput, seq, up, down uint64, final bool) ix.UsageReport {
	r := ix.UsageReport{EpochID: e.EpochID, NodeID: e.NodeID, InstallationID: e.InstallationID, Generation: e.Generation, Sequence: seq, ProcessIdentity: e.ProcessIdentity, Final: final, Counters: []ix.Counter{}}
	for _, p := range b.Principals {
		r.Counters = append(r.Counters, ix.Counter{PrincipalID: p.ID, Upload: up, Download: down})
	}
	if final {
		r.FinalNonce = strings.Repeat("c", 64)
		r.FinalProofSHA = executionFinalSHA(r)
	}
	return r
}
func executionObservation(b ix.Bundle, e ix.EpochInput) ix.Observation {
	return ix.Observation{NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: b.Generation, OperationID: b.OperationID, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, State: "active", EpochID: e.EpochID, ObservedUnixNano: time.Now().UnixNano(), LeaseExpires: time.Now().Add(25 * time.Second).UnixNano(), ProcessReady: true, HealthState: "healthy", AccountingState: "accepted"}
}
func (f *executionFixture) ready(t *testing.T, b ix.Bundle, e ix.EpochInput) {
	t.Helper()
	if _, err := f.s.AcceptInboundExecutionUsage(context.Background(), f.key, executionReport(b, e, 1, 0, 0, false)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ObserveInboundExecution(context.Background(), f.key, executionObservation(b, e)); err != nil {
		t.Fatal(err)
	}
}

func TestManagedInboundFullLifecycleAndFinancialReplay(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	bound, err := f.s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{EventID: "execution-bind-clients-0001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Select managed customers"})
	if err != nil || bound.Revision != 1 || bound.ActiveRevision != 0 {
		t.Fatal(bound, err)
	}
	op := f.command(t, "execution-activate-0001", "activate")
	if op.Revision != 2 || op.State != "queued" {
		t.Fatal(op)
	}
	b := f.bundle(t)
	if b.NodeID == "hub-local" || len(b.Listeners) != 1 || len(b.Principals) != 1 || b.Listeners[0].Listen != "198.51.100.10" || ix.VerifyBundle(f.key, b) != nil {
		t.Fatal("incorrect explicit target bundle")
	}
	for _, table := range []string{"native_inbound_targets", "native_inbound_clients", "native_inbound_plan_promotions", "native_metered_route_commands"} {
		if planCount(t, f.s, table) != 0 {
			t.Fatal("legacy namespace mutated", table)
		}
	}
	e := f.birth(t, b, "epoch-managed-0001")
	if err = f.s.ObserveInboundExecution(ctx, f.key, executionObservation(b, e)); !errors.Is(err, ErrConflict) {
		t.Fatal("ready without durable zero", err)
	}
	f.ready(t, b, e)
	r := executionReport(b, e, 2, 100, 200, false)
	receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r)
	if err != nil || receipt.SHA256 != ix.Digest(r) {
		t.Fatal(err)
	}
	again, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r)
	if err != nil || !again.Replayed {
		t.Fatal(err)
	}
	usage, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || usage.Uplink != 100 || usage.Downlink != 200 {
		t.Fatal("duplicate charge", usage, err)
	}
	bad := r
	bad.Counters = append([]ix.Counter{}, r.Counters...)
	bad.Counters[0].Upload++
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, bad); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting replay", err)
	}
	loaded, err := f.s.NativeInboundPlan(ctx, f.key, 501)
	if err != nil || !loaded.Editable || loaded.Profile.Enabled || loaded.Profile.BoundClients != 0 {
		t.Fatal("managed plan lost draft semantics", err)
	}
	edit := f.plan
	edit.EventID = "execution-edit-active-0001"
	edit.ExpectedPlanRevision = 1
	edit.ExpectedInboundRevision = 1
	edit.Profile = loaded.Profile.InboundInput
	edit.Profile.Name = "Updated managed profile"
	edit.Nodes = append([]NativeInboundPlanNode{}, edit.Nodes...)
	edit.Nodes[0].DirectPort++
	if _, err = f.s.SaveNativeInboundPlan(ctx, f.key, edit); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || !state.PendingChanges || state.ActiveRevision != 2 {
		t.Fatal("active edit overwritten snapshot", state, err)
	}
	if f.bundle(t).ConfigSHA != b.ConfigSHA {
		t.Fatal("draft changed active configuration")
	}
	f.command(t, "execution-apply-edited-0001", "apply")
	next := f.bundle(t)
	candidate := executionEpoch(next, "epoch-managed-0002")
	if _, err = f.s.RegisterInboundExecutionEpoch(ctx, f.key, candidate); !errors.Is(err, ErrConflict) {
		t.Fatal("new birth before old seal", err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 3, 110, 220, true)); err != nil {
		t.Fatal("final old generation after desired replacement", err)
	}
	e2 := f.birth(t, next, "epoch-managed-0002")
	f.ready(t, next, e2)
	state, err = f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.PendingChanges || state.ActivePlanRevision != 2 {
		t.Fatal(state, err)
	}
	f.command(t, "execution-stop-active-0001", "stop")
	stop := f.bundle(t)
	if len(stop.Listeners) != 0 || len(stop.Principals) != 0 {
		t.Fatal("stop retained listeners")
	}
	o := executionObservation(stop, ix.EpochInput{})
	o.State = "stopped"
	o.ProcessReady = false
	o.LeaseExpires = 0
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); !errors.Is(err, ErrConflict) {
		t.Fatal("stop without final accounting", err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(next, e2, 2, 0, 0, true)); err != nil {
		t.Fatal(err)
	}
	o.ObservedUnixNano = time.Now().UnixNano()
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	state, err = f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.DesiredState != "stopped" || state.ActiveRevision != 0 || len(state.EffectiveClientIDs) != 0 {
		t.Fatal(state, err)
	}
	usage, _ = f.s.ClientUsage(ctx, f.client.ID)
	if usage.Uplink != 110 || usage.Downlink != 220 {
		t.Fatal("final usage lost", usage)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal("schema16 reopen", err)
	}
	defer reopened.Close()
	state, err = reopened.InboundExecution(ctx, f.key, 501)
	if err != nil || state.DesiredState != "stopped" {
		t.Fatal(state, err)
	}
}

func TestManagedInboundStopUsesReviewedTargetsAfterUnsentDraftEdit(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-stop-draft-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-stop-draft-0001")
	f.ready(t, b, e)
	loaded, err := f.s.NativeInboundPlan(ctx, f.key, 501)
	if err != nil {
		t.Fatal(err)
	}
	edit := copiedPlan(f.plan)
	edit.EventID = "execution-stop-draft-edit-0001"
	edit.ExpectedPlanRevision = loaded.PlanRevision
	edit.ExpectedInboundRevision = loaded.InboundRevision
	edit.Profile = loaded.Profile.InboundInput
	edit.Nodes[0].NodeID = "remote-unapplied"
	edit.Nodes[0].InstallationID = strings.Repeat("d", 32)
	edit.Nodes[0].ListenIP = "198.51.100.12"
	if _, err = f.s.SaveNativeInboundPlan(ctx, f.key, edit); err != nil {
		t.Fatal(err)
	}
	op := f.command(t, "execution-stop-draft-stop-0001", "stop")
	if len(op.Targets) != 1 || op.Targets[0].NodeID != b.NodeID {
		t.Fatal("stop claimed an unsent draft target", op.Targets)
	}
	if _, found, err := f.s.InboundExecutionBundle(ctx, f.key, "remote-unapplied", strings.Repeat("d", 32)); err != nil || found {
		t.Fatal("unsent draft node acquired managed ownership", found, err)
	}
	stopped := f.bundle(t)
	if len(stopped.Listeners) != 0 || stopped.Generation != b.Generation+1 {
		t.Fatal("reviewed target was not stopped")
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 0, 0, true)); err != nil {
		t.Fatal(err)
	}
	o := executionObservation(stopped, ix.EpochInput{})
	o.State, o.ProcessReady, o.LeaseExpires = "stopped", false, 0
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
}

func TestManagedInboundRevokeReconcilesAndRejectsForeignEpochReports(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-revoke-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-revoke-0001")
	f.ready(t, b, e)
	foreign := executionReport(b, e, 2, 100, 200, false)
	foreign.NodeID = "foreign-node"
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, foreign); !errors.Is(err, ErrConflict) {
		t.Fatal("foreign principal", err)
	}
	if _, err := f.s.SetClientState(ctx, SetClientState{EventID: "execution-revoke-client-0001", ClientID: f.client.ID, Action: "disable", Reason: "Explicit client revocation"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.InboundExecutionAuthority(ctx, f.key, b.NodeID, b.InstallationID, b.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked customer renewed", err)
	}
	changed, err := f.s.RefreshInboundExecutionEligibility(ctx, f.key, b.NodeID, b.InstallationID)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	next := f.bundle(t)
	if next.Generation != b.Generation+1 || len(next.Listeners) != 0 {
		t.Fatal("revoke did not withdraw listener")
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 17, 31, true)); err != nil {
		t.Fatal("revocation discarded final bytes", err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 3, 18, 32, false)); !errors.Is(err, ErrConflict) {
		t.Fatal("sealed epoch reopened", err)
	}
}

func TestManagedInboundOwnerAggregateAndDowngradeFence(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	in := f.plan
	in.EventID = "execution-tunnel-plan-0001"
	in.Profile = InboundInput{ID: 502, Name: "Owner tunnel", Spec: &inboundspec.Spec{Version: 1, Protocol: "tunnel", Network: "tcp", Security: "none", Settings: map[string]any{"rewriteAddress": "example.org", "rewritePort": 443, "allowedNetwork": "tcp"}}}
	in.Nodes = append([]NativeInboundPlanNode{}, in.Nodes...)
	in.Nodes[0].DirectPort = 23185
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, in); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 502)
	if err != nil {
		t.Fatal(err)
	}
	cmd := ix.CommandInput{EventID: "execution-aggregate-activate-0001", Action: "activate", ExpectedRevision: 0, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{}, AccountingMode: ix.AccountingOwnerAggregate, Reason: "Owner aggregate listener"}
	p, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, cmd)
	if err != nil || !p.Valid {
		t.Fatal(p, err)
	}
	cmd.ReviewSHA = p.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, f.key, "owner", 502, cmd); err != nil {
		t.Fatal(err)
	}
	b := f.bundle(t)
	if len(b.Principals) != 1 || b.Principals[0].Billable || b.Principals[0].ClientID != "" || b.Principals[0].Role != "owner_aggregate" {
		t.Fatal("fabricated retail attribution", b.Principals)
	}
	e := f.birth(t, b, "epoch-aggregate-0001")
	f.ready(t, b, e)
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 47, 83, false)); err != nil {
		t.Fatal(err)
	}
	usage, _ := f.s.ClientUsage(ctx, f.client.ID)
	if usage.Uplink != 0 || usage.Downlink != 0 {
		t.Fatal("aggregate charged retail")
	}
	var total int64
	if err = f.s.db.QueryRow(`SELECT upload+download FROM inbound_execution_aggregate_totals`).Scan(&total); err != nil || total != 130 {
		t.Fatal(total, err)
	}
	if _, err = f.s.db.Exec(`UPDATE inbound_execution_bundles SET config_sha='bad'`); err == nil {
		t.Fatal("immutable bundle editable")
	}
	if _, err = f.s.db.Exec(`PRAGMA user_version=8`); err != nil {
		t.Fatal(err)
	}
	f.s.Close()
	if reopened, err := Open(f.path); err == nil {
		reopened.Close()
		t.Fatal("downgraded execution database accepted")
	}
}

func TestManagedInboundSiblingBundleAndAtomicSharedRevocation(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-sibling-first-0001", "activate")
	first := f.bundle(t)
	e1 := f.birth(t, first, "epoch-sibling-first")
	f.ready(t, first, e1)
	plan := copiedPlan(f.plan)
	plan.EventID = "execution-create-sibling-0001"
	plan.Profile.ID = 502
	plan.Profile.Name = "Second managed inbound"
	plan.Nodes[0].DirectPort++
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, plan); err != nil {
		t.Fatal(err)
	}
	in := ix.CommandInput{EventID: "execution-sibling-second-0001", Action: "activate", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Co-resident reviewed inbound"}
	preview, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, in)
	if err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	in.ReviewSHA = preview.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, f.key, "owner", 502, in); err != nil {
		t.Fatal(err)
	}
	combined := f.bundle(t)
	if len(combined.Listeners) != 2 || len(combined.Principals) != 2 || combined.Principals[0].Counter == combined.Principals[1].Counter {
		t.Fatal("sibling overwritten or counter doubled", combined.Principals)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(first, e1, 2, 3, 4, true)); err != nil {
		t.Fatal(err)
	}
	e2 := f.birth(t, combined, "epoch-sibling-second")
	f.ready(t, combined, e2)
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.Targets[0].DesiredGeneration != combined.Generation || !state.Targets[0].ProcessReady || len(state.Targets[0].Listeners) != 1 {
		t.Fatal("sibling state stuck on historical node generation", state, err)
	}
	if _, err = f.s.SetClientState(ctx, SetClientState{EventID: "execution-revoke-shared-0001", ClientID: f.client.ID, Action: "disable", Reason: "Revoke across all co-resident routes"}); err != nil {
		t.Fatal(err)
	}
	changed, err := f.s.RefreshInboundExecutionEligibility(ctx, f.key, combined.NodeID, combined.InstallationID)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	stopped := f.bundle(t)
	if len(stopped.Listeners) != 0 || stopped.Generation != combined.Generation+1 {
		t.Fatal("shared revoke was partial")
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(combined, e2, 2, 10, 20, true)); err != nil {
		t.Fatal(err)
	}
	o := executionObservation(stopped, ix.EpochInput{})
	o.State = "stopped"
	o.ProcessReady = false
	o.LeaseExpires = 0
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{501, 502} {
		v, e := f.s.InboundExecution(ctx, f.key, id)
		if e != nil || v.ObservedState != "blocked" || v.ActiveRevision != 0 || v.LatestOperation == nil || v.LatestOperation.State != "blocked" {
			t.Fatal("component did not settle each inbound", id, v, e)
		}
	}
}

func TestManagedInboundUncertainTailHoldsSharedQuotaAndKeepsKnownBytes(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-uncertain-activate-0001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "epoch-uncertain-0001")
	f.ready(t, b, epoch)
	observation := executionObservation(b, epoch)
	observation.State = "failed"
	observation.ProcessReady = false
	observation.AccountingState = "uncertain"
	observation.ErrorCode = "accounting_tail_uncertain"
	if err := f.s.ObserveInboundExecution(ctx, f.key, observation); err != nil {
		t.Fatal(err)
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Holds != 1 || account.Eligible {
		t.Fatal("unobserved tail not held", account, err)
	}
	if err = f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, epoch.EpochID, "Repeated crash report"); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 {
		t.Fatal("duplicate hold")
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, epoch, 2, 53, 97, false)); err != nil {
		t.Fatal("known outbox usage discarded", err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, epoch, 3, 59, 101, true)); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 || account.Uplink != 59 || account.Downlink != 101 || account.Eligible {
		t.Fatal("uncertainty silently forgiven", account)
	}
	var status string
	if err = f.s.db.QueryRow(`SELECT state FROM inbound_execution_epochs WHERE epoch_id=?`, epoch.EpochID).Scan(&status); err != nil || status != "uncertain" {
		t.Fatal(status, err)
	}
}

func TestManagedInboundRevokedBeforeZeroCanOnlySealUnopenedBirth(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-before-zero-activate-0001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "epoch-before-zero-0001")
	if _, err := f.s.SetClientState(ctx, SetClientState{EventID: "execution-before-zero-revoke-0001", ClientID: f.client.ID, Action: "disable", Reason: "Revoke before admission"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RegisterInboundExecutionEpoch(ctx, f.key, epoch); err != nil {
		t.Fatal("lost historical registration became unrecoverable", err)
	}
	if err := f.s.InboundExecutionAuthority(ctx, f.key, b.NodeID, b.InstallationID, b.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("historical registration granted revoked authority", err)
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, epoch, 1, 0, 0, false)); err != nil {
		t.Fatal("zero receipt was confused with admission authority", err)
	}
	if err := f.s.ObserveInboundExecution(ctx, f.key, executionObservation(b, epoch)); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked generation advertised ready", err)
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, epoch, 2, 0, 0, true)); err != nil {
		t.Fatal("proven unopened birth permanently fenced", err)
	}
}

func TestManagedInboundUsageFailureRollsBackReceiptCounterAndQuotaTogether(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-atomic-usage-0001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "epoch-atomic-usage-0001")
	f.ready(t, b, epoch)
	if _, err := f.s.db.Exec(`CREATE TRIGGER execution_test_fail_delta BEFORE INSERT ON inbound_execution_usage_deltas WHEN NEW.sequence=2 BEGIN SELECT RAISE(ABORT,'synthetic storage failure after totals'); END`); err != nil {
		t.Fatal(err)
	}
	report := executionReport(b, epoch, 2, 100, 200, false)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, report); err == nil {
		t.Fatal("failed write returned a receipt")
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Uplink != 0 || account.Downlink != 0 {
		t.Fatal("partial financial commit", account, err)
	}
	var sequence uint64
	if err = f.s.db.QueryRow(`SELECT sequence FROM inbound_execution_epochs WHERE epoch_id=?`, epoch.EpochID).Scan(&sequence); err != nil || sequence != 1 {
		t.Fatal("cursor committed without deltas", sequence, err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER execution_test_fail_delta`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, report); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Uplink != 100 || account.Downlink != 200 {
		t.Fatal(account)
	}
}

func TestManagedInboundAccountingReadinessExpiresWithoutFreshReports(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-report-fresh-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-report-fresh-0001")
	f.ready(t, b, e)
	tx, err := executionTx(f.s, ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Now().UnixNano()
	until, err := executionAccountingFreshUntil(ctx, tx, b.NodeID, b.InstallationID, b.Generation, e.EpochID, now)
	if err != nil || until <= now || until > now+int64(15*time.Second) {
		t.Fatal("fresh financial evidence missing", until, err)
	}
	if _, err = executionAccountingFreshUntil(ctx, tx, b.NodeID, b.InstallationID, b.Generation, e.EpochID, now+int64(16*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatal("historic zero receipt sustained publication indefinitely", err)
	}
}
