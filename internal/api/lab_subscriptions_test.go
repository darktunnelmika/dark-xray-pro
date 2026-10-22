package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

const labSubscriptionClientPath = "/api/v1/workspace/clients/handover-customer/subscription"

// Model a consumer that stops reading its HTTP body. Its socket backpressure
// must not retain the shared health/replacement lifecycle lock.
type labSubscriptionSlowWriter struct {
	header          http.Header
	status          int
	entered         chan struct{}
	release         chan struct{}
	once            sync.Once
	lifecycle       *sync.Mutex
	headerAvailable bool
	bodyAvailable   bool
}

func (w *labSubscriptionSlowWriter) Header() http.Header { return w.header }
func (w *labSubscriptionSlowWriter) lifecycleAvailable() bool {
	if !w.lifecycle.TryLock() {
		return false
	}
	w.lifecycle.Unlock()
	return true
}
func (w *labSubscriptionSlowWriter) WriteHeader(status int) {
	w.status = status
	w.headerAvailable = w.lifecycleAvailable()
}
func (w *labSubscriptionSlowWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	w.bodyAvailable = w.lifecycleAvailable()
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func newLabSubscriptionAPI(t *testing.T) *handoverAPI {
	t.Helper()
	f := newHandoverAPI(t)
	if err := f.s.EnableMeteredHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableMeteredHealthRounds(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableLabSubscriptions(); err != nil {
		t.Fatal(err)
	}
	return f
}

func labSubscriptionDecode(t *testing.T, w *httptest.ResponseRecorder, want int, out any) {
	t.Helper()
	if w.Code != want {
		t.Fatal("LAB subscription HTTP status", w.Code, "want", want)
	}
	if out != nil && json.Unmarshal(w.Body.Bytes(), out) != nil {
		t.Fatal("LAB subscription reply was not the documented JSON")
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("subscription metadata or secret was cacheable")
	}
}

func labSubscriptionNoSecret(t *testing.T, w *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	for _, field := range []string{`"token":`, `"path":`, `"uuid":`, `"privateKey":`, `"payload_enc":`, `"config_base64":`, "vless://"} {
		if strings.Contains(w.Body.String(), field) {
			t.Fatal("subscription metadata/mutation leaked a secret field")
		}
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(w.Body.String(), secret) {
			t.Fatal("subscription metadata/mutation exposed private material")
		}
	}
}

func labSubscriptionReveal(t *testing.T, f *handoverAPI, path string, generation uint64, cookie *http.Cookie, csrf string) (string, string) {
	t.Helper()
	w := browserRequest(t, f.s.BrowserHandler(), http.MethodPost, path+"/reveal", map[string]any{"expected_generation": generation}, cookie, csrf, "https://"+browserHost)
	var out struct {
		business.SubscriptionSecret
		Path string `json:"path"`
	}
	labSubscriptionDecode(t, w, http.StatusOK, &out)
	raw, err := base64.RawURLEncoding.Strict().DecodeString(out.Token)
	if err != nil || len(raw) != 32 || out.Generation != generation || out.ClientID == "" || out.Path != "/api/v1/subscriptions/"+out.Token || strings.Contains(out.Path, browserHost) {
		t.Fatal("explicit reveal did not return a canonical capability and relative path")
	}
	return out.Token, out.Path
}

func TestLabSubscriptionsStartupAndDefaultOff(t *testing.T) {
	f := newHandoverAPI(t)
	if err := f.s.EnableLabSubscriptions(); err == nil || f.s.labSubscriptionsEnabled {
		t.Fatal("subscription delivery enabled without health transport")
	}
	if err := f.s.EnableMeteredHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableLabSubscriptions(); err == nil || f.s.labSubscriptionsEnabled {
		t.Fatal("subscription delivery enabled without complete-round support")
	}
	if err := f.s.EnableMeteredHealthRounds(); err != nil {
		t.Fatal(err)
	}
	metadata := func() map[string]json.RawMessage {
		w := browserRequest(t, f.s.BrowserHandler(), http.MethodGet, labSubscriptionClientPath, nil, f.cookie, "", "")
		var out map[string]json.RawMessage
		labSubscriptionDecode(t, w, http.StatusOK, &out)
		labSubscriptionNoSecret(t, w, f.tokens...)
		return out
	}
	if v := metadata(); string(v["enabled"]) != "false" || string(v["scope"]) != `"lab_single_handover"` || v["snapshot"] != nil {
		t.Fatal("N17 implicitly advertised subscription delivery")
	}
	body := map[string]any{"event_id": handoverTestID(500), "expected_generation": 0}
	labSubscriptionDecode(t, f.post(t, labSubscriptionClientPath+"/issue", body), http.StatusConflict, nil)
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	labSubscriptionDecode(t, request(t, f.s.BrowserHandler(), http.MethodGet, "/api/v1/subscriptions/"+token, "", nil), http.StatusNotFound, nil)
	if err := f.s.EnableLabSubscriptions(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableLabSubscriptions(); err == nil {
		t.Fatal("startup subscription schema switch mutated twice")
	}
	if v := metadata(); string(v["enabled"]) != "true" || string(v["state"]) != `"absent"` || string(v["generation"]) != "0" || v["snapshot"] != nil {
		t.Fatal("explicit startup created a token, snapshot or readiness state")
	}
	capability := browserRequest(t, f.s.BrowserHandler(), http.MethodGet, "/api/v1/workspace/metered-route-transactions", nil, f.cookie, "", "")
	if capability.Code != http.StatusOK || !strings.Contains(capability.Body.String(), `"lab_subscriptions_enabled":true`) {
		t.Fatal("owner discovery omitted the independent LAB opt-in")
	}
}

func TestLabSubscriptionsOwnerCSRFAndSecretLifecycle(t *testing.T) {
	f := newLabSubscriptionAPI(t)
	h := f.s.BrowserHandler()
	base := labSubscriptionClientPath
	body := map[string]any{"event_id": handoverTestID(501), "expected_generation": 0}
	for _, tc := range []struct{ method, path string }{{http.MethodGet, base}, {http.MethodPost, base + "/issue"}, {http.MethodPost, base + "/reveal"}} {
		labSubscriptionDecode(t, browserRequest(t, h, tc.method, tc.path, body, nil, "", ""), http.StatusUnauthorized, nil)
		labSubscriptionDecode(t, request(t, h, tc.method, tc.path, ownerToken, body), http.StatusUnauthorized, nil)
	}
	for _, action := range []string{"issue", "rotate", "revoke", "reveal"} {
		for _, headers := range [][2]string{{"", "https://" + browserHost}, {"bad", "https://" + browserHost}, {f.csrf, "https://foreign.invalid"}} {
			labSubscriptionDecode(t, browserRequest(t, h, http.MethodPost, base+"/"+action, body, f.cookie, headers[0], headers[1]), http.StatusForbidden, nil)
		}
	}
	var issued business.SubscriptionTokenReceipt
	w := f.post(t, base+"/issue", body)
	labSubscriptionDecode(t, w, http.StatusOK, &issued)
	labSubscriptionNoSecret(t, w, f.tokens...)
	if issued.ClientID != "handover-customer" || issued.Generation != 1 || issued.State != "active" || issued.Replayed || issued.Superseded {
		t.Fatal("token issuance receipt changed client scope or generation")
	}
	w = f.post(t, base+"/issue", body)
	var again business.SubscriptionTokenReceipt
	labSubscriptionDecode(t, w, http.StatusOK, &again)
	labSubscriptionNoSecret(t, w)
	if !again.Replayed || again.Superseded || again.Generation != issued.Generation {
		t.Fatal("exact issue retry created another credential")
	}
	secret, publicPath := labSubscriptionReveal(t, f, base, 1, f.cookie, f.csrf)
	if second, secondPath := labSubscriptionReveal(t, f, base, 1, f.cookie, f.csrf); second != secret || secondPath != publicPath {
		t.Fatal("reveal changed a stable capability")
	}
	w = browserRequest(t, h, http.MethodGet, base, nil, f.cookie, "", "")
	var metadata business.LabSubscriptionView
	labSubscriptionDecode(t, w, http.StatusOK, &metadata)
	labSubscriptionNoSecret(t, w, secret)
	if metadata.Generation != 1 || metadata.State != "active" || metadata.Snapshot != nil {
		t.Fatal("token alone materialized consumer content")
	}
	labSubscriptionDecode(t, request(t, h, http.MethodGet, publicPath, "", nil), http.StatusNotFound, nil)
	rotate := map[string]any{"event_id": handoverTestID(502), "expected_generation": 1}
	w = f.post(t, base+"/rotate", rotate)
	var rotated business.SubscriptionTokenReceipt
	labSubscriptionDecode(t, w, http.StatusOK, &rotated)
	labSubscriptionNoSecret(t, w, secret)
	if rotated.Generation != 2 || rotated.State != "active" {
		t.Fatal("rotation did not advance the active credential generation")
	}
	labSubscriptionDecode(t, f.post(t, base+"/reveal", map[string]any{"expected_generation": 1}), http.StatusConflict, nil)
	current, _ := labSubscriptionReveal(t, f, base, 2, f.cookie, f.csrf)
	if current == secret {
		t.Fatal("token rotation reused a revoked capability")
	}
	w = f.post(t, base+"/issue", body)
	labSubscriptionDecode(t, w, http.StatusOK, &again)
	labSubscriptionNoSecret(t, w, secret, current)
	if !again.Replayed || !again.Superseded || again.Generation != 1 {
		t.Fatal("superseded retry restored the old token generation")
	}
	w = f.post(t, base+"/revoke", map[string]any{"event_id": handoverTestID(503), "expected_generation": 2})
	var revoked business.SubscriptionTokenReceipt
	labSubscriptionDecode(t, w, http.StatusOK, &revoked)
	labSubscriptionNoSecret(t, w, secret, current)
	if revoked.Generation != 3 || revoked.State != "revoked" {
		t.Fatal("revoke did not persist a new terminal capability generation")
	}
	labSubscriptionDecode(t, f.post(t, base+"/reveal", map[string]any{"expected_generation": 3}), http.StatusNotFound, nil)
	labSubscriptionDecode(t, request(t, h, http.MethodGet, "/api/v1/subscriptions/"+current, "", nil), http.StatusNotFound, nil)
}

func TestLabSubscriptionsRepresentativeScopeAndPermissions(t *testing.T) {
	f := newLabSubscriptionAPI(t)
	ctx := context.Background()
	for _, rep := range []string{"subscription-rep-a", "subscription-rep-b"} {
		if _, err := f.s.business.CreateRepresentative(ctx, rep, rep); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.business.AdjustCredits(ctx, business.CreditChange{EventID: "subscription-credit-" + rep, RepresentativeID: rep, VolumeDeltaBytes: 2 << 20, Reason: "Synthetic subscription RBAC"}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.business.CreateClient(ctx, business.NewClient{EventID: "subscription-client-" + rep, ClientID: "client-" + rep, RepresentativeID: rep, Name: rep, DurationDays: 30, VolumeBytes: 1 << 20}); err != nil {
			t.Fatal(err)
		}
	}
	const repID = "subscription-rep-a"
	if _, err := f.s.business.ProvisionRepresentativeLogin(ctx, repID, repID, repPassword); err != nil {
		t.Fatal(err)
	}
	rep, csrf := browserLogin(t, f.s, repID, repPassword)
	h := f.s.BrowserHandler()
	base := "/api/v1/workspace/clients/client-" + repID + "/subscription"
	body := map[string]any{"event_id": handoverTestID(510), "expected_generation": 0}
	for _, path := range []string{labSubscriptionClientPath, "/api/v1/workspace/clients/client-subscription-rep-b/subscription"} {
		labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, path, nil, rep, "", ""), http.StatusForbidden, nil)
		for _, action := range []string{"issue", "reveal", "rotate", "revoke"} {
			labSubscriptionDecode(t, browserRequest(t, h, http.MethodPost, path+"/"+action, body, rep, csrf, "https://"+browserHost), http.StatusForbidden, nil)
		}
	}
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, base, nil, rep, "", ""), http.StatusOK, nil)
	w := browserRequest(t, h, http.MethodPost, base+"/issue", body, rep, csrf, "https://"+browserHost)
	labSubscriptionDecode(t, w, http.StatusOK, nil)
	labSubscriptionNoSecret(t, w)
	secret, _ := labSubscriptionReveal(t, f, base, 1, rep, csrf)
	materialize := handoverBase + "/" + f.plan.ID + "/lab-subscription-snapshot"
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodPost, materialize, map[string]any{"event_id": handoverTestID(511), "client_id": "client-" + repID, "round_id": handoverTestID(512), "expected_revision": 0}, rep, csrf, "https://"+browserHost), http.StatusForbidden, nil)
	rights := business.DefaultRepresentativePermissions()
	rights.ManageClients = false
	change := business.RepresentativeControlChange{EventID: "subscription-restrict-manage-01", RepresentativeID: repID, ExpectedRevision: 0, DisplayName: repID, Permissions: rights, Reason: "Exercise independent subscription secret permission"}
	if _, err := f.s.business.UpdateRepresentativeControl(ctx, change); err != nil {
		t.Fatal(err)
	}
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, base, nil, rep, "", ""), http.StatusUnauthorized, nil)
	rep, csrf = browserLogin(t, f.s, repID, repPassword)
	w = browserRequest(t, h, http.MethodGet, base, nil, rep, "", "")
	labSubscriptionDecode(t, w, http.StatusOK, nil)
	labSubscriptionNoSecret(t, w, secret)
	for _, action := range []string{"issue", "reveal", "rotate", "revoke"} {
		labSubscriptionDecode(t, browserRequest(t, h, http.MethodPost, base+"/"+action, map[string]any{"event_id": handoverTestID(513), "expected_generation": 1}, rep, csrf, "https://"+browserHost), http.StatusForbidden, nil)
	}
	change.EventID, change.ExpectedRevision = "subscription-restrict-view-001", 1
	change.Permissions.ViewClients = false
	if _, err := f.s.business.UpdateRepresentativeControl(ctx, change); err != nil {
		t.Fatal(err)
	}
	rep, _ = browserLogin(t, f.s, repID, repPassword)
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, base, nil, rep, "", ""), http.StatusForbidden, nil)
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, base, nil, f.cookie, "", ""), http.StatusOK, nil)
	if ownerSecret, _ := labSubscriptionReveal(t, f, base, 1, f.cookie, f.csrf); ownerSecret != secret {
		t.Fatal("representative permission update changed the owner's retained credential")
	}
}

