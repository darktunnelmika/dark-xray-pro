//go:build linux

package managedxray

import (
	"sync"
	"testing"
	"time"
)

// Hold the owned child's stop barrier so this contract does not depend on how
// quickly Linux kills/reaps a process or on when a polling loop gets scheduled.
func TestUsageObserverFailWaitsForOwnedProcessExit(t *testing.T) {
	f := newObserverFixture(t)
	process := f.runtime.process
	cancelled := make(chan struct{})
	var cancelOnce sync.Once
	observer := &usageObserver{
		process: process,
		cancel:  func() { cancelOnce.Do(func() { close(cancelled) }) },
		status: UsageObservationStatus{
			Required:           true,
			Running:            true,
			SettlementRequired: true,
		},
	}

	process.guardMu.Lock()
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(process.guardMu.Unlock) }
	// Also release the barrier when an assertion fails, before fixture cleanup.
	t.Cleanup(unlock)
	stopped := make(chan struct{})
	go func() {
		observer.fail("observation_failed")
		close(stopped)
	}()

	select {
	case <-cancelled:
	case <-time.After(4 * time.Second):
		t.Fatal("observer did not request cancellation")
	}

	// Failure is immediately visible and denies health, but is not proof that
	// the owned child has stopped. It is deliberately still listening here.
	status := observer.snapshot()
	if status.Failure != "observation_failed" || !status.Running {
		t.Fatalf("stop receipt published before child exit: %+v", status)
	}
	if observer.healthy() {
		t.Fatal("failing observer remained healthy")
	}
	if process.dead() {
		t.Fatal("owned child exited before stop barrier was released")
	}
	assertPort(t, f.candidate.Listeners[0].NodePort, true)
	select {
	case <-stopped:
		t.Fatal("failure handler returned before child exit")
	default:
	}

	unlock()
	select {
	case <-stopped:
	case <-time.After(4 * time.Second):
		t.Fatal("owned child stop did not complete")
	}
	status = observer.snapshot()
	if status.Running || status.Failure != "observation_failed" ||
		!status.Required || !status.SettlementRequired {
		t.Fatalf("invalid completed stop receipt: %+v", status)
	}
	if !process.dead() {
		t.Fatal("stopped observer retained a live owned child")
	}
	assertPort(t, f.candidate.Listeners[0].NodePort, false)

	observer.fail("observer_stopped")
	if observer.snapshot().Failure != "observation_failed" {
		t.Fatal("repeated failure replaced the original failure reason")
	}
}
