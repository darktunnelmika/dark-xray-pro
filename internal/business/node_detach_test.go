package business

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"

	"reflect"
	"strings"
	"testing"
)

func TestDetachNodeDestinationAndRelay(t *testing.T) {
	a := NativeInboundPlanNode{NodeID: "a", InstallationID: strings.Repeat("a", 32), DirectPort: 21185, TunnelPort: 21186}
	b := NativeInboundPlanNode{NodeID: "b", InstallationID: strings.Repeat("b", 32), DirectPort: 21185, TunnelPort: 21186, SwapEnabled: true, SwapSourceNodeID: a.NodeID, SwapSourceInstallationID: a.InstallationID, SwapSourcePort: 21187, SwapEntryAddress: "entry.example", SwapEntryPort: 21187, SwapDestinationPort: 21188}
	rows, changed := detachPlanNode([]NativeInboundPlanNode{a, b}, "a", a.InstallationID)
	if !changed || len(rows) != 1 || rows[0].NodeID != "b" || rows[0].SwapEnabled || rows[0].SwapSourceNodeID != "" || rows[0].SwapDestinationPort != 0 || rows[0].DirectPort != b.DirectPort || rows[0].TunnelPort != b.TunnelPort {
		t.Fatal(rows, changed)
	}
	if _, changed = detachPlanNode([]NativeInboundPlanNode{a, b}, "a", strings.Repeat("c", 32)); changed {
		t.Fatal("foreign installation detached")
	}
}
func TestDetachManagedNodeOfflinePreservesCustomerAndFinalAccounting(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	// Activate two destinations. The second never acknowledges or opens an
	// epoch, modelling an offline participant that used to block the whole op.
	p, e := f.s.NativeInboundPlan(ctx, f.key, 501)
	if e != nil {
		t.Fatal(e)
	}
	in := f.plan
	in.EventID = "detach-add-second-node-0001"
	in.ExpectedPlanRevision = p.PlanRevision
	in.ExpectedInboundRevision = p.InboundRevision
	b := in.Nodes[0]
	b.NodeID = "remote-b"
	b.InstallationID = strings.Repeat("b", 32)
	b.ListenIP = "198.51.100.20"
	b.DirectHosts = []string{"b.example.org"}
	in.Nodes = append(in.Nodes, b)
	if _, e = f.s.SaveNativeInboundPlan(ctx, f.key, in); e != nil {
		t.Fatal(e)
	}
	f.command(t, "detach-active-two-nodes-0001", "activate")
	oldBundle := f.bundle(t)
	epoch := f.birth(t, oldBundle, "detach-live-old-epoch-0001")
	if _, e = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(oldBundle, epoch, 1, 0, 0, false)); e != nil {
		t.Fatal(e)
	}
	before, e := f.s.Client(ctx, f.client.ID)
	if e != nil {
		t.Fatal(e)
	}
	ids, e := f.s.DetachManagedNode(ctx, f.key, "owner", "detach-owner-offline-0001", "remote-b", b.InstallationID)
	if e != nil || !reflect.DeepEqual(ids, []uint64{501}) {
		t.Fatal(ids, e)
	}
	p, e = f.s.NativeInboundPlan(ctx, f.key, 501)
	if e != nil || len(p.Nodes) != 1 || p.Nodes[0].NodeID != "remote-a" {
		t.Fatal(p, e)
	}
	state, e := f.s.InboundExecution(ctx, f.key, 501)
	if e != nil || len(state.Targets) != 1 || state.Targets[0].NodeID != "remote-a" || !reflect.DeepEqual(state.ClientIDs, []string{f.client.ID}) {
		t.Fatal(state, e)
	}
	if after, e := f.s.Client(ctx, f.client.ID); e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("customer changed", after, e)
	}
	// A genuine late final from the superseded generation remains admissible.
	if _, e = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(oldBundle, epoch, 2, 11, 19, true)); e != nil {
		t.Fatal("late final rejected", e)
	}
	next := f.bundle(t)
	newEpoch := f.birth(t, next, "detach-live-new-epoch-0001")
	if _, e = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(next, newEpoch, 1, 0, 0, false)); e != nil {
		t.Fatal(e)
	}
	o := executionObservation(next, newEpoch)
	// No synthetic acknowledgement from offline remote-b is submitted.
	if e = f.s.ObserveInboundExecution(ctx, f.key, o); e != nil {
		t.Fatal(e)
	}
	state, e = f.s.InboundExecution(ctx, f.key, 501)
	if e != nil || state.OwnerResumeRequired || state.ObservedState != "active" {
		t.Fatal(state, e)
	}
	// Replay after an ambiguous response cannot reattach the destination.
	if _, e = f.s.DetachManagedNode(ctx, f.key, "owner", "detach-owner-offline-0001", "remote-b", b.InstallationID); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=? AND final=1`, epoch.EpochID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
func TestDetachManagedLastNodeRetainsInboundAndClient(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "detach-last-node-active-0001", "activate")
	oldBundle := f.bundle(t)
	oldEpoch := f.birth(t, oldBundle, "detach-last-live-epoch-0001")
	f.ready(t, oldBundle, oldEpoch)
	ids, e := f.s.DetachManagedNode(ctx, f.key, "owner", "detach-last-node-0001", "remote-a", strings.Repeat("a", 32))
	if e != nil || len(ids) != 1 {
		t.Fatal(ids, e)
	}
	p, e := f.s.NativeInboundPlan(ctx, f.key, 501)
	if e != nil || len(p.Nodes) != 0 || p.Profile.ID != 501 {
		t.Fatal(p, e)
	}
	state, e := f.s.InboundExecution(ctx, f.key, 501)
	if e != nil || len(state.Targets) != 0 || len(state.ClientIDs) != 1 || state.ClientIDs[0] != f.client.ID {
		t.Fatal(state, e)
	}
	if _, e = f.s.Client(ctx, f.client.ID); e != nil {
		t.Fatal(e)
	}
	var sealed int
	if e = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_epochs WHERE state='sealed'`).Scan(&sealed); e != nil || sealed != 0 {
		t.Fatal("fabricated settlement", sealed, e)
	}
	// A final from the detached installation still settles exact measured bytes.
	if _, e = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(oldBundle, oldEpoch, 2, 23, 41, true)); e != nil {
		t.Fatal("detached final rejected", e)
	}
	var up, down int64
	if e = f.s.db.QueryRow(`SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?`, f.client.ID).Scan(&up, &down); e != nil || up != 23 || down != 41 {
		t.Fatal("detached final lost or double charged", up, down, e)
	}
}

