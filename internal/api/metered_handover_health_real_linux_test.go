//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type handoverHealthInbound struct {
	ID            uint64
	Port          uint16
	Credential    string
	SystemProbeID string
}

type handoverHealthFixture struct {
	Server                *Server
	Cookie                *http.Cookie
	CSRF                  string
	Client                *http.Client
	Endpoint              string
	DropBefore, DropAfter *atomic.Bool
	Store                 *business.Store
	Key                   []byte
	Runtime               *mx.TransactionRuntime
	Token                 string
	Plan                  meteredHandoverInput
	Origin                *httptest.Server
	Port                  uint16
	Credential            string
	SystemProbeID         string
	CustomerCredential    string
	CustomerID            string
	Inbounds              []handoverHealthInbound
	Customers             []business.Client
	Behavior              *atomic.Int32
	Requests              *atomic.Int64
	Started               <-chan struct{}
	Release               chan<- struct{}
	Drain                 func()
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
	for _, mode := range []string{"verified-and-bound", "reject-wrong-verifier", "drain-during-probe", "owner-agent-lost-replies", "owner-agent-complete-round"} {
		t.Run(mode, func(t *testing.T) {
			inboundCount := 1
			if mode == "owner-agent-complete-round" {
				inboundCount = 2
			}
			testMeteredHandoverTrafficInbounds(t, binary, pin, "normal", inboundCount, func(f handoverHealthFixture) {
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
				case "owner-agent-complete-round":
					testCompleteHandoverHealthRound(t, ctx, f, pin)
				case "owner-agent-lost-replies":
					body := map[string]any{"id": handoverTestID(240), "inbound_id": 7612, "confirm_probe": true}
					path := handoverBase + "/" + f.Plan.ID + "/health"
					post := func() business.MeteredHealthIntent {
						t.Helper()
						w := browserRequest(t, f.Server.BrowserHandler(), "POST", path, body, f.Cookie, f.CSRF, "https://"+browserHost)
						if w.Code != 202 {
							t.Fatal("owner health queue", w.Code, w.Body.String())
						}
						var v business.MeteredHealthIntent
						if json.Unmarshal(w.Body.Bytes(), &v) != nil {
							t.Fatal("invalid queue reply")
						}
						return v
					}
					v := post()
					if again := post(); again != v {
						t.Fatal("owner retry replaced intent")
					}
					f.DropBefore.Store(true)
					if err := f.Runtime.SyncMeteredHealth(ctx, f.Client, f.Endpoint); err == nil {
						t.Fatal("lost precommit reply hidden")
					}
					if f.Requests.Load() != 1 {
						t.Fatal("probe count before commit", f.Requests.Load())
					}
					a, err := f.Store.MeteredHandoverAudit(ctx, f.Key, f.Plan.ID)
					if err != nil || a.HistoricalHealthEvidence != nil {
						t.Fatal("uncommitted receipt invented evidence", err)
					}
					f.DropAfter.Store(true)
					if err := f.Runtime.SyncMeteredHealth(ctx, f.Client, f.Endpoint); err == nil {
						t.Fatal("lost committed reply hidden")
					}
					a, err = f.Store.MeteredHandoverAudit(ctx, f.Key, f.Plan.ID)
					if err != nil || a.HistoricalHealthEvidence == nil || a.CanPublish {
						t.Fatal("HUB receipt not committed", err)
					}
					if err := f.Runtime.SyncMeteredHealth(ctx, f.Client, f.Endpoint); err != nil {
						t.Fatal("durable exact receipt replay", err)
					}
					if err := f.Runtime.SyncMeteredHealth(ctx, f.Client, f.Endpoint); err != nil {
						t.Fatal("empty health poll", err)
					}
					if f.Requests.Load() != 1 || f.Runtime.Status().Observed.SubscriptionPublished {
						t.Fatal("lost reply reran probe or published")
					}
				case "verified-and-bound":
					q, err := f.Store.IssueMeteredHealthChallenge(ctx, f.Key, []byte(f.Token), handoverTestID(101), f.Plan.ID, 7612, endpoint)
					if err != nil {
						t.Fatal("HUB-issued challenge", err)
					}
					p, err := f.Runtime.ProbeMeteredProtocol(ctx, q, endpoint)
					if err != nil || !mx.VerifyProtocolHealthReceipt([]byte(f.Token), q, p, time.Now()) || f.Requests.Load() != 1 {
						t.Fatal("real protocol proof", err)
					}
					bad := p
					bad.ResponseSHA = strings.Repeat("a", 64)
					if _, err := f.Store.ConsumeMeteredHealthProof(ctx, f.Key, []byte(f.Token), q.ID, endpoint, bad); err == nil {
						t.Fatal("HUB accepted tampered receipt")
					}
					evidence, err := f.Store.ConsumeMeteredHealthProof(ctx, f.Key, []byte(f.Token), q.ID, endpoint, p)
					if err != nil || evidence.Replayed || evidence.RequestSHA != p.RequestSHA {
						t.Fatal("HUB proof consumption", err)
					}
					again, err := f.Store.ConsumeMeteredHealthProof(ctx, f.Key, []byte(f.Token), q.ID, endpoint, p)
					if err != nil || !again.Replayed || again.Accepted != evidence.Accepted {
						t.Fatal("HUB lost-reply retry", err)
					}
					if _, err := f.Store.IssueMeteredHealthChallenge(ctx, f.Key, []byte(f.Token), handoverTestID(102), f.Plan.ID, 7612, endpoint); err == nil {
						t.Fatal("consumed operation reissued challenge")
					}
					view, err := f.Store.MeteredHandover(ctx, f.Plan.ID)
					if err != nil || view.CanPublish || view.Phase != "target_applied_health_required" {
						t.Fatal("HUB proof published subscription", err)
					}
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

// Record the Agent's actual outbound proof without changing its request,
// certificate pinning, or the receiving HUB's handling of that proof.
type handoverHealthRecordingTransport struct {
	http.RoundTripper
	mu       sync.Mutex
	receipts []mx.ProtocolHealthSubmission
}

func (t *handoverHealthRecordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/api/v1/agent/metered-health/receipt" {
		if r.GetBody == nil {
			return nil, fmt.Errorf("health fixture requires a replayable receipt body")
		}
		body, err := r.GetBody()
		if err != nil {
			return nil, err
		}
		var proof mx.ProtocolHealthSubmission
		err = json.NewDecoder(body).Decode(&proof)
		body.Close()
		if err != nil {
			return nil, err
		}
		t.mu.Lock()
		t.receipts = append(t.receipts, proof)
		t.mu.Unlock()
	}
	return t.RoundTripper.RoundTrip(r)
}

func (t *handoverHealthRecordingTransport) saved() []mx.ProtocolHealthSubmission {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]mx.ProtocolHealthSubmission(nil), t.receipts...)
}

func testCompleteHandoverHealthRound(t *testing.T, ctx context.Context, f handoverHealthFixture, pin string) {
	t.Helper()
	if len(f.Inbounds) != 2 || f.Inbounds[0].ID != 7612 || f.Inbounds[1].ID != 7613 || f.Inbounds[0].Port == f.Inbounds[1].Port || f.Inbounds[0].SystemProbeID == f.Inbounds[1].SystemProbeID {
		t.Fatal("round requires two distinct destination inbounds and probe identities")
	}
	beforeClients := make(map[string]business.Client)
	beforeUsage := make(map[string]usageledger.Account)
	for _, customer := range f.Customers {
		c, err := f.Store.Client(ctx, customer.ID)
		if err != nil {
			t.Fatal(err)
		}
		u, err := f.Store.ClientUsage(ctx, customer.ID)
		if err != nil {
			t.Fatal(err)
		}
		beforeClients[c.ID], beforeUsage[c.ID] = c, u
	}
	postOwner := func() business.MeteredHealthRound {
		t.Helper()
		body := map[string]any{"id": handoverTestID(250), "confirmed": true}
		w := browserRequest(t, f.Server.BrowserHandler(), "POST", handoverBase+"/"+f.Plan.ID+"/health-rounds", body, f.Cookie, f.CSRF, "https://"+browserHost)
		var round business.MeteredHealthRound
		if w.Code != http.StatusAccepted || json.Unmarshal(w.Body.Bytes(), &round) != nil {
			t.Fatal("owner complete-inbound round", w.Code, w.Body.String())
		}
		return round
	}
	round := postOwner()
	if again := postOwner(); !reflect.DeepEqual(round, again) {
		t.Fatal("owner retry replaced the immutable health round")
	}
	if round.ID != handoverTestID(250) || round.HandoverID != f.Plan.ID || round.NodeID != f.Plan.TargetNode || round.InstallationID != f.Plan.TargetInstallation || round.Current || round.State != "pending" || round.Expires <= round.Created || round.Expires-round.Created > int64(8*time.Second) || round.Expires > f.Runtime.Status().Observed.LeaseExpires || len(round.Members) != 2 || f.Requests.Load() != 0 {
		t.Fatal("owner round did not bind a complete bounded destination set", round)
	}
	for i, m := range round.Members {
		if m.InboundID != f.Inbounds[i].ID || m.Port != f.Inbounds[i].Port || m.State != "pending" || m.ChallengeID != "" || m.Accepted != 0 || m.IntentID == "" || (i > 0 && m.IntentID == round.Members[i-1].IntentID) {
			t.Fatal("owner selected/omitted a required destination member", m)
		}
	}
	audit := func() business.MeteredHealthRound {
		t.Helper()
		w := browserRequest(t, f.Server.BrowserHandler(), "GET", handoverBase+"/"+f.Plan.ID, nil, f.Cookie, "", "")
		var v business.MeteredHandoverAudit
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.CanPublish || v.Phase != "target_applied_health_required" || v.HistoricalHealthEvidence != nil || len(v.HealthRounds) != 1 {
			t.Fatal("owner round audit/publication boundary", w.Code, w.Body.String())
		}
		r := v.HealthRounds[0]
		if r.ID != round.ID || r.ManifestSHA != round.ManifestSHA || r.Created != round.Created || r.Expires != round.Expires || len(r.Members) != 2 {
			t.Fatal("health proof changed immutable round coverage/deadline")
		}
		for i, m := range r.Members {
			if m.InboundID != round.Members[i].InboundID || m.Port != round.Members[i].Port || m.IntentID != round.Members[i].IntentID {
				t.Fatal("health proof changed a required member")
			}
		}
		return r
	}
	postAgent := func(path string, body any, want int, out any) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequestWithContext(ctx, "POST", f.Endpoint+"/api/v1/agent/metered-health/"+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+f.Token)
		r.Header.Set("Content-Type", "application/json")
		res, err := f.Client.Do(r)
		if err != nil {
			t.Fatal("pinned Agent request", err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatal("Agent health status", path, res.StatusCode, want)
		}
		if out != nil && json.NewDecoder(io.LimitReader(res.Body, 128<<10)).Decode(out) != nil {
			t.Fatal("invalid Agent delivery")
		}
	}
	recorded := &handoverHealthRecordingTransport{RoundTripper: f.Client.Transport}
	client := *f.Client
	client.Transport = recorded
	// Stop exactly after the first real proof commits, before the Agent can
	// poll/probe the second member. Its pending outbox must survive the loss.
	f.DropAfter.Store(true)
	if err := f.Runtime.SyncMeteredHealth(ctx, &client, f.Endpoint); err == nil {
		t.Fatal("lost committed first-member reply was hidden")
	}
	proofs := recorded.saved()
	if f.Requests.Load() != 1 || len(proofs) != 1 {
		t.Fatal("partial round did not execute exactly one actual probe", f.Requests.Load(), len(proofs))
	}
	partial := audit()
	if partial.Current || partial.State != "pending" || partial.Members[0].State != "accepted" || partial.Members[0].Accepted == 0 || partial.Members[0].ChallengeID != proofs[0].ChallengeID || partial.Members[1].State != "pending" || partial.Members[1].Accepted != 0 {
		t.Fatal("one member incorrectly completed the all-inbound round", partial)
	}
	// Issue member two through the authenticated Agent poll without executing
	// its probe. Member one's proof cannot satisfy this different challenge.
	poll := mx.SignMeteredTransactionPoll([]byte(f.Token), mx.TransactionPoll{NodeID: f.Plan.TargetNode, InstallationID: f.Plan.TargetInstallation, BinarySHA: pin, At: time.Now().UnixNano(), Status: f.Runtime.Status()})
	var next mx.ProtocolHealthDelivery
	postAgent("poll", poll, http.StatusOK, &next)
	if next.Request.InboundID != f.Inbounds[1].ID || next.Request.Port != f.Inbounds[1].Port || next.Request.ID == proofs[0].ChallengeID || next.Request.Expires > round.Expires || next.Request.Expires-next.Request.Issued > int64(8*time.Second) {
		t.Fatal("second member delivery lost exact coverage/deadline")
	}
	postAgent("receipt", mx.ProtocolHealthSubmission{ChallengeID: next.Request.ID, Receipt: proofs[0].Receipt}, http.StatusConflict, nil)
	postAgent("receipt", proofs[0], http.StatusNoContent, nil)
	stillPartial := audit()
	if stillPartial.Current || stillPartial.State != "pending" || stillPartial.Members[0] != partial.Members[0] || stillPartial.Members[1].State != "pending" || stillPartial.Members[1].Accepted != 0 || f.Requests.Load() != 1 {
		t.Fatal("wrong/replayed proof refreshed evidence, completed the round, or generated traffic")
	}
	// The Agent first resends its unchanged saved proof, then immediately runs
	// the remaining member once and commits that independent nonce receipt.
	if err := f.Runtime.SyncMeteredHealth(ctx, &client, f.Endpoint); err != nil {
		t.Fatal("Agent did not finish the complete-inbound round", err)
	}
	proofs = recorded.saved()
	if len(proofs) != 3 || proofs[0] != proofs[1] || proofs[2].ChallengeID != next.Request.ID || f.Requests.Load() != 2 {
		t.Fatal("lost ACK replay repeated traffic or changed its saved proof", len(proofs), f.Requests.Load())
	}
	complete := audit()
	if !complete.Current || complete.State != "complete" || complete.Members[0] != partial.Members[0] || complete.Members[1].State != "accepted" || complete.Members[1].Accepted == 0 || complete.Members[1].ChallengeID != next.Request.ID {
		t.Fatal("two real member proofs did not produce current complete evidence", complete)
	}
	postAgent("receipt", proofs[2], http.StatusNoContent, nil)
	if err := f.Runtime.SyncMeteredHealth(ctx, &client, f.Endpoint); err != nil || f.Requests.Load() != 2 || f.Runtime.Status().Observed.SubscriptionPublished || f.Runtime.Status().Observed.DataPlaneVerified {
		t.Fatal("completed round replay reexecuted a probe or granted publication", err)
	}
	if replay := audit(); !reflect.DeepEqual(complete, replay) {
		t.Fatal("exact complete-round replay changed its accepted evidence")
	}
	for id, before := range beforeClients {
		after, err := f.Store.Client(ctx, id)
		if err != nil || after != before {
			t.Fatal("health round changed customer identity, plan, expiry or charged quota", id, err)
		}
		u, err := f.Store.ClientUsage(ctx, id)
		b := beforeUsage[id]
		if err != nil || u.Limit != b.Limit || u.Plan != b.Plan || u.Holds != b.Holds || u.Known != b.Known || u.Uplink < b.Uplink || u.Downlink < b.Downlink || (id != f.CustomerID && u != b) {
			t.Fatal("health round altered customer accounting", id, err)
		}
	}
	// The surrounding fixture then drains both listeners, proves exact final
	// customer accounting, and requires nonzero traffic on BOTH target probes
	// while BOTH unused source probes remain zero and lack financial accounts.
}
