package subscriptioncodec

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func codecRoute() Route {
	return Route{
		InboundID: 1, Kind: "direct", Host: "127.0.0.1", Port: 10443,
		Label: "تهران + # ? & % / تست", UUID: "af469f46-e18a-45f4-bf57-c7f649b10db5",
		Transport: "tcp", ServerName: "one.example.org", ShortID: "aa11",
		PublicKey: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32)),
	}
}

func TestRawAndBase64ShareOneCanonicalConsumerDocument(t *testing.T) {
	a := codecRoute()
	b := a
	b.InboundID, b.Kind, b.Host, b.Port, b.Label = 2, "swap", "2001:db8::1", 1185, "مسیر دوم"
	input := []Route{b, a}
	before := append([]Route(nil), input...)
	raw, err := Render(input, FormatRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("render mutated caller routes")
	}
	again, err := Render([]Route{a, b}, FormatRaw)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("input order changed the subscription bytes")
	}
	encoded, err := Render(input, FormatBase64)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(encoded))
	if err != nil || !bytes.Equal(decoded, raw) || bytes.ContainsAny(encoded, "\r\n") {
		t.Fatal("base64 did not encode exactly the raw document")
	}
	if !bytes.HasSuffix(raw, []byte("\n")) || bytes.Contains(raw, []byte("\n\n")) {
		t.Fatal("line framing is ambiguous")
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatal("route count changed")
	}
	seen := map[string]bool{}
	for _, line := range lines {
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "vless" || u.User == nil || u.User.Username() != a.UUID || u.Path != "" || u.RawPath != "" {
			t.Fatal("rendered document is not a VLESS share URI")
		}
		if _, set := u.User.Password(); set {
			t.Fatal("unexpected userinfo password")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q) != 8 {
			t.Fatalf("unexpected query shape: %v", err)
		}
		want := map[string]string{"encryption": "none", "flow": "xtls-rprx-vision", "fp": "chrome", "pbk": a.PublicKey,
			"security": "reality", "sid": "aa11", "sni": "one.example.org", "type": "tcp"}
		for key, value := range want {
			if len(q[key]) != 1 || q.Get(key) != value {
				t.Fatalf("query field %s changed", key)
			}
		}
		seen[u.Hostname()] = true
		if u.Hostname() == "127.0.0.1" && (u.Port() != "10443" || u.Fragment != a.Label) {
			t.Fatal("label escaping or port changed")
		}
		if u.Hostname() == "2001:db8::1" && (u.Host != "[2001:db8::1]:1185" || u.Fragment != b.Label) {
			t.Fatal("IPv6 host was not bracketed")
		}
		if strings.Contains(line, "+") || strings.Contains(line, "تهران") || strings.Contains(line, " ") {
			t.Fatal("URI components were not percent encoded")
		}
	}
	if !seen[a.Host] || !seen[b.Host] {
		t.Fatal("an endpoint disappeared")
	}
}

