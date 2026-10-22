//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func healthTransportDisk(t *testing.T) *transactionDisk {
	t.Helper()
	key, _ := protocolHealthTestRequest(t)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return &transactionDisk{version: 2, dir: dir, aead: aead, aad: []byte("isolated health journal fixture")}
}

func TestProtocolHealthTransportDurableLostReplyAndRestart(t *testing.T) {
	ctx := context.Background()
	key, q := protocolHealthTestRequest(t)
	// Replay a proof that is already expired; only its original completion is
	// cryptographic evidence. The receiving HUB decides committed history.
	q.Issued = time.Now().Add(-10 * time.Second).UnixNano()
	q.Expires = q.Issued + int64(time.Second)
	q, _ = SignProtocolHealthRequest(key, q)
	p := ProtocolHealthReceipt{Version: ProtocolHealthVersion, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: q.Issued + 1}
	p.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", p)
	d := healthTransportDisk(t)
	state := transactionState{Version: 2, Health: &protocolHealthAttempt{Request: q, Receipt: &p}}
	if err := d.save(state); err != nil {
		t.Fatal(err)
	}
	var bodies []string
	polls := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/metered-health/poll" {
			polls++
			if len(bodies) != 2 {
				t.Error("polled before the saved proof was acknowledged")
			}
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/api/v1/agent/metered-health/receipt" {
			t.Error("unexpected health transport path")
			w.WriteHeader(500)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if r.Header.Get("Authorization") != "Bearer "+string(key) {
			t.Error("missing node authentication")
		}
		if len(bodies) == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	open := func() *TransactionRuntime {
		s, err := d.load(key)
		if err != nil {
			t.Fatal(err)
		}
		return &TransactionRuntime{metered: &MeteredTransactionOptions{}, runtime: &Runtime{}, journal: d, state: s, options: Options{LeaseKey: key, NodeID: q.NodeID, InstallationID: q.InstallationID}}
	}
	if err := open().SyncMeteredHealth(ctx, srv.Client(), srv.URL); err == nil {
		t.Fatal("lost reply hidden")
	}
	if err := open().SyncMeteredHealth(ctx, srv.Client(), srv.URL); err != nil {
		t.Fatal("restart receipt replay", err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || polls != 1 {
		t.Fatal("receipt changed on restart")
	}
	s, err := d.load(key)
	if err != nil || !s.Health.Acknowledged {
		t.Fatal("HUB ACK not durable", err)
	}
	raw, err := os.ReadFile(filepath.Join(d.dir, "transactions.enc"))
	if err != nil || strings.Contains(string(raw), q.ID) || strings.Contains(string(raw), string(key)) {
		t.Fatal("plaintext health journal", err)
	}
	s.Health.Receipt.Signature = strings.Repeat("a", 64)
	if err = d.save(s); err != nil {
		t.Fatal(err)
	}
	if _, err = d.load(key); err == nil {
		t.Fatal("invalid saved proof accepted")
	}
}

func TestProtocolHealthTransportUnknownAttemptCannotExecuteAgain(t *testing.T) {
	key, q := protocolHealthTestRequest(t)
	endpoint := ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.com", CertificateSHA: strings.Repeat("a", 64)}
	q.EndpointSHA = endpoint.SHA()
	q, _ = SignProtocolHealthRequest(key, q)
	d := healthTransportDisk(t)
	if err := d.save(transactionState{Version: 2, Health: &protocolHealthAttempt{Request: q}}); err != nil {
		t.Fatal(err)
	}
	polls := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/metered-health/poll" {
			t.Error("unknown probe tried to send receipt")
			w.WriteHeader(500)
			return
		}
		polls++
		json.NewEncoder(w).Encode(ProtocolHealthDelivery{q, endpoint})
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	for i := 0; i < 2; i++ {
		s, err := d.load(key)
		if err != nil {
			t.Fatal(err)
		}
		runtime := &TransactionRuntime{metered: &MeteredTransactionOptions{}, runtime: &Runtime{}, journal: d, state: s, instance: q.RuntimeInstance, options: Options{LeaseKey: key, NodeID: q.NodeID, InstallationID: q.InstallationID}}
		if err = runtime.SyncMeteredHealth(context.Background(), srv.Client(), srv.URL); err == nil {
			t.Fatal("unknown attempt reran")
		}
	}
	if polls != 2 {
		t.Fatal("unknown restart fixture", polls)
	}
}

type healthTransportHarness struct {
	t        *testing.T
	key      []byte
	base     ProtocolHealthRequest
	endpoint ProtocolHealthEndpoint
	disk     *transactionDisk
	server   *httptest.Server
	now      atomic.Int64
	mu       sync.Mutex
	queue    []ProtocolHealthRequest
	repeat   *ProtocolHealthRequest
	statuses []int
	receipts []ProtocolHealthSubmission
	polls    int
}

func newHealthTransportHarness(t *testing.T) *healthTransportHarness {
	t.Helper()
	key, q := protocolHealthTestRequest(t)
	h := &healthTransportHarness{t: t, key: key, base: q, disk: healthTransportDisk(t), endpoint: ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.com", CertificateSHA: strings.Repeat("a", 64)}}
	h.base.EndpointSHA = h.endpoint.SHA()
	h.base, _ = SignProtocolHealthRequest(key, h.base)
	h.now.Store(q.Issued + 1)
	h.server = httptest.NewUnstartedServer(http.HandlerFunc(h.serve))
	h.server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	h.server.StartTLS()
	t.Cleanup(h.server.Close)
	return h
}

func (h *healthTransportHarness) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+string(h.key) {
		h.t.Error("health request lost exact node authentication")
		w.WriteHeader(401)
		return
	}
	switch r.URL.Path {
	case "/api/v1/agent/metered-health/poll":
		h.polls++
		var q ProtocolHealthRequest
		if len(h.queue) != 0 {
			q, h.queue = h.queue[0], h.queue[1:]
		} else if h.repeat != nil {
			q = *h.repeat
		} else {
			w.WriteHeader(204)
			return
		}
		json.NewEncoder(w).Encode(ProtocolHealthDelivery{q, h.endpoint})
	case "/api/v1/agent/metered-health/receipt":
		var p ProtocolHealthSubmission
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			h.t.Error("invalid health receipt body", err)
			w.WriteHeader(400)
			return
		}
		// A receipt must already exist in the encrypted outbox before any post.
		s, err := h.disk.load(h.key)
		found := false
		if err == nil {
			for _, a := range protocolHealthAttempts(s) {
				if a.Request.ID == p.ChallengeID && a.Receipt != nil && *a.Receipt == p.Receipt {
					found = true
				}
			}
		}
		if !found {
			h.t.Error("posted proof was not durably saved first", err)
			w.WriteHeader(500)
			return
		}
		h.receipts = append(h.receipts, p)
		status := 204
		if len(h.statuses) != 0 {
			status, h.statuses = h.statuses[0], h.statuses[1:]
		}
		w.WriteHeader(status)
	default:
		h.t.Error("unexpected health path", r.URL.Path)
		w.WriteHeader(404)
	}
}

