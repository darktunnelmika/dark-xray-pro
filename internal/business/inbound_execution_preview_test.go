package business

import (
	"context"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestManagedInboundPreviewReportsSiblingResourceConflictWithoutMutatingExecution(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-preview-active-0001", "activate")
	before := f.bundle(t)
	epoch := f.birth(t, before, "epoch-preview-active-0001")
	f.ready(t, before, epoch)
	// A saved edit changes the draft reservation, while the old listener stays
	// active until an explicitly reviewed apply. Execution preview must still
	// detect the immutable active snapshot's occupied port.
	edit := copiedPlan(f.plan)
	edit.EventID = "execution-preview-pending-edit-0001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
	edit.Nodes[0].DirectPort++
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, edit); err != nil {
		t.Fatal(err)
	}
	plan := copiedPlan(f.plan)
	plan.EventID, plan.Profile.ID, plan.Profile.Name = "execution-preview-collision-plan-0001", 502, "Conflicting second listener"
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, plan); err != nil {
		t.Fatal(err)
	}
	in := ix.CommandInput{EventID: "execution-preview-collision-0001", Action: "activate", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Review a conflicting candidate without replacing the running sibling"}
	preview, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, in)
	if err != nil || preview.Valid || preview.ReviewSHA != "" || len(preview.Blockers) != 1 || preview.Blockers[0].Code != "execution_resource_conflict" {
		t.Fatal("candidate conflict masqueraded as a storage failure", preview, err)
	}
	after := f.bundle(t)
	state, err := f.s.InboundExecution(ctx, f.key, 502)
	if err != nil || ix.Digest(before) != ix.Digest(after) || state.Revision != 0 || state.ActiveRevision != 0 {
		t.Fatal("failed preview changed durable execution", state, err)
	}
}
