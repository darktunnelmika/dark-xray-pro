package hubcore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Business analytics: who is active, who renews, who is about to lapse,
// who left, what sells and what it earns. The owner sees everything (or
// one seller); a reseller sees its own customers and its sub-sellers'.
// Days and months follow Tehran time.

// Analytics is the whole report.
type Analytics struct {
	Scope        string           `json:"scope"`
	Active       int64            `json:"active"`
	Total        int64            `json:"total"`
	Expiring7d   int64            `json:"expiring_7d"`
	RenewRate    float64          `json:"renew_rate"` // 0–1, last 30 days; -1 = nothing ended
	Renewed30d   int64            `json:"renewed_30d"`
	Lapsed30d    int64            `json:"lapsed_30d"`
	RevenueMonth int64            `json:"revenue_month"` // toman, shop + reseller sales this month
	RevenuePrev  int64            `json:"revenue_prev"`
	Months       []AMonth         `json:"months"` // last 6, oldest first
	Daily        []ADay           `json:"daily"`  // active customers, last 30 days
	Products     []AProduct       `json:"products"`
	AtRisk       []ARisk          `json:"at_risk"`
	Sellers      []ASellerRenewal `json:"sellers"`
}

type AMonth struct {
	Month   string `json:"month"` // YYYY-MM (Tehran)
	Shop    int64  `json:"shop"`  // toman of fulfilled shop orders
	Resell  int64  `json:"resell"`
	New     int64  `json:"new"`
	Renewed int64  `json:"renewed"`
	Churned int64  `json:"churned"` // ended in that month and not renewed for 7+ days
}

type ADay struct {
	Day    string `json:"day"`
	Active int64  `json:"active"`
}

type AProduct struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Toman int64  `json:"toman"`
}

type ARisk struct {
	Email     string  `json:"email"`
	Seller    string  `json:"seller"`
	Expires   int64   `json:"expires"`
	LeftBytes int64   `json:"left_bytes"` // -1 = unlimited
	LeftShare float64 `json:"left_share"` // remaining volume 0–1, -1 = unlimited
	Reason    string  `json:"reason"`     // expiring | volume
	Telegram  bool    `json:"telegram"`
	Reminded  int64   `json:"reminded"` // last reminder, unix
}

type ASellerRenewal struct {
	Seller  string  `json:"seller"`
	Name    string  `json:"name"`
	Renewed int64   `json:"renewed"`
	Lapsed  int64   `json:"lapsed"`
	Rate    float64 `json:"rate"`
}

func tehranMonth(unix int64) string {
	return time.Unix(unix+tehranOffset, 0).UTC().Format("2006-01")
}

// analyticsScope returns the SQL condition on clients (alias c) and the
// sellers it covers.
func (s *Store) analyticsScope(ctx context.Context, by Principal, seller string) (string, []any, error) {
	switch {
	case by.Role == "rep":
		return `(c.rep_id=? OR c.rep_id IN (SELECT id FROM reps WHERE parent_id=? AND parent_id<>''))`, []any{by.RepID, by.RepID}, nil
	case by.Role != "owner":
		return "", nil, ErrAuth
	case seller == "":
		return "1=1", nil, nil
	case seller == "-":
		return "c.rep_id=''", nil, nil
	default:
		return "c.rep_id=?", []any{seller}, nil
	}
}

