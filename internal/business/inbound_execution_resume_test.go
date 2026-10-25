package business

import (
	"context"
	"errors"
	"strings"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestManagedInboundOwnerLossRequiresFreshReviewAfterAutomaticEligibilityGeneration(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	in := ix.CommandInput{EventID: "execution-resume-original-0001", Action: "activate", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Original reviewed activation"}
	preview, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 501, in)
	if err != nil || !preview.Valid {
		t.Fatal(err)
	}
	in.ReviewSHA = preview.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, f.key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-owner-resume-0001")
	f.ready(t, b, e)
	if err = f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID, "Lost incarnation"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RefreshInboundExecutionEligibility(ctx, f.key, b.NodeID, b.InstallationID); err != nil {
		t.Fatal(err)
	}
	loss := reviewedOwnerLoss(t, f, 501, "execution-resume-owner-loss-0001", e.EpochID, 1)
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, loss); err != nil {
		t.Fatal(err)
	}
	if changed, err := f.s.RefreshInboundExecutionEligibility(ctx, f.key, b.NodeID, b.InstallationID); err != nil || !changed {
		t.Fatal("fixture did not create a post-resolution automatic generation", changed, err)
	}
	auto := f.bundle(t)
	if auto.Generation <= b.Generation || len(auto.Listeners) == 0 {
		t.Fatal("fixture did not retain active desired intent")
	}
	if err = f.s.InboundExecutionAuthority(ctx, f.key, auto.NodeID, auto.InstallationID, auto.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("eligibility refresh resumed service without explicit review", err)
	}
	if replay, err := f.s.QueueInboundExecution(ctx, f.key, "owner", 501, in); err != nil || !replay.Replayed {
		t.Fatal("original receipt stopped being replayable", err)
	}
	if err = f.s.InboundExecutionAuthority(ctx, f.key, auto.NodeID, auto.InstallationID, auto.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("historical command replay silently authorized new admission", err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || !state.OwnerResumeRequired || state.ObservedState != "blocked" || state.PublicationState != "withheld" || !hasString(state.AllowedActions, "restart") {
		t.Fatal("fresh owner action was unavailable while a system operation was pending", state, err)
	}
	resume := f.command(t, "execution-resume-reviewed-0001", "restart")
	current := f.bundle(t)
	if err = f.s.InboundExecutionAuthority(ctx, f.key, current.NodeID, current.InstallationID, current.Generation); err != nil {
		t.Fatal("fresh reviewed restart did not release admission", err)
	}
	event, found, err := f.s.InboundExecutionResume(ctx, f.key, current.NodeID, current.InstallationID, e.EpochID, current.Generation)
	if err != nil || !found || event != resume.ID {
		t.Fatal("signed lease cannot identify the actual fresh owner decision", event, found, err)
	}
	if _, err = f.s.db.Exec(`UPDATE inbound_execution_resume_events SET operation_id=?`, in.EventID); err == nil {
		t.Fatal("owner resume audit was mutable")
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestManagedInboundOwnerLossPausesAllHistoricalTargetsUntilReviewedResume(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	plan := copiedPlan(f.plan)
	plan.EventID, plan.ExpectedPlanRevision, plan.ExpectedInboundRevision = "execution-resume-two-targets-plan-0001", 1, 1
	second := plan.Nodes[0]
	second.NodeID, second.InstallationID, second.ListenIP, second.DirectHosts = "remote-b", strings.Repeat("b", 32), "198.51.100.12", []string{"b.example.org"}
	plan.Nodes = append(plan.Nodes, second)
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, plan); err != nil {
		t.Fatal(err)
	}
	f.command(t, "execution-resume-two-targets-activate-0001", "activate")
	a := f.bundle(t)
	b, _, err := f.s.InboundExecutionBundle(ctx, f.key, second.NodeID, second.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	ea := f.birth(t, a, "epoch-resume-two-targets-a-0001")
	eb := f.birth(t, b, "epoch-resume-two-targets-b-0001")
	f.ready(t, a, ea)
	f.ready(t, b, eb)
	if err = f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, a.NodeID, a.InstallationID, ea.EpochID, "First target lost its incarnation"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, eb, 2, 0, 0, true)); err != nil {
		t.Fatal(err)
	}
	loss := reviewedOwnerLoss(t, f, 501, "execution-resume-two-targets-loss-0001", ea.EpochID, 1)
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, loss); err != nil {
		t.Fatal(err)
	}
	for _, target := range []ix.Bundle{a, b} {
		if err = f.s.InboundExecutionAuthority(ctx, f.key, target.NodeID, target.InstallationID, target.Generation); !errors.Is(err, ErrConflict) {
			t.Fatal("another historical target resumed from the released shared quota hold", target.NodeID, err)
		}
	}
	f.command(t, "execution-resume-two-targets-stop-0001", "stop")
	if required, err := func() (bool, error) {
		tx, err := executionTx(f.s, ctx, f.key)
		if err != nil {
			return false, err
		}
		defer tx.Rollback()
		return executionResumeRequired(ctx, tx, a.NodeID, a.InstallationID)
	}(); err != nil || !required {
		t.Fatal("stop command consumed an admission authorization", required, err)
	}
	f.command(t, "execution-resume-two-targets-new-activate-0001", "activate")
	for _, node := range []NativeInboundPlanNode{plan.Nodes[0], second} {
		current, _, err := f.s.InboundExecutionBundle(ctx, f.key, node.NodeID, node.InstallationID)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.s.InboundExecutionAuthority(ctx, f.key, node.NodeID, node.InstallationID, current.Generation); err != nil {
			t.Fatal("reviewed complete bundle did not clear its target scope", err)
		}
	}
}
