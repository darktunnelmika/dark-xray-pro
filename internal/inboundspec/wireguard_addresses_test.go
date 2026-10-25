package inboundspec

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestAmneziaWGIPv6AllocationPreservesSmallSubnetBits(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		id     uint64
		want   string
	}{
		{"fd42:1234:5678:9abc::/64", 1, "fd42:1234:5678:9abc::2/128"},
		{"fd42:1234:5678:9abc:8000::/65", 1, "fd42:1234:5678:9abc:8000::2/128"},
		{"fd42:1234:5678:9abc:abcd:ef01::/96", 2, "fd42:1234:5678:9abc:abcd:ef01:0:3/128"},
		{"fd42::ab00/120", 253, "fd42::abfe/128"},
		{"fd42::abfc/126", 1, "fd42::abfe/128"},
		{"fd42::abff/126", 2, "fd42::abff/128"},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			s := fixture(t, "amneziawg")
			server := obj(s.Settings, "server")
			server["ipv6Enabled"], server["ipv6Subnet"] = true, tc.prefix
			n, err := Normalize(s)
			if err != nil {
				t.Fatal(err)
			}
			u := testUsers()[0]
			u.ID = tc.id
			addresses, err := WireGuardAddresses(n, u)
			if err != nil || len(addresses) != 2 || addresses[1] != tc.want {
				t.Fatal("peer escaped prefix or lost its subnet bits", addresses, err)
			}
			p := netip.MustParsePrefix(tc.prefix).Masked()
			a := netip.MustParsePrefix(addresses[1]).Addr()
			if !p.Contains(a) || a == p.Addr() || a == p.Addr().Next() {
				t.Fatal("peer reused reserved server/network address", addresses)
			}
		})
	}
}

func TestAmneziaWGSmallIPv6CapacityAppliesBeforeStableHash(t *testing.T) {
	s := fixture(t, "amneziawg")
	server := obj(s.Settings, "server")
	server["ipv6Enabled"], server["ipv6Subnet"] = true, "fd42::1c/126"
	n, err := Normalize(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range testUsers() {
		u.ID = 0
		a, err := WireGuardAddresses(n, u)
		if err != nil || len(a) != 2 || !netip.MustParsePrefix("fd42::1c/126").Contains(netip.MustParsePrefix(a[1]).Addr()) {
			t.Fatal("implicit peer index ignored the smaller IPv6 capacity", a, err)
		}
		b, err := WireGuardAddresses(n, u)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatal("stable identity changed without a profile change", a, b, err)
		}
	}
	u := testUsers()[0]
	u.ID = 3
	_, err = WireGuardAddresses(n, u)
	var field *FieldError
	if !errors.As(err, &field) || field.Code != "subnet_capacity_exceeded" {
		t.Fatal("an explicit peer index overflowed a /126", err)
	}
}

func TestAmneziaWGSmallIPv6ServerAndExportsShareExactPeerAddress(t *testing.T) {
	s := fixture(t, "amneziawg")
	server := obj(s.Settings, "server")
	server["ipv6Enabled"], server["ipv6Subnet"] = true, "fd42::7c/126"
	n, err := Normalize(s)
	if err != nil {
		t.Fatal(err)
	}
	users := testUsers()
	rendered, err := Render(n, "127.0.0.1", 23456, "small-v6", users)
	if err != nil {
		t.Fatal(err)
	}
	peers := rendered.Config["peers"].([]any)
	for i, u := range users {
		want, err := WireGuardAddresses(n, u)
		if err != nil || !reflect.DeepEqual(peers[i].(map[string]any)["allowedIPs"], want) {
			t.Fatal("server/client peer allocation diverged", err)
		}
		client, err := ExportClient(n, "conf", u, Endpoint{Address: "node.example.test", Port: 23456})
		if err != nil || !strings.Contains(string(client), "\nAddress = "+strings.Join(want, ", ")+"\n") {
			t.Fatal("exported client lost the exact server-assigned peer addresses", err)
		}
	}
}
