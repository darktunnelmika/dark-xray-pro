package hubcore

import (
	"context"
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
	// The node hears only of fully applied batches (usageinbox.go): it keeps
	// counting the others itself, so its limits never get looser.
	if err := tx.QueryRowContext(ctx, `SELECT config_revision,config_sha256,applied_seq FROM nodes WHERE id=?`, nodeID).
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
	// One query for the clients and their usage (not one per client: a node
	// can carry tens of thousands).
	rows, err := s.ro.QueryContext(ctx, `SELECT c.email,c.enabled,c.volume_bytes,c.expires_unix,c.duration_seconds,c.max_ips,
COALESCE(u.up,0),COALESCE(u.down,0),COALESCE(u.first_seen_unix,0),COALESCE(u.last_seen_unix,0)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email
WHERE c.email IN (SELECT ci.email FROM client_inbounds ci JOIN inbound_routes r ON r.tag=ci.tag WHERE r.node_id=?) ORDER BY c.email`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wire.UserLimit{}
	for rows.Next() {
		var c Client
		var u Usage
		if err := rows.Scan(&c.Email, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &c.MaxIPs, &u.Up, &u.Down, &u.FirstSeen, &u.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, wire.UserLimit{Email: c.Email, Enabled: c.Enabled, ExpiresUnix: EffectiveExpiry(c, u),
			RemainingBytes: Remaining(c, u), MaxIPs: c.MaxIPs})
	}
	return out, rows.Err()
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
	// Read first, before the rows hold a connection: a second query while
	// they are open waits for another one, and with every reader doing the
	// same (a burst of subscription fetches ranks nodes in each) the pool
	// runs dry and all of them wait for ever.
	dist := s.DistSHA()
	hidden := s.hiddenTunnels(ctx)
	rows, err := s.ro.QueryContext(ctx, `SELECT id,name,agent_version,last_error,last_seen_unix,xray_running,xray_restarts,spooled,
config_revision,reported_revision,usage_seq,host_json,public_ip,agent_sha,xray_version,agent_arch,traffic_percent,public_ip_fixed FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeStatus
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
