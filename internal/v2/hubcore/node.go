package hubcore

import (
	"context"
	"encoding/json"
	"errors"
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
	users, err := s.limits(ctx, nodeID)
	if err != nil {
		return wire.SyncResponse{}, err
	}
	resp := wire.SyncResponse{ConfigRevision: uint64(rev), Users: users, UsageSeq: uint64(usageSeq), PollSeconds: PollSeconds, UnixTime: now}
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
	if err := tx.QueryRowContext(ctx, `SELECT usage_seq FROM nodes WHERE id=?`, nodeID).Scan(&last); err != nil {
		return wire.UsageAck{}, ErrNotFound
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
	for _, t := range b.Traffic {
		if t.Up < 0 || t.Down < 0 || !emailRE.MatchString(t.Email) {
			return wire.UsageAck{}, ErrInvalid
		}
		if t.Up == 0 && t.Down == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)
ON CONFLICT(email) DO UPDATE SET up=up+excluded.up, down=down+excluded.down, last_seen_unix=excluded.last_seen_unix,
first_seen_unix=CASE WHEN first_seen_unix=0 THEN excluded.first_seen_unix ELSE first_seen_unix END`,
			t.Email, t.Up, t.Down, ts, ts); err != nil {
			return wire.UsageAck{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_daily(email,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(email,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, nodeID, day, t.Up, t.Down); err != nil {
			return wire.UsageAck{}, err
		}
	}
	for _, t := range b.Inbounds {
		if t.Up < 0 || t.Down < 0 || !idRE.MatchString(t.Email) || t.Email == apiTag || t.Up+t.Down == 0 {
			continue // informational: a bad entry never rejects the batch
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_traffic(tag,day,up,down) VALUES(?,?,?,?)
ON CONFLICT(tag,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, day, t.Up, t.Down); err != nil {
			return wire.UsageAck{}, err
		}
	}
	if b.Online != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM online WHERE node_id=?`, nodeID); err != nil {
			return wire.UsageAck{}, err
		}
		for _, o := range b.Online {
			ips, _ := json.Marshal(o.IPs)
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO online(email,node_id,ips,seen_unix) VALUES(?,?,?,?)`, o.Email, nodeID, string(ips), ts); err != nil {
				return wire.UsageAck{}, err
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_history WHERE bucket < ?`, s.now().Add(-7*24*time.Hour).Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE unix < ?`, cut.Unix())
	return err
}

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
config_revision,reported_revision,usage_seq,host_json FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeStatus
	for rows.Next() {
		var n NodeStatus
		var hj string
		if err := rows.Scan(&n.ID, &n.Name, &n.AgentVersion, &n.LastError, &n.LastSeen, &n.XrayRunning, &n.XrayRestarts,
			&n.Spooled, &n.ConfigRevision, &n.ReportedRevision, &n.UsageSeq, &hj); err != nil {
			return nil, err
		}
		if hj != "" {
			var h wire.Host
			if json.Unmarshal([]byte(hj), &h) == nil {
				n.Host = &h
			}
		}
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