func TestLabSubscriptionsStrictHTTPAndAgentBoundaries(t *testing.T) {
	f := newLabSubscriptionAPI(t)
	h := f.s.BrowserHandler()
	base := labSubscriptionClientPath
	for _, event := range []string{"", "not-an-event", strings.Repeat("A", 32), strings.Repeat("a", 31), strings.Repeat("a", 33)} {
		labSubscriptionDecode(t, f.post(t, base+"/issue", map[string]any{"event_id": event, "expected_generation": 0}), http.StatusUnprocessableEntity, nil)
	}
	for _, body := range []map[string]any{
		{"event_id": handoverTestID(520), "expected_generation": -1},
		{"event_id": handoverTestID(520), "expected_generation": 0, "token": "caller-selected"},
		{"event_id": handoverTestID(520), "expected_generation": 0, "client_id": "another-client"},
	} {
		labSubscriptionDecode(t, f.post(t, base+"/issue", body), http.StatusUnprocessableEntity, nil)
	}
	for _, body := range []map[string]any{{"expected_generation": 0}, {"expected_generation": 1, "token": "caller-selected"}} {
		labSubscriptionDecode(t, f.post(t, base+"/reveal", body), http.StatusUnprocessableEntity, nil)
	}
	materialize := handoverBase + "/" + f.plan.ID + "/lab-subscription-snapshot"
	for _, body := range []map[string]any{
		{"event_id": strings.Repeat("A", 32), "client_id": "handover-customer", "round_id": handoverTestID(521), "expected_revision": 0},
		{"event_id": handoverTestID(520), "client_id": "handover-customer", "round_id": "invalid", "expected_revision": 0},
		{"event_id": handoverTestID(520), "client_id": "handover-customer", "round_id": handoverTestID(521), "expected_revision": -1},
		{"event_id": handoverTestID(520), "client_id": "handover-customer", "round_id": handoverTestID(521), "expected_revision": 0, "endpoint": "caller-selected"},
	} {
		labSubscriptionDecode(t, f.post(t, materialize, body), http.StatusUnprocessableEntity, nil)
	}
	labSubscriptionDecode(t, f.post(t, materialize+"?publish=true", map[string]any{}), http.StatusUnprocessableEntity, nil)
	body := map[string]any{"event_id": handoverTestID(520), "expected_generation": 0}
	labSubscriptionDecode(t, f.post(t, base+"/issue?format=raw", body), http.StatusUnprocessableEntity, nil)
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, base+"?include=token", nil, f.cookie, "", ""), http.StatusUnprocessableEntity, nil)
	for _, raw := range []string{`{"event_id":"` + handoverTestID(520) + `","expected_generation":0} {}`, `{"event_id":"` + strings.Repeat("a", 600) + `","expected_generation":0}`} {
		r := httptest.NewRequest(http.MethodPost, "https://"+browserHost+base+"/issue", strings.NewReader(raw))
		r.AddCookie(f.cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.Header.Set("Origin", "https://"+browserHost)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		labSubscriptionDecode(t, w, http.StatusUnprocessableEntity, nil)
	}
	canonical := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	consumer := "/api/v1/subscriptions/" + canonical
	for _, token := range []string{"bad", canonical + "=", canonical[:42], strings.Repeat("A", 42) + "B"} {
		w := request(t, h, http.MethodGet, "/api/v1/subscriptions/"+token, "", nil)
		labSubscriptionDecode(t, w, http.StatusNotFound, nil)
		labSubscriptionNoSecret(t, w, token)
	}
	for _, query := range []string{"format=", "format=unknown", "format=raw&format=base64", "format=raw&extra=1", "extra=1", "format=%zz", "format=raw;extra=1"} {
		labSubscriptionDecode(t, request(t, h, http.MethodGet, consumer+"?"+query, "", nil), http.StatusUnprocessableEntity, nil)
	}
	for _, query := range []string{"", "?format=raw", "?format=base64"} {
		labSubscriptionDecode(t, request(t, h, http.MethodGet, consumer+query, "", nil), http.StatusNotFound, nil)
	}
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		labSubscriptionDecode(t, request(t, h, method, consumer, "", nil), http.StatusMethodNotAllowed, nil)
	}
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodGet, base+"/reveal", nil, f.cookie, "", ""), http.StatusMethodNotAllowed, nil)
	labSubscriptionDecode(t, browserRequest(t, h, http.MethodPost, base, body, f.cookie, f.csrf, "https://"+browserHost), http.StatusMethodNotAllowed, nil)
	for _, tc := range []struct{ method, path string }{{http.MethodGet, base}, {http.MethodPost, base + "/issue"}, {http.MethodPost, base + "/reveal"}, {http.MethodGet, consumer}, {http.MethodPost, handoverBase + "/" + f.plan.ID + "/lab-subscription-snapshot"}} {
		r := httptest.NewRequest(tc.method, "https://"+browserHost+tc.path, strings.NewReader(`{}`))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.AddCookie(f.cookie)
		r.Header.Set("Authorization", "Bearer "+ownerToken)
		w := httptest.NewRecorder()
		f.s.AgentHandler().ServeHTTP(w, r)
		labSubscriptionDecode(t, w, http.StatusNotFound, nil)
	}
}

