package hubcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/nodeagent"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

const vlessTemplate = `{"protocol":"vless","port":18580,"listen":"0.0.0.0","settings":{"flow":"xtls-rprx-vision"},
"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"dest":"example.com:443","serverNames":["example.com"],"privateKey":"test","shortIds":[""]}}}`

func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Unix(1_800_000_000, 0)
	s.SetClock(func() time.Time { return now })
	return s, &now
}

func seed(t *testing.T, s *Store) string {
	t.Helper()
	ctx := context.Background()
	tok, err := s.CreateNode(ctx, "de-1", "Germany")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutInbound(ctx, "de-vless", "de-1", []byte(vlessTemplate), true); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Client{
		{Email: "alice", Enabled: true, VolumeBytes: 1000, Inbounds: []string{"de-vless"}},
		{Email: "bob", Enabled: true, VolumeBytes: -1, DurationSeconds: 86400, MaxIPs: 2, Inbounds: []string{"de-vless"}},
	} {
		if err := s.PutClient(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	return tok
}

func TestRenderNodeInjectsClientsAndStatsAPI(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	cfg, err := s.RenderNode(context.Background(), "de-1")
	if err != nil {
		t.Fatal(err)
	}
	emails, err := nodeagent.ConfigEmails(cfg)
	if err != nil || len(emails) != 2 {
		t.Fatalf("emails %v %v", emails, err)
	}
	var parsed struct {
		Inbounds []struct {
			Tag      string `json:"tag"`
			Settings struct {
				Clients []map[string]string `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
		Policy map[string]any `json:"policy"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Inbounds[0].Tag != "api" || parsed.Inbounds[1].Settings.Clients[0]["flow"] != "xtls-rprx-vision" {
		t.Fatalf("unexpected config %s", cfg)
	}
	again, _ := s.RenderNode(context.Background(), "de-1")
	if string(again) != string(cfg) {
		t.Fatal("render must be deterministic")
	}
}

func TestUsageIsCountedExactlyOnce(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	b := wire.UsageBatch{Seq: 1, ToUnix: 1_800_000_000, Traffic: []wire.UserTraffic{{Email: "alice", Up: 100, Down: 200}}}
	for i := 0; i < 3; i++ {
		ack, err := s.ApplyUsage(ctx, "de-1", b)
		if err != nil || ack.Seq != 1 {
			t.Fatalf("ack %v %v", ack, err)
		}
	}
	u, _ := s.ClientUsage(ctx, "alice")
	if u.Up+u.Down != 300 {
		t.Fatalf("usage %+v, want 300 once", u)
	}
}

func TestFirstConnectionStartsTerm(t *testing.T) {
	s, now := newStore(t)
	seed(t, s)
	ctx := context.Background()
	resp, err := s.Sync(ctx, "de-1", wire.SyncRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range resp.Users {
		if u.Email == "bob" && u.ExpiresUnix != 0 {
			t.Fatal("unused first-connection plan must not expire yet")
		}
	}
	*now = now.Add(time.Hour)
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{Seq: 1, ToUnix: now.Unix(), Traffic: []wire.UserTraffic{{Email: "bob", Down: 1}}}); err != nil {
		t.Fatal(err)
	}
	resp, _ = s.Sync(ctx, "de-1", wire.SyncRequest{ConfigRevision: resp.ConfigRevision})
	for _, u := range resp.Users {
		if u.Email == "bob" && u.ExpiresUnix != now.Unix()+86400 {
			t.Fatalf("bob expires %d, want first use + 1 day", u.ExpiresUnix)
		}
	}
	if resp.Config != nil {
		t.Fatal("unchanged config must not be resent")
	}
}

func TestEndToEndHubOutage(t *testing.T) {
	s, now := newStore(t)
	tok := seed(t, s)
	ctx := context.Background()
	srv := httptest.NewTLSServer(NodeHandler(s))
	defer srv.Close()
	hub := &nodeagent.HTTPHub{BaseURL: srv.URL, NodeID: "de-1", Token: tok, Client: srv.Client()}
	x := &recordingXray{}
	st := &scriptedStats{}
	a, err := nodeagent.New(nodeagent.Config{NodeID: "de-1", StateDir: t.TempDir(), Now: func() time.Time { return *now }}, hub, x, st)
	if err != nil {
		t.Fatal(err)
	}
	a.Exchange(ctx)
	st.add("alice", 400)
	a.Collect(ctx)
	a.Exchange(ctx)
	u, _ := s.ClientUsage(ctx, "alice")
	if u.Up+u.Down != 400 {
		t.Fatalf("hub usage %d, want 400", u.Up+u.Down)
	}

	srv.Close() // HUB outage
	st.add("alice", 700)
	a.Collect(ctx)
	a.Exchange(ctx)
	if a.Spool().Len() != 1 {
		t.Fatal("usage must wait in the spool")
	}

	srv2 := httptest.NewTLSServer(NodeHandler(s))
	defer srv2.Close()
	hub.BaseURL, hub.Client = srv2.URL, srv2.Client()
	a.Exchange(ctx)
	u, _ = s.ClientUsage(ctx, "alice")
	if u.Up+u.Down != 1100 {
		t.Fatalf("hub usage %d after recovery, want 1100", u.Up+u.Down)
	}
}

func TestNodeAuth(t *testing.T) {
	s, _ := newStore(t)
	tok := seed(t, s)
	if s.AuthNode(context.Background(), "de-1", tok) != nil {
		t.Fatal("valid token rejected")
	}
	if s.AuthNode(context.Background(), "de-1", tok+"x") == nil || s.AuthNode(context.Background(), "nope", tok) == nil {
		t.Fatal("invalid credentials accepted")
	}
}

type recordingXray struct{ n int }

func (r *recordingXray) Apply(context.Context, []byte) (bool, error) { r.n++; return true, nil }
func (r *recordingXray) Status() (bool, uint64, string)              { return true, 1, "" }
func (r *recordingXray) Install(context.Context, []byte) error       { return nil }

type scriptedStats struct{ pending []wire.UserTraffic }

func (s *scriptedStats) add(email string, n int64) {
	s.pending = append(s.pending, wire.UserTraffic{Email: email, Down: n})
}
func (s *scriptedStats) TakeTraffic(context.Context) ([]wire.UserTraffic, error) {
	t := s.pending
	s.pending = nil
	return t, nil
}
func (s *scriptedStats) OnlineIPs(context.Context) (map[string][]string, error) { return nil, nil }

func TestSubscriptionLinks(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "de-1", "Germany"); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"public_key":"PUB","remark":"Dark VPN","endpoints":[{"name":"Direct","address":"167.233.192.101","port":18580}]}`)
	if err := s.PutInboundMeta(ctx, "de-vless", "de-1", []byte(vlessTemplate), meta, true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutClient(ctx, Client{Email: "alice", UUID: "u-1", Enabled: true, VolumeBytes: 1 << 30, Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	links, info, err := s.Subscription(ctx, "alice")
	if err != nil || len(links) != 1 || info != "upload=0; download=0; total=1073741824; expire=0" {
		t.Fatalf("links %v info %q err %v", links, info, err)
	}
}

func TestAdminAndPublicSubscription(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "de-1", "Germany"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandler(s, "admintok"))
	defer h.Close()
	do := func(method, path, body, tok string) (int, string) {
		req, _ := http.NewRequest(method, h.URL+path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, _ := do("GET", "/api/v2/admin/clients", "", "wrong"); code != 401 {
		t.Fatalf("bad token got %d", code)
	}
	inb := `{"node_id":"de-1","template":` + vlessTemplate + `,"meta":{"public_key":"PUB","endpoints":[{"name":"Direct","address":"1.2.3.4","port":18580}]}}`
	if code, body := do("PUT", "/api/v2/admin/inbounds/de-vless", inb, "admintok"); code != 200 {
		t.Fatalf("inbound %d %s", code, body)
	}
	if code, body := do("PUT", "/api/v2/admin/clients/alice", `{"Enabled":true,"VolumeBytes":-1,"MaxIPs":2,"Inbounds":["de-vless"]}`, "admintok"); code != 200 {
		t.Fatalf("client %d %s", code, body)
	}
	list, err := s.Clients(ctx)
	if err != nil || len(list) != 1 || list[0].SubToken == "" || list[0].Inbounds[0] != "de-vless" {
		t.Fatalf("clients %+v %v", list, err)
	}
	code, body := do("GET", "/sub/"+list[0].SubToken+"?raw", "", "")
	if code != 200 || !strings.HasPrefix(body, "vless://") {
		t.Fatalf("sub %d %q", code, body)
	}
	if code, _ := do("GET", "/sub/0000000000000000000", "", ""); code != 404 {
		t.Fatal("unknown sub token must 404")
	}
	// Updating the client keeps its subscription link.
	do("PUT", "/api/v2/admin/clients/alice", `{"Enabled":true,"VolumeBytes":1000,"Inbounds":["de-vless"]}`, "admintok")
	again, _ := s.Clients(ctx)
	if again[0].SubToken != list[0].SubToken || again[0].UUID != list[0].UUID {
		t.Fatal("credentials changed on update")
	}
}

func TestLoginSessionAndPreset(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.SetAdmin(ctx, "owner", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "owner", "wrong password!!"); err == nil {
		t.Fatal("wrong password accepted")
	}
	h := httptest.NewServer(AdminHandler(s, ""))
	defer h.Close()
	resp, err := http.Post(h.URL+"/api/v2/auth/login", "application/json", strings.NewReader(`{"Username":"owner","Password":"correct horse battery"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login %v %v", resp.Status, err)
	}
	cookie := resp.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie must be HttpOnly and SameSite=Strict")
	}
	if _, err := s.CreateNode(ctx, "uk-1", "UK"); err != nil {
		t.Fatal(err)
	}
	body := `{"tag":"uk-grpc","node_id":"uk-1","preset":"vless-reality-grpc","port":2083,"sni":"www.speedtest.net","endpoints":[{"name":"Direct","address":"5.101.216.178","port":2083}]}`
	call := func(withHeader bool) int {
		req, _ := http.NewRequest("POST", h.URL+"/api/v2/admin/inbounds/preset", strings.NewReader(body))
		req.AddCookie(cookie)
		if withHeader {
			req.Header.Set("X-Dark-Request", "1")
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if call(false) != 401 {
		t.Fatal("cookie mutation without X-Dark-Request must be refused (CSRF)")
	}
	if code := call(true); code != 200 {
		t.Fatalf("preset create %d", code)
	}
	list, err := s.Inbounds(ctx)
	if err != nil || len(list) != 1 || list[0].Network != "grpc" || list[0].Security != "reality" {
		t.Fatalf("inbounds %+v %v", list, err)
	}
	if strings.Contains(string(list[0].Meta), "privateKey") {
		t.Fatal("private key must not leave the HUB")
	}
	cfg, err := s.RenderNode(ctx, "uk-1")
	if err != nil || !strings.Contains(string(cfg), `"serviceName"`) {
		t.Fatalf("render %v", err)
	}
}

func TestRealityKeysShape(t *testing.T) {
	priv, pub, err := RealityKeys()
	if err != nil || len(priv) != 43 || len(pub) != 43 || priv == pub {
		t.Fatalf("keys %q %q %v", priv, pub, err)
	}
}

func TestRepresentativeCreditAndScope(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s) // owner clients alice, bob on de-vless
	if err := s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.AdjustCredit(ctx, "owner", "ali", 10<<30, 3, "start"); err != nil {
		t.Fatal(err)
	}
	vol := Client{Email: "r1", Enabled: true, VolumeBytes: 4 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali-login", "ali", vol, "volume"); err != nil {
		t.Fatal(err)
	}
	// Raising volume charges only the difference.
	vol.VolumeBytes = 6 << 30
	if err := s.RepPutClient(ctx, "ali-login", "ali", vol, "volume"); err != nil {
		t.Fatal(err)
	}
	// 4 GiB left: 5 GiB more must fail and leave the client unchanged.
	vol.VolumeBytes = 11 << 30
	if err := s.RepPutClient(ctx, "ali-login", "ali", vol, "volume"); !errors.Is(err, ErrCredit) {
		t.Fatalf("over-credit edit: %v", err)
	}
	// Unlimited for 60 days costs 2 units.
	unl := Client{Email: "r2", Enabled: true, DurationSeconds: 60 * 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali-login", "ali", unl, "unlimited"); err != nil {
		t.Fatal(err)
	}
	unl.Email, unl.DurationSeconds = "r3", 61*86400 // 3 units, only 1 left
	if err := s.RepPutClient(ctx, "ali-login", "ali", unl, "unlimited"); !errors.Is(err, ErrCredit) {
		t.Fatalf("over-unit create: %v", err)
	}
	reps, _ := s.Reps(ctx)
	if reps[0].VolumeCredit != 4<<30 || reps[0].UnlimitedUnits != 1 || reps[0].Clients != 2 {
		t.Fatalf("rep balances %+v", reps[0])
	}
	list, _ := s.Clients(ctx)
	for _, c := range list {
		if c.Email == "r1" && c.VolumeBytes != 6<<30 {
			t.Fatal("failed edit must not change the client")
		}
		if c.Email == "r3" {
			t.Fatal("failed create must not leave a client")
		}
	}
	// A rep can't touch the owner's client.
	if err := s.RepPutClient(ctx, "ali-login", "ali", Client{Email: "alice", Enabled: true, VolumeBytes: 1, DurationSeconds: 86400}, "volume"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rep edited owner client: %v", err)
	}
	mine, _ := s.scopedClients(ctx, Principal{Role: "rep", RepID: "ali"})
	if len(mine) != 2 {
		t.Fatalf("rep sees %d clients, want 2", len(mine))
	}
}

func TestChangePasswordKeepsRole(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if err := s.SetRepLogin(ctx, "ali", "first password", "ali"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, "ali", "wrong password", "second password"); err == nil {
		t.Fatal("wrong old password accepted")
	}
	if err := s.ChangePassword(ctx, "ali", "first password", "second password"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.principal(ctx, "ali")
	if p.Role != "rep" || p.RepID != "ali" {
		t.Fatalf("role lost: %+v", p)
	}
}

func TestOneLineInstall(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	dist := t.TempDir()
	for _, f := range []string{"dark-node", "xray"} {
		if err := os.WriteFile(filepath.Join(dist, f), []byte("bin-"+f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ca := filepath.Join(dist, "ca.pem")
	_ = os.WriteFile(ca, []byte("CA"), 0o600)
	tok, err := s.CreateNodeInstall(ctx, "fi-1", "Finland")
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(InstallHandler(s, InstallConfig{DistDir: dist, PublicURL: "https://hub.example:28096", HubURL: "https://hub.example:28443", CAFile: ca}))
	defer h.Close()
	get := func(p string) (int, string) {
		r, err := http.Get(h.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	code, script := get("/install/" + tok)
	if code != 200 || !strings.Contains(script, "DARK_NODE_ID=fi-1") || !strings.Contains(script, "sha256sum -c") {
		t.Fatalf("script %d %q", code, script)
	}
	if code, body := get("/install/" + tok + "/xray"); code != 200 || body != "bin-xray" {
		t.Fatalf("xray %d %q", code, body)
	}
	code, nodeTok := get("/install/" + tok + "/token")
	if code != 200 || s.AuthNode(ctx, "fi-1", nodeTok) != nil {
		t.Fatal("installer token must authenticate the node")
	}
	if code, _ := get("/install/" + tok + "/token"); code != 404 {
		t.Fatal("install link must die after the credential is fetched")
	}
}

func TestBulkGroupsTransferNodeManagement(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	// alice: 1000 B, no expiry; bob: first-use 1 day.
	if err := s.PutClient(ctx, Client{Email: "carol", Enabled: true, VolumeBytes: 1 << 30, ExpiresUnix: now.Unix() - 10, Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGroup(ctx, Group{ID: "vip", Name: "VIP", Members: []string{"bob", "carol", "ghost"}}); err != nil {
		t.Fatal(err)
	}
	gs, _ := s.Groups(ctx)
	if len(gs) != 1 || len(gs[0].Members) != 2 {
		t.Fatalf("groups %+v", gs)
	}
	n, err := s.BulkExtend(ctx, BulkRequest{Group: "vip", AddDays: 30, AddBytes: 1 << 30})
	if err != nil || n != 2 {
		t.Fatalf("bulk %d %v", n, err)
	}
	list, _ := s.Clients(ctx)
	for _, c := range list {
		switch c.Email {
		case "carol":
			if c.ExpiresUnix != now.Unix()+30*86400 || c.VolumeBytes != 2<<30 {
				t.Fatalf("expired client must renew from today: %+v", c.Client)
			}
		case "bob":
			if c.DurationSeconds != 31*86400 || c.VolumeBytes != -1 {
				t.Fatalf("first-use term must grow, unlimited stays: %+v", c.Client)
			}
		}
	}
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if err := s.TransferClient(ctx, "carol", "ali"); err != nil {
		t.Fatal(err)
	}
	if mine, _ := s.scopedClients(ctx, Principal{Role: "rep", RepID: "ali"}); len(mine) != 1 {
		t.Fatal("transferred client must belong to the new rep")
	}
	if err := s.SetClientNote(ctx, "carol", "tg: @carol"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameNode(ctx, "de-1", "Frankfurt"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNode(ctx, "de-1"); err != nil {
		t.Fatal(err)
	}
	ins, _ := s.Inbounds(ctx)
	if len(ins) != 0 {
		t.Fatal("deleting a node removes its inbounds")
	}
	list, _ = s.Clients(ctx)
	if len(list) != 3 {
		t.Fatal("clients survive node deletion")
	}
}

func TestNodeHostHistory(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	for i := 0; i < 3; i++ {
		_, err := s.Sync(ctx, "de-1", wire.SyncRequest{Host: &wire.Host{CPUPercent: float64(10 * (i + 1)), RAMUsed: 1, RAMTotal: 4, RXRate: 100, OnlineUsers: i}})
		if err != nil {
			t.Fatal(err)
		}
		*now = now.Add(5 * time.Minute)
	}
	pts, err := s.NodeHistory(ctx, "de-1", 1)
	if err != nil || len(pts) != 3 || pts[2].CPU != 30 || pts[0].RAM != 25 {
		t.Fatalf("history %+v %v", pts, err)
	}
	nodes, _ := s.Nodes(ctx)
	if nodes[0].Host == nil || nodes[0].Host.CPUPercent != 30 {
		t.Fatalf("latest host %+v", nodes[0].Host)
	}
}

func TestBackupAndRestore(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	dir := t.TempDir()
	path, err := s.AutoBackup(ctx, dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "restored.sqlite")
	if err := RestoreBackup(path, target); err != nil {
		t.Fatal(err)
	}
	r, err := Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	list, _ := r.Clients(ctx)
	nodes, _ := r.Nodes(ctx)
	if len(list) != 2 || len(nodes) != 1 {
		t.Fatalf("restored %d clients %d nodes", len(list), len(nodes))
	}
	if err := RestoreBackup(filepath.Join(dir, "missing.gz"), target); err == nil {
		t.Fatal("missing backup must fail")
	}
}

func TestRepPolicyRefundLedgerFinance(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.AdjustCredit(ctx, "owner", "ali", 20<<30, 4, "start"); err != nil {
		t.Fatal(err)
	}
	pol := RepPolicy{MaxClients: 2, MaxDays: 60, MaxClientGB: 8, AllowUnlimited: false, CanDelete: true, RefundUnused: true,
		Inbounds: []string{"de-vless"}, PriceGB: 1000, PriceUnit: 50000}
	if err := s.SetRepPolicy(ctx, "ali", pol); err != nil {
		t.Fatal(err)
	}
	base := Client{Email: "p1", Enabled: true, VolumeBytes: 5 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}
	cases := []struct {
		name string
		edit func(c *Client) string
	}{
		{"unlimited", func(c *Client) string { return "unlimited" }},
		{"too long", func(c *Client) string { c.DurationSeconds = 61 * 86400; return "volume" }},
		{"too big", func(c *Client) string { c.VolumeBytes = 9 << 30; return "volume" }},
		{"inbound", func(c *Client) string { c.Inbounds = []string{"other"}; return "volume" }},
	}
	for _, tc := range cases {
		c := base
		plan := tc.edit(&c)
		if err := s.RepPutClient(ctx, "ali", "ali", c, plan); !errors.Is(err, ErrPolicy) {
			t.Fatalf("%s: want policy error, got %v", tc.name, err)
		}
	}
	for _, e := range []string{"p1", "p2"} {
		c := base
		c.Email = e
		if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
			t.Fatal(err)
		}
	}
	c := base
	c.Email = "p3"
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("client cap: %v", err)
	}
	// p1 used 2 GiB of 5: deleting it returns 3 GiB.
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('p1',?,?,1,1)`, int64(1<<30), int64(1<<30)); err != nil {
		t.Fatal(err)
	}
	b, u, err := s.RepDeleteClient(ctx, "ali", "ali", "p1")
	if err != nil || b != 3<<30 || u != 0 {
		t.Fatalf("refund %d %d %v", b, u, err)
	}
	if _, _, err := s.RepDeleteClient(ctx, "ali", "ali", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting the owner's client: %v", err)
	}
	reps, _ := s.Reps(ctx)
	if reps[0].VolumeCredit != (20-10+3)<<30 || reps[0].Policy.MaxClients != 2 {
		t.Fatalf("balance %+v", reps[0])
	}
	if err := s.RecordPayment(ctx, "owner", "ali", 4000, "cash"); err != nil {
		t.Fatal(err)
	}
	led, err := s.Ledger(ctx, "ali", 0, 10)
	if err != nil || len(led) != 6 || led[0].Kind != "payment" || led[1].Kind != "delete" || led[2].Kind != "refund" {
		t.Fatalf("ledger %v %+v", err, led)
	}
	fin, err := s.Finance(ctx, 0)
	if err != nil || len(fin) != 1 {
		t.Fatal(err)
	}
	f := fin[0]
	// sold 10 GiB, refunded 3: 7 GB x 1000 = 7000, paid 4000.
	if f.SoldBytes != 10<<30 || f.RefundBytes != 3<<30 || f.Revenue != 7000 || f.Debt != 3000 || f.Clients != 1 {
		t.Fatalf("finance %+v", f)
	}
	// Turning deletes off is enforced.
	pol.CanDelete = false
	_ = s.SetRepPolicy(ctx, "ali", pol)
	if _, _, err := s.RepDeleteClient(ctx, "ali", "ali", "p2"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("delete disabled: %v", err)
	}
}

func TestClientDailyUsageFillsGaps(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	day := now.UTC().Format("2006-01-02")
	if _, err := s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('alice','de',?,10,20),('alice','fi',?,1,2)`, day, day); err != nil {
		t.Fatal(err)
	}
	got, err := s.ClientDailyUsage(ctx, "alice", 7)
	if err != nil || len(got) != 7 || got[6].Day != day || got[6].Up != 11 || got[6].Down != 22 || got[0].Up != 0 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestInboundTrafficToggleAndRaw(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	ack, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1,
		Traffic:  []wire.UserTraffic{{Email: "alice", Up: 1, Down: 2}},
		Inbounds: []wire.UserTraffic{{Email: "de-vless", Up: 100, Down: 200}, {Email: "api", Up: 5}, {Email: "bad tag!", Up: 1}}})
	if err != nil || ack.Seq != 1 {
		t.Fatal(ack, err)
	}
	list, _ := s.Inbounds(ctx)
	if len(list) != 1 || list[0].Today != 300 || list[0].Total != 300 {
		t.Fatalf("%+v", list)
	}
	raw, err := s.InboundRaw(ctx, "de-vless")
	if err != nil || raw.NodeID != "de-1" || len(raw.Template) == 0 {
		t.Fatal(raw, err)
	}
	if err := s.SetInboundEnabled(ctx, "de-vless", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.RenderNode(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), `"de-vless"`) {
		t.Fatal("a disabled inbound must leave the node config")
	}
	if err := s.SetInboundEnabled(ctx, "nope", true); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPresetsRenderForEveryProtocol(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "n1", "N"); err != nil {
		t.Fatal(err)
	}
	for i, p := range Presets {
		req := PresetRequest{Tag: "in-" + strconv.Itoa(i), NodeID: "n1", Preset: p, Port: 20000 + i, SNI: "www.speedtest.net",
			Endpoints: []sub.Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 20000 + i}}}
		if err := s.CreatePreset(ctx, req); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	tags := []string{}
	for i := range Presets {
		tags = append(tags, "in-"+strconv.Itoa(i))
	}
	if err := s.PutClient(ctx, Client{Email: "all", Enabled: true, VolumeBytes: -1, Inbounds: tags}); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.RenderNode(ctx, "n1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []map[string]any `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil || len(parsed.Inbounds) != len(Presets)+1 {
		t.Fatalf("%v %d", err, len(parsed.Inbounds))
	}
	for _, in := range parsed.Inbounds[1:] {
		if len(in.Settings.Clients) != 1 || in.Settings.Clients[0]["email"] != "all" {
			t.Fatalf("%s clients %v", in.Protocol, in.Settings.Clients)
		}
		if in.Protocol == "shadowsocks" {
			pw, _ := in.Settings.Clients[0]["password"].(string)
			if raw, _ := base64.StdEncoding.DecodeString(pw); len(raw) != 16 {
				t.Fatalf("ss client key %q", pw)
			}
		}
	}
	links, _, err := s.Subscription(ctx, "all")
	if err != nil || len(links) != len(Presets) {
		t.Fatalf("%v %d links: %v", err, len(links), links)
	}
	// A broken shadowsocks template is refused.
	if err := s.PutInbound(ctx, "bad-ss", "n1", []byte(`{"protocol":"shadowsocks","port":1,"settings":{"method":"aes-256-gcm","password":"x"}}`), true); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestDarkSwapRelay(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if _, err := s.CreateNode(ctx, "ir-relay", "Relay"); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"endpoints":[{"name":"Direct","address":"9.9.9.9","port":18580}]}`)
	if err := s.PutInboundMeta(ctx, "de-vless", "de-1", []byte(vlessTemplate), meta, true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInbound(ctx, "relay-own", "ir-relay", []byte(strings.Replace(vlessTemplate, "18580", "6900", 1)), true); err != nil {
		t.Fatal(err)
	}
	bad := []Swap{
		{ID: "s1", InboundTag: "de-vless", SourceNode: "de-1", SourcePort: 6801, EntryAddress: "ir.example", EntryPort: 6801}, // same node
		{ID: "s1", InboundTag: "de-vless", SourceNode: "ir-relay", SourcePort: 6900, EntryAddress: "ir.example", EntryPort: 6801}, // port taken
		{ID: "s1", InboundTag: "nope", SourceNode: "ir-relay", SourcePort: 6801, EntryAddress: "ir.example", EntryPort: 6801},
	}
	for i, w := range bad {
		if err := s.PutSwap(ctx, w); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad %d: %v", i, err)
		}
	}
	ok := Swap{ID: "s1", InboundTag: "de-vless", SourceNode: "ir-relay", SourcePort: 6801, EntryAddress: "ir.example", EntryPort: 443, Enabled: true}
	if err := s.PutSwap(ctx, ok); err != nil {
		t.Fatal(err)
	}
	dup := ok
	dup.ID = "s2"
	if err := s.PutSwap(ctx, dup); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate relay port: %v", err)
	}
	cfg, err := s.RenderNode(ctx, "ir-relay")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `"tag":"swap-s1"`) || !strings.Contains(string(cfg), `"address":"9.9.9.9"`) {
		t.Fatalf("relay config: %s", cfg)
	}
	links, _, err := s.Subscription(ctx, "alice")
	if err != nil || len(links) != 2 || !strings.Contains(strings.Join(links, " "), "@ir.example:443") {
		t.Fatalf("%v %v", err, links)
	}
	list, _ := s.Swaps(ctx, "de-vless")
	if len(list) != 1 || list[0].Target != "9.9.9.9:18580" {
		t.Fatalf("%+v", list)
	}
	ok.Enabled = false
	_ = s.PutSwap(ctx, ok)
	cfg, _ = s.RenderNode(ctx, "ir-relay")
	links, _, _ = s.Subscription(ctx, "alice")
	if strings.Contains(string(cfg), "swap-s1") || len(links) != 1 {
		t.Fatal("a paused swap must leave config and subscription")
	}
	if err := s.DeleteSwap(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
}

func TestInsights(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	day := now.UTC().Format("2006-01-02")
	_, _ = s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('alice','de-1',?,1,9),('bob','de-1',?,50,50)`, day, day)
	_, _ = s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(1,'de-1','bob','blocked:volume')`)
	in, err := s.Insights(ctx, 7)
	if err != nil || len(in.Daily) != 7 || in.Daily[6].Up != 51 || len(in.Top) != 2 || in.Top[0].Email != "bob" || len(in.Events) != 1 {
		t.Fatalf("%v %+v", err, in)
	}
}
