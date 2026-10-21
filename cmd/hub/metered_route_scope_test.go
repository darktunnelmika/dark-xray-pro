package main

import (
	"strings"
	"testing"
)

func TestMeteredRouteStartupAllowlist(t *testing.T) {
	list := `[{"node_id":"node-a","installation_id":"` + strings.Repeat("a", 32) + `","binary_sha":"` + strings.Repeat("b", 64) + `"}]`
	if got, err := meteredRouteNodes(list, "", "", ""); err != nil || len(got) != 1 || got[0].NodeID != "node-a" {
		t.Fatal("list", err)
	}
	if got, err := meteredRouteNodes("", "legacy", strings.Repeat("a", 32), strings.Repeat("b", 64)); err != nil || len(got) != 1 || got[0].NodeID != "legacy" {
		t.Fatal("legacy", err)
	}
	for _, bad := range []string{"[]", "null", list + "{}", `[{"unknown":true}]`, strings.Repeat(" ", 16385)} {
		if _, err := meteredRouteNodes(bad, "", "", ""); err == nil {
			t.Fatal("invalid list accepted")
		}
	}
	if _, err := meteredRouteNodes(list, "legacy", "", ""); err == nil {
		t.Fatal("mixed scopes accepted")
	}
}