func TestDetachManagedNodeDoesNotApplySavedDraft(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "detach-draft-active-0001", "activate")
	oldBundle := f.bundle(t)
	epoch := f.birth(t, oldBundle, "detach-draft-old-epoch-0001")
	f.ready(t, oldBundle, epoch)
	p, e := f.s.NativeInboundPlan(ctx, f.key, 501)
	if e != nil {
		t.Fatal(e)
	}
	in := f.plan
	in.EventID = "detach-draft-save-0001"
	in.ExpectedPlanRevision = p.PlanRevision
	in.ExpectedInboundRevision = p.InboundRevision
	in.Profile.Name = "Unapplied draft name"
	in.Nodes = append([]NativeInboundPlanNode{}, in.Nodes...)
	in.Nodes[0].DirectPort++
	if _, e = f.s.SaveNativeInboundPlan(ctx, f.key, in); e != nil {
		t.Fatal(e)
	}
	// The detached node was the sole destination; the saved draft must survive
	// as an editable profile without claiming its name/port were ever applied.
	if _, e = f.s.DetachManagedNode(ctx, f.key, "owner", "detach-draft-owner-0001", "remote-a", strings.Repeat("a", 32)); e != nil {
		t.Fatal(e)
	}
	tx, e := executionTx(f.s, ctx, f.key)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	h, e := readExecutionHead(ctx, tx, f.key, 501)
	if e != nil {
		t.Fatal(e)
	}
	s, e := readExecutionSnapshot(ctx, tx, f.key, 501, h.DesiredRevision)
	if e != nil {
		t.Fatal(e)
	}
	if s.Name == in.Profile.Name || s.Ref.PlanRevision != p.PlanRevision || len(s.Nodes) != 0 {
		t.Fatal("unsent draft applied", s.Name, s.Ref)
	}
}

