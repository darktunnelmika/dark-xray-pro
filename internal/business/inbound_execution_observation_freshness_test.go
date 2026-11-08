package business

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func observationFreshnessFixture(t *testing.T) (*executionFixture, ix.Bundle, ix.EpochInput) {
	t.Helper()
	f := newExecutionFixture(t)
	t.Cleanup(func() { _ = f.s.Close() })
	f.command(t, "observe-freshness-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "observe-freshness-epoch-0001")
	f.ready(t, b, e)
	return f, b, e
}

func observationBytes(t *testing.T, f *executionFixture, node string) []byte {
	t.Helper()
	var raw []byte
	if err := f.s.db.QueryRow(`SELECT observation_enc FROM inbound_execution_nodes WHERE node_id=?`, node).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestManagedObservationFreshnessBypassesBusyWriterButCannotRenewOrAcceptUsage(t *testing.T) {
	f, b, e := observationFreshnessFixture(t)
	ctx := context.Background()
	before := observationBytes(t, f, b.NodeID)
	// Occupy the only writer connection. Freshness must use committed WAL
	// readers, while financial acceptance and execution authority still wait.
	conn, err := f.s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	o := executionObservation(b, e)
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = f.s.ObserveInboundExecution(bounded, f.key, o); err != nil {
		t.Fatal("unchanged observation waited for writer", err)
	}
	state, err := f.s.InboundExecution(bounded, f.key, 501)
	if err != nil || len(state.Targets) != 1 || state.Targets[0].ReportedAt != o.ObservedUnixNano {
		t.Fatal("fresh display missing", state, err)
	}
	for _, check := range []func(context.Context) error{
		func(ctx context.Context) error {
			return f.s.InboundExecutionAuthority(ctx, f.key, b.NodeID, b.InstallationID, b.Generation)
		},
		func(ctx context.Context) error {
			_, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 10, 20, false))
			return err
		},
	} {
		deadline, stop := context.WithTimeout(ctx, 30*time.Millisecond)
		err = check(deadline)
		stop()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("freshness bypassed durable authority", err)
		}
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, observationBytes(t, f, b.NodeID)) {
		t.Fatal("timestamp refresh rewrote durable state")
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Uplink != 0 || account.Downlink != 0 {
		t.Fatal("uncommitted usage appeared", account, err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 10, 20, false)); err != nil {
		t.Fatal(err)
	}
}

func TestManagedObservationFreshnessPersistsTransitionsAndRejectsReorderedReports(t *testing.T) {
	f, b, e := observationFreshnessFixture(t)
	ctx := context.Background()
	old := observationBytes(t, f, b.NodeID)
	o := executionObservation(b, e)
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	for _, delta := range []int64{-1, 0} {
		bad := o
		bad.ObservedUnixNano += delta
		bad.State, bad.ProcessReady = "failed", false
		if err := f.s.ObserveInboundExecution(ctx, f.key, bad); !errors.Is(err, ErrConflict) {
			t.Fatal("reordered transition accepted", err)
		}
	}
	o.ObservedUnixNano++
	o.State, o.ProcessReady, o.AccountingState, o.ErrorCode = "failed", false, "uncertain", "accounting_tail_uncertain"
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(old, observationBytes(t, f, b.NodeID)) {
		t.Fatal("uncertainty was only cached")
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Holds != 1 || account.Eligible {
		t.Fatal("uncertain tail lost its hold", account, err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.Targets[0].ProcessReady {
		t.Fatal("old freshness hid uncertainty", state, err)
	}
	final := executionReport(b, e, 2, 13, 21, true)
	r, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final)
	if err != nil || !r.Final {
		t.Fatal("final acceptance changed", r, err)
	}
	replay, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final)
	if err != nil || !replay.Replayed || replay.SHA256 != r.SHA256 {
		t.Fatal("final replay changed", replay, err)
	}
	account, err = f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Holds != 1 || account.Uplink != 13 || account.Downlink != 21 {
		t.Fatal("accounting changed", account, err)
	}
}

func TestManagedObservationFreshnessRejectsRevocationAndExpiredAccounting(t *testing.T) {
	for _, kind := range []string{"revoked", "stale_accounting", "expired_lease", "changed_identity"} {
		t.Run(kind, func(t *testing.T) {
			f, b, e := observationFreshnessFixture(t)
			ctx := context.Background()
			o := executionObservation(b, e)
			if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
				t.Fatal(err)
			}
			o.ObservedUnixNano++
			switch kind {
			case "revoked":
				_, err := f.s.SetClientState(ctx, SetClientState{EventID: "observation-revoke-client-0001", ClientID: f.client.ID, Action: "disable", Reason: "Verify freshness cannot grant authority"})
				if err != nil {
					t.Fatal(err)
				}
			case "stale_accounting":
				_, err := f.s.db.Exec(`UPDATE inbound_execution_usage_receipts SET created=? WHERE epoch_id=?`, time.Now().Add(-16*time.Second).UnixNano(), e.EpochID)
				if err != nil {
					t.Fatal(err)
				}
			case "expired_lease":
				o.LeaseExpires = time.Now().Add(-time.Second).UnixNano()
			case "changed_identity":
				o.EpochID = "unregistered-other-epoch"
			}
			if err := f.s.ObserveInboundExecution(ctx, f.key, o); !errors.Is(err, ErrConflict) {
				t.Fatal("invalid freshness accepted", err)
			}
		})
	}
}

