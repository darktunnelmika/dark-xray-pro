package business

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
)

func TestManagedArchiveReservationReleasedOnlyAfterAcknowledgement(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	replacement := f.plan
	replacement.Profile.ID = 502
	replacement.EventID = "replacement-after-archive-0001"
	replacement.Nodes = append([]NativeInboundPlanNode(nil), f.plan.Nodes...)
	replacement.Nodes[0].ListenIP = "198.51.100.20"
	checkConflict := func() {
		t.Helper()
		if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, replacement); !errors.Is(err, ErrConflict) {
			t.Fatalf("unacknowledged reservation released: %v", err)
		}
	}
	checkConflict()
	f.command(t, "archive-reservation-request-0001", "archive")
	checkConflict()
	b := f.bundle(t)
	o := executionObservation(b, ix.EpochInput{})
	o.State = "stopped"
	o.ProcessReady = false
	o.LeaseExpires = 0
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"stop", "activate", "restart"} {
		_, err = f.s.PreviewInboundExecution(ctx, f.key, "owner", 501, ix.CommandInput{EventID: "closed-archive-reject-" + action, Action: action, ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, Reason: "Completed archives remain immutable"})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("completed archive accepted %s: %v", action, err)
		}
	}
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, replacement); err != nil {
		t.Fatalf("completed archive retained reservation: %v", err)
	}
}
func TestManagedPendingArchiveCanBeSafelyStopped(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	f.command(t, "archive-pending-request-0001", "archive")
	f.command(t, "archive-pending-stop-0001", "stop")
	b := f.bundle(t)
	if len(b.Listeners) != 0 || len(b.Principals) != 0 {
		t.Fatal("recovery stop retained listeners")
	}
	o := executionObservation(b, ix.EpochInput{})
	o.State = "stopped"
	o.ProcessReady = false
	o.LeaseExpires = 0
	if err := f.s.ObserveInboundExecution(context.Background(), f.key, o); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(context.Background(), f.key, 501)
	if err != nil || state.DesiredState != "stopped" || state.ObservedState != "stopped" {
		t.Fatalf("stop did not settle: %#v %v", state, err)
	}
	f.command(t, "archive-recovery-final-0001", "archive")
}
