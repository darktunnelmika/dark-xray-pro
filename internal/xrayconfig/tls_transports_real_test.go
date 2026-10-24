package xrayconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

// Called by the mandatory stock-core parent. Each client is constructed from
// the exported URI and trusts only a generated local CA. The test does not use
// allowInsecure, system trust, nonlocal listeners or managed runtime authority.
func exerciseRealTLSTransports(t *testing.T, binary, pin string) {
	material, root := tlsFixtureMaterial(t, nil)
	_, wrongRoot := tlsFixtureMaterial(t, nil)
	passed, executed := true, 0
	for _, transport := range []string{"tcp", "grpc", "xhttp", "ws", "httpupgrade"} {
		passed = t.Run("tls-"+transport, func(t *testing.T) {
			executed++
			exerciseTLSConnection(t, binary, pin, transport, "", material, root, "")
		}) && passed
	}
	for _, variant := range []struct{ transport, mode string }{{"xhttp", "packet-up"}, {"xhttp", "stream-up"}, {"xhttp", "stream-one"}, {"grpc", "gun"}} {
		passed = t.Run("tls-"+variant.transport+"-"+variant.mode, func(t *testing.T) {
			executed++
			exerciseTLSConnection(t, binary, pin, variant.transport, "", material, root, variant.mode)
		}) && passed
	}
	if passed && executed == 9 {
		t.Log("TLS_TRANSPORT_TRAFFIC_PASS: exported URIs carried exact finite payload through all five TLS transports, all XHTTP modes and both gRPC modes")
	}
	passed, executed = true, 0
	for _, transport := range []string{"tcp", "grpc", "xhttp", "ws", "httpupgrade"} {
		passed = t.Run("tls-untrusted-"+transport, func(t *testing.T) {
			executed++
			exerciseTLSConnection(t, binary, pin, transport, "trust", material, wrongRoot, "")
		}) && passed
	}
	passed = t.Run("tls-wrong-sni", func(t *testing.T) {
		executed++
		exerciseTLSConnection(t, binary, pin, "tcp", "sni", material, root, "")
	}) && passed
	if passed && executed == 6 {
		t.Log("TLS_TRUST_REJECT_PASS: wrong CA on every transport and wrong SNI carried no origin traffic")
	}
	passed, executed = true, 0
	for _, transport := range []string{"xhttp", "ws", "httpupgrade"} {
		for _, mismatch := range []string{"path", "host"} {
			passed = t.Run("tls-"+transport+"-wrong-"+mismatch, func(t *testing.T) {
				executed++
				exerciseTLSConnection(t, binary, pin, transport, mismatch, material, root, "")
			}) && passed
		}
	}
	if passed && executed == 6 {
		t.Log("TLS_HTTP_MISMATCH_REJECT_PASS: XHTTP, WebSocket and HTTPUpgrade reject mismatched path and Host")
	}
}

