package business

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestMeteredHealthQueueDurableOwnerIntentAndScopedDelivery(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t)
	if err := f.s.EnableMeteredHealthQueue(ctx); err != nil {
		t.Fatal(err)
	}
	id := routeTestID(251)
	v, err := f.s.QueueMeteredHealth(ctx, f.key, id, f.in.ID, 10, healthEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	if again, err := f.s.QueueMeteredHealth(ctx, f.key, id, f.in.ID, 10, healthEndpoint()); err != nil || again != v {
		t.Fatal("owner retry", err)
	}
	if _, err := f.s.QueueMeteredHealth(ctx, f.key, id, f.in.ID, 11, healthEndpoint()); err == nil {
		t.Fatal("intent rebound")
	}
	if _, err := f.s.QueueMeteredHealth(ctx, f.key, routeTestID(252), f.in.ID, 10, healthEndpoint()); err == nil {
		t.Fatal("parallel owner intent")
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
	if _, found, err := s.PollMeteredHealth(ctx, f.key, routeTestToken, f.source.NodeID, f.source.InstallationID, healthEndpoint()); err != nil || found {
		t.Fatal("source acquired target probe", err)
	}
	if _, found, err := s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, strings.Repeat("e", 32), healthEndpoint()); err != nil || found {
		t.Fatal("other installation acquired probe", err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, found, e := s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint())
			if !found && e == nil {
				t.Error("owner work lost")
			}
			ids <- q.ID
			errs <- e
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var nonce string
	for id := range ids {
		if nonce == "" {
			nonce = id
		}
		if nonce != id {
			t.Fatal("poll retry replaced nonce")
		}
	}
	q, found, err := s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint())
	if err != nil || !found || q.Expires > v.Expires {
		t.Fatal("delivery exceeded owner window", err)
	}
	if _, err = s.ConsumeMeteredHealthProof(ctx, f.key, routeTestToken, q.ID, healthEndpoint(), healthProof(q)); err != nil {
		t.Fatal(err)
	}
	if _, found, err = s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint()); err != nil || found {
		t.Fatal("consumed work delivered again", err)
	}
	a, err := s.MeteredHandoverAudit(ctx, f.key, f.in.ID)
	if err != nil || len(a.HealthIntents) != 1 || a.HistoricalHealthEvidence == nil || a.CanPublish {
		t.Fatal("owner audit", err)
	}
}

func TestMeteredHealthQueueRechecksOwnerBindingAndImmutableSchema(t *testing.T) {
	ctx := context.Background()
	f := healthApplied(t)
	if err := f.s.EnableMeteredHealthQueue(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.QueueMeteredHealth(ctx, f.key, routeTestID(253), f.in.ID, 10, healthEndpoint()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE pro_health_intents SET expires=expires+1`, `DELETE FROM pro_health_intents`} {
		if _, err := f.s.db.Exec(q); err == nil {
			t.Fatal("mutable owner intent")
		}
	}
	f.target.Status.Instance = routeTestID(254)
	meteredQueuePoll(t, f.s, f.key, &f.target, false)
	if _, _, err := f.s.PollMeteredHealth(ctx, f.key, routeTestToken, f.target.NodeID, f.target.InstallationID, healthEndpoint()); err == nil {
		t.Fatal("changed runtime accepted")
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER pro_health_intents_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(f.path); err == nil {
		s.Close()
		t.Fatal("damaged queue policy repaired")
	}
}
