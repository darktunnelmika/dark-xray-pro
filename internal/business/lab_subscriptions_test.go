package business

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func enableSubscriptionSchema(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, enable := range []func(context.Context) error{s.EnableUsageLedger, s.EnableSystemProbeAccounting, s.EnableMeteredHealthChallenges, s.EnableMeteredHealthQueue, s.EnableMeteredHealthRounds, s.EnableLabSubscriptions} {
		if err := enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
func labSubscriptionFixture(t *testing.T, complete bool) (*handoverFixture, MeteredHealthRound, SubscriptionSecret) {
	t.Helper()
	ctx := context.Background()
	f := healthRoundApplied(t, "tcp", true)
	if _, err := f.s.BootstrapOwner(ctx, "owner-lab", "synthetic-subscription-password"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableLabSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: routeTestID(901), Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secret, err := f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	round := queuedHealthRound(t, f, 902)
	if complete {
		completeSubscriptionRound(t, f, round)
	}
	return f, round, secret
}
func completeSubscriptionRound(t *testing.T, f *handoverFixture, round MeteredHealthRound) {
	t.Helper()
	for range round.Members {
		q := roundPoll(t, f)
		if _, err := f.s.ConsumeMeteredHealthProof(context.Background(), f.key, routeTestToken, q.ID, healthEndpoint(), healthProof(q)); err != nil {
			t.Fatal(err)
		}
	}
}
func materializeSubscription(t *testing.T, f *handoverFixture, round MeteredHealthRound) LabSubscriptionSnapshot {
	t.Helper()
	v, err := f.s.MaterializeLabSubscription(context.Background(), f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(903), ClientID: f.client.ID, RoundID: round.ID}, healthEndpoint())
	if err != nil {
		t.Fatal("materialize", err)
	}
	return v
}

func TestLabSubscriptionSnapshotCompleteProofDeliveryRotationAndRetry(t *testing.T) {
	ctx := context.Background()
	f, round, secret := labSubscriptionFixture(t, false)
	before, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil {
		t.Fatal(err)
	}
	var generation uint64
	if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	in := LabSubscriptionMaterialization{EventID: routeTestID(903), ClientID: f.client.ID, RoundID: round.ID}
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, in, healthEndpoint()); err == nil {
		t.Fatal("pending set published")
	}
	q := roundPoll(t, f)
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), healthProof(q)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, in, healthEndpoint()); err == nil {
		t.Fatal("subset covered full candidate")
	}
	q = roundPoll(t, f)
	if _, err = f.s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), healthProof(q)); err != nil {
		t.Fatal(err)
	}
	snapshot := materializeSubscription(t, f, round)
	if snapshot.EventID != in.EventID || snapshot.ID == snapshot.EventID {
		t.Fatal("snapshot event correlation lost or confused with identity")
	}
	if snapshot.RouteCount != 2 || snapshot.Revision != 1 || !snapshot.Current || snapshot.ExpiresUnixNano != round.Expires || snapshot.ExpiresUnixNano > f.target.Status.Observed.LeaseExpires {
		t.Fatal("unbounded/partial snapshot", snapshot)
	}
	content, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || !content.Snapshot.Current || content.SourceNodeID != f.source.NodeID || content.SourceInstallationID != f.source.InstallationID || bytes.Count(content.Raw, []byte("vless://")) != 2 || bytes.Count(content.Raw, []byte(f.client.UUID)) != 2 || bytes.Contains(content.Raw, []byte("privateKey")) || bytes.Contains(content.Raw, []byte("system_probe")) {
		t.Fatal("consumer output mismatch", err)
	}
	var cipher []byte
	if err = f.s.db.QueryRow(`SELECT payload_enc FROM pro_subscription_snapshots WHERE id=?`, snapshot.ID).Scan(&cipher); err != nil || bytes.Contains(cipher, []byte(f.client.UUID)) || bytes.Contains(cipher, content.Raw) {
		t.Fatal("snapshot stored plaintext", err)
	}
	if err = f.s.db.QueryRow(`SELECT token_enc FROM pro_subscription_credentials WHERE client_id=?`, f.client.ID).Scan(&cipher); err != nil || bytes.Contains(cipher, []byte(secret.Token)) {
		t.Fatal("token stored plaintext", err)
	}
	wrongKey := bytes.Repeat([]byte{9}, 32)
	if _, err = f.s.ReadLabSubscription(ctx, wrongKey, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("wrong key read")
	}
	again, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, in, healthEndpoint())
	if err != nil || !again.Replayed || again.ID != snapshot.ID || again.CreatedUnixNano != snapshot.CreatedUnixNano || again.ExpiresUnixNano != snapshot.ExpiresUnixNano {
		t.Fatal("retry changed snapshot", err)
	}
	changed := in
	changed.ExpectedRevision = 1
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, changed, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("retry mismatch", err)
	}
	changed.EventID = routeTestID(904)
	next, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, changed, healthEndpoint())
	if err != nil || next.ID == snapshot.ID || next.Revision != 2 || next.ExpiresUnixNano != snapshot.ExpiresUnixNano {
		t.Fatal("snapshot replacement changed lease", err)
	}
	again, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, in, healthEndpoint())
	if err != nil || !again.Replayed || again.Current || again.ID != snapshot.ID {
		t.Fatal("old retry resurrected pointer", err)
	}
	otherEndpoint := healthEndpoint()
	otherEndpoint.CertificateSHA = strings.Repeat("b", 64)
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, otherEndpoint); err == nil {
		t.Fatal("endpoint change accepted old proof")
	}
	view, err := f.s.LabSubscription(ctx, f.key, "owner", f.client.ID, otherEndpoint)
	if err != nil || view.Snapshot == nil || view.Snapshot.Current {
		t.Fatal("metadata used unpinned proof", err)
	}
	rotation := SubscriptionTokenMutation{EventID: routeTestID(905), Action: "rotate", ExpectedGeneration: 1}
	receipt, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, rotation)
	if err != nil || receipt.Generation != 2 || receipt.State != "active" {
		t.Fatal("rotate", err)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token survived rotation", err)
	}
	secret2, err := f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 2)
	if err != nil || secret2.Token == secret.Token {
		t.Fatal("rotation reused token", err)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret2.Token, healthEndpoint()); err != nil {
		t.Fatal("rotation changed independent snapshot", err)
	}
	if _, err = f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: routeTestID(906), Action: "revoke", ExpectedGeneration: 2}); err != nil {
		t.Fatal(err)
	}
	old, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, rotation)
	if err != nil || !old.Replayed || !old.Superseded || old.Generation != 2 {
		t.Fatal("superseded retry", err)
	}
	if _, err = f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 2); err == nil {
		t.Fatal("revealed revoked historical secret")
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret2.Token, healthEndpoint()); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked token read", err)
	}
	resume := SubscriptionTokenMutation{EventID: routeTestID(907), Action: "rotate", ExpectedGeneration: 3}
	if _, err = f.s.MutateLabSubscription(ctx, wrongKey, "owner", f.client.ID, resume); err == nil {
		t.Fatal("wrong master key replaced revoked credential")
	}
	if resumed, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, resume); err != nil || resumed.Generation != 4 {
		t.Fatal("valid rotation from revoked failed", err)
	}
	after, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || meteredHash(before) != meteredHash(after) {
		t.Fatal("changed customer accounting", err)
	}
	var afterGeneration uint64
	var reservations int
	if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&afterGeneration); err != nil || afterGeneration != generation {
		t.Fatal("changed runtime generation", err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM native_metered_handover_nodes WHERE handover_id=?`, f.in.ID).Scan(&reservations); err != nil || reservations != 2 {
		t.Fatal("released reservations", err)
	}
	if a := roundAudit(t, f); a.CanPublish || a.Phase != "target_applied_health_required" {
		t.Fatal("promoted LAB to production publication")
	}
}

func TestLabSubscriptionTwoHandleCASAndRevocationRead(t *testing.T) {
	ctx := context.Background()
	f, round, secret := labSubscriptionFixture(t, true)
	s2, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, store := range []*Store{f.s, s2} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			_, e := store.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(910 + i), ClientID: f.client.ID, RoundID: round.ID}, healthEndpoint())
			results <- e
		}(i, store)
	}
	wg.Wait()
	close(results)
	committed := 0
	for e := range results {
		if e == nil {
			committed++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if committed != 1 {
		t.Fatal("two snapshot CAS writers committed", committed)
	}
	results = make(chan error, 2)
	for i, store := range []*Store{f.s, s2} {
		wg.Add(1)
		go func(i int, store *Store) {
			defer wg.Done()
			_, e := store.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: routeTestID(912 + i), Action: "rotate", ExpectedGeneration: 1})
			results <- e
		}(i, store)
	}
	wg.Wait()
	close(results)
	committed = 0
	for e := range results {
		if e == nil {
			committed++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if committed != 1 {
		t.Fatal("two rotation CAS writers committed", committed)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token survived competing rotation", err)
	}
	current, err := f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s2.ReadLabSubscription(ctx, f.key, current.Token, healthEndpoint()); err != nil {
		t.Fatal("reopened reader lost snapshot", err)
	}
	// Reads may linearize before the revoke, but no read begun after it commits
	// may expose content. Exercise the two independent connection pools.
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = f.s.ReadLabSubscription(ctx, f.key, current.Token, healthEndpoint())
		}()
	}
	close(start)
	if _, err = s2.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: routeTestID(915), Action: "revoke", ExpectedGeneration: 2}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for _, store := range []*Store{f.s, s2} {
		if _, err = store.ReadLabSubscription(ctx, f.key, current.Token, healthEndpoint()); !errors.Is(err, ErrNotFound) {
			t.Fatal("read after committed revoke", err)
		}
	}
	if _, err = f.s.db.Exec(`UPDATE pro_subscription_token_heads SET generation=1 WHERE client_id=?`, f.client.ID); err == nil {
		t.Fatal("token head rollback allowed")
	}
	if _, err = f.s.db.Exec(`DELETE FROM pro_subscription_snapshot_heads WHERE client_id=?`, f.client.ID); err == nil {
		t.Fatal("snapshot pointer deletion allowed")
	}
}

func TestLabSubscriptionCurrentEligibilityAndRuntimeInvalidation(t *testing.T) {
	for _, test := range []struct {
		name, query string
		args        func(*handoverFixture) []any
	}{
		{"disabled", `UPDATE clients SET deployment_state='disabled' WHERE id=?`, func(f *handoverFixture) []any { return []any{f.client.ID} }},
		{"archived", `UPDATE clients SET archived=1 WHERE id=?`, func(f *handoverFixture) []any { return []any{f.client.ID} }},
		{"expired", `UPDATE clients SET expires_unix=? WHERE id=?`, func(f *handoverFixture) []any { return []any{time.Now().Unix() - 1, f.client.ID} }},
		{"representative_disabled", `UPDATE representatives SET enabled=0 WHERE id=?`, func(f *handoverFixture) []any { return []any{f.client.RepresentativeID} }},
		{"quota_hold", `UPDATE pro_usage_accounts SET holds=1 WHERE client_id=?`, func(f *handoverFixture) []any { return []any{f.client.ID} }},
		{"generation_changed", `UPDATE native_config_generation SET value=value+1 WHERE id=1`, func(f *handoverFixture) []any { return nil }},
		{"observation_lost", `UPDATE native_metered_route_channels SET observed_at=0 WHERE node_id=?`, func(f *handoverFixture) []any { return []any{f.target.NodeID} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, round, secret := labSubscriptionFixture(t, true)
			materializeSubscription(t, f, round)
			if _, err := f.s.db.Exec(test.query, test.args(f)...); err != nil {
				t.Fatal(err)
			}
			if content, err := f.s.ReadLabSubscription(context.Background(), f.key, secret.Token, healthEndpoint()); err == nil || len(content.Raw) != 0 {
				t.Fatal("stale snapshot served", err)
			}
		})
	}
}

func TestLabSubscriptionDeadlineSurvivesRestartAndRetry(t *testing.T) {
	ctx := context.Background()
	f, round, secret := labSubscriptionFixture(t, true)
	snapshot := materializeSubscription(t, f, round)
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err != nil {
		t.Fatal("fresh durable snapshot unreadable", err)
	}
	remaining := time.Until(time.Unix(0, snapshot.ExpiresUnixNano)) + 2*time.Millisecond
	if remaining > 0 {
		time.Sleep(remaining)
	}
	if content, err := s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil || len(content.Raw) != 0 {
		t.Fatal("served expired snapshot", err)
	}
	view, err := s.LabSubscription(ctx, f.key, "owner", f.client.ID, healthEndpoint())
	if err != nil || view.Snapshot == nil || view.Snapshot.Current || view.Snapshot.ExpiresUnixNano != snapshot.ExpiresUnixNano {
		t.Fatal("restart renewed deadline", err)
	}
	again, err := s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(903), ClientID: f.client.ID, RoundID: round.ID}, healthEndpoint())
	if err != nil || !again.Replayed || again.Current || again.ExpiresUnixNano != snapshot.ExpiresUnixNano || again.CreatedUnixNano != snapshot.CreatedUnixNano {
		t.Fatal("historical retry renewed proof", err)
	}
	if reveal, err := s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 1); err != nil || reveal.Token != secret.Token {
		t.Fatal("deadline changed independent token", err)
	}
}

func TestLabSubscriptionSchemaOptInAndTamperFailClosed(t *testing.T) {
	ctx := context.Background()
	s, path := opened(t)
	var version, objects int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("default version changed", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_subscription_*'`).Scan(&objects); err != nil || objects != 0 {
		t.Fatal("default enabled subscriptions", err)
	}
	if err := s.EnableLabSubscriptions(ctx); err == nil {
		t.Fatal("enabled without N17")
	}
	enableSubscriptionSchema(t, s)
	if err := s.EnableLabSubscriptions(ctx); err != nil {
		t.Fatal("enable retry", err)
	}
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != usageledger.LabSubscriptionsBusinessSchemaVersion {
		t.Fatal("explicit schema version", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal("sticky reopen", err)
	}
	defer s.Close()
	if s.usage == nil {
		t.Fatal("reopen forgot usage constraints")
	}
	if _, err = s.db.Exec(`DROP TRIGGER pro_subscription_token_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); !errors.Is(err, usageledger.ErrSchema) {
		t.Fatal("altered immutable schema accepted", err)
	}
	t.Run("partial namespace", func(t *testing.T) {
		s, path := opened(t)
		if _, e := s.db.Exec(`CREATE TABLE pro_subscription_unreviewed(id TEXT)`); e != nil {
			t.Fatal(e)
		}
		if _, e := Open(path); !errors.Is(e, usageledger.ErrSchema) {
			t.Fatal("partial namespace silently adopted", e)
		}
	})
}

func TestLabSubscriptionCiphertextAndPointerTamper(t *testing.T) {
	f, round, secret := labSubscriptionFixture(t, true)
	snapshot := materializeSubscription(t, f, round)
	for _, q := range []string{`UPDATE pro_subscription_snapshots SET body_sha='bad'`, `DELETE FROM pro_subscription_credentials`, `UPDATE pro_subscription_token_heads SET credential_id=NULL,state='revoked'`} {
		if _, err := f.s.db.Exec(q); err == nil {
			t.Fatal("immutable state changed", q)
		}
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER pro_subscription_snapshots_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec(`UPDATE pro_subscription_snapshots SET payload_enc=zeroblob(length(payload_enc)) WHERE id=?`, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReadLabSubscription(context.Background(), f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("tampered ciphertext served")
	}
}

func TestLabSubscriptionScopedPermissionsAndRetryReauthorization(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if _, err := s.BootstrapOwner(ctx, "owner-scope", "synthetic-subscription-password"); err != nil {
		t.Fatal(err)
	}
	enableSubscriptionSchema(t, s)
	key := bytes.Repeat([]byte{7}, 32)
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("sub-rep-%d", i)
		newRep(t, s, id)
		grant(t, s, id, "subscription-grant-"+id, 1000, 0)
		if _, err := s.ProvisionRepresentativeLogin(ctx, id, id, "synthetic-subscription-password"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateClient(ctx, NewClient{EventID: "subscription-create-" + id, ClientID: "client-" + id, RepresentativeID: id, Name: "Synthetic client", DurationDays: 30, VolumeBytes: 100}); err != nil {
			t.Fatal(err)
		}
	}
	clientID, actor := "client-sub-rep-0", "sub-rep-0"
	in := SubscriptionTokenMutation{EventID: routeTestID(930), Action: "issue"}
	if _, err := s.MutateLabSubscription(ctx, key, "sub-rep-1", clientID, in); err == nil {
		t.Fatal("cross-representative mutation")
	}
	if _, err := s.MutateLabSubscription(ctx, key, actor, clientID, in); err != nil {
		t.Fatal(err)
	}
	permissions := DefaultRepresentativePermissions()
	permissions.ManageClients = false
	change := RepresentativeControlChange{EventID: routeTestID(931), RepresentativeID: actor, DisplayName: "Scoped synthetic rep", Permissions: permissions, Reason: "Exercise subscription authority"}
	if _, err := s.UpdateRepresentativeControl(ctx, change); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevealLabSubscription(ctx, key, actor, clientID, 1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revealed after permission revoke", err)
	}
	if _, err := s.MutateLabSubscription(ctx, key, actor, clientID, in); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("replay bypassed current permission", err)
	}
	if view, err := s.LabSubscription(ctx, key, actor, clientID, healthEndpoint()); err != nil || view.State != "active" {
		t.Fatal("view_clients lost independent access", err)
	}
	if _, err := s.RevealLabSubscription(ctx, key, "owner", clientID, 1); err != nil {
		t.Fatal("owner scope lost", err)
	}
	change.EventID, change.ExpectedRevision, change.LoginSuspended = routeTestID(932), 1, true
	if _, err := s.UpdateRepresentativeControl(ctx, change); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabSubscription(ctx, key, actor, clientID, healthEndpoint()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("suspended login accessed metadata", err)
	}
}
