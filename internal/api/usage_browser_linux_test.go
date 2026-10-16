//go:build linux

package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// A real Chromium client creates the customer through the compiled panel.
// The test supervisor then drives disposable Xray, actual Registry/TLS receipt
// ingestion and the same Store. This is not automatic production admission;
// the normal metered-runtime fence MUST stay enabled until that is finished.
func TestRealBrowserNativeUsageXraySQLiteAndRevocation(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_LIVE_USAGE_BROWSER") != "1" {
		t.Skip("explicit browser + pinned core acceptance only")
	}
	if os.Geteuid() == 0 {
		t.Fatal("test must be unprivileged")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_USAGE_BROWSER_SCRIPT")
	if !filepath.IsAbs(binary) || len(pin) != 64 || !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("explicit core pin and absolute UI/test paths required")
	}
	if _, e := os.Stat(filepath.Join(webRoot, "index.html")); e != nil {
		t.Fatal("compiled UI required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	s := enabledOwner(t)
	if e := s.business.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	key := bytes.Repeat([]byte{7}, 32)
	if e := s.EnableNativeInbounds(key); e != nil {
		t.Fatal(e)
	}
	owner := s.Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/", owner)
	mux.Handle("/", http.FileServer(http.Dir(webRoot)))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	private := t.TempDir()
	marker := filepath.Join(private, "receipt.json")
	control := filepath.Join(private, "browser-control.json")
	controlJSON, _ := json.Marshal(map[string]string{"username": ownerUsername, "password": ownerPassword, "receipt_marker": marker})
	if e := os.WriteFile(control, controlJSON, 0600); e != nil {
		t.Fatal(e)
	}
	browser := exec.CommandContext(ctx, "node", script)
	browser.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+control)
	output, e := browser.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	var browserErrors liveUsageSafeBuffer
	browser.Stderr = &browserErrors
	if e = browser.Start(); e != nil {
		t.Fatal(e)
	}
	messages := make(chan string, 8)
	browserExit := make(chan error, 1)
	browserDone := make(chan struct{})
	go func() {
		scan := bufio.NewScanner(output)
		for scan.Scan() {
			messages <- scan.Text()
		}
		browserExit <- browser.Wait()
		close(messages)
		close(browserDone)
	}()
	defer func() {
		_ = browser.Process.Kill()
		select {
		case <-browserDone:
		case <-time.After(3 * time.Second):
		}
	}()
	waitMessage := func(expected string) {
		t.Helper()
		for {
			select {
			case <-ctx.Done():
				t.Fatalf("browser acceptance deadline: %s", browserErrors.String())
			case line, ok := <-messages:
				if !ok {
					t.Fatalf("browser exited before %s: %s", expected, browserErrors.String())
				}
				if line == expected {
					return
				}
			}
		}
	}
	waitMessage("NATIVE_UI_CUSTOMER_CREATED")
	customer, e := s.business.Client(ctx, "live-usage-client")
	if e != nil {
		t.Fatal(e)
	}
	if customer.RepresentativeID != "live-usage-rep" || customer.VolumeBytes != 10737418 {
		t.Fatalf("UI failed to persist intended native customer: volume=%d", customer.VolumeBytes)
	}
	originalUUID := customer.UUID
	repBefore, e := s.business.Representative(ctx, customer.RepresentativeID)
	if e != nil {
		t.Fatal(e)
	}

	ticket, e := s.registry.Issue("live-usage-node", "Disposable usage acceptance Node")
	if e != nil {
		t.Fatal(e)
	}
	agent := httptest.NewUnstartedServer(s.AgentHandler())
	agent.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	agent.StartTLS()
	defer agent.Close()
	enrollment, _ := json.Marshal(map[string]string{"pair_code": ticket.PairCode, "node_id": "live-usage-node", "installation_id": installID, "agent_token": nodeToken})
	enrollmentResponse, e := agent.Client().Post(agent.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(enrollment))
	if e != nil {
		t.Fatal(e)
	}
	enrollmentResponse.Body.Close()
	if enrollmentResponse.StatusCode != 200 {
		t.Fatal("real TLS Registry enrollment rejected", enrollmentResponse.StatusCode)
	}

	reality := liveUsageReality(t)
	payload := bytes.Repeat([]byte("DARK-PRO-REAL-USAGE-"), 900000)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer origin.Close()
	port, statsPort, socksPort := liveUsagePort(t), liveUsagePort(t), liveUsagePort(t)
	for statsPort == port {
		statsPort = liveUsagePort(t)
	}
	for socksPort == port || socksPort == statsPort {
		socksPort = liveUsagePort(t)
	}
	if _, e = s.business.CreateInbound(ctx, key, business.InboundInput{ID: 777, Name: "Live browser acceptance", Transport: "tcp", RealityTarget: "one.example.org:443", ServerNames: []string{"one.example.org"}, ShortIDs: []string{"aa11"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.business.BindInboundClients(ctx, 777, []string{customer.ID}, true); e != nil {
		t.Fatal(e)
	}
	if e = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 777, NodeID: "live-usage-node", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: port}); e != nil {
		t.Fatal(e)
	}
	cfg, e := s.business.BuildNodeConfig(ctx, key, "live-usage-node", installID)
	if e != nil {
		t.Fatal(e)
	}
	statsAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(statsPort)))
	testSeal := func(c xrayconfig.Candidate) xrayconfig.Candidate {
		var doc map[string]any
		if json.Unmarshal(c.Config, &doc) != nil {
			t.Fatal("invalid native candidate")
		}
		r := doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
		r["dest"] = reality.Listener.Addr().String()
		r["serverNames"] = []string{"one.example.org"}
		// These LOCAL API/origin exceptions exist only in test code, never renderer.
		doc["api"] = map[string]any{"tag": "test-meter", "listen": statsAddress, "services": []string{"StatsService"}}
		doc["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": strconv.Itoa(origin.Listener.Addr().(*net.TCPAddr).Port)}}}
		raw, e := json.Marshal(doc)
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(raw)
		c.Config = raw
		c.SHA256 = hex.EncodeToString(sum[:])
		m := sha256.Sum256([]byte("TEST-live-browser-usage:" + c.SHA256))
		c.ManifestSHA256 = hex.EncodeToString(m[:])
		return c
	}
	cfg.Candidate = testSeal(cfg.Candidate)
	cfg.ConfigSHA = cfg.Candidate.SHA256
	cfg.ManifestSHA = cfg.Candidate.ManifestSHA256
	options := managedxray.Options{Directory: filepath.Join(private, "runtime"), StateKey: bytes.Repeat([]byte{2}, 32), LeaseKey: []byte(nodeToken), NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin}
	runtime, e := managedxray.Open(options)
	if e != nil {
		t.Fatal(e)
	}
	defer runtime.Close()
	lease := liveUsageLease(t, options, cfg.Sequence, cfg.Candidate)
	if _, e = runtime.Apply(ctx, managedxray.Revision{Sequence: cfg.Sequence, Candidate: cfg.Candidate}, lease); e != nil {
		t.Fatal(e)
	}
	if e = s.business.RecordNativeDelivery(ctx, cfg, lease); e != nil {
		t.Fatal(e)
	}
	birth, e := runtime.ObservationProcess()
	if e != nil {
		t.Fatal(e)
	}
	epoch, e := s.business.RegisterNativeUsageEpoch(ctx, cfg, business.UsageBirth{EpochID: strings.Repeat("b", 32), KernelBootID: birth.BootID, PID: birth.PID, StartTicks: birth.StartTicks})
	if e != nil {
		t.Fatal(e)
	}
	profiles, e := s.business.ListInbounds(ctx, key)
	if e != nil {
		t.Fatal(e)
	}
	clientDoc := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
		"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": customer.UUID, "encryption": "none", "flow": "xtls-rprx-vision"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": "one.example.org", "fingerprint": "chrome", "publicKey": profiles[0].PublicKey, "shortId": "aa11"}}}}}
	raw, _ := json.Marshal(clientDoc)
	clientPath := filepath.Join(private, "client.json")
	if e = os.WriteFile(clientPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()
	coreClient := exec.CommandContext(clientCtx, binary, "run", "-config", clientPath)
	coreClient.Stdout = io.Discard
	coreClient.Stderr = io.Discard
	if e = coreClient.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { clientCancel(); _ = coreClient.Wait() }()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	liveUsageWait(t, func() bool {
		conn, e := net.DialTimeout("tcp4", socks, 100*time.Millisecond)
		if e == nil {
			conn.Close()
		}
		return e == nil
	})
	fetch := func() (int, error) {
		transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return liveUsageSOCKS(ctx, socks, address)
		}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 8 * time.Second}
		res, e := client.Get(origin.URL)
		if e != nil {
			return 0, e
		}
		defer res.Body.Close()
		body, e := io.ReadAll(io.LimitReader(res.Body, 32<<20))
		if !bytes.Equal(body, payload) && e == nil {
			return len(body), errors.New("payload mismatch")
		}
		return len(body), e
	}
	count, e := fetch()
	if e != nil || count != len(payload) {
		t.Fatal("real VLESS/REALITY transfer failed", e)
	}
	var sample []uc.Usage
	liveUsageWait(t, func() bool {
		values, e := uc.Collect(ctx, binary, pin, statsAddress, epoch)
		if e == nil && len(values) == 1 && values[0].Downlink >= int64(len(payload)) {
			sample = values
			return true
		}
		return false
	})
	box, e := uc.Open(uc.Options{Directory: filepath.Join(private, "outbox"), Key: bytes.Repeat([]byte{4}, 32), Epoch: epoch, Create: true})
	if e != nil {
		t.Fatal(e)
	}
	defer box.Close()
	if _, e = box.Observe(sample, false); e != nil {
		t.Fatal(e)
	}
	certPin := sha256.Sum256(agent.Certificate().Raw)
	sender, e := uc.NewSender(uc.SenderOptions{Endpoint: agent.URL, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID, Token: nodeToken, PinSHA256: hex.EncodeToString(certPin[:]), Client: agent.Client()})
	if e != nil {
		t.Fatal(e)
	}
	defer sender.Close()
	if sent, e := sender.FlushOne(ctx, box); e != nil || !sent {
		t.Fatal("Registry-authenticated TLS report commit failed", e)
	}
	recorded, e := s.business.ClientUsage(ctx, customer.ID)
	if e != nil || recorded.Total != sample[0].Uplink+sample[0].Downlink || !recorded.Exhausted || recorded.Eligible {
		t.Fatal("actual native ledger mismatch", e)
	}
	next, e := s.business.BuildNodeConfig(ctx, key, cfg.NodeID, cfg.InstallationID)
	if e != nil || next.UserCount != 0 {
		t.Fatal("exhausted user kept in next config", e)
	}
	next.Candidate = testSeal(next.Candidate)
	if _, e = runtime.Apply(ctx, managedxray.Revision{Sequence: next.Sequence, Candidate: next.Candidate}, liveUsageLease(t, options, next.Sequence, next.Candidate)); e != nil {
		t.Fatal(e)
	}
	if n, e := fetch(); e == nil && n == len(payload) {
		t.Fatal("revoked credential still passed data")
	}
	kept, e := s.business.Client(ctx, customer.ID)
	if e != nil || kept.UUID != originalUUID {
		t.Fatal("metering changed native identity", e)
	}
	repAfter, e := s.business.Representative(ctx, customer.RepresentativeID)
	if e != nil || repAfter.VolumeCreditBytes != repBefore.VolumeCreditBytes {
		t.Fatal("usage double charged representative sales credit", e)
	}
	evidence, _ := json.Marshal(map[string]string{"total": strconv.FormatInt(recorded.Total, 10)})
	if e = os.WriteFile(marker, evidence, 0600); e != nil {
		t.Fatal(e)
	}
	waitMessage("NATIVE_UI_COMMITTED_USAGE_VERIFIED")
	if e = <-browserExit; e != nil {
		t.Fatalf("browser failed: %s", browserErrors.String())
	}
	t.Logf("LIVE UI + real Registry TLS + Xray + project modernc: customer created via browser; %d actual recorded bytes; same UUID revoked; UI exact total and exhausted state verified; no production deployment", recorded.Total)
}
func liveUsagePort(t *testing.T) uint16 {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}
func liveUsageWait(t *testing.T, fn func() bool) {
	t.Helper()
	end := time.Now().Add(6 * time.Second)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatal("bounded live test readiness failed")
}
func liveUsageLease(t *testing.T, o managedxray.Options, seq uint64, c xrayconfig.Candidate) managedxray.Lease {
	t.Helper()
	now := time.Now()
	l, e := managedxray.SignLease(o.LeaseKey, managedxray.Lease{NodeID: o.NodeID, InstallationID: o.InstallationID, Sequence: seq, ConfigSHA: c.SHA256, ManifestSHA: c.ManifestSHA256, Issued: now.UnixNano(), Expires: now.Add(40 * time.Second).UnixNano()})
	if e != nil {
		t.Fatal(e)
	}
	return l
}

