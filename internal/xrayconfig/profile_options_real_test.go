package xrayconfig

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
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
	"sync/atomic"
	"testing"
	"time"
)

// Called by the existing mandatory pinned-core CI test. Every address that a
// subprocess can reach is an ephemeral loopback fixture; no external target,
// customer, runtime store or managed server is used.
func exerciseRealProfileOptions(t *testing.T, binary, pin string) {
	t.Helper()
	for _, rejected := range []bool{false, true} {
		name := "accepted-version"
		if rejected {
			name = "rejected-version"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			target := advancedRealityTarget(t)
			payload := strings.Repeat("advanced-reality-local-payload-", 256)
			var requests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, payload)
			}))
			defer origin.Close()
			ports := map[uint16]bool{}
			port := func() uint16 {
				for {
					listener, err := net.Listen("tcp4", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					p := uint16(listener.Addr().(*net.TCPAddr).Port)
					_ = listener.Close()
					if !ports[p] {
						ports[p] = true
						return p
					}
				}
			}
			serverPort, statsPort, socksPort := port(), port(), port()
			stats := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(statsPort)))
			p := testProfile(1)
			p.Users = p.Users[:1]
			p.ServerNames = []string{"one.example.org"}
			p.Options = &ProfileOptions{RealityMaxTimeDiff: 60000, RealityMinClientVer: "1.0.0", RealityMaxClientVer: "255.255.255"}
			if rejected {
				p.Options.RealityMinClientVer = ""
				p.Options.RealityMaxClientVer = "0.0.0"
			}
			candidate, err := RenderAssigned("isolated-options", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: serverPort}})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err = WithUsageAPI(candidate, stats)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if json.Unmarshal(candidate.Config, &doc) != nil {
				t.Fatal("invalid disposable server configuration")
			}
			// Test-only local target and exact local HTTP destination allowance.
			in := doc["inbounds"].([]any)[0].(map[string]any)
			in["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["dest"] = target.Listener.Addr().String()
			originPort := origin.Listener.Addr().(*net.TCPAddr).Port
			doc["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{
				"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": strconv.Itoa(originPort),
			}}}
			candidate.Config, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(candidate.Config)
			candidate.SHA256 = hex.EncodeToString(digest[:])
			manifest := sha256.Sum256([]byte("DARK-PRO-TEST-ADVANCED-REALITY:" + candidate.SHA256))
			candidate.ManifestSHA256 = hex.EncodeToString(manifest[:])
			_, admissionPayload, _, err := UsageBootstrap(candidate)
			if err != nil {
				t.Fatal("advanced configuration did not survive the accounting adapter", err)
			}
			var original, admitted map[string]json.RawMessage
			if json.Unmarshal(candidate.Config, &original) != nil || json.Unmarshal(admissionPayload, &admitted) != nil || !bytes.Equal(original["inbounds"], admitted["inbounds"]) {
				t.Fatal("accounting adapter rewrote advanced server bytes")
			}
			if _, err := ValidateOffline(ctx, candidate, binary, pin); err != nil {
				t.Fatal("pinned core rejected the isolated advanced server", err)
			}
			advancedCoreProcess(t, ctx, binary, candidate.Config)
			advancedWaitFor(t, func() bool {
				conn, err := net.DialTimeout("tcp4", stats, 50*time.Millisecond)
				if err == nil {
					_ = conn.Close()
				}
				return err == nil
			})
			key, err := base64.RawURLEncoding.DecodeString(p.RealityPrivateKey)
			if err != nil {
				t.Fatal(err)
			}
			private, err := ecdh.X25519().NewPrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			client := map[string]any{
				"log":      map[string]string{"loglevel": "none"},
				"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
				"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{
					"address": "127.0.0.1", "port": serverPort, "users": []any{map[string]any{"id": uuidA, "encryption": "none", "flow": "xtls-rprx-vision"}},
				}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{
					"serverName": "one.example.org", "fingerprint": "chrome", "publicKey": base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), "shortId": p.ShortIDs[0],
				}}}},
			}
			rawClient, err := json.Marshal(client)
			if err != nil {
				t.Fatal(err)
			}
			advancedCoreProcess(t, ctx, binary, rawClient)
			socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
			advancedWaitFor(t, func() bool {
				conn, err := net.DialTimeout("tcp4", socks, 50*time.Millisecond)
				if err == nil {
					_ = conn.Close()
				}
				return err == nil
			})
			transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
				return advancedSOCKS(ctx, socks, address)
			}}
			defer transport.CloseIdleConnections()
			httpClient := &http.Client{Transport: transport, Timeout: 4 * time.Second}
			response, err := httpClient.Get(origin.URL)
			var body []byte
			if err == nil {
				body, err = io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
				_ = response.Body.Close()
			}
			if rejected {
				if err == nil || string(body) == payload || requests.Load() != 0 {
					t.Fatal("client outside the configured version range reached the HTTP origin")
				}
				t.Log("ADVANCED_REALITY_VERSION_REJECT_PASS")
				return
			}
			if err != nil || string(body) != payload || requests.Load() != 1 {
				t.Fatal("advanced REALITY did not carry the finite local payload", err)
			}
			var up, down int64
			advancedWaitFor(t, func() bool {
				var err error
				up, down, err = advancedCounters(ctx, binary, stats, p.Users[0].Email)
				return err == nil && up > 0 && down >= int64(len(payload))
			})
			upAgain, downAgain, err := advancedCounters(ctx, binary, stats, p.Users[0].Email)
			if err != nil || upAgain < up || downAgain < down {
				t.Fatal("advanced configuration reset or lost user counters", err)
			}
			t.Logf("ADVANCED_REALITY_TRAFFIC_PASS: payload=%d uplink=%d downlink=%d", len(payload), upAgain, downAgain)
		})
	}
}

