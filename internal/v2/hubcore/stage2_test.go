package hubcore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestNodeTrafficPercent(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.SetNodeTrafficPercent(ctx, "de-1", 5); err == nil {
		t.Fatal("out of range")
	}
	if err := s.SetNodeTrafficPercent(ctx, "de-1", 150); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{Seq: 1, Traffic: []wire.UserTraffic{{Email: "alice", Up: 100, Down: 200}},
		Inbounds: []wire.UserTraffic{{Email: "de-vless", Up: 100, Down: 200}}}); err != nil {
		t.Fatal(err)
	}
	c, _ := s.ClientByEmail(ctx, "alice")
	if c.Usage.Up != 150 || c.Usage.Down != 300 {
		t.Fatalf("billed usage %+v", c.Usage)
	}
	var in int64
	_ = s.db.QueryRow(`SELECT up+down FROM inbound_traffic WHERE tag='de-vless'`).Scan(&in)
	if in != 300 {
		t.Fatal("server totals stay real", in)
	}
	ns, _ := s.Nodes(ctx)
	if ns[0].TrafficPercent != 150 {
		t.Fatal(ns[0].TrafficPercent)
	}
}

func TestPlainInbounds(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	for i, p := range []PresetRequest{{Tag: "px", Preset: "socks"}, {Tag: "hp", Preset: "http"}, {Tag: "fw", Preset: "dokodemo", Path: "9.9.9.9:443"}} {
		if err := s.ComposeInbound(ctx, p, []Route{{NodeID: "de-1", DirectPort: 1080 + i, Hosts: []Host{{Path: "direct", Enabled: true, Address: "1.2.3.4", Port: 1080 + i}}}}); err != nil {
			t.Fatal(p.Preset, err)
		}
	}
	if err := s.ComposeInbound(ctx, PresetRequest{Tag: "bad", Preset: "dokodemo", Path: "nope"}, []Route{{NodeID: "de-1", DirectPort: 1999}}); err == nil {
		t.Fatal("a forward needs a target")
	}
	c, _ := s.ClientByEmail(ctx, "alice")
	c.Inbounds = append(c.Inbounds, "px")
	_ = s.PutClient(ctx, c.Client)
	cfg, err := s.RenderNode(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	_ = json.Unmarshal(cfg, &parsed)
	seen := 0
	for _, in := range parsed.Inbounds {
		set, _ := in["settings"].(map[string]any)
		switch in["tag"] {
		case "px":
			seen++
			if set["clients"] != nil || set["auth"] != "password" {
				t.Fatal("socks: no panel clients, password auth", set)
			}
		case "fw":
			seen++
			if set["address"] != "9.9.9.9" || intOf(set["port"]) != 443 {
				t.Fatal(set)
			}
		case "hp":
			seen++
		}
	}
	if seen != 3 {
		t.Fatal("plain inbounds rendered", seen)
	}
	links, _ := s.ClientLinks(ctx, "alice")
	for _, l := range links {
		if strings.HasPrefix(l.Link, "socks") {
			t.Fatal("plain inbounds stay out of subscriptions")
		}
	}
	raw, _ := s.InboundRaw(ctx, "px")
	if !strings.Contains(string(raw.Meta), "proxy_pass") {
		t.Fatal(string(raw.Meta))
	}
}

func TestOutboundTestQueue(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if _, err := s.StartOutboundTest(ctx, "de-1", "nope"); err == nil {
		t.Fatal("unknown outbound")
	}
	id, err := s.StartOutboundTest(ctx, "de-1", "direct")
	if err != nil || id == "" {
		t.Fatal(id, err)
	}
	tasks := s.openTasks(ctx, "de-1")
	if len(tasks) != 1 || tasks[0].Kind != wire.TaskOutboundTest || !strings.Contains(string(tasks[0].Args), `"tag":"direct"`) {
		t.Fatal(tasks)
	}
}

func TestInboundLimitsAndBranding(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.SetInboundLimits(ctx, "de-vless", 1000, 0); err != nil {
		t.Fatal(err)
	}
	day := now.UTC().Format("2006-01-02")
	if _, err := s.db.Exec(`INSERT INTO inbound_traffic(tag,day,up,down) VALUES('de-vless',?,400,700)`, day); err != nil {
		t.Fatal(err)
	}
	s.checkInboundLimits(ctx)
	raw, _ := s.InboundRaw(ctx, "de-vless")
	if raw.Enabled {
		t.Fatal("past its cap the inbound turns off")
	}
	_ = s.SetInboundEnabled(ctx, "de-vless", true)
	if err := s.SetInboundLimits(ctx, "de-vless", 5000, now.Unix()-1); err != nil {
		t.Fatal(err)
	}
	s.checkInboundLimits(ctx)
	if raw, _ = s.InboundRaw(ctx, "de-vless"); raw.Enabled {
		t.Fatal("past its end date the inbound turns off")
	}
	v := s.SubSettings(ctx)
	v.Accent, v.Logo = "#ff8800", "data:image/png;base64,iVBORw0KGgo="
	if err := s.PutSubSettings(ctx, v); err != nil {
		t.Fatal(err)
	}
	bad := v
	bad.Accent = "red;}body{display:none"
	if err := s.PutSubSettings(ctx, bad); err == nil {
		t.Fatal("a colour must be #rrggbb")
	}
	bad = v
	bad.Logo = "javascript:alert(1)"
	if err := s.PutSubSettings(ctx, bad); err == nil {
		t.Fatal("a logo must be an inline image")
	}
	d, err := s.subPageData(ctx, "alice", "https://x/sub/t", "fa")
	if err != nil || string(d.Accent) != "#ff8800" || !strings.HasPrefix(string(d.Logo), "data:image/png") {
		t.Fatal(d.Accent, d.Logo, err)
	}
}
