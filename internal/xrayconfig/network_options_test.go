package xrayconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNetworkOptionsRenderEveryCustomerListener(t *testing.T) {
	p := testProfile(1)
	p.Transport = "xhttp"
	p.Options = &ProfileOptions{
		XHTTP:  &XHTTPOptions{Path: "/typed%20Path", Host: "Edge.example.org", Mode: "packet-up", XPaddingBytes: "256-512"},
		Client: &ClientOptions{Fingerprint: "firefox", SpiderX: "/client?source=plan"},
	}
	want := *p.Options.XHTTP
	copy, err := NormalizeProfileOptions(p.Transport, p.Options)
	if err != nil || copy == p.Options || copy.XHTTP == p.Options.XHTTP || copy.Client == p.Options.Client {
		t.Fatal("nested options are not independently owned", err)
	}
	copy.XHTTP.Path, copy.Client.SpiderX = "/changed", "/changed"
	if *p.Options.XHTTP != want || p.Options.Client.SpiderX != "/client?source=plan" {
		t.Fatal("normalization aliased nested caller values")
	}
	assert := func(c Candidate) {
		t.Helper()
		if c.CanActivate || !c.RequiresDataPlaneProof || !c.RequiresSubscriptionCommit {
			t.Fatal("new transport compilation granted runtime authority")
		}
		var doc struct {
			Inbounds []inbound `json:"inbounds"`
		}
		if err := json.Unmarshal(c.Config, &doc); err != nil {
			t.Fatal(err)
		}
		for _, in := range doc.Inbounds {
			if in.Protocol == "dokodemo-door" {
				if in.Stream.XHTTP != nil || bytes.Contains(c.Config, []byte(p.RealityPrivateKey)) {
					t.Fatal("relay received customer transport or credentials")
				}
				continue
			}
			got := optionsFromStream(in.Stream)
			if in.Stream.Network != "xhttp" || in.Stream.GRPC != nil || got.XHTTP == nil || *got.XHTTP != want || got.Client != nil {
				t.Fatal("customer XHTTP settings changed or client metadata entered the server")
			}
			for _, user := range in.Settings.Clients {
				if user.Flow != "" {
					t.Fatal("XHTTP inherited TCP Vision flow")
				}
			}
		}
	}
	legacy, err := Render(planFor(t, 10443, 11680), []Profile{p})
	if err != nil {
		t.Fatal(err)
	}
	assert(legacy)
	native, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443, SwapPort: 11680}})
	if err != nil {
		t.Fatal(err)
	}
	assert(native)
	nodes, swaps := creationFixture()
	topology, err := RenderCreationTopology(p, nodes, swaps)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range topology.Nodes {
		assert(node.Candidate)
	}
}

func TestClientMetadataCannotChangeServerCandidate(t *testing.T) {
	for _, transport := range []string{"tcp", "grpc", "xhttp"} {
		t.Run(transport, func(t *testing.T) {
			p := testProfile(1)
			p.Transport = transport
			if transport == "grpc" {
				p.GRPCService = "dark-pro"
			}
			render := func() []Candidate {
				t.Helper()
				legacy, err := Render(planFor(t, 10443, 11680), []Profile{p})
				if err != nil {
					t.Fatal(err)
				}
				native, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443, SwapPort: 11680}})
				if err != nil {
					t.Fatal(err)
				}
				nodes, swaps := creationFixture()
				topology, err := RenderCreationTopology(p, nodes, swaps)
				if err != nil {
					t.Fatal(err)
				}
				out := []Candidate{legacy, native}
				for _, node := range topology.Nodes {
					out = append(out, node.Candidate)
				}
				return out
			}
			baseline := render()
			p.Options = &ProfileOptions{Client: &ClientOptions{Fingerprint: "firefox", SpiderX: "/client-only?source=plan"}}
			if !reflect.DeepEqual(render(), baseline) {
				t.Fatal("client export metadata changed server bytes, digest, manifest or listeners")
			}
		})
	}
}