func advancedRealityTarget(t *testing.T) *httptest.Server {
	return advancedRealityTargetProtocol(t, true)
}

func advancedRealityTargetProtocol(t *testing.T, http2 bool) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "one.example.org"}, DNSNames: []string{"one.example.org"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.EnableHTTP2 = http2
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func advancedCoreProcess(t *testing.T, ctx context.Context, binary string, raw []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private-core.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(run, binary, "run", "-config", path)
	command.Stdout, command.Stderr, command.WaitDelay = io.Discard, io.Discard, 250*time.Millisecond
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
}

func advancedWaitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("isolated advanced-core fixture timed out")
}

func advancedSOCKS(ctx context.Context, socks, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host).To4()
	if err != nil || ip == nil || !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("test destination must be loopback")
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp4", socks)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()
	deadline := time.Now().Add(4 * time.Second)
	if end, exists := ctx.Deadline(); exists && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
	if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	reply := make([]byte, 2)
	if _, err = io.ReadFull(conn, reply); err != nil || !bytes.Equal(reply, []byte{5, 0}) {
		return nil, fmt.Errorf("test SOCKS negotiation failed")
	}
	request := append([]byte{5, 1, 0, 1}, ip...)
	request = append(request, byte(p>>8), byte(p))
	if _, err = conn.Write(request); err != nil {
		return nil, err
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(conn, header); err != nil || header[1] != 0 {
		return nil, fmt.Errorf("test SOCKS route refused")
	}
	length := 0
	switch header[3] {
	case 1:
		length = 6
	case 4:
		length = 18
	default:
		return nil, fmt.Errorf("unexpected test SOCKS bound address")
	}
	if _, err = io.ReadFull(conn, make([]byte, length)); err != nil {
		return nil, err
	}
	ok = true
	return conn, nil
}

func advancedCounters(ctx context.Context, binary, endpoint, email string) (int64, int64, error) {
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(call, binary, "api", "statsquery", "--server="+endpoint, "--timeout=2", "-pattern=user>>>", "-reset=false")
	command.Stderr, command.WaitDelay = io.Discard, 250*time.Millisecond
	raw, err := command.Output()
	if err != nil {
		return 0, 0, err
	}
	var doc struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, 0, err
	}
	var up, down int64
	for _, row := range doc.Stat {
		value, err := strconv.ParseInt(strings.Trim(string(row.Value), "\""), 10, 64)
		if err != nil {
			return 0, 0, err
		}
		switch row.Name {
		case "user>>>" + email + ">>>traffic>>>uplink":
			up = value
		case "user>>>" + email + ">>>traffic>>>downlink":
			down = value
		}
	}
	return up, down, nil
}
