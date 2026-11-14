package hubcore

import (
	"context"
	"fmt"
	"time"
)

// Limits for a whole inbound, like 3x-ui's inbound total and expiry: once
// its traffic since the cap was set reaches the cap, or its end date
// passes, the inbound is turned off and the owners are told.

func (s *Store) ensureInboundLimitColumns() error {
	for _, c := range []struct{ col, def string }{
		{"cap_bytes", "INTEGER NOT NULL DEFAULT 0"}, {"cap_base", "INTEGER NOT NULL DEFAULT 0"}, {"ends_unix", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumn("inbounds", c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) inboundTotal(ctx context.Context, tag string) int64 {
	var n int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(up+down),0) FROM inbound_traffic WHERE tag IN (?, ?||'__t')`, tag, tag).Scan(&n)
	return n
}

// SetInboundLimits sets a traffic cap (counted from now) and an end time
// (0 = none for either).
func (s *Store) SetInboundLimits(ctx context.Context, tag string, capBytes, ends int64) error {
	if capBytes < 0 || ends < 0 {
		return ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE inbounds SET cap_bytes=?, cap_base=?, ends_unix=? WHERE tag=?`, capBytes, s.inboundTotal(ctx, tag), ends, tag)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// checkInboundLimits turns off inbounds past their cap or end time.
func (s *Store) checkInboundLimits(ctx context.Context) {
	rows, err := s.ro.QueryContext(ctx, `SELECT tag,cap_bytes,cap_base,ends_unix FROM inbounds WHERE enabled=1 AND (cap_bytes>0 OR ends_unix>0)`)
	if err != nil {
		return
	}
	type lim struct {
		tag             string
		capB, base, end int64
	}
	var list []lim
	for rows.Next() {
		var l lim
		if rows.Scan(&l.tag, &l.capB, &l.base, &l.end) == nil {
			list = append(list, l)
		}
	}
	rows.Close()
	now := s.now().Unix()
	for _, l := range list {
		why := ""
		if l.capB > 0 && s.inboundTotal(ctx, l.tag)-l.base >= l.capB {
			why = fmt.Sprintf("به سقف ترافیک %s رسید", humanBytes(uint64(l.capB)))
		} else if l.end > 0 && now >= l.end {
			why = "تاریخ پایانش رسید"
		}
		if why == "" {
			continue
		}
		if err := s.SetInboundEnabled(ctx, l.tag, false); err == nil {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, now, "", l.tag, "inbound:off")
			notifyOwners(ctx, "⏹ اینباند "+l.tag+" "+why+" و خاموش شد. برای روشن کردن دوباره، سقف یا تاریخ را در تنظیمات اینباند عوض کنید.")
		}
	}
}

// RunInboundLimits checks inbound caps every minute.
func (s *Store) RunInboundLimits(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.checkInboundLimits(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
