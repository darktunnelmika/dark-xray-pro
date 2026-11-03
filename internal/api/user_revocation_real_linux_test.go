//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
)

// Both modes are intentional assertions, not skips: stock demonstrates why an
// API success is insufficient; patched must close A while the SAME B stream
// keeps relaying on the SAME Xray process. This is still a QA supervisor, not
// activation of continuous production Agent admission (which stays 503-gated).
func TestRealCoreUserIsolationContract(t *testing.T) {
	mode := os.Getenv("DARK_PRO_TEST_USER_CLOSE_MODE")
	if mode == "" {
		t.Skip("explicit stock or patched conformance run required")
	}
	if mode != "stock" && mode != "patched" {
		t.Fatal("invalid conformance mode")
	}
	if os.Geteuid() == 0 {
		t.Fatal("core conformance must run unprivileged")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if !filepath.IsAbs(binary) || len(pin) != 64 {
		t.Fatal("explicit pinned core required")
	}
	for _, tc := range []struct {
		name, transport   string
		vision, originTLS bool
	}{
		{"REALITY_Vision_TLS_payload", "tcp", true, true},
		{"REALITY_TCP", "tcp", false, false},
		{"REALITY_gRPC", "grpc", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s := enabledOwner(t)
			if e := s.business.EnableUsageLedger(ctx); e != nil {
				t.Fatal(e)
			}
			key := bytes.Repeat([]byte{7}, 32)
			if e := s.EnableNativeInbounds(key); e != nil {
				t.Fatal(e)
			}
			if _, e := s.business.CreateRepresentative(ctx, "isolation-rep", "Disposable representative"); e != nil {
				t.Fatal(e)
			}
			if _, e := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "isolation-credit-00001", RepresentativeID: "isolation-rep", VolumeDeltaBytes: 1 << 20, Reason: "Synthetic conformance"}); e != nil {
				t.Fatal(e)
			}
			a, e := s.business.CreateClient(ctx, business.NewClient{EventID: "isolation-create-a-001", ClientID: "isolation-a", RepresentativeID: "isolation-rep", Name: "A", DurationDays: 30, VolumeBytes: 1 << 16})
			if e != nil {
				t.Fatal(e)
			}
			b, e := s.business.CreateClient(ctx, business.NewClient{EventID: "isolation-create-b-001", ClientID: "isolation-b", RepresentativeID: "owner", Name: "B", DurationDays: 30, VolumeBytes: 1 << 22})
			if e != nil {
				t.Fatal(e)
			}
			repBefore, e := s.business.Representative(ctx, a.RepresentativeID)
			if e != nil {
				t.Fatal(e)
			}
			ticket, e := s.registry.Issue("isolation-node", "Disposable paired Node")
			if e != nil {
				t.Fatal(e)
			}
			agent := httptest.NewUnstartedServer(s.AgentHandler())
			agent.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			agent.StartTLS()
			defer agent.Close()
			raw, _ := json.Marshal(map[string]string{"pair_code": ticket.PairCode, "node_id": "isolation-node", "installation_id": installID, "agent_token": nodeToken})
			res, e := agent.Client().Post(agent.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(raw))
			if e != nil {
				t.Fatal(e)
			}
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal("enrollment refused", res.StatusCode)
			}
			reality := liveUsageReality(t)
			origin, tlsClient := isolationOrigin(t, reality, tc.originTLS)
			serverPort, statsPort := liveUsagePort(t), liveUsagePort(t)
			for statsPort == serverPort {
				statsPort = liveUsagePort(t)
			}
			if _, e := s.business.CreateInbound(ctx, key, business.InboundInput{ID: 881, Name: "User isolation", Transport: tc.transport, GRPCService: map[bool]string{true: "pro-isolation", false: ""}[tc.transport == "grpc"], RealityTarget: "one.example.org:443", ServerNames: []string{"one.example.org"}, ShortIDs: []string{"aa11"}}); e != nil {
				t.Fatal(e)
			}
			if e := s.business.BindInboundClients(ctx, 881, []string{a.ID, b.ID}, true); e != nil {
				t.Fatal(e)
			}
			if e := s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 881, NodeID: "isolation-node", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: serverPort}); e != nil {
				t.Fatal(e)
			}
			cfg, e := s.business.BuildNodeConfig(ctx, key, "isolation-node", installID)
			if e != nil {
				t.Fatal(e)
			}
			statsAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(statsPort)))
			var doc map[string]any
			if e = json.Unmarshal(cfg.Candidate.Config, &doc); e != nil {
				t.Fatal(e)
			}
			in := doc["inbounds"].([]any)[0].(map[string]any)
			tag := in["tag"].(string)
			settings := in["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
			settings["dest"] = reality.Listener.Addr().String()
			flow := ""
			if tc.vision {
				flow = "xtls-rprx-vision"
			}
			for _, u := range in["settings"].(map[string]any)["clients"].([]any) {
				u.(map[string]any)["flow"] = flow
			}
			// QA-only localhost origin/API exceptions, NEVER production config defaults.
			doc["api"] = map[string]any{"tag": "isolation-test-api", "listen": statsAddress, "services": []string{"StatsService", "HandlerService"}}
			_, op, _ := net.SplitHostPort(origin)
			doc["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": op}}}
			raw, _ = json.Marshal(doc)
			sum := sha256.Sum256(raw)
			cfg.Candidate.Config = raw
			cfg.ConfigSHA = hex.EncodeToString(sum[:])
			cfg.Candidate.SHA256 = cfg.ConfigSHA
			manifest := sha256.Sum256([]byte("TEST-isolation:" + cfg.ConfigSHA))
			cfg.ManifestSHA = hex.EncodeToString(manifest[:])
			cfg.Candidate.ManifestSHA256 = cfg.ManifestSHA
			o := managedxray.Options{Directory: filepath.Join(t.TempDir(), "runtime"), StateKey: bytes.Repeat([]byte{4}, 32), LeaseKey: []byte(nodeToken), NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin}
			runtime, e := managedxray.Open(o)
			if e != nil {
				t.Fatal(e)
			}
			defer runtime.Close()
			lease := liveUsageLease(t, o, cfg.Sequence, cfg.Candidate)
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
			epoch, e := s.business.RegisterNativeUsageEpoch(ctx, cfg, business.UsageBirth{EpochID: strings.Repeat("c", 32), KernelBootID: birth.BootID, PID: birth.PID, StartTicks: birth.StartTicks})
			if e != nil {
				t.Fatal(e)
			}
			profiles, e := s.business.ListInbounds(ctx, key)
			if e != nil {
				t.Fatal(e)
			}
			socksA := isolationClient(t, ctx, binary, a.UUID, profiles[0].PublicKey, tc.transport, flow, serverPort)
			socksB := isolationClient(t, ctx, binary, b.UUID, profiles[0].PublicKey, tc.transport, flow, serverPort)
			connect := func(socks string) net.Conn {
				t.Helper()
				c, e := liveUsageSOCKS(ctx, socks, origin)
				if e != nil {
					t.Fatal(e)
				}
				if tlsClient != nil {
					x := tls.Client(c, tlsClient.Clone())
					if e = x.HandshakeContext(ctx); e != nil {
						c.Close()
						t.Fatal(e)
					}
					c = x
				}
				t.Cleanup(func() { c.Close() })
				return c
			}
			a1, a2, b1 := connect(socksA), connect(socksA), connect(socksB)
			for _, c := range []net.Conn{a1, a2, b1} {
				if e := isolationEcho(c, []byte("established-before-quota")); e != nil {
					t.Fatal(e)
				}
			}
			out, e := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "outbox"), Key: bytes.Repeat([]byte{8}, 32), Epoch: epoch, Create: true})
			if e != nil {
				t.Fatal(e)
			}
			defer out.Close()
			cert := sha256.Sum256(agent.Certificate().Raw)
			sender, e := uc.NewSender(uc.SenderOptions{Endpoint: agent.URL, NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, Token: nodeToken, PinSHA256: hex.EncodeToString(cert[:]), Client: agent.Client()})
			if e != nil {
				t.Fatal(e)
			}
			defer sender.Close()
			// Repeated real observations + durable delivery. No invented counter sample
			// or fake receipt is fed to the ledger. The controller decision below is
			// intentionally test-owned until production admission/recovery is integrated.
			sample := func() error {
				u, e := uc.Collect(ctx, binary, pin, statsAddress, epoch)
				if e != nil {
					return e
				}
				if _, e = out.Observe(u, false); e != nil {
					return e
				}
				for {
					sent, e := sender.FlushOne(ctx, out)
					if e != nil {
						return e
					}
					if !sent {
						return nil
					}
				}
			}
			if e = sample(); e != nil {
				t.Fatal(e)
			}
			initial, e := s.business.ClientUsage(ctx, a.ID)
			if e != nil || initial.Exhausted {
				t.Fatal("bad initial test allowance", e)
			}
			payload := bytes.Repeat([]byte("quota-payload-"), 4000)
			if e = isolationEcho(a1, payload); e != nil {
				t.Fatal(e)
			}
			if mode == "stock" && tc.originTLS {
				// Negative control: this pinned upstream's downlink splice updates
				// counters only on ReadFrom completion. Do not pretend quota already
				// fired. Demonstrate the separate manual-removal gap afterwards.
				for i := 0; i < 3; i++ {
					if e = sample(); e != nil {
						t.Fatal(e)
					}
					time.Sleep(100 * time.Millisecond)
				}
				observed, e := s.business.ClientUsage(ctx, a.ID)
				if e != nil {
					t.Fatal(e)
				}
				if observed.Downlink-initial.Downlink >= int64(len(payload)) {
					t.Fatal("stock deferred-splice control was not reproduced")
				}
				t.Logf("stock Vision/TLS: %d payload bytes echoed but only %d downlink bytes yet observable; no live hard-quota claim", len(payload), observed.Downlink-initial.Downlink)
			} else {
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					if e = sample(); e != nil {
						t.Fatal(e)
					}
					used, e := s.business.ClientUsage(ctx, a.ID)
					if e != nil {
						t.Fatal(e)
					}
					if used.Exhausted {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-ticker.C:
					}
				}
			}
			if mode == "patched" && tc.originTLS {
				live, e := s.business.ClientUsage(ctx, a.ID)
				if e != nil || live.Downlink-initial.Downlink < 32*1024 {
					t.Fatal("Vision downlink was not reported while its connection was still open", e)
				}
			}
			email, _ := uc.CounterEmail(a.ID)
			cmd := exec.CommandContext(ctx, binary, "api", "rmu", "--server="+statsAddress, "--timeout=3", "-tag="+tag, email)
			if output, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("remove API failed: %v %s", e, output)
			}
			// A removed UUID must not create a new working stream in either mode.
			newA, e := liveUsageSOCKS(ctx, socksA, origin)
			if e == nil {
				if tlsClient != nil {
					x := tls.Client(newA, tlsClient.Clone())
					_ = x.SetDeadline(time.Now().Add(2 * time.Second))
					e = x.HandshakeContext(ctx)
					newA = x
				}
				if e == nil {
					e = isolationEcho(newA, []byte("revoked-new-stream"))
				}
				newA.Close()
				if e == nil {
					t.Fatal("removed UUID opened a new working stream")
				}
			}
			if mode == "patched" {
				for _, c := range []net.Conn{a1, a2} {
					c.SetReadDeadline(time.Now().Add(2 * time.Second))
					_, e = c.Read(make([]byte, 1))
					if e == nil {
						t.Fatal("removed user's existing stream stayed open")
					}
					if n, ok := e.(net.Error); ok && n.Timeout() {
						t.Fatal("API success did not close existing user stream")
					}
				}
			} else {
				for _, c := range []net.Conn{a1, a2} {
					if e := isolationEcho(c, []byte("stock-existing-still-relays")); e != nil {
						t.Fatal("stock control no longer reproduces the removal gap", e)
					}
				}
			}
			if e = isolationEcho(b1, []byte("same-B-connection-after-A-removal")); e != nil {
				t.Fatal("B was disrupted or needed reconnect", e)
			}
			still, e := runtime.ObservationProcess()
			if e != nil || still != birth {
				t.Fatal("user isolation restarted or replaced Xray", e)
			}
			if e = sample(); e != nil {
				t.Fatal(e)
			}
			used, e := s.business.ClientUsage(ctx, a.ID)
			if e != nil || !used.Known || ((mode != "stock" || !tc.originTLS) && (!used.Exhausted || used.Eligible)) {
				t.Fatal("quota not committed", e)
			}
			kept, e := s.business.Client(ctx, a.ID)
			if e != nil || kept.UUID != a.UUID {
				t.Fatal("revocation deleted identity", e)
			}
			repAfter, e := s.business.Representative(ctx, a.RepresentativeID)
			if e != nil || repAfter.VolumeCreditBytes != repBefore.VolumeCreditBytes {
				t.Fatal("consumption re-debited sales credit", e)
			}
			t.Logf("mode=%s transport=%s recorded_A=%d: two A streams checked; SAME B stream and SAME core PID preserved; exact Store/Registry/TLS receipts; no deployment", mode, tc.name, used.Total)
		})
	}
}

