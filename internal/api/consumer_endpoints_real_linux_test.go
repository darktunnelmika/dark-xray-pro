//go:build linux

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// This literal is assigned only inside scripts/test-hub-active-traffic.sh's
// isolated network namespace, with no main/default route and no host changes.
const consumerTrafficAddress = "11.0.0.2"

type consumerIngressRelay struct {
	port, origin uint16
	mu           sync.Mutex
	listener     net.Listener
	connections  map[net.Conn]bool
	started      bool
	closed       bool
	wg           sync.WaitGroup
	accepted     atomic.Int64
	up, down     atomic.Int64
}

func reserveConsumerIngressRelay(t *testing.T, origin uint16) *consumerIngressRelay {
	t.Helper()
	listener, err := net.Listen("tcp4", net.JoinHostPort(consumerTrafficAddress, "0"))
	if err != nil {
		t.Fatal("isolated consumer ingress address is unavailable", err)
	}
	r := &consumerIngressRelay{port: uint16(listener.Addr().(*net.TCPAddr).Port), origin: origin, listener: listener, connections: map[net.Conn]bool{}}
	t.Cleanup(r.close)
	return r
}

func (r *consumerIngressRelay) withdrawReservation(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.closed || r.listener == nil {
		t.Fatal("ingress reservation is not withdrawable")
	}
	r.listener.Close()
	r.listener = nil
}

func (r *consumerIngressRelay) start(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.closed {
		t.Fatal("ingress relay restarted")
	}
	if r.listener == nil {
		listener, err := net.Listen("tcp4", net.JoinHostPort(consumerTrafficAddress, strconv.Itoa(int(r.port))))
		if err != nil {
			t.Fatal("cannot restore exact synthetic ingress tuple", err)
		}
		r.listener = listener
	}
	r.started = true
	listener := r.listener
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			if r.closed || len(r.connections) >= 16 {
				r.mu.Unlock()
				client.Close()
				continue
			}
			r.connections[client] = true
			r.wg.Add(1)
			r.mu.Unlock()
			go r.forward(client)
		}
	}()
}

func (r *consumerIngressRelay) forward(client net.Conn) {
	defer r.wg.Done()
	defer client.Close()
	defer func() { r.mu.Lock(); delete(r.connections, client); r.mu.Unlock() }()
	_ = client.SetDeadline(time.Now().Add(45 * time.Second))
	origin, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(r.origin))), time.Second)
	if err != nil {
		return
	}
	defer origin.Close()
	_ = origin.SetDeadline(time.Now().Add(45 * time.Second))
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.connections[origin] = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.connections, origin); r.mu.Unlock() }()
	r.accepted.Add(1)
	var pumps sync.WaitGroup
	pumps.Add(2)
	go func() {
		defer pumps.Done()
		n, _ := io.Copy(origin, client)
		r.up.Add(n)
		_ = origin.(*net.TCPConn).CloseWrite()
	}()
	go func() {
		defer pumps.Done()
		n, _ := io.Copy(client, origin)
		r.down.Add(n)
		_ = client.(*net.TCPConn).CloseWrite()
	}()
	pumps.Wait()
}

