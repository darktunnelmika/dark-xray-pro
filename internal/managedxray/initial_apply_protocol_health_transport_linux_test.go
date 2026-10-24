//go:build linux

package managedxray

import (
	"context"
	"strings"
	"testing"
)

func newInitialApplyHealthTransportHarness(t *testing.T) *healthTransportHarness {
	t.Helper()
	h := newHealthTransportHarness(t)
	h.base.Version, h.base.HandoverID, h.base.InitialApplySHA = InitialApplyProtocolHealthVersion, "", strings.Repeat("a", 64)
	var err error
	h.base, err = SignProtocolHealthRequest(h.key, h.base)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestInitialApplyProtocolTransportOutboxSurvivesRestartExpiryAndLostReplies(t *testing.T) {
	h := newInitialApplyHealthTransportHarness(t)
	q := h.request(1, h.base.Issued)
	h.deliver(q)
	h.statuses = []int{503, 503, 204}
	probes := 0
	probe := func(_ context.Context, got ProtocolHealthRequest, endpoint ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		probes++
		if got != q || endpoint.SHA() != q.EndpointSHA {
			t.Fatal("v4 transport changed initial Apply or verifier authority")
		}
		s, err := h.disk.load(h.key)
		if err != nil || s.Health == nil || s.Health.Request != q || s.Health.Receipt != nil {
			t.Fatal("initial Apply probe started before its durable intent", err)
		}
		return h.proof(got), nil
	}
	if err := h.sync(h.open(), probe); err == nil || probes != 1 {
		t.Fatal("lost initial Apply receipt response was hidden or probe did not execute once", err, probes)
	}
	noProbe := func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		t.Fatal("historical initial Apply outbox caused another probe")
		return ProtocolHealthReceipt{}, ErrProtocolHealth
	}
	h.now.Store(q.Expires)
	restarted := h.open()
	restarted.instance = strings.Repeat("1", 32)
	if err := h.sync(restarted, noProbe); err == nil {
		t.Fatal("lost expired historical receipt response was hidden")
	}
	p := h.proof(q)
	s, err := h.disk.load(h.key)
	if err != nil || s.Health == nil || s.Health.Request != q || s.Health.Receipt == nil || *s.Health.Receipt != p || s.Health.Acknowledged || s.Health.RejectedAfterExpiry {
		t.Fatal("restart or expiry discarded the exact unacknowledged v4 proof", err)
	}
	if err = h.sync(h.open(), noProbe); err != nil || probes != 1 {
		t.Fatal("v4 receipt replay after restart failed", err, probes)
	}
	h.mu.Lock()
	if len(h.receipts) != 3 || h.receipts[0] != h.receipts[1] || h.receipts[0] != h.receipts[2] || h.receipts[0].Receipt != p || h.polls != 2 {
		t.Error("v4 outbox changed evidence or polled ahead of its pending receipt")
	}
	h.mu.Unlock()
	s, err = h.disk.load(h.key)
	if err != nil || s.Health == nil || !s.Health.Acknowledged || *s.Health.Receipt != p {
		t.Fatal("initial Apply receipt acknowledgment was not durable", err)
	}
	// A still-retained nonce cannot move to another promotion/Apply digest,
	// even when its changed request is signed by the same trusted HUB.
	h.now.Store(q.Issued + 1)
	changed := q
	changed.InitialApplySHA = strings.Repeat("b", 64)
	changed, err = SignProtocolHealthRequest(h.key, changed)
	if err != nil {
		t.Fatal(err)
	}
	h.deliver(changed)
	if err = h.sync(h.open(), noProbe); err == nil {
		t.Fatal("retained nonce changed initial Apply authority")
	}
}

