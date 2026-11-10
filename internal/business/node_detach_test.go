package business

import (
	"context"

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