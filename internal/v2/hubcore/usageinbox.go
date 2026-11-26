package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Usage batches are taken in two steps, so a big batch never holds the
// database for long:
//
//  1. accept: one short transaction stores the batch in usage_inbox and
//     moves the node's usage_seq. From here the batch is counted exactly
//     once, whatever happens: a repeat of the same sequence is acknowledged
//     without being stored again.
//  2. apply: the batch is applied in chunks of usageChunk rows (about 25 ms each for 500), each in its
//     own short transaction that also records how far it got (a
//     compare-and-set on usage_inbox.done), so other writers — the panel,
//     the APIs — get their turn between chunks, and a crash in the middle
//     resumes where it stopped instead of counting anything twice.
//
// A node is told (SyncResponse.UsageSeq) only the sequences that are fully
// applied (nodes.applied_seq): until then it keeps counting the batch
// locally, so limits are never looser than before. ApplyUsage returns after
// both steps, so callers see the usage at once, as before.

const usageChunk = 500

const usageInboxSchema = `
CREATE TABLE IF NOT EXISTS usage_inbox (
  node_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  body TEXT NOT NULL,
  has_online INTEGER NOT NULL DEFAULT 0,
  ts INTEGER NOT NULL,
  pct INTEGER NOT NULL DEFAULT 100,
  done INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(node_id, seq)
);
`

func (s *Store) ensureUsageInboxSchema() error {
	if _, err := s.db.Exec(usageInboxSchema); err != nil {
		return err
	}
	var has int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name='applied_seq'`).Scan(&has)
	if has == 0 {
		if err := s.addColumn("nodes", "applied_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
		// Before the inbox, every accepted batch was applied at once.
		if _, err := s.db.Exec(`UPDATE nodes SET applied_seq=usage_seq`); err != nil {
			return err
		}
	}
	return nil
}

// usageLock serialises the batches of one node inside this process.
func (s *Store) usageLock(nodeID string) *sync.Mutex {
	m, _ := s.usageMu.LoadOrStore(nodeID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// ApplyUsage stores one batch exactly once. A batch with a sequence the
// node already delivered is acknowledged without being counted again.
func (s *Store) ApplyUsage(ctx context.Context, nodeID string, b wire.UsageBatch) (wire.UsageAck, error) {
	if b.Seq == 0 {
		return wire.UsageAck{}, ErrInvalid
	}
	for _, t := range b.Traffic {
		if t.Up < 0 || t.Down < 0 || !emailRE.MatchString(t.Email) {
			return wire.UsageAck{}, ErrInvalid
		}
	}
	mu := s.usageLock(nodeID)
	mu.Lock()
	defer mu.Unlock()
	seq, err := s.acceptUsage(ctx, nodeID, b)
	if err != nil {
		return wire.UsageAck{}, err
	}
	if err := s.drainUsage(ctx, nodeID); err != nil {
		return wire.UsageAck{}, err
	}
	return wire.UsageAck{Seq: seq}, nil
}

// acceptUsage stores the batch in the inbox (step 1) and returns the
// sequence to acknowledge.
func (s *Store) acceptUsage(ctx context.Context, nodeID string, b wire.UsageBatch) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var last uint64
	var pct int64
	if err := tx.QueryRowContext(ctx, `SELECT usage_seq,traffic_percent FROM nodes WHERE id=?`, nodeID).Scan(&last, &pct); err != nil {
		return 0, ErrNotFound
	}
	if b.Seq <= last {
		return last, nil
	}
	if pct <= 0 {
		pct = 100
	}
	now := s.now()
	ts := b.ToUnix
	if ts <= 0 || ts > now.Unix()+300 {
		ts = now.Unix()
	}
	body, err := json.Marshal(b)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_inbox(node_id,seq,body,has_online,ts,pct) VALUES(?,?,?,?,?,?)`,
		nodeID, b.Seq, string(body), b.Online != nil, ts, pct); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET usage_seq=?, last_seen_unix=? WHERE id=?`, b.Seq, now.Unix(), nodeID); err != nil {
		return 0, err
	}
	return b.Seq, tx.Commit()
}

// drainAllUsage applies whatever any node left in the inbox.
func (s *Store) drainAllUsage(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT node_id FROM usage_inbox`)
	if err != nil {
		return err
	}
	var nodes []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			nodes = append(nodes, n)
		}
	}
	rows.Close()
	for _, n := range nodes {
		mu := s.usageLock(n)
		mu.Lock()
		err := s.drainUsage(ctx, n)
		mu.Unlock()
		if err != nil {
			log.Printf("usage inbox %s: %v", n, err)
		}
	}
	return nil
}