func exerciseTLSConnection(t *testing.T, binary, pin, transportName, mismatch string, material *TLSMaterial, rootPEM, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	payload := strings.Repeat("tls-transport-local-payload-", 256)
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = io.WriteString(w, payload) }))
	defer origin.Close()
	port := inboundFixturePorts(t)
	serverPort, socksPort := port(), port()
	p := tlsFixtureProfile(t, transportName, material)
	p.Users = p.Users[:1]
	if transportName == "xhttp" && mode != "" {
		p.Options.XHTTP.Mode = mode
	}
	if transportName == "grpc" && mode != "" {
		p.Options.GRPCMultiMode = mode == "multi"
	}
	// Sniffing remains generic authoring configuration; prove that carrying it
	// through the TLS candidate does not prevent this exact local HTTP request.
	p.Options.Sniffing = &SniffingOptions{Enabled: true, DestOverride: []string{"http", "tls"}, RouteOnly: true}
	candidate, err := RenderAssigned("isolated-tls", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: serverPort}})
	if err != nil {
		t.Fatal(err)
	}
	var server map[string]any
	if json.Unmarshal(candidate.Config, &server) != nil {
		t.Fatal("invalid isolated TLS server JSON")
	}
	server["outbounds"].([]any)[0].(map[string]any)["settings"] = inboundFixtureAllow(origin.Listener.Addr().(*net.TCPAddr).Port, []string{"127.0.0.1/32"})
	candidate.Config, err = json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(candidate.Config)
	candidate.SHA256 = hex.EncodeToString(digest[:])
	manifest := sha256.Sum256([]byte("DARK-PRO-TEST-TLS:" + candidate.SHA256))
	candidate.ManifestSHA256 = hex.EncodeToString(manifest[:])
	if _, err := ValidateOffline(ctx, candidate, binary, pin); err != nil {
		t.Fatal("pinned core rejected isolated TLS server", err)
	}
	advancedCoreProcess(t, ctx, binary, candidate.Config)
	serverAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(serverPort)))
	advancedWaitFor(t, func() bool { return networkListenerReady(serverAddress) })
	route := subscriptioncodec.Route{InboundID: p.InboundID, Kind: "direct", Host: "127.0.0.1", Port: serverPort, Label: "isolated TLS proof", UUID: p.Users[0].UUID,
		Transport: p.Transport, Security: p.Security, ServerName: p.ServerNames[0], GRPCService: p.GRPCService, GRPCAuthority: p.Options.GRPCAuthority, GRPCMultiMode: p.Options.GRPCMultiMode,
		XHTTP: p.Options.XHTTP, WebSocket: p.Options.WebSocket, HTTPUpgrade: p.Options.HTTPUpgrade, Client: p.Options.Client}
	share, err := subscriptioncodec.Render([]subscriptioncodec.Route{route}, subscriptioncodec.FormatRaw)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(strings.TrimSuffix(string(share), "\n"))
	if err != nil {
		t.Fatal(err)
	}
	query := uri.Query()
	switch mismatch {
	case "path":
		query.Set("path", "/wrong-tls")
	case "host":
		query.Set("host", "wrong.example.org")
	case "sni":
		query.Set("sni", "wrong.example.org")
	}
	uri.RawQuery = query.Encode()
	client := networkClientFromShareWithTLSRoot(t, uri, socksPort, rootPEM)
	advancedCoreProcess(t, ctx, binary, client)
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	advancedWaitFor(t, func() bool { return networkListenerReady(socks) })
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		return advancedSOCKS(ctx, socks, address)
	}}
	defer transport.CloseIdleConnections()
	response, requestErr := (&http.Client{Transport: transport, Timeout: 4 * time.Second}).Get(origin.URL)
	var body []byte
	if requestErr == nil {
		body, requestErr = io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
		_ = response.Body.Close()
	}
	if mismatch != "" {
		if requestErr == nil || len(body) != 0 || requests.Load() != 0 {
			t.Fatal("invalid TLS or HTTP settings reached local origin")
		}
		return
	}
	if requestErr != nil || string(body) != payload || requests.Load() != 1 {
		t.Fatal("TLS share URI failed to carry exact finite payload", requestErr)
	}
}

func exercisePinnedTLSTopologyOffline(t *testing.T, binary, pin string) {
	t.Helper()
	material, _ := tlsFixtureMaterial(t, nil)
	for _, transport := range []string{"tcp", "grpc", "xhttp", "ws", "httpupgrade"} {
		t.Run("tls-"+transport, func(t *testing.T) {
			p := tlsFixtureProfile(t, transport, material)
			nodes, swaps := creationFixture()
			topology, err := RenderCreationTopology(p, nodes, swaps)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range topology.Nodes {
				result, err := ValidateOffline(context.Background(), node.Candidate, binary, pin)
				if err != nil || !result.XraySyntaxValidated || result.CanActivate {
					t.Fatal("pinned core rejected TLS customer/relay topology", err)
				}
			}
		})
	}
}
