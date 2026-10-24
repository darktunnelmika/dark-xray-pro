package xrayconfig

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
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

// The mandatory pinned-core parent calls this bounded, disposable fixture.
// Its server, REALITY camouflage target, client and HTTP origin are all on
// loopback. The client is built from our exported URI, so transport metadata
// must survive the codec before it can carry data. This is not an accounting
// admission proof and must not widen UsageBootstrap or publication scope.
func exerciseRealNetworkOptions(t *testing.T, binary, pin string) {
	t.Helper()
	passed := true
	executed := 0
	for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
		passed = t.Run("xhttp-"+mode, func(t *testing.T) {
			executed++
			exerciseNetworkConnection(t, binary, pin, "xhttp", mode, "")
		}) && passed
	}
	if passed && executed == 4 {
		t.Log("XHTTP_REALITY_TRAFFIC_PASS: exported URI carried finite payload in all four modes")
	}
	passed = true
	executed = 0
	for _, mismatch := range []string{"path", "host", "padding"} {
		passed = t.Run("xhttp-wrong-"+mismatch, func(t *testing.T) {
			executed++
			exerciseNetworkConnection(t, binary, pin, "xhttp", "stream-one", mismatch)
		}) && passed
	}
	if passed && executed == 3 {
		t.Log("XHTTP_REALITY_MISMATCH_REJECT_PASS: wrong path, Host and padding reached no origin")
	}
	passed = true
	executed = 0
	for _, mode := range []string{"gun", "multi"} {
		passed = t.Run("grpc-"+mode, func(t *testing.T) {
			executed++
			exerciseNetworkConnection(t, binary, pin, "grpc", mode, "")
		}) && passed
	}
	if passed && executed == 2 {
		t.Log("GRPC_CLIENT_OPTIONS_TRAFFIC_PASS: exported authority and both modes carried finite payload")
	}
}

func exerciseNetworkConnection(t *testing.T, binary, pin, transportName, mode, mismatch string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	target := advancedRealityTarget(t)
	payload := strings.Repeat("typed-network-local-payload-", 256)
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, payload)
	}))
	defer origin.Close()
	port := inboundFixturePorts(t)
	serverPort, socksPort := port(), port()
	p := testProfile(1)
	p.Users, p.ServerNames, p.Transport = p.Users[:1], []string{"one.example.org"}, transportName
	p.Options = &ProfileOptions{Client: &ClientOptions{Fingerprint: "firefox", SpiderX: "/client?source=local-proof"}}
	if transportName == "xhttp" {
		p.Options.XHTTP = &XHTTPOptions{Path: "/typed%20network", Host: "xhttp.example.org", Mode: mode, XPaddingBytes: "256-256"}
	} else {
		p.GRPCService = "typed-network"
		p.Options.GRPCAuthority, p.Options.GRPCMultiMode = "grpc.example.org:443", mode == "multi"
	}
	candidate, err := RenderAssigned("isolated-network", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: serverPort}})
	if err != nil {
		t.Fatal(err)
	}
	// Only this disposable fixture replaces the validated public target with a
	// generated TLS 1.3 loopback origin. No managed state or signed lease exists.
	var server map[string]any
	if json.Unmarshal(candidate.Config, &server) != nil {
		t.Fatal("invalid disposable server JSON")
	}
	in := server["inbounds"].([]any)[0].(map[string]any)
	in["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["dest"] = target.Listener.Addr().String()
	server["outbounds"].([]any)[0].(map[string]any)["settings"] = inboundFixtureAllow(origin.Listener.Addr().(*net.TCPAddr).Port, []string{"127.0.0.1/32"})
	candidate.Config, err = json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(candidate.Config)
	candidate.SHA256 = hex.EncodeToString(digest[:])
	manifest := sha256.Sum256([]byte("DARK-PRO-TEST-NETWORK:" + candidate.SHA256))
	candidate.ManifestSHA256 = hex.EncodeToString(manifest[:])
	if _, err := ValidateOffline(ctx, candidate, binary, pin); err != nil {
		t.Fatal("pinned core rejected isolated network server", err)
	}
	advancedCoreProcess(t, ctx, binary, candidate.Config)
	serverAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(serverPort)))
	advancedWaitFor(t, func() bool { return networkListenerReady(serverAddress) })
	key, err := base64.RawURLEncoding.DecodeString(p.RealityPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().NewPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	route := subscriptioncodec.Route{
		InboundID: p.InboundID, Kind: "direct", Host: "127.0.0.1", Port: serverPort, Label: "isolated network proof", UUID: p.Users[0].UUID,
		Transport: p.Transport, GRPCService: p.GRPCService, GRPCAuthority: p.Options.GRPCAuthority, GRPCMultiMode: p.Options.GRPCMultiMode,
		XHTTP: p.Options.XHTTP, Client: p.Options.Client, ServerName: p.ServerNames[0], ShortID: p.ShortIDs[0],
		PublicKey: base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
	}
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
		query.Set("path", "/wrong-network")
	case "host":
		query.Set("host", "wrong.example.org")
	case "padding":
		query.Set("extra", `{"xPaddingBytes":"1-1"}`)
	}
	uri.RawQuery = query.Encode()
	client := networkClientFromShare(t, uri, socksPort)
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
			t.Fatal("mismatched XHTTP settings carried data to the origin")
		}
		return
	}
	if requestErr != nil || string(body) != payload || requests.Load() != 1 {
		t.Fatal("exported client settings failed to carry the exact finite payload", requestErr)
	}
}

