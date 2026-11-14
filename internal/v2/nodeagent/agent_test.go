package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
	xstats "github.com/darktunnelmika/dark-xray-pro/internal/xraystats"
)

const testConfig = `{"api":{"tag":"api","services":["StatsService"]},"stats":{},
"inbounds":[{"tag":"vless-1","protocol":"vless","settings":{"clients":[
 {"id":"11111111-1111-1111-1111-111111111111","email":"alice"},
 {"id":"22222222-2222-2222-2222-222222222222","email":"bob"},
 {"id":"33333333-3333-3333-3333-333333333333"}]}}]}`

type fakeStats struct {
	mu      sync.Mutex
	traffic []wire.UserTraffic
	online  map[string][]string
	fail    bool
}

func (f *fakeStats) TakeTraffic(context.Context) ([]wire.UserTraffic, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("xray down")
	}
	t := f.traffic
	f.traffic = nil
	return t, nil
}

func (f *fakeStats) OnlineIPs(context.Context) (map[string][]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.online, nil
}

type fakeXray struct {
	mu      sync.Mutex
	applied [][]byte
	reject  bool
}

func (f *fakeXray) Apply(_ context.Context, cfg []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reject {
		return false, errors.New("rejected")
	}
	f.applied = append(f.applied, cfg)
	return true, nil
}

func (f *fakeXray) Status() (bool, uint64, string) { return true, 1, "" }

func (f *fakeXray) Install(context.Context, []byte) error { return nil }

type fakeUsers struct {
	mu  sync.Mutex
	ops []string
}

func (f *fakeUsers) Supports(p string) bool { return p == "vless" }
func (f *fakeUsers) AddUser(_ context.Context, u ConfigUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "+"+u.Tag+"/"+u.Email)
	return nil
}
func (f *fakeUsers) RemoveUser(_ context.Context, u ConfigUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, "-"+u.Tag+"/"+u.Email)
	return nil
}

func (f *fakeXray) last(t *testing.T) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applied) == 0 {
		t.Fatal("nothing applied")
	}
	emails, err := ConfigEmails(f.applied[len(f.applied)-1])
	if err != nil {
		t.Fatal(err)
	}
	return emails
}

type fakeHub struct {
	mu       sync.Mutex
	down     bool
	resp     wire.SyncResponse
	received []wire.UsageBatch
	acked    uint64
}

func (h *fakeHub) Sync(_ context.Context, _ wire.SyncRequest) (wire.SyncResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.down {
		return wire.SyncResponse{}, errors.New("hub down")
	}
	r := h.resp
	r.UsageSeq = h.acked
	return r, nil
}

func (h *fakeHub) DeliverUsage(_ context.Context, b wire.UsageBatch) (wire.UsageAck, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.down {
		return wire.UsageAck{}, errors.New("hub down")
	}
	if b.Seq > h.acked {
		h.received = append(h.received, b)
		h.acked = b.Seq
	}
	return wire.UsageAck{Seq: h.acked}, nil
}

func (h *fakeHub) total(email string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var n int64
	for _, b := range h.received {
		for _, t := range b.Traffic {
			if t.Email == email {
				n += t.Up + t.Down
			}
		}
	}
	return n
}

func newTestAgent(t *testing.T, dir string, hub Hub, x Applier, s StatsSource, now *time.Time) *Agent {
	t.Helper()
	a, err := New(Config{NodeID: "n1", StateDir: dir, Now: func() time.Time { return *now }}, hub, x, s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func limits(alice, bob wire.UserLimit) []wire.UserLimit {
	alice.Email, bob.Email = "alice", "bob"
	return []wire.UserLimit{alice, bob}
}

func TestHubOutageKeepsServingAndDeliversLater(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: -1}, wire.UserLimit{Enabled: true, RemainingBytes: -1})}}
	x := &fakeXray{}
	st := &fakeStats{}
	a := newTestAgent(t, dir, hub, x, st, &now)
	a.Exchange(ctx)
	a.enforce(ctx)
	if got := x.last(t); len(got) != 2 {
		t.Fatalf("served %v, want alice and bob (unnamed account dropped)", got)
	}

	hub.mu.Lock()
	hub.down = true
	hub.mu.Unlock()
	applies := len(x.applied)
	for i := 0; i < 5; i++ {
		st.mu.Lock()
		st.traffic = []wire.UserTraffic{{Email: "alice", Up: 100, Down: 900}}
		st.mu.Unlock()
		now = now.Add(10 * time.Second)
		a.Collect(ctx)
		a.Exchange(ctx)
		a.enforce(ctx)
	}
	if len(x.applied) != applies {
		t.Fatal("hub outage must not restart or change Xray")
	}
	if a.Spool().Len() == 0 {
		t.Fatal("usage must be spooled during the outage")
	}

	hub.mu.Lock()
	hub.down = false
	hub.mu.Unlock()
	a.Exchange(ctx)
	if got := hub.total("alice"); got != 5000 {
		t.Fatalf("hub received %d bytes for alice, want 5000", got)
	}
	if a.Spool().Len() != 0 {
		t.Fatal("acknowledged batches must leave the spool")
	}
}