func (h *healthTransportHarness) request(id int, issued int64) ProtocolHealthRequest {
	h.t.Helper()
	q := h.base
	q.ID, q.InboundID, q.Port = fmt.Sprintf("%032x", id), uint64(7600+id), uint16(27600+id)
	q.Issued, q.Expires = issued, issued+int64(6*time.Second)
	q, err := SignProtocolHealthRequest(h.key, q)
	if err != nil {
		h.t.Fatal(err)
	}
	return q
}

func (h *healthTransportHarness) deliver(q ...ProtocolHealthRequest) {
	h.mu.Lock()
	h.queue = append(h.queue, q...)
	h.mu.Unlock()
}

func (h *healthTransportHarness) open() *TransactionRuntime {
	h.t.Helper()
	s, err := h.disk.load(h.key)
	if err != nil {
		h.t.Fatal(err)
	}
	return &TransactionRuntime{metered: &MeteredTransactionOptions{}, runtime: &Runtime{}, journal: h.disk, state: s, instance: h.base.RuntimeInstance, options: Options{LeaseKey: h.key, NodeID: h.base.NodeID, InstallationID: h.base.InstallationID}}
}

func (h *healthTransportHarness) proof(q ProtocolHealthRequest) ProtocolHealthReceipt {
	p := ProtocolHealthReceipt{Version: ProtocolHealthVersion, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: q.Issued + 1}
	p.Signature = transactionMAC(h.key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", p)
	return p
}

func (h *healthTransportHarness) sync(r *TransactionRuntime, probe func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error)) error {
	return r.syncMeteredHealth(context.Background(), h.server.Client(), h.server.URL, probe, func() time.Time { return time.Unix(0, h.now.Load()) })
}

