package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

// Client templates, next plans, per-node usage and device (HWID) limits.

const planSchema = `
CREATE TABLE IF NOT EXISTS client_templates (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  body TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS client_devices (
  email TEXT NOT NULL,
  hwid TEXT NOT NULL,
  os TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  agent TEXT NOT NULL DEFAULT '',
  first_unix INTEGER NOT NULL,
  last_unix INTEGER NOT NULL,
  PRIMARY KEY(email, hwid)
);
`

func (s *Store) ensurePlanSchema() error {
	if _, err := s.db.Exec(planSchema); err != nil {
		return err
	}
	if err := s.addColumn("clients", "next_plan", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.addColumn("clients", "hwid_limit", "INTEGER NOT NULL DEFAULT 0")
}

// ---------- templates ----------

// ClientTemplate pre-fills a new client.
type ClientTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	GB          float64  `json:"gb"`   // 0 = unlimited
	Days        int      `json:"days"` // 0 = no expiry
	FirstUse    bool     `json:"first_use"`
	MaxIPs      int      `json:"max_ips"`
	HWIDLimit   int      `json:"hwid_limit"`
	ResetPeriod string   `json:"reset_period"`
	Inbounds    []string `json:"inbounds"`
	Prefix      string   `json:"prefix"` // suggested name prefix
}

