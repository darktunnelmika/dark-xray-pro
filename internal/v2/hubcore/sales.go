package hubcore

import (
	"context"
	"time"
)

// Sales report: what each reseller sold in a period next to the period
// before it, split into new services and renewals, with the shop's toman
// orders and a day-by-day series. Days follow Tehran time (UTC+03:30).

// SalesRep is one reseller's sales in the period.
type SalesRep struct {
	RepID      string `json:"rep_id"`
	Name       string `json:"name"`
	Parent     string `json:"parent"`
	New        int64  `json:"new"`
	Renew      int64  `json:"renew"`
	SoldBytes  int64  `json:"sold_bytes"`
	SoldUnits  int64  `json:"sold_units"`
	Refund     int64  `json:"refund_bytes"`
	Net        int64  `json:"net_bytes"`
	PrevNet    int64  `json:"prev_net_bytes"`
	PrevCount  int64  `json:"prev_count"`
	ShopOrders int64  `json:"shop_orders"`
	ShopToman  int64  `json:"shop_toman"`
	Revenue    int64  `json:"revenue"` // net volume and units at the reseller's prices
	Clients    int64  `json:"clients"`
}

// SalesDay is one day's sales across resellers.
type SalesDay struct {
	Day   string `json:"day"`
	Bytes int64  `json:"bytes"`
	Count int64  `json:"count"`
}

// SalesReport is the whole report.
type SalesReport struct {
	Days     int        `json:"days"`
	From     int64      `json:"from"`
	To       int64      `json:"to"`
	Reps     []SalesRep `json:"reps"`
	Daily    []SalesDay `json:"daily"`
	OwnerNew int64      `json:"owner_shop_orders"` // the owner's own shop orders
	OwnerTmn int64      `json:"owner_shop_toman"`
}

const tehranOffset = 3*3600 + 30*60

// SalesReport covers the last days (1–365) up to now.
func (s *Store) SalesReport(ctx context.Context, days int) (SalesReport, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	now := s.now().Unix()
	// Periods start at Tehran midnight: "1 day" = today so far.
	today := (now+tehranOffset)/86400*86400 - tehranOffset
	from := today - int64(days-1)*86400
	prev := from - int64(days)*86400
	r := SalesReport{Days: days, From: from, To: now, Reps: []SalesRep{}, Daily: []SalesDay{}}
	rows, err := s.ro.QueryContext(ctx, `SELECT r.id,r.name,r.parent_id,r.price_gb,r.price_unit,
(SELECT COUNT(*) FROM clients c WHERE c.rep_id=r.id),
COALESCE(SUM(CASE WHEN l.unix>=? AND l.kind='charge' AND l.note NOT LIKE 'renew%' THEN 1 END),0),
COALESCE(SUM(CASE WHEN l.unix>=? AND l.kind='charge' AND l.note LIKE 'renew%' THEN 1 END),0),
COALESCE(SUM(CASE WHEN l.unix>=? AND l.kind='charge' THEN -l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.unix>=? AND l.kind='charge' THEN -l.units_delta END),0),
COALESCE(SUM(CASE WHEN l.unix>=? AND l.kind='refund' THEN l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.unix>=? AND l.kind='refund' THEN l.units_delta END),0),
COALESCE(SUM(CASE WHEN l.unix<? AND l.kind='charge' THEN -l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.unix<? AND l.kind='refund' THEN l.volume_delta END),0),
COALESCE(SUM(CASE WHEN l.unix<? AND l.kind='charge' THEN 1 END),0)
FROM reps r LEFT JOIN credit_ledger l ON l.rep_id=r.id AND l.unix>=?
GROUP BY r.id ORDER BY r.id`, from, from, from, from, from, from, from, from, from, prev)
	if err != nil {
		return r, err
	}
	idx := map[string]int{}
	for rows.Next() {
		var x SalesRep
		var priceGB, priceUnit, refundUnits, prevSold, prevRefund int64
		if err := rows.Scan(&x.RepID, &x.Name, &x.Parent, &priceGB, &priceUnit, &x.Clients, &x.New, &x.Renew, &x.SoldBytes, &x.SoldUnits,
			&x.Refund, &refundUnits, &prevSold, &prevRefund, &x.PrevCount); err != nil {
			rows.Close()
			return r, err
		}
		x.Net = x.SoldBytes - x.Refund
		x.PrevNet = prevSold - prevRefund
		x.Revenue = int64(float64(x.Net)/(1<<30)*float64(priceGB)+0.5) + (x.SoldUnits-refundUnits)*priceUnit
		idx[x.RepID] = len(r.Reps)
		r.Reps = append(r.Reps, x)
	}
	rows.Close()
	orders, err := s.ro.QueryContext(ctx, `SELECT seller,COUNT(*),COALESCE(SUM(amount),0) FROM orders
WHERE status='fulfilled' AND created_unix>=? GROUP BY seller`, from)
	if err != nil {
		return r, err
	}
	for orders.Next() {
		var seller string
		var n, amt int64
		if err := orders.Scan(&seller, &n, &amt); err != nil {
			orders.Close()
			return r, err
		}
		if seller == "" {
			r.OwnerNew, r.OwnerTmn = n, amt
		} else if i, ok := idx[seller]; ok {
			r.Reps[i].ShopOrders, r.Reps[i].ShopToman = n, amt
		}
	}
	orders.Close()
	daily, err := s.ro.QueryContext(ctx, `SELECT (unix+?)/86400,COALESCE(SUM(-volume_delta),0),COUNT(*) FROM credit_ledger
WHERE kind='charge' AND unix>=? GROUP BY 1`, tehranOffset, from)
	if err != nil {
		return r, err
	}
	got := map[int64]SalesDay{}
	for daily.Next() {
		var d int64
		var x SalesDay
		if err := daily.Scan(&d, &x.Bytes, &x.Count); err != nil {
			daily.Close()
			return r, err
		}
		got[d] = x
	}
	daily.Close()
	for i := 0; i < days; i++ {
		d := (from+tehranOffset)/86400 + int64(i)
		x := got[d]
		x.Day = time.Unix(d*86400, 0).UTC().Format("2006-01-02")
		r.Daily = append(r.Daily, x)
	}
	return r, nil
}
