package hubcore

import (
	"context"
	"time"
)

// Excel reports for 7, 30 or 90 days: day by day sales, refunds, shop money
// by payment method and traffic; the resellers (or a reseller's
// sub-sellers); a reseller's credit ledger; and the client list. The owner
// gets the whole panel (or one reseller's report), a reseller its own.
// Days follow Tehran time, except traffic, which is kept per UTC day.

func gbf(b int64) float64 { return float64(int64(float64(b)/(1<<30)*100+0.5)) / 100 }

func tehranDayKey(u int64) int64 { return (u + tehranOffset) / 86400 }

var ledgerKindFa = map[string]string{"charge": "فروش/تمدید", "refund": "برگشت", "adjust": "شارژ/اصلاح دستی", "move": "انتقال", "resale": "فروش پنل"}

// ReportWorkbook builds the report. rep "" = the owner's whole panel.
func (s *Store) ReportWorkbook(ctx context.Context, rep string, days int) ([]XSheet, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	now := s.now().Unix()
	today := tehranDayKey(now)*86400 - tehranOffset
	from := today - int64(days-1)*86400

	type day struct {
		sales, sold, back       int64
		cardN, cardT, walT, str int64
		traffic                 int64
	}
	byDay := map[int64]*day{}
	get := func(k int64) *day {
		if byDay[k] == nil {
			byDay[k] = &day{}
		}
		return byDay[k]
	}
	// Credit ledger: what was sold and what came back.
	q := `SELECT unix,kind,volume_delta FROM credit_ledger WHERE unix>=?`
	args := []any{from}
	if rep != "" {
		q += ` AND rep_id=?`
		args = append(args, rep)
	} else {
		q += ` AND substr(rep_id,1,1)<>'~'` // not connected shops'
	}
	rows, err := s.ro.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u, v int64
		var k string
		if rows.Scan(&u, &k, &v) != nil {
			continue
		}
		d := get(tehranDayKey(u))
		switch k {
		case "charge":
			d.sales++
			d.sold += -v
		case "refund":
			d.back += v
		}
	}
	rows.Close()
	// Shop orders delivered, by payment method.
	q = `SELECT created_unix,method,amount,stars FROM orders WHERE status='fulfilled' AND created_unix>=?`
	args = []any{from}
	if rep != "" {
		q += ` AND seller=?`
		args = append(args, rep)
	}
	rows, err = s.ro.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u, amt, stars int64
		var m string
		if rows.Scan(&u, &m, &amt, &stars) != nil {
			continue
		}
		d := get(tehranDayKey(u))
		switch {
		case m == "stars":
			d.str += stars
		case m == "wallet":
			d.walT += amt
		default:
			d.cardN++
			d.cardT += amt
		}
	}
	rows.Close()
	// Traffic (UTC days).
	fromDay := time.Unix(from, 0).UTC().Format("2006-01-02")
	q = `SELECT d.day,SUM(d.up+d.down) FROM usage_daily d WHERE d.day>=?`
	args = []any{fromDay}
	if rep != "" {
		q = `SELECT d.day,SUM(d.up+d.down) FROM usage_daily d JOIN clients c ON c.email=d.email WHERE d.day>=? AND c.rep_id=?`
		args = append(args, rep)
	} else {
		q += ` AND d.email NOT IN (SELECT email FROM clients WHERE substr(rep_id,1,1)='~')` // not connected shops'
	}
	rows, err = s.ro.QueryContext(ctx, q+` GROUP BY d.day`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ds string
		var b int64
		if rows.Scan(&ds, &b) != nil {
			continue
		}
		if t, err := time.Parse("2006-01-02", ds); err == nil {
			get(t.Unix() / 86400).traffic += b // the UTC date counts on the Tehran day of the same date
		}
	}
	rows.Close()

	daily := XSheet{Name: "روزانه", RTL: true, Rows: [][]any{{"تاریخ", "Date", "فروش و تمدید (تعداد)", "حجم فروخته (GB)", "برگشتی به اعتبار (GB)",
		"سفارش کارت‌به‌کارت (تعداد)", "کارت‌به‌کارت (تومان)", "از کیف پول (تومان)", "استارز", "ترافیک مصرفی (GB)"}}}
	var tot day
	for i := 0; i < days; i++ {
		k := tehranDayKey(from) + int64(i)
		d := get(k)
		u := k*86400 - tehranOffset + 3600
		daily.Rows = append(daily.Rows, []any{jalaliDate(u), time.Unix(k*86400, 0).UTC().Format("2006-01-02"), d.sales, gbf(d.sold), gbf(d.back),
			d.cardN, d.cardT, d.walT, d.str, gbf(d.traffic)})
		tot.sales += d.sales
		tot.sold += d.sold
		tot.back += d.back
		tot.cardN += d.cardN
		tot.cardT += d.cardT
		tot.walT += d.walT
		tot.str += d.str
		tot.traffic += d.traffic
	}
	daily.Rows = append(daily.Rows, []any{"جمع", "Total", tot.sales, gbf(tot.sold), gbf(tot.back), tot.cardN, tot.cardT, tot.walT, tot.str, gbf(tot.traffic)})
	out := []XSheet{daily}

	// Resellers (the owner) or the reseller's sub-sellers.
	if sr, err := s.SalesReport(ctx, days); err == nil {
		reps := XSheet{Name: "نماینده‌ها", RTL: true, Rows: [][]any{{"نام", "شناسه", "زیرمجموعهٔ", "کلاینت‌ها", "فروش جدید", "تمدید", "حجم فروخته (GB)",
			"برگشتی (GB)", "خالص (GB)", "خالص دورهٔ قبل (GB)", "سفارش فروشگاه", "مبلغ فروشگاه (تومان)", "درآمد با قیمت فروش (تومان)"}}}
		if rep != "" {
			reps.Name = "زیرفروشنده‌ها"
		}
		for _, x := range sr.Reps {
			if rep != "" && x.Parent != rep {
				continue
			}
			reps.Rows = append(reps.Rows, []any{x.Name, x.RepID, x.Parent, x.Clients, x.New, x.Renew, gbf(x.SoldBytes), gbf(x.Refund), gbf(x.Net), gbf(x.PrevNet),
				x.ShopOrders, x.ShopToman, x.Revenue})
		}
		if len(reps.Rows) > 1 {
			out = append(out, reps)
		}
	}

	// A reseller's credit ledger.
	if rep != "" {
		led := XSheet{Name: "دفتر اعتبار", RTL: true, Rows: [][]any{{"تاریخ", "ساعت", "نوع", "حجم (GB)", "واحد نامحدود", "توسط", "یادداشت"}}}
		rows, err := s.ro.QueryContext(ctx, `SELECT unix,kind,volume_delta,units_delta,actor,note FROM credit_ledger WHERE rep_id=? AND unix>=? ORDER BY id`, rep, from)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var u, v, n int64
			var k, a, note string
			if rows.Scan(&u, &k, &v, &n, &a, &note) != nil {
				continue
			}
			kind := ledgerKindFa[k]
			if kind == "" {
				kind = k
			}
			led.Rows = append(led.Rows, []any{jalaliDate(u), time.Unix(u+tehranOffset, 0).UTC().Format("15:04"), kind, gbf(v), n, a, note})
		}
		rows.Close()
		out = append(out, led)
	}

	// Clients.
	var list []ClientView
	if rep == "" {
		list, err = s.Clients(ctx)
	} else {
		list, err = s.clientViews(ctx, "c.rep_id=?", rep)
	}
	if err != nil {
		return nil, err
	}
	cs := XSheet{Name: "کلاینت‌ها", RTL: true, Rows: [][]any{{"نام", "نماینده", "پلن", "حجم کل (GB)", "مصرف (GB)", "مانده (GB)", "انقضا", "وضعیت", "آخرین اتصال", "یادداشت"}}}
	for _, c := range list {
		total, left := any("نامحدود"), any("نامحدود")
		if c.VolumeBytes >= 0 {
			total, left = gbf(c.VolumeBytes), gbf(c.Remaining)
		}
		exp := "—"
		if c.EffectiveUntil > 0 {
			exp = jalaliDate(c.EffectiveUntil)
		} else if c.DurationSeconds > 0 {
			exp = "از اولین اتصال"
		}
		st := "فعال"
		switch {
		case !c.Enabled:
			st = "خاموش"
		case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
			st = "منقضی"
		case c.VolumeBytes > 0 && c.Remaining == 0:
			st = "حجم تمام"
		}
		seen := "هرگز"
		if c.Usage.LastSeen > 0 {
			seen = jalaliDate(c.Usage.LastSeen)
		}
		cs.Rows = append(cs.Rows, []any{c.Email, c.RepID, c.Plan, total, gbf(c.Usage.Up + c.Usage.Down), left, exp, st, seen, c.Note})
	}
	out = append(out, cs)
	return out, nil
}
