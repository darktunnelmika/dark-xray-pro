package hubcore

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Reseller alerts: a low-credit warning (to the reseller and the owner,
// once each time the credit falls under the reseller's threshold) and a
// daily summary for the reseller. The bots deliver them; the store decides
// what is due and remembers what was sent.

const defaultLowCreditGB = 50

// RepAlertGB is a reseller's low-credit threshold in GB (0 = off).
func (s *Store) RepAlertGB(ctx context.Context, repID string) int64 {
	v := s.Setting(ctx, "rep_low_gb:"+repID)
	if v == "" {
		return defaultLowCreditGB
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return max(n, 0)
}

func (s *Store) SetRepAlertGB(ctx context.Context, repID string, gb int64) error {
	if gb < 0 || gb > 1_000_000 {
		return ErrInvalid
	}
	var n int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, repID).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	return s.SetSetting(ctx, "rep_low_gb:"+repID, strconv.FormatInt(gb, 10))
}

// RepLowCredit is one reseller whose credit just fell under its threshold.
type RepLowCredit struct {
	RepID  string
	Name   string
	Credit int64
	Limit  int64 // bytes
}

// DueLowCredit returns resellers newly under their threshold for one
// audience ("rep" or "owner") and marks them as told. A reseller back over
// its threshold is forgotten, so the next fall warns again.
// repID "" checks every reseller.
func (s *Store) DueLowCredit(ctx context.Context, audience, repID string) []RepLowCredit {
	reps, err := s.Reps(ctx)
	if err != nil {
		return nil
	}
	var out []RepLowCredit
	for _, r := range reps {
		if repID != "" && r.ID != repID {
			continue
		}
		key := "rep_low_told:" + audience + ":" + r.ID
		limit := s.RepAlertGB(ctx, r.ID) << 30
		told := s.Setting(ctx, key) == "1"
		switch {
		case limit > 0 && r.Enabled && r.VolumeCredit < limit:
			if !told {
				out = append(out, RepLowCredit{RepID: r.ID, Name: r.Name, Credit: r.VolumeCredit, Limit: limit})
				_ = s.SetSetting(ctx, key, "1")
			}
		case told:
			_ = s.SetSetting(ctx, key, "")
		}
	}
	return out
}

// RepSummary is a reseller's last 24 hours.
type RepSummary struct {
	RepID    string
	Name     string
	Credit   int64
	Sold     int64
	Sales    int
	Back     int64
	Expiring int
	Ended    int
	Active   int
}

// DueRepSummary returns the reseller's daily summary once a day after
// 09:00 Tehran time (ok=false when not due or already sent today).
func (s *Store) DueRepSummary(ctx context.Context, repID string) (RepSummary, bool) {
	now := s.now().UTC()
	if now.Hour() < 5 || now.Hour() == 5 && now.Minute() < 30 {
		return RepSummary{}, false
	}
	day := now.Format("2006-01-02")
	key := "rep_daily:" + repID
	if s.Setting(ctx, key) == day {
		return RepSummary{}, false
	}
	list, err := s.RepOverviews(ctx, repID, false)
	if err != nil || len(list) == 0 {
		return RepSummary{}, false
	}
	o := list[0]
	sum := RepSummary{RepID: repID, Name: o.Name, Credit: o.VolumeCredit, Expiring: o.Expiring, Ended: o.Ended, Active: o.Active}
	since := s.now().Add(-24 * time.Hour).Unix()
	_ = s.ro.QueryRowContext(ctx, `SELECT
COALESCE(SUM(CASE WHEN kind='charge' THEN -volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='charge' THEN 1 END),0),
COALESCE(SUM(CASE WHEN kind='refund' THEN volume_delta END),0)
FROM credit_ledger WHERE rep_id=? AND unix>=?`, repID, since).Scan(&sum.Sold, &sum.Sales, &sum.Back)
	_ = s.SetSetting(ctx, key, day)
	return sum, true
}

// GBText is a whole-GB figure for messages ("4,130 GB").
func GBText(b int64) string {
	n := (b + 1<<29) >> 30
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return fmt.Sprintf("%s GB", s)
}
