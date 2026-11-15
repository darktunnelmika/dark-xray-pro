package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestOwnerAlerts(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	var mu sync.Mutex
	var got []string
	old := ownerNotify
	ownerNotify = func(_ context.Context, text string) { mu.Lock(); got = append(got, text); mu.Unlock() }
	defer func() { ownerNotify = old }()
	if err := s.PutAlerts(ctx, AlertSettings{CPUPercent: 30, ReportHours: 24}); err == nil {
		t.Fatal("thresholds under 50 must be refused")
	}
	if err := s.PutAlerts(ctx, AlertSettings{Login: true, CPUPercent: 80, ReportHours: 6}); err != nil {
		t.Fatal(err)
	}
	if a := s.Alerts(ctx); !a.Login || a.CPUPercent != 80 || a.ReportHours != 6 {
		t.Fatal("alerts", a)
	}
	wait := func() { time.Sleep(50 * time.Millisecond) }
	s.checkLoad(ctx, "node:x", "نود X", "پردازنده", 95, 80)
	wait()
	if len(got) != 0 {
		t.Fatal("one high reading must not alert")
	}
	s.checkLoad(ctx, "node:x", "نود X", "پردازنده", 96, 80)
	s.checkLoad(ctx, "node:x", "نود X", "پردازنده", 97, 80)
	wait()
	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n != 1 || !strings.Contains(got[0], "نود X") {
		t.Fatalf("one alert expected: %v", got)
	}
	s.checkLoad(ctx, "node:x", "نود X", "پردازنده", 40, 80)
	wait()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || !strings.Contains(got[1], "عادی") {
		t.Fatalf("calm-down note expected: %v", got)
	}
	r, _ := http.NewRequest("POST", "/", nil)
	r.RemoteAddr = "9.9.9.9:1"
	mu.Unlock()
	s.noteLogin(ctx, "boss", r)
	wait()
	mu.Lock()
	if len(got) != 3 || !strings.Contains(got[2], "boss") || !strings.Contains(got[2], "9.9.9.9") {
		t.Fatalf("login notice: %v", got)
	}
}

