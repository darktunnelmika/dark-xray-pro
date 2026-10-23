package business

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func consumerFixture(t *testing.T) (*handoverFixture, []mx.ConsumerEndpointApproval, SubscriptionSecret) {
	t.Helper()
	ctx := context.Background()
	f := healthRoundApplied(t, "tcp", true)
	if _, err := f.s.BootstrapOwner(ctx, "consumer-owner", "synthetic-consumer-password"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableLabSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	approvals := []mx.ConsumerEndpointApproval{
		{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID, InboundID: 10, Address: "11.0.0.2", Port: 24010},
		{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID, InboundID: 11, Address: "11.0.0.2", Port: 24011},
		{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID, InboundID: 10, Address: "11.0.0.3", Port: 25010},
		{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID, InboundID: 11, Address: "11.0.0.3", Port: 25011},
	}
	if err := f.s.EnableConsumerManifests(ctx, approvals); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: routeTestID(1200), Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secret, err := f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return f, approvals, secret
}

func consumerInput(event int, revision uint64, alternate bool) ConsumerManifestMutation {
	address, port := "11.0.0.2", uint16(24010)
	if alternate {
		address, port = "11.0.0.3", 25010
	}
	return ConsumerManifestMutation{EventID: routeTestID(event), Action: "replace", ExpectedRevision: revision, Members: []ConsumerManifestMember{
		{InboundID: 10, ConsumerAddress: address, ConsumerPort: port},
		{InboundID: 11, ConsumerAddress: address, ConsumerPort: port + 1},
	}}
}

func saveConsumerManifest(t *testing.T, f *handoverFixture, in ConsumerManifestMutation) ConsumerManifest {
	t.Helper()
	m, err := f.s.MutateMeteredConsumerManifest(context.Background(), f.key, "owner", f.in.ID, in)
	if err != nil {
		t.Fatal("consumer manifest", err)
	}
	return m
}

func queueConsumerRound(t *testing.T, f *handoverFixture, m ConsumerManifest, event int) MeteredHealthRound {
	t.Helper()
	r, err := f.s.QueueMeteredConsumerHealthRound(context.Background(), f.key, routeTestID(event), f.in.ID, m.ID, m.Revision, healthEndpoint())
	if err != nil {
		t.Fatal("consumer round", err)
	}
	return r
}

// Synthetic signed evidence tests HUB transaction authority; namespace CI
// separately proves actual traffic through the exact URI ingress address.
func consumerHealthProof(q mx.ProtocolHealthRequest) mx.ProtocolHealthReceipt {
	h := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	p := mx.ProtocolHealthReceipt{Version: q.Version, RequestSHA: meteredHash(q), ResponseSHA: hex.EncodeToString(h[:]), Completed: time.Now().UnixNano()}
	raw, _ := json.Marshal(p)
	m := hmac.New(sha256.New, routeTestToken)
	m.Write([]byte(fmt.Sprintf("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v%d\x00", q.Version)))
	m.Write(raw)
	p.Signature = hex.EncodeToString(m.Sum(nil))
	return p
}

func completeConsumerRound(t *testing.T, f *handoverFixture, r MeteredHealthRound) {
	t.Helper()
	for range r.Members {
		q := roundPoll(t, f)
		if q.Version != mx.ConsumerProtocolHealthVersion || q.ConsumerManifestSHA != r.ConsumerManifestSHA || q.ConsumerAddress == "" || q.ConsumerPort < 1024 {
			t.Fatal("origin challenge substituted for consumer path", q)
		}
		if _, err := f.s.ConsumeMeteredHealthProof(context.Background(), f.key, routeTestToken, q.ID, healthEndpoint(), consumerHealthProof(q)); err != nil {
			t.Fatal(err)
		}
	}
}

func materializeConsumer(t *testing.T, f *handoverFixture, r MeteredHealthRound, event int) LabSubscriptionSnapshot {
	t.Helper()
	v, err := f.s.MaterializeLabSubscription(context.Background(), f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(event), ClientID: f.client.ID, RoundID: r.ID, ConsumerManifestID: r.ConsumerManifestID, ConsumerManifestRevision: r.ConsumerManifestRevision}, healthEndpoint())
	if err != nil {
		t.Fatal("consumer snapshot", err)
	}
	return v
}

