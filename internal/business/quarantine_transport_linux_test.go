//go:build linux

package business_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQuarantineRealAgentTLS(t *testing.T) {
	binary, core := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary == "" {
		t.Skip("requires pinned offline core")
	}
	ctx := context.Background()
	store, id, key, pins, password := business.BrowserPreparedQuarantineFixture(t)
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
	ticket, err := reg.Issue("quarantine-node", "Disposable offline node")
	if err != nil {
		t.Fatal(err)
	}
	install := strings.Repeat("a", 32)
	token := strings.Repeat("b", 64)
	if _, err = reg.Claim(ticket.PairCode, "quarantine-node", install, token); err != nil {
		t.Fatal(err)
	}
	srv, err := api.NewServer(telemetry.NewStore(time.Minute, time.Now), reg, strings.Repeat("h", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []func() error{
		func() error { return srv.EnableBusinessLab(store) },
		func() error { return srv.EnableNativeInbounds(key) },
		func() error { return srv.EnableClientOwnershipV1Lab(ctx) },
		func() error {
			return srv.StageDarkRestoreLocalHandoff(ctx, pins.Path, pins.SHA, pins.PublicPath, pins.PublicSHA)
		},
	} {
		if err = fn(); err != nil {
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
		t.Fatal("owner login", login.Code)
	}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(login.Body.Bytes(), &session)
	cookie := login.Result().Cookies()[0]
	pub, signer, _ := ed25519.GenerateKey(nil)
	body := map[string]string{"CoreSHA": core, "PublicKey": hex.EncodeToString(pub)}
	route := "/api/v1/workspace/dark-restore/handoff/quarantine"
	if w := request(route, body, cookie, ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	approved := request(route, body, cookie, session.CSRF)
	if approved.Code != 200 {
		t.Fatal("approve", approved.Code, approved.Body.String())
	}
	var approval struct {
		SHA string `json:"plan_sha256"`
	}
	json.Unmarshal(approved.Body.Bytes(), &approval)
	remote := httptest.NewUnstartedServer(srv.AgentHandler())
	remote.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	remote.StartTLS()
	defer remote.Close()
	agent := func(plan string, receipt *business.DarkRestoreQuarantineRegistration, secret string) (int, []byte) {
		raw, _ := json.Marshal(struct {
			NodeID, InstallationID, PlanSHA string
			Receipt                         *business.DarkRestoreQuarantineRegistration
		}{"quarantine-node", install, plan, receipt})
		r, _ := http.NewRequest("POST", remote.URL+"/api/v1/agent/darkrestore-quarantine", bytes.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+secret)
		res, err := remote.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	if code, _ := agent(approval.SHA, nil, "wrong"); code != 401 {
		t.Fatal("agent auth", code)
	}
	if code, _ := agent(strings.Repeat("0", 64), nil, token); code != 403 {
		t.Fatal("wrong pin", code)
	}
	code, raw := agent(approval.SHA, nil, token)
	if code != 200 {
		t.Fatal("fetch", code)
	}
	var delivery business.DarkRestoreQuarantineDelivery
	if err = json.Unmarshal(raw, &delivery); err != nil {
		t.Fatal(err)
	}
	plan, err := delivery.Open("quarantine-node", install, approval.SHA)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	receipt, err := plan.RegisterOffline(ctx, "quarantine-node", install, approval.SHA, binary, core, dir, signer)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		code, raw = agent(approval.SHA, &receipt, token)
		if code != 200 {
			t.Fatal("receipt/replay", code, string(raw))
		}
	}

	if worker := os.Getenv("DARK_PRO_TEST_QUARANTINE_WORKER"); worker != "" {
		cert := remote.Certificate()
		digest := sha256.Sum256(cert.Raw)
		command := exec.Command(worker, "-test.run=^TestQuarantineWorkerPinnedTLS$", "-test.v")
		command.Env = append(os.Environ(), "DARK_PRO_QUARANTINE_WORKER_TEST=1", "QUARANTINE_TEST_URL="+remote.URL, "QUARANTINE_TEST_TLS_PIN="+hex.EncodeToString(digest[:]), "QUARANTINE_TEST_CA="+string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})), "QUARANTINE_TEST_KEY="+hex.EncodeToString(signer), "QUARANTINE_TEST_INSTALL="+install, "QUARANTINE_TEST_TOKEN="+token, "QUARANTINE_TEST_PLAN="+approval.SHA, "QUARANTINE_TEST_DIR="+dir)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("real Agent worker: %v %s", err, out)
		} else {
			t.Log(string(out))
		}
	}
	after, err := store.ListRepresentatives(ctx, 100, 0)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("balances changed", err)
	}
	c, err := store.Client(ctx, id)
	if err != nil || c.DeploymentState != "disabled" {
		t.Fatal("client escaped quarantine", err)
	}
	if _, err = store.SetClientState(ctx, business.SetClientState{EventID: "quarantine-tls-enable-0001", ClientID: id, Action: "enable", Reason: "Verify node offline receipt cannot activate"}); err == nil {
		t.Fatal("offline node proof activated")
	}

	altered := receipt
	altered.Signature = append([]byte(nil), receipt.Signature...)
	altered.Signature[0] ^= 1
	if code, _ = agent(approval.SHA, &altered, token); code != 409 {
		t.Fatal("forged receipt", code)
	}
	if _, err = store.ExtendClientScoped(ctx, business.ExtendClient{EventID: "quarantine-tls-renew-0001", ClientID: id, AdditionalVolumeBytes: 20, Reason: "Invalidate node registration after renewal"}, c.RepresentativeID); err != nil {
		t.Fatal(err)
	}
	if code, _ = agent(approval.SHA, &receipt, token); code != 409 {
		t.Fatal("obsolete registration", code)
	}
	if code, _ = agent(approval.SHA, nil, token); code != 409 {
		t.Fatal("obsolete candidate delivery", code)
	}
	t.Log("real TLS fetch, pinned core validation, signed receipt replay, unchanged balance and disabled client")
}
