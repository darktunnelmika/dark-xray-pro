package inboundexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func tunDNSFixture(tag string, settings map[string]any) json.RawMessage {
	if settings == nil {
		settings = map[string]any{"name": tag, "gateway": []string{"10.91.0.1/24"}, "dns": []string{"1.1.1.1", "8.8.8.8"}, "autoSystemDnsToGateway": true}
	}
	raw, _ := json.Marshal(map[string]any{"tag": tag, "protocol": "tun", "settings": settings})
	return raw
}

func TestTUNDNSCompilerBuildsCompleteOrderedPathWithoutChangingDefault(t *testing.T) {
	settings := map[string]any{"name": "tunone", "gateway": []string{"fd42:1::1/64", "10.91.0.1/24", "10.92.0.1/24"}, "dns": []string{"1.1.1.1", "2001:4860:4860::8888"}, "autoSystemDnsToGateway": true}
	first := tunDNSFixture("tun-one", settings)
	settings["name"], settings["gateway"] = "tuntwo", []string{"fd42:2::1/64"}
	second := tunDNSFixture("tun-two", settings)
	direct := json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)
	priorRule := json.RawMessage(`{"type":"field","inboundTag":["ordinary-proxy"],"outboundTag":"direct"}`)
	dns, outbounds, rules, err := compileTUNDNS([]json.RawMessage{first, second}, []json.RawMessage{direct}, []json.RawMessage{priorRule}, nil)
	if err != nil || len(outbounds) != 2 || !bytes.Equal(outbounds[0], direct) || len(rules) != 3 || !bytes.Equal(rules[2], priorRule) {
		t.Fatal("generated DNS changed the default or sibling route order", err)
	}
	var policy struct {
		Servers        []string `json:"servers"`
		Tag            string   `json:"tag"`
		QueryStrategy  string   `json:"queryStrategy"`
		UseSystemHosts bool     `json:"useSystemHosts"`
	}
	if json.Unmarshal(dns, &policy) != nil || !reflect.DeepEqual(policy.Servers, []string{"1.1.1.1", "2001:4860:4860::8888"}) || policy.Tag != tunDNSUpstreamTag || policy.QueryStrategy != "UseIP" || policy.UseSystemHosts {
		t.Fatal("explicit upstream policy was lost", string(dns))
	}
	for i, tc := range []struct{ tag, ip string }{{"tun-one", "10.91.0.2/32"}, {"tun-two", "fd42:2::2/128"}} {
		var rule struct {
			InboundTag  []string `json:"inboundTag"`
			IP          []string `json:"ip"`
			Port        string   `json:"port"`
			Network     string   `json:"network"`
			OutboundTag string   `json:"outboundTag"`
		}
		if json.Unmarshal(rules[i], &rule) != nil || !reflect.DeepEqual(rule.InboundTag, []string{tc.tag}) || !reflect.DeepEqual(rule.IP, []string{tc.ip}) || rule.Port != "53" || rule.Network != "tcp,udp" || rule.OutboundTag != tunDNSOutboundTag {
			t.Fatal("DNS rule was not scoped to the owned advertised address", string(rules[i]))
		}
	}
	var out struct {
		Tag      string         `json:"tag"`
		Protocol string         `json:"protocol"`
		Settings map[string]any `json:"settings"`
	}
	if json.Unmarshal(outbounds[1], &out) != nil || out.Tag != tunDNSOutboundTag || out.Protocol != "dns" || out.Settings["rewriteAddress"] != "1.1.1.1" || out.Settings["rewritePort"] != float64(53) || out.Settings["rewriteNetwork"] != nil {
		t.Fatal("DNS forwarding lost non-address queries or their transport", string(outbounds[1]))
	}
	wantRules := []any{map[string]any{"action": "hijack", "qType": "1,28"}, map[string]any{"action": "direct"}}
	if !reflect.DeepEqual(out.Settings["rules"], wantRules) {
		t.Fatal("TXT/SRV queries would be fabricated as empty success", out.Settings["rules"])
	}
	if _, _, _, err = compileTUNDNS([]json.RawMessage{first, second}, []json.RawMessage{direct}, []json.RawMessage{priorRule}, []json.RawMessage{dns}); err != nil {
		t.Fatal("exact existing sibling resolver policy did not match", err)
	}
}

