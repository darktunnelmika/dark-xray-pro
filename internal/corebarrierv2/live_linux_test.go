//go:build linux

package corebarrierv2_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
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

	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

type coreProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	log  string
}

func nativeBinary(t *testing.T) string {
	t.Helper()
	bin, pin := os.Getenv("DXP_INBOUND_CORE_V2_BINARY"), os.Getenv("DXP_INBOUND_CORE_V2_SHA256")
	if bin == "" && pin == "" {
		t.Skip("set DXP_INBOUND_CORE_V2_BINARY and DXP_INBOUND_CORE_V2_SHA256 for real engine acceptance")
	}
	if bin == "" || len(pin) != 64 {
		t.Fatal("a binary and SHA256 pin are both required")
	}
	f, err := os.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	_ = f.Close()
	if err != nil || fmt.Sprintf("%x", h.Sum(nil)) != pin {
		t.Fatal("native runtime binary digest mismatch")
	}
	return bin
}

func startCore(t *testing.T, bin string, config map[string]any, controlled bool) *coreProcess {
	t.Helper()
	dir := t.TempDir()
	if os.Getenv("DXP_INBOUND_CORE_V2_DEBUG") == "1" {
		config["log"] = map[string]any{"loglevel": "debug"}
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "core.log")
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-config", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "GORACE=halt_on_error=1 atexit_sleep_ms=0"}
	if os.Getenv("DXP_TUN_DNS_NAMESPACE") == "1" {
		// Only the isolated DNS harness supplies these tools. Its private /run
		// contains the test's own system bus, never the host resolver socket.
		tools := os.Getenv("DXP_TUN_DNS_TOOLS")
		if !filepath.IsAbs(tools) {
			t.Fatal("isolated DNS tools path missing")
		}
		cmd.Env[0] = "PATH=" + tools + ":/usr/bin:/bin"
	}
	if controlled {
		cmd.Env = append(cmd.Env, "DARK_PRO_INBOUND_V2=1")
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	p := &coreProcess{cmd: cmd, done: make(chan struct{}), log: logPath}
	go func() { _ = cmd.Wait(); _ = log.Close(); close(p.done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-p.done
		output := processLog(p)
		if strings.Contains(output, "WARNING: DATA RACE") {
			t.Error("native runtime race detector reported a data race")
		}
		if t.Failed() {
			t.Logf("fixture core output: %s", output)
		}
	})
	return p
}

func processLog(p *coreProcess) string {
	// Only public synthetic fixtures are used by this test harness.
	data, _ := os.ReadFile(p.log)
	if len(data) > 12000 {
		data = append(append(data[:6000:6000], []byte("\n... fixture output truncated ...\n")...), data[len(data)-6000:]...)
	}
	return string(data)
}

// Probe sockets close before the fixture cores bind. Remember every returned
// port for this test process so another fixture endpoint cannot select it again.
var allocatedFixturePorts sync.Map

func freePort(t *testing.T) uint16 {
	t.Helper()
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := ln.Addr().(*net.TCPAddr).Port
		udp, err := net.ListenPacket("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err == nil {
			_, alreadyAllocated := allocatedFixturePorts.LoadOrStore(uint16(p), struct{}{})
			_ = udp.Close()
			_ = ln.Close()
			if !alreadyAllocated {
				return uint16(p)
			}
		} else {
			_ = ln.Close()
		}
	}
	t.Fatal("cannot allocate a distinct TCP/UDP fixture port")
	return 0
}

func endpoint(port uint16) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))) }

func query(bin string, port uint16, pattern string, reset bool) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"api", "statsquery", "--server=" + endpoint(port), "--timeout=6", "-pattern", pattern}
	if reset {
		args = append(args, "-reset")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "GORACE=halt_on_error=1 atexit_sleep_ms=0"}
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("stats request: %w: %s", err, raw)
	}
	var response struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	values := map[string]int64{}
	for _, s := range response.Stat {
		if _, duplicate := values[s.Name]; duplicate {
			return nil, fmt.Errorf("duplicate counter %s", s.Name)
		}
		value := strings.Trim(string(s.Value), `"`)
		if value == "" {
			value = "0"
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		values[s.Name] = n
	}
	return values, nil
}