func TestRebootWithoutHubServesLastState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 3, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: -1}, wire.UserLimit{Enabled: false, RemainingBytes: -1})}}
	a := newTestAgent(t, dir, hub, &fakeXray{}, &fakeStats{}, &now)
	a.Exchange(ctx)

	x2 := &fakeXray{}
	down := &fakeHub{down: true}
	b := newTestAgent(t, dir, down, x2, &fakeStats{}, &now)
	b.enforce(ctx)
	if got := x2.last(t); len(got) != 1 || got[0] != "alice" {
		t.Fatalf("after reboot served %v, want only alice", got)
	}
}

func TestLocalVolumeEnforcementDuringOutage(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: 1500}, wire.UserLimit{Enabled: true, RemainingBytes: -1})}}
	x := &fakeXray{}
	st := &fakeStats{}
	a := newTestAgent(t, t.TempDir(), hub, x, st, &now)
	a.Exchange(ctx)
	hub.mu.Lock()
	hub.down = true
	hub.mu.Unlock()
	st.traffic = []wire.UserTraffic{{Email: "alice", Up: 1000, Down: 600}}
	a.Collect(ctx)
	a.enforce(ctx)
	if got := x.last(t); len(got) != 1 || got[0] != "bob" {
		t.Fatalf("served %v, want alice blocked for volume", got)
	}
	if a.Decision()["alice"] != ReasonVolume {
		t.Fatalf("decision %v", a.Decision())
	}
}

func TestDeliveredUsageNotDoubleCounted(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: 1000}, wire.UserLimit{Enabled: true, RemainingBytes: -1})}}
	st := &fakeStats{}
	a := newTestAgent(t, t.TempDir(), hub, &fakeXray{}, st, &now)
	a.Exchange(ctx)
	st.traffic = []wire.UserTraffic{{Email: "alice", Up: 400}}
	a.Collect(ctx)
	// The HUB now reports remaining already reduced by the delivered batch.
	hub.mu.Lock()
	hub.resp.Users[0].RemainingBytes = 600
	hub.mu.Unlock()
	a.Exchange(ctx)
	if used := a.localUsed()["alice"]; used != 0 {
		t.Fatalf("local used %d after HUB included the batch, want 0", used)
	}
}

func TestIPLimitCooldown(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: -1, MaxIPs: 1}, wire.UserLimit{Enabled: true, RemainingBytes: -1})}}
	st := &fakeStats{online: map[string][]string{"alice": {"1.1.1.1", "2.2.2.2"}}}
	x := &fakeXray{}
	a := newTestAgent(t, t.TempDir(), hub, x, st, &now)
	a.Exchange(ctx)
	a.enforce(ctx)
	if a.Decision()["alice"] != ReasonIPLimit {
		t.Fatal("alice must be blocked for the IP limit")
	}
	st.mu.Lock()
	st.online = nil
	st.mu.Unlock()
	now = now.Add(time.Minute)
	a.enforce(ctx)
	if a.Decision()["alice"] != ReasonIPLimit {
		t.Fatal("block must hold for the cooldown")
	}
	now = now.Add(2 * time.Minute)
	a.enforce(ctx)
	if _, blocked := a.Decision()["alice"]; blocked {
		t.Fatal("block must lift after the cooldown")
	}
}

func TestRejectedConfigKeepsLastGood(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: -1}, wire.UserLimit{Enabled: true, RemainingBytes: -1})}}
	x := &fakeXray{}
	a := newTestAgent(t, t.TempDir(), hub, x, &fakeStats{}, &now)
	a.Exchange(ctx)
	a.enforce(ctx)
	n := len(x.applied)
	x.reject = true
	hub.mu.Lock()
	hub.resp.ConfigRevision = 2
	hub.resp.Config = json.RawMessage(`{"inbounds":[{"protocol":"vless","settings":{"clients":[{"email":"alice","id":"x"}]}}],"broken":true}`)
	hub.mu.Unlock()
	a.Exchange(ctx)
	a.enforce(ctx)
	if len(x.applied) != n {
		t.Fatal("a rejected config must not replace the running one")
	}
}

