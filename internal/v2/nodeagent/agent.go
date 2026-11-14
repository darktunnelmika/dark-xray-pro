package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/hostmetrics"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Version is reported to the HUB.
var Version = "v2.0.2"

// Applier is the part of Supervisor the agent uses.
type Applier interface {
	Apply(ctx context.Context, cfg []byte) (bool, error)
	Install(ctx context.Context, cfg []byte) error
	Status() (running bool, restarts uint64, lastErr string)
}

// Config configures one Node agent.
type Config struct {
	NodeID          string
	StateDir        string
	CollectInterval time.Duration // default 10s
	IPLimitCooldown time.Duration // default 2m
	MaxSpool        int           // default 8640 (one day at 10s)
	Logf            func(string, ...any)
	Now             func() time.Time
	AgentSHA        string // sha256 of the running binary, reported to the HUB
}

// Agent ties Xray, the usage spool, local enforcement and the HUB together.
// None of its data-plane duties wait for the HUB.
type Agent struct {
	cfg   Config
	hub   Hub
	xray  Applier
	stats StatsSource
	users UserAPI
	spool *Spool

	mu           sync.Mutex
	state        wire.SyncResponse // last desired state from the HUB
	delivered    map[uint64]map[string]int64
	blockedUntil map[string]time.Time
	lastDecision Decision
	appliedKey   string
	appliedSkel  string
	appliedUsers InboundUsers
	liveChanges  uint64
	restarts     uint64
	pendingDisc  []wire.Disconnect
	lastOnline   time.Time
	lastFrom     int64
	lastHost     *hostSample
	onlineCount  int
	tasks        *taskBox
}

type hostSample struct {
	raw hostmetrics.Raw
	at  time.Time
}

// hostStats samples machine health; rates need two samples.
func (a *Agent) hostStats() *wire.Host {
	raw, err := hostmetrics.Read()
	if err != nil {
		return nil
	}
	now := time.Now()
	h := &wire.Host{RAMUsed: raw.RAMUsed, RAMTotal: raw.RAMTotal}
	if raw.DiskTotal != nil && raw.DiskUsed != nil {
		h.DiskTotal, h.DiskUsed = *raw.DiskTotal, *raw.DiskUsed
	}
	if raw.Load1 != nil {
		h.Load1 = *raw.Load1
	}
	if raw.UptimeSec != nil {
		h.UptimeSec = *raw.UptimeSec
	}
	if raw.TCPInUse != nil {
		h.TCP = *raw.TCPInUse
	}
	a.mu.Lock()
	prev := a.lastHost
	a.lastHost = &hostSample{raw: raw, at: now}
	h.OnlineUsers = a.onlineCount
	a.mu.Unlock()
	if prev != nil {
		if cpu, ok := hostmetrics.CPUPercent(prev.raw, raw); ok {
			h.CPUPercent = cpu
		}
		if dt := now.Sub(prev.at).Seconds(); dt > 0 && raw.Interface == prev.raw.Interface {
			if raw.RXBytes >= prev.raw.RXBytes {
				h.RXRate = float64(raw.RXBytes-prev.raw.RXBytes) / dt
			}
			if raw.TXBytes >= prev.raw.TXBytes {
				h.TXRate = float64(raw.TXBytes-prev.raw.TXBytes) / dt
			}
		}
	}
	return h
}

func New(cfg Config, hub Hub, xray Applier, stats StatsSource) (*Agent, error) {
	if cfg.NodeID == "" || cfg.StateDir == "" {
		return nil, errors.New("node id and state dir are required")
	}
	if cfg.CollectInterval <= 0 {
		cfg.CollectInterval = 5 * time.Second
	}
	if cfg.IPLimitCooldown <= 0 {
		cfg.IPLimitCooldown = 2 * time.Minute
	}
	if cfg.MaxSpool <= 0 {
		cfg.MaxSpool = 8640
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, err
	}
	sp, err := OpenSpool(filepath.Join(cfg.StateDir, "spool"), cfg.MaxSpool)
	if err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg, hub: hub, xray: xray, stats: stats, spool: sp,
		delivered: map[uint64]map[string]int64{}, blockedUntil: map[string]time.Time{}, lastDecision: Decision{}, tasks: newTaskBox(filepath.Join(cfg.StateDir, "tasks.json"))}
	if b, err := os.ReadFile(a.statePath()); err == nil {
		_ = json.Unmarshal(b, &a.state)
	}
	a.lastFrom = cfg.Now().Unix()
	return a, nil
}

