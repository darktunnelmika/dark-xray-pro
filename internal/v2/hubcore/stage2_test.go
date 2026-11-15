package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
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

func TestHostFragmentAndMux(t *testing.T) {
	tpl := []byte(`{"protocol":"vless","port":443,"settings":{},"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"a.com"},"wsSettings":{"path":"/x"}}}`)
	in := sub.Inbound{Tag: "w", Template: tpl, Meta: sub.Meta{Endpoints: []sub.Endpoint{{Name: "cdn", Address: "1.1.1.1", Port: 443, Fragment: true, Mux: 8}, {Name: "plain", Address: "2.2.2.2", Port: 443}}}}
	b, err := sub.XrayJSON(sub.Account{Email: "a", UUID: "u"}, []sub.Inbound{in})
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	_ = json.Unmarshal(b, &list)
	if len(list) != 2 {
		t.Fatal(string(b))
	}
	first, _ := json.Marshal(list[0])
	second, _ := json.Marshal(list[1])
	if !strings.Contains(string(first), `"fragment"`) || !strings.Contains(string(first), `"concurrency":8`) {
		t.Fatal("the cdn address asks for fragment and mux", string(first))
	}
	if strings.Contains(string(second), `"fragment"`) || strings.Contains(string(second), `"mux"`) {
		t.Fatal("other addresses stay plain", string(second))
	}
}

