package hubcore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reseller events that come from the panel's state rather than from one
// request, so they cover every way a customer is sold or used (panel,
// Telegram bot, shop, API):
//
//   - client.renewed and client.usage_reset come from the HUB's own event
//     log (renewals, automatic renewals, usage resets);
//   - client.first_used, client.expired and client.volume_exhausted are a
//     customer's state changing, client.expiring (3 days or less left) and
//     client.volume_low (under 10% left) are told once per period, found by
//     a scan every few minutes.
//
// client.created, client.updated, client.deleted and credit.* are written
// where the change happens (reps.go, repctl.go, sellers.go).

const clientStateSchema = `CREATE TABLE IF NOT EXISTS api_client_state (
  email TEXT PRIMARY KEY,
  rep_id TEXT NOT NULL,
  status TEXT NOT NULL,
  expiring_mark INTEGER NOT NULL DEFAULT 0,
  volume_mark TEXT NOT NULL DEFAULT ''
);`

func (s *Store) ensureClientStateSchema() error {
	_, err := s.db.Exec(clientStateSchema)
	return err
}

const clientScanEvery = 5 * time.Minute

func init() {
	apiEventSources = append(apiEventSources, hubLogEvents, func(ctx context.Context, s *Store) {
		if s.clientScanDue() {
			clientStateEvents(ctx, s)
		}
	})
}

var clientScans sync.Map // *Store → time of its last scan

func (s *Store) clientScanDue() bool {
	now := time.Now()
	if last, ok := clientScans.Load(s); ok && now.Sub(last.(time.Time)) < clientScanEvery {
		return false
	}
	clientScans.Store(s, now)
	return true
}

func eventGB(b int64) float64 {
	if b < 0 {
		return -1
	}
	return roundGB(float64(b) / gibF)
}

// creditEvent tells a reseller its credit changed.
func (s *Store) creditEvent(ctx context.Context, repID string, volume, units int64, note string) {
	if volume == 0 && units == 0 {
		return
	}
	var vol, left int64
	_ = s.db.QueryRowContext(ctx, `SELECT volume_credit,unlimited_units FROM reps WHERE id=?`, repID).Scan(&vol, &left)
	ev := "credit.added"
	if volume < 0 || (volume == 0 && units < 0) {
		ev = "credit.removed"
	}
	s.emitAPIEvent(ctx, repTarget(repID), ev, map[string]any{"volume_gb": roundGB(float64(volume) / gibF), "units": units,
		"credit_gb": roundGB(float64(vol) / gibF), "credit_units": left, "note": note})
}

// hubLogEvents turns the HUB's event log (renewals, resets) into reseller
// events. It starts at the end of the log the first time.
func hubLogEvents(ctx context.Context, s *Store) {
	cur, err := strconv.ParseInt(s.Setting(ctx, "api_hublog_cursor"), 10, 64)
	if err != nil {
		var top int64
		_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM events`).Scan(&top)
		_ = s.SetSetting(ctx, "api_hublog_cursor", strconv.FormatInt(top, 10))
		return
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT e.id,e.email,e.kind,COALESCE(c.rep_id,'') FROM events e LEFT JOIN clients c ON c.email=e.email
WHERE e.id>? ORDER BY e.id LIMIT 2000`, cur)
	if err != nil {
		return
	}
	type row struct {
		id               int64
		email, kind, rep string
	}
	var list []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.email, &r.kind, &r.rep) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	for _, r := range list {
		if r.rep == "" {
			continue
		}
		switch {
		case r.kind == "renew" || r.kind == "autorenew":
			if cv, err := s.repClient(ctx, r.rep, r.email); err == nil {
				s.emitAPIEvent(ctx, repTarget(r.rep), "client.renewed", map[string]any{"name": r.email, "automatic": r.kind == "autorenew",
					"plan": cv.Plan, "volume_gb": eventGB(cv.VolumeBytes), "expires_unix": cv.EffectiveUntil})
			}
		case strings.HasPrefix(r.kind, "reset:"):
			s.emitAPIEvent(ctx, repTarget(r.rep), "client.usage_reset", map[string]any{"name": r.email, "reason": strings.TrimPrefix(r.kind, "reset:")})
		}
	}
	_ = s.SetSetting(ctx, "api_hublog_cursor", strconv.FormatInt(list[len(list)-1].id, 10))
}

// clientStateEvents compares every reseller customer with what was seen
// last time and tells what changed. A customer seen for the first time is
// only remembered.
func clientStateEvents(ctx context.Context, s *Store) {
	list, err := s.clientViews(ctx, "c.rep_id<>?", "")
	if err != nil {
		return
	}
	type state struct {
		status, volMark string
		expMark         int64
	}
	seen := map[string]state{}
	rows, err := s.ro.QueryContext(ctx, `SELECT email,status,expiring_mark,volume_mark FROM api_client_state`)
	if err != nil {
		return
	}
	for rows.Next() {
		var e string
		var st state
		if rows.Scan(&e, &st.status, &st.expMark, &st.volMark) == nil {
			seen[e] = st
		}
	}
	rows.Close()
	now := s.now().Unix()
	for _, c := range list {
		status := pgStatus(c, now)
		old, known := seen[c.Email]
		st := state{status: status, expMark: old.expMark, volMark: old.volMark}
		target := repTarget(c.RepID)
		used := c.Usage.Up + c.Usage.Down
		base := map[string]any{"name": c.Email, "plan": c.Plan, "volume_gb": eventGB(c.VolumeBytes), "used_gb": roundGB(float64(used) / gibF),
			"expires_unix": c.EffectiveUntil}
		if known && old.status != status {
			switch {
			case old.status == "on_hold" && status == "active":
				s.emitAPIEvent(ctx, target, "client.first_used", base)
			case status == "expired":
				s.emitAPIEvent(ctx, target, "client.expired", base)
			case status == "limited":
				s.emitAPIEvent(ctx, target, "client.volume_exhausted", base)
			}
		}
		if status == "active" && c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400 && old.expMark != c.EffectiveUntil {
			if known {
				ev := map[string]any{"days_left": (c.EffectiveUntil - now + 86399) / 86400}
				for k, v := range base {
					ev[k] = v
				}
				s.emitAPIEvent(ctx, target, "client.expiring", ev)
			}
			st.expMark = c.EffectiveUntil
		}
		if mark := fmt.Sprintf("%d:%d", c.VolumeBytes, c.LastReset); status == "active" && c.VolumeBytes > 0 && used*10 >= c.VolumeBytes*9 && old.volMark != mark {
			if known {
				ev := map[string]any{"left_gb": roundGB(float64(c.VolumeBytes-used) / gibF)}
				for k, v := range base {
					ev[k] = v
				}
				s.emitAPIEvent(ctx, target, "client.volume_low", ev)
			}
			st.volMark = mark
		}
		if !known || st != old {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO api_client_state(email,rep_id,status,expiring_mark,volume_mark) VALUES(?,?,?,?,?)
ON CONFLICT(email) DO UPDATE SET rep_id=excluded.rep_id,status=excluded.status,expiring_mark=excluded.expiring_mark,volume_mark=excluded.volume_mark`,
				c.Email, c.RepID, st.status, st.expMark, st.volMark)
		}
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM api_client_state WHERE email NOT IN (SELECT email FROM clients WHERE rep_id<>'')`)
}