func TestProtocolHealthTransportDrainsMembersAndReplaysDelayedPriorNonce(t *testing.T) {
	h := newHealthTransportHarness(t)
	a, b := h.request(1, h.base.Issued), h.request(2, h.base.Issued)
	h.deliver(a, b, a)
	probes := make(map[string]int)
	probe := func(ctx context.Context, q ProtocolHealthRequest, endpoint ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		probes[q.ID]++
		s, err := h.disk.load(h.key)
		if err != nil || s.Health == nil || s.Health.Request != q || s.Health.Receipt != nil {
			t.Fatal("probe started before durable intent", err)
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > maxProtocolHealthSyncTime {
			t.Fatal("sync has no independent time bound")
		}
		return h.proof(q), nil
	}
	if err := h.sync(h.open(), probe); err != nil {
		t.Fatal(err)
	}
	if probes[a.ID] != 1 || probes[b.ID] != 1 {
		t.Fatal("delayed prior nonce repeated execution", probes)
	}
	h.mu.Lock()
	if h.polls != 4 || len(h.receipts) != 3 || h.receipts[0] != h.receipts[2] {
		t.Error("members were not drained with exact historical replay", h.polls, len(h.receipts))
	}
	h.mu.Unlock()
	s, err := h.disk.load(h.key)
	if err != nil || s.Version != meteredTransactionJournalVersion || s.Health == nil || s.Health.Request != b || !s.Health.Acknowledged || len(s.HealthHistory) != 1 || s.HealthHistory[0].Request != a || !s.HealthHistory[0].Acknowledged {
		t.Fatal("replay history was not durable", err)
	}
	// Reopening does not lose A when B occupies the current slot.
	h.deliver(a)
	if err = h.sync(h.open(), probe); err != nil || probes[a.ID] != 1 || probes[b.ID] != 1 {
		t.Fatal("restart forgot prior challenge", err, probes)
	}
	changed := a
	changed.Port++
	changed, _ = SignProtocolHealthRequest(h.key, changed)
	h.deliver(changed)
	if err = h.sync(h.open(), probe); err == nil || probes[a.ID] != 1 {
		t.Fatal("same nonce with a different signed binding was executed", err, probes)
	}
}

func TestProtocolHealthTransportUnknownAttemptRetirementRejectsClockRollback(t *testing.T) {
	h := newHealthTransportHarness(t)
	a := h.request(1, h.base.Issued)
	h.deliver(a)
	probes := 0
	interrupted := func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		probes++
		return ProtocolHealthReceipt{}, ErrProtocolHealth
	}
	if err := h.sync(h.open(), interrupted); err == nil || probes != 1 {
		t.Fatal("interrupted attempt was hidden", err, probes)
	}
	// A lost attempt cannot be reexecuted or displaced while still unexpired.
	for _, q := range []ProtocolHealthRequest{a, h.request(2, h.base.Issued)} {
		h.deliver(q)
		if err := h.sync(h.open(), interrupted); err == nil || probes != 1 {
			t.Fatal("unresolved attempt was reexecuted/displaced", err, probes)
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
		t.Fatal("fresh challenge after expiry failed", err, probes)
	}
	s, err := h.disk.load(h.key)
	if err != nil || s.HealthRetiredThrough != a.Expires || s.Health == nil || s.Health.Request != b || len(s.HealthHistory) != 0 {
		t.Fatal("expired unknown attempt has no durable retirement barrier", err)
	}
	// Make A valid under the test wall clock again. The saved barrier, not the
	// current time, must prevent its nonce from executing after a restart.
	h.now.Store(a.Issued + 1)
	h.deliver(a)
	if err = h.sync(h.open(), completed); err == nil || probes != 2 {
		t.Fatal("clock rollback resurrected a retired nonce", err, probes)
	}
}

func TestProtocolHealthTransportHistoricalOutboxSurvivesLossAndRestart(t *testing.T) {
	h := newHealthTransportHarness(t)
	a := h.request(1, h.base.Issued-int64(20*time.Second))
	b := h.request(2, h.base.Issued)
	pa, pb := h.proof(a), h.proof(b)
	state := transactionState{Version: meteredTransactionJournalVersion, Health: &protocolHealthAttempt{Request: b, Receipt: &pb, Acknowledged: true}, HealthHistory: []protocolHealthAttempt{{Request: a, Receipt: &pa}}, HealthRetiredThrough: a.Expires + 1}
	if err := h.disk.save(state); err != nil {
		t.Fatal(err)
	}
	h.statuses = []int{503, 204}
	noProbe := func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		t.Fatal("saved historical outbox caused a new probe")
		return ProtocolHealthReceipt{}, ErrProtocolHealth
	}
	if err := h.sync(h.open(), noProbe); err == nil {
		t.Fatal("lost historical reply was hidden")
	}
	s, err := h.disk.load(h.key)
	if err != nil || len(s.HealthHistory) != 1 || s.HealthHistory[0].Acknowledged || *s.HealthHistory[0].Receipt != pa {
		t.Fatal("lost reply discarded/altered outbox", err)
	}
	if err = h.sync(h.open(), noProbe); err != nil {
		t.Fatal("historical outbox restart replay", err)
	}
	h.mu.Lock()
	if len(h.receipts) != 2 || h.receipts[0] != h.receipts[1] || h.polls != 1 {
		t.Error("historical outbox replay was not exact/prioritized")
	}
	h.mu.Unlock()
	s, err = h.disk.load(h.key)
	if err != nil || !s.HealthHistory[0].Acknowledged || *s.HealthHistory[0].Receipt != pa || s.Health.Request != b {
		t.Fatal("historical outbox ACK was not durable", err)
	}
}