func liveUsageReality(t *testing.T) *httptest.Server {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(100), Subject: pkix.Name{CommonName: "one.example.org"}, DNSNames: []string{"one.example.org"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	kb, e := x509.MarshalECPrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	pair, e := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}
func liveUsageSOCKS(ctx context.Context, socks, target string) (net.Conn, error) {
	c, e := (&net.Dialer{}).DialContext(ctx, "tcp4", socks)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	deadline := time.Now().Add(4 * time.Second)
	if d, yes := ctx.Deadline(); yes && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	if _, e = c.Write([]byte{5, 1, 0}); e != nil {
		return nil, e
	}
	answer := make([]byte, 2)
	if _, e = io.ReadFull(c, answer); e != nil {
		return nil, e
	}
	if !bytes.Equal(answer, []byte{5, 0}) {
		return nil, errors.New("test SOCKS connection refused")
	}
	host, ps, e := net.SplitHostPort(target)
	if e != nil {
		return nil, e
	}
	port, e := strconv.Atoi(ps)
	if e != nil {
		return nil, e
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return nil, business.ErrInvalid
	}
	request := append([]byte{5, 1, 0, 1}, ip...)
	request = append(request, byte(port>>8), byte(port))
	if _, e = c.Write(request); e != nil {
		return nil, e
	}
	header := make([]byte, 4)
	if _, e = io.ReadFull(c, header); e != nil {
		return nil, e
	}
	if header[1] != 0 {
		return nil, errors.New("test SOCKS connection refused")
	}
	size := 0
	switch header[3] {
	case 1:
		size = 6
	case 4:
		size = 18
	case 3:
		b := make([]byte, 1)
		if _, e = io.ReadFull(c, b); e != nil {
			return nil, e
		}
		size = int(b[0]) + 2
	default:
		return nil, errors.New("test SOCKS connection refused")
	}
	if _, e = io.ReadFull(c, make([]byte, size)); e != nil {
		return nil, e
	}
	ok = true
	return c, nil
}

type liveUsageSafeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *liveUsageSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *liveUsageSafeBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
