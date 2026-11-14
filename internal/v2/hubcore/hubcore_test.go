package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/nodeagent"
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