func TestConsumerManifestExactCompletePathAndBoundedSubscription(t *testing.T) {
	ctx := context.Background()
	f, _, secret := consumerFixture(t)
	before, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generation uint64
	if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.MeteredConsumerManifest(ctx, f.key, "owner", f.in.ID)
	if err != nil || view.State != "absent" || view.Revision != 0 || len(view.RequiredInbounds) != 2 || len(view.ApprovedEndpoints) != 4 {
		t.Fatal("initial manifest metadata", view, err)
	}
	in := consumerInput(1201, 0, false)
	m := saveConsumerManifest(t, f, in)
	if m.ID == m.EventID || m.EventID != in.EventID || m.Revision != 1 || !m.Current || m.State != "active" || len(m.Members) != 2 || m.Members[0].OriginPort < 1024 || m.Members[0].OriginPort == m.Members[0].ConsumerPort {
		t.Fatal("manifest identity or origin/ingress binding", m)
	}
	if _, err = f.s.QueueMeteredHealthRound(ctx, f.key, routeTestID(1202), f.in.ID, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy round selected consumer path", err)
	}
	if _, err = f.s.QueueMeteredHealth(ctx, f.key, routeTestID(1203), f.in.ID, 10, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy member bypass", err)
	}
	if _, err = f.s.QueueMeteredConsumerHealthRound(ctx, f.key, routeTestID(1204), f.in.ID, m.ID, m.Revision+1, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("stale explicit manifest ref", err)
	}
	r := queueConsumerRound(t, f, m, 1205)
	if r.ConsumerManifestID != m.ID || r.ConsumerManifestRevision != m.Revision || r.ConsumerManifestSHA != m.ConsumerManifestSHA || r.Expires-r.Created > int64(8*time.Second) || r.Members[0].Port != m.Members[0].OriginPort || r.Members[0].ConsumerPort != m.Members[0].ConsumerPort {
		t.Fatal("round consumer scope", r)
	}
	q := roundPoll(t, f)
	if q.Version != mx.ConsumerProtocolHealthVersion || q.Port != m.Members[0].OriginPort || q.ConsumerPort != m.Members[0].ConsumerPort || q.Expires != r.Expires {
		t.Fatal("signed path binding", q)
	}
	bad := consumerHealthProof(q)
	bad.Version = mx.ProtocolHealthVersion
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), bad); err == nil {
		t.Fatal("v2 proof covered consumer path")
	}
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), consumerHealthProof(q)); err != nil {
		t.Fatal(err)
	}
	input := LabSubscriptionMaterialization{EventID: routeTestID(1206), ClientID: f.client.ID, RoundID: r.ID, ConsumerManifestID: m.ID, ConsumerManifestRevision: m.Revision}
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, input, healthEndpoint()); err == nil {
		t.Fatal("partial consumer set published")
	}
	q = roundPoll(t, f)
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), consumerHealthProof(q)); err != nil {
		t.Fatal(err)
	}
	missing := input
	missing.ConsumerManifestID = ""
	missing.ConsumerManifestRevision = 0
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, missing, healthEndpoint()); err == nil {
		t.Fatal("implicit manifest snapshot")
	}
	snapshot, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, input, healthEndpoint())
	if err != nil || !snapshot.Current || snapshot.ConsumerManifestID != m.ID || snapshot.ConsumerManifestSHA != m.ConsumerManifestSHA || snapshot.ExpiresUnixNano != r.Expires {
		t.Fatal("consumer snapshot", snapshot, err)
	}
	content, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || bytes.Count(content.Raw, []byte("@11.0.0.2:")) != 2 || !bytes.Contains(content.Raw, []byte(":24010?")) || !bytes.Contains(content.Raw, []byte(":24011?")) || bytes.Contains(content.Raw, []byte("127.0.0.1")) {
		t.Fatal("output bypassed ingress", err)
	}
	again, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, input, healthEndpoint())
	if err != nil || !again.Replayed || again.ID != snapshot.ID || again.ExpiresUnixNano != snapshot.ExpiresUnixNano {
		t.Fatal("retry changed original snapshot", err)
	}
	r2, err := f.s.QueueMeteredConsumerHealthRound(ctx, f.key, r.ID, f.in.ID, m.ID, m.Revision, healthEndpoint())
	if err != nil || r2.Expires != r.Expires || !r2.Current {
		t.Fatal("round retry renewed proof", err)
	}
	after, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || meteredHash(before) != meteredHash(after) {
		t.Fatal("manifest changed accounting", err)
	}
	var afterGeneration uint64
	var reservations int
	if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&afterGeneration); err != nil || generation != afterGeneration {
		t.Fatal("manifest changed runtime generation", err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM native_metered_handover_nodes WHERE handover_id=?`, f.in.ID).Scan(&reservations); err != nil || reservations != 2 {
		t.Fatal("manifest released handover reservations", err)
	}
	if a := roundAudit(t, f); a.CanPublish || a.Phase != "target_applied_health_required" || len(a.HealthRounds) != 1 || !a.HealthRounds[0].Current {
		t.Fatal("consumer proof changed publication authority", a)
	}
}

func TestConsumerManifestInvalidatesLegacyAndEveryPriorRevision(t *testing.T) {
	ctx := context.Background()
	f, r, secret := labSubscriptionFixture(t, true)
	legacy := materializeSubscription(t, f, r)
	approvals := []mx.ConsumerEndpointApproval{{NodeID: f.target.NodeID, InstallationID: f.target.InstallationID, InboundID: 10, Address: "11.0.0.2", Port: 24010}, {NodeID: f.target.NodeID, InstallationID: f.target.InstallationID, InboundID: 11, Address: "11.0.0.2", Port: 24011}}
	if err := f.s.EnableConsumerManifests(ctx, approvals); err != nil {
		t.Fatal(err)
	}
	// Schema 14 alone must preserve the exact legacy ciphertext/AAD contract.
	upgraded, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || upgraded.Snapshot.ID != legacy.ID || !bytes.Contains(upgraded.Raw, []byte("@127.0.0.1:")) {
		t.Fatal("schema upgrade broke legacy snapshot authentication", err)
	}
	legacyRetry, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(903), ClientID: f.client.ID, RoundID: r.ID}, healthEndpoint())
	if err != nil || !legacyRetry.Replayed || !legacyRetry.Current || legacyRetry.ExpiresUnixNano != legacy.ExpiresUnixNano {
		t.Fatal("schema upgrade broke legacy exact retry", err)
	}
	collision := consumerInput(903, 0, false)
	if _, err = f.s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, collision); !errors.Is(err, ErrConflict) {
		t.Fatal("snapshot event reused for manifest", err)
	}
	m := saveConsumerManifest(t, f, consumerInput(1210, 0, false))
	if _, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("old local snapshot survived manifest opt-in")
	}
	v, err := f.s.LabSubscription(ctx, f.key, "owner", f.client.ID, healthEndpoint())
	if err != nil || v.Snapshot == nil || v.Snapshot.ID != legacy.ID || v.Snapshot.Current {
		t.Fatal("legacy snapshot history lost or resurrected", err)
	}
	again, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(903), ClientID: f.client.ID, RoundID: r.ID}, healthEndpoint())
	if err != nil || !again.Replayed || again.Current {
		t.Fatal("legacy snapshot retry became current", err)
	}
	if _, err = f.s.QueueMeteredHealthRound(ctx, f.key, r.ID, f.in.ID, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy queue retained authority after manifest", err)
	}
	if a := roundAudit(t, f); a.HealthRounds[0].Current || a.HealthRounds[0].State != "state_changed" {
		t.Fatal("legacy origin round still current", a.HealthRounds)
	}
	revoked := saveConsumerManifest(t, f, ConsumerManifestMutation{EventID: routeTestID(1211), Action: "revoke", ExpectedRevision: m.Revision})
	if revoked.Current || revoked.State != "revoked" || revoked.Revision != 2 {
		t.Fatal("revocation authority", revoked)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("revocation fell back to local snapshot")
	}
	old, err := f.s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, consumerInput(1210, 0, false))
	if err != nil || !old.Replayed || old.Current || old.ID != m.ID {
		t.Fatal("historical manifest retry resurrected", err)
	}
}

func TestConsumerManifestReplacementRejectsInflightReceiptAndSnapshot(t *testing.T) {
	ctx := context.Background()
	f, _, secret := consumerFixture(t)
	m := saveConsumerManifest(t, f, consumerInput(1220, 0, false))
	r := queueConsumerRound(t, f, m, 1221)
	completeConsumerRound(t, f, r)
	materializeConsumer(t, f, r, 1222)
	next := saveConsumerManifest(t, f, consumerInput(1223, 1, true))
	if next.Revision != 2 || next.ConsumerManifestSHA == m.ConsumerManifestSHA {
		t.Fatal("manifest replacement binding", next)
	}
	if _, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("old consumer URI survived replacement")
	}
	replay, err := f.s.QueueMeteredConsumerHealthRound(ctx, f.key, r.ID, f.in.ID, m.ID, m.Revision, healthEndpoint())
	if err != nil || replay.Current || replay.State != "state_changed" || replay.Expires != r.Expires {
		t.Fatal("round replay replaced authority", replay, err)
	}
	if _, err = f.s.QueueMeteredConsumerHealthRound(ctx, f.key, r.ID, f.in.ID, next.ID, next.Revision, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("round ID rebound to newer manifest", err)
	}
	t.Run("inflight", func(t *testing.T) {
		f, _, _ := consumerFixture(t)
		m := saveConsumerManifest(t, f, consumerInput(1230, 0, false))
		r := queueConsumerRound(t, f, m, 1231)
		q := roundPoll(t, f)
		again, err := f.s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, r.Members[0].IntentID, f.in.ID, q.InboundID, healthEndpoint())
		if err != nil || again != q {
			t.Fatal("live v3 retry changed original signed bytes", err)
		}
		withoutPolicy, err := Open(f.path)
		if err != nil {
			t.Fatal(err)
		}
		defer withoutPolicy.Close()
		if _, err = withoutPolicy.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, r.Members[0].IntentID, f.in.ID, q.InboundID, healthEndpoint()); !errors.Is(err, ErrConflict) {
			t.Fatal("unexpired v3 challenge replay bypassed startup policy", err)
		}
		saveConsumerManifest(t, f, consumerInput(1232, 1, true))
		if _, err = f.s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, r.Members[0].IntentID, f.in.ID, q.InboundID, healthEndpoint()); !errors.Is(err, ErrConflict) {
			t.Fatal("unexpired v3 challenge replay bypassed manifest replacement", err)
		}
		expiredRetry, err := f.s.issueMeteredHealthChallenge(ctx, f.key, routeTestToken, r.Members[0].IntentID, f.in.ID, q.InboundID, healthEndpoint(), func() time.Time { return time.Unix(0, q.Expires+1) })
		if err != nil || expiredRetry != q {
			t.Fatal("expired historical v3 retry changed bytes or deadline", err)
		}
		if _, err := f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), consumerHealthProof(q)); err == nil {
			t.Fatal("stale consumer receipt committed")
		}
		if _, _, err := f.s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint()); err == nil {
			t.Fatal("stale consumer queue polled")
		}
		raw, err := f.s.QueueMeteredConsumerHealthRound(ctx, f.key, r.ID, f.in.ID, m.ID, m.Revision, healthEndpoint())
		if err != nil || raw.Current || raw.Expires != r.Expires {
			t.Fatal("pending retry renewed deadline", err)
		}
	})
}

func TestConsumerManifestPolicyRestartCASAndCurrentReauthorization(t *testing.T) {
	ctx := context.Background()
	f, approvals, secret := consumerFixture(t)
	// The caller cannot mutate the Store's startup approvals after enabling.
	approvals[0].Address = "11.0.0.99"
	m := saveConsumerManifest(t, f, consumerInput(1240, 0, false))
	r := queueConsumerRound(t, f, m, 1241)
	completeConsumerRound(t, f, r)
	materializeConsumer(t, f, r, 1242)
	s2, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err = s2.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("reopen without feature approved retained consumer path")
	}
	a, err := s2.MeteredHandoverAudit(ctx, f.key, f.in.ID)
	if err != nil || a.HealthRounds[0].Current {
		t.Fatal("audit omitted current startup policy", err)
	}
	approvals[0].Address = "11.0.0.2"
	if err = s2.EnableConsumerManifests(ctx, approvals); err != nil {
		t.Fatal(err)
	}
	if _, err = s2.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err != nil {
		t.Fatal("reopen lost exact approved path", err)
	}
	if err = s2.EnableConsumerManifests(ctx, approvals[2:]); !errors.Is(err, ErrConflict) {
		t.Fatal("running policy changed", err)
	}
	s3, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if err = s3.EnableConsumerManifests(ctx, approvals[2:]); err != nil {
		t.Fatal(err)
	}
	if _, err = s3.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("withdrawn endpoint still served")
	}
	v, err := s3.MeteredConsumerManifest(ctx, f.key, "owner", f.in.ID)
	if err != nil || v.Manifest == nil || v.Manifest.Current {
		t.Fatal("metadata ignored withdrawn approval", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, store := range []*Store{f.s, s2} {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			_, e := s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, consumerInput(1243+i, 1, true))
			results <- e
		}(i, store)
	}
	wg.Wait()
	close(results)
	committed := 0
	for err := range results {
		if err == nil {
			committed++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if committed != 1 {
		t.Fatal("CAS across Store handles", committed)
	}
	if _, err = f.s.db.Exec(`UPDATE auth_users SET disabled=1 WHERE id='owner'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, consumerInput(1240, 0, false)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("replay bypassed current owner status", err)
	}
}