func waitCore(t *testing.T, bin string, api uint16, p *coreProcess) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			t.Fatalf("native core exited: %s", processLog(p))
		default:
		}
		v, err := query(bin, api, barrier.Protocol+"health", false)
		lastErr = err
		if err == nil && v[barrier.Protocol+"engine-ready"] == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("native core never became ready: %v; %s", lastErr, processLog(p))
}

func waitClient(t *testing.T, port uint16, p *coreProcess) {
	t.Helper()
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		select {
		case <-p.done:
			t.Fatalf("client exited: %s", processLog(p))
		default:
		}
		c, err := net.DialTimeout("tcp", endpoint(port), 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("client listener unavailable: %s", processLog(p))
}

func targetAddress(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && ip.IsGlobalUnicast() {
			return ip.String()
		}
	}
	t.Fatal("WireGuard/MASQUE traffic fixtures require an assigned non-loopback IPv4 address")
	return ""
}

func echoTarget(t *testing.T, host string) uint16 {
	return echoTargetTLS(t, host, false)
}

func echoTargetTLS(t *testing.T, host string, encrypted bool) uint16 {
	t.Helper()
	p := freePort(t)
	address := net.JoinHostPort(host, strconv.Itoa(int(p)))
	ln, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{fixtureCertificate(t)}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})
	}
	udp, err := net.ListenPacket("udp4", address)
	if err != nil {
		_ = ln.Close()
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		_ = ln.Close()
		_ = udp.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, e := udp.ReadFrom(b)
			if e != nil {
				return
			}
			_, _ = udp.WriteTo(b[:n], a)
		}
	}()
	return p
}

func socks(t *testing.T, port, target uint16, host string, command byte) (net.Conn, *net.UDPAddr) {
	t.Helper()
	c, err := net.DialTimeout("tcp", endpoint(port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = c.Write([]byte{5, 1, 0})
	if err != nil {
		t.Fatal(err)
	}
	var pair [2]byte
	if _, err = io.ReadFull(c, pair[:]); err != nil || pair != [2]byte{5, 0} {
		t.Fatalf("SOCKS greeting: %v %v", pair, err)
	}
	request := []byte{5, command, 0, 1, 127, 0, 0, 1, byte(target >> 8), byte(target)}
	copy(request[4:8], net.ParseIP(host).To4())
	if command == 3 {
		for i := 4; i < len(request); i++ {
			request[i] = 0
		}
	}
	if _, err = c.Write(request); err != nil {
		t.Fatal(err)
	}
	var header [4]byte
	if _, err = io.ReadFull(c, header[:]); err != nil || header[0] != 5 || header[1] != 0 {
		t.Fatalf("SOCKS request: %v %v", header, err)
	}
	size := 4
	if header[3] == 4 {
		size = 16
	} else if header[3] != 1 {
		t.Fatalf("unexpected SOCKS address type %d", header[3])
	}
	address := make([]byte, size+2)
	if _, err = io.ReadFull(c, address); err != nil {
		t.Fatal(err)
	}
	ip := net.IP(address[:size])
	if ip.IsUnspecified() {
		ip = net.ParseIP("127.0.0.1")
	}
	return c, &net.UDPAddr{IP: ip, Port: int(binary.BigEndian.Uint16(address[size:]))}
}

func sendTCP(t *testing.T, c net.Conn, payload []byte) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(6 * time.Second))
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatal("TCP payload: ", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("TCP payload changed")
	}
}

func sendUDP(t *testing.T, client, target uint16, host string, payload []byte) net.Conn {
	t.Helper()
	control, relay := socks(t, client, 0, host, 3)
	udp, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	_ = udp.SetDeadline(time.Now().Add(6 * time.Second))
	packet := append([]byte{0, 0, 0, 1, 127, 0, 0, 1, byte(target >> 8), byte(target)}, payload...)
	copy(packet[4:8], net.ParseIP(host).To4())
	if _, err = udp.Write(packet); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 65535)
	n, err := udp.Read(b)
	if err != nil {
		t.Fatal("UDP payload: ", err)
	}
	if n < 10 || b[3] != 1 || !bytes.Equal(b[10:n], payload) {
		t.Fatal("UDP payload changed")
	}
	return control
}

func fixtures(t *testing.T) map[string]inboundspec.Spec {
	t.Helper()
	raw, err := os.ReadFile("../inboundspec/testdata/protocols.json")
	if err != nil {
		t.Fatal(err)
	}
	var specs []inboundspec.Spec
	if err = json.Unmarshal(raw, &specs); err != nil {
		t.Fatal(err)
	}
	out := map[string]inboundspec.Spec{}
	for _, s := range specs {
		out[s.Protocol] = s
	}
	return out
}