func TestTUNDNSCompilerRejectsResolverLoopsAndConflictingPolicies(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		edit       func(map[string]any)
	}{
		{"missing", "tun_dns_explicit_upstream_required", func(s map[string]any) { delete(s, "dns") }},
		{"system", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"localhost"} }},
		{"hostname", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"resolver.example.test"} }},
		{"bootstrap", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"https+local://dns.example.test/dns-query"} }},
		{"loopback", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"127.0.0.53"} }},
		{"unspecified", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"::"} }},
		{"multicast", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"ff02::1"} }},
		{"mapped", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"::ffff:127.0.0.1"} }},
		{"duplicate", "tun_dns_explicit_upstream_required", func(s map[string]any) { s["dns"] = []string{"1.1.1.1", "1.1.1.1"} }},
		{"missing_gateway", "tun_dns_explicit_upstream_required", func(s map[string]any) { delete(s, "gateway") }},
		{"network_gateway", "tun_dns_gateway_invalid", func(s map[string]any) { s["gateway"] = []string{"10.91.0.0/24"} }},
		{"broadcast_target", "tun_dns_gateway_invalid", func(s map[string]any) { s["gateway"] = []string{"10.91.0.2/30"} }},
		{"ipv6_target_outside_prefix", "tun_dns_gateway_invalid", func(s map[string]any) { s["gateway"] = []string{"fd42::1/128"} }},
		{"own_gateway", "tun_dns_upstream_route_loop", func(s map[string]any) { s["dns"] = []string{"10.91.0.2"} }},
		{"automatic_route", "tun_dns_upstream_route_loop", func(s map[string]any) { s["autoSystemRoutingTable"] = []string{"0.0.0.0/0"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := map[string]any{"gateway": []string{"10.91.0.1/24"}, "dns": []string{"1.1.1.1"}, "autoSystemDnsToGateway": true}
			tc.edit(s)
			_, _, _, err := compileTUNDNS([]json.RawMessage{tunDNSFixture("tun-one", s)}, []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}, nil, nil)
			var field *inboundspec.FieldError
			if !errors.As(err, &field) || field.Code != tc.code {
				t.Fatal("unsafe host DNS policy was not precisely refused", err)
			}
		})
	}
	base := []json.RawMessage{tunDNSFixture("tun-one", nil)}
	direct := []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}
	for _, tc := range []struct {
		name, code       string
		extra            []json.RawMessage
		outbounds, rules []json.RawMessage
		policies         []json.RawMessage
	}{
		{"sibling_resolver_order", "tun_dns_policy_conflict", []json.RawMessage{tunDNSFixture("tun-two", map[string]any{"gateway": []string{"10.92.0.1/24"}, "dns": []string{"8.8.8.8", "1.1.1.1"}, "autoSystemDnsToGateway": true})}, direct, nil, nil},
		{"sibling_capture", "tun_dns_upstream_route_loop", []json.RawMessage{tunDNSFixture("tun-two", map[string]any{"gateway": []string{"10.92.0.1/24"}, "autoSystemRoutingTable": []string{"1.1.1.0/24"}})}, direct, nil, nil},
		{"reserved_inbound", "tun_dns_reserved_tag_conflict", []json.RawMessage{json.RawMessage(`{"tag":"dark-pro-inbound-dns-upstream","protocol":"http"}`)}, direct, nil, nil},
		{"reserved_outbound", "tun_dns_reserved_tag_conflict", nil, []json.RawMessage{direct[0], json.RawMessage(`{"tag":"dark-pro-inbound-dns","protocol":"dns"}`)}, nil, nil},
		{"reserved_route_reference", "tun_dns_reserved_tag_conflict", nil, direct, []json.RawMessage{json.RawMessage(`{"type":"field","inboundTag":["dark-pro-inbound-dns-upstream"],"outboundTag":"direct"}`)}, nil},
		{"owned_interface_collision", "execution_resource_conflict", []json.RawMessage{tunDNSFixture("tun-two", map[string]any{"name": "tun-one", "gateway": []string{"10.92.0.1/24"}})}, direct, nil, nil},
		{"global_policy", "tun_dns_policy_conflict", nil, direct, nil, []json.RawMessage{json.RawMessage(`{"servers":["8.8.8.8"]}`)}},
		{"proxy_bootstrap", "tun_dns_upstream_route_unsupported", nil, []json.RawMessage{json.RawMessage(`{"tag":"proxy","protocol":"socks"}`)}, nil, nil},
		{"conflicting_dns_route", "tun_dns_route_conflict", nil, direct, []json.RawMessage{json.RawMessage(`{"type":"field","inboundTag":["tun-one"],"port":"53","outboundTag":"direct"}`)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := compileTUNDNS(append(append([]json.RawMessage{}, base...), tc.extra...), tc.outbounds, tc.rules, tc.policies)
			var field *inboundspec.FieldError
			if !errors.As(err, &field) || field.Code != tc.code {
				t.Fatal("sibling policy was silently changed", err)
			}
		})
	}
}