func (s *Store) Templates(ctx context.Context) ([]ClientTemplate, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT body FROM client_templates ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ClientTemplate{}
	for rows.Next() {
		var b string
		var t ClientTemplate
		if rows.Scan(&b) == nil && json.Unmarshal([]byte(b), &t) == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func (s *Store) PutTemplate(ctx context.Context, t ClientTemplate) error {
	if !idRE.MatchString(t.ID) || strings.TrimSpace(t.Name) == "" || t.GB < 0 || t.Days < 0 || t.MaxIPs < 0 || t.HWIDLimit < 0 || !resetPeriods[t.ResetPeriod] {
		return ErrInvalid
	}
	b, _ := json.Marshal(t)
	_, err := s.db.ExecContext(ctx, `INSERT INTO client_templates(id,name,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, body=excluded.body`, t.ID, t.Name, string(b))
	return err
}

func (s *Store) DeleteTemplate(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM client_templates WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- next plan ----------

// NextPlan is applied automatically when the current plan runs out (volume
// used up or time expired): usage is reset and the new limits start.
type NextPlan struct {
	GB       float64 `json:"gb"`   // 0 = unlimited
	Days     int     `json:"days"` // 0 = no expiry
	FirstUse bool    `json:"first_use"`
}

func (s *Store) SetNextPlan(ctx context.Context, email string, p *NextPlan) error {
	v := ""
	if p != nil {
		if p.GB < 0 || p.Days < 0 || (p.GB == 0 && p.Days == 0) {
			return fmt.Errorf("%w: a next plan needs volume or days", ErrInvalid)
		}
		b, _ := json.Marshal(p)
		v = string(b)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET next_plan=? WHERE email=?`, v, email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// runNextPlans switches clients whose plan ran out to their next plan.
func (s *Store) runNextPlans(ctx context.Context) int {
	rows, err := s.ro.QueryContext(ctx, `SELECT c.email,c.uuid,c.secret,c.enabled,c.volume_bytes,c.expires_unix,c.duration_seconds,c.max_ips,c.next_plan,
COALESCE(u.up,0),COALESCE(u.down,0),COALESCE(u.first_seen_unix,0),COALESCE(u.last_seen_unix,0)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email WHERE c.next_plan<>'' AND c.enabled=1`)
	if err != nil {
		return 0
	}
	type due struct {
		email string
		plan  NextPlan
	}
	var list []due
	now := s.now().Unix()
	for rows.Next() {
		var c Client
		var u Usage
		var np string
		if rows.Scan(&c.Email, &c.UUID, &c.Secret, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &c.MaxIPs, &np, &u.Up, &u.Down, &u.FirstSeen, &u.LastSeen) != nil {
			continue
		}
		exp := EffectiveExpiry(c, u)
		ended := Remaining(c, u) == 0 || (exp > 0 && exp <= now)
		var p NextPlan
		if ended && json.Unmarshal([]byte(np), &p) == nil {
			list = append(list, due{c.Email, p})
		}
	}
	rows.Close()
	for _, d := range list {
		if err := s.applyNextPlan(ctx, d.email, d.plan); err != nil {
			log.Printf("next plan %s: %v", d.email, err)
		}
	}
	return len(list)
}

func (s *Store) applyNextPlan(ctx context.Context, email string, p NextPlan) error {
	if err := s.ResetUsage(ctx, email, "next-plan"); err != nil {
		return err
	}
	vol := int64(-1)
	if p.GB > 0 {
		vol = int64(p.GB * (1 << 30))
	}
	var exp, dur int64
	if p.Days > 0 {
		if p.FirstUse {
			dur = int64(p.Days) * 86400
		} else {
			exp = s.now().Unix() + int64(p.Days)*86400
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET volume_bytes=?, expires_unix=?, duration_seconds=?, next_plan='' WHERE email=?`, vol, exp, dur, email); err != nil {
		return err
	}
	if p.FirstUse {
		// The new period starts at the next connection.
		if _, err := tx.ExecContext(ctx, `UPDATE client_usage SET first_seen_unix=0 WHERE email=?`, email); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, s.now().Unix(), "", email, "next-plan"); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- per-node usage ----------

// NodeUsage is a client's traffic on one node over a period.
type NodeUsage struct {
	NodeID string `json:"node_id"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
}

func (s *Store) ClientNodeUsage(ctx context.Context, email string, days int) ([]NodeUsage, error) {
	if days <= 0 || days > 400 {
		days = 30
	}
	from := s.now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT node_id,SUM(up),SUM(down) FROM usage_daily WHERE email=? AND day>=? GROUP BY node_id ORDER BY SUM(up+down) DESC`, email, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeUsage{}
	for rows.Next() {
		var n NodeUsage
		if err := rows.Scan(&n.NodeID, &n.Up, &n.Down); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---------- devices (HWID) ----------

// Device is an app install that fetched the subscription (apps such as
// Happ, v2rayN and Hiddify send an x-hwid header).
type Device struct {
	HWID  string `json:"hwid"`
	OS    string `json:"os"`
	Model string `json:"model"`
	Agent string `json:"agent"`
	First int64  `json:"first"`
	Last  int64  `json:"last"`
}

// ErrDeviceLimit: the subscription was fetched from one device too many.
var ErrDeviceLimit = errors.New("device limit reached")

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// CheckDevice records the device behind a subscription fetch and refuses
// a new one past the client's limit. Fetches without an x-hwid header are
// allowed unless strict mode is on.
func (s *Store) CheckDevice(ctx context.Context, email, hwid, os, model, agent string) error {
	var limit int
	if err := s.ro.QueryRowContext(ctx, `SELECT hwid_limit FROM clients WHERE email=?`, email).Scan(&limit); err != nil {
		return err
	}
	hwid = clip(hwid, 128)
	if hwid == "" {
		if limit > 0 && s.Setting(ctx, "hwid_strict") == "1" {
			return ErrDeviceLimit
		}
		return nil
	}
	now := s.now().Unix()
	r, err := s.db.ExecContext(ctx, `UPDATE client_devices SET last_unix=?, os=COALESCE(NULLIF(?,''),os), model=COALESCE(NULLIF(?,''),model), agent=COALESCE(NULLIF(?,''),agent) WHERE email=? AND hwid=?`, now, clip(os, 64), clip(model, 64), clip(agent, 128), email, hwid)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n > 0 {
		return nil
	}
	if limit > 0 {
		var count int
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_devices WHERE email=?`, email).Scan(&count)
		if count >= limit {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, now, "", email, "device-refused")
			return ErrDeviceLimit
		}
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO client_devices(email,hwid,os,model,agent,first_unix,last_unix) VALUES(?,?,?,?,?,?,?)`,
		email, hwid, clip(os, 64), clip(model, 64), clip(agent, 128), now, now)
	return err
}

func (s *Store) Devices(ctx context.Context, email string) ([]Device, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT hwid,os,model,agent,first_unix,last_unix FROM client_devices WHERE email=? ORDER BY last_unix DESC`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.HWID, &d.OS, &d.Model, &d.Agent, &d.First, &d.Last); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDevice(ctx context.Context, email, hwid string) error {
	q := `DELETE FROM client_devices WHERE email=? AND hwid=?`
	args := []any{email, hwid}
	if hwid == "*" {
		q, args = `DELETE FROM client_devices WHERE email=?`, []any{email}
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

func (s *Store) SetHWIDLimit(ctx context.Context, email string, n int) error {
	if n < 0 || n > 100 {
		return ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET hwid_limit=? WHERE email=?`, n, email)
	if err != nil {
		return err
	}
	if c, _ := r.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}
