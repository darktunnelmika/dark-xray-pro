package business

import (
	"context"
	"errors"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// No exporter is started. Every diagnostic from the real Store must take the
// full-queue path while the original accounting and authentication work returns.
func fillBusinessDiagnosticQueue(t *testing.T, s *Store) func() {
	t.Helper()
	s.EnablePhaseDiagnostics()
	if s.phaseDiagnostics == nil || cap(s.phaseDiagnostics.queue) != diagnosticQueueCapacity {
		t.Fatal("fixed diagnostic queue was not enabled")
	}
	for len(s.phaseDiagnostics.queue) < diagnosticQueueCapacity {
		s.phaseDiagnostics.queue <- diagnosticRecord{}
	}
	return func() {
		t.Helper()
		if len(s.phaseDiagnostics.queue) != diagnosticQueueCapacity {
			t.Fatal("Store work unexpectedly consumed or replaced the saturated queue")
		}
		if s.db.Stats().InUse != 0 {
			t.Fatal("Store work retained a pooled connection")
		}
	}
}

type businessDiagnosticAccounting struct {
	uplink, downlink, baseline, holds       int64
	sequence, receipts, finals, cursorFinal int64
	firstSequence, lastSequence             int64
	state                                   string
}

func readBusinessDiagnosticAccounting(t *testing.T, s *Store, client, epoch string) businessDiagnosticAccounting {
	t.Helper()
	var got businessDiagnosticAccounting
	if err := s.db.QueryRow(`SELECT uplink,downlink,baseline_complete,holds FROM pro_usage_accounts WHERE client_id=?`, client).Scan(&got.uplink, &got.downlink, &got.baseline, &got.holds); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT e.state,e.sequence,COUNT(r.sequence),COALESCE(SUM(r.final),0),
		COALESCE(MAX(CASE WHEN r.sequence=e.sequence THEN r.final ELSE 0 END),0),
		COALESCE(MIN(r.sequence),0),COALESCE(MAX(r.sequence),0)
		FROM inbound_execution_epochs e LEFT JOIN inbound_execution_usage_receipts r ON r.epoch_id=e.epoch_id
		WHERE e.epoch_id=? GROUP BY e.state,e.sequence`, epoch).Scan(&got.state, &got.sequence, &got.receipts, &got.finals, &got.cursorFinal, &got.firstSequence, &got.lastSequence); err != nil {
		t.Fatal(err)
	}
	var ledgerUp, ledgerDown int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(upload),0),COALESCE(SUM(download),0)
		FROM inbound_execution_usage_deltas WHERE client_id=? AND role='customer'`, client).Scan(&ledgerUp, &ledgerDown); err != nil {
		t.Fatal(err)
	}
	if ledgerUp != got.uplink || ledgerDown != got.downlink {
		t.Fatal("diagnostic fixture account and committed deltas diverged")
	}
	return got
}

func TestBusinessPhaseDiagnosticsFullQueuePreservesReplayFinalAndReads(t *testing.T) {
	var disabled businessDiagnosticAccounting
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "full_queue"
		}
		t.Run(name, func(t *testing.T) {
			f := newExecutionFixture(t)
			if f.s.phaseDiagnostics != nil {
				t.Fatal("diagnostics were not disabled by default")
			}
			if enabled {
				defer fillBusinessDiagnosticQueue(t, f.s)()
			}
			ctx := context.Background()
			f.command(t, "phase-diagnostic-activate-0001", "activate")
			b := f.bundle(t)
			epoch := f.birth(t, b, "phase-diagnostic-epoch-0001")
			f.ready(t, b, epoch)
			report := executionReport(b, epoch, 2, 100, 200, false)
			receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, report)
			if err != nil || receipt.SHA256 != ix.Digest(report) || receipt.Replayed || receipt.Final {
				t.Fatal("ordinary report acceptance changed", receipt, err)
			}
			replay, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, report)
			if err != nil || !replay.Replayed || replay.SHA256 != receipt.SHA256 || replay.Sequence != 2 {
				t.Fatal("exact report replay changed", replay, err)
			}
			conflict := report
			conflict.Counters = append([]ix.Counter{}, report.Counters...)
			conflict.Counters[0].Upload++
			if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, conflict); !errors.Is(err, ErrConflict) {
				t.Fatal("conflicting replay escaped its existing fence", err)
			}
			final := executionReport(b, epoch, 3, 110, 220, true)
			accepted, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final)
			if err != nil || !accepted.Final || accepted.Replayed || accepted.SHA256 != ix.Digest(final) {
				t.Fatal("final report acceptance changed", accepted, err)
			}
			again, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final)
			if err != nil || !again.Final || !again.Replayed || again.SHA256 != accepted.SHA256 {
				t.Fatal("final replay changed", again, err)
			}
			want := businessDiagnosticAccounting{uplink: 110, downlink: 220, baseline: 1, holds: 0,
				sequence: 3, receipts: 3, finals: 1, cursorFinal: 1, firstSequence: 1, lastSequence: 3, state: "sealed"}
			got := readBusinessDiagnosticAccounting(t, f.s, f.client.ID, epoch.EpochID)
			if got != want {
				t.Fatalf("final accounting changed: got %+v want %+v", got, want)
			}
			f.command(t, "phase-diagnostic-stop-0001", "stop")
			stop := f.bundle(t)
			observation := executionObservation(stop, ix.EpochInput{})
			observation.State, observation.ProcessReady, observation.LeaseExpires = "stopped", false, 0
			if err := f.s.ObserveInboundExecution(ctx, f.key, observation); err != nil {
				t.Fatal("settled stopped observation changed", err)
			}
			state, err := f.s.InboundExecution(ctx, f.key, 501)
			if err != nil || state.DesiredState != "stopped" || state.ActiveRevision != 0 || len(state.EffectiveClientIDs) != 0 {
				t.Fatal("stopped runtime projection changed", state, err)
			}
			view, err := f.s.InboundExecutionSubscription(ctx, f.key, "owner", f.client.ID)
			if err != nil || view.ClientID != f.client.ID {
				t.Fatal("metadata read failed with diagnostic delivery unavailable", err)
			}
			if after := readBusinessDiagnosticAccounting(t, f.s, f.client.ID, epoch.EpochID); after != got {
				t.Fatal("observation/runtime/metadata work changed settled accounting")
			}
			if enabled {
				if got != disabled {
					t.Fatal("enabled diagnostics changed the disabled accounting result")
				}
			} else {
				disabled = got
			}
		})
	}
}