func TestLabSubscriptionsPendingProofCurrentMetadataAndAdmission(t *testing.T) {
	f := newHealthRoundAPI(t)
	if err := f.s.EnableLabSubscriptions(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := labSubscriptionClientPath
	labSubscriptionDecode(t, f.post(t, base+"/issue", map[string]any{"event_id": handoverTestID(530), "expected_generation": 0}), http.StatusOK, nil)
	secret, consumer := labSubscriptionReveal(t, f, base, 1, f.cookie, f.csrf)
	roundPath := handoverBase + "/" + f.plan.ID + "/health-rounds"
	var round business.MeteredHealthRound
	labSubscriptionDecode(t, f.post(t, roundPath, map[string]any{"id": handoverTestID(531), "confirmed": true}), http.StatusAccepted, &round)
	materialize := handoverBase + "/" + f.plan.ID + "/lab-subscription-snapshot"
	body := map[string]any{"event_id": handoverTestID(532), "client_id": "handover-customer", "round_id": round.ID, "expected_revision": 0}
	labSubscriptionDecode(t, f.post(t, materialize, body), http.StatusConflict, nil)
	metadata := func(wantCurrent bool, wantSnapshot bool) {
		t.Helper()
		w := browserRequest(t, f.s.BrowserHandler(), http.MethodGet, base, nil, f.cookie, "", "")
		var out business.LabSubscriptionView
		labSubscriptionDecode(t, w, http.StatusOK, &out)
		labSubscriptionNoSecret(t, w, secret)
		if (out.Snapshot != nil) != wantSnapshot || (out.Snapshot != nil && out.Snapshot.Current != wantCurrent) {
			t.Fatal("owner metadata disagrees with current delivery authority")
		}
	}
	metadata(false, false)
	labSubscriptionDecode(t, request(t, f.s.BrowserHandler(), http.MethodGet, consumer, "", nil), http.StatusNotFound, nil)
	// Synthetic signed evidence tests the HTTP authority boundary; the separate
	// pinned-core case proves that actual protocol traffic produces this proof.
	q, found, err := f.s.business.PollMeteredHealth(ctx, f.key, []byte(f.tokens[1]), f.p[1].NodeID, f.p[1].InstallationID, roundEndpoint())
	if err != nil || !found {
		t.Fatal("synthetic round challenge", err)
	}
	responseSHA := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	proof := mx.ProtocolHealthReceipt{Version: mx.ProtocolHealthVersion, RequestSHA: handoverHash(q), ResponseSHA: hex.EncodeToString(responseSHA[:]), Completed: time.Now().UnixNano()}
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(f.tokens[1]))
	mac.Write([]byte("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2\x00"))
	mac.Write(raw)
	proof.Signature = hex.EncodeToString(mac.Sum(nil))
	if _, err := f.s.business.ConsumeMeteredHealthProof(ctx, f.key, []byte(f.tokens[1]), q.ID, roundEndpoint(), proof); err != nil {
		t.Fatal("consume synthetic complete-round proof", err)
	}
	var snapshot business.LabSubscriptionSnapshot
	labSubscriptionDecode(t, f.post(t, materialize, body), http.StatusCreated, &snapshot)
	if !snapshot.Current || snapshot.EventID != handoverTestID(532) || snapshot.RouteCount != 1 || snapshot.RoundID != round.ID || snapshot.ExpiresUnixNano > round.Expires {
		t.Fatal("complete synthetic round lost its bounded snapshot authority")
	}
	metadata(true, true)
	labSubscriptionDecode(t, request(t, f.s.BrowserHandler(), http.MethodGet, consumer, "", nil), http.StatusOK, nil)
	for _, node := range []string{f.plan.SourceNode, f.plan.TargetNode} {
		original := f.s.meteredRouteNodes[node]
		f.s.meteredRouteNodes[node] = handoverTestID(999)
		metadata(false, true)
		labSubscriptionDecode(t, request(t, f.s.BrowserHandler(), http.MethodGet, consumer, "", nil), http.StatusNotFound, nil)
		labSubscriptionDecode(t, f.post(t, materialize, body), http.StatusConflict, nil)
		f.s.meteredRouteNodes[node] = original
	}
	// A current SQLite observation cannot substitute for the current Registry.
	_, emptyRegistry, _, _ := newLab(t)
	originalRegistry := f.s.registry
	f.s.registry = emptyRegistry
	metadata(false, true)
	labSubscriptionDecode(t, request(t, f.s.BrowserHandler(), http.MethodGet, consumer, "", nil), http.StatusNotFound, nil)
	f.s.registry = originalRegistry
	metadata(true, true)
	// Hold the same lifecycle lock that protects health/replacement work. A
	// public request must return immediately instead of joining an unbounded queue.
	f.s.replacementLifecycle.Lock()
	done := make(chan *httptest.ResponseRecorder, 1)
	h := f.s.BrowserHandler()
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, consumer, nil))
		done <- w
	}()
	var busy *httptest.ResponseRecorder
	select {
	case busy = <-done:
		f.s.replacementLifecycle.Unlock()
	case <-time.After(time.Second):
		f.s.replacementLifecycle.Unlock()
		<-done
		t.Fatal("consumer queued behind the lifecycle mutex")
	}
	labSubscriptionDecode(t, busy, http.StatusServiceUnavailable, nil)
	labSubscriptionNoSecret(t, busy, secret)
	if busy.Header().Get("Retry-After") != "1" {
		t.Fatal("busy public delivery omitted its bounded retry hint")
	}
	labSubscriptionDecode(t, request(t, h, http.MethodGet, consumer, "", nil), http.StatusOK, nil)
	slow := &labSubscriptionSlowWriter{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{}), lifecycle: &f.s.replacementLifecycle}
	written := make(chan struct{})
	go func() {
		h.ServeHTTP(slow, httptest.NewRequest(http.MethodGet, consumer, nil))
		close(written)
	}()
	select {
	case <-slow.entered:
	case <-time.After(time.Second):
		close(slow.release)
		<-written
		t.Fatal("bounded consumer document did not reach the HTTP writer")
	}
	available := f.s.replacementLifecycle.TryLock()
	if available {
		f.s.replacementLifecycle.Unlock()
	}
	close(slow.release)
	<-written
	if !available || !slow.headerAvailable || !slow.bodyAvailable || slow.status != http.StatusOK {
		t.Fatal("slow consumer retained the health/replacement lock while writing a response")
	}
	view, err := f.s.business.MeteredHandover(ctx, f.plan.ID)
	if err != nil || view.CanPublish || view.Phase != "target_applied_health_required" {
		t.Fatal("LAB delivery changed production handover publication state", err)
	}
}
