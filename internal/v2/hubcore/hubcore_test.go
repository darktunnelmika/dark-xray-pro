package hubcore

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
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