func TestBusinessPhaseDiagnosticsFullQueuePreservesUsageRollback(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "phase-diagnostic-rollback-activate-0001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "phase-diagnostic-rollback-epoch-0001")
	f.ready(t, b, epoch)
	defer fillBusinessDiagnosticQueue(t, f.s)()
	before := readBusinessDiagnosticAccounting(t, f.s, f.client.ID, epoch.EpochID)
	if _, err := f.s.db.Exec(`CREATE TRIGGER diagnostic_test_fail_delta BEFORE INSERT ON inbound_execution_usage_deltas
		WHEN NEW.sequence=2 BEGIN SELECT RAISE(ABORT,'synthetic diagnostic rollback canary'); END`); err != nil {
		t.Fatal(err)
	}
	report := executionReport(b, epoch, 2, 100, 200, false)
	if receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, report); err == nil || receipt.EpochID != "" {
		t.Fatal("aborted transaction returned an accepted receipt")
	}
	if after := readBusinessDiagnosticAccounting(t, f.s, f.client.ID, epoch.EpochID); after != before {
		t.Fatal("failed usage transaction changed receipt, cursor, totals or holds")
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER diagnostic_test_fail_delta`); err != nil {
		t.Fatal(err)
	}
	if receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, report); err != nil || receipt.Replayed || receipt.SHA256 != ix.Digest(report) {
		t.Fatal("unchanged report did not recover after removing the synthetic fault", err)
	}
	if receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, report); err != nil || !receipt.Replayed {
		t.Fatal("recovery replay changed", err)
	}
	want := businessDiagnosticAccounting{uplink: 100, downlink: 200, baseline: 1, holds: 0,
		sequence: 2, receipts: 2, finals: 0, cursorFinal: 0, firstSequence: 1, lastSequence: 2, state: "open"}
	if after := readBusinessDiagnosticAccounting(t, f.s, f.client.ID, epoch.EpochID); after != want {
		t.Fatalf("recovered accounting changed: got %+v want %+v", after, want)
	}
}

func TestBusinessPhaseDiagnosticsFullQueuePreservesAuthPoolCancellation(t *testing.T) {
	s, session := authAvailabilityStore(t)
	defer fillBusinessDiagnosticQueue(t, s)()
	if s.db.Stats().MaxOpenConnections != 1 {
		t.Fatal("fixture lost the real single-connection policy")
	}
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	beforeWaits := s.db.Stats().WaitCount
	calls := []struct {
		name string
		call func(context.Context) error
	}{
		{"verify", func(ctx context.Context) error { _, err := s.VerifySession(ctx, session.Token); return err }},
		{"login", func(ctx context.Context) error {
			_, err := s.Login(ctx, authAvailabilityUser, authAvailabilityPassword)
			return err
		}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			if err := call.call(ctx); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnauthenticated) {
				t.Fatal("diagnostics changed the waiting authentication error", err)
			}
		})
	}
	if s.db.Stats().WaitCount < beforeWaits+int64(len(calls)) {
		t.Fatal("fixture did not exercise actual pooled-connection waits")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	verified, err := s.VerifySession(context.Background(), session.Token)
	if err != nil || verified.User.ID != "owner" {
		t.Fatal("temporary contention lost the existing session", err)
	}
	if _, err := s.Login(context.Background(), authAvailabilityUser, authAvailabilityPassword); err != nil {
		t.Fatal("login did not recover after releasing the sole connection", err)
	}
	if err := s.Logout(context.Background(), session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.VerifySession(context.Background(), session.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("diagnostic path bypassed a genuine revocation", err)
	}
}