func TestAmneziaWGAndMTProto(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	for i, p := range []PresetRequest{{Tag: "awg", Preset: "amneziawg"}, {Tag: "tgp", Preset: "mtproto", SNI: "www.cloudflare.com"}} {
		if err := s.ComposeInbound(ctx, p, []Route{{NodeID: "de-1", DirectPort: 51820 + i, Hosts: []Host{{Path: "direct", Enabled: true, Address: "1.2.3.4", Port: 51820 + i}}}}); err != nil {
			t.Fatal(p.Preset, err)
		}
	}
	c, _ := s.ClientByEmail(ctx, "alice")
	c.Inbounds = append(c.Inbounds, "awg")
	_ = s.PutClient(ctx, c.Client)
	// An agent older than v2.6 never gets them (its Xray would refuse the config).
	cfg, _ := s.RenderNode(ctx, "de-1")
	if strings.Contains(string(cfg), "amneziawg") || strings.Contains(string(cfg), "mtproto") {
		t.Fatal("old agent must not get side inbounds")
	}
	if _, err := s.db.Exec(`UPDATE nodes SET agent_version='v2.6.0' WHERE id='de-1'`); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.RenderNode(ctx, "de-1")
	var parsed struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	_ = json.Unmarshal(cfg, &parsed)
	var awg, tgp map[string]any
	for _, in := range parsed.Inbounds {
		switch in["tag"] {
		case "awg":
			awg = in
		case "tgp":
			tgp = in
		}
	}
	if awg == nil || tgp == nil {
		t.Fatal(string(cfg))
	}
	set := awg["settings"].(map[string]any)
	peers, _ := set["peers"].([]any)
	if len(peers) != 1 || set["jc"] == nil || set["h4"] == nil || set["secretKey"] == nil {
		t.Fatal("awg settings", set)
	}
	if sec, _ := tgp["settings"].(map[string]any)["secret"].(string); !strings.HasPrefix(sec, "ee") || !strings.HasSuffix(sec, "7777772e636c6f7564666c6172652e636f6d") {
		t.Fatal("mtproto secret must carry the fake-TLS domain", sec)
	}
	if strings.Contains(string(stripSide(cfg)), "amneziawg") {
		t.Fatal("xray's own check sees no side inbounds")
	}
	links, err := s.ClientLinks(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var conf string
	for _, l := range links {
		if l.Kind == "awg" {
			conf = l.Link
		}
		if strings.HasPrefix(l.Link, "amneziawg") || strings.Contains(l.Link, "mtproto") {
			t.Fatal("not in plain subscriptions", l.Link)
		}
	}
	for _, want := range []string{"[Interface]", "PrivateKey = ", "Address = 10.67.0.2/32", "Jc = ", "H4 = ", "[Peer]", "Endpoint = 1.2.3.4:51820"} {
		if !strings.Contains(conf, want) {
			t.Fatalf("awg conf misses %q:\n%s", want, conf)
		}
	}
}

func TestTUICPreset(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.ComposeInbound(ctx, PresetRequest{Tag: "tq", Preset: "tuic"}, []Route{{NodeID: "de-1", DirectPort: 8443, Hosts: []Host{{Path: "direct", Enabled: true, Address: "1.2.3.4", Port: 8443}}}}); err != nil {
		t.Fatal(err)
	}
	c, _ := s.ClientByEmail(ctx, "alice")
	c.Inbounds = append(c.Inbounds, "tq")
	_ = s.PutClient(ctx, c.Client)
	// TUIC needs agent v2.7; a v2.6 agent never gets it.
	if _, err := s.db.Exec(`UPDATE nodes SET agent_version='v2.6.0' WHERE id='de-1'`); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.RenderNode(ctx, "de-1"); strings.Contains(string(cfg), `"tuic"`) {
		t.Fatal("v2.6 agent must not get TUIC")
	}
	_, _ = s.db.Exec(`UPDATE nodes SET agent_version='v2.7.0' WHERE id='de-1'`)
	cfg, _ := s.RenderNode(ctx, "de-1")
	var parsed struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []map[string]any `json:"clients"`
			} `json:"settings"`
			Stream struct {
				TLS struct {
					Certificates []map[string]any `json:"certificates"`
				} `json:"tlsSettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	_ = json.Unmarshal(cfg, &parsed)
	found := false
	for _, in := range parsed.Inbounds {
		if in.Tag != "tq" {
			continue
		}
		found = true
		if in.Protocol != "tuic" || len(in.Settings.Clients) != 1 || in.Settings.Clients[0]["uuid"] == nil || in.Settings.Clients[0]["password"] == nil || in.Settings.Clients[0]["email"] != "alice" {
			t.Fatalf("tuic inbound %+v", in)
		}
		if len(in.Stream.TLS.Certificates) != 1 || in.Stream.TLS.Certificates[0]["certificate"] == nil {
			t.Fatal("tuic certificate must be inline for the agent")
		}
	}
	if !found || strings.Contains(string(stripSide(cfg)), `"tuic"`) {
		t.Fatal("tuic rendered for the agent, never for Xray")
	}
	links, err := s.ClientLinks(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var tl string
	for _, l := range links {
		if l.Kind == "tuic" {
			tl = l.Link
		}
	}
	if !strings.HasPrefix(tl, "tuic://") || !strings.Contains(tl, "congestion_control=bbr") || !strings.Contains(tl, "allow_insecure=1") || !strings.Contains(tl, "@1.2.3.4:8443") {
		t.Fatal("tuic link", tl)
	}
	sb, _, _, err := s.SubscriptionBody(ctx, "alice", "singbox")
	if err != nil || !strings.Contains(string(sb), `"type":"tuic"`) && !strings.Contains(string(sb), `"type": "tuic"`) {
		t.Fatal("sing-box tuic", err)
	}
	cl, _, _, err := s.SubscriptionBody(ctx, "alice", "clash")
	if err != nil || !strings.Contains(string(cl), "type: tuic") || !strings.Contains(string(cl), "congestion-controller: bbr") {
		t.Fatal("clash tuic", err, string(cl))
	}
	if xj, _, _, _ := s.SubscriptionBody(ctx, "alice", "xray"); strings.Contains(string(xj), "tuic") {
		t.Fatal("xray has no tuic outbound")
	}
}

func TestDeletedClientLeavesNoUsage(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('alice',500,900,100,200)`); err != nil {
		t.Fatal(err)
	}
	_, _ = s.db.Exec(`INSERT INTO online(email,node_id,ips,seen_unix) VALUES('alice','de-1','["1.1.1.1"]',200)`)
	if err := s.DeleteClient(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutClient(ctx, Client{Email: "alice", Enabled: true, VolumeBytes: -1}); err != nil {
		t.Fatal(err)
	}
	u, _ := s.ClientUsage(ctx, "alice")
	if u.Up+u.Down != 0 || u.FirstSeen != 0 {
		t.Fatalf("a new client with an old name must start clean: %+v", u)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM online WHERE email='alice'`).Scan(&n)
	if n != 0 {
		t.Fatal("online row survived the delete")
	}
}

func TestNodePublicIPPinAndTunnel(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	s.NodeAddress(ctx, "de-1", "127.0.0.1:5000") // through a local tunnel: ignored
	var ip string
	_ = s.db.QueryRow(`SELECT public_ip FROM nodes WHERE id='de-1'`).Scan(&ip)
	if ip == "127.0.0.1" {
		t.Fatal("a tunnelled connection must not become the node address")
	}
	if err := s.SetNodePublicIP(ctx, "de-1", "10.0.0.5"); !errors.Is(err, ErrInvalid) {
		t.Fatal("private addresses are refused", err)
	}
	if err := s.SetNodePublicIP(ctx, "de-1", "167.233.192.101"); err != nil {
		t.Fatal(err)
	}
	s.NodeAddress(ctx, "de-1", "198.51.100.7:4000") // pinned: not overwritten
	_ = s.db.QueryRow(`SELECT public_ip FROM nodes WHERE id='de-1'`).Scan(&ip)
	if ip != "167.233.192.101" {
		t.Fatal("pinned address changed", ip)
	}
	_ = s.SetNodePublicIP(ctx, "de-1", "")
	s.NodeAddress(ctx, "de-1", "198.51.100.7:4000")
	_ = s.db.QueryRow(`SELECT public_ip FROM nodes WHERE id='de-1'`).Scan(&ip)
	if ip != "198.51.100.7" {
		t.Fatal("automatic again after clearing", ip)
	}
}

func TestBulkClientActions(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	now := s.now().Unix()
	_ = s.PutClient(ctx, Client{Email: "old1", Enabled: true, VolumeBytes: -1, ExpiresUnix: now - 40*86400, Inbounds: []string{"de-vless"}})
	_ = s.PutClient(ctx, Client{Email: "fresh", Enabled: true, VolumeBytes: -1, ExpiresUnix: now - 86400, Inbounds: []string{"de-vless"}})
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('alice',5,7,1,1)`)
	// Preview, then delete clients expired more than 30 days ago.
	list, err := s.CleanupClients(ctx, CleanupRequest{Expired: true, MinDays: 30, DryRun: true})
	if err != nil || len(list) != 1 || list[0] != "old1" {
		t.Fatalf("preview %v %v", list, err)
	}
	if _, err := s.ClientByEmail(ctx, "old1"); err != nil {
		t.Fatal("a dry run must not delete")
	}
	if done, _ := s.CleanupClients(ctx, CleanupRequest{Expired: true, MinDays: 30}); len(done) != 1 {
		t.Fatal("cleanup", done)
	}
	if _, err := s.ClientByEmail(ctx, "old1"); err == nil {
		t.Fatal("old1 must be gone")
	}
	// Zero the usage of everyone on an inbound.
	c, _ := s.ClientByEmail(ctx, "alice")
	tag := c.Inbounds[0]
	if n, err := s.ResetInboundUsage(ctx, tag); err != nil || n == 0 {
		t.Fatal("reset", n, err)
	}
	if c, _ := s.ClientByEmail(ctx, "alice"); c.Usage.Up+c.Usage.Down != 0 {
		t.Fatal("usage not reset")
	}
	// Copy a client: same plan and inbounds, new credentials.
	if err := s.CloneClient(ctx, "alice", "alice-2"); err != nil {
		t.Fatal(err)
	}
	a, _ := s.ClientByEmail(ctx, "alice")
	b, _ := s.ClientByEmail(ctx, "alice-2")
	if b.VolumeBytes != a.VolumeBytes || len(b.Inbounds) != len(a.Inbounds) || b.UUID == a.UUID || b.SubToken == a.SubToken {
		t.Fatalf("clone %+v vs %+v", b.Client, a.Client)
	}
	if err := s.CloneClient(ctx, "alice", "alice-2"); err == nil {
		t.Fatal("cloning onto an existing name must fail")
	}
}