func TestTUNDNSCompilerKeepsDisjointRules(t *testing.T) {
	prior := []json.RawMessage{
		json.RawMessage(`{"type":"field","inboundTag":["tun-one"],"port":"80,443","outboundTag":"direct"}`),
		json.RawMessage(`{"type":"field","inboundTag":["tun-one"],"ip":["192.0.2.0/24"],"port":53,"outboundTag":"direct"}`),
	}
	_, _, rules, err := compileTUNDNS([]json.RawMessage{tunDNSFixture("tun-one", nil)}, []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}, prior, nil)
	if err != nil || len(rules) != 3 || !reflect.DeepEqual(rules[1:], prior) {
		t.Fatal("disjoint existing routes were rejected or changed", err)
	}
}

func TestTUNDNSCompilerPreservesUnrelatedDNSWhenTakeoverIsDisabled(t *testing.T) {
	policy := json.RawMessage(`{"servers":["8.8.8.8"]}`)
	direct := []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}
	rules := []json.RawMessage{json.RawMessage(`{"type":"field","inboundTag":["tun-one"],"outboundTag":"direct"}`)}
	dns, outs, gotRules, err := compileTUNDNS([]json.RawMessage{tunDNSFixture("tun-one", map[string]any{"gateway": []string{"10.91.0.1/24"}})}, direct, rules, []json.RawMessage{policy})
	if err != nil || !bytes.Equal(dns, policy) || !reflect.DeepEqual(outs, direct) || !reflect.DeepEqual(gotRules, rules) {
		t.Fatal("ordinary TUN or an explicit resolver policy was rewritten", err)
	}
}

func TestTUNDNSCompiledBundlePassesPinnedCoreOfflineValidation(t *testing.T) {
	path := os.Getenv("DARK_PRO_TEST_INBOUND_CORE")
	if path == "" {
		t.Skip("set DARK_PRO_TEST_INBOUND_CORE to the separately pinned v2 test engine")
	}
	config, _ := json.Marshal(map[string]any{"inbounds": []json.RawMessage{tunDNSFixture("dns-check", nil)}, "outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}}})
	item := CompileItem{Ref: RevisionRef{InboundID: 5, Revision: 1}, Profile: xrayconfig.Profile{InboundID: 5, Spec: &inboundspec.Spec{Version: 1, Protocol: "tun", Network: "tun", Security: "none"}}, AccountingMode: AccountingOwnerAggregate, Topology: xrayconfig.CreationTopology{Nodes: []xrayconfig.CreationNodeCandidate{{NodeID: "node", InstallationID: strings.Repeat("a", 32), Candidate: xrayconfig.Candidate{Config: config, Listeners: []xrayconfig.Listener{{InboundID: 5, Tag: "dns-check", Kind: "direct"}}}}}}}
	b, err := Compile(context.Background(), CompileRequest{NodeID: "node", InstallationID: strings.Repeat("a", 32), Generation: 1, OperationID: "dns-offline-check", Items: []CompileItem{item}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "candidate.json")
	if err = os.WriteFile(file, b.NativeConfig, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path, "run", "-test", "-config", file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal("bounded DNS configuration failed pinned core parsing", err, string(output))
	}
}
