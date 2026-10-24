package xrayconfig

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These standalone subprocess fixtures cannot enroll a managed runtime or
// authorize a subscription. In particular, VLESS fallbacks dial directly and
// deliberately remain ineligible for UsageBootstrap and final-TCP admission.
func exerciseRealInboundOptions(t *testing.T, core, pin string) {
	t.Run("sniffing", func(t *testing.T) { exerciseRealSniffing(t, core, pin) })
	t.Run("fallbacks", func(t *testing.T) { exerciseRealFallbacks(t, core, pin) })
}

func exerciseRealSniffing(t *testing.T, core, pin string) {
	cases := []struct {
		name    string
		options SniffingOptions
		second  bool
	}{
		{"disabled", SniffingOptions{DestOverride: []string{"http"}}, false},
		{"http-override", SniffingOptions{Enabled: true, DestOverride: []string{"http"}}, true},
		{"route-only", SniffingOptions{Enabled: true, DestOverride: []string{"http"}, RouteOnly: true}, false},
		{"metadata-only", SniffingOptions{Enabled: true, DestOverride: []string{"http"}, MetadataOnly: true}, false},
		{"excluded-domain", SniffingOptions{Enabled: true, DestOverride: []string{"http"}, DomainsExcluded: []string{"full:127.0.0.2"}}, false},
		{"excluded-original-ip", SniffingOptions{Enabled: true, DestOverride: []string{"http"}, IPsExcluded: []string{"127.0.0.1/32"}}, false},
		{"different-excluded-ip", SniffingOptions{Enabled: true, DestOverride: []string{"http"}, IPsExcluded: []string{"127.0.0.2/32"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			target := advancedRealityTarget(t)
			payloadA, payloadB := strings.Repeat("original-local-origin-", 96), strings.Repeat("sniffed-local-origin-", 96)
			var requestsA, requestsB atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestsA.Add(1)
				_, _ = io.WriteString(w, payloadA)
			}))
			defer origin.Close()
			originPort := origin.Listener.Addr().(*net.TCPAddr).Port
			listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.2", strconv.Itoa(originPort)))
			if err != nil {
				t.Fatal(err)
			}
			other := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestsB.Add(1)
				_, _ = io.WriteString(w, payloadB)
			})}}
			other.Start()
			defer other.Close()
			ports := inboundFixturePorts(t)
			serverPort, statsPort, socksPort := ports(), ports(), ports()
			stats := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(statsPort)))
			p, candidate := inboundFixtureCandidate(t, &ProfileOptions{Sniffing: &tc.options}, serverPort, stats)
			var doc map[string]any
			if err := json.Unmarshal(candidate.Config, &doc); err != nil {
				t.Fatal(err)
			}
			inboundFixtureLocalTarget(doc, target.Listener.Addr().String())
			direct := doc["outbounds"].([]any)[0].(map[string]any)
			direct["settings"] = inboundFixtureAllow(originPort, []string{"127.0.0.1/32", "127.0.0.2/32"})
			host := "127.0.0.2"
			if tc.options.RouteOnly {
				// Success requires the sniffed domain to pick this route, while
				// freedom must still dial the ORIGINAL IP. Ignoring routeOnly or
				// skipping sniffing therefore cannot accidentally pass the test.
				// The domain is only a match key and is never resolved.
				host = "sniffed.example.invalid"
				doc["outbounds"] = []any{map[string]any{"tag": "unmatched", "protocol": "blackhole"}, direct}
				doc["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "domain": []string{"full:" + host}, "outboundTag": "direct"}}}
			}
			candidate = inboundFixtureDocument(t, candidate, doc)
			if _, err := ValidateOffline(ctx, candidate, core, pin); err != nil {
				t.Fatal("stock core rejected the local sniffing fixture", err)
			}
			advancedCoreProcess(t, ctx, core, candidate.Config)
			inboundFixtureWaitSocket(t, stats)
			client := inboundFixtureClient(t, p, serverPort, socksPort, false)
			advancedCoreProcess(t, ctx, core, client)
			socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
			inboundFixtureWaitSocket(t, socks)
			body, err := inboundFixtureHTTP(ctx, socks, origin.URL, host)
			want, firstCount, secondCount := payloadA, int64(1), int64(0)
			if tc.second {
				want, firstCount, secondCount = payloadB, 0, 1
			}
			if err != nil || string(body) != want || requestsA.Load() != firstCount || requestsB.Load() != secondCount {
				t.Fatalf("sniffing routed to the wrong local origin: error=%v first=%d second=%d", err, requestsA.Load(), requestsB.Load())
			}
			var up, down int64
			advancedWaitFor(t, func() bool {
				var err error
				up, down, err = advancedCounters(ctx, core, stats, p.Users[0].Email)
				return err == nil && up > 0 && down >= int64(len(want))
			})
			t.Logf("SNIFFING_TRAFFIC_CASE_PASS: case=%s uplink=%d downlink=%d", tc.name, up, down)
		})
	}
	if !t.Failed() {
		t.Log("SNIFFING_TRAFFIC_PASS")
	}
}

