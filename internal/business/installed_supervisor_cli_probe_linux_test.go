//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

func TestInstalledSupervisorCLIProbe(t *testing.T) {
	if os.Getenv("DARK_PRO_INSTALLED_SUPERVISOR_PROBE") != "1" {
		t.Skip("explicit private binary acceptance")
	}
	if os.Geteuid() != 0 {
		t.Fatal("private chroot bootstrap requires root")
	}
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatal("isolated loopback namespace required")
	}
	plainTarget := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	plainTarget.Listener.Close()
	plainTarget.Listener, err = net.Listen("tcp4", "11.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	plainTarget.EnableHTTP2 = true
	plainTarget.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	plainTarget.StartTLS()
	defer plainTarget.Close()
	customerPort := meterPort(t)
	store, id, key, pins, password, originalURI, _ := BrowserPreparedRuntimeFixture(t, plainTarget.Listener.Addr().String(), customerPort)
	balances := ownershipBalances(t, store)
	base, err := os.MkdirTemp("/var/tmp", "m5-installed-cli.")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("private fixture", base)
	// Preserve this reproducible private fixture and its logs for QA review.
	os.Chmod(base, 0755)
	root := filepath.Join(base, "root")
	state := "/var/lib/dark-xray-pro-lab"
	agent := filepath.Join(base, "agent")
	for _, p := range []string{filepath.Join(root, state), filepath.Join(root, "tmp"), agent} {
		if err = os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	copyFile := func(from, to string, mode os.FileMode) {
		data, e := os.ReadFile(from)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(to, data, mode); e != nil {
			t.Fatal(e)
		}
	}
	hubBinary := os.Getenv("DARK_PRO_TEST_HUB_BINARY")
	agentBinary := os.Getenv("DARK_PRO_TEST_AGENT_BINARY")
	core := os.Getenv("DARK_PRO_TEST_XRAY_BINARY")
	coreSHA := os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	copyFile(hubBinary, filepath.Join(root, "hub"), 0500)
	copyFile(agentBinary, filepath.Join(agent, "agent"), 0500)
	copyFile(core, filepath.Join(agent, "xray"), 0500)
	dbPath := filepath.Join(root, state, "business.sqlite")
	if _, err = store.db.Exec("VACUUM INTO ?", dbPath); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dbPath, 0600); err != nil {
		t.Fatal(err)
	}
	for to, from := range map[string]string{"handoff": pins.Path, "public": pins.PublicPath} {
		copyFile(from, filepath.Join(root, state, to), 0600)
	}
	reg, err := noderegistry.Open(filepath.Join(root, state, "registry.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := reg.Issue("quarantine-node", "Actual artifact CLI fixture")
	if err != nil {
		t.Fatal(err)
	}
	install, token := strings.Repeat("a", 32), strings.Repeat("b", 64)
	if _, err = reg.Claim(ticket.PairCode, "quarantine-node", install, token); err != nil {
		t.Fatal(err)
	}
	identity := agentidentity.Identity{NodeID: "quarantine-node", InstallationID: install, AgentToken: token, Paired: true}
	identityRaw, _ := json.Marshal(identity)
	os.WriteFile(filepath.Join(agent, "identity.json"), identityRaw, 0600)
	pub, signer, _ := ed25519.GenerateKey(nil)
	os.WriteFile(filepath.Join(agent, "signer"), []byte(hex.EncodeToString(signer)), 0600)
	// The HUB and Agent trust only this disposable test certificate.
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	certificate := tlsServer.TLS.Certificates[0]
	tlsServer.Close()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	privateDER, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "cert.pem"), certPEM, 0600)
	os.WriteFile(filepath.Join(root, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600)
	os.WriteFile(filepath.Join(agent, "ca.pem"), certPEM, 0600)
	certHash := sha256.Sum256(certificate.Certificate[0])
	uid, gid := 65534, 65534
	for _, dir := range []string{root, agent} {
		if err = filepath.Walk(dir, func(path string, info os.FileInfo, e error) error {
			if e != nil {
				return e
			}
			return os.Chown(path, uid, gid)
		}); err != nil {
			t.Fatal(err)
		}
	}
	ownerPort, nodePort := meterPort(t), meterPort(t)
	ownerAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(ownerPort)))
	nodeAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(nodePort)))
	hubLog, _ := os.OpenFile(filepath.Join(base, "hub.log"), os.O_CREATE|os.O_WRONLY, 0600)
	defer hubLog.Close()
	hub := exec.Command("/hub", "-listen", ownerAddress, "-agent-tls-listen", nodeAddress, "-agent-tls-cert", "/cert.pem", "-agent-tls-key", "/key.pem", "-business-db", state+"/business.sqlite", "-registry", state+"/registry.json", "-replacements", state+"/replacements.json", "-darkrestore-signed-handoff", state+"/handoff", "-darkrestore-handoff-sha256", pins.SHA, "-darkrestore-handoff-public-key", state+"/public", "-darkrestore-handoff-public-key-sha256", pins.PublicSHA)
	hub.Dir = "/"
	hub.SysProcAttr = &syscall.SysProcAttr{Chroot: root, Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, Pdeathsig: syscall.SIGKILL}
	hub.Env = []string{"DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_REMOTE_AGENT=1", "DARK_PRO_ENABLE_BUSINESS_CORE=1", "DARK_PRO_ENABLE_USAGE_LEDGER=1", "DARK_PRO_ENABLE_NATIVE_INBOUNDS=1", "DARK_PRO_ENABLE_CLIENT_OWNERSHIP_V1=1", "DARK_PRO_LAB_TOKEN=" + strings.Repeat("h", 32), "DARK_PRO_NATIVE_INBOUND_KEY_HEX=" + hex.EncodeToString(key)}
	hub.Stdout = hubLog
	hub.Stderr = hubLog
	if err = hub.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { hub.Process.Kill(); hub.Wait() }()
	origin := "http://" + ownerAddress
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	deadline := time.Now().Add(8 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		r, e := client.Get(origin + "/api/v1/session/status")
		if e == nil {
			r.Body.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("actual HUB did not start", base)
	}
	var cookie *http.Cookie
	csrf := ""
	request := func(path string, body any) (int, []byte) {
		raw, _ := json.Marshal(body)
		r, _ := http.NewRequest("POST", origin+path, bytes.NewReader(raw))
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		if cookie == nil && len(response.Cookies()) > 0 {
			cookie = response.Cookies()[0]
		}
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, data
	}
	status, raw := request("/api/v1/session/login", map[string]string{"username": "ownership-owner", "password": password})
	if status != 200 {
		t.Fatal("actual owner login", status)
	}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	json.Unmarshal(raw, &session)
	csrf = session.CSRF
	status, raw = request("/api/v1/workspace/dark-restore/handoff/prepare", map[string]any{"handoff_sha256": pins.SHA, "event_id": "supervisor-link-delivery-0001", "inbound_id": 505, "plan_revision": 1, "inbound_revision": 1})
	if status != 200 {
		t.Fatal("actual prepared subscription", status, string(raw))
	}
	var delivery struct {
		Content string `json:"content"`
	}
	json.Unmarshal(raw, &delivery)
	if delivery.Content != originalURI {
		t.Fatal("artifact changed UUID/profile output")
	}
	status, raw = request("/api/v1/workspace/dark-restore/handoff/quarantine", map[string]any{"CoreSHA": coreSHA, "PublicKey": hex.EncodeToString(pub), "NativeRuntime": true})
	if status != 200 {
		t.Fatal("actual operator approval", status, string(raw))
	}
	var approval struct {
		SHA string `json:"plan_sha256"`
	}
	json.Unmarshal(raw, &approval)
	agentLog, _ := os.OpenFile(filepath.Join(base, "agent.log"), os.O_CREATE|os.O_WRONLY, 0600)
	defer agentLog.Close()
	node := exec.Command(filepath.Join(agent, "agent"), "-id", "quarantine-node", "-hub", "https://"+nodeAddress, "-hub-cert-sha256", hex.EncodeToString(certHash[:]), "-hub-ca-file", filepath.Join(agent, "ca.pem"), "-identity-file", filepath.Join(agent, "identity.json"), "-managed-runtime-dir", filepath.Join(agent, "runtime"), "-managed-listen-ip", "127.0.0.1", "-stage-xray-binary", filepath.Join(agent, "xray"), "-stage-xray-sha256", coreSHA)
	node.Dir = agent
	node.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, Pdeathsig: syscall.SIGKILL}
	node.Env = []string{"DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_REMOTE_AGENT=1", "DARK_PRO_ENABLE_DARKRESTORE_RUNTIME=1", "DARK_PRO_ENABLE_FINAL_TCP=1", "DARK_PRO_NATIVE_RUNTIME_KEY_HEX=" + strings.Repeat("02", 32), "DARK_PRO_QUARANTINE_PLAN_SHA=" + approval.SHA, "DARK_PRO_QUARANTINE_SIGNER_FILE=" + filepath.Join(agent, "signer")}
	node.Stdout = agentLog
	node.Stderr = agentLog
	if err = node.Start(); err != nil {
		t.Fatal(err)
	}
	nodeDone := false
	defer func() {
		if !nodeDone {
			node.Process.Kill()
			node.Wait()
		}
	}()
	deadline = time.Now().Add(12 * time.Second)
	admitted := false
	for time.Now().Before(deadline) {
		status, raw = request("/api/v1/workspace/dark-restore/handoff/quarantine/admission", map[string]string{"plan_sha256": approval.SHA, "event_id": "installed-supervisor-admit-0001"})
		if status == 200 {
			admitted = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !admitted {
		t.Fatal("actual Agent birth/admission missing", base)
	}
	parsed, err := url.Parse(strings.TrimSpace(delivery.Content))
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	socksPort := meterPort(t)
	config := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": parsed.Hostname(), "port": int(customerPort), "users": []any{map[string]any{"id": parsed.User.Username(), "encryption": "none", "flow": q.Get("flow")}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid")}}}}}
	raw, _ = json.Marshal(config)
	consumerPath := filepath.Join(base, "consumer.json")
	os.WriteFile(consumerPath, raw, 0600)
	consumer := exec.Command(core, "run", "-config", consumerPath)
	consumer.Stdout = io.Discard
	consumer.Stderr = io.Discard
	if err = consumer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { consumer.Process.Kill(); consumer.Wait() }()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	meterWait(t, func() bool {
		c, e := net.DialTimeout("tcp", socks, 50*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	payload := strings.Repeat("artifact-supervisor-real-", 600)
	source := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	source.Listener.Close()
	source.Listener, err = net.Listen("tcp4", "11.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	source.Start()
	defer source.Close()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, target string) (net.Conn, error) { return meterSOCKS(ctx, socks, target) }}
	defer transport.CloseIdleConnections()
	meterWait(t, func() bool {
		r, e := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(source.URL)
		if e != nil {
			return false
		}
		defer r.Body.Close()
		b, e := io.ReadAll(r.Body)
		return e == nil && string(b) == payload
	})
	time.Sleep(6 * time.Second)
	if err = node.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	exit := make(chan error, 1)
	go func() { exit <- node.Wait() }()
	select {
	case err = <-exit:
		nodeDone = true
		if err != nil {
			t.Fatal("Agent shutdown", err, base)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Agent settlement timeout", base)
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var up, down int64
	if err = db.QueryRow("SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?", id).Scan(&up, &down); err != nil || up+down < int64(len(payload)) {
		t.Fatal("artifact native accounting", err, up, down, base)
	}
	var sealed int
	if err = db.QueryRow("SELECT COUNT(*) FROM pro_usage_epochs WHERE state='sealed'").Scan(&sealed); err != nil || sealed != 1 {
		t.Fatal("artifact final epoch", err, sealed, base)
	}
	// Compare all representative financial values directly from the actual HUB database.
	for _, before := range balances {
		var volume, unlimited int64
		if err = db.QueryRow("SELECT volume_credit_bytes,unlimited_credit_units FROM representatives WHERE id=?", before.ID).Scan(&volume, &unlimited); err != nil || volume != before.VolumeCreditBytes || unlimited != before.UnlimitedCreditUnits {
			t.Fatal("artifact wallet changed", err)
		}
	}
	t.Logf("INSTALLED_CLI_SUPERVISOR_PASS native=%d historical=14 remaining=%d; actual HUB and Agent binaries, owner API, URI traffic, renewal and SIGTERM settlement", up+down, 999986-up-down)
}
