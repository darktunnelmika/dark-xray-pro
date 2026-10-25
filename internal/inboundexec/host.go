package inboundexec

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

const (
	HostTUN          = "tun"
	HostTUNSystemDNS = "tun_system_dns"
	HostIPv6Alias    = "ipv6_alias"
)

// HostCapabilities reports measured prerequisites, not a successful network
// mutation or an end-to-end traffic test. Freshness is assigned by the HUB on
// the authenticated poll; the Agent cannot choose its own receipt timestamp.
type HostCapabilities struct {
	Version           int  `json:"version"`
	NetworkAdmin      bool `json:"network_admin"`
	ChildNetworkAdmin bool `json:"child_network_admin"`
	TUNDevice         bool `json:"tun_device"`
	IPv6              bool `json:"ipv6"`
	SystemDNSControl  bool `json:"system_dns_control"`
	TUNDNSActivation  bool `json:"tun_dns_activation"`
}

func HostRequirementsForSpec(s *inboundspec.Spec) []string {
	if s == nil {
		return nil
	}
	switch s.Protocol {
	case "tun":
		out := []string{HostTUN}
		if enabled, _ := s.Settings["autoSystemDnsToGateway"].(bool); enabled {
			out = append(out, HostTUNSystemDNS)
		}
		return out
	case "amneziawg":
		server, _ := s.Settings["server"].(map[string]any)
		for _, name := range []string{"ipv6ExternalInterface", "externalInterface"} {
			if value, _ := server[name].(string); strings.TrimSpace(value) != "" {
				return []string{HostIPv6Alias}
			}
		}
	}
	return nil
}

// BundleHostRequirements inspects the actual private configurations as well as
// their public listener metadata. A missing metadata field cannot bypass the
// runtime's host boundary, including bundles created before this field existed.
func BundleHostRequirements(b Bundle) ([]string, error) {
	set := map[string]bool{}
	for _, listener := range b.Listeners {
		for _, feature := range listener.HostRequirements {
			switch feature {
			case HostTUN, HostTUNSystemDNS, HostIPv6Alias:
				set[feature] = true
			default:
				return nil, ErrInvalid
			}
		}
	}
	if len(b.NativeConfig) != 0 {
		var cfg struct {
			Inbounds []struct {
				Protocol string `json:"protocol"`
				Settings struct {
					AutoSystemDNS bool `json:"autoSystemDnsToGateway"`
				} `json:"settings"`
			} `json:"inbounds"`
		}
		if json.Unmarshal(b.NativeConfig, &cfg) != nil {
			return nil, ErrInvalid
		}
		for _, in := range cfg.Inbounds {
			if in.Protocol == "tun" {
				set[HostTUN] = true
				if in.Settings.AutoSystemDNS {
					set[HostTUNSystemDNS] = true
				}
			}
		}
	}
	for _, component := range b.Components {
		if component.Engine != "amneziawg" {
			continue
		}
		var cfg struct {
			Server struct {
				ExternalInterface     string `json:"externalInterface"`
				IPv6ExternalInterface string `json:"ipv6ExternalInterface"`
			} `json:"server"`
		}
		if json.Unmarshal(component.Config, &cfg) != nil {
			return nil, ErrInvalid
		}
		if cfg.Server.ExternalInterface != "" || cfg.Server.IPv6ExternalInterface != "" {
			set[HostIPv6Alias] = true
		}
	}
	out := make([]string, 0, len(set))
	for feature := range set {
		out = append(out, feature)
	}
	sort.Strings(out)
	return out, nil
}

func HostRequirementBlockers(requirements []string, host *HostCapabilities, fresh bool) []Blocker {
	if len(requirements) == 0 {
		return nil
	}
	if host == nil || host.Version != 1 || !fresh {
		return []Blocker{{Code: "host_capabilities_unavailable"}}
	}
	codes := map[string]bool{}
	for _, feature := range requirements {
		switch feature {
		case HostTUN:
			if !host.ChildNetworkAdmin {
				codes["host_network_admin_missing"] = true
			}
			if !host.TUNDevice {
				codes["host_tun_device_missing"] = true
			}
		case HostTUNSystemDNS:
			if !host.SystemDNSControl {
				codes["host_dns_control_unavailable"] = true
			}
			if !host.TUNDNSActivation {
				codes["host_tun_dns_engine_unavailable"] = true
			}
		case HostIPv6Alias:
			if !host.NetworkAdmin {
				codes["host_network_admin_missing"] = true
			}
			if !host.IPv6 {
				codes["host_ipv6_unavailable"] = true
			}
		default:
			codes["host_feature_unsupported"] = true
		}
	}
	keys := make([]string, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	out := make([]Blocker, 0, len(keys))
	for _, code := range keys {
		out = append(out, Blocker{Code: code})
	}
	return out
}
