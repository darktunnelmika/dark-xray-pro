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

func TestRemarkTemplate(t *testing.T) {
	in := Inbound{Tag: "uk", Template: []byte(`{"protocol":"vless","settings":{},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverNames":["a.com"],"shortIds":["ab"]}}}`),
		Meta: Meta{PublicKey: "PUB", Remark: "Dark", Endpoints: []Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 443}, {Address: "5.6.7.8", Port: 443}}}}
	a := Account{UUID: "u", RemarkTemplate: "{remark} · {path} · {left}", Vars: map[string]string{"left": "3 GB"}}
	links, err := Links(a, []Inbound{in})
	if err != nil || len(links) != 2 {
		t.Fatal(err, links)
	}
	if !strings.HasSuffix(links[0], "#Dark%20%C2%B7%20Direct%20%C2%B7%203%20GB") {
		t.Fatalf("templated name: %s", links[0])
	}
	// An empty {path} leaves no doubled separator.
	if !strings.HasSuffix(links[1], "#Dark%20%C2%B7%203%20GB") {
		t.Fatalf("empty variable: %s", links[1])
	}
	a.InfoLines = []string{"📊 3 GB"}
	links, _ = Links(a, []Inbound{in})
	if len(links) != 3 || !strings.Contains(links[0], "127.0.0.1:1") {
		t.Fatalf("info line: %v", links)
	}
}
