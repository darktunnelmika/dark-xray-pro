package subscriptioncodec

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestNetworkOptionsOwnValuesAndPreserveExplicitDefaults(t *testing.T) {
	for _, value := range []*XHTTPOptions{nil, {}} {
		if got, err := NormalizeXHTTPOptions(value); err != nil || got != nil {
			t.Fatal("empty XHTTP options changed default identity", err)
		}
	}
	for _, value := range []*ClientOptions{nil, {}} {
		if got, err := NormalizeClientOptions(value); err != nil || got != nil {
			t.Fatal("empty client options changed default identity", err)
		}
	}
	x := &XHTTPOptions{Path: "/Case%20Path", Host: "EDGE.example", Mode: "auto", XPaddingBytes: "100-1000"}
	c := &ClientOptions{Fingerprint: "chrome", SpiderX: "/Case%20Path?b=2&a=%2F"}
	xCopy, err := NormalizeXHTTPOptions(x)
	if err != nil || xCopy == x || *xCopy != *x {
		t.Fatal("XHTTP normalization rewrote text or reused caller storage", err)
	}
	cCopy, err := NormalizeClientOptions(c)
	if err != nil || cCopy == c || *cCopy != *c {
		t.Fatal("client normalization rewrote text or reused caller storage", err)
	}
	x.Path, c.SpiderX = "/changed", "/changed"
	if xCopy.Path != "/Case%20Path" || cCopy.SpiderX != "/Case%20Path?b=2&a=%2F" {
		t.Fatal("normalized options follow caller mutation")
	}
}

func TestNetworkOptionsRejectAmbiguousAndUnboundedInput(t *testing.T) {
	for _, path := range []string{"relative", "//host/path", "/with space", "/with\nnewline", "/with\\slash", "/x?query", "/x#fragment", "/bad%escape", "/" + strings.Repeat("x", MaxConnectionPathBytes), string([]byte{'/', 0xff})} {
		if _, err := NormalizeXHTTPOptions(&XHTTPOptions{Path: path}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid XHTTP path accepted: %q", path)
		}
	}
	for _, padding := range []string{"0", "-1", "2-1", "01", "+1", "1-01", "1-2-3", "1.5", "65537", "1-65537", "2147483648", "100-", " 100", "100 "} {
		if _, err := NormalizeXHTTPOptions(&XHTTPOptions{XPaddingBytes: padding}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid padding accepted: %q", padding)
		}
	}
	for _, padding := range []string{"1", "1-1", "100-1000", "65536", "1-65536"} {
		if _, err := NormalizeXHTTPOptions(&XHTTPOptions{XPaddingBytes: padding}); err != nil {
			t.Fatalf("supported padding rejected: %q", padding)
		}
	}
	for _, mode := range []string{"stream-down", "AUTO", "gun", "multi"} {
		if _, err := NormalizeXHTTPOptions(&XHTTPOptions{Mode: mode}); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsupported XHTTP mode accepted")
		}
	}
	for _, host := range []string{"https://host.example", "user@host.example", "host.example/path", "host.example:443", "[host.example]:443", "host.example:0443", "[2001:db8::1]", "2001:db8::1", "host.example\r\nInjected: yes"} {
		if _, err := NormalizeXHTTPOptions(&XHTTPOptions{Host: host}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid XHTTP host accepted: %q", host)
		}
	}
	for _, spider := range []string{"relative", "//host/path", "/x#fragment", "/x\\path", "/bad%", "/?bad=%zz", "/?p=1", "/?%63=1", "/?t=2", "/?i=3", "/?r=4", "/?a=1;b=2", "/" + strings.Repeat("x", MaxConnectionPathBytes)} {
		if _, err := NormalizeClientOptions(&ClientOptions{SpiderX: spider}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid or crawler-control SpiderX accepted: %q", spider)
		}
	}
	for _, fingerprint := range []string{"unsafe", "hellogolang", "unknown", "Chrome", "chrome&security=none"} {
		if _, err := NormalizeClientOptions(&ClientOptions{Fingerprint: fingerprint}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unsupported fingerprint accepted: %q", fingerprint)
		}
	}
}