func networkListenerReady(address string) bool {
	conn, err := net.DialTimeout("tcp4", address, 50*time.Millisecond)
	if err == nil {
		_ = conn.Close()
	}
	return err == nil
}

// This is a test-only share-link adapter using the upstream URI field names.
// In particular extra is decoded JSON, not a double-encoded string, and is
// passed to the real core's extra parser rather than copied as a direct field.
func networkClientFromShare(t *testing.T, uri *url.URL, socksPort uint16) []byte {
	t.Helper()
	query, err := url.ParseQuery(uri.RawQuery)
	if err != nil || uri.Scheme != "vless" || uri.User == nil || uri.Hostname() != "127.0.0.1" || query.Get("security") != "reality" || query.Get("encryption") != "none" {
		t.Fatal("invalid exported test URI")
	}
	port, err := strconv.ParseUint(uri.Port(), 10, 16)
	if err != nil || port == 0 {
		t.Fatal("invalid exported test port")
	}
	allowed := map[string]bool{"encryption": true, "fp": true, "pbk": true, "security": true, "sid": true, "sni": true, "type": true, "spx": true, "mode": true}
	reality := map[string]any{"serverName": query.Get("sni"), "fingerprint": query.Get("fp"), "publicKey": query.Get("pbk"), "shortId": query.Get("sid")}
	if query.Has("spx") {
		reality["spiderX"] = query.Get("spx")
	}
	stream := map[string]any{"network": query.Get("type"), "security": "reality", "realitySettings": reality}
	switch query.Get("type") {
	case "xhttp":
		for _, key := range []string{"path", "host", "extra"} {
			allowed[key] = true
		}
		xhttp := map[string]any{"path": query.Get("path"), "mode": query.Get("mode")}
		if query.Has("host") {
			xhttp["host"] = query.Get("host")
		}
		if query.Has("extra") {
			var extra struct {
				XPaddingBytes string `json:"xPaddingBytes"`
			}
			decoder := json.NewDecoder(strings.NewReader(query.Get("extra")))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&extra) != nil || extra.XPaddingBytes == "" || decoder.Decode(&struct{}{}) != io.EOF {
				t.Fatal("non-typed XHTTP extra was exported")
			}
			xhttp["extra"] = json.RawMessage(query.Get("extra"))
		}
		stream["xhttpSettings"] = xhttp
	case "grpc":
		allowed["serviceName"], allowed["authority"] = true, true
		if query.Get("mode") != "gun" && query.Get("mode") != "multi" {
			t.Fatal("invalid gRPC share mode")
		}
		grpc := map[string]any{"serviceName": query.Get("serviceName"), "multiMode": query.Get("mode") == "multi"}
		if query.Has("authority") {
			grpc["authority"] = query.Get("authority")
		}
		stream["grpcSettings"] = grpc
	default:
		t.Fatal("unsupported test transport")
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			t.Fatal("unknown or duplicate exported query field")
		}
	}
	client := map[string]any{
		"log":      map[string]string{"loglevel": "none"},
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
		"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{
			"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": uri.User.Username(), "encryption": "none"}},
		}}}, "streamSettings": stream}},
	}
	raw, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
