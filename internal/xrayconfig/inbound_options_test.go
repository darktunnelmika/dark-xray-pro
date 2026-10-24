package xrayconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixtureSniffing() *SniffingOptions {
	return &SniffingOptions{Enabled: true, DestOverride: []string{"http", "tls", "quic", "fakedns"}, RouteOnly: true,
		DomainsExcluded: []string{"plain.example.org", "full:exact.example.org", "domain:example.org", "keyword:local", "regexp:^test[0-9]+\\.example\\.org$"},
		IPsExcluded:     []string{"192.0.2.10", "192.0.2.4/24", "2001:db8:0:0::1", "2001:db8::/32"}}
}

func fixtureFallbacks() []Fallback {
	return []Fallback{{Dest: "8080"}, {Path: "/ws", Dest: "127.0.0.1:8081", Xver: 1}, {Path: "/proxy-v2", Dest: "[::1]:8082", Xver: 2}}
}

func TestInboundOptionsNormalizeCopyAndPreserveText(t *testing.T) {
	for _, o := range []*ProfileOptions{{Sniffing: &SniffingOptions{}}, {Fallbacks: []Fallback{}}} {
		got, err := NormalizeProfileOptions("tcp", o)
		if err != nil || got != nil {
			t.Fatal("empty new sections must retain historical profile identity", err)
		}
	}
	in := &ProfileOptions{Sniffing: fixtureSniffing(), Fallbacks: fixtureFallbacks()}
	in.Sniffing.Enabled = false
	got, err := NormalizeProfileOptions("tcp", in)
	if err != nil || got == nil || !reflect.DeepEqual(got, in) {
		t.Fatal("valid rule text/order or disabled settings changed", err)
	}
	got.Sniffing.DestOverride[0] = "quic"
	got.Sniffing.DomainsExcluded[0] = "modified"
	got.Sniffing.IPsExcluded[0] = "203.0.113.1"
	got.Fallbacks[0].Dest = "9999"
	if in.Sniffing.DestOverride[0] != "http" || in.Sniffing.DomainsExcluded[0] != "plain.example.org" || in.Sniffing.IPsExcluded[0] != "192.0.2.10" || in.Fallbacks[0].Dest != "8080" {
		t.Fatal("normalization retained mutable caller slices")
	}
	if in.Sniffing.Enabled || got.Sniffing.Enabled {
		t.Fatal("disabled sniffing was enabled by normalization")
	}
}

func TestSniffingRejectsUnsupportedRulesWithSectionError(t *testing.T) {
	tests := map[string]*SniffingOptions{
		"unknown protocol":     {DestOverride: []string{"https"}},
		"duplicate protocol":   {DestOverride: []string{"http", "http"}},
		"external geosite":     {DomainsExcluded: []string{"geosite:cn"}},
		"external domain file": {DomainsExcluded: []string{"ext:private.dat:local"}},
		"invalid regexp":       {DomainsExcluded: []string{"regexp:["}},
		"empty regexp":         {DomainsExcluded: []string{"regexp:"}},
		"invalid full domain":  {DomainsExcluded: []string{"full:bad.example/path"}},
		"rule leading space":   {DomainsExcluded: []string{" example.org"}},
		"control in rule":      {DomainsExcluded: []string{"example\n.org"}},
		"domain rule too long": {DomainsExcluded: []string{strings.Repeat("a", 513)}},
		"too many domains":     {DomainsExcluded: make([]string, 257)},
		"external geoip":       {IPsExcluded: []string{"geoip:private"}},
		"ip zone":              {IPsExcluded: []string{"fe80::1%eth0"}},
		"invalid cidr":         {IPsExcluded: []string{"192.0.2.0/33"}},
		"mapped cidr width":    {IPsExcluded: []string{"::ffff:192.0.2.0/120"}},
		"unsupported negation": {IPsExcluded: []string{"!192.0.2.0/24"}},
		"hostname in ips":      {IPsExcluded: []string{"example.org"}},
		"too many ips":         {IPsExcluded: make([]string, 257)},
	}
	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeProfileOptions("tcp", &ProfileOptions{Sniffing: s})
			var field *ProfileOptionError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &field) || field.Field != "sniffing" || field.Code != "sniffing_invalid" {
				t.Fatal("missing safe sniffing field error", err)
			}
		})
	}
}