func TestManagedObservationFreshnessRestartAndGenerationDoNotReuseProjection(t *testing.T) {
	f, b, e := observationFreshnessFixture(t)
	ctx := context.Background()
	before := observationBytes(t, f, b.NodeID)
	var durable ix.Observation
	if err := openPlanValue(f.key, executionAAD("observation", b.NodeID+"/"+b.InstallationID, b.Generation), before, &durable); err != nil {
		t.Fatal(err)
	}
	o := executionObservation(b, e)
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.Targets[0].ReportedAt != durable.ObservedUnixNano {
		t.Fatal("reopen invented freshness", state, err)
	}
	o = executionObservation(b, e)
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	f.command(t, "observation-freshness-stop-0001", "stop")
	state, err = f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.Targets[0].ProcessReady {
		t.Fatal("new generation reused old readiness", state, err)
	}
	o.ObservedUnixNano++
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); !errors.Is(err, ErrConflict) {
		t.Fatal("superseded observation accepted", err)
	}
}

func TestManagedObservationFreshnessUpdatesSubscriptionWithoutChangingEntitlement(t *testing.T) {
	f, b, e := observationFreshnessFixture(t)
	ctx := context.Background()
	before, err := f.s.InboundExecutionSubscription(ctx, f.key, "owner", f.client.ID)
	if err != nil {
		t.Fatal(err)
	}
	o := executionObservation(b, e)
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.InboundExecutionSubscription(ctx, f.key, "owner", f.client.ID)
	if err != nil || !view.Available || view.RouteCount != before.RouteCount {
		t.Fatal("subscription lost current readiness", view, err)
	}
	sub, err := f.s.ProjectInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, "uri")
	if err != nil || sub.UserInfo == nil {
		t.Fatal("subscription entitlement missing", err)
	}
}

func TestManagedObservationFreshnessStoppedHeartbeatAvoidsWrites(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	f.command(t, "observation-stopped-stop-0001", "stop")
	b := f.bundle(t)
	o := executionObservation(b, ix.EpochInput{})
	o.State, o.ProcessReady, o.HealthState, o.AccountingState = "stopped", false, "stopped", "settled"
	ctx := context.Background()
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	before := observationBytes(t, f, b.NodeID)
	for i := 0; i < 10; i++ {
		o.ObservedUnixNano++
		o.LeaseExpires++
		if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(before, observationBytes(t, f, b.NodeID)) {
		t.Fatal("stopped heartbeat rewrote business WAL")
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || state.Targets[0].ReportedAt != o.ObservedUnixNano {
		t.Fatal("stopped freshness lost", state, err)
	}
}

func TestManagedObservationFreshnessGateCancellationAndCapacity(t *testing.T) {
	var c executionObservationFreshness
	unlock, err := c.lock(context.Background(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if release, err := c.lock(ctx, "node-a"); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatal(err)
	}
	other, err := c.lock(context.Background(), "node-b")
	if err != nil {
		t.Fatal(err)
	}
	other()
	unlock()
	if len(c.gates) != 0 {
		t.Fatal("gate references leaked")
	}
	var wg sync.WaitGroup
	for i := 0; i < executionObservationFreshnessLimit+10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.put([]byte("base"), ix.Observation{NodeID: fmt.Sprint(i), ObservedUnixNano: int64(i + 1)})
		}(i)
	}
	wg.Wait()
	if len(c.values) != executionObservationFreshnessLimit {
		t.Fatal("freshness cache unbounded", len(c.values))
	}
}