func TestProtocolHealthTransportRefusalIsTerminalOnlyAfterExpiry(t *testing.T) {
	h := newHealthTransportHarness(t)
	a := h.request(1, h.base.Issued)
	p := h.proof(a)
	if err := h.disk.save(transactionState{Version: meteredTransactionJournalVersion, Health: &protocolHealthAttempt{Request: a, Receipt: &p}}); err != nil {
		t.Fatal(err)
	}
	h.statuses = []int{409, 409}
	noProbe := func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		t.Fatal("HUB refusal reexecuted a saved proof")
		return ProtocolHealthReceipt{}, ErrProtocolHealth
	}
	if err := h.sync(h.open(), noProbe); err == nil {
		t.Fatal("early refusal was hidden")
	}
	s, err := h.disk.load(h.key)
	if err != nil || s.Health == nil || s.Health.Acknowledged || s.Health.RejectedAfterExpiry || *s.Health.Receipt != p {
		t.Fatal("early refusal discarded pending proof", err)
	}
	h.now.Store(a.Expires)
	if err = h.sync(h.open(), noProbe); err == nil {
		t.Fatal("terminal refusal was hidden")
	}
	s, err = h.disk.load(h.key)
	if err != nil || s.Health == nil || s.Health.Acknowledged || !s.Health.RejectedAfterExpiry || *s.Health.Receipt != p {
		t.Fatal("authenticated expired refusal was not durable", err)
	}
	b := h.request(2, a.Expires)
	h.deliver(b)
	if err = h.sync(h.open(), func(_ context.Context, q ProtocolHealthRequest, _ ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
		return h.proof(q), nil
	}); err != nil {
		t.Fatal("terminal refusal blocked fresh work", err)
	}
	s, err = h.disk.load(h.key)
	if err != nil || s.Health.Request != b || s.HealthRetiredThrough != a.Expires || len(s.HealthHistory) != 0 {
		t.Fatal("refused proof retirement was not atomic with fresh intent", err)
	}
}

