package hubcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

type renderedCfg struct {
	Inbounds  []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
	Routing   struct {
		DomainStrategy string           `json:"domainStrategy"`
		Rules          []map[string]any `json:"rules"`
		Balancers      []map[string]any `json:"balancers"`
	} `json:"routing"`
	DNS map[string]any `json:"dns"`
}

func renderFor(t *testing.T, s *Store, node string) renderedCfg {
	t.Helper()
	raw, err := s.RenderNode(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	var c renderedCfg
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func ruleText(c renderedCfg) string {
	b, _ := json.Marshal(c.Routing.Rules)
	return string(b)
}

func TestRoutingDefaultsAndGeo(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	s.NodeAddress(ctx, "de-1", "198.51.100.7:4000")
	c := renderFor(t, s, "de-1")
	rt := ruleText(c)
	if !strings.Contains(rt, `"10.0.0.0/8"`) || !strings.Contains(rt, `"198.51.100.7"`) || !strings.Contains(rt, `"bittorrent"`) {
		t.Fatalf("safe defaults missing: %s", rt)
	}
	if !strings.Contains(rt, `"inboundTag":["api"]`) {
		t.Fatal("api rule must stay first")
	}
	for _, in := range c.Inbounds {
		if sn, ok := in["sniffing"].(map[string]any); ok && sn["routeOnly"] != true {
			t.Fatalf("sniffing not routeOnly: %v", in["tag"])
		}
	}

	r := s.Routing(ctx)
	r.BlockAds, r.Iran = true, "block"
	r.Rules = []RouteRule{{Enabled: true, Domains: []string{"geosite:google"}, Outbound: "direct"}, {Enabled: true, Domains: []string{"domain:example.com"}, Outbound: "block"}}
	if err := s.PutRouting(ctx, r); err != nil {
		t.Fatal(err)
	}
	rt = ruleText(renderFor(t, s, "de-1"))
	if strings.Contains(rt, "geosite:") || strings.Contains(rt, "geoip:") || !strings.Contains(rt, "domain:example.com") {
		t.Fatalf("geo rules before the node has geo files: %s", rt)
	}
	if st := s.RoutingStatus(ctx); len(st.NodesWithoutGeo) != 1 {
		t.Fatalf("status %+v", st)
	}
	s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", Assets: map[string]string{"geoip.dat": "a", "geosite.dat": "b"}})
	c = renderFor(t, s, "de-1")
	rt = ruleText(c)
	for _, want := range []string{"geosite:google", "geosite:category-ads-all", "geosite:ir", "geoip:ir"} {
		if !strings.Contains(rt, want) {
			t.Fatalf("%s missing: %s", want, rt)
		}
	}
	if c.Routing.DomainStrategy != "IPIfNonMatch" {
		t.Fatalf("geoip rules need IPIfNonMatch, got %s", c.Routing.DomainStrategy)
	}
	if strings.Index(rt, "geosite:google") > strings.Index(rt, "category-ads-all") {
		t.Fatal("custom rules must come before ad and Iran rules")
	}
}

func TestRoutingOutboundsScopeAndValidation(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "fi-1", "Finland"); err != nil {
		t.Fatal(err)
	}
	warp := json.RawMessage(`{"protocol":"wireguard","settings":{"secretKey":"x","address":["172.16.0.2/32"],"peers":[{"publicKey":"y","endpoint":"engage.cloudflareclient.com:2408"}]}}`)
	r := defaultRouting()
	r.Outbounds = []RouteOutbound{{Tag: "warp", Kind: "warp", Nodes: []string{"de-1"}, Config: warp},
		{Tag: "chain", Kind: "custom", Config: json.RawMessage(`{"protocol":"socks","settings":{"servers":[{"address":"203.0.113.9","port":1080}]}}`)}}
	r.Balancers = []RouteBalancer{{Tag: "lb", Outbounds: []string{"warp", "chain"}, Strategy: "random"}}
	r.Rules = []RouteRule{{Enabled: true, Domains: []string{"domain:openai.com"}, Outbound: "warp"}, {Enabled: true, Ports: "25", Outbound: "lb"},
		{Enabled: false, Ports: "53", Outbound: "block"}, {Enabled: true, Users: []string{"alice"}, Outbound: "chain", Nodes: []string{"fi-1"}}}
	r.DNS = []string{"https://1.1.1.1/dns-query"}
	if err := s.PutRouting(ctx, r); err != nil {
		t.Fatal(err)
	}
	de := renderFor(t, s, "de-1")
	fi := renderFor(t, s, "fi-1")
	tags := func(c renderedCfg) string {
		var out []string
		for _, o := range c.Outbounds {
			out = append(out, o["tag"].(string))
		}
		return strings.Join(out, ",")
	}
	if tags(de) != "direct,block,warp,chain" || tags(fi) != "direct,block,chain" {
		t.Fatalf("outbounds de=%s fi=%s", tags(de), tags(fi))
	}
	if rt := ruleText(de); !strings.Contains(rt, "openai.com") || !strings.Contains(rt, `"balancerTag":"lb"`) || strings.Contains(rt, `"53"`) || strings.Contains(rt, "alice") {
		t.Fatalf("de rules %s", rt)
	}
	if rt := ruleText(fi); strings.Contains(rt, "openai.com") || !strings.Contains(rt, "alice") {
		t.Fatalf("fi rules (warp is de-only) %s", rt)
	}
	if len(fi.Routing.Balancers) != 1 || de.DNS == nil {
		t.Fatalf("balancer on fi (chain only) and dns: %+v %+v", fi.Routing.Balancers, de.DNS)
	}
	for _, bad := range []Routing{
		{Rules: []RouteRule{{Enabled: true, Ports: "1", Outbound: "nope"}}},
		{Outbounds: []RouteOutbound{{Tag: "direct", Config: warp}}},
		{Outbounds: []RouteOutbound{{Tag: "x", Config: json.RawMessage(`{}`)}}},
		{Rules: []RouteRule{{Enabled: true, Outbound: "block"}}},
		{Rules: []RouteRule{{Enabled: true, IPs: []string{"1.2.3"}, Outbound: "block"}}},
		{Iran: "nowhere"},
	} {
		if err := s.PutRouting(ctx, bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// A refused policy leaves the old one in place.
	if got := s.Routing(ctx); len(got.Outbounds) != 2 {
		t.Fatal("old policy lost after a refused save")
	}
	// Swap forwarders are exempt from the private/own-address block.
	_, routing, _, _ := s.nodeRouting(ctx, "de-1", []string{"swap-x"})
	b, _ := json.Marshal(routing["rules"])
	if !strings.Contains(string(b), `"inboundTag":["swap-x"],"outboundTag":"direct"`) {
		t.Fatalf("swap exemption missing: %s", b)
	}
}

func TestRegisterWARP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if in["key"] == "" || r.Method != "POST" {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"config":{"client_id":"AQID","peers":[{"public_key":"bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}],"interface":{"addresses":{"v4":"172.16.0.2","v6":"2606:4700::2"}}}}`))
	}))
	defer srv.Close()
	old := warpAPI
	warpAPI = srv.URL
	defer func() { warpAPI = old }()
	raw, err := RegisterWARP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"protocol":"wireguard"`, `"reserved":[1,2,3]`, `"172.16.0.2/32"`, `"2606:4700::2/128"`, "engage.cloudflareclient.com:2408", `"secretKey":"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("%s missing in %s", want, s)
		}
	}
}