func exerciseRealFallbacks(t *testing.T, core, pin string) {
	for _, h2 := range []bool{false, true} {
		name := "http1-target"
		if h2 {
			name = "http2-target"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			target := advancedRealityTargetProtocol(t, h2)
			ports := inboundFixturePorts(t)
			serverPort, rawPort, vlessPort := ports(), ports(), ports()
			defaultSink := newFallbackSink(t, "fallback-default", 0, serverPort)
			pathSink := newFallbackSink(t, "fallback-path", 1, serverPort)
			v2Sink := newFallbackSink(t, "fallback-proxy-v2", 2, serverPort)
			unreachableSink := newFallbackSink(t, "unsupported-alpn-selector", 0, serverPort)
			options := &ProfileOptions{Fallbacks: []Fallback{
				{Dest: defaultSink.address},
				{Path: "/selected", Dest: pathSink.address, Xver: 1},
				{Path: "/proxy-v2", Dest: v2Sink.address, Xver: 2},
			}}
			p, candidate := inboundFixtureCandidate(t, options, serverPort, "")
			var legitimateRequests atomic.Int64
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				legitimateRequests.Add(1)
				_, _ = io.WriteString(w, "legitimate-vless-with-fallbacks")
			}))
			defer origin.Close()
			var doc map[string]any
			if err := json.Unmarshal(candidate.Config, &doc); err != nil {
				t.Fatal(err)
			}
			inboundFixtureLocalTarget(doc, target.Listener.Addr().String())
			// Probe the reason for rejecting ALPN in the authoring schema.
			// These unsupported rows are injected into this disposable raw
			// fixture only: stock REALITY leaves negotiated ALPN empty, even
			// when its camouflage target negotiates HTTP/1.1 or HTTP/2.
			settings := doc["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)
			settings["fallbacks"] = append(settings["fallbacks"].([]any),
				map[string]any{"alpn": "http/1.1", "dest": unreachableSink.address},
				map[string]any{"alpn": "h2", "dest": unreachableSink.address})
			doc["outbounds"].([]any)[0].(map[string]any)["settings"] = inboundFixtureAllow(origin.Listener.Addr().(*net.TCPAddr).Port, []string{"127.0.0.1/32"})
			candidate = inboundFixtureDocument(t, candidate, doc)
			if _, err := ValidateOffline(ctx, candidate, core, pin); err != nil {
				t.Fatal("stock core rejected the local fallback fixture", err)
			}
			advancedCoreProcess(t, ctx, core, candidate.Config)
			server := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(serverPort)))
			inboundFixtureWaitSocket(t, server)
			// Freedom adds REALITY transport but no VLESS header. Its finite
			// plaintext is therefore a real fallback request after authentication.
			advancedCoreProcess(t, ctx, core, inboundFixtureClient(t, p, serverPort, rawPort, true))
			socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(rawPort)))
			inboundFixtureWaitSocket(t, socks)
			for _, check := range []struct {
				path, payload string
				sink          *fallbackSink
			}{{"/unknown", "fallback-default", defaultSink}, {"/selected?query=1", "fallback-path", pathSink}, {"/proxy-v2", "fallback-proxy-v2", v2Sink}} {
				body, err := inboundFixtureHTTP(ctx, socks, "http://"+server+check.path, "one.example.org")
				if err != nil || string(body) != check.payload {
					t.Fatal("fallback selected the wrong path/PROXY target", check.path, err)
				}
				check.sink.require(t, strings.Split(check.path, "?")[0])
			}
			// Legitimate clients must still traverse VLESS and the configured
			// outbound; fallback targets must not consume their protocol header.
			advancedCoreProcess(t, ctx, core, inboundFixtureClient(t, p, serverPort, vlessPort, false))
			legitimateSocks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(vlessPort)))
			inboundFixtureWaitSocket(t, legitimateSocks)
			body, err := inboundFixtureHTTP(ctx, legitimateSocks, origin.URL, "")
			if err != nil || string(body) != "legitimate-vless-with-fallbacks" || legitimateRequests.Load() != 1 {
				t.Fatal("fallback configuration intercepted a valid VLESS client", err)
			}
			if defaultSink.requests.Load() != 1 || pathSink.requests.Load() != 1 || v2Sink.requests.Load() != 1 || unreachableSink.connections.Load() != 0 {
				t.Fatal("an unselected fallback target received traffic")
			}
			t.Logf("FALLBACK_TRAFFIC_CASE_PASS: case=%s", name)
		})
	}
	if !t.Failed() {
		t.Log("FALLBACK_TRAFFIC_PASS")
		t.Log("REALITY_FALLBACK_ALPN_UNSUPPORTED_PASS")
	}
}

