package xrayconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func fullProfileOptions(transport string) *ProfileOptions {
	o := &ProfileOptions{RealityXver: 2, RealityMaxTimeDiff: 60000,
		RealityMinClientVer: "1.0.0", RealityMaxClientVer: "255.255.255", RealityShow: true}
	if transport == "grpc" {
		o.GRPCAuthority, o.GRPCMultiMode = "grpc.example.org:443", true
	}
	return o
}

func TestProfileOptionsNormalizationAndBounds(t *testing.T) {
	for _, options := range []*ProfileOptions{nil, {}} {
		got, err := NormalizeProfileOptions("tcp", options)
		if err != nil || got != nil {
			t.Fatal("empty advanced options changed the legacy profile representation")
		}
	}
	original := fullProfileOptions("grpc")
	copy, err := NormalizeProfileOptions("grpc", original)
	if err != nil || copy == original || !reflect.DeepEqual(copy, original) {
		t.Fatal("normalized options are invalid or still owned by the caller")
	}
	copy.GRPCAuthority = "changed.example.org"
	if original.GRPCAuthority == copy.GRPCAuthority {
		t.Fatal("normalization alias allowed post-validation mutation")
	}
	for _, authority := range []string{"example.org", "localhost", "192.0.2.10:65535", "[2001:db8::1]", "[2001:db8::1]:443"} {
		if _, err := NormalizeProfileOptions("grpc", &ProfileOptions{GRPCAuthority: authority}); err != nil {
			t.Errorf("valid authority %q rejected", authority)
		}
	}
	for _, maxTime := range []int64{0, 1, MaxRealityTimeDiffMilliseconds} {
		if _, err := NormalizeProfileOptions("tcp", &ProfileOptions{RealityMaxTimeDiff: maxTime}); err != nil {
			t.Fatal("representable millisecond setting rejected")
		}
	}
	invalid := []struct {
		name      string
		transport string
		options   ProfileOptions
	}{
		{"negative xver", "tcp", ProfileOptions{RealityXver: -1}},
		{"unsupported xver", "tcp", ProfileOptions{RealityXver: 3}},
		{"negative time", "tcp", ProfileOptions{RealityMaxTimeDiff: -1}},
		{"duration overflow", "tcp", ProfileOptions{RealityMaxTimeDiff: MaxRealityTimeDiffMilliseconds + 1}},
		{"short version", "tcp", ProfileOptions{RealityMinClientVer: "1.2"}},
		{"long version", "tcp", ProfileOptions{RealityMaxClientVer: "1.2.3.4"}},
		{"component overflow", "tcp", ProfileOptions{RealityMinClientVer: "256.0.0"}},
		{"component sign", "tcp", ProfileOptions{RealityMinClientVer: "+1.2.3"}},
		{"component padding", "tcp", ProfileOptions{RealityMaxClientVer: "1.02.3"}},
		{"range reversed", "tcp", ProfileOptions{RealityMinClientVer: "26.10.1", RealityMaxClientVer: "26.9.9"}},
		{"tcp authority", "tcp", ProfileOptions{GRPCAuthority: "example.org"}},
		{"tcp multimode", "tcp", ProfileOptions{GRPCMultiMode: true}},
		{"transport mismatch", "ws", ProfileOptions{RealityShow: true}},
	}
	for _, authority := range []string{"https://example.org", "example.org/path", "user@example.org", "example.org\r\nX:1", " example.org", "example.org:0", "example.org:65536", "example.org:0443", "[::1]:x", "[bad]", "[example.org]:443", "[192.0.2.10]:443", "2001:db8::1", "a..example.org", strings.Repeat("a", 64) + ".org"} {
		invalid = append(invalid, struct {
			name      string
			transport string
			options   ProfileOptions
		}{"authority " + authority, "grpc", ProfileOptions{GRPCAuthority: authority}})
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeProfileOptions(tc.transport, &tc.options); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid option accepted")
			}
			p := testProfile(1)
			p.Transport, p.Options = tc.transport, &tc.options
			if tc.transport == "grpc" {
				p.GRPCService = "dark-pro"
			}
			if _, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}}); !errors.Is(err, ErrInvalid) {
				t.Fatal("renderer bypassed advanced validation")
			}
		})
	}
}