func TestSpoolMergeKeepsAllBytes(t *testing.T) {
	sp, err := OpenSpool(t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := sp.Append(wire.UsageBatch{Traffic: []wire.UserTraffic{{Email: "a", Up: 1, Down: 2}}}); err != nil {
			t.Fatal(err)
		}
	}
	if sp.Len() > 3 {
		t.Fatalf("spool holds %d batches, max 3", sp.Len())
	}
	used, err := sp.UnackedTraffic()
	if err != nil || used["a"] != 30 {
		t.Fatalf("unacked %v %v, want 30", used, err)
	}
	pending, _ := sp.Pending()
	last := pending[len(pending)-1].Seq
	if err := sp.Ack(last); err != nil || sp.Len() != 0 {
		t.Fatal("ack must clear the spool")
	}
	sp2, err := OpenSpool(sp.dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	seq, _ := sp2.Append(wire.UsageBatch{})
	if seq <= last {
		t.Fatalf("sequence went back to %d after %d", seq, last)
	}
}

func TestSumUserStats(t *testing.T) {
	got := SumUserStats([]*xstats.Stat{
		{Name: "user>>>alice>>>traffic>>>uplink", Value: 5},
		{Name: "user>>>alice>>>traffic>>>downlink", Value: 7},
		{Name: "inbound>>>vless-1>>>traffic>>>uplink", Value: 99},
		{Name: "user>>>bob>>>traffic>>>downlink", Value: 0},
	})
	if len(got) != 1 || got[0].Email != "alice" || got[0].Up != 5 || got[0].Down != 7 {
		t.Fatalf("got %+v", got)
	}
}

func TestEvaluateUnknownAndExpiry(t *testing.T) {
	d := Evaluate(100, []wire.UserLimit{
		{Email: "a", Enabled: true, ExpiresUnix: 100, RemainingBytes: -1},
		{Email: "b", Enabled: true, ExpiresUnix: 0, RemainingBytes: -1},
	}, nil, nil)
	if d["a"] != ReasonExpired {
		t.Fatal("expiry at now must block")
	}
	if _, ok := d["b"]; ok {
		t.Fatal("no expiry yet (first-connection plan) must serve")
	}
}

func TestUserChangesDoNotRestartXray(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Users: limits(wire.UserLimit{Enabled: true, RemainingBytes: -1, MaxIPs: 1}, wire.UserLimit{Enabled: true, RemainingBytes: -1})}}
	st := &fakeStats{}
	x := &fakeXray{}
	u := &fakeUsers{}
	a := newTestAgent(t, t.TempDir(), hub, x, st, &now)
	a.SetUserAPI(u)
	a.Exchange(ctx)
	a.enforce(ctx)
	if len(x.applied) != 1 {
		t.Fatalf("first apply must start Xray once, got %d", len(x.applied))
	}
	// IP limit: alice must be removed live.
	st.mu.Lock()
	st.online = map[string][]string{"alice": {"1.1.1.1", "2.2.2.2"}}
	st.mu.Unlock()
	a.enforce(ctx)
	// HUB adds carol to the same inbound: only users change.
	hub.mu.Lock()
	hub.resp.ConfigRevision = 2
	hub.resp.Config = json.RawMessage(strings.Replace(testConfig, `{"id":"33333333`, `{"id":"44444444-4444-4444-4444-444444444444","email":"carol"},{"id":"33333333`, 1))
	hub.resp.Users = append(hub.resp.Users, wire.UserLimit{Email: "carol", Enabled: true, RemainingBytes: -1})
	hub.mu.Unlock()
	a.Exchange(ctx)
	a.enforce(ctx)
	if len(x.applied) != 1 {
		t.Fatalf("user changes restarted Xray %d times", len(x.applied)-1)
	}
	want := []string{"-vless-1/alice", "+vless-1/carol"}
	if strings.Join(u.ops, ",") != strings.Join(want, ",") {
		t.Fatalf("live ops %v, want %v", u.ops, want)
	}
	// A new inbound changes the structure: a restart is correct.
	hub.mu.Lock()
	hub.resp.ConfigRevision = 3
	hub.resp.Config = json.RawMessage(strings.Replace(testConfig, `"tag":"vless-1"`, `"tag":"vless-2"`, 1))
	hub.mu.Unlock()
	a.Exchange(ctx)
	a.enforce(ctx)
	if len(x.applied) != 2 {
		t.Fatal("a structural change must apply the new config")
	}
}

func TestDiffUsersAccountChange(t *testing.T) {
	from := InboundUsers{"t": {"a": {Tag: "t", Email: "a", ID: "1", Protocol: "vless"}}}
	to := InboundUsers{"t": {"a": {Tag: "t", Email: "a", ID: "2", Protocol: "vless"}}}
	ops := DiffUsers(from, to)
	if len(ops) != 2 || ops[0].Add || !ops[1].Add || ops[1].User.ID != "2" {
		t.Fatalf("ops %+v", ops)
	}
}