func TestPruneAndBackupLeftovers(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	old := time.Now().AddDate(-2, 0, 0).Unix()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(unix,actor,role,method,path,status) VALUES(?,?,?,?,?,?),(?,?,?,?,?,?)`,
		old, "a", "owner", "GET", "/x", 200, time.Now().Unix(), "b", "owner", "GET", "/y", 200); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(ctx, 400); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&n)
	if n != 1 {
		t.Fatal("audit rows after prune:", n)
	}
	dir := t.TempDir()
	stale := filepath.Join(dir, "snapshot-123.sqlite")
	_ = os.WriteFile(stale, []byte("x"), 0o600)
	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(stale, past, past)
	fresh := filepath.Join(dir, "snapshot-456.sqlite")
	_ = os.WriteFile(fresh, []byte("x"), 0o600)
	if _, err := s.AutoBackup(ctx, dir, 14); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale snapshot left behind")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a snapshot still being written must stay")
	}
}

func TestNodeDownAlert(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	var mu sync.Mutex
	var got []string
	old := ownerNotify
	ownerNotify = func(_ context.Context, text string) { mu.Lock(); got = append(got, text); mu.Unlock() }
	defer func() { ownerNotify = old }()
	count := func() int { time.Sleep(40 * time.Millisecond); mu.Lock(); defer mu.Unlock(); return len(got) }
	s.checkDown(ctx, "off:n1", "down n1", "up n1", true)
	if count() != 0 {
		t.Fatal("one bad check must not alert")
	}
	s.checkDown(ctx, "off:n1", "down n1", "up n1", true)
	s.checkDown(ctx, "off:n1", "down n1", "up n1", true)
	if count() != 1 {
		t.Fatal("exactly one down alert expected", got)
	}
	s.checkDown(ctx, "off:n1", "down n1", "up n1", false)
	if count() != 2 || !strings.HasPrefix(got[1], "up n1") {
		t.Fatal("recovery alert expected", got)
	}
	s.checkDown(ctx, "off:n2", "down n2", "up n2", true)
	s.checkDown(ctx, "off:n2", "down n2", "up n2", false)
	if count() != 2 {
		t.Fatal("a blip must stay silent", got)
	}
	if a := s.Alerts(ctx); !a.Nodes {
		t.Fatal("node alerts are on by default")
	}
}

func TestCustomStream(t *testing.T) {
	ep := []sub.Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 443}}
	ok := []StreamSpec{
		{Protocol: "vless", Network: "grpc", Security: "reality", GRPCMulti: true, Path: "svc"},
		{Protocol: "vless", Network: "tcp", Security: "reality", Vision: true, Fingerprint: "firefox", ServerNames: []string{"www.example.org"}},
		{Protocol: "trojan", Network: "xhttp", Security: "reality", XHTTPMode: "packet-up"},
		{Protocol: "vmess", Network: "ws", Security: "none", Host: "cdn.example.com"},
		{Protocol: "vless", Network: "httpupgrade", Security: "tls"},
	}
	for _, s := range ok {
		s := s
		p := PresetRequest{Tag: "x", Preset: "custom", Port: 443, SNI: "www.speedtest.net", Cert: "c1", Endpoints: ep, Stream: &s}
		tpl, meta, err := BuildPreset(p)
		if err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		var tt map[string]any
		_ = json.Unmarshal(tpl, &tt)
		st := tt["streamSettings"].(map[string]any)
		if tt["protocol"] != s.Protocol || st["network"] != s.Network || st["security"] != s.Security {
			t.Fatalf("%+v → %s", s, tpl)
		}
		in := sub.Inbound{Tag: "x", Template: tpl}
		_ = json.Unmarshal(meta, &in.Meta)
		links, err := sub.Links(sub.Account{Email: "a", UUID: "11111111-2222-3333-4444-555555555555", Secret: "pw"}, []sub.Inbound{in})
		if err != nil || len(links) != 1 || s.Protocol != "vmess" && (!strings.Contains(links[0], "type="+s.Network) || !strings.Contains(links[0], "security="+s.Security)) {
			t.Fatalf("%+v links %v %v", s, links, err)
		}
		if s.Network == "grpc" && !strings.Contains(links[0], "mode=multi") || s.Vision && !strings.Contains(links[0], "flow=xtls-rprx-vision") || s.Fingerprint == "firefox" && !strings.Contains(links[0], "fp=firefox") {
			t.Fatalf("%+v link %s", s, links[0])
		}
	}
	bad := []StreamSpec{
		{Protocol: "vmess", Network: "tcp", Security: "reality"},
		{Protocol: "vless", Network: "ws", Security: "reality"},
		{Protocol: "vless", Network: "grpc", Security: "reality", Vision: true},
		{Protocol: "vless", Network: "tcp", Security: "none", Vision: true},
		{Protocol: "ss", Network: "tcp", Security: "none"},
		{Protocol: "vless", Network: "xhttp", Security: "tls", XHTTPMode: "weird"},
	}
	for _, s := range bad {
		if err := checkStream(s); err == nil {
			t.Fatalf("must refuse %+v", s)
		}
	}
}

func TestComposeFromTemplate(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	route := func(port int) []Route {
		return []Route{{NodeID: "de-1", DirectPort: port, Hosts: []Host{{Path: "direct", Enabled: true, Address: "1.2.3.4", Port: port}}}}
	}
	cases := []struct {
		tag, tpl, want string
	}{
		{"t-kcp", `{"protocol":"vless","settings":{},"streamSettings":{"network":"kcp","security":"none","kcpSettings":{"mtu":1350,"header":{"type":"wechat-video"},"seed":"s3"}}}`, "headerType=wechat-video"},
		{"t-http", `{"protocol":"vless","settings":{},"streamSettings":{"network":"tcp","security":"none","tcpSettings":{"header":{"type":"http","request":{"path":["/x"],"headers":{"Host":["a.com"]}}}}}}`, "headerType=http"},
		{"t-tls", `{"protocol":"trojan","settings":{},"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"x.example.com","certificates":[{"dark_cert":"pending"}]},"wsSettings":{"path":"/w"}}}`, "allowInsecure=1"},
		{"t-ss", `{"protocol":"shadowsocks","settings":{"method":"aes-256-gcm","network":"tcp,udp"}}`, "ss://"},
	}
	port := 30000
	for _, c := range cases {
		port++
		if err := s.ComposeInbound(ctx, PresetRequest{Tag: c.tag, Preset: "template", Template: []byte(c.tpl), Meta: []byte(`{"remark":"T"}`)}, route(port)); err != nil {
			t.Fatalf("%s: %v", c.tag, err)
		}
		cl, _ := s.Clients(ctx)
		var bob Client
		for _, x := range cl {
			if x.Email == "bob" {
				bob = x.Client
			}
		}
		bob.Inbounds = append(bob.Inbounds, c.tag)
		if err := s.PutClient(ctx, bob); err != nil {
			t.Fatal(err)
		}
		links, _, err := s.Subscription(ctx, "bob")
		found := false
		for _, l := range links {
			if strings.Contains(l, "#T") || strings.Contains(l, c.want) && !strings.Contains(l, "127.0.0.1") {
				if strings.Contains(l, c.want) {
					found = true
				}
			}
		}
		if err != nil || !found {
			t.Fatalf("%s: link with %q missing in %v (%v)", c.tag, c.want, links, err)
		}
	}
	if _, err := s.RenderNode(ctx, "de-1"); err != nil {
		t.Fatal("render:", err)
	}
	if err := s.ComposeInbound(ctx, PresetRequest{Tag: "t-bad", Preset: "template", Template: []byte(`{"settings":{}}`)}, route(30100)); err == nil {
		t.Fatal("a template without a protocol must be refused")
	}
}