func withTLSFixturePin(t *testing.T, s inboundspec.Spec) inboundspec.Spec {
	t.Helper()
	if s.Security != "tls" {
		return s
	}
	cert := s.TLS["certificates"].([]any)[0].(map[string]any)["certificate"].([]any)
	var lines []string
	for _, v := range cert {
		lines = append(lines, v.(string))
	}
	block, _ := pem.Decode([]byte(strings.Join(lines, "\n")))
	if block == nil {
		t.Fatal("fixture certificate")
	}
	pin := sha256.Sum256(block.Bytes)
	if s.Client == nil {
		s.Client = map[string]any{}
	}
	s.Client["pinnedPeerCertSha256"] = []any{base64.StdEncoding.EncodeToString(pin[:])}
	return s
}

func fixtureCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	certificate := fixtures(t)["trojan"].TLS["certificates"].([]any)[0].(map[string]any)
	join := func(v any) []byte {
		var lines []string
		for _, line := range v.([]any) {
			lines = append(lines, line.(string))
		}
		return []byte(strings.Join(lines, "\n"))
	}
	pair, err := tls.X509KeyPair(join(certificate["certificate"]), join(certificate["key"]))
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func realityTarget(t *testing.T) string {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{fixtureCertificate(t)}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server.Listener.Addr().String()
}

func proxyClient(t *testing.T, bin string, spec inboundspec.Spec, user inboundspec.User, listen, local uint16, multiplex bool) {
	t.Helper()
	exported, err := inboundspec.ExportClient(spec, "xray", user, inboundspec.Endpoint{Address: "127.0.0.1", Port: listen})
	if err != nil {
		t.Fatal(err)
	}
	var client map[string]any
	if err = json.Unmarshal(exported, &client); err != nil {
		t.Fatal(err)
	}
	if multiplex {
		client["outbounds"].([]any)[0].(map[string]any)["mux"] = map[string]any{"enabled": true, "concurrency": 8, "xudpConcurrency": 8}
	}
	client["log"] = map[string]any{"loglevel": "none"}
	client["inbounds"] = []any{map[string]any{"listen": "127.0.0.1", "port": local, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}}
	cp := startCore(t, bin, client, false)
	waitClient(t, local, cp)
}