func TestInitialApplyProtocolTransportUnknownAttemptRetirementSurvivesRestart(t *testing.T) {
	h := newInitialApplyHealthTransportHarness(t)
	a := h.request(1, h.base.Issued)
	h.deliver(a)
	probes := 0
	interrupted := func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		probes++
		return ProtocolHealthReceipt{}, ErrProtocolHealth
	}
	if err := h.sync(h.open(), interrupted); err == nil || probes != 1 {
		t.Fatal("interrupted initial Apply probe did not retain an unknown attempt", err, probes)
	}
	for _, q := range []ProtocolHealthRequest{a, h.request(2, h.base.Issued)} {
		h.deliver(q)
		if err := h.sync(h.open(), interrupted); err == nil || probes != 1 {
			t.Fatal("restart reexecuted or displaced a live unknown v4 attempt", err, probes)
		}
	}
	h.now.Store(a.Expires)
	b := h.request(3, a.Expires)
	h.deliver(b)
	completed := func(_ context.Context, q ProtocolHealthRequest, _ ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		probes++
		return h.proof(q), nil
	}
	if err := h.sync(h.open(), completed); err != nil || probes != 2 {
		t.Fatal("fresh initial Apply challenge after expiry failed", err, probes)
	}
	s, err := h.disk.load(h.key)
	if err != nil || s.HealthRetiredThrough != a.Expires || s.Health == nil || s.Health.Request != b || len(s.HealthHistory) != 0 {
		t.Fatal("v4 unknown-attempt retirement lost its durable barrier", err)
	}
	h.now.Store(a.Issued + 1)
	h.deliver(a)
	if err = h.sync(h.open(), completed); err == nil || probes != 2 {
		t.Fatal("restart and clock rollback resurrected retired initial Apply proof work", err, probes)
	}
}

func TestInitialApplyProtocolJournalPreservesMixedEvidenceAndRejectsScopeSubstitution(t *testing.T) {
	h := newHealthTransportHarness(t)
	legacy := h.request(1, h.base.Issued)
	consumer := h.request(2, h.base.Issued)
	consumer.Version, consumer.ConsumerManifestSHA, consumer.ConsumerAddress, consumer.ConsumerPort = ConsumerProtocolHealthVersion, strings.Repeat("9", 64), "11.0.0.2", 39443
	consumer, err := SignProtocolHealthRequest(h.key, consumer)
	if err != nil {
		t.Fatal(err)
	}
	initial := h.request(3, h.base.Issued)
	initial.Version, initial.HandoverID, initial.InitialApplySHA = InitialApplyProtocolHealthVersion, "", strings.Repeat("a", 64)
	initial, err = SignProtocolHealthRequest(h.key, initial)
	if err != nil {
		t.Fatal(err)
	}
	pl, pc, pi := h.proof(legacy), h.proof(consumer), h.proof(initial)
	state := transactionState{Version: meteredTransactionJournalVersion,
		Health: &protocolHealthAttempt{Request: initial, Receipt: &pi},
		HealthHistory: []protocolHealthAttempt{
			{Request: legacy, Receipt: &pl, Acknowledged: true},
			{Request: consumer, Receipt: &pc, Acknowledged: true},
		},
	}
	if err = h.disk.save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := h.disk.load(h.key)
	if err != nil || loaded.Version != meteredTransactionJournalVersion || loaded.Health == nil || loaded.Health.Request != initial || *loaded.Health.Receipt != pi || len(loaded.HealthHistory) != 2 || loaded.HealthHistory[0].Request != legacy || *loaded.HealthHistory[0].Receipt != pl || loaded.HealthHistory[1].Request != consumer || *loaded.HealthHistory[1].Receipt != pc {
		t.Fatal("mixed v2/v3/v4 journal changed historical signed evidence", err)
	}
	changed := initial
	changed.InitialApplySHA = strings.Repeat("b", 64)
	changed, err = SignProtocolHealthRequest(h.key, changed)
	if err != nil {
		t.Fatal(err)
	}
	state.Health.Request = changed
	if err = h.disk.save(state); err != nil {
		t.Fatal(err)
	}
	if _, err = h.disk.load(h.key); err == nil {
		t.Fatal("journal transplanted an old receipt into another signed initial Apply scope")
	}
}