func (a *Agent) statePath() string { return filepath.Join(a.cfg.StateDir, "desired.json") }

// Spool exposes the usage spool (for status pages and tests).
func (a *Agent) Spool() *Spool { return a.spool }

// Run loops until ctx ends. A HUB outage only delays delivery and config
// changes; collection and enforcement continue on their own timer.
func (a *Agent) Run(ctx context.Context) {
	// Serve the last known desired state immediately after a reboot.
	a.enforce(ctx)
	collect := time.NewTicker(a.cfg.CollectInterval)
	defer collect.Stop()
	nextSync := time.Now()
	for {
		a.Collect(ctx)
		if !time.Now().Before(nextSync) {
			poll := a.Exchange(ctx)
			nextSync = time.Now().Add(poll)
		}
		a.enforce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-collect.C:
		}
	}
}

// Collect reads Xray counters and spools them durably.
func (a *Agent) Collect(ctx context.Context) {
	now := a.cfg.Now()
	traffic, err := a.stats.TakeTraffic(ctx)
	if err != nil {
		a.cfg.Logf("stats unavailable: %v", err)
		return
	}
	online, _ := a.stats.OnlineIPs(ctx)
	var inbounds []wire.UserTraffic
	if src, ok := unwrapStats(a.stats).(interface{ TakeInbound() []wire.UserTraffic }); ok {
		inbounds = src.TakeInbound()
	}
	a.mu.Lock()
	a.onlineCount = len(online)
	disc := a.pendingDisc
	a.pendingDisc = nil
	sendOnline := len(online) > 0 && now.Sub(a.lastOnline) >= time.Minute
	a.mu.Unlock()
	if len(traffic) == 0 && len(disc) == 0 && len(inbounds) == 0 && !sendOnline {
		return
	}
	b := wire.UsageBatch{NodeID: a.cfg.NodeID, FromUnix: a.lastFrom, ToUnix: now.Unix(), Traffic: traffic, Disconnects: disc, Inbounds: inbounds}
	if sendOnline || len(traffic) > 0 {
		b.Online = onlineList(online)
	}
	if _, err := a.spool.Append(b); err != nil {
		// Counters were already reset in Xray; keep them in memory so the
		// next successful append still carries these bytes.
		a.cfg.Logf("spool append failed: %v", err)
		a.mu.Lock()
		a.pendingDisc = append(disc, a.pendingDisc...)
		a.mu.Unlock()
		a.carry(traffic)
		return
	}
	a.mu.Lock()
	a.lastFrom = now.Unix()
	if sendOnline || len(traffic) > 0 {
		a.lastOnline = now
	}
	a.mu.Unlock()
}

func unwrapStats(s StatsSource) StatsSource {
	if c, ok := s.(*carryStats); ok {
		return c.inner
	}
	return s
}

// carry re-queues traffic after a failed spool write by wrapping the source.
func (a *Agent) carry(t []wire.UserTraffic) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.stats.(*carryStats); ok {
		c.add(t)
		return
	}
	cs := &carryStats{inner: a.stats}
	cs.add(t)
	a.stats = cs
}

type carryStats struct {
	inner StatsSource
	mu    sync.Mutex
	held  []wire.UserTraffic
}

func (c *carryStats) add(t []wire.UserTraffic) {
	c.mu.Lock()
	c.held = append(c.held, t...)
	c.mu.Unlock()
}