func TestProtocolHealthTransportHistoryBoundNeverEvictsLiveOrUnackedWork(t *testing.T) {
	h := newHealthTransportHarness(t)
	s := transactionState{Version: meteredTransactionJournalVersion}
	for i := 1; i <= maxProtocolHealthAttempts; i++ {
		q := h.request(i, h.base.Issued)
		p := h.proof(q)
		s.HealthHistory = append(s.HealthHistory, protocolHealthAttempt{Request: q, Receipt: &p, Acknowledged: true})
	}
	if err := h.disk.save(s); err != nil {
		t.Fatal(err)
	}
	q := h.request(maxProtocolHealthAttempts+1, h.base.Issued)
	r := h.open()
	if err := r.beginProtocolHealthAttempt(q, h.now.Load()); err == nil {
		t.Fatal("full history evicted a live nonce")
	}
	loaded, err := h.disk.load(h.key)
	if err != nil || protocolHealthAttemptCount(loaded) != maxProtocolHealthAttempts {
		t.Fatal("refused work mutated the durable history", err)
	}
	expiry := s.HealthHistory[0].Request.Expires
	q = h.request(maxProtocolHealthAttempts+2, expiry)
	if err = r.beginProtocolHealthAttempt(q, expiry); err != nil {
		t.Fatal("safe expired history retirement failed", err)
	}
	loaded, err = h.disk.load(h.key)
	if err != nil || loaded.HealthRetiredThrough != expiry || protocolHealthAttemptCount(loaded) != 1 || loaded.Health.Request != q {
		t.Fatal("retirement and new intent were not atomic", err)
	}
	// Expiration alone cannot discard a successful proof awaiting the HUB.
	s.HealthHistory[0].Acknowledged = false
	retired := retireProtocolHealthAttempts(s, expiry)
	if len(retired.HealthHistory) != 1 || retired.HealthHistory[0].Request != s.HealthHistory[0].Request || retired.HealthHistory[0].Receipt == nil || retired.HealthHistory[0].Acknowledged {
		t.Fatal("expired unacknowledged proof was retired")
	}
}

func TestProtocolHealthTransportBoundsRepeatedDeliveryAndCancellation(t *testing.T) {
	t.Run("work", func(t *testing.T) {
		h := newHealthTransportHarness(t)
		q := h.request(1, h.base.Issued)
		p := h.proof(q)
		if err := h.disk.save(transactionState{Version: meteredTransactionJournalVersion, Health: &protocolHealthAttempt{Request: q, Receipt: &p, Acknowledged: true}}); err != nil {
			t.Fatal(err)
		}
		h.repeat = &q
		if err := h.sync(h.open(), func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
			t.Fatal("duplicate delivery executed another probe")
			return ProtocolHealthReceipt{}, ErrProtocolHealth
		}); err != nil {
			t.Fatal(err)
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.polls != maxProtocolHealthSyncWork || len(h.receipts) != maxProtocolHealthSyncWork {
			t.Fatal("duplicate deliveries were not bounded", h.polls, len(h.receipts))
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		h := newHealthTransportHarness(t)
		q := h.request(1, h.base.Issued)
		h.deliver(q)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := h.open()
		started := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- r.syncMeteredHealth(ctx, h.server.Client(), h.server.URL, func(ctx context.Context, _ ProtocolHealthRequest, _ ProtocolHealthEndpoint) (ProtocolHealthReceipt, error) {
				close(started)
				<-ctx.Done()
				return ProtocolHealthReceipt{}, ctx.Err()
			}, func() time.Time { return time.Unix(0, h.now.Load()) })
		}()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("probe never started")
		}
		// A verifier wait cannot hold the command/status mutex used by the
		// independent emergency worker.
		status := make(chan struct{})
		go func() { r.Status(); close(status) }()
		select {
		case <-status:
		case <-time.After(2 * time.Second):
			t.Fatal("health wait blocked the independent command/status path")
		}
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancellation was hidden")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("health sync blocked cancellation")
		}
		s, err := h.disk.load(h.key)
		if err != nil || s.Health == nil || s.Health.Request != q || s.Health.Receipt != nil {
			t.Fatal("cancellation lost its durable unknown attempt", err)
		}
	})
}

