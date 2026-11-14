package sub

import (
	"strings"
	"testing"
)

func TestIranDirectInAppProfiles(t *testing.T) {
	in := Inbound{Tag: "uk", Template: []byte(`{"protocol":"vless","settings":{"flow":"xtls-rprx-vision"},
"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverNames":["www.speedtest.net"],"shortIds":["ab"]}}}`),
		Meta: Meta{PublicKey: "PUB", Remark: "UK", Endpoints: []Endpoint{{Name: "Direct", Address: "5.101.216.178", Port: 28443}}}}
	for _, on := range []bool{true, false} {
		sb, _ := SingBox(Account{UUID: "u-1", IranDirect: on}, []Inbound{in})
		cl, _ := Clash(Account{UUID: "u-1", IranDirect: on}, []Inbound{in})
		hasSB := strings.Contains(string(sb), "geoip-ir.srs") && strings.Contains(string(sb), `"outbound": "direct"`)
		hasCL := strings.Contains(string(cl), "GEOIP,IR,DIRECT") && strings.Index(string(cl), "GEOIP,IR") < strings.Index(string(cl), "MATCH")
		if hasSB != on || hasCL != on {
			t.Fatalf("iran direct=%v: sing-box %v clash %v\n%s\n%s", on, hasSB, hasCL, sb, cl)
		}
	}
	// No proxies: nothing to bypass, no rules.
	if out, _ := Clash(Account{IranDirect: true}, nil); strings.Contains(string(out), "GEOIP,IR") {
		t.Fatal("Iran rule without proxies")
	}
}