func (c *carryStats) TakeTraffic(ctx context.Context) ([]wire.UserTraffic, error) {
	t, err := c.inner.TakeTraffic(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	held := c.held
	c.held = nil
	c.mu.Unlock()
	if len(held) == 0 {
		return t, nil
	}
	return MergeBatches(wire.UsageBatch{Traffic: held}, wire.UsageBatch{Traffic: t}).Traffic, nil
}

func (c *carryStats) OnlineIPs(ctx context.Context) (map[string][]string, error) {
	return c.inner.OnlineIPs(ctx)
}

func onlineList(m map[string][]string) []wire.UserOnline {
	out := make([]wire.UserOnline, 0, len(m))
	for e, ips := range m {
		out = append(out, wire.UserOnline{Email: e, IPs: ips})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out
}

// Exchange delivers spooled usage, then syncs desired state. It returns the
// delay before the next exchange.
func (a *Agent) Exchange(ctx context.Context) time.Duration {
	poll := 10 * time.Second
	pending, err := a.spool.Pending()
	if err != nil {
		a.cfg.Logf("spool read failed: %v", err)
	}
	for _, b := range pending {
		ack, err := a.hub.DeliverUsage(ctx, b)
		if err != nil {
			a.cfg.Logf("usage delivery deferred: %v", err)
			break
		}
		if ack.Seq < b.Seq {
			a.cfg.Logf("hub acknowledged %d, expected %d", ack.Seq, b.Seq)
			break
		}
		a.mu.Lock()
		a.delivered[b.Seq] = trafficMap(b.Traffic)
		a.mu.Unlock()
		if err := a.spool.Ack(b.Seq); err != nil {
			a.cfg.Logf("spool ack failed: %v", err)
		}
	}
	running, restarts, lastErr := a.xray.Status()
	a.mu.Lock()
	rev := a.state.ConfigRevision
	a.mu.Unlock()
	results := a.tasks.pending()
	resp, err := a.hub.Sync(ctx, wire.SyncRequest{NodeID: a.cfg.NodeID, AgentVersion: Version, ConfigRevision: rev,
		XrayRunning: running, XrayRestarts: restarts, LastError: lastErr, SpooledBatches: a.spool.Len(), UnixTime: a.cfg.Now().Unix(),
		Host: a.hostStats(), Results: results, AgentSHA: a.cfg.AgentSHA})
	if err != nil {
		a.cfg.Logf("hub sync unavailable, serving last state: %v", err)
		return poll
	}
	a.tasks.delivered(results)
	a.tasks.start(ctx, resp.Tasks, a.cfg.Logf)
	if resp.PollSeconds > 0 {
		poll = time.Duration(resp.PollSeconds) * time.Second
	}
	a.mu.Lock()
	if len(resp.Config) == 0 && resp.ConfigRevision == a.state.ConfigRevision {
		resp.Config = a.state.Config
	}
	if len(resp.Config) == 0 {
		a.mu.Unlock()
		a.cfg.Logf("hub sent revision %d without config; keeping %d", resp.ConfigRevision, rev)
		return poll
	}
	a.state = resp
	for seq := range a.delivered {
		if seq <= resp.UsageSeq {
			delete(a.delivered, seq)
		}
	}
	data, _ := json.Marshal(a.state)
	a.mu.Unlock()
	if err := writeFileAtomic(a.statePath(), data); err != nil {
		a.cfg.Logf("desired state not persisted: %v", err)
	}
	return poll
}

func trafficMap(list []wire.UserTraffic) map[string]int64 {
	m := map[string]int64{}
	for _, t := range list {
		m[t.Email] += t.Up + t.Down
	}
	return m
}

// localUsed: bytes counted here that the HUB's RemainingBytes does not yet
// include (spooled batches and delivered batches newer than UsageSeq).
func (a *Agent) localUsed() map[string]int64 {
	used := map[string]int64{}
	pending, _ := a.spool.Pending()
	a.mu.Lock()
	seq := a.state.UsageSeq
	for s, m := range a.delivered {
		if s > seq {
			for e, n := range m {
				used[e] += n
			}
		}
	}
	a.mu.Unlock()
	for _, b := range pending {
		if b.Seq > seq {
			for _, t := range b.Traffic {
				used[t.Email] += t.Up + t.Down
			}
		}
	}
	return used
}

func (a *Agent) enforce(ctx context.Context) {
	a.mu.Lock()
	state := a.state
	a.mu.Unlock()
	if len(state.Config) == 0 {
		return
	}
	now := a.cfg.Now()
	online, _ := a.stats.OnlineIPs(ctx)
	d := Evaluate(now.Unix(), state.Users, a.localUsed(), online)
	a.mu.Lock()
	for e, until := range a.blockedUntil {
		if now.Before(until) {
			if _, ok := d[e]; !ok {
				d[e] = ReasonIPLimit
			}
		} else {
			delete(a.blockedUntil, e)
		}
	}
	for e, r := range d {
		if r == ReasonIPLimit {
			if _, ok := a.blockedUntil[e]; !ok {
				a.blockedUntil[e] = now.Add(a.cfg.IPLimitCooldown)
			}
		}
		if _, was := a.lastDecision[e]; !was {
			a.pendingDisc = append(a.pendingDisc, wire.Disconnect{Email: e, Reason: r, Unix: now.Unix()})
		}
	}
	a.lastDecision = d
	allowed := d.Allowed(state.Users)
	key := strings.Join(allowed, "\n") + "\x00" + string(state.Config)
	same := key == a.appliedKey
	a.mu.Unlock()
	if same {
		return
	}
	set := make(map[string]bool, len(allowed))
	for _, e := range allowed {
		set[e] = true
	}
	cfg, err := FilterConfig(state.Config, set)
	if err != nil {
		a.cfg.Logf("config %d unusable: %v", state.ConfigRevision, err)
		return
	}
	if a.applyLive(ctx, state.Config, cfg) {
		a.mu.Lock()
		a.appliedKey = key
		a.mu.Unlock()
		return
	}
	if _, err := a.xray.Apply(ctx, cfg); err != nil {
		a.cfg.Logf("config %d not applied, keeping last good: %v", state.ConfigRevision, err)
		return
	}
	skel, _ := Skeleton(state.Config)
	users, _ := ParseUsers(cfg)
	a.mu.Lock()
	a.appliedKey, a.appliedSkel, a.appliedUsers = key, skel, users
	a.restarts++
	a.mu.Unlock()
}

// applyLive changes only users through the Xray API when the inbound
// structure is unchanged. It returns false when a restart is required.
func (a *Agent) applyLive(ctx context.Context, hubCfg, filtered []byte) bool {
	if a.users == nil {
		return false
	}
	skel, err := Skeleton(hubCfg)
	if err != nil {
		return false
	}
	a.mu.Lock()
	sameSkel := skel == a.appliedSkel && a.appliedUsers != nil
	from := a.appliedUsers
	a.mu.Unlock()
	if !sameSkel {
		return false
	}
	to, err := ParseUsers(filtered)
	if err != nil {
		return false
	}
	ops := DiffUsers(from, to)
	for _, op := range ops {
		if !a.users.Supports(op.User.Protocol) {
			return false
		}
	}
	for _, op := range ops {
		var err error
		if op.Add {
			err = a.users.AddUser(ctx, op.User)
		} else {
			err = a.users.RemoveUser(ctx, op.User)
		}
		if err != nil {
			a.cfg.Logf("live user change failed, restarting Xray instead: %v", err)
			return false
		}
	}
	if err := a.xray.Install(ctx, filtered); err != nil {
		a.cfg.Logf("active config not persisted: %v", err)
	}
	a.mu.Lock()
	a.appliedUsers = to
	a.liveChanges += uint64(len(ops))
	a.mu.Unlock()
	return true
}

// SetUserAPI enables live user changes without Xray restarts.
func (a *Agent) SetUserAPI(u UserAPI) { a.users = u }

// Counters reports live user changes and full config applies.
func (a *Agent) Counters() (live, applies uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.liveChanges, a.restarts
}

// Decision returns the current local block list (copy).
func (a *Agent) Decision() Decision {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := Decision{}
	for k, v := range a.lastDecision {
		out[k] = v
	}
	return out
}
