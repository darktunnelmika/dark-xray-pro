package sub

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestXrayJSON(t *testing.T) {
	reality := Inbound{Tag: "uk", Template: []byte(`{"protocol":"vless","settings":{"flow":"xtls-rprx-vision"},
"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverNames":["www.speedtest.net"],"shortIds":["ab"]}}}`),
		Meta: Meta{PublicKey: "PUB", Remark: "UK", Endpoints: []Endpoint{{Name: "Direct", Address: "5.101.216.178", Port: 28443}}}}
	ws := Inbound{Tag: "ws", Template: []byte(`{"protocol":"trojan","settings":{},"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"x.com"},"wsSettings":{"path":"/t"}}}`),
		Meta: Meta{Remark: "WS", Endpoints: []Endpoint{{Address: "x.com", Port: 443}}}}
	hy := Inbound{Tag: "hy", Template: []byte(`{"protocol":"hysteria","settings":{},"streamSettings":{"network":"hysteria","security":"tls"}}`), Meta: Meta{Endpoints: []Endpoint{{Address: "h", Port: 1}}}}
	out, err := XrayJSON(Account{UUID: "u-1", Secret: "s", IranDirect: true, Fragment: &Fragment{Length: "50-100", Noise: true}}, []Inbound{reality, ws, hy})
	if err != nil {
		t.Fatal(err)
	}
	var cfgs []struct {
		Remarks   string           `json:"remarks"`
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(out, &cfgs); err != nil {
		t.Fatal(err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("hysteria must be skipped, got %d configs", len(cfgs))
	}
	c := cfgs[0]
	s := string(out)
	for _, want := range []string{`"dialerProxy": "fragment"`, `"length": "50-100"`, `"noises"`, `"publicKey": "PUB"`, `"flow": "xtls-rprx-vision"`, `geoip:ir`, `"path": "/t"`} {
		if !strings.Contains(s, want) {
			t.Errorf("%s missing", want)
		}
	}
	if c.Outbounds[0]["tag"] != "proxy" || len(c.Outbounds) != 4 {
		t.Fatalf("outbounds %+v", c.Outbounds)
	}
	// Without fragment: no dialer proxy, no fragment outbound.
	plain, _ := XrayJSON(Account{UUID: "u"}, []Inbound{reality})
	if strings.Contains(string(plain), "fragment") || strings.Contains(string(plain), "geoip:ir") {
		t.Fatalf("plain config has fragment or iran rules: %s", plain)
	}
	if Format("xray", "") != "xray" {
		t.Fatal("format")
	}
}
