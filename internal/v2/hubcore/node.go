package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// PollSeconds tells nodes how often to sync.
const PollSeconds = 5

// Sync records a node's report and returns its desired state. The config
// revision only moves when the rendered config actually changes.
func (s *Store) Sync(ctx context.Context, nodeID string, req wire.SyncRequest) (wire.SyncResponse, error) {
	cfg, err := s.RenderNode(ctx, nodeID)
	if err != nil {
		return wire.SyncResponse{}, err
	}
	digest := sha(cfg)
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wire.SyncResponse{}, err
	}
	defer tx.Rollback()
	var rev, usageSeq int64
	var oldDigest string
	if err := tx.QueryRowContext(ctx, `SELECT config_revision,config_sha256,usage_seq FROM nodes WHERE id=?`, nodeID).
		Scan(&rev, &oldDigest, &usageSeq); err != nil {
		return wire.SyncResponse{}, ErrNotFound
	}
	if digest != oldDigest {
		rev++
	}
	lastErr := req.LastError
	if req.XrayRunning && int64(req.ConfigRevision) == rev {
		// The agent remembers its last failure for good; once Xray runs on the
		// current config that failure is history, not the node's state.
		lastErr = ""
	}
	if len(lastErr) > 500 {
		lastErr = lastErr[:500]
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET last_seen_unix=?, agent_version=?, xray_running=?, xray_restarts=?, last_error=?,
spooled=?, reported_revision=?, config_revision=?, config_sha256=? WHERE id=?`,
		now, req.AgentVersion, req.XrayRunning, req.XrayRestarts, lastErr, req.SpooledBatches, req.ConfigRevision, rev, digest, nodeID); err != nil {
		return wire.SyncResponse{}, err
	}
	if req.Host != nil {
		hj, _ := json.Marshal(req.Host)
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET host_json=? WHERE id=?`, string(hj), nodeID); err != nil {
			return wire.SyncResponse{}, err
		}
		bucket := now - now%300
		ramPct := 0.0
		if req.Host.RAMTotal > 0 {
			ramPct = 100 * float64(req.Host.RAMUsed) / float64(req.Host.RAMTotal)
		}
		// One row per node per 5 minutes; the latest sample in the bucket wins.
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_history(node_id,bucket,cpu,ram_pct,rx_rate,tx_rate,online) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(node_id,bucket) DO UPDATE SET cpu=excluded.cpu, ram_pct=excluded.ram_pct, rx_rate=MAX(rx_rate,excluded.rx_rate),
tx_rate=MAX(tx_rate,excluded.tx_rate), online=MAX(online,excluded.online)`,
			nodeID, bucket, req.Host.CPUPercent, ramPct, req.Host.RXRate, req.Host.TXRate, req.Host.OnlineUsers); err != nil {
			return wire.SyncResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return wire.SyncResponse{}, err
	}
	s.applyTaskResults(ctx, nodeID, req.Results)
	s.recordAssets(ctx, nodeID, req.Assets)
	s.recordXray(ctx, nodeID, req.XrayVersion, req.Arch)
	if req.AgentSHA != "" && len(req.AgentSHA) <= 64 {
		_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET agent_sha=? WHERE id=? AND agent_sha<>?`, req.AgentSHA, nodeID, req.AgentSHA)
	}
	users, err := s.limits(ctx, nodeID)
	if err != nil {
		return wire.SyncResponse{}, err
	}
	resp := wire.SyncResponse{ConfigRevision: uint64(rev), Users: users, UsageSeq: uint64(usageSeq), PollSeconds: PollSeconds, UnixTime: now,
		Tasks: s.openTasks(ctx, nodeID), Assets: s.geoWanted(), TrustedIPs: s.TrustedIPs(ctx)}
	resp.IPOver = s.ipOver(ctx, now, users, resp.TrustedIPs)
	if uint64(rev) != req.ConfigRevision {
		resp.Config = cfg
	}
	return resp, nil
}

