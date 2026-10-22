//go:build linux

package managedxray

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/metered-health/receipt" {
			t.Error("replay polled/executed another probe")
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
		return &TransactionRuntime{metered: &MeteredTransactionOptions{}, journal: d, state: s, options: Options{LeaseKey: key, NodeID: q.NodeID, InstallationID: q.InstallationID}}
	}
	if err := open().SyncMeteredHealth(ctx, srv.Client(), srv.URL); err == nil {
		t.Fatal("lost reply hidden")
	}
	if err := open().SyncMeteredHealth(ctx, srv.Client(), srv.URL); err != nil {
		t.Fatal("restart receipt replay", err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
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