func TestConsumerManifestRejectsIncompleteUnapprovedAndCrossInboundRoutes(t *testing.T) {
	ctx := context.Background()
	f, _, _ := consumerFixture(t)
	if _, err := f.s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, consumerInput(1200, 0, false)); !errors.Is(err, ErrConflict) {
		t.Fatal("token event reused for manifest", err)
	}
	for name, change := range map[string]func(*ConsumerManifestMutation){
		"partial": func(v *ConsumerManifestMutation) { v.Members = v.Members[:1] },
		"extra": func(v *ConsumerManifestMutation) {
			v.Members = append(v.Members, ConsumerManifestMember{InboundID: 12, ConsumerAddress: "11.0.0.2", ConsumerPort: 24012})
		},
		"foreign endpoint":  func(v *ConsumerManifestMutation) { v.Members[0].ConsumerPort++ },
		"unapproved":        func(v *ConsumerManifestMutation) { v.Members[0].ConsumerAddress = "11.0.0.9" },
		"origin input":      func(v *ConsumerManifestMutation) { v.Members[0].OriginPort = 10000 },
		"duplicate inbound": func(v *ConsumerManifestMutation) { v.Members[1].InboundID = 10 },
		"duplicate ingress": func(v *ConsumerManifestMutation) { v.Members[1].ConsumerPort = v.Members[0].ConsumerPort },
		"dns":               func(v *ConsumerManifestMutation) { v.Members[0].ConsumerAddress = "example.com" },
		"loopback":          func(v *ConsumerManifestMutation) { v.Members[0].ConsumerAddress = "127.0.0.1" },
	} {
		t.Run(name, func(t *testing.T) {
			in := consumerInput(1250, 0, false)
			change(&in)
			if _, err := f.s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, in); err == nil {
				t.Fatal("invalid consumer manifest accepted")
			}
		})
	}
	if _, err := f.s.MutateMeteredConsumerManifest(ctx, f.key, "not-owner", f.in.ID, consumerInput(1251, 0, false)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("non-owner manifest", err)
	}
	m := saveConsumerManifest(t, f, consumerInput(1252, 0, false))
	if _, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: m.EventID, Action: "revoke", ExpectedGeneration: 1}); !errors.Is(err, ErrConflict) {
		t.Fatal("manifest event reused for token", err)
	}
	if _, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: m.EventID, ClientID: f.client.ID, RoundID: routeTestID(1299), ConsumerManifestID: m.ID, ConsumerManifestRevision: m.Revision}, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("manifest event reused for snapshot", err)
	}
	changed := consumerInput(1252, 0, true)
	if _, err := f.s.MutateMeteredConsumerManifest(ctx, f.key, "owner", f.in.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("same event different path", err)
	}
	for _, query := range []string{`UPDATE pro_consumer_manifests SET state='revoked'`, `DELETE FROM pro_consumer_manifests`, `DELETE FROM pro_consumer_manifest_heads`, `UPDATE pro_consumer_manifest_heads SET revision=revision+1`} {
		if _, err := f.s.db.Exec(query); err == nil {
			t.Fatal("mutable manifest evidence", query)
		}
	}
	if m.Revision != 1 {
		t.Fatal("invalid requests consumed revision")
	}
}