func inboundFixturePorts(t *testing.T) func() uint16 {
	t.Helper()
	used := map[uint16]bool{}
	return func() uint16 {
		for {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := uint16(listener.Addr().(*net.TCPAddr).Port)
			_ = listener.Close()
			if !used[port] {
				used[port] = true
				return port
			}
		}
	}
}

func inboundFixtureCandidate(t *testing.T, options *ProfileOptions, port uint16, stats string) (Profile, Candidate) {
	t.Helper()
	p := testProfile(1)
	p.Users = p.Users[:1]
	p.ServerNames = []string{"one.example.org"}
	p.Options = options
	candidate, err := RenderAssigned("isolated-inbound-options", strings.Repeat("b", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: port}})
	if err == nil && stats != "" {
		candidate, err = WithUsageAPI(candidate, stats)
	}
	if err != nil {
		t.Fatal(err)
	}
	return p, candidate
}

func inboundFixtureLocalTarget(doc map[string]any, address string) {
	doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["dest"] = address
}

func inboundFixtureAllow(port int, ips []string) map[string]any {
	return map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": ips, "port": strconv.Itoa(port)}}}
}

func inboundFixtureDocument(t *testing.T, candidate Candidate, doc map[string]any) Candidate {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Config = raw
	digest := sha256.Sum256(raw)
	candidate.SHA256 = hex.EncodeToString(digest[:])
	manifest := sha256.Sum256([]byte("DARK-PRO-TEST-INBOUND-OPTIONS:" + candidate.SHA256))
	candidate.ManifestSHA256 = hex.EncodeToString(manifest[:])
	return candidate
}

