package business

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func healthRoundApplied(t *testing.T, transport string, secondProbe bool) *handoverFixture {
	t.Helper()
	ctx := context.Background()
	return healthApplied(t, func(s *Store, key []byte, source, target mx.TransactionPoll) {
		in := nativeInput()
		in.ID, in.Transport = 11, transport
		if transport == "grpc" {
			in.GRPCService = "round-health"
		}
		if _, err := s.CreateInbound(ctx, key, in); err != nil {
			t.Fatal(err)
		}
		if err := s.BindInboundClients(ctx, 11, []string{"metered-native"}, true); err != nil {
			t.Fatal(err)
		}
		for i, p := range []mx.TransactionPoll{source, target} {
			port := uint16(19591 + i)
			if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 11, NodeID: p.NodeID, InstallationID: p.InstallationID, ListenIP: "127.0.0.1", DirectPort: port}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveNativeRouteDraft(ctx, key, NativeRouteDraftInput{EventID: fmt.Sprintf("health-round-draft-extra-%d", i), InboundID: 11, NodeID: p.NodeID, InstallationID: p.InstallationID, ExpectedInboundRevision: 1, DirectPort: port, Reason: "Exercise complete multi-inbound coverage"}); err != nil {
				t.Fatal(err)
			}
			if secondProbe {
				if _, err := s.ProvisionMeteredProbe(ctx, key, p.NodeID, p.InstallationID, 11); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := s.EnableMeteredHealthQueue(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.EnableMeteredHealthRounds(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func queuedHealthRound(t *testing.T, f *handoverFixture, n int) MeteredHealthRound {
	t.Helper()
	r, err := f.s.QueueMeteredHealthRound(context.Background(), f.key, routeTestID(n), f.in.ID, healthEndpoint())
	if err != nil {
		t.Fatal("queue round", err)
	}
	return r
}

func roundAudit(t *testing.T, f *handoverFixture) MeteredHandoverAudit {
	t.Helper()
	a, err := f.s.MeteredHandoverAudit(context.Background(), f.key, f.in.ID)
	if err != nil {
		t.Fatal("audit round", err)
	}
	return a
}

func roundPoll(t *testing.T, f *handoverFixture) mx.ProtocolHealthRequest {
	t.Helper()
	q, found, err := f.s.PollMeteredHealth(context.Background(), f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint())
	if err != nil || !found {
		t.Fatal("poll round member", found, err)
	}
	return q
}

func TestMeteredHealthRoundCompleteSetConcurrentRetryAndAccounting(t *testing.T) {
	ctx := context.Background()
	f := healthRoundApplied(t, "tcp", true)
	before, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generation int64
	if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	r := queuedHealthRound(t, f, 301)
	if len(r.Members) != 2 || r.Members[0].InboundID != 10 || r.Members[1].InboundID != 11 || r.State != "pending" || r.Current || r.Expires > f.target.Status.Observed.LeaseExpires || r.Expires-r.Created > int64(8*time.Second) {
		t.Fatal("incomplete/unbounded required set", r)
	}
	s2, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := f.s
			if i%2 == 1 {
				s = s2
			}
			again, e := s.QueueMeteredHealthRound(ctx, f.key, r.ID, f.in.ID, healthEndpoint())
			if e != nil || meteredHash(again) != meteredHash(r) {
				t.Error("lost owner reply allocated new work", e)
			}
		}(i)
	}
	wg.Wait()
	if _, err = f.s.QueueMeteredHealthRound(ctx, f.key, routeTestID(302), f.in.ID, healthEndpoint()); err == nil {
		t.Fatal("overlapping active round")
	}
	if _, err = f.s.QueueMeteredHealth(ctx, f.key, routeTestID(303), f.in.ID, 10, healthEndpoint()); err == nil {
		t.Fatal("legacy subset bypassed round")
	}
	q := roundPoll(t, f)
	if q.InboundID != 10 || q.Expires != r.Expires || q.ID == r.Members[0].IntentID || q.ID == r.ID {
		t.Fatal("unbound first member")
	}
	if again := roundPoll(t, f); again != q {
		t.Fatal("delivery retry replaced nonce")
	}
	proof := healthProof(q)
	results := make(chan MeteredHealthEvidence, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := f.s
			if i%2 == 1 {
				s = s2
			}
			v, e := s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), proof)
			if e != nil {
				t.Error("concurrent proof", e)
			}
			results <- v
		}(i)
	}
	wg.Wait()
	close(results)
	first, accepted := 0, int64(0)
	for v := range results {
		if !v.Replayed {
			first++
		}
		if accepted == 0 {
			accepted = v.Accepted
		}
		if v.Accepted != accepted {
			t.Fatal("retry changed acceptance time")
		}
	}
	if first != 1 {
		t.Fatal("proof committed multiple times", first)
	}
	a := roundAudit(t, f)
	if len(a.HealthRounds) != 1 || a.HealthRounds[0].Current || a.HealthRounds[0].State != "pending" || a.HealthRounds[0].Members[1].State != "pending" {
		t.Fatal("one inbound became complete coverage")
	}
	q2 := roundPoll(t, f)
	if q2.InboundID != 11 || q2.ID == q.ID || q2.SystemProbeID == q.SystemProbeID || q2.Expires != r.Expires {
		t.Fatal("second scoped member missing")
	}
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q2.ID, healthEndpoint(), proof); err == nil {
		t.Fatal("one inbound proof covered another")
	}
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q2.ID, healthEndpoint(), healthProof(q2)); err != nil {
		t.Fatal(err)
	}
	if _, found, e := f.s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint()); e != nil || found {
		t.Fatal("accepted members delivered again", e)
	}
	a = roundAudit(t, f)
	if a.HealthRounds[0].State != "complete" || !a.HealthRounds[0].Current || a.CanPublish || a.Phase != "target_applied_health_required" || a.HistoricalHealthEvidence != nil {
		t.Fatal("invalid complete-round authority", a.HealthRounds[0].State)
	}
	after, err := f.s.ClientUsage(ctx, f.client.ID)
	var afterGeneration int64
	if err != nil || meteredHash(before) != meteredHash(after) {
		t.Fatal("health altered customer accounting", err)
	}
	if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&afterGeneration); err != nil || generation != afterGeneration {
		t.Fatal("health altered runtime generation", err)
	}
	var reservations int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM native_metered_handover_nodes WHERE handover_id=?`, f.in.ID).Scan(&reservations); err != nil || reservations != 2 {
		t.Fatal("health released lifecycle reservation", reservations, err)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), f.client.UUID) || strings.Contains(string(raw), "privateKey") {
		t.Fatal("audit exposed private configuration")
	}
}

func signedHealthProofAt(q mx.ProtocolHealthRequest, at time.Time) mx.ProtocolHealthReceipt {
	p := healthProof(q)
	p.Completed, p.Signature = at.UnixNano(), ""
	raw, _ := json.Marshal(p)
	m := hmac.New(sha256.New, routeTestToken)
	m.Write([]byte("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2\x00"))
	m.Write(raw)
	p.Signature = hex.EncodeToString(m.Sum(nil))
	return p
}

func TestMeteredHealthRoundLaterRoundNeverCombinesPriorCoverage(t *testing.T) {
	ctx := context.Background()
	f := healthRoundApplied(t, "tcp", true)
	// Explicit internal clocks exercise expiry without weakening signed wire
	// timestamps or spending eight real seconds on every repeated CI iteration.
	at := time.Now().Add(-10 * time.Second)
	clock := func() time.Time { return at }
	r1, err := f.s.queueMeteredHealthRound(ctx, f.key, routeTestID(310), f.in.ID, healthEndpoint(), clock)
	if err != nil {
		t.Fatal(err)
	}
	at = at.Add(time.Millisecond)
	q1, err := f.s.issueMeteredHealthChallenge(ctx, f.key, routeTestToken, r1.Members[0].IntentID, f.in.ID, 10, healthEndpoint(), clock)
	if err != nil {
		t.Fatal(err)
	}
	p1 := signedHealthProofAt(q1, at)
	v1, err := f.s.consumeMeteredHealthProof(ctx, f.key, routeTestToken, q1.ID, healthEndpoint(), p1, clock)
	if err != nil {
		t.Fatal(err)
	}
	r2 := queuedHealthRound(t, f, 311)
	if r1.Members[0].IntentID == r2.Members[0].IntentID || r2.Members[0].State != "pending" {
		t.Fatal("later round inherited old coverage")
	}
	if old, e := f.s.QueueMeteredHealthRound(ctx, f.key, r1.ID, f.in.ID, healthEndpoint()); e != nil || old.Expires != r1.Expires || old.State != "expired" || old.Current {
		t.Fatal("old owner retry renewed health", e)
	}
	if replay, e := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q1.ID, healthEndpoint(), p1); e != nil || !replay.Replayed || replay.Accepted != v1.Accepted {
		t.Fatal("lost historical receipt not recoverable", e)
	}
	q2, err := f.s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, r2.Members[1].IntentID, f.in.ID, 11, healthEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q2.ID, healthEndpoint(), healthProof(q2)); err != nil {
		t.Fatal(err)
	}
	a := roundAudit(t, f)
	if len(a.HealthRounds) != 2 || a.HealthRounds[0].State != "pending" || a.HealthRounds[0].Current || a.HealthRounds[1].State != "expired" || a.HealthRounds[1].Current {
		t.Fatal("cross-round union became complete")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f.s = s
	q3 := roundPoll(t, f)
	if q3.InboundID != 10 || q3.ID == q1.ID {
		t.Fatal("restart reused old member nonce")
	}
	if _, err = s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q3.ID, healthEndpoint(), p1); err == nil {
		t.Fatal("prior round proof consumed new nonce")
	}
	if _, err = s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q3.ID, healthEndpoint(), healthProof(q3)); err != nil {
		t.Fatal(err)
	}
	if a = roundAudit(t, f); !a.HealthRounds[0].Current || a.HealthRounds[0].State != "complete" {
		t.Fatal("new full coverage not recognized")
	}
}

func TestMeteredHealthRoundRejectsUnsupportedOrMissingProbeAtomically(t *testing.T) {
	for _, tc := range []struct {
		name, transport string
		probe           bool
	}{{"missing-scoped-probe", "tcp", false}, {"unsupported-grpc", "grpc", false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := healthRoundApplied(t, tc.transport, tc.probe)
			if _, err := f.s.QueueMeteredHealthRound(context.Background(), f.key, routeTestID(320), f.in.ID, healthEndpoint()); err == nil {
				t.Fatal("silently skipped unsupported required inbound")
			}
			for _, table := range []string{"pro_health_rounds", "pro_health_round_members", "pro_health_intents", "pro_health_challenges"} {
				var n int
				if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
					t.Fatal("failed round left partial work", table, n, err)
				}
			}
		})
	}
}

func TestMeteredHealthRoundFreshnessRechecksAllRuntimeState(t *testing.T) {
	for _, mode := range []string{"target-stopped", "source-restarted", "target-instance", "client-disabled", "pending-drain", "expired"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := healthRoundApplied(t, "tcp", true)
			r := queuedHealthRound(t, f, 330)
			q := roundPoll(t, f)
			p := healthProof(q)
			switch mode {
			case "target-stopped":
				f.target.Status.Observed.ProcessReady = false
				meteredQueuePoll(t, f.s, f.key, &f.target, false)
			case "source-restarted":
				f.source.Status.Observed.ProcessReady = true
				f.source.Status.Observed.ActiveSequence = f.source.Status.Observed.SavedSequence
				f.source.Status.Observed.ActiveSHA = f.source.Status.Observed.SavedSHA
				f.source.Status.Observed.LeaseExpires = time.Now().Add(time.Second).UnixNano()
				meteredQueuePoll(t, f.s, f.key, &f.source, false)
			case "target-instance":
				f.target.Status.Instance = routeTestID(331)
				meteredQueuePoll(t, f.s, f.key, &f.target, false)
			case "client-disabled":
				if _, err := f.s.db.Exec(`UPDATE clients SET deployment_state='disabled' WHERE id=?`, f.client.ID); err != nil {
					t.Fatal(err)
				}
			case "pending-drain":
				if _, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.target.NodeID, meteredQueueRequest(f.target, 332, "drain", f.in.TargetStageID)); err != nil {
					t.Fatal(err)
				}
			}
			clock := time.Now
			if mode == "expired" {
				clock = func() time.Time { return time.Unix(0, r.Expires) }
			}
			if _, err := f.s.consumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p, clock); err == nil {
				t.Fatal("changed/expired round accepted proof")
			}
			tx, err := f.s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			row, err := readHealthRound(ctx, tx, r.ID)
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			v := f.s.assessHealthRound(ctx, tx, f.key, row, clock().UnixNano())
			tx.Rollback()
			want := "state_changed"
			if mode == "expired" {
				want = "expired"
			}
			if v.Current || v.State != want {
				t.Fatal("audit presented stale approval", v.State)
			}
		})
	}
}

func TestMeteredHealthRoundDeadlineCheckedBeforeCommit(t *testing.T) {
	ctx := context.Background()
	f := healthRoundApplied(t, "tcp", true)
	start := time.Now()
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return start
		}
		return start.Add(8 * time.Second)
	}
	if _, err := f.s.queueMeteredHealthRound(ctx, f.key, routeTestID(340), f.in.ID, healthEndpoint(), clock); err == nil {
		t.Fatal("round committed after its deadline")
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM pro_health_rounds`).Scan(&n); err != nil || n != 0 {
		t.Fatal("failed round not rolled back", n, err)
	}
	r := queuedHealthRound(t, f, 340)
	q := roundPoll(t, f)
	p := healthProof(q)
	calls = 0
	clock = func() time.Time {
		calls++
		if calls == 1 {
			return time.Now()
		}
		return time.Unix(0, r.Expires)
	}
	if _, err := f.s.consumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p, clock); err == nil {
		t.Fatal("receipt committed after round deadline")
	}
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM pro_health_round_receipts`).Scan(&n); err != nil || n != 0 {
		t.Fatal("late receipt not rolled back", n, err)
	}
}