// Analytics builds the report for by ("" seller = all, "-" = the owner's own).
func (s *Store) Analytics(ctx context.Context, by Principal, seller string) (Analytics, error) {
	cond, args, err := s.analyticsScope(ctx, by, seller)
	if err != nil {
		return Analytics{}, err
	}
	cond = "(" + cond + ") AND c.trial=0" // free trials are not customers
	now := s.now().Unix()
	a := Analytics{Scope: seller, RenewRate: -1, Months: []AMonth{}, Daily: []ADay{}, Products: []AProduct{}, AtRisk: []ARisk{}, Sellers: []ASellerRenewal{}}
	if by.Role == "rep" {
		a.Scope = by.RepID
	}
	q := func(sql string, extra ...any) *rowScanner {
		return &rowScanner{s: s, ctx: ctx, sql: sql, args: append(append([]any{}, extra...), args...)}
	}
	used := `COALESCE((SELECT up+down FROM client_usage u WHERE u.email=c.email),0)`
	live := `c.enabled=1 AND (c.expires_unix=0 OR c.expires_unix>?) AND (c.volume_bytes<=0 OR ` + used + `<c.volume_bytes)`
	_ = q(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN `+live+` THEN 1 END),0) FROM clients c WHERE `+cond, now).one(&a.Total, &a.Active)

	// Renewals: a renew reset, or a fulfilled renewal order, in the window.
	renewedIn := func(from, to int64) string {
		return fmt.Sprintf(`(EXISTS (SELECT 1 FROM usage_resets r WHERE r.email=c.email AND r.reason='renew' AND r.unix>=%d AND r.unix<%d)
 OR EXISTS (SELECT 1 FROM orders o WHERE o.client_email=c.email AND o.renew=1 AND o.status='fulfilled' AND o.done_unix>=%d AND o.done_unix<%d))`, from, to, from, to)
	}
	from30 := now - 30*86400
	_ = q(`SELECT COUNT(*) FROM clients c WHERE ` + renewedIn(from30, now+1) + ` AND ` + cond).one(&a.Renewed30d)
	// Lapsed: ended in the window, more than 3 days ago, and not renewed since.
	_ = q(`SELECT COUNT(*) FROM clients c WHERE c.expires_unix>=? AND c.expires_unix<? AND NOT `+renewedIn(from30, now+1)+` AND `+cond, from30, now-3*86400).one(&a.Lapsed30d)
	if a.Renewed30d+a.Lapsed30d > 0 {
		a.RenewRate = float64(a.Renewed30d) / float64(a.Renewed30d+a.Lapsed30d)
	}
	_ = q(`SELECT COUNT(*) FROM clients c WHERE c.enabled=1 AND c.expires_unix>? AND c.expires_unix<=? AND `+cond, now, now+7*86400).one(&a.Expiring7d)

	// Months: shop toman, reseller sales, new and renewed services, churn.
	monthStart := func(back int) int64 {
		t := time.Unix(now+tehranOffset, 0).UTC()
		m := time.Date(t.Year(), t.Month()-time.Month(back), 1, 0, 0, 0, 0, time.UTC)
		return m.Unix() - tehranOffset
	}
	for back := 5; back >= 0; back-- {
		from, to := monthStart(back), monthStart(back-1)
		m := AMonth{Month: tehranMonth(from)}
		_ = q(`SELECT COALESCE(SUM(o.amount),0) FROM orders o JOIN clients c ON c.email=o.client_email WHERE o.status='fulfilled' AND o.done_unix>=? AND o.done_unix<? AND `+cond, from, to).one(&m.Shop)
		if by.Role == "owner" {
			_ = q(`SELECT COALESCE(SUM(CAST(-l.volume_delta/1073741824.0*r.price_gb AS INTEGER) + -l.units_delta*r.price_unit),0)
