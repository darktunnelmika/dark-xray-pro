package business

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const MaxNativeInboundEndpointsPerPath = 64

// An endpoint describes the address advertised to a client. Port zero inherits
// the Node listener port; changing it does not provision a NAT or tunnel rule.
// Credentials remain in the client/profile models, never in these records.
type NativeInboundEndpoint struct {
	Address     string `json:"address"`
	Port        uint16 `json:"port"`
	Label       string `json:"label,omitempty"`
	Enabled     bool   `json:"enabled"`
	SNI         string `json:"sni,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	SpiderX     string `json:"spider_x,omitempty"`
}

type nativeEndpointPresence struct{ Direct, Tunnel bool }

// Record section presence before normalization so an older editor cannot
// silently erase richer endpoint settings by submitting only the Host mirror.
func captureEndpointPresence(in *NativeInboundPlanInput) {
	if in.endpointOptionsPresence == nil {
		in.endpointOptionsPresence = map[string]nativeEndpointPresence{}
	}
	for _, n := range in.Nodes {
		p := in.endpointOptionsPresence[n.NodeID]
		p.Direct = p.Direct || n.DirectEndpoints != nil
		p.Tunnel = p.Tunnel || n.TunnelEndpoints != nil
		in.endpointOptionsPresence[n.NodeID] = p
	}
}

func checkEndpointPresence(in NativeInboundPlanInput, old NativeInboundPlan) error {
	selected := map[string]bool{}
	for _, n := range in.Nodes {
		selected[n.NodeID] = true
	}
	for _, n := range old.Nodes {
		if !selected[n.NodeID] {
			continue
		}
		p := in.endpointOptionsPresence[n.NodeID]
		if len(n.DirectEndpoints) != 0 && !p.Direct {
			return planError(n.NodeID, "direct_endpoints", "endpoint_options_required", ErrConflict)
		}
		if len(n.TunnelEndpoints) != 0 && !p.Tunnel {
			return planError(n.NodeID, "tunnel_endpoints", "endpoint_options_required", ErrConflict)
		}
	}
	return nil
}

func effectiveEndpointClient(profile InboundInput, endpoint NativeInboundEndpoint) (sni, fingerprint, spider string) {
	if len(profile.ServerNames) != 0 {
		sni = profile.ServerNames[0]
	}
	fingerprint, spider = "chrome", "/"
	if profile.Options != nil && profile.Options.Client != nil {
		if profile.Options.Client.Fingerprint != "" {
			fingerprint = profile.Options.Client.Fingerprint
		}
		if profile.Options.Client.SpiderX != "" {
			spider = profile.Options.Client.SpiderX
		}
	}
	if endpoint.SNI != "" {
		sni = endpoint.SNI
	}
	if endpoint.Fingerprint != "" {
		fingerprint = endpoint.Fingerprint
	}
	if endpoint.SpiderX != "" {
		spider = endpoint.SpiderX
	}
	return
}

func normalizePlanEndpoints(profile InboundInput, node, path string, port uint16, hosts []string, input []NativeInboundEndpoint) ([]NativeInboundEndpoint, error) {
	if len(input) == 0 {
		return nil, nil
	}
	field := path + "_endpoints"
	if len(input) > MaxNativeInboundEndpointsPerPath {
		return nil, planError(node, field, "endpoint_limit", ErrInvalid)
	}
	if port == 0 {
		return nil, planError(node, field, "disabled_path_has_endpoints", ErrInvalid)
	}
	out := append([]NativeInboundEndpoint(nil), input...)
	mirror := []string{}
	addresses, endpoints := map[string]bool{}, map[string]bool{}
	for i := range out {
		e := &out[i]
		fail := func(part, code string) ([]NativeInboundEndpoint, error) {
			return nil, planError(node, fmt.Sprintf("%s.%d.%s", field, i, part), code, ErrInvalid)
		}
		e.Address = canonicalRouteHost(e.Address)
		if e.Address == "" {
			return fail("address", "host_invalid")
		}
		e.Label = strings.TrimSpace(e.Label)
		if utf8.RuneCountInString(e.Label) > 128 || !utf8.ValidString(e.Label) || strings.IndexFunc(e.Label, unicode.IsControl) >= 0 {
			return fail("label", "endpoint_label_invalid")
		}
		if e.SNI != "" {
			allowed := false
			for _, name := range profile.ServerNames {
				if e.SNI == name {
					allowed = true
					break
				}
			}
			if !allowed {
				return fail("sni", "endpoint_sni_invalid")
			}
		}
		if _, err := xrayconfig.NormalizeClientOptions(&xrayconfig.ClientOptions{Fingerprint: e.Fingerprint, SpiderX: e.SpiderX}); err != nil {
			if _, fpErr := xrayconfig.NormalizeClientOptions(&xrayconfig.ClientOptions{Fingerprint: e.Fingerprint}); fpErr != nil {
				return fail("fingerprint", "client_fingerprint_invalid")
			}
			return fail("spider_x", "client_spider_x_invalid")
		}
		publicPort := e.Port
		if publicPort == 0 {
			publicPort = port
		}
		sni, fp, spider := effectiveEndpointClient(profile, *e)
		identity := fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s", e.Address, publicPort, sni, fp, spider)
		if endpoints[identity] {
			return fail("address", "endpoint_duplicate")
		}
		endpoints[identity] = true
		if e.Enabled && !addresses[e.Address] {
			mirror = append(mirror, e.Address)
			addresses[e.Address] = true
		}
	}
	// The old bare Host list is an exact compatibility mirror, not another
	// independently editable source of endpoint truth.
	if len(mirror) != len(hosts) || len(mirror) > 0 && !reflect.DeepEqual(mirror, hosts) {
		return nil, planError(node, field, "endpoint_hosts_mismatch", ErrInvalid)
	}
	return out, nil
}

// Safe, credential-free server-derived output for Review. It is connection
// metadata only: no claim of reachability, installed forwarding or publication.
type NativeInboundEndpointPreview struct {
	NodeID        string                   `json:"node_id"`
	Path          string                   `json:"path"`
	Address       string                   `json:"address"`
	Port          uint16                   `json:"port"`
	ListenPort    uint16                   `json:"listen_port"`
	Label         string                   `json:"label"`
	Enabled       bool                     `json:"enabled"`
	ServerName    string                   `json:"server_name"`
	Transport     string                   `json:"transport"`
	Fingerprint   string                   `json:"fingerprint"`
	SpiderX       string                   `json:"spider_x"`
	GRPCService   string                   `json:"grpc_service,omitempty"`
	GRPCAuthority string                   `json:"grpc_authority,omitempty"`
	GRPCMultiMode bool                     `json:"grpc_multi_mode,omitempty"`
	XHTTP         *xrayconfig.XHTTPOptions `json:"xhttp,omitempty"`
}

func previewPlanEndpoints(plan NativeInboundPlan) []NativeInboundEndpointPreview {
	out := []NativeInboundEndpointPreview{}
	add := func(node, path string, listener uint16, e NativeInboundEndpoint) {
		port := e.Port
		if port == 0 {
			port = listener
		}
		label := e.Label
		if label == "" {
			label = plan.Profile.Name + " · " + map[string]string{"direct": "Direct", "tunnel": "Tunnel", "swap": "Swap"}[path]
		}
		sni, fp, spider := effectiveEndpointClient(plan.Profile.InboundInput, e)
		v := NativeInboundEndpointPreview{NodeID: node, Path: path, Address: e.Address, Port: port, ListenPort: listener,
			Label: label, Enabled: e.Enabled, ServerName: sni, Transport: plan.Profile.Transport, Fingerprint: fp, SpiderX: spider}
		if plan.Profile.Transport == "grpc" {
			v.GRPCService = plan.Profile.GRPCService
		}
		if o := plan.Profile.Options; o != nil {
			if plan.Profile.Transport == "grpc" {
				v.GRPCAuthority, v.GRPCMultiMode = o.GRPCAuthority, o.GRPCMultiMode
			}
			if o.XHTTP != nil {
				copy := *o.XHTTP
				v.XHTTP = &copy
			}
		}
		out = append(out, v)
	}
	for _, n := range plan.Nodes {
		for _, p := range []struct {
			kind      string
			port      uint16
			hosts     []string
			endpoints []NativeInboundEndpoint
		}{
			{"direct", n.DirectPort, n.DirectHosts, n.DirectEndpoints}, {"tunnel", n.TunnelPort, n.TunnelHosts, n.TunnelEndpoints},
		} {
			if p.port == 0 {
				continue
			}
			if len(p.endpoints) != 0 {
				for _, e := range p.endpoints {
					add(n.NodeID, p.kind, p.port, e)
				}
			} else {
				for _, host := range p.hosts {
					add(n.NodeID, p.kind, p.port, NativeInboundEndpoint{Address: host, Enabled: true})
				}
			}
		}
		if n.SwapEnabled && n.SwapEntryAddress != "" {
			add(n.NodeID, "swap", n.SwapDestinationPort, NativeInboundEndpoint{Address: n.SwapEntryAddress, Port: n.SwapEntryPort, Enabled: true})
		}
	}
	return out
}

func planNeedsExtendedEnvelope(plan NativeInboundPlan) bool {
	if hasInboundNetworkOptions(plan.Profile.Transport, plan.Profile.Options) {
		return true
	}
	for _, n := range plan.Nodes {
		if len(n.DirectEndpoints) != 0 || len(n.TunnelEndpoints) != 0 {
			return true
		}
	}
	return false
}
