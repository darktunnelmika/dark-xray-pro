//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageingest"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
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
	"sync"
	"testing"
	"time"
)

func TestRealImportedQuarantineAdmissionAndTraffic(t *testing.T) {
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("requires pinned final TCP core and isolated namespace")
	}
	if binary == "" || os.Geteuid() == 0 {
		t.Fatal("unprivileged pinned core required")
	}
	ctx := context.Background()
	f, evidence, key, _ := preparedCutoverFixture(t)
	template := meterRealityTarget(t)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	target.Listener.Close()
	listener, err := net.Listen("tcp4", "11.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target.Listener = listener
	target.TLS = template.TLS.Clone()
	target.EnableHTTP2 = true
	target.StartTLS()
	defer target.Close()
	plan := NativeInboundPlanInput{EventID: "admission-plan-create-0001", Profile: nativeInput(), Reason: "Isolated imported admission", Nodes: []NativeInboundPlanNode{{NodeID: "admit-node", InstallationID: strings.Repeat("a", 32), ListenIP: "127.0.0.1", DirectPort: meterPort(t), DirectHosts: []string{"127.0.0.1"}}}}
	plan.Profile.ID = 503
	plan.Profile.RealityTarget = target.Listener.Addr().String()
	plan.Profile.ServerNames = []string{"one.example.org"}
	if _, err := f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
		t.Fatal(err)
	}
	delivered, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, evidence.ClientID, DarkRestorePreparedDeliveryInputV1{EventID: "admission-link-deliver-0001", InboundID: 503, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"})
	if err != nil {
		t.Fatal(err)
	}
	before := ownershipBalances(t, f.s)
	stage, err := f.s.DarkRestoreQuarantinePlan(ctx, key, f.owner.Token, evidence.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	sha, _ := stage.Digest()
	pub, signer, _ := ed25519.GenerateKey(nil)
	options := mx.Options{Directory: filepath.Join(t.TempDir(), "runtime"), StateKey: bytes.Repeat([]byte{2}, 32), LeaseKey: bytes.Repeat([]byte{3}, 64), NodeID: stage.NodeID, InstallationID: stage.InstallationID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin, ExperimentalFinalTCP: true}
	runtime, err := mx.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	now := time.Now()
	lease, err := mx.SignLease(options.LeaseKey, mx.Lease{NodeID: stage.NodeID, InstallationID: stage.InstallationID, Sequence: 1, ConfigSHA: stage.ConfigSHA, ManifestSHA: stage.ManifestSHA, Issued: now.UnixNano(), Expires: now.Add(35 * time.Second).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	birth, err := stage.PrepareRuntime(ctx, runtime, sha, lease, signer)
	if err != nil {
		t.Fatal(err)
	}
	registered, err := f.s.RegisterDarkRestoreBirth(ctx, key, f.owner.Token, evidence.ClientID, sha, pub, lease, birth)
	if err != nil {
		t.Fatal(err)
	}
	epoch := registered.Epoch
	principal := usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
	token := strings.Repeat("s", 64)
	handler, err := usageingest.NewWithAdmissionGate(f.s.UsageLedger(), func(_ context.Context, node, install, secret string) (usageledger.Principal, bool) {
		return principal, node == epoch.NodeID && install == epoch.InstallationID && secret == token
	}, f.s.CheckDarkRestoreAdmission)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var last uc.Report
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var report uc.Report
		if r.URL.Path == usageingest.AgentPath && json.Unmarshal(raw, &report) == nil && report.Sequence > 0 {
			mu.Lock()
			last = report
			mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	receiver.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	receiver.StartTLS()
	defer receiver.Close()
	cert := receiver.Certificate()
	digest := sha256.Sum256(cert.Raw)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}}}
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: receiver.URL, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID, Token: token, PinSHA256: hex.EncodeToString(digest[:]), Client: client})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "denied"), Key: bytes.Repeat([]byte{9}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = box.Observe([]uc.Usage{{ClientID: evidence.ClientID}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err = sender.FlushOne(ctx, box); err == nil {
		t.Fatal("zero acknowledged without owner admission")
	}
	box.Close()
	permit, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, evidence.ClientID, sha, "admission-owner-approve-0001")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.AuthorizeDarkRestoreAdmission(ctx, key, f.owner.Token, evidence.ClientID, sha, "admission-owner-approve-0001")
	if err != nil || permit.EventID != replay.EventID {
		t.Fatal("admission replay", err)
	}
	state, err := runtime.ActivateUsageAdmission(ctx, mx.UsageObservation{Epoch: epoch, StatsEndpoint: mx.MeteredStatsEndpoint, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: time.Second, MaxPendingAge: 2 * time.Second})
	if err != nil {
		t.Fatal("native admission", err)
	}
	_ = state
	uri, err := url.Parse(strings.TrimSpace(string(delivered.Subscription.Content)))
	if err != nil {
		t.Fatal(err)
	}
	q := uri.Query()
	socksPort := meterPort(t)
	config := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": uri.Hostname(), "port": int(plan.Nodes[0].DirectPort), "users": []any{map[string]any{"id": uri.User.Username(), "encryption": "none", "flow": q.Get("flow")}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid")}}}}}
	raw, _ := json.Marshal(config)
	path := filepath.Join(t.TempDir(), "client.json")
	os.WriteFile(path, raw, 0600)
	command := exec.Command(binary, "run", "-config", path)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Kill(); command.Wait() }()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	meterWait(t, func() bool {
		c, e := net.DialTimeout("tcp", socks, 50*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	payload := strings.Repeat("imported-real-accounted-", 500)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) }))
	origin.Listener, err = net.Listen("tcp4", "11.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin.Start()
	defer origin.Close()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) { return meterSOCKS(ctx, socks, address) }}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 4 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal("actual imported URI traffic", err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != payload {
		t.Fatal("traffic body", err)
	}
	if _, err = runtime.SettleActiveTCP(ctx, sender); err != nil {
		t.Fatal("final counter seal", err)
	}
	account, err := f.s.ClientUsage(ctx, evidence.ClientID)
	if err != nil || account.Total < int64(len(payload)) {
		t.Fatal("real counters missing", err)
	}
	mu.Lock()
	final := last
	mu.Unlock()
	if !final.Quiesced {
		t.Fatal("missing exact final report")
	}
	receipt, err := f.s.UsageLedger().AcceptChecked(ctx, principal, final, f.s.CheckDarkRestoreAdmission)
	if err != nil || receipt.SHA != final.SHA {
		t.Fatal("final replay", err)
	}
	after, err := f.s.ClientUsage(ctx, evidence.ClientID)
	if err != nil || after.Total != account.Total || !after.RemainingNativeBudgetKnown || after.RemainingNativeBudgetBytes != 999986-after.Total {
		t.Fatal("double billing", err)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("wallet changed")
	}

	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("admitted traffic offline backup", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.ClientUsage(ctx, evidence.ClientID)
	if err != nil || recovered.Total != after.Total {
		t.Fatal("native total after reopen", err)
	}
	t.Logf("IMPORTED_REAL_TRAFFIC_PASS native=%d historical=14 remaining=%d exact replay without extra wallet debit", account.Total, 999986-account.Total)
}
