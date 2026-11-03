//go:build linux

package business_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealImportedAgentSupervisorTLS(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("requires isolated final TCP core")
	}
	for _, mode := range []string{"normal", "revoked", "authority-outage", "hub-restart", "full-outage", "lost-birth-reply", "lost-apply-reply", "apply-refused", "owner-logout", "connection", "connection-lost-reply", "connection-bad-nonce", "connection-bad-pin"} {
		t.Run(mode, func(t *testing.T) { realImportedAgentSupervisorTLS(t, mode) })
	}
}
func realImportedAgentSupervisorTLS(t *testing.T, mode string) {
	worker, binary := os.Getenv("DARK_PRO_TEST_QUARANTINE_WORKER"), os.Getenv("DARK_PRO_TEST_XRAY_BINARY")
	if worker == "" || binary == "" {
		t.Fatal("real Agent worker and pinned core required")
	}
	ctx := context.Background()
	connectionMode := strings.HasPrefix(mode, "connection")
	var verifierRequests atomic.Int64
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/dark-pro/protocol-health/v1/") {
			verifierRequests.Add(1)
			if mode == "connection-bad-nonce" {
				io.WriteString(w, "foreign response")
				return
			}
			io.WriteString(w, "DARK-PRO-PROTOCOL-HEALTH-v1\n"+strings.TrimPrefix(r.URL.Path, "/dark-pro/protocol-health/v1/")+"\n")
			return
		}
		w.WriteHeader(204)
	}))
	target.Listener.Close()
	listener, err := net.Listen("tcp4", "11.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target.Listener = listener
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.EnableHTTP2 = true
	target.StartTLS()
	defer target.Close()
	portListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(portListener.Addr().(*net.TCPAddr).Port)
	portListener.Close()
	store, id, key, pins, password, uri, dbPath := business.BrowserPreparedRuntimeFixture(t, target.Listener.Addr().String(), port)
	before, err := store.ListRepresentatives(ctx, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	registryDir := t.TempDir()
	if err := os.Chmod(registryDir, 0700); err != nil {
		t.Fatal(err)
	}
	reg, err := noderegistry.Open(filepath.Join(registryDir, "registry.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := reg.Issue("quarantine-node", "Disposable metered supervisor")
	if err != nil {
		t.Fatal(err)
	}
	install, token := strings.Repeat("a", 32), strings.Repeat("b", 64)
	if _, err = reg.Claim(ticket.PairCode, "quarantine-node", install, token); err != nil {
		t.Fatal(err)
	}
	srv, err := api.NewServer(telemetry.NewStore(time.Minute, time.Now), reg, strings.Repeat("h", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []func() error{
		func() error { return srv.EnableBusinessLab(store) }, func() error { return srv.EnableNativeInbounds(key) }, func() error { return srv.EnableClientOwnershipV1Lab(ctx) }, func() error {
			return srv.StageDarkRestoreLocalHandoff(ctx, pins.Path, pins.SHA, pins.PublicPath, pins.PublicSHA)
		},
	} {
		if err = fn(); err != nil {
			t.Fatal(err)
		}
	}
	if connectionMode {
		cert := target.Certificate()
		h := sha256.Sum256(cert.Raw)
		verifier := mx.ProtocolHealthEndpoint{Address: target.Listener.Addr().String(), ServerName: "example.com", CertificateSHA: hex.EncodeToString(h[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})}
		if mode == "connection-bad-pin" {
			verifier.CertificateSHA = strings.Repeat("f", 64)
		}
		if err = srv.EnableDarkRestoreSystemProbeChecks(verifier); err != nil {
			t.Fatal(err)
		}
		if err = srv.EnableDarkRestoreCustomerConnectionChecks(verifier); err != nil {
			t.Fatal(err)
		}
	}
	browser := srv.BrowserHandler()
	request := func(path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "https://pro.example"+path, bytes.NewReader(raw))
		r.Header.Set("Origin", "https://pro.example")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		browser.ServeHTTP(w, r)
		return w
	}
	login := request("/api/v1/session/login", map[string]string{"username": "ownership-owner", "password": password}, nil, "")
	if login.Code != 200 {
		t.Fatal("login", login.Code)
	}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(login.Body.Bytes(), &session)
	cookie := login.Result().Cookies()[0]
	pub, signer, _ := ed25519.GenerateKey(nil)
	approved := request("/api/v1/workspace/dark-restore/handoff/quarantine", map[string]any{"CoreSHA": os.Getenv("DARK_PRO_TEST_XRAY_SHA256"), "PublicKey": hex.EncodeToString(pub), "NativeRuntime": true}, cookie, session.CSRF)
	if approved.Code != 200 {
		t.Fatal("approval", approved.Code, approved.Body.String())
	}
	var approval struct {
		SHA string `json:"plan_sha256"`
	}
	json.Unmarshal(approved.Body.Bytes(), &approval)
	var renewals atomic.Int64
	var outage atomic.Bool
	var fullOutage atomic.Bool
	var connectionPosts atomic.Int64
	var systemProbePosts atomic.Int64
	var lostConnection atomic.Bool
	var lostSystemProbe atomic.Bool
	if mode == "connection-lost-reply" {
		lostConnection.Store(true)
		lostSystemProbe.Store(true)
	}
	var lostApply atomic.Bool
	if mode == "lost-apply-reply" {
		lostApply.Store(true)
	}
	var lostBirth atomic.Bool
	var active atomic.Pointer[api.Server]
	active.Store(srv)
	if mode == "lost-birth-reply" {
		lostBirth.Store(true)
	}
	remote := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fullOutage.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/api/v1/agent/darkrestore-runtime" {
			if outage.Load() {
				w.WriteHeader(503)
				return
			}
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var phase struct{ Phase string }
			json.Unmarshal(raw, &phase)
			if phase.Phase == "connection-result" {
				connectionPosts.Add(1)
			}
			if phase.Phase == "system-probe-result" {
				systemProbePosts.Add(1)
			}
			if phase.Phase == "system-probe-result" && lostSystemProbe.Swap(false) {
				recorder := httptest.NewRecorder()
				active.Load().AgentHandler().ServeHTTP(recorder, r)
				if recorder.Code != 200 {
					t.Errorf("system probe result never committed: %d %s", recorder.Code, recorder.Body.String())
				}
				w.WriteHeader(503)
				return
			}
			if phase.Phase == "connection-result" && lostConnection.Swap(false) {
				recorder := httptest.NewRecorder()
				active.Load().AgentHandler().ServeHTTP(recorder, r)
				if recorder.Code != 200 {
					t.Errorf("connection result never committed: %d %s", recorder.Code, recorder.Body.String())
				}
				w.WriteHeader(503)
				return
			}
			if phase.Phase == "birth" && lostBirth.Swap(false) {
				recorder := httptest.NewRecorder()
				active.Load().AgentHandler().ServeHTTP(recorder, r)
				if recorder.Code != 200 {
					t.Errorf("lost birth never committed: %d", recorder.Code)
				}
				w.WriteHeader(503)
				return
			}
			if phase.Phase == "applied" && mode == "apply-refused" {
				w.WriteHeader(403)
				return
			}
			if phase.Phase == "applied" && lostApply.Swap(false) {
				recorder := httptest.NewRecorder()
				active.Load().AgentHandler().ServeHTTP(recorder, r)
				if recorder.Code != 200 {
					t.Errorf("lost apply never committed: %d", recorder.Code)
				}
				w.WriteHeader(503)
				return
			}
			if phase.Phase == "renew" {
				renewals.Add(1)
			}
		}
		active.Load().AgentHandler().ServeHTTP(w, r)
	}))
	remote.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	remote.StartTLS()
	defer remote.Close()
	cert := remote.Certificate()
	digest := sha256.Sum256(cert.Raw)
	command := exec.Command(worker, "-test.run=^TestDarkRestoreSupervisorPinnedTLS$", "-test.v")
	command.Env = append(os.Environ(), "DARK_PRO_QUARANTINE_SUPERVISOR_TEST=1", "QUARANTINE_TEST_URL="+remote.URL, "QUARANTINE_TEST_TLS_PIN="+hex.EncodeToString(digest[:]), "QUARANTINE_TEST_CA="+string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})), "QUARANTINE_TEST_KEY="+hex.EncodeToString(signer), "QUARANTINE_TEST_INSTALL="+install, "QUARANTINE_TEST_TOKEN="+token, "QUARANTINE_TEST_PLAN="+approval.SHA, "QUARANTINE_TEST_DIR="+filepath.Join(t.TempDir(), "runtime"))
	if !connectionMode && mode != "normal" && mode != "lost-birth-reply" && mode != "lost-apply-reply" && mode != "owner-logout" {
		command.Env = append(command.Env, "QUARANTINE_TEST_EXPECT_STOP=1")
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	defer func() {
		if !done {
			command.Process.Kill()
			command.Wait()
		}
	}()
	deadline := time.Now().Add(8 * time.Second)
	admitted := false
	for time.Now().Before(deadline) {
		w := request("/api/v1/workspace/dark-restore/handoff/quarantine/admission", map[string]string{"plan_sha256": approval.SHA, "event_id": "supervisor-owner-admit-0001"}, cookie, session.CSRF)
		if w.Code == 200 {
			admitted = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !admitted {
		command.Process.Kill()
		command.Wait()
		done = true
		t.Fatalf("node birth did not reach owner admission: %s", output.String())
	}
	if mode == "apply-refused" {
		err = command.Wait()
		done = true
		if err != nil {
			t.Fatalf("receipt refusal did not stop cleanly: %v %s", err, output.String())
		}
		account, e := store.ClientUsage(ctx, id)
		if e != nil || account.Total != 0 || !account.RemainingNativeBudgetKnown || account.RemainingNativeBudgetBytes != 999986 {
			t.Fatal("refused receipt accounting", e)
		}
		db, e := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
		if e != nil {
			t.Fatal(e)
		}
		var sealed int
		e = db.QueryRow("SELECT COUNT(*) FROM pro_usage_epochs WHERE state='sealed'").Scan(&sealed)
		db.Close()
		if e != nil || sealed != 1 {
			t.Fatal("receipt failure did not seal final usage", sealed, e)
		}
		c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
		if e == nil {
			c.Close()
			t.Fatal("unacknowledged process still listening")
		}
		after, e := store.ListRepresentatives(ctx, 100, 0)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("receipt refusal wallet change", e)
		}
		if e = store.Close(); e != nil {
			t.Fatal(e)
		}
		if _, e = business.ValidateOfflineBackup(ctx, dbPath); e != nil {
			t.Fatal("refusal backup", e)
		}
		t.Log("APPLY_REFUSAL_STOP_SETTLED: no unacknowledged listener, exact zero final, no wallet change")
		return
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	for time.Now().Before(deadline) {
		conn, e := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	parsed, err := url.Parse(strings.TrimSpace(uri))
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	socksListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	socksPort := socksListener.Addr().(*net.TCPAddr).Port
	socksListener.Close()
	config := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": parsed.Hostname(), "port": int(port), "users": []any{map[string]any{"id": parsed.User.Username(), "encryption": "none", "flow": q.Get("flow")}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid")}}}}}
	raw, _ := json.Marshal(config)
	path := filepath.Join(t.TempDir(), "client.json")
	os.WriteFile(path, raw, 0600)
	consumer := exec.Command(binary, "run", "-config", path)
	consumer.Stdout = io.Discard
	consumer.Stderr = io.Discard
	if err = consumer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { consumer.Process.Kill(); consumer.Wait() }()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort))
	for time.Now().Before(deadline) {
		conn, e := net.DialTimeout("tcp", socks, 50*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	payload := strings.Repeat("agent-supervised-import-", 600)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	origin.Listener.Close()
	origin.Listener, err = net.Listen("tcp4", "11.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin.Start()
	defer origin.Close()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return supervisorSOCKS(ctx, socks, address)
	}}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 4 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal("actual Agent subscription", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != payload {
		t.Fatal("actual payload", err)
	}
	applyDeadline := time.Now().Add(3 * time.Second)
	applyRecorded := false
	for time.Now().Before(applyDeadline) {
		get := httptest.NewRequest("GET", "https://pro.example/api/v1/workspace/dark-restore/handoff/quarantine/admission/evidence", nil)
		get.AddCookie(cookie)
		w := httptest.NewRecorder()
		browser.ServeHTTP(w, get)
		var e business.DarkRestoreNativeAdmissionEvidenceV1
		if w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &e) == nil && e.ListenerApplyRecorded && len(e.ListenerApplyReceiptSHA) == 64 && !e.NativeApplyRecorded && !e.ServingHealthRecorded && !e.AuthorizesMutation {
			applyRecorded = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !applyRecorded {
		t.Fatal("real Agent listener apply receipt missing")
	}
	if connectionMode {
		input := map[string]any{"plan_sha256": approval.SHA, "event_id": "customer-connection-test-0001", "acknowledge_usage": false}
		denied := request("/api/v1/workspace/dark-restore/handoff/quarantine/connection-check", input, cookie, session.CSRF)
		if denied.Code != 422 {
			t.Fatal("missing consent", denied.Code, denied.Body.String())
		}
		input["acknowledge_usage"] = true
		queued := request("/api/v1/workspace/dark-restore/handoff/quarantine/connection-check", input, cookie, session.CSRF)
		if queued.Code != 200 {
			t.Fatal("connection queue", queued.Code, queued.Body.String())
		}
		replay := request("/api/v1/workspace/dark-restore/handoff/quarantine/connection-check", input, cookie, session.CSRF)
		if replay.Code != 200 || !bytes.Equal(replay.Body.Bytes(), queued.Body.Bytes()) {
			t.Fatal("intent replay", replay.Code)
		}
		verified := false
		var lastStatus business.DarkRestoreConnectionStatus
		checkDeadline := time.Now().Add(16 * time.Second)
		for time.Now().Before(checkDeadline) {
			get := httptest.NewRequest("GET", "https://pro.example/api/v1/workspace/dark-restore/handoff/quarantine/connection-check", nil)
			get.AddCookie(cookie)
			w := httptest.NewRecorder()
			browser.ServeHTTP(w, get)
			var status business.DarkRestoreConnectionStatus
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil {
				t.Fatal("connection status", w.Code, w.Body.String())
			}
			lastStatus = status
			if status.CustomerConnectionVerified {
				if status.State != "verified" || status.PlanSHA != approval.SHA || status.ClientID != id || status.AuthorizesMutation || len(status.ReceiptSHA) != 64 {
					t.Fatal("connection proof scope", w.Body.String())
				}
				verified = true
				break
			}
			if status.State == "expired_unknown" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		want := mode == "connection" || mode == "connection-lost-reply"
		if verified != want {
			t.Fatalf("connection receipt verified=%v want=%v; verifier=%d worker=%s", verified, want, verifierRequests.Load(), output.String())
		}
		if !want && lastStatus.State != "expired_unknown" {
			t.Fatal("failed probe never reached an expired issued attempt", lastStatus)
		}
		if mode == "connection-bad-nonce" && verifierRequests.Load() != 2 {
			t.Fatal("system and customer nonce failures were not each exercised once", verifierRequests.Load())
		}
		if mode == "connection-bad-pin" && verifierRequests.Load() != 0 {
			t.Fatal("untrusted TLS pin reached verifier HTTP", verifierRequests.Load())
		}
		if want && verifierRequests.Load() != 2 {
			t.Fatal("system and customer test traffic did not each run exactly once", verifierRequests.Load())
		}
		get := httptest.NewRequest("GET", "https://pro.example/api/v1/workspace/dark-restore/handoff/quarantine/admission/evidence", nil)
		get.AddCookie(cookie)
		evidenceResponse := httptest.NewRecorder()
		browser.ServeHTTP(evidenceResponse, get)
		var evidence struct {
			business.DarkRestoreNativeAdmissionEvidenceV1
			SystemProbe *business.DarkRestoreSystemProbeStatusV1 `json:"system_probe"`
		}
		if evidenceResponse.Code != 200 || json.Unmarshal(evidenceResponse.Body.Bytes(), &evidence) != nil ||
			evidence.ServingHealthRecorded || evidence.NativeApplyRecorded || evidence.AuthorizesMutation ||
			evidence.SystemProbe == nil || evidence.SystemProbe.ServingHealth != want ||
			evidence.SystemProbe.AuthorizesMutation || evidence.SystemProbe.PlanSHA != approval.SHA {
			t.Fatal("separate admission/system-probe evidence boundary", evidenceResponse.Code, evidenceResponse.Body.String())
		}
	}
	if mode == "owner-logout" {
		w := request("/api/v1/session/logout", map[string]any{}, cookie, session.CSRF)
		if w.Code != 200 && w.Code != 204 {
			t.Fatal("owner logout", w.Code)
		}
	}
	if mode == "revoked" {
		if _, err = store.SetClientState(ctx, business.SetClientState{EventID: "supervisor-revoke-0001", ClientID: id, Action: "disable", Reason: "Verify active Agent revocation"}); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "authority-outage" {
		outage.Store(true)
	}
	if mode == "hub-restart" {
		next, e := api.NewServer(telemetry.NewStore(time.Minute, time.Now), reg, strings.Repeat("h", 32))
		if e != nil {
			t.Fatal(e)
		}
		if e = next.EnableBusinessLab(store); e != nil {
			t.Fatal(e)
		}
		active.Store(next)
	}
	if mode == "full-outage" {
		fullOutage.Store(true)
	}
	err = command.Wait()
	done = true
	if err != nil {
		t.Fatalf("Agent supervisor failed: %v %s", err, output.String())
	}

	if mode == "full-outage" || mode == "connection-lost-reply" {
		fullOutage.Store(false)
		for attempt := 0; attempt < 2; attempt++ {
			recovery := exec.Command(worker, "-test.run=^TestDarkRestoreSupervisorPinnedTLS$", "-test.v")
			for _, v := range command.Env {
				if !strings.HasPrefix(v, "QUARANTINE_TEST_EXPECT_STOP=") {
					recovery.Env = append(recovery.Env, v)
				}
			}
			recovery.Env = append(recovery.Env, "QUARANTINE_TEST_EXPECT_STOP=0")
			if out, e := recovery.CombinedOutput(); e != nil {
				t.Fatalf("saved final receipt recovery: %v %s", e, out)
			}
		}
	}
	if mode == "connection-lost-reply" && connectionPosts.Load() != 4 {
		t.Fatal("lost acknowledgment and restart did not replay one saved customer proof", connectionPosts.Load())
	}
	if mode == "connection-lost-reply" && systemProbePosts.Load() < 2 {
		t.Fatal("lost system-probe acknowledgment did not replay saved proof", systemProbePosts.Load())
	}
	if connectionMode && (mode == "connection" || mode == "connection-lost-reply") && verifierRequests.Load() != 2 {
		t.Fatal("later polling repeated system or customer network traffic", verifierRequests.Load())
	}
	account, err := store.ClientUsage(ctx, id)
	if err != nil || account.Total < int64(len(payload)) || !account.RemainingNativeBudgetKnown || account.RemainingNativeBudgetBytes != 999986-account.Total {
		t.Fatal("native accounting", err)
	}
	after, err := store.ListRepresentatives(ctx, 100, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("wallet changed", err)
	}
	if (mode == "normal" || mode == "owner-logout") && renewals.Load() < 2 {
		t.Fatal("supervisor did not renew live epoch")
	}

	conn, e := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if e == nil {
		conn.Close()
		t.Fatal("listener remained after supervisor stop")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err = business.ValidateOfflineBackup(ctx, dbPath); err != nil {
		t.Fatal("supervised traffic backup", err)
	}
	reopened, err := business.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.ClientUsage(ctx, id)
	if err != nil || recovered.Total != account.Total {
		t.Fatal("reopened supervisor accounting", err)
	}
	t.Logf("AGENT_SUPERVISOR_REAL_PASS native=%d history=14 remaining=%d renewals=%d; final stop settled", account.Total, account.RemainingNativeBudgetBytes, renewals.Load())
}

func supervisorSOCKS(ctx context.Context, socks, target string) (net.Conn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp4", socks)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			c.Close()
		}
	}()
	c.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	answer := make([]byte, 2)
	if _, err = io.ReadFull(c, answer); err != nil || !bytes.Equal(answer, []byte{5, 0}) {
		return nil, errors.New("SOCKS greeting refused")
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host).To4()
	port, err := strconv.Atoi(portText)
	if err != nil || ip == nil {
		return nil, errors.New("invalid isolated destination")
	}
	request := append([]byte{5, 1, 0, 1}, ip...)
	request = append(request, byte(port>>8), byte(port))
	if _, err = c.Write(request); err != nil {
		return nil, err
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(c, header); err != nil || header[1] != 0 {
		return nil, errors.New("SOCKS connection refused")
	}
	size := 0
	switch header[3] {
	case 1:
		size = 6
	case 4:
		size = 18
	case 3:
		b := make([]byte, 1)
		if _, err = io.ReadFull(c, b); err != nil {
			return nil, err
		}
		size = int(b[0]) + 2
	default:
		return nil, errors.New("invalid SOCKS address")
	}
	if _, err = io.ReadFull(c, make([]byte, size)); err != nil {
		return nil, err
	}
	success = true
	return c, nil
}