FROM credit_ledger l JOIN reps r ON r.id=l.rep_id LEFT JOIN clients c ON c.email=`+ledgerClient+`
WHERE l.kind IN ('charge','refund') AND l.unix>=? AND l.unix<? AND (c.email IS NULL AND ?='1=1' OR c.email IS NOT NULL AND `+cond+`)`, from, to, cond).one(&m.Resell)
		}
		_ = q(`SELECT COUNT(*) FROM clients c WHERE c.created_unix>=? AND c.created_unix<? AND `+cond, from, to).one(&m.New)
		_ = q(`SELECT COUNT(*) FROM clients c WHERE ` + renewedIn(from, to) + ` AND ` + cond).one(&m.Renewed)
		_ = q(`SELECT COUNT(*) FROM clients c WHERE c.expires_unix>=? AND c.expires_unix<? AND c.expires_unix<? AND NOT `+renewedIn(from, now+1)+` AND `+cond, from, to, now-7*86400).one(&m.Churned)
		a.Months = append(a.Months, m)
	}
	cur := a.Months[len(a.Months)-1]
	prev := a.Months[len(a.Months)-2]
	a.RevenueMonth, a.RevenuePrev = cur.Shop+cur.Resell, prev.Shop+prev.Resell

	// Active customers per day (any traffic that day), last 30 days.
	got := map[string]int64{}
	rs := q(`SELECT d.day, COUNT(DISTINCT d.email) FROM usage_daily d JOIN clients c ON c.email=d.email WHERE d.day>=? AND `+cond+` GROUP BY d.day`,
		time.Unix(now-29*86400, 0).UTC().Format("2006-01-02"))
	_ = rs.each(func(sc func(...any) error) error {
		var d string
		var n int64
		if err := sc(&d, &n); err != nil {
			return err
		}
		got[d] = n
		return nil
	})
	for i := 29; i >= 0; i-- {
		d := time.Unix(now-int64(i)*86400, 0).UTC().Format("2006-01-02")
		a.Daily = append(a.Daily, ADay{Day: d, Active: got[d]})
	}

	// Best sellers, last 30 days.
	_ = q(`SELECT o.product_id, COALESCE(p.name,o.product_id), COUNT(*), COALESCE(SUM(o.amount),0) FROM orders o JOIN clients c ON c.email=o.client_email
LEFT JOIN products p ON p.id=o.product_id WHERE o.status='fulfilled' AND o.done_unix>=? AND `+cond+`
GROUP BY o.product_id ORDER BY COUNT(*) DESC, 4 DESC LIMIT 10`, from30).each(func(sc func(...any) error) error {
		var p AProduct
		if err := sc(&p.ID, &p.Name, &p.Count, &p.Toman); err != nil {
			return err
		}
		a.Products = append(a.Products, p)
		return nil
	})

	// At risk: ends within 7 days, or under 15% volume left.
	_ = q(`SELECT c.email, c.rep_id, c.expires_unix, c.volume_bytes, `+used+`,
EXISTS (SELECT 1 FROM tg_links t WHERE t.email=c.email),
COALESCE((SELECT CAST(value AS INTEGER) FROM settings WHERE key='remind:'||c.email),0)
FROM clients c WHERE c.enabled=1 AND (c.expires_unix=0 OR c.expires_unix>?) AND
 ((c.expires_unix>0 AND c.expires_unix<=?) OR (c.volume_bytes>0 AND `+used+`>=c.volume_bytes*0.85 AND `+used+`<c.volume_bytes)) AND `+cond+`
ORDER BY CASE WHEN c.expires_unix>0 THEN c.expires_unix ELSE 9e18 END LIMIT 300`, now, now+7*86400).each(func(sc func(...any) error) error {
		var r ARisk
		var vol, usedB int64
		if err := sc(&r.Email, &r.Seller, &r.Expires, &vol, &usedB, &r.Telegram, &r.Reminded); err != nil {
			return err
		}
		r.LeftBytes, r.LeftShare = -1, -1
		if vol > 0 {
			r.LeftBytes = max(0, vol-usedB)
			r.LeftShare = float64(r.LeftBytes) / float64(vol)
		}
		r.Reason = "expiring"
		if r.Expires == 0 || r.Expires > now+7*86400 {
			r.Reason = "volume"
		}
		a.AtRisk = append(a.AtRisk, r)
		return nil
	})

	// Renewal rate per seller (owner, all sellers only).
	if by.Role == "owner" && seller == "" {
		_ = q(`SELECT c.rep_id, COALESCE((SELECT name FROM reps WHERE id=c.rep_id),''),