// limits reads usage after usage_seq was read above; a batch committed in
// between can only make RemainingBytes smaller, so the Node may at worst
// count those bytes twice for one poll (stricter, never looser).
func (s *Store) limits(ctx context.Context, nodeID string) ([]wire.UserLimit, error) {
	clients, err := s.nodeClients(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]wire.UserLimit, 0, len(clients))
	for _, c := range clients {
		u, err := s.ClientUsage(ctx, c.Email)
		if err != nil {
			return nil, err
		}
		out = append(out, wire.UserLimit{Email: c.Email, Enabled: c.Enabled, ExpiresUnix: EffectiveExpiry(c, u),
			RemainingBytes: Remaining(c, u), MaxIPs: c.MaxIPs})
	}
	return out, nil
}

// ApplyUsage stores one batch exactly once. A batch with a sequence the
// node already delivered is acknowledged without being counted again.
func (s *Store) ApplyUsage(ctx context.Context, nodeID string, b wire.UsageBatch) (wire.UsageAck, error) {
	if b.Seq == 0 {
		return wire.UsageAck{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wire.UsageAck{}, err
	}
	defer tx.Rollback()
	var last uint64
	var pct int64
	if err := tx.QueryRowContext(ctx, `SELECT usage_seq,traffic_percent FROM nodes WHERE id=?`, nodeID).Scan(&last, &pct); err != nil {
		return wire.UsageAck{}, ErrNotFound
	}
	if pct <= 0 {
		pct = 100
	}
	if b.Seq <= last {
		return wire.UsageAck{Seq: last}, nil
	}
	now := s.now()
	ts := b.ToUnix
	if ts <= 0 || ts > now.Unix()+300 {
		ts = now.Unix()
	}
	day := time.Unix(ts, 0).UTC().Format("2006-01-02")
	// Statements reused for every row of the batch (thousands of users).
	prep := func(q string) (*sql.Stmt, error) { return tx.PrepareContext(ctx, q) }
	stUsage, err := prep(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)
ON CONFLICT(email) DO UPDATE SET up=up+excluded.up, down=down+excluded.down, last_seen_unix=excluded.last_seen_unix,
first_seen_unix=CASE WHEN first_seen_unix=0 THEN excluded.first_seen_unix ELSE first_seen_unix END`)
	if err != nil {
		return wire.UsageAck{}, err
	}
	defer stUsage.Close()
	stDaily, err := prep(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(email,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`)
	if err != nil {
		return wire.UsageAck{}, err
	}
	defer stDaily.Close()
	renamed := renamedNow(ctx, tx)
	for _, t := range b.Traffic {
		if t.Up < 0 || t.Down < 0 || !emailRE.MatchString(t.Email) {
			return wire.UsageAck{}, ErrInvalid
		}
		if to, ok := renamed[t.Email]; ok {
			t.Email = to
		}
		if t.Up == 0 && t.Down == 0 {
			continue
		}
		if pct != 100 {
			t.Up, t.Down = t.Up*pct/100, t.Down*pct/100
		}
		if _, err := stUsage.ExecContext(ctx, t.Email, t.Up, t.Down, ts, ts); err != nil {
			return wire.UsageAck{}, err
		}
		if _, err := stDaily.ExecContext(ctx, t.Email, nodeID, day, t.Up, t.Down); err != nil {
			return wire.UsageAck{}, err
		}
	}
	for _, t := range b.Outbounds {
		if t.Up < 0 || t.Down < 0 || !idRE.MatchString(t.Email) || t.Email == apiTag || t.Up+t.Down == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbound_traffic(tag,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(tag,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, nodeID, day, t.Up, t.Down); err != nil {
			return wire.UsageAck{}, err
		}
	}
	for _, t := range b.Inbounds {
		if t.Up < 0 || t.Down < 0 || !idRE.MatchString(t.Email) || t.Email == apiTag || t.Up+t.Down == 0 {
			continue // informational: a bad entry never rejects the batch
		}
		t.Email = parentTag(t.Email)
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_traffic(tag,day,up,down) VALUES(?,?,?,?)
ON CONFLICT(tag,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, day, t.Up, t.Down); err != nil {
			return wire.UsageAck{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO route_traffic(tag,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(tag,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, nodeID, day, t.Up, t.Down); err != nil {
			return wire.UsageAck{}, err
		}
	}
	if b.Online != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM online WHERE node_id=?`, nodeID); err != nil {
			return wire.UsageAck{}, err
		}
		stOnline, err := prep(`INSERT OR REPLACE INTO online(email,node_id,ips,seen_unix) VALUES(?,?,?,?)`)
		if err != nil {
			return wire.UsageAck{}, err
		}
		defer stOnline.Close()
		stIP, err := prep(`INSERT INTO ip_log(email,ip,node_id,first_unix,last_unix) VALUES(?,?,?,?,?)
ON CONFLICT(email,ip,node_id) DO UPDATE SET last_unix=MAX(last_unix,excluded.last_unix)`)
		if err != nil {
			return wire.UsageAck{}, err
		}
		defer stIP.Close()
		for _, o := range b.Online {
			if to, ok := renamed[o.Email]; ok {
				o.Email = to
			}
			ips, _ := json.Marshal(o.IPs)
			if _, err := stOnline.ExecContext(ctx, o.Email, nodeID, string(ips), ts); err != nil {
				return wire.UsageAck{}, err
			}
			for i, ip := range o.IPs {
				if i >= 32 || len(ip) > 64 {
					break
				}
				if _, err := stIP.ExecContext(ctx, o.Email, ip, nodeID, ts, ts); err != nil {
					return wire.UsageAck{}, err
				}
			}
		}
	}
	for _, d := range b.Disconnects {
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, d.Unix, nodeID, d.Email, "blocked:"+d.Reason); err != nil {
			return wire.UsageAck{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET usage_seq=?, last_seen_unix=? WHERE id=?`, b.Seq, now.Unix(), nodeID); err != nil {
		return wire.UsageAck{}, err
	}
	if err := tx.Commit(); err != nil {
		return wire.UsageAck{}, err
	}
	return wire.UsageAck{Seq: b.Seq}, nil
}

// Prune bounds history: daily rows older than keepDays and old events.
func (s *Store) Prune(ctx context.Context, keepDays int) error {
	cut := s.now().AddDate(0, 0, -keepDays)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM usage_daily WHERE day < ?`, cut.UTC().Format("2006-01-02")); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM outbound_traffic WHERE day < ?`, cut.UTC().Format("2006-01-02")); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM ip_log WHERE last_unix < ?`, s.now().Add(-30*24*time.Hour).Unix()); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_history WHERE bucket < ?`, s.now().Add(-7*24*time.Hour).Unix()); err != nil {
		return err
	}
	// The audit log keeps a year; held discount codes expire after an hour.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE unix < ?`, s.now().AddDate(-1, 0, 0).Unix()); err != nil && !noTable(err) {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM discount_pending WHERE unix < ?`, s.now().Add(-2*time.Hour).Unix()); err != nil && !noTable(err) && !strings.Contains(err.Error(), "no such column") {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE unix < ?`, cut.Unix())
	return err
}

func noTable(err error) bool { return err != nil && strings.Contains(err.Error(), "no such table") }

// NodeStatus is what the dashboard shows per node.
type NodeStatus struct {
	ID, Name, AgentVersion, LastError string
	LastSeen                          int64
	XrayRunning                       bool
	XrayRestarts                      int64
	Spooled                           int
	ConfigRevision, ReportedRevision  int64
	UsageSeq                          int64
	Host                              *wire.Host
	PublicIP                          string
	PublicIPFixed                     bool `json:"public_ip_fixed"`
	AgentSHA                          string
	UpdateAvailable                   bool   `json:"update_available"`
	XrayVersion                       string `json:"xray_version"`
	Arch                              string `json:"arch"`
	// TrafficPercent bills client traffic on this node: 150 counts 1 GB as
	// 1.5 GB (a premium node), 50 as half. Server-side totals stay real.
	TrafficPercent int `json:"traffic_percent"`
}

// HistoryPoint is one 5-minute sample of a node's health.
type HistoryPoint struct {
	Unix   int64   `json:"t"`
	CPU    float64 `json:"cpu"`
	RAM    float64 `json:"ram"`
	RX     float64 `json:"rx"`
	TX     float64 `json:"tx"`
	Online int     `json:"online"`
}

// NodeHistory returns up to the last `hours` of samples.
func (s *Store) NodeHistory(ctx context.Context, id string, hours int) ([]HistoryPoint, error) {
	if hours <= 0 || hours > 24*7 {
		hours = 24
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT bucket,cpu,ram_pct,rx_rate,tx_rate,online FROM node_history WHERE node_id=? AND bucket>=? ORDER BY bucket`,
		id, s.now().Unix()-int64(hours)*3600)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HistoryPoint{}
	for rows.Next() {
		var p HistoryPoint
		if err := rows.Scan(&p.Unix, &p.CPU, &p.RAM, &p.RX, &p.TX, &p.Online); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Online reports whether the node synced within 3 polls.
func (n NodeStatus) Online(now time.Time) bool {
	return now.Unix()-n.LastSeen <= 3*PollSeconds
}

func (s *Store) Nodes(ctx context.Context) ([]NodeStatus, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,name,agent_version,last_error,last_seen_unix,xray_running,xray_restarts,spooled,
config_revision,reported_revision,usage_seq,host_json,public_ip,agent_sha,xray_version,agent_arch,traffic_percent,public_ip_fixed FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeStatus
	dist := s.DistSHA()
	hidden := s.hiddenTunnels(ctx)
	for rows.Next() {
		var n NodeStatus
		var hj string
		if err := rows.Scan(&n.ID, &n.Name, &n.AgentVersion, &n.LastError, &n.LastSeen, &n.XrayRunning, &n.XrayRestarts,
			&n.Spooled, &n.ConfigRevision, &n.ReportedRevision, &n.UsageSeq, &hj, &n.PublicIP, &n.AgentSHA, &n.XrayVersion, &n.Arch, &n.TrafficPercent, &n.PublicIPFixed); err != nil {
			return nil, err
		}
		if hj != "" {
			var h wire.Host
			if json.Unmarshal([]byte(hj), &h) == nil {
				kept := h.Tunnels[:0]
				for _, t := range h.Tunnels {
					if !hidden[n.ID+":"+t.Iface] {
						kept = append(kept, t)
					}
				}
				h.Tunnels = kept
				n.Host = &h
			}
		}
		n.UpdateAvailable = dist != "" && n.AgentSHA != "" && n.AgentSHA != dist
		out = append(out, n)
	}
	return out, rows.Err()
}

// bearer extracts "Bearer <token>".
func bearer(h string) (string, error) {
	tok, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || tok == "" {
		return "", errors.New("missing bearer token")
	}
	return tok, nil
}

// hiddenTunnels: tunnels the owner keeps out of the panel (they belong to
// another panel on the same server); "node:iface" keys.
func (s *Store) hiddenTunnels(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	for _, k := range strings.Split(s.Setting(ctx, "tunnels_hidden"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			out[k] = true
		}
	}
	return out
}

// HideTunnel hides or shows one tunnel of a node.
func (s *Store) HideTunnel(ctx context.Context, node, iface string, hide bool) error {
	if !idRE.MatchString(node) || iface == "" || len(iface) > 32 || strings.ContainsAny(iface, ",: ") {
		return ErrInvalid
	}
	m := s.hiddenTunnels(ctx)
	if hide {
		m[node+":"+iface] = true
	} else {
		delete(m, node+":"+iface)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return s.SetSetting(ctx, "tunnels_hidden", strings.Join(keys, ","))
}