func TestGRPCUsesServiceNameAndNoVisionFlow(t *testing.T) {
	r := codecRoute()
	r.Transport, r.GRPCService, r.ShortID = "grpc", "native-service_1", ""
	raw, err := Render([]Route{r}, FormatRaw)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(strings.TrimSuffix(string(raw), "\n"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("type") != "grpc" || q.Get("serviceName") != r.GRPCService || q.Get("mode") != "gun" || q.Has("flow") || !q.Has("sid") || q.Get("sid") != "" {
		t.Fatal("gRPC URI disagrees with native stream settings")
	}
}

func TestCanonicalDNSUUIDAndIPv6DetectDuplicateConnections(t *testing.T) {
	a := codecRoute()
	a.Host, a.ServerName, a.UUID = "NODE.EXAMPLE.ORG", "ONE.EXAMPLE.ORG", strings.ToUpper(a.UUID)
	b := a
	b.Host, b.UUID, b.InboundID, b.Label = "node.example.org", strings.ToLower(a.UUID), 2, "alternate label"
	if _, err := Render([]Route{a, b}, FormatRaw); !errors.Is(err, ErrInvalid) {
		t.Fatal("different metadata hid a duplicate connection")
	}
	raw, err := Render([]Route{a}, FormatRaw)
	if err != nil || !bytes.Contains(raw, []byte("@node.example.org:")) || !bytes.Contains(raw, []byte("sni=ONE.EXAMPLE.ORG")) || !bytes.Contains(raw, []byte(strings.ToLower(a.UUID))) {
		t.Fatal("endpoint/UUID canonicalization changed the exact Reality SNI")
	}
	a, b = codecRoute(), codecRoute()
	a.Host, b.Host = "2001:0db8:0:0:0:0:0:1", "2001:db8::1"
	if _, err := Render([]Route{a, b}, FormatRaw); !errors.Is(err, ErrInvalid) {
		t.Fatal("IPv6 spelling hid a duplicate connection")
	}
}

func TestUnsafeAndAmbiguousConsumerRoutesFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Route)
	}{
		{"zero inbound", func(r *Route) { r.InboundID = 0 }},
		{"unsafe numeric ID", func(r *Route) { r.InboundID = 1 << 53 }},
		{"unknown kind", func(r *Route) { r.Kind = "probe" }},
		{"zero port", func(r *Route) { r.Port = 0 }},
		{"UUID URL syntax", func(r *Route) { r.UUID += "@other.example" }},
		{"URL host", func(r *Route) { r.Host = "https://node.example.org" }},
		{"userinfo host", func(r *Route) { r.Host = "user@node.example.org" }},
		{"query host", func(r *Route) { r.Host = "node.example.org?x=1" }},
		{"fragment host", func(r *Route) { r.Host = "node.example.org#x" }},
		{"host with port", func(r *Route) { r.Host = "node.example.org:443" }},
		{"IPv6 brackets", func(r *Route) { r.Host = "[2001:db8::1]" }},
		{"IPv6 zone", func(r *Route) { r.Host = "fe80::1%eth0" }},
		{"unspecified IP", func(r *Route) { r.Host = "0.0.0.0" }},
		{"multicast IP", func(r *Route) { r.Host = "ff02::1" }},
		{"malformed numeric IP", func(r *Route) { r.Host = "127.000.0.1" }},
		{"raw IDN", func(r *Route) { r.Host = "مثال.example" }},
		{"label newline", func(r *Route) { r.Label = "a\nvless://injected" }},
		{"label carriage return", func(r *Route) { r.Label = "a\rb" }},
		{"label Unicode line break", func(r *Route) { r.Label = "a\u2028b" }},
		{"label invalid UTF8", func(r *Route) { r.Label = string([]byte{0xff}) }},
		{"label too long", func(r *Route) { r.Label = strings.Repeat("x", 257) }},
		{"empty label", func(r *Route) { r.Label = " " }},
		{"IP SNI", func(r *Route) { r.ServerName = "127.0.0.1" }},
		{"query SNI", func(r *Route) { r.ServerName = "one.example.org&pbk=other" }},
		{"unknown transport", func(r *Route) { r.Transport = "ws" }},
		{"TCP grpc service", func(r *Route) { r.GRPCService = "unexpected" }},
		{"gRPC missing service", func(r *Route) { r.Transport = "grpc" }},
		{"gRPC invalid service", func(r *Route) { r.Transport, r.GRPCService = "grpc", "service?name" }},
		{"bad key", func(r *Route) { r.PublicKey = "bad" }},
		{"zero key", func(r *Route) { r.PublicKey = base64.RawURLEncoding.EncodeToString(make([]byte, 32)) }},
		{"padded key", func(r *Route) { r.PublicKey += "=" }},
		{"noncanonical key bits", func(r *Route) { r.PublicKey = strings.Repeat("A", 42) + "B" }},
		{"odd short ID", func(r *Route) { r.ShortID = "a" }},
		{"uppercase short ID", func(r *Route) { r.ShortID = "AA11" }},
		{"long short ID", func(r *Route) { r.ShortID = strings.Repeat("a", 18) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := codecRoute()
			tc.change(&r)
			got, err := Render([]Route{r}, FormatRaw)
			if !errors.Is(err, ErrInvalid) || got != nil {
				t.Fatalf("invalid route produced a document: %v", err)
			}
		})
	}
	if got, err := Render(nil, FormatRaw); !errors.Is(err, ErrInvalid) || got != nil {
		t.Fatal("empty subscription was accepted")
	}
	if got, err := Render(make([]Route, MaxRoutes+1), FormatRaw); !errors.Is(err, ErrInvalid) || got != nil {
		t.Fatal("unbounded subscription was accepted")
	}
	if got, err := Render([]Route{codecRoute()}, Format("auto")); !errors.Is(err, ErrFormat) || got != nil {
		t.Fatal("unrecognized format was accepted")
	}
	a, b := codecRoute(), codecRoute()
	b.UUID, b.Port = "11223344-5566-4788-99aa-bbccddeeff00", 1185
	if got, err := Render([]Route{a, b}, FormatRaw); !errors.Is(err, ErrInvalid) || got != nil {
		t.Fatal("multiple customer UUIDs were combined")
	}
}