func TestRepBrand(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSeller(ctx, "ali", Rep{ID: "ali-sub", Name: "Sub", Enabled: true}, "alisub", "password-123456"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSubSettings(ctx, SubSettings{Title: "Main VPN", SupportURL: "https://t.me/main"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBrand(ctx, "ali", RepBrand{Title: "Ali VPN", SupportURL: "https://t.me/ali", BaseURL: "https://sub.ali.example", Accent: "#ff0088"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBrand(ctx, "ali", RepBrand{BaseURL: "ftp://x"}); err == nil {
		t.Fatal("bad address accepted")
	}
	for _, c := range []Client{{Email: "ali-cust", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}}, {Email: "sub-cust", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}}} {
		if err := s.PutClient(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE clients SET rep_id='ali' WHERE email='ali-cust'`)
	_, _ = s.db.ExecContext(ctx, `UPDATE clients SET rep_id='ali-sub' WHERE email='sub-cust'`)
	for _, e := range []string{"ali-cust", "sub-cust"} {
		set, base := s.brandFor(ctx, e)
		if set.Title != "Ali VPN" || set.SupportURL != "https://t.me/ali" || set.Accent != "#ff0088" || base != "https://sub.ali.example" {
			t.Fatalf("%s: %+v %s", e, set, base)
		}
		d, err := s.subPageData(ctx, e, "https://sub.ali.example/sub/x", "fa")
		if err != nil || d.Title != "Ali VPN" || d.Support != "https://t.me/ali" {
			t.Fatalf("%s page: %+v %v", e, d, err)
		}
	}
	if set, base := s.brandFor(ctx, "alice"); set.Title != "Main VPN" || base != "" {
		t.Fatalf("a direct customer must keep the panel's brand: %+v %q", set, base)
	}
	if got := s.SubBaseFor(ctx, "alice", "https://hub"); got != "https://hub" {
		t.Fatal(got)
	}
}

func TestSmartOrder(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mk := func(id string, cpu float64, seen int64, xray bool) {
		if _, err := s.CreateNode(ctx, id, id); err != nil {
			t.Fatal(err)
		}
		host, _ := json.Marshal(wire.Host{CPUPercent: cpu, RAMUsed: 1, RAMTotal: 10})
		_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET last_seen_unix=?, xray_running=?, host_json=? WHERE id=?`, seen, xray, string(host), id)
	}
	now := s.now().Unix()
	mk("busy", 95, now, true)
	mk("calm", 5, now, true)
	mk("dead", 1, now-3600, true)
	ins := []sub.Inbound{
		{Tag: "a-busy", Meta: sub.Meta{Endpoints: []sub.Endpoint{{Node: "busy", Address: "1.1.1.1", Port: 1}}}},
		{Tag: "b-dead", Meta: sub.Meta{Endpoints: []sub.Endpoint{{Node: "dead", Address: "3.3.3.3", Port: 1}}}},
		{Tag: "c-calm", Meta: sub.Meta{Endpoints: []sub.Endpoint{{Node: "dead", Address: "4.4.4.4", Port: 1}, {Node: "calm", Address: "2.2.2.2", Port: 1}}}},
	}
	got := s.rankInbounds(ctx, append([]sub.Inbound(nil), ins...))
	sorted := append([]sub.Inbound(nil), got...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Rank < sorted[j].Rank })
	if sorted[0].Tag != "c-calm" || sorted[1].Tag != "a-busy" || sorted[2].Tag != "b-dead" {
		t.Fatalf("order %v %v %v", sorted[0].Tag, sorted[1].Tag, sorted[2].Tag)
	}
	if sorted[0].Meta.Endpoints[0].Address != "2.2.2.2" {
		t.Fatal("the live address must come first inside an inbound")
	}
	_ = s.SetSetting(ctx, "sub_hide_down", "1")
	got = s.rankInbounds(ctx, append([]sub.Inbound(nil), ins...))
	for _, in := range got {
		for _, e := range in.Meta.Endpoints {
			if e.Node == "dead" {
				t.Fatal("a down node's address must be left out")
			}
		}
	}
	if len(got) != 2 {
		t.Fatalf("an inbound only on the down node must be left out: %d", len(got))
	}
	_ = s.SetSetting(ctx, "sub_smart", "0")
	if got := s.rankInbounds(ctx, append([]sub.Inbound(nil), ins...)); got[0].Tag != "a-busy" {
		t.Fatal("smart order off must keep the list as is")
	}
}

func TestInboundSpeed(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "n1", "n1"); err != nil {
		t.Fatal(err)
	}
	for i, tag := range []string{"cheap", "vip"} {
		if err := s.CreatePreset(ctx, PresetRequest{Tag: tag, NodeID: "n1", Preset: "vless-reality-tcp", Port: 443 + i, SNI: "www.speedtest.net",
			Endpoints: []sub.Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 443 + i}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutClient(ctx, Client{Email: "u", Enabled: true, VolumeBytes: -1, Inbounds: []string{"cheap", "vip"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInboundSpeed(ctx, "cheap", 10); err == nil {
		t.Fatal("a cap under 64 kbit/s must be refused")
	}
	if err := s.SetInboundSpeed(ctx, "cheap", 15000); err != nil {
		t.Fatal(err)
	}
	render := func() string {
		cfg, err := s.RenderNode(ctx, "n1")
		if err != nil {
			t.Fatal(err)
		}
		return string(cfg)
	}
	if strings.Contains(render(), "dark-limit") {
		t.Fatal("an old agent has no limiter; nothing may route to it")
	}
	_, _ = s.db.Exec(`UPDATE nodes SET agent_version='v2.10.0' WHERE id='n1'`)
	cfg := render()
	if !strings.Contains(cfg, `"pass":"15000"`) || !strings.Contains(cfg, `"user":"inbound:cheap"`) || !strings.Contains(cfg, `"inboundTag":["cheap"`) || strings.Contains(cfg, `inbound:vip`) {
		t.Fatal(cfg)
	}
	list, _ := s.Inbounds(ctx)
	for _, v := range list {
		if v.Tag == "cheap" && v.SpeedKbps != 15000 {
			t.Fatal("the list must show the cap")
		}
	}
	_ = s.SetInboundSpeed(ctx, "cheap", 0)
	if strings.Contains(render(), "dark-limit") {
		t.Fatal("removing the cap must remove the route")
	}
}