func TestProfileValidationReportsExactPublicFields(t *testing.T) {
	tests := []struct {
		name, field, code string
		edit              func(*Profile)
	}{
		{"transport", "transport", "transport_invalid", func(p *Profile) { p.Transport = "ws" }},
		{"service", "grpc_service", "grpc_service_invalid", func(p *Profile) { p.Transport = "grpc"; p.GRPCService = "bad service" }},
		{"target", "reality_target", "target_invalid", func(p *Profile) { p.RealityTarget = "bad target" }},
		{"sni", "server_names", "server_names_invalid", func(p *Profile) { p.ServerNames = []string{"duplicate.example", "DUPLICATE.example"} }},
		{"shortid", "short_ids", "short_ids_invalid", func(p *Profile) { p.ShortIDs = []string{"01", "01"} }},
		{"xhttp transport", "options.xhttp", "xhttp_invalid", func(p *Profile) { p.Options = &ProfileOptions{XHTTP: &XHTTPOptions{Path: "/x"}} }},
		{"xhttp path", "options.xhttp.path", "xhttp_path_invalid", func(p *Profile) {
			p.Transport = "xhttp"
			p.Options = &ProfileOptions{XHTTP: &XHTTPOptions{Path: "//invalid"}}
		}},
		{"xhttp host", "options.xhttp.host", "xhttp_host_invalid", func(p *Profile) {
			p.Transport = "xhttp"
			p.Options = &ProfileOptions{XHTTP: &XHTTPOptions{Host: "host/path"}}
		}},
		{"xhttp mode", "options.xhttp.mode", "xhttp_mode_invalid", func(p *Profile) {
			p.Transport = "xhttp"
			p.Options = &ProfileOptions{XHTTP: &XHTTPOptions{Mode: "unknown"}}
		}},
		{"xhttp padding", "options.xhttp.x_padding_bytes", "xhttp_padding_invalid", func(p *Profile) {
			p.Transport = "xhttp"
			p.Options = &ProfileOptions{XHTTP: &XHTTPOptions{XPaddingBytes: "2-1"}}
		}},
		{"fingerprint", "options.client.fingerprint", "client_fingerprint_invalid", func(p *Profile) { p.Options = &ProfileOptions{Client: &ClientOptions{Fingerprint: "unsafe"}} }},
		{"spider", "options.client.spider_x", "client_spider_x_invalid", func(p *Profile) { p.Options = &ProfileOptions{Client: &ClientOptions{SpiderX: "/?p=1"}} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := testProfile(1)
			tc.edit(&p)
			err := ValidateProfile(p)
			var issue *ProfileValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &issue) || issue.Field != tc.field || issue.Code != tc.code || strings.Contains(err.Error(), p.RealityPrivateKey) {
				t.Fatalf("public field error is imprecise or exposed private input: %v", err)
			}
		})
	}
	for _, options := range []*ProfileOptions{
		{GRPCAuthority: "grpc.example.org"}, {GRPCMultiMode: true}, {Fallbacks: []Fallback{{Dest: "127.0.0.1:8080"}}},
	} {
		p := testProfile(1)
		p.Transport, p.Options = "xhttp", options
		if !errors.Is(ValidateProfile(p), ErrInvalid) {
			t.Fatal("unsupported cross-transport option accepted")
		}
	}
}

func TestXHTTPRemainsOutsideAccountingAndLabPublication(t *testing.T) {
	p := testProfile(1)
	p.Transport = "xhttp"
	c, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WithUsageAPI(c, "127.0.0.1:15443"); !errors.Is(err, ErrInvalid) {
		t.Fatal("authoring XHTTP silently widened the pre-traffic accounting gate")
	}
	for _, network := range []string{"tcp", "xhttp"} {
		t.Run(network, func(t *testing.T) {
			candidate := labSubscriptionCandidate(t)
			editLabSubscriptionConfig(t, &candidate, func(doc map[string]any) {
				s := doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)
				s["network"], s["xhttpSettings"] = network, map[string]any{}
			})
			if _, _, _, err := UsageBootstrap(candidate); !errors.Is(err, ErrInvalid) {
				t.Fatal("accounting accepted XHTTP or a smuggled transport section")
			}
			if _, err := LabSubscriptionRoutes(candidate, uuidA, map[uint64]string{11: "first", 22: "second"}); !errors.Is(err, ErrInvalid) {
				t.Fatal("LAB publication silently dropped XHTTP settings")
			}
		})
	}
}
