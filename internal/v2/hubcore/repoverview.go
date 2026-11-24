package hubcore

import (
	"context"
	"time"
)

// RepOverview is one reseller's dashboard: credit, clients by state, this
// month's movement and the last 30 days, day by day.
type RepOverview struct {
	RepID          string        `json:"rep_id"`
	Name           string        `json:"name"`
	VolumeCredit   int64         `json:"volume_credit"`
	UnlimitedUnits int64         `json:"unlimited_units"`
	UsedBytes      int64         `json:"used_bytes"` // lifetime traffic of the reseller's users
	UsedHere       int64         `json:"used_here"`  // the same, on this panel only
	Clients        int           `json:"clients"`
	Active         int           `json:"active"`
	Expiring       int           `json:"expiring"` // ends within 3 days or under 10% volume left
	Ended          int           `json:"ended"`    // expired or out of volume
	Disabled       int           `json:"disabled"`
	Online         int           `json:"online"`
	MonthSold      int64         `json:"month_sold"`     // volume charged this month
	MonthBack      int64         `json:"month_back"`     // volume returned this month (expiry, deletes)
	MonthTopup     int64         `json:"month_topup"`    // credit added by the owner this month
	MonthSales     int           `json:"month_sales"`    // charges this month
	LastLogin      int64         `json:"last_login"`     // newest panel sign-in of its logins (0 = never)
	Days           []RepDay      `json:"days,omitempty"` // oldest first
	Recent         []LedgerEntry `json:"recent,omitempty"`
}

type RepDay struct {
	Day     string `json:"day"`
	Sold    int64  `json:"sold"`    // volume charged
	Back    int64  `json:"back"`    // volume returned
	Traffic int64  `json:"traffic"` // the reseller's users' traffic
}

// expiringSoon reports a live client close to its end.
func expiringSoon(c ClientView, now int64) bool {
	if c.EffectiveUntil > 0 && c.EffectiveUntil-now <= 3*86400 {
		return true
	}
	return c.VolumeBytes > 0 && c.Remaining*10 <= c.VolumeBytes
}

func clientEnded(c ClientView, now int64) bool {
	return (c.EffectiveUntil > 0 && c.EffectiveUntil <= now) || (c.VolumeBytes >= 0 && c.Remaining == 0)
}

func monthStart(t time.Time) int64 {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location()).Unix()
}

// RepOverviews summarises every reseller (detail=false) or one with its
// daily series and recent ledger (detail=true, repID set).
func (s *Store) RepOverviews(ctx context.Context, repID string, detail bool) ([]RepOverview, error) {
	reps, err := s.Reps(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	nowU := now.Unix()
	month := monthStart(now)
	clients, err := s.Clients(ctx)
	if err != nil {
		return nil, err
	}
	var out []RepOverview
	for _, r := range reps {
		if repID != "" && r.ID != repID {
			continue
		}
		o := RepOverview{RepID: r.ID, Name: r.Name, VolumeCredit: r.VolumeCredit, UnlimitedUnits: r.UnlimitedUnits, UsedBytes: r.UsedBytes, UsedHere: r.UsedHere}
		for _, c := range clients {
			if c.RepID != r.ID {
				continue
			}
			o.Clients++
			switch {
			case !c.Enabled:
				o.Disabled++
			case clientEnded(c, nowU):
				o.Ended++
			default:
				o.Active++
				if expiringSoon(c, nowU) {
					o.Expiring++
				}
			}
			if c.Online {
				o.Online++
			}
		}
		_ = s.ro.QueryRowContext(ctx, `SELECT
COALESCE(SUM(CASE WHEN kind='charge' THEN -volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='refund' THEN volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='adjust' AND actor<>'old panel' AND actor<>'import' AND volume_delta>0 THEN volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='charge' THEN 1 END),0)
FROM credit_ledger WHERE rep_id=? AND unix>=?`, r.ID, month).Scan(&o.MonthSold, &o.MonthBack, &o.MonthTopup, &o.MonthSales)
		_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(MAX(a.unix),0) FROM audit_log a JOIN admins d ON d.username=a.actor
WHERE d.rep_id=? AND a.path='/api/v2/auth/login' AND a.status=200`, r.ID).Scan(&o.LastLogin)
		if detail {
			o.Days = s.repDays(ctx, r.ID, now, 30)
			o.Recent, _ = s.Ledger(ctx, r.ID, 0, 10)
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Store) repDays(ctx context.Context, repID string, now time.Time, days int) []RepDay {
	today := now.UTC()
	from := today.AddDate(0, 0, -(days - 1))
	fromDay := from.Format("2006-01-02")
	fromUnix := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC).Unix()
	got := map[string]*RepDay{}
	at := func(day string) *RepDay {
		if d := got[day]; d != nil {
			return d
		}
		d := &RepDay{Day: day}
		got[day] = d
		return d
	}
	if rows, err := s.ro.QueryContext(ctx, `SELECT unix,kind,volume_delta FROM credit_ledger WHERE rep_id=? AND unix>=? AND kind IN ('charge','refund')`, repID, fromUnix); err == nil {
		for rows.Next() {
			var u, v int64
			var k string
			if rows.Scan(&u, &k, &v) == nil {
				d := at(time.Unix(u, 0).UTC().Format("2006-01-02"))
				if k == "charge" {
					d.Sold += -v
				} else {
					d.Back += v
				}
			}
		}
		rows.Close()
	}
	if rows, err := s.ro.QueryContext(ctx, `SELECT d.day,SUM(d.up+d.down) FROM usage_daily d JOIN clients c ON c.email=d.email
WHERE c.rep_id=? AND d.day>=? GROUP BY d.day`, repID, fromDay); err == nil {
		for rows.Next() {
			var day string
			var b int64
			if rows.Scan(&day, &b) == nil {
				at(day).Traffic = b
			}
		}
		rows.Close()
	}
	out := make([]RepDay, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		d := RepDay{Day: day}
		if g := got[day]; g != nil {
			d = *g
		}
		out = append(out, d)
	}
	return out
}
