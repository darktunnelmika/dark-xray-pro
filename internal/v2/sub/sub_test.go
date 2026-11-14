package sub

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestLinksRealityDirectAndTunnel(t *testing.T) {
	in := Inbound{Tag: "de-vless", Template: []byte(`{"protocol":"vless","settings":{"flow":"xtls-rprx-vision"},
"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverNames":["akam.bing.com"],"shortIds":["ab12"]}}}`),
		Meta: Meta{PublicKey: "PUB", Remark: "Dark VPN", Endpoints: []Endpoint{{Name: "Direct", Address: "167.233.192.101", Port: 18580}, {Name: "Tunnel", Address: "5.202.75.85", Port: 2400}}}}
	links, err := Links(Account{Email: "a", UUID: "u-1"}, []Inbound{in})
	if err != nil || len(links) != 2 {
		t.Fatalf("links %v %v", links, err)
	}
	for _, want := range []string{"vless://u-1@167.233.192.101:18580?", "pbk=PUB", "sid=ab12", "sni=akam.bing.com", "flow=xtls-rprx-vision", "security=reality", "#Dark%20VPN%20%7C%20Direct"} {
		if !strings.Contains(links[0], want) {
			t.Fatalf("%s missing %q", links[0], want)
		}
	}
	if !strings.Contains(links[1], "@5.202.75.85:2400?") {
		t.Fatalf("tunnel endpoint missing: %s", links[1])
	}
	body, _ := base64.StdEncoding.DecodeString(Base64(links))
	if strings.Count(string(body), "\n") != 1 {
		t.Fatal("base64 body must hold both links")
	}
}

func TestGRPCLinkHasNoFlow(t *testing.T) {
	in := Inbound{Tag: "g", Template: []byte(`{"protocol":"vless","settings":{"flow":"xtls-rprx-vision"},
"streamSettings":{"network":"grpc","security":"reality","grpcSettings":{"serviceName":"aparat-stream"},"realitySettings":{"serverNames":["akam.bing.com"]}}}`),
		Meta: Meta{PublicKey: "P", Endpoints: []Endpoint{{Address: "1.2.3.4", Port: 443}}}}
	links, _ := Links(Account{UUID: "u"}, []Inbound{in})
	if strings.Contains(links[0], "flow=") || !strings.Contains(links[0], "serviceName=aparat-stream") {
		t.Fatalf("bad grpc link %s", links[0])
	}
}

func TestUserinfo(t *testing.T) {
	if got := Userinfo(1, 2, -1, 0); got != "upload=1; download=2; total=0; expire=0" {
		t.Fatal(got)
	}
}

func TestSingBoxAndClash(t *testing.T) {
	in := Inbound{Tag: "uk", Template: []byte(`{"protocol":"vless","settings":{"flow":"xtls-rprx-vision"},
"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverNames":["www.speedtest.net"],"shortIds":["ab"]}}}`),
		Meta: Meta{PublicKey: "PUB", Remark: "UK", Endpoints: []Endpoint{{Name: "Direct", Address: "5.101.216.178", Port: 28443}}}}
	sb, err := SingBox(Account{UUID: "u-1"}, []Inbound{in})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(sb, &cfg); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range cfg.Outbounds {
		if o["type"] == "vless" && o["server"] == "5.101.216.178" && o["flow"] == "xtls-rprx-vision" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sing-box outbound missing: %s", sb)
	}
	cl, err := Clash(Account{UUID: "u-1"}, []Inbound{in})
	if err != nil || !strings.Contains(string(cl), "public-key: \"PUB\"") || !strings.Contains(string(cl), "MATCH,Dark VPN") {
		t.Fatalf("clash %s %v", cl, err)
	}
	if Format("", "sing-box 1.12") != "singbox" || Format("", "clash.meta") != "clash" || Format("", "v2rayNG/1.9") != "base64" || Format("clash", "") != "clash" {
		t.Fatal("format detection")
	}
}