func (r *consumerIngressRelay) close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		if r.listener != nil {
			r.listener.Close()
		}
		for connection := range r.connections {
			connection.Close()
		}
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func testConsumerManifestHealthRound(t *testing.T, ctx context.Context, f handoverHealthFixture, pin string) {
	t.Helper()
	if len(f.ConsumerApprovals) != 2 || len(f.ConsumerRelays) != 2 || len(f.Inbounds) != 2 {
		t.Fatal("consumer manifest fixture lost the complete independent ingress set")
	}
	members := make([]business.ConsumerManifestMember, 0, len(f.ConsumerApprovals))
	for i, approval := range f.ConsumerApprovals {
		if approval.NodeID != f.Plan.TargetNode || approval.InstallationID != f.Plan.TargetInstallation || approval.InboundID != f.Inbounds[i].ID || approval.Address != consumerTrafficAddress || approval.Port != f.ConsumerRelays[i].port || f.ConsumerRelays[i].origin != f.Inbounds[i].Port {
			t.Fatal("fixture approval and actual relay do not describe the same origin/ingress")
		}
		members = append(members, business.ConsumerManifestMember{InboundID: approval.InboundID, ConsumerAddress: approval.Address, ConsumerPort: approval.Port})
	}
	body := map[string]any{"event_id": handoverTestID(371), "action": "replace", "expected_revision": 0, "members": members, "confirmed": true}
	path := handoverBase + "/" + f.Plan.ID + "/consumer-manifest"
	post := func(want int) business.ConsumerManifest {
		t.Helper()
		w := browserRequest(t, f.Server.BrowserHandler(), http.MethodPost, path, body, f.Cookie, f.CSRF, "https://"+browserHost)
		var manifest business.ConsumerManifest
		if w.Code != want || json.Unmarshal(w.Body.Bytes(), &manifest) != nil {
			t.Fatal("owner consumer manifest response", w.Code, "want", want)
		}
		return manifest
	}
	manifest := post(http.StatusCreated)
	if manifest.ID == "" || manifest.EventID != handoverTestID(371) || manifest.Revision != 1 || manifest.State != "active" || !manifest.Current || len(manifest.Members) != 2 || manifest.ConsumerManifestSHA == "" {
		t.Fatal("owner manifest did not bind the complete applied candidate")
	}
	replay := post(http.StatusOK)
	if !replay.Replayed || replay.ID != manifest.ID || replay.Revision != manifest.Revision || replay.ConsumerManifestSHA != manifest.ConsumerManifestSHA || replay.CreatedUnixNano != manifest.CreatedUnixNano {
		t.Fatal("manifest retry changed identity, revision or content")
	}
	legacy := browserRequest(t, f.Server.BrowserHandler(), http.MethodPost, handoverBase+"/"+f.Plan.ID+"/health-rounds", map[string]any{"id": handoverTestID(372), "confirmed": true}, f.Cookie, f.CSRF, "https://"+browserHost)
	if legacy.Code != http.StatusConflict {
		t.Fatal("manifested route accepted a local-only health round", legacy.Code)
	}
	// Prove that an open origin alone is insufficient. The request remains a
	// valid, independently approved v3 tuple while its ingress is closed.
	f.ConsumerRelays[0].withdrawReservation(t)
	certificate := f.Origin.Certificate()
	certificateSHA := sha256.Sum256(certificate.Raw)
	endpoint := mx.ProtocolHealthEndpoint{Address: f.Origin.Listener.Addr().String(), ServerName: "example.com", CertificateSHA: hex.EncodeToString(certificateSHA[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})}
	credentialSHA := sha256.Sum256([]byte(f.Inbounds[0].Credential))
	status := f.Runtime.Status()
	if status.Usage == nil || !status.Observed.ProcessReady || status.CurrentStageID != f.Plan.TargetStageID || status.Observed.LeaseExpires < time.Now().Add(6*time.Second).UnixNano() {
		t.Fatal("closed-ingress test requires a healthy leased applied origin")
	}
	q, err := mx.SignProtocolHealthRequest([]byte(f.Token), mx.ProtocolHealthRequest{
		Version: mx.ConsumerProtocolHealthVersion, ID: handoverTestID(373), HandoverID: f.Plan.ID,
		NodeID: f.Plan.TargetNode, InstallationID: f.Plan.TargetInstallation, RuntimeInstance: status.Instance,
		StageID: f.Plan.TargetStageID, ApplyCommandID: handoverTestID(22),
		InboundID: f.Inbounds[0].ID, Port: f.Inbounds[0].Port, CredentialSHA: hex.EncodeToString(credentialSHA[:]), SystemProbeID: f.Inbounds[0].SystemProbeID,
		EndpointSHA: endpoint.SHA(), ConsumerManifestSHA: manifest.ConsumerManifestSHA, ConsumerAddress: members[0].ConsumerAddress, ConsumerPort: members[0].ConsumerPort,
		Expected: mx.Condition(status.Observed), Usage: *status.Usage, Issued: time.Now().UnixNano(), Expires: time.Now().Add(6 * time.Second).UnixNano(),
	})
	if err != nil {
		t.Fatal("closed-ingress challenge construction", err)
	}
	closedCtx, cancelClosed := context.WithTimeout(ctx, 800*time.Millisecond)
	proof, err := f.Runtime.ProbeMeteredProtocol(closedCtx, q, endpoint)
	cancelClosed()
	if err == nil || proof != (mx.ProtocolHealthReceipt{}) || f.Requests.Load() != 0 {
		t.Fatal("healthy origin shortcut a closed approved consumer ingress")
	}
	for _, relay := range f.ConsumerRelays {
		relay.start(t)
		relay := relay
		t.Cleanup(func() {
			relay.close()
			if relay.accepted.Load() < 2 || relay.up.Load() < 16<<10 || relay.down.Load() < 16<<10 {
				t.Error("real health and customer bytes did not cross every advertised consumer ingress", relay.accepted.Load(), relay.up.Load(), relay.down.Load())
			}
		})
	}
	testCompleteHandoverHealthRound(t, ctx, f, pin, manifest)
}
