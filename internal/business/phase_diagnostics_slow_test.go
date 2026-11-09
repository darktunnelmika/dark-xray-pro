package business

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestBusinessPhaseDiagnosticsSlowCompletionSurvivesOrdinaryFlood(t *testing.T) {
	s := diagnosticTestStore()
	d := s.phaseDiagnostics
	for n := 0; n < diagnosticQueueCapacity*4; n++ {
		d.record(diagnosticTestRecord())
	}
	_, span := s.BeginDiagnostic(context.Background(), DiagnosticOperationUsage, DiagnosticSourceStore)
	span.Enter(DiagnosticPhaseCommit)
	// Advance the span's clock origin, not the test wall clock.
	span.phaseStarted = time.Now().Add(-2 * time.Second)
	span.started = span.phaseStarted
	span.End(nil)
	if d.snapshot().active != 0 || len(d.slowQueue) != 2 {
		t.Fatal("ordinary flood hid slow completion or left a current holder")
	}
	commit, ok := d.nextRecord(true)
	if !ok || commit.kind != diagnosticPhaseEnd || commit.phase != DiagnosticPhaseCommit || commit.duration < int64(2*time.Second) || commit.span != span.sequence {
		t.Fatalf("slow commit was not selected first: %+v", commit)
	}
	ordinary, ok := d.nextRecord(false)
	if !ok || ordinary.kind != diagnosticPhaseBegin {
		t.Fatal("ordinary lane was starved")
	}
	exit, ok := d.nextRecord(true)
	if !ok || exit.kind != diagnosticScopeExit || exit.span != span.sequence {
		t.Fatal("slow scope exit missing")
	}
}

func TestBusinessPhaseDiagnosticsSlowBoundedLossAndShutdown(t *testing.T) {
	d := newPhaseDiagnostics(nil)
	r := diagnosticTestRecord()
	r.kind, r.duration = diagnosticPhaseEnd, int64(time.Second)
	for n := 0; n < diagnosticSlowCapacity+7; n++ {
		d.record(r)
	}
	for n := 0; n < diagnosticQueueCapacity+3; n++ {
		d.record(diagnosticTestRecord())
	}
	s := d.snapshot()
	if s.pending != diagnosticSlowCapacity+diagnosticQueueCapacity || s.queueFull != 10 || s.attempted != s.enqueued+s.queueFull {
		t.Fatal("bounded admission accounting mismatch")
	}
	d.flushEvents(nil)
	if len(d.slowQueue) != diagnosticSlowCapacity-diagnosticBatchLimit/2 || len(d.queue) != diagnosticQueueCapacity-diagnosticBatchLimit/2 || d.snapshot().transportDropped != diagnosticBatchLimit {
		t.Fatal("batch lost fairness, output bound, or transport-loss accounting")
	}
	d.closed.Store(true)
	d.discardQueued()
	d.record(r)
	s = d.snapshot()
	if s.pending != 0 || s.closedDropped != 1 || s.enqueued != s.transportDropped+s.shutdownDropped || s.attempted != s.enqueued+s.queueFull+s.closedDropped {
		t.Fatal("shutdown loss was hidden")
	}
}

func TestBusinessPhaseDiagnosticsSlowThresholdAndBorrowing(t *testing.T) {
	d := newPhaseDiagnostics(nil)
	r := diagnosticTestRecord()
	r.kind, r.duration = diagnosticPhaseEnd, int64(time.Second)-1
	d.record(r)
	r.duration++
	d.record(r)
	if len(d.queue) != 1 || len(d.slowQueue) != 1 {
		t.Fatal("threshold boundary mismatch")
	}
	d.flushEvents(nil)
	if d.snapshot().pending != 0 || d.snapshot().transportDropped != 2 {
		t.Fatal("unused lane slots were not borrowed")
	}
	for n := 0; n < diagnosticBatchLimit; n++ {
		d.record(r)
	}
	d.flushEvents(nil)
	if d.snapshot().pending != 0 || d.snapshot().transportDropped != diagnosticBatchLimit+2 {
		t.Fatal("slow-only batch was not drained within the output bound")
	}
}

func TestBusinessPhaseDiagnosticsSlowConcurrentAdmission(t *testing.T) {
	d := newPhaseDiagnostics(nil)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				r := diagnosticTestRecord()
				if n%2 == 0 {
					r.kind, r.duration = diagnosticPhaseEnd, int64(time.Second)
				}
				d.record(r)
			}
		}()
	}
	for n := 0; n < 100; n++ {
		d.flushEvents(nil)
	}
	d.closed.Store(true)
	wg.Wait()
	d.discardQueued()
	s := d.snapshot()
	if s.pending != 0 || s.producers != 0 || s.attempted != 8000 || s.attempted != s.enqueued+s.queueFull+s.closedDropped || s.enqueued != s.transportDropped+s.shutdownDropped {
		t.Fatal("concurrent accounting mismatch")
	}
}
