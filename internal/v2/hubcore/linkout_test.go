package hubcore

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestOutboundFromLink(t *testing.T) {
	vm := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"DE vm","add":"de.example.com","port":"443","id":"11111111-2222-3333-4444-555555555555","net":"ws","path":"/ws","host":"cdn.example.com","tls":"tls"}`))
	cases := []struct{ link, proto, want string }{
		{"vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?type=tcp&security=reality&pbk=PK&sid=ab&sni=www.google.com&fp=chrome&flow=xtls-rprx-vision#Up%20DE", "vless", `"publicKey":"PK"`},
		{"vless://11111111-2222-3333-4444-555555555555@up.example.com:443?type=xhttp&security=tls&path=%2Fx&mode=packet-up#x", "vless", `"xhttpSettings"`},
		{"trojan://secret@tr.example.com:443?security=tls&type=grpc&serviceName=gs#tr", "trojan", `"serviceName":"gs"`},
		{"vmess://" + vm, "vmess", `"Host":"cdn.example.com"`},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw")) + "@5.6.7.8:8388#ss1", "shadowsocks", `"method":"chacha20-ietf-poly1305"`},
		{"ss://2022-blake3-aes-128-gcm:a2V5@5.6.7.8:8388#ss2", "shadowsocks", `"password":"a2V5"`},
		{"socks://u:p@9.9.9.9:1080#s", "socks", `"user":"u"`},
	}
	for _, c := range cases {
		o, err := OutboundFromLink(c.link, "")
		if err != nil {
			t.Fatalf("%s: %v", c.link, err)
		}
		var cfg map[string]any
		_ = json.Unmarshal(o.Config, &cfg)
		if cfg["protocol"] != c.proto || cfg["tag"] != o.Tag || o.Tag == "" || !strings.Contains(string(o.Config), c.want) {
			t.Fatalf("%s → %s %s", c.link, o.Tag, o.Config)
		}
		if err := validateRouting(Routing{Outbounds: []RouteOutbound{o}}); err != nil {
			t.Fatalf("%s: routing refuses it: %v", c.link, err)
		}
	}
	if o, _ := OutboundFromLink("vless://u@1.2.3.4:443#Up DE", "mine"); o.Tag != "mine" {
		t.Fatal("explicit tag")
	}
	for _, bad := range []string{"hello", "vless://@1.2.3.4:443", "vless://u@host", "ss://zz"} {
		if _, err := OutboundFromLink(bad, ""); err == nil {
			t.Fatal("must refuse", bad)
		}
	}
}
