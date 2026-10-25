package inboundexec

import (
	"reflect"
	"testing"
)

func TestHostRequirementsComeFromActualConfiguration(t *testing.T) {
	bundle := Bundle{NativeConfig: []byte(`{"inbounds":[{"protocol":"tun","settings":{"autoSystemDnsToGateway":true}}]}`), Components: []Component{{Engine: "amneziawg", Config: []byte(`{"server":{"ipv6ExternalInterface":"eth0"}}`)}}}
	got, err := BundleHostRequirements(bundle)
	if err != nil || !reflect.DeepEqual(got, []string{HostIPv6Alias, HostTUN, HostTUNSystemDNS}) {
		t.Fatalf("private configuration lost host requirements: %v, %v", got, err)
	}
	bundle.NativeConfig = []byte(`{"inbounds":[{"protocol":"http","settings":{}}]}`)
	bundle.Components[0].Config = []byte(`{"server":{"ipv6Enabled":true}}`)
	got, err = BundleHostRequirements(bundle)
	if err != nil || len(got) != 0 {
		t.Fatal("ordinary userspace IPv6 was made privileged", got, err)
	}
	bundle.Listeners = []Listener{{HostRequirements: []string{"unknown"}}}
	if _, err := BundleHostRequirements(bundle); err == nil {
		t.Fatal("unknown host boundary was ignored")
	}
}

func TestHostPrerequisitesCannotBeInferredFromBinariesOrStaleReports(t *testing.T) {
	ready := &HostCapabilities{Version: 1, NetworkAdmin: true, ChildNetworkAdmin: true, TUNDevice: true, IPv6: true, SystemDNSControl: true, TUNDNSActivation: true}
	required := []string{HostTUN, HostTUNSystemDNS, HostIPv6Alias}
	if got := HostRequirementBlockers(nil, nil, false); len(got) != 0 {
		t.Fatal("ordinary protocols gained unrelated host prerequisites")
	}
	for _, tc := range []struct {
		name  string
		host  *HostCapabilities
		fresh bool
	}{{"no report", nil, true}, {"stale report", ready, false}, {"unknown version", &HostCapabilities{Version: 2}, true}} {
		t.Run(tc.name, func(t *testing.T) {
			got := HostRequirementBlockers(required, tc.host, tc.fresh)
			if len(got) != 1 || got[0].Code != "host_capabilities_unavailable" {
				t.Fatal("missing authenticated prerequisite evidence was accepted", got)
			}
		})
	}
	if got := HostRequirementBlockers(required, ready, true); len(got) != 0 {
		t.Fatal("measured prerequisites rejected", got)
	}
	unprivileged := &HostCapabilities{Version: 1, TUNDevice: true, IPv6: true, SystemDNSControl: true, TUNDNSActivation: true}
	if got := HostRequirementBlockers(required, unprivileged, true); len(got) != 1 || got[0].Code != "host_network_admin_missing" {
		t.Fatal("an installed TUN device was mistaken for execution privilege", got)
	}
}