// drainUsage applies a node's accepted batches, oldest first (step 2). The
// caller holds the node's usage lock.
func (s *Store) drainUsage(ctx context.Context, nodeID string) error {
	for {
		var seq uint64
		var body string
		var hasOnline bool
		var ts, pct, done int64
		err := s.db.QueryRowContext(ctx, `SELECT seq,body,has_online,ts,pct,done FROM usage_inbox WHERE node_id=? ORDER BY seq LIMIT 1`, nodeID).
			Scan(&seq, &body, &hasOnline, &ts, &pct, &done)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		var b wire.UsageBatch
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			return fmt.Errorf("usage inbox %s/%d: %w", nodeID, seq, err)
		}
		if hasOnline && b.Online == nil {
			b.Online = []wire.UserOnline{} // an empty snapshot still clears the node's list
		}
		b.Seq = seq
		for {
			// One chunk of any node at a time: the panel's own writes then
			// wait behind at most one chunk, not one per reporting node.
			s.applyMu.Lock()
			next, finished, err := s.applyUsageChunk(ctx, nodeID, b, ts, pct, done)
			s.applyMu.Unlock()
			if err != nil {
				return err
			}
			if finished {
				break
			}
			done = next
		}
	}
}

// applyUsageChunk applies rows [done, done+usageChunk) of the batch — the
// traffic rows first, then the online rows — in one transaction, or the
// last part (inbound and outbound totals, disconnects) and closes the
// batch. It returns the new position and whether the batch is finished.
func (s *Store) applyUsageChunk(ctx context.Context, nodeID string, b wire.UsageBatch, ts, pct, done int64) (int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	day := time.Unix(ts, 0).UTC().Format("2006-01-02")
	nT, nO := int64(len(b.Traffic)), int64(len(b.Online))
	end := min(done+usageChunk, nT+nO)
	last := done >= nT+nO
	if !last {
		r, err := tx.ExecContext(ctx, `UPDATE usage_inbox SET done=? WHERE node_id=? AND seq=? AND done=?`, end, nodeID, b.Seq, done)
		if err != nil {
			return 0, false, err
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return 0, false, fmt.Errorf("usage inbox %s/%d moved underneath", nodeID, b.Seq)
		}
	}
	prep := func(q string) (*sql.Stmt, error) { return tx.PrepareContext(ctx, q) }
	renamed := renamedNow(ctx, tx)
	if done < nT {
		// One revision for the chunk: connected shops read "usage changed
		// since my cursor" from it (tenantbulk.go).
		var rev int64
		if err := tx.QueryRowContext(ctx, `UPDATE rev_seq SET v=v+1 WHERE id=1 RETURNING v`).Scan(&rev); err != nil {
			return 0, false, err
		}
		stUsage, err := prep(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix,rev) VALUES(?,?,?,?,?,?)
ON CONFLICT(email) DO UPDATE SET up=up+excluded.up, down=down+excluded.down, last_seen_unix=excluded.last_seen_unix,
first_seen_unix=CASE WHEN first_seen_unix=0 THEN excluded.first_seen_unix ELSE first_seen_unix END, rev=excluded.rev`)
		if err != nil {
			return 0, false, err
		}
		defer stUsage.Close()
		stDaily, err := prep(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(email,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`)
		if err != nil {
			return 0, false, err
		}
		defer stDaily.Close()
		for _, t := range b.Traffic[done:min(end, nT)] {
			if to, ok := renamed[t.Email]; ok {
				t.Email = to
			}
			if t.Up == 0 && t.Down == 0 {
				continue
			}
			if pct != 100 {
				t.Up, t.Down = t.Up*pct/100, t.Down*pct/100
			}
			if _, err := stUsage.ExecContext(ctx, t.Email, t.Up, t.Down, ts, ts, rev); err != nil {
				return 0, false, err
			}
			if _, err := stDaily.ExecContext(ctx, t.Email, nodeID, day, t.Up, t.Down); err != nil {
				return 0, false, err
			}
		}
	}
	if b.Online != nil && end > nT && !last {
		from := max(done, nT) - nT
		if from == 0 {
			// The node's snapshot replaces its old one.
			if _, err := tx.ExecContext(ctx, `DELETE FROM online WHERE node_id=?`, nodeID); err != nil {
				return 0, false, err
			}
		}
		stOnline, err := prep(`INSERT OR REPLACE INTO online(email,node_id,ips,seen_unix) VALUES(?,?,?,?)`)
		if err != nil {
			return 0, false, err
		}
		defer stOnline.Close()
		stIP, err := prep(`INSERT INTO ip_log(email,ip,node_id,first_unix,last_unix) VALUES(?,?,?,?,?)
ON CONFLICT(email,ip,node_id) DO UPDATE SET last_unix=MAX(last_unix,excluded.last_unix)`)
		if err != nil {
			return 0, false, err
		}
		defer stIP.Close()
		for _, o := range b.Online[from : end-nT] {
			if to, ok := renamed[o.Email]; ok {
				o.Email = to
			}
			ips, _ := json.Marshal(o.IPs)
			if _, err := stOnline.ExecContext(ctx, o.Email, nodeID, string(ips), ts); err != nil {
				return 0, false, err
			}
			for i, ip := range o.IPs {
				if i >= 32 || len(ip) > 64 {
					break
				}
				if _, err := stIP.ExecContext(ctx, o.Email, ip, nodeID, ts, ts); err != nil {
					return 0, false, err
				}
			}
		}
	}
	if !last {
		return end, false, tx.Commit()
	}
	// The last part: small totals and events, then the batch is closed.
	if b.Online != nil && nO == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM online WHERE node_id=?`, nodeID); err != nil {
			return 0, false, err
		}
	}
	for _, t := range b.Outbounds {
		if t.Up < 0 || t.Down < 0 || !idRE.MatchString(t.Email) || t.Email == apiTag || t.Up+t.Down == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbound_traffic(tag,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(tag,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, nodeID, day, t.Up, t.Down); err != nil {
			return 0, false, err
		}
	}
	for _, t := range b.Inbounds {
		if t.Up < 0 || t.Down < 0 || !idRE.MatchString(t.Email) || t.Email == apiTag || t.Up+t.Down == 0 {
			continue // informational: a bad entry never rejects the batch
		}
		t.Email = parentTag(t.Email)
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_traffic(tag,day,up,down) VALUES(?,?,?,?)
ON CONFLICT(tag,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, day, t.Up, t.Down); err != nil {
			return 0, false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO route_traffic(tag,node_id,day,up,down) VALUES(?,?,?,?,?)
ON CONFLICT(tag,node_id,day) DO UPDATE SET up=up+excluded.up, down=down+excluded.down`, t.Email, nodeID, day, t.Up, t.Down); err != nil {
			return 0, false, err
		}
	}
	for _, d := range b.Disconnects {
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, d.Unix, nodeID, d.Email, "blocked:"+d.Reason); err != nil {
			return 0, false, err
		}
	}
	r, err := tx.ExecContext(ctx, `DELETE FROM usage_inbox WHERE node_id=? AND seq=? AND done=?`, nodeID, b.Seq, done)
	if err != nil {
		return 0, false, err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return 0, false, fmt.Errorf("usage inbox %s/%d moved underneath", nodeID, b.Seq)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET applied_seq=MAX(applied_seq,?) WHERE id=?`, b.Seq, nodeID); err != nil {
		return 0, false, err
	}
	return done, true, tx.Commit()
}