func TestProfileOptionsPreserveHistoricalDefaultCandidates(t *testing.T) {
	// Digests independently obtained by running the same fixtures with the
	// unmodified PR #104 head 259def9, before advanced options were introduced.
	golden := map[string][4]string{
		"tcp":  {"3e71b44c0b984786f7b741db1081167476eae941b01d93ecece4427a0254839b", "dbc7ae70c1e9074212d2996872a1438a26dbbb8ef47d9933bb2fb2ccf606fba6", "e58f2f26743bec8215418a007b1a917dc129666b01750e085c028cd4d88d479f", "6cd059d20d1b741536cd86dba92bbf7ae58d3e1032b9ac9061a8c23ff2983c73"},
		"grpc": {"16b9d486a1f3090eadc10452287d12ec9d02544d420e7e1ffde49619a19b4e31", "51bca245287b0e7424007ef673d740211925bb584c8176285e312a18c76f0b3f", "a0c3338827846c232119f35dff22da82d9b6a2d8aba6a2e2aa849ebb4b3cf0d6", "42f90d65a41d6bd04c4ab4f159e35d27877e5e817245522272dbd7589aeaf4ea"},
	}
	for transport, want := range golden {
		for _, options := range []*ProfileOptions{nil, {}, {XHTTP: &XHTTPOptions{}, Client: &ClientOptions{}}} {
			p := testProfile(1)
			p.Transport, p.Options = transport, options
			if transport == "grpc" {
				p.GRPCService = "dark-pro"
			}
			legacy, err := Render(planFor(t, 10443, 11680), []Profile{p})
			if err != nil || legacy.SHA256 != want[0] {
				t.Fatalf("%s legacy candidate bytes changed: %v", transport, err)
			}
			native, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443, SwapPort: 11680}})
			if err != nil || native.SHA256 != want[1] || native.ManifestSHA256 != want[2] {
				t.Fatalf("%s native default candidate/manifest bytes changed: %v", transport, err)
			}
			nodes, swaps := creationFixture()
			topology, err := RenderCreationTopology(p, nodes, swaps)
			if err != nil || topology.SHA256 != want[3] {
				t.Fatalf("%s default topology bytes changed: %v", transport, err)
			}
		}
	}
}

func TestProfileOptionsReachEveryDestinationAndAdmissionPayload(t *testing.T) {
	for _, transport := range []string{"tcp", "grpc"} {
		t.Run(transport, func(t *testing.T) {
			p := testProfile(1)
			p.Transport, p.Options = transport, fullProfileOptions(transport)
			if transport == "grpc" {
				p.GRPCService = "dark-pro"
			}
			assertOptions := func(c Candidate) {
				t.Helper()
				var doc struct {
					Inbounds []inbound `json:"inbounds"`
				}
				if err := json.Unmarshal(c.Config, &doc); err != nil {
					t.Fatal(err)
				}
				for _, in := range doc.Inbounds {
					if in.Protocol == "dokodemo-door" {
						if !reflect.DeepEqual(*optionsFromStream(in.Stream), ProfileOptions{}) || bytes.Contains(c.Config, []byte(p.RealityPrivateKey)) {
							t.Fatal("relay received destination settings or credentials")
						}
						continue
					}
					if !reflect.DeepEqual(optionsFromStream(in.Stream), p.Options) || in.Stream.Network != transport || len(in.Settings.Clients) != len(p.Users) {
						t.Fatal("a customer listener lost its advanced settings or identities")
					}
				}
			}
			legacy, err := Render(planFor(t, 10443, 11680), []Profile{p})
			if err != nil {
				t.Fatal(err)
			}
			assertOptions(legacy)
			native, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443, SwapPort: 11680}})
			if err != nil {
				t.Fatal(err)
			}
			assertOptions(native)
			metered, err := WithUsageAPI(native, "127.0.0.1:15443")
			if err != nil {
				t.Fatal(err)
			}
			_, payload, _, err := UsageBootstrap(metered)
			if err != nil {
				t.Fatal(err)
			}
			var original, delivered map[string]json.RawMessage
			if json.Unmarshal(metered.Config, &original) != nil || json.Unmarshal(payload, &delivered) != nil || !bytes.Equal(original["inbounds"], delivered["inbounds"]) {
				t.Fatal("AddInbound admission payload changed the advanced listener config")
			}
			nodes, swaps := creationFixture()
			topology, err := RenderCreationTopology(p, nodes, swaps)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range topology.Nodes {
				assertOptions(node.Candidate)
			}
		})
	}
}

func TestProfileOptionsValidatedWhenReadingCandidate(t *testing.T) {
	p := testProfile(1)
	p.Options = fullProfileOptions("tcp")
	c, err := RenderAssigned("new-lab", strings.Repeat("1", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil {
		t.Fatal(err)
	}
	c, err = WithUsageAPI(c, "127.0.0.1:15443")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LabSubscriptionRoutes(c, uuidA, map[uint64]string{1: "Advanced TCP"}); err != nil {
		t.Fatal("valid server options blocked an otherwise eligible LAB consumer")
	}
	for _, field := range []string{"xver", "maxTimeDiff", "minClientVer", "maxClientVer", "unknownOption"} {
		t.Run(field, func(t *testing.T) {
			bad := c
			editLabSubscriptionConfig(t, &bad, func(doc map[string]any) {
				r := doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
				switch field {
				case "xver", "maxTimeDiff":
					r[field] = -1
				case "minClientVer", "maxClientVer":
					r[field] = "999.0.0"
				default:
					r[field] = "unexpected"
				}
			})
			if _, err := LabSubscriptionRoutes(bad, uuidA, map[uint64]string{1: "Advanced TCP"}); !errors.Is(err, ErrInvalid) {
				t.Fatal("subscription adapter silently dropped invalid server options")
			}
			if _, _, _, err := UsageBootstrap(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("admission adapter accepted invalid advanced options")
			}
		})
	}
}
