package hubcore

import (
	"context"
	"fmt"
)

// Daily cap: an optional most traffic a client may use in one day, so one
// customer cannot burn the whole volume at once. It is enforced through the
// limits the nodes already get: a capped client's RemainingBytes is never
// more than what is left of today's cap, so a node blocks it when the day's
// share is used and lets it in again when the next day starts. Days follow
// the usage records (UTC; the new day starts at 03:30 Tehran time).

func (s *Store) ensureDailyCapSchema() error {
	return s.addColumn("clients", "daily_cap_bytes", "INTEGER NOT NULL DEFAULT 0")
}

// MaxDailyCapGB bounds the daily cap (GB).
const MaxDailyCapGB = 10000

// usageDay is today's key in usage_daily.
func (s *Store) usageDay() string { return s.now().UTC().Format("2006-01-02") }

// SetDailyCap sets a client's daily cap in bytes (0 removes it).
func (s *Store) SetDailyCap(ctx context.Context, email string, bytes int64) error {
	if bytes < 0 || bytes > MaxDailyCapGB<<30 {
		return fmt.Errorf("%w: daily cap: 0 to %d GB", ErrInvalid, MaxDailyCapGB)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET daily_cap_bytes=? WHERE email=?`, bytes, email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DailyCap is a client's daily cap and what it used today.
type DailyCap struct {
	CapBytes  int64 `json:"cap_bytes"`  // 0 = no cap
	UsedToday int64 `json:"used_today"` // bytes
	Reached   bool  `json:"reached"`
}

func (s *Store) DailyCapOf(ctx context.Context, email string) (DailyCap, error) {
	var d DailyCap
	if err := s.ro.QueryRowContext(ctx, `SELECT daily_cap_bytes FROM clients WHERE email=?`, email).Scan(&d.CapBytes); err != nil {
		return d, ErrNotFound
	}
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(up+down),0) FROM usage_daily WHERE email=? AND day=?`, email, s.usageDay()).Scan(&d.UsedToday)
	d.Reached = d.CapBytes > 0 && d.UsedToday >= d.CapBytes
	return d, nil
}

// capRemaining lowers a client's remaining bytes to what is left of today's
// cap (-1 = unlimited stays unlimited only without a cap).
func capRemaining(remaining, capBytes, usedToday int64) int64 {
	if capBytes <= 0 {
		return remaining
	}
	left := capBytes - usedToday
	if left < 0 {
		left = 0
	}
	if remaining < 0 || left < remaining {
		return left
	}
	return remaining
}
