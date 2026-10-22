package business

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func healthEndpoint() mx.ProtocolHealthEndpoint {
	return mx.ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.com", CertificateSHA: strings.Repeat("a", 64)}
}

func healthApplied(t *testing.T, prepare ...func(*Store, []byte, mx.TransactionPoll, mx.TransactionPoll)) *handoverFixture {
	t.Helper()
	ctx := context.Background()
	setup := []func(*Store, []byte, mx.TransactionPoll, mx.TransactionPoll){func(s *Store, key []byte, source, target mx.TransactionPoll) {
		if err := s.EnableSystemProbeAccounting(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.EnableMeteredHealthChallenges(ctx); err != nil {
			t.Fatal(err)
		}
		for _, p := range []mx.TransactionPoll{source, target} {
			if _, err := s.ProvisionMeteredProbe(ctx, key, p.NodeID, p.InstallationID, 10); err != nil {
				t.Fatal(err)
			}
		}
	}}
	f := newHandoverFixture(t, append(setup, prepare...)...)
	f.create(t)
	d := f.drain(t)
	if err := f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, f.seal(t, d)); err != nil {
		t.Fatal(err)
	}
	meteredQueuePoll(t, f.s, f.key, &f.source, false)
	if _, err := f.s.QueueMeteredHandoverCommand(ctx, f.key, f.in.ID, meteredQueueRequest(f.target, 32, "apply", f.in.TargetStageID)); err != nil {
		t.Fatal(err)
	}
	d = meteredQueuePoll(t, f.s, f.key, &f.target, true)
	b, err := mx.SignBirthReport(string(routeTestToken), d.Command.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12346, StartTicks: 78})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := f.s.MeteredRouteAuthority(ctx, f.key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command, Birth: &b})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1}
	for _, b := range epoch.Bindings {
		zero.Users = append(zero.Users, uc.Usage{ClientID: b.ClientID})
	}
	zero.SHA = meteredHash(zero)
	if _, err = f.s.usage.Accept(ctx, usageledger.Principal{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	u := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("e", 64)}
	c := d.Command
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = f.s.AcceptMeteredRouteReceipt(ctx, f.key, routeTestToken, meteredQueueAck(c, o, "applied", u, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	f.target.Status.Serial, f.target.Status.CurrentStageID, f.target.Status.StagedID, f.target.Status.Observed, f.target.Status.Usage = c.Serial, c.StageID, "", o, &u
	meteredQueuePoll(t, f.s, f.key, &f.target, false)
	return f
}

// Synthetic signed node evidence for HUB transaction tests; actual networking
// is separately exercised by the pinned-core CI namespace fixture.
func healthProof(q mx.ProtocolHealthRequest) mx.ProtocolHealthReceipt {
	h := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	p := mx.ProtocolHealthReceipt{Version: mx.ProtocolHealthVersion, RequestSHA: meteredHash(q), ResponseSHA: hex.EncodeToString(h[:]), Completed: time.Now().UnixNano()}
	raw, _ := json.Marshal(p)
	m := hmac.New(sha256.New, routeTestToken)
	m.Write([]byte("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2\x00"))
	m.Write(raw)
	p.Signature = hex.EncodeToString(m.Sum(nil))
	return p
}

func issueHealth(t *testing.T, f *handoverFixture) mx.ProtocolHealthRequest {
	t.Helper()
	q, err := f.s.IssueMeteredHealthChallenge(context.Background(), f.key, routeTestToken, routeTestID(201), f.in.ID, 10, healthEndpoint())
	if err != nil {
		t.Fatal("issue", err)
	}
	return q
}

func TestMeteredHealthChallengeExactRetryAndConcurrentConsumption(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t)
	q := issueHealth(t, f)
	if q.ID == routeTestID(201) || q.NodeID != f.target.NodeID || q.ApplyCommandID != routeTestID(32) || q.Expires-q.Issued > int64(8*time.Second) || q.Expires > f.target.Status.Observed.LeaseExpires {
		t.Fatal("unbound challenge")
	}
	if got := issueHealth(t, f); got != q {
		t.Fatal("retry renewed/replaced nonce")
	}
	if _, err := f.s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, routeTestID(202), f.in.ID, 10, healthEndpoint()); err == nil {
		t.Fatal("parallel challenge")
	}
	e := healthEndpoint()
	e.CertificateSHA = strings.Repeat("b", 64)
	if _, err := f.s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, routeTestID(201), f.in.ID, 10, e); err == nil {
		t.Fatal("intent changed verifier")
	}
	p := healthProof(q)
	// Close/reopen before first use: issuance must already be durable.
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f.s = s
	s2, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var wg sync.WaitGroup
	results := make(chan MeteredHealthEvidence, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := s
			if i%2 == 1 {
				store = s2
			}
			v, e := store.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p)
			results <- v
			errs <- e
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent receipt", e)
		}
	}
	first := 0
	var accepted int64
	for v := range results {
		if !v.Replayed {
			first++
		}
		if accepted == 0 {
			accepted = v.Accepted
		}
		if v.Accepted != accepted || v.RequestSHA != meteredHash(q) {
			t.Fatal("retry altered durable evidence")
		}
	}
	if first != 1 {
		t.Fatal("consumed more than once", first)
	}
	bad := p
	bad.Signature = strings.Repeat("a", 64)
	if _, err = s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), bad); err == nil {
		t.Fatal("different retry proof")
	}
	// Exact retry after expiry is explicitly historical, not fresh authority.
	v, err := s.consumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p, func() time.Time { return time.Unix(0, q.Expires+1) })
	if err != nil || !v.Replayed || v.Accepted != accepted {
		t.Fatal("lost committed reply", err)
	}
	if _, err = s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, routeTestID(203), f.in.ID, 10, healthEndpoint()); err == nil {
		t.Fatal("second approval for operation")
	}
	view, err := s.MeteredHandover(ctx, f.in.ID)
	if err != nil || view.CanPublish || view.Phase != "target_applied_health_required" {
		t.Fatal("proof invented publication", err)
	}
	audit, err := s.MeteredHandoverAudit(ctx, f.key, f.in.ID)
	if err != nil || audit.HistoricalHealthEvidence == nil || audit.HistoricalHealthEvidence.Accepted != accepted || audit.CanPublish {
		t.Fatal("historical owner audit", err)
	}
	var n int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM pro_health_receipts`).Scan(&n); err != nil || n != 1 {
		t.Fatal("receipt not single use", n, err)
	}
}

func TestMeteredHealthChallengeRejectsUnissuedTamperedExpiredProof(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t)
	q := issueHealth(t, f)
	p := healthProof(q)
	if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, routeTestID(209), healthEndpoint(), p); err == nil {
		t.Fatal("unissued nonce")
	}
	for _, bad := range []mx.ProtocolHealthReceipt{{}, func() mx.ProtocolHealthReceipt { b := p; b.RequestSHA = strings.Repeat("f", 64); return b }(), func() mx.ProtocolHealthReceipt { b := p; b.Completed = q.Expires; return b }()} {
		if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), bad); err == nil {
			t.Fatal("invalid receipt")
		}
	}
	if _, err := f.s.consumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p, func() time.Time { return time.Unix(0, q.Expires) }); err == nil {
		t.Fatal("expired challenge")
	}
	wrong := sha256.Sum256([]byte("different node key"))
	if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, wrong[:], q.ID, healthEndpoint(), p); err == nil {
		t.Fatal("other node key")
	}
	e := healthEndpoint()
	e.ServerName = "changed.example.com"
	if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, e, p); err == nil {
		t.Fatal("changed approved verifier")
	}
	if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p); err != nil {
		t.Fatal("rejection consumed nonce", err)
	}
}

func TestMeteredHealthChallengeRechecksLiveState(t *testing.T) {
	for _, mode := range []string{"pending-drain", "stopped-target", "source-restarted", "target-instance", "stale-poll", "customer-disabled", "unknown-epoch"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := healthApplied(t)
			q := issueHealth(t, f)
			p := healthProof(q)
			switch mode {
			case "pending-drain":
				_, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.target.NodeID, meteredQueueRequest(f.target, 33, "drain", f.in.TargetStageID))
				if err != nil {
					t.Fatal(err)
				}
			case "stopped-target":
				f.target.Status.Observed.ProcessReady = false
				meteredQueuePoll(t, f.s, f.key, &f.target, false)
			case "source-restarted":
				f.source.Status.Observed.ProcessReady = true
				f.source.Status.Observed.ActiveSequence = f.source.Status.Observed.SavedSequence
				f.source.Status.Observed.ActiveSHA = f.source.Status.Observed.SavedSHA
				f.source.Status.Observed.LeaseExpires = time.Now().Add(time.Second).UnixNano()
				meteredQueuePoll(t, f.s, f.key, &f.source, false)
			case "target-instance":
				f.target.Status.Instance = routeTestID(211)
				meteredQueuePoll(t, f.s, f.key, &f.target, false)
			case "stale-poll":
				_, err := f.s.db.Exec(`UPDATE native_metered_route_channels SET observed_at=1 WHERE node_id=?`, f.target.NodeID)
				if err != nil {
					t.Fatal(err)
				}
			case "customer-disabled":
				_, err := f.s.db.Exec(`UPDATE clients SET deployment_state='disabled' WHERE id=?`, f.client.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "unknown-epoch":
				if err := f.s.usage.MarkEpochUncertain(ctx, usageledger.Principal{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID}, q.Usage.EpochID, "test missing tail"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p); err == nil {
				t.Fatal("stale state accepted")
			}
			var n int
			if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM pro_health_receipts`).Scan(&n); err != nil || n != 0 {
				t.Fatal("failed proof left receipt", n, err)
			}
		})
	}
}