func inboundFixtureWaitSocket(t *testing.T, address string) {
	t.Helper()
	advancedWaitFor(t, func() bool {
		conn, err := net.DialTimeout("tcp4", address, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	})
}

func inboundFixtureClient(t *testing.T, p Profile, server, socks uint16, rawReality bool) []byte {
	t.Helper()
	key, err := base64.RawURLEncoding.DecodeString(p.RealityPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().NewPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	outbound := map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{
		"address": "127.0.0.1", "port": server, "users": []any{map[string]any{"id": uuidA, "encryption": "none", "flow": "xtls-rprx-vision"}},
	}}}}
	if rawReality {
		settings := inboundFixtureAllow(int(server), []string{"127.0.0.1/32"})
		settings["redirect"] = net.JoinHostPort("127.0.0.1", strconv.Itoa(int(server)))
		outbound["protocol"], outbound["settings"] = "freedom", settings
	}
	outbound["streamSettings"] = map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{
		"serverName": "one.example.org", "fingerprint": "chrome", "publicKey": base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()), "shortId": p.ShortIDs[0],
	}}
	client := map[string]any{"log": map[string]string{"loglevel": "none"},
		"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": socks, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
		"outbounds": []any{outbound}}
	raw, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func inboundFixtureHTTP(ctx context.Context, socks, url, host string) ([]byte, error) {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		return advancedSOCKS(ctx, socks, address)
	}}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if host != "" {
		request.Host = host
	}
	response, err := (&http.Client{Transport: transport, Timeout: 4 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(io.LimitReader(response.Body, 8192))
}

type fallbackObservation struct {
	path string
	err  error
}

type fallbackSink struct {
	address     string
	connections atomic.Int64
	requests    atomic.Int64
	observed    chan fallbackObservation
}

func newFallbackSink(t *testing.T, payload string, xver int, serverPort uint16) *fallbackSink {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sink := &fallbackSink{address: listener.Addr().String(), observed: make(chan fallbackObservation, 8)}
	var workers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			sink.connections.Add(1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
				r := bufio.NewReader(conn)
				observation := fallbackObservation{}
				defer func() {
					// A regression must never leave test cleanup blocked on a
					// diagnostic channel if the core makes unexpected retries.
					select {
					case sink.observed <- observation:
					default:
					}
				}()
				if observation.err = readFallbackProxy(r, xver, serverPort); observation.err != nil {
					return
				}
				request, err := http.ReadRequest(r)
				if err != nil {
					observation.err = err
					return
				}
				_ = request.Body.Close()
				observation.path = request.URL.Path
				sink.requests.Add(1)
				_, observation.err = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(payload), payload)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		workers.Wait()
	})
	return sink
}

func (s *fallbackSink) require(t *testing.T, path string) {
	t.Helper()
	select {
	case observation := <-s.observed:
		if observation.err != nil || observation.path != path {
			t.Fatalf("fallback target did not receive exact request/PROXY header: path=%s err=%v", observation.path, observation.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("fallback target received no request")
	}
}

func readFallbackProxy(r *bufio.Reader, xver int, serverPort uint16) error {
	switch xver {
	case 0:
		return nil
	case 1:
		line, err := r.ReadString('\n')
		fields := strings.Fields(line)
		if err != nil || len(fields) != 6 || fields[0] != "PROXY" || fields[1] != "TCP4" || fields[2] != "127.0.0.1" || fields[3] != "127.0.0.1" || fields[5] != strconv.Itoa(int(serverPort)) || !strings.HasSuffix(line, "\r\n") {
			return fmt.Errorf("incorrect PROXY v1 loopback header")
		}
		port, err := strconv.ParseUint(fields[4], 10, 16)
		if err != nil || port == 0 {
			return fmt.Errorf("incorrect PROXY v1 source port")
		}
		return nil
	case 2:
		header := make([]byte, 28)
		if _, err := io.ReadFull(r, header); err != nil {
			return err
		}
		if !bytes.Equal(header[:16], []byte("\r\n\r\n\x00\r\nQUIT\n\x21\x11\x00\x0c")) || !net.IP(header[16:20]).Equal(net.IPv4(127, 0, 0, 1)) || !net.IP(header[20:24]).Equal(net.IPv4(127, 0, 0, 1)) || binary.BigEndian.Uint16(header[24:26]) == 0 || binary.BigEndian.Uint16(header[26:28]) != serverPort {
			return fmt.Errorf("incorrect PROXY v2 loopback header")
		}
		return nil
	default:
		return fmt.Errorf("unsupported test PROXY header")
	}
}