func TestFallbacksRejectUnsupportedDestinationsAndShadowing(t *testing.T) {
	tests := map[string][]Fallback{
		"zero port": {{Dest: "0"}}, "port overflow": {{Dest: "65536"}}, "port padding": {{Dest: "080"}}, "port sign": {{Dest: "+80"}},
		"URL": {{Dest: "https://example.org:443"}}, "unix file": {{Dest: "/tmp/socket"}}, "unix abstract": {{Dest: "@socket"}},
		"pseudo serve": {{Dest: "serve-ws-none"}}, "missing port": {{Dest: "example.org"}}, "IPv6 without port": {{Dest: "[::1]"}},
		"bad IPv6 brackets": {{Dest: "[example.org]:443"}}, "bad host": {{Dest: "example.org/path:443"}},
		"path missing slash": {{Dest: "80", Path: "websocket"}}, "path query": {{Dest: "80", Path: "/ws?q=1"}},
		"path fragment": {{Dest: "80", Path: "/ws#one"}}, "path too long": {{Dest: "80", Path: "/" + strings.Repeat("a", 55)}},
		"path space": {{Dest: "80", Path: "/with space"}}, "ALPN uppercase": {{Dest: "80", ALPN: "H2"}},
		"ALPN control": {{Dest: "80", ALPN: "h2\r\n"}}, "ALPN too long": {{Dest: "80", ALPN: strings.Repeat("a", 256)}},
		"REALITY HTTP1 ALPN": {{Dest: "80", ALPN: "http/1.1"}}, "REALITY HTTP2 ALPN": {{Dest: "80", ALPN: "h2"}},
		"REALITY custom ALPN": {{Dest: "80", ALPN: "custom"}},
		"HTTP2 path":          {{Dest: "80", ALPN: "h2", Path: "/ws"}},
		"negative xver":       {{Dest: "80", Xver: -1}}, "unsupported xver": {{Dest: "80", Xver: 3}},
		"duplicate default": {{Dest: "80"}, {Dest: "81"}}, "duplicate selector": {{Dest: "80", Path: "/ws"}, {Dest: "81", Path: "/ws"}},
		"too many": make([]Fallback, 33),
	}
	for name, list := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeProfileOptions("tcp", &ProfileOptions{Fallbacks: list})
			var field *ProfileOptionError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &field) || field.Field != "fallbacks" || field.Code != "fallbacks_invalid" {
				t.Fatal("missing safe fallback field error", err)
			}
		})
	}
	if _, err := NormalizeProfileOptions("grpc", &ProfileOptions{Fallbacks: fixtureFallbacks()}); !errors.Is(err, ErrInvalid) {
		t.Fatal("gRPC fallback accepted")
	}
}

func TestInboundOptionsRenderOnDestinationsOnly(t *testing.T) {
	p := testProfile(1)
	p.Options = &ProfileOptions{Sniffing: fixtureSniffing(), Fallbacks: fixtureFallbacks()}
	check := func(c Candidate) {
		t.Helper()
		var doc struct {
			Inbounds []inbound `json:"inbounds"`
		}
		if err := json.Unmarshal(c.Config, &doc); err != nil {
			t.Fatal(err)
		}
		for _, in := range doc.Inbounds {
			if in.Protocol == "dokodemo-door" {
				if in.Sniffing != nil || len(in.Settings.Fallbacks) > 0 {
					t.Fatal("carrier inherited exit behavior")
				}
				continue
			}
			if !reflect.DeepEqual(optionsFromInbound(in).Sniffing, p.Options.Sniffing) || !reflect.DeepEqual(in.Settings.Fallbacks, p.Options.Fallbacks) {
				t.Fatal("destination lost sniffing or fallback fields")
			}
		}
	}
	c, err := Render(planFor(t, 10443, 11680), []Profile{p})
	if err != nil {
		t.Fatal(err)
	}
	check(c)
	c, err = RenderAssigned("options-test", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443, SwapPort: 11680}})
	if err != nil {
		t.Fatal(err)
	}
	check(c)
	nodes, swaps := creationFixture()
	top, err := RenderCreationTopology(p, nodes, swaps)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range top.Nodes {
		check(node.Candidate)
	}
	if _, err := WithUsageAPI(c, "127.0.0.1:15443"); !errors.Is(err, ErrInvalid) {
		t.Fatal("fallback bypassed pretraffic admission")
	}
	if _, err := LabSubscriptionRoutes(c, uuidA, map[uint64]string{1: "Unsupported fallback"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("fallback widened LAB subscription eligibility")
	}
	// Sniffing bytes may be inspected/prepared, but retain the existing final
	// accounting and consumer proof gates. No new public route is authorized.
	p.Options.Fallbacks = nil
	c, err = RenderAssigned("options-test", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil {
		t.Fatal(err)
	}
	c, err = WithUsageAPI(c, "127.0.0.1:15443")
	if err != nil {
		t.Fatal(err)
	}
	_, raw, _, err := UsageBootstrap(c)
	if err != nil {
		t.Fatal(err)
	}
	var orig, delivered map[string]json.RawMessage
	if json.Unmarshal(c.Config, &orig) != nil || json.Unmarshal(raw, &delivered) != nil || !bytes.Equal(orig["inbounds"], delivered["inbounds"]) {
		t.Fatal("admission discarded sniffing settings")
	}
	if _, err := LabSubscriptionRoutes(c, uuidA, map[uint64]string{1: "Unsupported sniffing"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("sniffing widened LAB subscription grammar")
	}
}

func TestEmptyInboundOptionsKeepHistoricalHashes(t *testing.T) {
	p := testProfile(1)
	c, err := RenderAssigned("options-test", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil {
		t.Fatal(err)
	}
	p.Options = &ProfileOptions{Sniffing: &SniffingOptions{}, Fallbacks: []Fallback{}}
	again, err := RenderAssigned("options-test", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil || !bytes.Equal(c.Config, again.Config) || c.SHA256 != again.SHA256 || c.ManifestSHA256 != again.ManifestSHA256 {
		t.Fatal("cleared sections changed historical candidate identity", err)
	}
}

func exercisePinnedInboundOptionsOffline(t *testing.T) {
	t.Helper()
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	for _, transport := range []string{"tcp", "grpc"} {
		p := testProfile(7)
		p.Transport = transport
		p.Options = &ProfileOptions{Sniffing: fixtureSniffing()}
		if transport == "grpc" {
			p.GRPCService = "dark-options"
		} else {
			p.Options.Fallbacks = fixtureFallbacks()
		}
		nodes, swaps := creationFixture()
		top, err := RenderCreationTopology(p, nodes, swaps)
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range top.Nodes {
			if _, err := ValidateOffline(context.Background(), node.Candidate, binary, pin); err != nil {
				t.Fatalf("pinned core rejected %s sniffing/fallback topology: %v", transport, err)
			}
		}
	}
	t.Log("INBOUND_OPTIONS_OFFLINE_PASS")
}