func TestConsumerManifestSchemaStickyAndTamperFailsClosed(t *testing.T) {
	ctx := context.Background()
	s, path := opened(t)
	var version, objects int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("default version changed", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_consumer_*'`).Scan(&objects); err != nil || objects != 0 {
		t.Fatal("default consumer objects", err)
	}
	if err := s.EnableConsumerManifests(ctx, []mx.ConsumerEndpointApproval{{NodeID: "n", InstallationID: routeTestID(1), InboundID: 10, Address: "11.0.0.2", Port: 24010}}); err == nil {
		t.Fatal("consumer feature without N18")
	}
	enableSubscriptionSchema(t, s)
	approvals := []mx.ConsumerEndpointApproval{{NodeID: "n", InstallationID: routeTestID(1), InboundID: 10, Address: "11.0.0.2", Port: 24010}}
	if err := s.EnableConsumerManifests(ctx, approvals); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableConsumerManifests(ctx, approvals); err != nil {
		t.Fatal("idempotent enable", err)
	}
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != usageledger.ConsumerManifestsBusinessSchemaVersion {
		t.Fatal("consumer version", err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal("schema14 reopen", err)
	}
	defer s2.Close()
	if s2.consumerPolicy != nil {
		t.Fatal("reopen fabricated startup authorization")
	}
	if _, err = s.db.Exec(`DROP TRIGGER pro_consumer_manifests_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); !errors.Is(err, usageledger.ErrSchema) {
		t.Fatal("tampered immutable consumer schema accepted", err)
	}
	t.Run("partial namespace", func(t *testing.T) {
		s, path := opened(t)
		if _, err := s.db.Exec(`CREATE TABLE pro_consumer_unreviewed(id TEXT)`); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path); !errors.Is(err, usageledger.ErrSchema) {
			t.Fatal("unknown consumer namespace accepted", err)
		}
	})
	t.Run("body tamper", func(t *testing.T) {
		f, _, _ := consumerFixture(t)
		m := saveConsumerManifest(t, f, consumerInput(1260, 0, false))
		if _, err := f.s.db.Exec(`DROP TRIGGER pro_consumer_manifests_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.db.Exec(`UPDATE pro_consumer_manifests SET manifest=replace(manifest,?,?) WHERE id=?`, "11.0.0.2", "11.0.0.3", m.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.MeteredConsumerManifest(ctx, f.key, "owner", f.in.ID); err == nil {
			t.Fatal("mutated manifest digest accepted")
		}
	})
}