func TestMeteredHealthChallengeSchemaAndAppendOnly(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t)
	q := issueHealth(t, f)
	p := healthProof(q)
	if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE pro_health_challenges SET expires=expires+1`, `DELETE FROM pro_health_challenges`, `UPDATE pro_health_receipts SET accepted=accepted+1`, `DELETE FROM pro_health_receipts`} {
		if _, err := f.s.db.Exec(query); err == nil {
			t.Fatal("mutable health audit", query)
		}
	}
	if err := f.s.EnableMeteredHealthChallenges(ctx); err != nil {
		t.Fatal("migration replay", err)
	}
	if err := f.s.EnableSystemProbeAccounting(ctx); err != nil {
		t.Fatal("probe migration downgraded health", err)
	}
	var version int
	if err := f.s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 10 {
		t.Fatal(version, err)
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER pro_health_receipts_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(f.path); err == nil {
		s.Close()
		t.Fatal("damaged challenge policy silently repaired")
	}
}

func TestMeteredHealthChallengeInterruptedReceiptRemainsRetryable(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t)
	q := issueHealth(t, f)
	p := healthProof(q)
	interrupted, cancel := context.WithCancel(ctx)
	if _, err := f.s.consumeMeteredHealthProof(interrupted, f.key, routeTestToken, q.ID, healthEndpoint(), p, func() time.Time { cancel(); return time.Now() }); err == nil {
		t.Fatal("cancelled transaction committed")
	}
	// Crossing the expiration during checks must also roll back consumption.
	calls := 0
	if _, err := f.s.consumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p, func() time.Time {
		calls++
		if calls > 1 {
			return time.Unix(0, q.Expires)
		}
		return time.Now()
	}); err == nil {
		t.Fatal("expired while checking")
	}
	v, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p)
	if err != nil || v.Replayed {
		t.Fatal("uncommitted reply consumed nonce", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), p)
	if err != nil || !again.Replayed || again.Accepted != v.Accepted {
		t.Fatal("committed lost reply not durable", err)
	}
}