func TestDetachOfflineNodeKeepsUnrelatedHealthyGeneration(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "detach-healthy-active-0001", "activate")
	bundle := f.bundle(t)
	epoch := f.birth(t, bundle, "detach-healthy-old-epoch-0001")
	f.ready(t, bundle, epoch)
	plan := f.plan
	plan.Profile.ID = 502
	plan.EventID = "detach-stopped-sibling-plan-0001"
	plan.Nodes = append([]NativeInboundPlanNode{}, plan.Nodes...)
	plan.Nodes[0].DirectPort += 10
	offline := plan.Nodes[0]
	offline.NodeID = "remote-b"
	offline.InstallationID = strings.Repeat("b", 32)
	offline.ListenIP = "198.51.100.20"
	offline.DirectHosts = []string{"b.example.org"}
	plan.Nodes = append(plan.Nodes, offline)
	if _, e := f.s.SaveNativeInboundPlan(ctx, f.key, plan); e != nil {
		t.Fatal(e)
	}
	state, e := f.s.InboundExecution(ctx, f.key, 502)
	if e != nil {
		t.Fatal(e)
	}
	in := ix.CommandInput{EventID: "detach-stopped-sibling-command-0001", Action: "stop", ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, Reason: "Offline sibling test"}
	preview, e := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, in)
	if e != nil || !preview.Valid {
		t.Fatal(e, preview.Blockers)
	}
	in.ReviewSHA = preview.ReviewSHA
	if _, e = f.s.QueueInboundExecution(ctx, f.key, "owner", 502, in); e != nil {
		t.Fatal(e)
	}
	// A disconnected stopped sibling also references the removed node. Its
	// other offline destination must not enter inbound 502's completion barrier.
	another := f.plan
	another.Profile.ID = 503
	another.EventID = "detach-disconnected-sibling-plan-0001"
	another.Nodes = []NativeInboundPlanNode{offline}
	another.Nodes[0].DirectPort += 10
	c := another.Nodes[0]
	c.NodeID = "remote-c"
	c.InstallationID = strings.Repeat("c", 32)
	c.ListenIP = "198.51.100.30"
	c.DirectHosts = []string{"c.example.org"}
	another.Nodes = append(another.Nodes, c)
	if _, e = f.s.SaveNativeInboundPlan(ctx, f.key, another); e != nil {
		t.Fatal(e)
	}
	cstate, e := f.s.InboundExecution(ctx, f.key, 503)
	if e != nil {
		t.Fatal(e)
	}
	cin := ix.CommandInput{EventID: "detach-disconnected-sibling-stop-0001", Action: "stop", ExpectedRevision: cstate.Revision, ExpectedPlanRevision: cstate.PlanRevision, ExpectedInboundRevision: cstate.InboundRevision, Reason: "Disconnected stopped sibling"}
	cp, e := f.s.PreviewInboundExecution(ctx, f.key, "owner", 503, cin)
	if e != nil || !cp.Valid {
		t.Fatal(e, cp.Blockers)
	}
	cin.ReviewSHA = cp.ReviewSHA
	if _, e = f.s.QueueInboundExecution(ctx, f.key, "owner", 503, cin); e != nil {
		t.Fatal(e)
	}
	// Settle the original healthy incarnation and acknowledge its current bundle.
	if _, e = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(bundle, epoch, 2, 5, 7, true)); e != nil {
		t.Fatal(e)
	}
	before := f.bundle(t)
	live := f.birth(t, before, "detach-healthy-current-epoch-0001")
	f.ready(t, before, live)
	if _, e = f.s.DetachManagedNode(ctx, f.key, "owner", "detach-offline-sibling-0001", "remote-b", offline.InstallationID); e != nil {
		t.Fatal(e)
	}
	after := f.bundle(t)
	if ix.Digest(before) != ix.Digest(after) {
		t.Fatal("unrelated healthy node received a new generation", before.Generation, after.Generation)
	}
	state, e = f.s.InboundExecution(ctx, f.key, 502)
	if e != nil || state.ObservedState != "stopped" {
		t.Fatal("offline node still blocks administrative completion", state, e)
	}
	other, e := f.s.InboundExecution(ctx, f.key, 503)
	if e != nil || other.ObservedState == "stopped" {
		t.Fatal("unrelated offline destination falsely acknowledged", other, e)
	}
	if len(state.LatestOperation.Targets) != 1 || state.LatestOperation.Targets[0].NodeID != "remote-a" {
		t.Fatal("foreign targets entered inbound completion barrier", state.LatestOperation)
	}
}