func TestProtocolHealthJournalUpgradePreservesEvidenceAndRefusesDowngrade(t *testing.T) {
	h := newHealthTransportHarness(t)
	q := h.request(1, h.base.Issued)
	p := h.proof(q)
	legacy := transactionState{Version: 2, Health: &protocolHealthAttempt{Request: q, Receipt: &p}}
	if err := h.disk.save(legacy); err != nil {
		t.Fatal(err)
	}
	s, err := h.disk.load(h.key)
	if err != nil || s.Version != meteredTransactionJournalVersion || s.Health == nil || s.Health.Request != q || *s.Health.Receipt != p || s.Health.Acknowledged || s.HealthRetiredThrough != 0 || len(s.HealthHistory) != 0 {
		t.Fatal("v2 journal upgrade changed pending evidence", err)
	}
	if err = h.disk.save(s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(h.disk.dir, "transactions.enc"))
	if err != nil {
		t.Fatal(err)
	}
	n := h.disk.aead.NonceSize()
	plain, err := h.disk.aead.Open(nil, raw[:n], raw[n:], h.disk.aad)
	if err != nil {
		t.Fatal(err)
	}
	var decoded transactionState
	if err = json.Unmarshal(plain, &decoded); err != nil || decoded.Version == 2 {
		t.Fatal("new saved journal could pass the old binary's exact v2 guard", err)
	}
	if bytes.Contains(raw, []byte(q.ID)) || bytes.Contains(raw, []byte(q.Signature)) {
		t.Fatal("migrated health evidence was saved in plaintext")
	}
	for _, mode := range []string{"legacy-history", "legacy-barrier", "negative-barrier", "duplicate", "overflow", "unknown-version", "nonmetered", "history-proof", "missing-proof", "conflicting-terminal"} {
		t.Run(mode, func(t *testing.T) {
			d := healthTransportDisk(t)
			bad := transactionState{Version: meteredTransactionJournalVersion, Health: &protocolHealthAttempt{Request: q, Receipt: &p}}
			switch mode {
			case "legacy-history":
				bad.Version = 2
				bad.HealthHistory = []protocolHealthAttempt{*bad.Health}
			case "legacy-barrier":
				bad.Version, bad.HealthRetiredThrough = 2, q.Expires
			case "negative-barrier":
				bad.HealthRetiredThrough = -1
			case "duplicate":
				bad.HealthHistory = []protocolHealthAttempt{*bad.Health}
			case "overflow":
				bad.HealthHistory = make([]protocolHealthAttempt, maxProtocolHealthAttempts)
			case "unknown-version":
				bad.Version = meteredTransactionJournalVersion + 1
			case "nonmetered":
				d.version, bad.Version = 1, 1
			case "history-proof":
				invalid := p
				invalid.Signature = strings.Repeat("0", 64)
				bad.Health = nil
				bad.HealthHistory = []protocolHealthAttempt{{Request: q, Receipt: &invalid}}
			case "missing-proof":
				bad.Health.Receipt, bad.Health.Acknowledged = nil, true
			case "conflicting-terminal":
				bad.Health.Acknowledged, bad.Health.RejectedAfterExpiry = true, true
			}
			if err := d.save(bad); err != nil {
				t.Fatal(err)
			}
			if _, err := d.load(h.key); err == nil {
				t.Fatal("invalid journal accepted")
			}
		})
	}
}
