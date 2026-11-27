package hubcore

import (
	"context"
	"math"
)

// Credit forecast for resellers: how fast a reseller spends its volume
// credit (what its new clients and renewals took, less what came back, over
// the last week) and so how many days the credit lasts, with the owner's
// top-up package that covers about a month at that pace. The low-credit
// warning and the daily summary carry it, and a reseller whose credit lasts
// three days or less is warned even above its GB threshold.

// CreditForecast is a reseller's spending pace and what it means.
type CreditForecast struct {
	DailyBytes int64       `json:"daily_bytes"` // net credit spent per day (last 7 days)
	DaysLeft   float64     `json:"days_left"`   // -1 = not spending
	Package    *RepPackage `json:"package,omitempty"`
}

const (
	forecastWindowDays = 7
	forecastWarnDays   = 3 // warn when the credit lasts this long or less
	forecastClearDays  = 5 // ...and again only after it went back over this
	forecastCoverDays  = 30
)

// RepForecast reads a reseller's pace. credit is its current volume credit.
func (s *Store) RepForecast(ctx context.Context, repID string, credit int64) CreditForecast {
	f := CreditForecast{DaysLeft: -1}
	now := s.now().Unix()
	from := now - forecastWindowDays*86400
	var created int64
	var parent string
	_ = s.ro.QueryRowContext(ctx, `SELECT created_unix,parent_id FROM reps WHERE id=?`, repID).Scan(&created, &parent)
	days := float64(forecastWindowDays)
	if created > from {
		days = math.Max(1, float64(now-created)/86400)
	}
	var spent, back int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN kind='charge' THEN -volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='refund' THEN volume_delta END),0) FROM credit_ledger WHERE rep_id=? AND unix>=?`, repID, from).Scan(&spent, &back)
	net := spent - back
	if net <= 0 {
		return f
	}
	f.DailyBytes = int64(float64(net) / days)
	if f.DailyBytes <= 0 {
		return f
	}
	f.DaysLeft = math.Max(0, float64(credit)/float64(f.DailyBytes))
	// Top-ups are sold by the owner to its own resellers (repsale.go); a
	// sub-seller tops up from its parent.
	if parent == "" {
		if rs := s.RepSale(ctx); rs.Enabled && len(rs.Packages) > 0 {
			f.Package = pickPackage(rs.Packages, f.DailyBytes*forecastCoverDays)
		}
	}
	return f
}

// pickPackage is the smallest package that covers need bytes, or the
// biggest one when none does.
func pickPackage(pkgs []RepPackage, need int64) *RepPackage {
	var best, biggest *RepPackage
	for i := range pkgs {
		p := &pkgs[i]
		if biggest == nil || p.GB > biggest.GB {
			biggest = p
		}
		if p.GB<<30 >= need && (best == nil || p.GB < best.GB) {
			best = p
		}
	}
	if best == nil {
		best = biggest
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

// RepRunningOut is a reseller whose credit lasts only a few more days.
type RepRunningOut struct {
	RepID    string
	Name     string
	Credit   int64
	Forecast CreditForecast
}

// DueRunningOut returns the reseller once when its credit falls to
// forecastWarnDays days at its pace (and marks it); it is forgotten when the
// credit lasts more than forecastClearDays again. ok=false: nothing due.
func (s *Store) DueRunningOut(ctx context.Context, repID string) (RepRunningOut, bool) {
	reps, err := s.Reps(ctx)
	if err != nil {
		return RepRunningOut{}, false
	}
	for _, r := range reps {
		if r.ID != repID || !r.Enabled {
			continue
		}
		f := s.RepForecast(ctx, r.ID, r.VolumeCredit)
		key := "rep_days_told:" + r.ID
		told := s.Setting(ctx, key) == "1"
		switch {
		case f.DaysLeft >= 0 && f.DaysLeft <= forecastWarnDays:
			if !told {
				_ = s.SetSetting(ctx, key, "1")
				return RepRunningOut{RepID: r.ID, Name: r.Name, Credit: r.VolumeCredit, Forecast: f}, true
			}
		case told && (f.DaysLeft < 0 || f.DaysLeft > forecastClearDays):
			_ = s.SetSetting(ctx, key, "")
		}
	}
	return RepRunningOut{}, false
}
