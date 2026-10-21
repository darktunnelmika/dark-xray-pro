//go:build linux

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

type handoverHealthFixture struct {
	Runtime            *mx.TransactionRuntime
	Token              string
	Plan               meteredHandoverInput
	Origin             *httptest.Server
	Port               uint16
	Credential         string
	SystemProbeID      string
	CustomerCredential string
	CustomerID         string
	Behavior           *atomic.Int32
	Requests           *atomic.Int64
	Started            <-chan struct{}
	Release            chan<- struct{}
	Drain              func()
}

func TestRealPinnedMeteredHandoverProtocolHealth(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_HUB_TRAFFIC") != "1" {
		t.Skip("explicit isolated-network HUB traffic acceptance required")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	interfaces, err := net.Interfaces()
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" || !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 || err != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatal("nonroot pinned core in isolated loopback namespace required")
	}
	for _, mode := range []string{"verified-and-bound", "reject-wrong-verifier", "drain-during-probe"} {
		t.Run(mode, func(t *testing.T) {
			testMeteredHandoverTraffic(t, binary, pin, "normal", func(f handoverHealthFixture) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				cert := f.Origin.Certificate()
				h := sha256.Sum256(cert.Raw)
				endpoint := mx.ProtocolHealthEndpoint{Address: f.Origin.Listener.Addr().String(), ServerName: "example.com", CertificateSHA: hex.EncodeToString(h[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})}
				credential := sha256.Sum256([]byte(f.Credential))
				request := func(n int, e mx.ProtocolHealthEndpoint) mx.ProtocolHealthRequest {
					t.Helper()
					status := f.Runtime.Status()
					q, err := mx.SignProtocolHealthRequest([]byte(f.Token), mx.ProtocolHealthRequest{Version: mx.ProtocolHealthVersion, SystemProbeID: f.SystemProbeID, ID: handoverTestID(100 + n), HandoverID: f.Plan.ID, NodeID: f.Plan.TargetNode, InstallationID: f.Plan.TargetInstallation, RuntimeInstance: status.Instance, StageID: f.Plan.TargetStageID, ApplyCommandID: handoverTestID(22), InboundID: 7612, Port: f.Port, CredentialSHA: hex.EncodeToString(credential[:]), EndpointSHA: e.SHA(), Expected: mx.Condition(status.Observed), Usage: *status.Usage, Issued: time.Now().UnixNano(), Expires: time.Now().Add(6 * time.Second).UnixNano()})
					if err != nil {
						t.Fatal(err)
					}
					return q
				}
				reject := func(q mx.ProtocolHealthRequest, e mx.ProtocolHealthEndpoint) {
					t.Helper()
					p, err := f.Runtime.ProbeMeteredProtocol(ctx, q, e)
					if err == nil || p != (mx.ProtocolHealthReceipt{}) {
						t.Fatal("invalid probe produced health evidence", err)
					}
				}
				switch mode {
				case "verified-and-bound":
					q := request(1, endpoint)
					p, err := f.Runtime.ProbeMeteredProtocol(ctx, q, endpoint)
					if err != nil || !mx.VerifyProtocolHealthReceipt([]byte(f.Token), q, p, time.Now()) || f.Requests.Load() != 1 {
						t.Fatal("real protocol proof", err)
					}
					bad := p
					bad.ResponseSHA = strings.Repeat("a", 64)
					if mx.VerifyProtocolHealthReceipt([]byte(f.Token), q, bad, time.Now()) {
						t.Fatal("tampered proof accepted")
					}
					wrongKey := sha256.Sum256([]byte("independent protocol health verifier test key"))
					if string(wrongKey[:]) == f.Token {
						t.Fatal("wrong-key fixture reused the target key")
					}
					if mx.VerifyProtocolHealthReceipt(wrongKey[:], q, p, time.Now()) {
						t.Fatal("proof accepted under an independent key")
					}
					if mx.VerifyProtocolHealthReceipt([]byte(f.Token), q, p, time.Unix(0, q.Expires)) {
						t.Fatal("forged/expired proof accepted")
					}
					changed := q
					changed.HandoverID = handoverTestID(77)
					changed, _ = mx.SignProtocolHealthRequest([]byte(f.Token), changed)
					if mx.VerifyProtocolHealthReceipt([]byte(f.Token), changed, p, time.Now()) {
						t.Fatal("proof moved to another operation")
					}
					for i, mutate := range []func(*mx.ProtocolHealthRequest){
						func(q *mx.ProtocolHealthRequest) { q.RuntimeInstance = handoverTestID(88) },
						func(q *mx.ProtocolHealthRequest) { q.Usage.EpochID = handoverTestID(89) },
						func(q *mx.ProtocolHealthRequest) { q.ApplyCommandID = handoverTestID(90) },
						func(q *mx.ProtocolHealthRequest) { q.CredentialSHA = strings.Repeat("b", 64) },
						func(q *mx.ProtocolHealthRequest) { q.InboundID = 7613 },
					} {
						bad := request(10+i, endpoint)
						mutate(&bad)
						bad, _ = mx.SignProtocolHealthRequest([]byte(f.Token), bad)
						reject(bad, endpoint)
					}
					if f.Requests.Load() != 1 {
						t.Fatal("stale request reached verifier")
					}
					customerHash := sha256.Sum256([]byte(f.CustomerCredential))
					badCustomer := request(30, endpoint)
					badCustomer.CredentialSHA = hex.EncodeToString(customerHash[:])
					badCustomer, _ = mx.SignProtocolHealthRequest([]byte(f.Token), badCustomer)
					reject(badCustomer, endpoint)
					badCustomer.SystemProbeID = f.CustomerID
					badCustomer, _ = mx.SignProtocolHealthRequest([]byte(f.Token), badCustomer)
					reject(badCustomer, endpoint)
					if f.Requests.Load() != 1 {
						t.Fatal("financial customer credential reached health verifier")
					}
					raw, _ := json.Marshal(p)
					if strings.Contains(string(raw), f.Credential) || strings.Contains(string(raw), "privateKey") {
						t.Fatal("private credential in receipt")
					}
					status := f.Runtime.Status()
					if status.Observed.DataPlaneVerified || status.Observed.SubscriptionPublished {
						t.Fatal("probe silently granted publication authority")
					}
				case "reject-wrong-verifier":
					f.Behavior.Store(1)
					reject(request(1, endpoint), endpoint)
					f.Behavior.Store(2)
					reject(request(2, endpoint), endpoint)
					if f.Requests.Load() != 2 {
						t.Fatal("redirect followed or probe bypassed")
					}
					f.Behavior.Store(0)
					bad := endpoint
					bad.CertificateSHA = strings.Repeat("a", 64)
					reject(request(3, bad), bad)
					bad = endpoint
					bad.ServerName = "wrong.example.org"
					reject(request(4, bad), bad)
					if f.Requests.Load() != 2 {
						t.Fatal("untrusted TLS verifier got request")
					}
				case "drain-during-probe":
					f.Behavior.Store(3)
					q := request(1, endpoint)
					type result struct {
						p   mx.ProtocolHealthReceipt
						err error
					}
					done := make(chan result, 1)
					go func() { p, err := f.Runtime.ProbeMeteredProtocol(ctx, q, endpoint); done <- result{p, err} }()
					select {
					case <-f.Started:
					case <-ctx.Done():
						t.Fatal("probe did not reach verifier")
					}
					// Drain must complete while the verifier response is still held.
					f.Drain()
					close(f.Release)
					select {
					case got := <-done:
						if got.err == nil || got.p != (mx.ProtocolHealthReceipt{}) {
							t.Fatal("stopped epoch produced proof")
						}
					case <-ctx.Done():
						t.Fatal("probe did not stop")
					}
					reject(q, endpoint)
				}
			})
		})
	}
}