func TestShareURIKeepsGRPCAndXHTTPClientSettings(t *testing.T) {
	for _, transport := range []string{"grpc", "xhttp"} {
		t.Run(transport, func(t *testing.T) {
			r := codecRoute()
			r.Transport = transport
			r.Client = &ClientOptions{Fingerprint: "firefox", SpiderX: "/client%20path?source=one&next=%2F"}
			if transport == "grpc" {
				r.GRPCService, r.GRPCAuthority, r.GRPCMultiMode = "multi-service", "Authority.Example:8443", true
			} else {
				r.XHTTP = &XHTTPOptions{Path: "/xhttp%20Path", Host: "Host.Example", Mode: "stream-up", XPaddingBytes: "200-400"}
			}
			raw, err := Render([]Route{r}, FormatRaw)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(strings.TrimSuffix(string(raw), "\n"))
			if err != nil {
				t.Fatal(err)
			}
			q, err := url.ParseQuery(u.RawQuery)
			if err != nil || q.Get("fp") != "firefox" || q.Get("spx") != r.Client.SpiderX || q.Has("flow") || q.Get("security") != "reality" {
				t.Fatal("client metadata or protocol settings changed", err)
			}
			if transport == "grpc" {
				if len(q) != 11 || q.Get("serviceName") != r.GRPCService || q.Get("authority") != r.GRPCAuthority || q.Get("mode") != "multi" || q.Has("extra") {
					t.Fatal("gRPC client settings were lost or an extra field leaked")
				}
			} else {
				var extra struct {
					XPaddingBytes string `json:"xPaddingBytes"`
				}
				decoder := json.NewDecoder(strings.NewReader(q.Get("extra")))
				decoder.DisallowUnknownFields()
				if len(q) != 12 || q.Get("path") != r.XHTTP.Path || q.Get("host") != r.XHTTP.Host || q.Get("mode") != r.XHTTP.Mode || decoder.Decode(&extra) != nil || extra.XPaddingBytes != r.XHTTP.XPaddingBytes || q.Has("serviceName") {
					t.Fatal("XHTTP transport or typed padding did not survive the share link")
				}
			}
			encoded, err := Render([]Route{r}, FormatBase64)
			decoded, decodeErr := base64.StdEncoding.Strict().DecodeString(string(encoded))
			if err != nil || decodeErr != nil || !bytes.Equal(raw, decoded) {
				t.Fatal("new settings differ between raw and Base64")
			}
		})
	}
}

func TestShareOptionsDefaultsAndTransportIsolation(t *testing.T) {
	r := codecRoute()
	baseline, err := Render([]Route{r}, FormatRaw)
	if err != nil {
		t.Fatal(err)
	}
	r.Client, r.XHTTP = &ClientOptions{}, &XHTTPOptions{}
	defaults, err := Render([]Route{r}, FormatRaw)
	if err != nil || !bytes.Equal(defaults, baseline) {
		t.Fatal("empty option sections changed historical TCP share bytes")
	}
	for _, mutate := range []func(*Route){
		func(r *Route) { r.GRPCAuthority = "host.example" },
		func(r *Route) { r.GRPCMultiMode = true },
		func(r *Route) { r.XHTTP = &XHTTPOptions{Path: "/wrong-transport"} },
		func(r *Route) { r.Transport = "xhttp"; r.GRPCService = "wrong-service" },
		func(r *Route) { r.Client = &ClientOptions{Fingerprint: "unknown"} },
	} {
		candidate := codecRoute()
		mutate(&candidate)
		if _, err := Render([]Route{candidate}, FormatRaw); !errors.Is(err, ErrInvalid) {
			t.Fatal("cross-transport or invalid client metadata was silently dropped")
		}
	}
}