func TestDetachStoppedLastNodeCanArchiveWithoutRemoteAcknowledgement(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "detach-stopped-last-command-0001", "stop")
	if _, e := f.s.DetachManagedNode(ctx, f.key, "owner", "detach-stopped-last-node-0001", "remote-a", strings.Repeat("a", 32)); e != nil {
		t.Fatal(e)
	}
	s, e := f.s.InboundExecution(ctx, f.key, 501)
	if e != nil || s.ObservedState != "stopped" || len(s.Targets) != 0 {
		t.Fatal("empty stopped inbound requires no acknowledgement", s, e)
	}
	summaries, e := f.s.ListNativeInboundPlans(ctx, f.key)
	if e != nil || len(summaries) != 1 || len(summaries[0].NodeIDs) != 0 {
		t.Fatal("empty inbound broke the global plan index", summaries, e)
	}
	f.command(t, "detach-empty-owner-archive-0001", "archive")
	s, e = f.s.InboundExecution(ctx, f.key, 501)
	if e != nil || s.ObservedState != "archived" || s.DesiredState != "archived" || len(s.Targets) != 0 {
		t.Fatal("empty archive remained queued", s, e)
	}
	var n int
	if e = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_heads WHERE pending_operation<>''`).Scan(&n); e != nil || n != 0 {
		t.Fatal("empty topology left a pending command", n, e)
	}
	if _, e = f.s.Client(ctx, f.client.ID); e != nil {
		t.Fatal("archiving empty inbound removed customer", e)
	}
}

func TestManagedPendingStopCanBeArchivedWithoutForgingSettlement(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "pending-stop-for-archive-0001", "stop")
	f.command(t, "pending-stop-archive-owner-0001", "archive")
	s, e := f.s.InboundExecution(ctx, f.key, 501)
	if e != nil || s.DesiredState != "archived" {
		t.Fatal(s, e)
	}
	// The old node is still unacknowledged: retirement does not free the socket
	// reservation or fabricate a closed accounting epoch.
	other := f.plan
	other.Profile.ID = 502
	other.EventID = "pending-archive-port-reuse-0001"
	if _, e = f.s.SaveNativeInboundPlan(ctx, f.key, other); !errors.Is(e, ErrConflict) {
		t.Fatal("unacknowledged archive released its reservation", e)
	}
	var receipts int
	if e = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_usage_receipts`).Scan(&receipts); e != nil || receipts != 0 {
		t.Fatal("fabricated settlement", receipts, e)
	}
}
