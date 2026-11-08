package business

import (
	"context"
	"database/sql"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"sort"
	"time"
)

type InboundPathTraffic struct {
	NodeID     string `json:"node_id"`
	Tag        string `json:"tag"`
	Kind       string `json:"kind"`
	Upload     uint64 `json:"upload,string"`
	Download   uint64 `json:"download,string"`
	Measured   bool   `json:"measured"`
	LastUsage  int64  `json:"last_usage_unix_nano"`
	Generation uint64 `json:"generation"`
	EpochID    string `json:"epoch_id"`
	Fresh      bool   `json:"fresh"`
}
type InboundTraffic struct {
	InboundID uint64               `json:"inbound_id"`
	SampledAt int64                `json:"sampled_at_unix_nano"`
	Coverage  string               `json:"coverage"`
	Paths     []InboundPathTraffic `json:"paths"`
}

// Read only: these network counters never grant a lease or debit a client.
// Totals survive epoch changes; absence of a counter is not a zero reading.
func (s *Store) InboundTraffic(ctx context.Context, key []byte, id uint64) (InboundTraffic, error) {
	out := InboundTraffic{InboundID: id, SampledAt: time.Now().UnixNano(), Coverage: "since_path_metering", Paths: []InboundPathTraffic{}}
	if s.readOnly == nil {
		return out, ErrUnavailable
	}
	tx, err := s.readOnly.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	h, err := readExecutionHead(ctx, tx, key, id)
	if err != nil {
		return out, err
	}
	var archived bool
	if err = tx.QueryRowContext(ctx, "SELECT archived FROM native_inbounds WHERE id=?", id).Scan(&archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return out, err
	}
	if archived || h.DesiredState == "archived" {
		return out, ErrNotFound
	}
	if h.DesiredRevision == 0 {
		return out, nil
	}
	snap, err := readExecutionSnapshot(ctx, tx, key, id, h.DesiredRevision)
	if err != nil {
		return out, err
	}
	nodes := executionSnapshotNodes(snap)
	names := make([]string, 0, len(nodes))
	for n := range nodes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, node := range names {
		var gen uint64
		if err = tx.QueryRowContext(ctx, "SELECT desired_generation FROM inbound_execution_nodes WHERE node_id=? AND installation_id=?", node, nodes[node]).Scan(&gen); err != nil {
			return out, err
		}
		b, e := readExecutionBundle(ctx, tx, key, node, nodes[node], gen)
		if e != nil {
			return out, e
		}
		var epoch, state string
		var seq uint64
		var received int64
		e = tx.QueryRowContext(ctx, "SELECT epoch_id,state,sequence FROM inbound_execution_epochs WHERE node_id=? AND installation_id=? AND generation=? ORDER BY created DESC LIMIT 1", node, nodes[node], gen).Scan(&epoch, &state, &seq)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		if epoch != "" && seq > 0 {
			e = tx.QueryRowContext(ctx, "SELECT created FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=?", epoch, seq).Scan(&received)
			if e != nil {
				return out, e
			}
		}
		for _, l := range b.Listeners {
			if l.InboundID != id {
				continue
			}
			row := InboundPathTraffic{NodeID: node, Tag: l.Tag, Kind: l.Kind, Generation: gen, EpochID: epoch, LastUsage: received}
			pid := "aggregate-" + ix.BytesSHA([]byte(node + "\x00" + l.Tag))[:32]
			for _, p := range b.Principals {
				if p.ID == pid && !p.Billable {
					row.Measured = true
					break
				}
			}
			if row.Measured {
				e = tx.QueryRowContext(ctx, "SELECT upload,download FROM inbound_execution_aggregate_totals WHERE principal_id=? AND node_id=? AND inbound_id=?", pid, node, id).Scan(&row.Upload, &row.Download)
				if e != nil && !errors.Is(e, sql.ErrNoRows) {
					return out, e
				}
				if errors.Is(e, sql.ErrNoRows) {
					row.Measured = false
				}
			}
			row.Fresh = row.Measured && state == "open" && received > 0 && received <= out.SampledAt && out.SampledAt-received < int64(15*time.Second)
			out.Paths = append(out.Paths, row)
		}
	}
	return out, tx.Commit()
}

type InboundTrafficBucket struct {
	NodeID      string `json:"node_id"`
	PrincipalID string `json:"principal_id"`
	Role        string `json:"role"`
	At          int64  `json:"at_unix"`
	Upload      uint64 `json:"upload,string"`
	Download    uint64 `json:"download,string"`
}
type InboundTrafficHistory struct {
	InboundID     uint64                 `json:"inbound_id"`
	Window        string                 `json:"window"`
	From          int64                  `json:"from_unix"`
	Until         int64                  `json:"until_unix"`
	BucketSeconds int64                  `json:"bucket_seconds"`
	Coverage      string                 `json:"coverage"`
	Buckets       []InboundTrafficBucket `json:"buckets"`
}

// History is requested explicitly, never on the 3s live polling loop. It uses
// a bounded read-only snapshot and does not add writes/indexes to accounting.
func (s *Store) InboundTrafficHistory(ctx context.Context, id uint64, window string) (InboundTrafficHistory, error) {
	out := InboundTrafficHistory{InboundID: id, Window: window, Coverage: "since_path_metering", Buckets: []InboundTrafficBucket{}}
	duration := int64(3600)
	switch window {
	case "1h":
	case "24h":
		duration = 86400
	default:
		return out, ErrInvalid
	}
	out.Until = time.Now().Unix()
	out.From = out.Until - duration
	out.BucketSeconds = duration / 60
	if s.readOnly == nil {
		return out, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.readOnly.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var found int
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_inbounds i WHERE id=? AND archived=0 AND NOT EXISTS(SELECT 1 FROM inbound_execution_heads h WHERE h.inbound_id=i.id AND h.desired_state='archived')", id).Scan(&found)
	if err != nil {
		return out, err
	}
	if found != 1 {
		return out, ErrNotFound
	}
	// Scan each retained delta once; no per-listener nested scan or writer access.
	rows, err := tx.QueryContext(ctx, `SELECT a.node_id,d.principal_id,a.role,(r.created/1000000000/?)*?,SUM(d.upload),SUM(d.download)
 FROM inbound_execution_usage_deltas d NOT INDEXED
 JOIN inbound_execution_aggregate_totals a ON a.principal_id=d.principal_id
 JOIN inbound_execution_usage_receipts r ON r.epoch_id=d.epoch_id AND r.sequence=d.sequence
 WHERE a.inbound_id=? AND r.created>=? AND r.created<=? AND a.role IN('owner_aggregate','transit')
 GROUP BY a.node_id,d.principal_id,a.role,r.created/1000000000/? ORDER BY 4 LIMIT 32769`, out.BucketSeconds, out.BucketSeconds, id, out.From*1e9, time.Now().UnixNano(), out.BucketSeconds)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var b InboundTrafficBucket
		if err = rows.Scan(&b.NodeID, &b.PrincipalID, &b.Role, &b.At, &b.Upload, &b.Download); err != nil {
			rows.Close()
			return out, err
		}
		out.Buckets = append(out.Buckets, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Buckets) > 32768 {
		return out, ErrUnavailable
	}
	return out, tx.Commit()
}
