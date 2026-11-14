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

func TestShadowsocks2022Links(t *testing.T) {
	in := Inbound{Tag: "ss", Template: []byte(`{"protocol":"shadowsocks","port":8388,"settings":{"method":"2022-blake3-aes-128-gcm","password":"AAAAAAAAAAAAAAAAAAAAAA=="}}`),
		Meta: Meta{Remark: "SS", Endpoints: []Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 8388}}}}
	a := Account{Email: "x", UUID: "u", Secret: "s3cret"}
	links, err := Links(a, []Inbound{in})
	if err != nil || len(links) != 1 {
		t.Fatal(links, err)
	}
	key := SS2022UserKey("s3cret", "2022-blake3-aes-128-gcm")
	if raw, _ := base64.StdEncoding.DecodeString(key); len(raw) != 16 {
		t.Fatalf("user key %q", key)
	}
	if !strings.HasPrefix(links[0], "ss://2022-blake3-aes-128-gcm:") || !strings.Contains(links[0], "@1.2.3.4:8388#") {
		t.Fatal(links[0])
	}
	if raw, _ := base64.StdEncoding.DecodeString(SS2022UserKey("s3cret", "2022-blake3-aes-256-gcm")); len(raw) != 32 {
		t.Fatal("256-bit key size")
	}
	sb, _ := SingBox(a, []Inbound{in})
	cl, _ := Clash(a, []Inbound{in})
	if !strings.Contains(string(sb), `"type": "shadowsocks"`) || !strings.Contains(string(cl), "type: ss") || !strings.Contains(string(cl), "AAAAAAAAAAAAAAAAAAAAAA==:"+key) {
		t.Fatalf("formats:\n%s\n%s", sb, cl)
	}
	// Legacy (non-2022) shadowsocks is not offered.
	in.Template = []byte(`{"protocol":"shadowsocks","port":1,"settings":{"method":"aes-256-gcm","password":"x"}}`)
	if links, _ := Links(a, []Inbound{in}); len(links) != 0 {
		t.Fatal(links)
	}
}

func TestHysteria2Links(t *testing.T) {
	in := Inbound{Tag: "hy", Template: []byte(`{"protocol":"hysteria","port":443,"settings":{"version":2},"streamSettings":{"network":"hysteria","security":"tls","tlsSettings":{"serverName":"www.bing.com","alpn":["h3"]}}}`),
		Meta: Meta{Remark: "HY", Insecure: true, Endpoints: []Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 443}}}}
	a := Account{Email: "x", UUID: "u", Secret: "abc123"}
	links, _ := Links(a, []Inbound{in})
	if len(links) != 1 || !strings.HasPrefix(links[0], "hysteria2://abc123@1.2.3.4:443/?") || !strings.Contains(links[0], "insecure=1") || !strings.Contains(links[0], "sni=www.bing.com") {
		t.Fatal(links)
	}
	sb, _ := SingBox(a, []Inbound{in})
	cl, _ := Clash(a, []Inbound{in})
	if !strings.Contains(string(sb), `"type": "hysteria2"`) || !strings.Contains(string(cl), "type: hysteria2") || !strings.Contains(string(cl), "skip-cert-verify: true") {
		t.Fatalf("%s\n%s", sb, cl)
	}
}