func isolationEcho(c net.Conn, payload []byte) error {
	if e := c.SetDeadline(time.Now().Add(2 * time.Second)); e != nil {
		return e
	}
	if _, e := io.Copy(c, bytes.NewReader(payload)); e != nil {
		return e
	}
	got := make([]byte, len(payload))
	if _, e := io.ReadFull(c, got); e != nil {
		return e
	}
	if !bytes.Equal(got, payload) {
		return errors.New("echo payload mismatch")
	}
	return nil
}
func isolationOrigin(t *testing.T, reality *httptest.Server, secure bool) (string, *tls.Config) {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var client *tls.Config
	if secure {
		server := reality.TLS.Clone()
		server.NextProtos = nil
		l = tls.NewListener(l, server)
		roots := x509.NewCertPool()
		roots.AddCert(reality.Certificate())
		client = &tls.Config{RootCAs: roots, ServerName: "one.example.org", MinVersion: tls.VersionTLS13}
	}
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	done := make(chan struct{})
	var wg sync.WaitGroup
	go func() {
		defer close(done)
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			connections[c] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_, _ = io.Copy(c, c)
				mu.Lock()
				delete(connections, c)
				mu.Unlock()
			}()
		}
	}()
	t.Cleanup(func() {
		l.Close()
		<-done
		mu.Lock()
		for c := range connections {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return l.Addr().String(), client
}
func isolationClient(t *testing.T, ctx context.Context, binary, uuid, public, transport, flow string, server uint16) string {
	t.Helper()
	port := liveUsagePort(t)
	stream := map[string]any{"network": transport, "security": "reality", "realitySettings": map[string]any{"serverName": "one.example.org", "fingerprint": "chrome", "publicKey": public, "shortId": "aa11"}}
	if transport == "grpc" {
		stream["grpcSettings"] = map[string]any{"serviceName": "pro-isolation"}
	}
	doc := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": server, "users": []any{map[string]any{"id": uuid, "encryption": "none", "flow": flow}}}}}, "streamSettings": stream}}}
	raw, _ := json.Marshal(doc)
	path := filepath.Join(t.TempDir(), "client.json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	child, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(child, binary, "run", "-config", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e := cmd.Start(); e != nil {
		cancel()
		t.Fatal(e)
	}
	t.Cleanup(func() { cancel(); cmd.Wait() })
	address := fmt.Sprintf("127.0.0.1:%d", port)
	liveUsageWait(t, func() bool {
		c, e := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	return address
}
