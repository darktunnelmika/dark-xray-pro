package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Usage resets: manual (button) or periodic per client (day, week, month,
// counted from the last reset). Every reset is logged with the bytes it
// cleared, so a reset can be audited and the totals explained.
//
// IP log: every address a node reports for a client is kept with first
// and last sighting for 30 days.

const resetSchema = `
CREATE TABLE IF NOT EXISTS usage_resets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL,
  unix INTEGER NOT NULL,
  up INTEGER NOT NULL,
  down INTEGER NOT NULL,
  reason TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS usage_resets_email ON usage_resets(email, unix);
CREATE TABLE IF NOT EXISTS ip_log (
  email TEXT NOT NULL,
  ip TEXT NOT NULL,
  node_id TEXT NOT NULL,
  first_unix INTEGER NOT NULL,
  last_unix INTEGER NOT NULL,
  PRIMARY KEY(email, ip, node_id)
);
CREATE INDEX IF NOT EXISTS ip_log_last ON ip_log(last_unix);
`

var resetPeriods = map[string]bool{"": true, "day": true, "week": true, "month": true}

func (s *Store) ensureResetSchema() error {
	if _, err := s.db.Exec(resetSchema); err != nil {
		return err
	}
	if err := s.addColumn("clients", "reset_period", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.addColumn("clients", "last_reset_unix", "INTEGER NOT NULL DEFAULT 0")
}

// ResetUsage clears a client's metered total and logs what was cleared.
func (s *Store) ResetUsage(ctx context.Context, email, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, email).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	var up, down int64
	_ = tx.QueryRowContext(ctx, `SELECT up,down FROM client_usage WHERE email=?`, email).Scan(&up, &down)
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_resets(email,unix,up,down,reason) VALUES(?,?,?,?,?)`, email, now, up, down, reason); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE client_usage SET up=0, down=0 WHERE email=?`, email); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET last_reset_unix=? WHERE email=?`, now, email); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, now, "", email, "reset:"+reason); err != nil {
		return err
	}
	return tx.Commit()
}

// SetResetPeriod sets the periodic reset ("" turns it off). The period
// starts now.
func (s *Store) SetResetPeriod(ctx context.Context, email, period string) error {
	if !resetPeriods[period] {
		return fmt.Errorf("%w: period must be day, week, month or empty", ErrInvalid)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET reset_period=?, last_reset_unix=CASE WHEN last_reset_unix=0 OR reset_period<>? THEN ? ELSE last_reset_unix END WHERE email=?`,
		period, period, s.now().Unix(), email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// nextReset is when a periodic reset is due.
func nextReset(period string, last int64) int64 {
	t := time.Unix(last, 0).UTC()
	switch period {
	case "day":
		return t.AddDate(0, 0, 1).Unix()
	case "week":
		return t.AddDate(0, 0, 7).Unix()
	case "month":
		return t.AddDate(0, 1, 0).Unix()
	}
	return 0
}

// runDueResets resets clients whose period has elapsed; returns how many.
func (s *Store) runDueResets(ctx context.Context) int {
	rows, err := s.ro.QueryContext(ctx, `SELECT email,reset_period,last_reset_unix FROM clients WHERE reset_period<>''`)
	if err != nil {
		return 0
	}
	type due struct{ email, period string }
	var list []due
	now := s.now().Unix()
	for rows.Next() {
		var d due
		var last int64
		if rows.Scan(&d.email, &d.period, &last) == nil && last > 0 && nextReset(d.period, last) <= now {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		if err := s.ResetUsage(ctx, d.email, "auto:"+d.period); err != nil {
			log.Printf("periodic reset %s: %v", d.email, err)
		}
	}
	return len(list)
}

// RunResets checks periodic resets and next plans every minute.
func (s *Store) RunResets(ctx context.Context) {
	for {
		s.runDueResets(ctx)
		s.runNextPlans(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

// UsageReset is one logged reset.
type UsageReset struct {
	Unix   int64  `json:"unix"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
	Reason string `json:"reason"`
}

func (s *Store) UsageResets(ctx context.Context, email string) ([]UsageReset, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT unix,up,down,reason FROM usage_resets WHERE email=? ORDER BY unix DESC LIMIT 50`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageReset{}
	for rows.Next() {
		var r UsageReset
		if err := rows.Scan(&r.Unix, &r.Up, &r.Down, &r.Reason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClientIP is one address seen for a client.
type ClientIP struct {
	IP     string `json:"ip"`
	NodeID string `json:"node_id"`
	First  int64  `json:"first"`
	Last   int64  `json:"last"`
	Online bool   `json:"online"`
}

// ClientIPs lists addresses seen in the last 30 days, online ones first.
func (s *Store) ClientIPs(ctx context.Context, email string) ([]ClientIP, error) {
	online := map[string]bool{}
	rows, err := s.ro.QueryContext(ctx, `SELECT node_id,ips FROM online WHERE email=? AND seen_unix>=?`, email, s.now().Unix()-120)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var node, ips string
		var list []string
		if rows.Scan(&node, &ips) == nil && json.Unmarshal([]byte(ips), &list) == nil {
			for _, ip := range list {
				online[node+"|"+ip] = true
			}
		}
	}
	rows.Close()
	rows, err = s.ro.QueryContext(ctx, `SELECT ip,node_id,first_unix,last_unix FROM ip_log WHERE email=? AND last_unix>=? ORDER BY last_unix DESC LIMIT 200`,
		email, s.now().Unix()-30*86400)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClientIP{}
	var off []ClientIP
	for rows.Next() {
		var c ClientIP
		if err := rows.Scan(&c.IP, &c.NodeID, &c.First, &c.Last); err != nil {
			return nil, err
		}
		c.Online = online[c.NodeID+"|"+c.IP]
		if c.Online {
			out = append(out, c)
		} else {
			off = append(off, c)
		}
	}
	return append(out, off...), rows.Err()
}