SUM(CASE WHEN `+renewedIn(from30, now+1)+` THEN 1 ELSE 0 END),
SUM(CASE WHEN c.expires_unix>=? AND c.expires_unix<? AND NOT `+renewedIn(from30, now+1)+` THEN 1 ELSE 0 END)
FROM clients c WHERE `+cond+` GROUP BY c.rep_id`, from30, now-3*86400).each(func(sc func(...any) error) error {
			var x ASellerRenewal
			if err := sc(&x.Seller, &x.Name, &x.Renewed, &x.Lapsed); err != nil {
				return err
			}
			if x.Renewed+x.Lapsed == 0 {
				return nil
			}
			x.Rate = float64(x.Renewed) / float64(x.Renewed+x.Lapsed)
			a.Sellers = append(a.Sellers, x)
			return nil
		})
	}
	return a, nil
}

// RemindRenewal records a reminder for a client the caller may see and
// returns the Telegram chats and the seller whose bot should send it.
func (s *Store) RemindRenewal(ctx context.Context, by Principal, email string) (seller string, chats []int64, err error) {
	cond, args, err := s.analyticsScope(ctx, by, "")
	if err != nil {
		return "", nil, err
	}
	var rep string
	if err := s.ro.QueryRowContext(ctx, `SELECT c.rep_id FROM clients c WHERE c.email=? AND `+cond, append([]any{email}, args...)...).Scan(&rep); err != nil {
		return "", nil, ErrNotFound
	}
	chats, _ = s.ClientTelegram(ctx, email)
	if len(chats) == 0 {
		return rep, nil, fmt.Errorf("%w: this customer has no Telegram linked", ErrInvalid)
	}
	last, _ := s.settingInt(ctx, "remind:"+email)
	if now := s.now().Unix(); now-last < 12*3600 {
		return rep, nil, fmt.Errorf("%w: reminded less than 12 hours ago", ErrInvalid)
	}
	_ = s.SetSetting(ctx, "remind:"+email, fmt.Sprint(s.now().Unix()))
	_, err = s.db.ExecContext(ctx, `INSERT INTO tg_outbox(seller,email,created_unix) VALUES(?,?,?)`, rep, email, s.now().Unix())
	return rep, chats, err
}

func (s *Store) settingInt(ctx context.Context, key string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(strings.TrimSpace(s.Setting(ctx, key)), &n)
	return n, err
}

// Renewal reminders wait here for the seller's bot.
const outboxSchema = `CREATE TABLE IF NOT EXISTS tg_outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seller TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL,
  created_unix INTEGER NOT NULL,
  sent INTEGER NOT NULL DEFAULT 0
);`

func (s *Store) ensureOutboxSchema() error {
	_, err := s.db.Exec(outboxSchema)
	return err
}

// Reminder is one renewal reminder for a bot to send.
type Reminder struct {
	ID     int64
	Seller string
	Email  string
	Chats  []int64
}

// PendingReminders lists reminders not sent yet (each bot keeps its own).
func (s *Store) PendingReminders(ctx context.Context) []Reminder {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,seller,email FROM tg_outbox WHERE sent=0 ORDER BY id LIMIT 100`)
	if err != nil {
		return nil
	}
	var out []Reminder
	for rows.Next() {
		var r Reminder
		if rows.Scan(&r.ID, &r.Seller, &r.Email) == nil {
			out = append(out, r)
		}
	}
	rows.Close()
	for i := range out {
		out[i].Chats, _ = s.ClientTelegram(ctx, out[i].Email)
	}
	return out
}

// ReminderSent marks a reminder done.
func (s *Store) ReminderSent(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE tg_outbox SET sent=1 WHERE id=?`, id)
}

// rowScanner keeps the report code short: one query, one or many rows.
type rowScanner struct {
	s    *Store
	ctx  context.Context
	sql  string
	args []any
}

func (r *rowScanner) one(dst ...any) error {
	return r.s.ro.QueryRowContext(r.ctx, r.sql, r.args...).Scan(dst...)
}

func (r *rowScanner) each(f func(scan func(...any) error) error) error {
	rows, err := r.s.ro.QueryContext(r.ctx, r.sql, r.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := f(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}
