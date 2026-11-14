package hubcore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