func TestPinnedNativeInboundTrafficAndFinalReceipt(t *testing.T) {
	bin := nativeBinary(t)
	type testCase struct {
		name     string
		spec     inboundspec.Spec
		udp      bool
		mux      bool
		innerTLS bool
	}
	var cases []testCase
	for _, protocol := range []string{"vless", "vmess", "trojan", "shadowsocks", "wireguard", "hysteria", "http", "mixed", "masque", "tunnel"} {
		s := fixtures(t)[protocol]
		cases = append(cases, testCase{name: protocol, spec: s, udp: protocol != "http" && protocol != "tunnel"})
	}
	for _, network := range []string{"kcp", "ws", "grpc", "httpupgrade", "xhttp", "xdrive"} {
		s := fixtures(t)["vless"]
		s.Network = network
		if network == "xdrive" {
			s.Stream = map[string]any{"xdriveSettings": map[string]any{"service": "local", "remoteFolder": t.TempDir(), "flushIntervalMs": 5, "pollIntervalMs": 5, "maxPollIntervalMs": 20}}
		}
		cases = append(cases, testCase{name: "transport_" + network, spec: s, udp: true})
	}
	for _, network := range []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"} {
		s := fixtures(t)["vless"]
		s.Network = network
		s.Security = "tls"
		s.TLS = fixtures(t)["trojan"].TLS
		cases = append(cases, testCase{name: "tls_" + network, spec: s, udp: true})
	}
	for _, protocol := range []string{"vless", "vmess", "trojan"} {
		cases = append(cases, testCase{name: protocol + "_mux_xudp", spec: fixtures(t)[protocol], udp: true, mux: true})
	}
	for _, network := range []string{"tcp", "grpc", "xhttp"} {
		s := fixtures(t)["vless"]
		s.Network, s.Security = network, "reality"
		s.Reality = map[string]any{"privateKey": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x12}, 32)), "serverNames": []any{"fixture.example.test"}, "shortIds": []any{"1122334455667788"}}
		s.Client = map[string]any{"fingerprint": "chrome"}
		if network == "tcp" {
			s.Client["flow"] = "xtls-rprx-vision"
		}
		cases = append(cases, testCase{name: "reality_" + network, spec: s, udp: true})
		if network == "tcp" {
			cases = append(cases, testCase{name: "reality_tcp_vision_tls", spec: s, udp: true, innerTLS: true})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := withTLSFixturePin(t, tc.spec)
			if spec.Security == "reality" {
				spec.Reality["target"] = realityTarget(t)
			}
			targetHost := targetAddress(t)
			target, listen, api, clientPort := echoTargetTLS(t, targetHost, tc.innerTLS), freePort(t), freePort(t), freePort(t)
			users := []inboundspec.User{{ID: 1, UUID: "11111111-1111-4111-8111-111111111111", Email: "one@example.test"}, {ID: 2, UUID: "22222222-2222-4222-8222-222222222222", Email: "two@example.test"}}
			bound := users
			if spec.Protocol == "tunnel" {
				bound = nil
				spec.Settings["rewriteAddress"] = targetHost
				spec.Settings["rewritePort"] = int(target)
			}
			rendered, err := inboundspec.Render(spec, "127.0.0.1", listen, "customer", bound)
			if err != nil {
				t.Fatal(err)
			}
			server := map[string]any{
				"log": map[string]any{"loglevel": "none"}, "stats": map[string]any{},
				"policy":    map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true}},
				"api":       map[string]any{"tag": "private-api", "services": []string{"StatsService"}},
				"inbounds":  []any{rendered.Inbound, map[string]any{"tag": barrier.ControlTag, "listen": "127.0.0.1", "port": api, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}},
				"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp,udp", "ip": []string{targetHost + "/32"}, "port": strconv.Itoa(int(target))}}}}},
				"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{barrier.ControlTag}, "outboundTag": "private-api"}}},
			}
			p := startCore(t, bin, server, true)
			waitCore(t, bin, api, p)
			capability, err := query(bin, api, barrier.Protocol+"capabilities", false)
			if err != nil || len(capability) != 1 || capability[barrier.Protocol+"scope/native-inbounds-v2"] != 1 {
				t.Fatal(capability, err)
			}
			health, _ := query(bin, api, barrier.Protocol+"health", false)
			if health[barrier.Protocol+"admission-open"] != 0 {
				t.Fatal("started with admission open")
			}
			// Actual attempted traffic during preparation must not consume counters.
			if c, e := net.DialTimeout("tcp", endpoint(listen), time.Second); e == nil {
				_ = c.SetDeadline(time.Now().Add(100 * time.Millisecond))
				_, _ = c.Write([]byte("unadmitted-probe"))
				var b [1]byte
				_, _ = c.Read(b[:])
				_ = c.Close()
			}
			zero, err := query(bin, api, "", false)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range zero {
				if (strings.HasPrefix(name, "user>>>") || strings.HasPrefix(name, "inbound>>>customer>>>")) && value != 0 {
					t.Fatalf("pre-admission usage %s=%d", name, value)
				}
			}
			activate := strings.Repeat("a", 64)
			v, err := query(bin, api, barrier.Protocol+"activate/"+activate, false)
			if err != nil || v[barrier.Protocol+"activated/"+activate] != 1 {
				t.Fatal(v, err)
			}
			if _, err = query(bin, api, "", true); err == nil {
				t.Fatal("counter reset allowed")
			}
			var c net.Conn
			if spec.Protocol == "tunnel" {
				c, err = net.DialTimeout("tcp", endpoint(listen), 2*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
			} else {
				proxyClient(t, bin, spec, users[0], listen, clientPort, tc.mux)
				c, _ = socks(t, clientPort, target, targetHost, 1)
			}
			payload := bytes.Repeat([]byte("native-runtime-payload/"), 128)
			if tc.innerTLS {
				// Actual inner TLS 1.3 application records trigger Vision's switch
				// to raw copy; a plain echo payload does not exercise that path.
				pair := fixtureCertificate(t)
				certificate, err := x509.ParseCertificate(pair.Certificate[0])
				if err != nil {
					t.Fatal(err)
				}
				roots := x509.NewCertPool()
				roots.AddCert(certificate)
				secure := tls.Client(c, &tls.Config{RootCAs: roots, ServerName: "fixture.example.test", MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13})
				_ = secure.SetDeadline(time.Now().Add(6 * time.Second))
				if err = secure.Handshake(); err != nil {
					t.Fatal("Vision inner TLS handshake: ", err)
				}
				c = secure
				payload = bytes.Repeat([]byte("active-vision-tls-record/"), 8192)
			}
			sendTCP(t, c, payload)
			udpBytes := 0
			if tc.udp {
				datagram := []byte("native-runtime-datagram")
				_ = sendUDP(t, clientPort, target, targetHost, datagram)
				udpBytes = len(datagram)
			}
			live, err := query(bin, api, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if len(bound) > 0 {
				for _, direction := range []string{"uplink", "downlink"} {
					name := "user>>>one@example.test>>>traffic>>>" + direction
					if live[name] < int64(len(payload)+udpBytes) {
						t.Fatalf("missing %s accounting: %v", direction, live)
					}
					if live["user>>>two@example.test>>>traffic>>>"+direction] != 0 {
						t.Fatal("cross-user accounting", live)
					}
				}
			} else if live["inbound>>>customer>>>traffic>>>uplink"] < int64(len(payload)) {
				t.Fatal("missing owner accounting", live)
			}
			connections := []net.Conn{c}
			if len(bound) > 0 && tc.name == spec.Protocol {
				// Both configured identities must work, with the first connection
				// still alive, without charging the second identity to the first.
				secondPort := freePort(t)
				proxyClient(t, bin, spec, users[1], listen, secondPort, false)
				second, _ := socks(t, secondPort, target, targetHost, 1)
				connections = append(connections, second)
				payload2 := bytes.Repeat([]byte("second-user-payload/"), 197)
				sendTCP(t, second, payload2)
				udp2 := 0
				if tc.udp {
					datagram := []byte("second-user-datagram")
					_ = sendUDP(t, secondPort, target, targetHost, datagram)
					udp2 = len(datagram)
				}
				secondLive, err := query(bin, api, "", false)
				if err != nil {
					t.Fatal(err)
				}
				for _, direction := range []string{"uplink", "downlink"} {
					firstName := "user>>>one@example.test>>>traffic>>>" + direction
					secondName := "user>>>two@example.test>>>traffic>>>" + direction
					if secondLive[firstName] != live[firstName] || secondLive[secondName] < int64(len(payload2)+udp2) {
						t.Fatalf("second identity accounting mismatch: first=%v after=%v", live, secondLive)
					}
				}
				live = secondLive
			}
			finalNonce := strings.Repeat("b", 64)
			final, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
			if err != nil {
				t.Fatalf("final failed: %v; %s", err, processLog(p))
			}
			if final[barrier.Protocol+"final/"+finalNonce] != 1 {
				t.Fatal("missing final marker", final)
			}
			for name, value := range live {
				if strings.HasPrefix(name, "user>>>") || strings.HasPrefix(name, "inbound>>>customer>>>") {
					if final[name] < value {
						t.Fatalf("counter lost at final: %s %d < %d", name, final[name], value)
					}
				}
			}
			for _, active := range connections {
				_ = active.SetDeadline(time.Now().Add(time.Second))
				_, _ = active.Write([]byte("after-final"))
				var tail [11]byte
				if n, _ := active.Read(tail[:]); n > 0 {
					t.Fatal("traffic survived final fence")
				}
			}
			replay, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
			if err != nil {
				t.Fatal(err)
			}
			first, _ := json.Marshal(final)
			again, _ := json.Marshal(replay)
			if !bytes.Equal(first, again) {
				t.Fatal("final snapshot changed")
			}
			// An immutable cache alone cannot prove that no untracked producer
			// changed the actual counters after the final read.
			ordinary, err := query(bin, api, "", false)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range ordinary {
				if (strings.HasPrefix(name, "user>>>") || strings.HasPrefix(name, "inbound>>>customer>>>")) && final[name] != value {
					t.Fatalf("counter changed after final: %s=%d, sealed=%d", name, value, final[name])
				}
			}
			if _, err = query(bin, api, barrier.Protocol+"activate/"+activate, false); err == nil {
				t.Fatal("reactivated a finalized process")
			}
			health, err = query(bin, api, barrier.Protocol+"health", false)
			if err != nil || health[barrier.Protocol+"final-sealed"] != 1 || health[barrier.Protocol+"admission-open"] != 0 {
				t.Fatal(health, err)
			}
		})
	}
}
