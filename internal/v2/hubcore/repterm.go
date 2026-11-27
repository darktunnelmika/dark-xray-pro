package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Reseller panels with a term (monthly reseller-ship): a panel can run until
// a date, set by the owner, bought with a package that has days (the
// Telegram reseller sale), or made and renewed through the partner API.
//
// When the term ends the panel stops selling, like an overdue bill
// (repStopQ): no new customers, renewals, resets or credit moves, for the
// panel and for the sub-sellers under it. It can still sign in, see its
// customers and renew. What happens to its customers is the panel's choice:
//
//   - "block" (the default): they keep working until their own end;
//   - "suspend": after grace days they are switched off too; the ones
//     switched off this way come back on by themselves when the term is
//     renewed.
//
// Three days and one day before the end the reseller is reminded (its
// Telegram ids that manage the panel, from its own bot); the owner and the
// reseller hear when it ends and when customers are switched off. Every
// step is also an event: term.* for the reseller, panel.* for the partner
// that made the panel.

const repTermSchema = `CREATE TABLE IF NOT EXISTS rep_term_off (
  email TEXT PRIMARY KEY,
  rep_id TEXT NOT NULL,
  unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS rep_term_off_rep ON rep_term_off(rep_id);`

func (s *Store) ensureRepTermSchema() error {
	for _, c := range []struct{ col, def string }{
		{"term_until", "INTEGER NOT NULL DEFAULT 0"},
		{"term_mode", "TEXT NOT NULL DEFAULT 'block'"},
		{"term_grace", "INTEGER NOT NULL DEFAULT 0"},
		{"term_told", "INTEGER NOT NULL DEFAULT 0"}, // 1: 3 days left, 2: 1 day, 3: ended, 4: customers off
	} {
		if err := s.addColumn("reps", c.col, c.def); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(repTermSchema)
	return err
}

func init() { apiEventSources = append(apiEventSources, repTermTick) }

// RepTerm is a panel's term.
type RepTerm struct {
	Until     int64  `json:"expires_unix"` // 0: no term, the panel runs on
	OnExpiry  string `json:"on_expiry"`    // block | suspend
	GraceDays int64  `json:"grace_days"`   // suspend: days after the end before customers are switched off
	State     string `json:"state"`        // none | active | expiring | expired | suspended
	DaysLeft  int64  `json:"days_left"`    // whole days left, 0 when ended or none
}

func checkTermMode(mode string, grace int64) error {
	if mode != "" && mode != "block" && mode != "suspend" {
		return fmt.Errorf("%w: on_expiry is block (customers keep working) or suspend (customers are switched off too)", ErrInvalid)
	}
	if grace < 0 || grace > 365 {
		return fmt.Errorf("%w: grace_days is 0 to 365", ErrInvalid)
	}
	return nil
}

// RepTermOf reads a panel's term.
func (s *Store) RepTermOf(ctx context.Context, rep string) (RepTerm, error) {
	var t RepTerm
	var told int
	err := s.ro.QueryRowContext(ctx, `SELECT term_until,term_mode,term_grace,term_told FROM reps WHERE id=?`, rep).Scan(&t.Until, &t.OnExpiry, &t.GraceDays, &told)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	now := s.now().Unix()
	switch {
	case t.Until == 0:
		t.State = "none"
	case t.Until > now:
		t.State, t.DaysLeft = "active", (t.Until-now)/86400
		if t.Until-now <= 3*86400 {
			t.State = "expiring"
		}
	case told >= 4:
		t.State = "suspended"
	default:
		t.State = "expired"
	}
	return t, nil
}

// SetRepTerm sets a panel's term outright (until 0 = no term). mode ""
// and grace < 0 keep what the panel has.
func (s *Store) SetRepTerm(ctx context.Context, actor, rep string, until int64, mode string, grace int64) (RepTerm, error) {
	if until < 0 {
		return RepTerm{}, fmt.Errorf("%w: expires_unix is 0 (no term) or a Unix time", ErrInvalid)
	}
	return s.changeTerm(ctx, actor, rep, func(old int64) int64 { return until }, mode, grace)
}

// ExtendRepTerm adds days to a panel's term, from its end or from now when
// it has ended (or never had one). mode "" and grace < 0 keep the panel's.
func (s *Store) ExtendRepTerm(ctx context.Context, actor, rep string, days int64, mode string, grace int64) (RepTerm, error) {
	if days < 1 || days > 3650 {
		return RepTerm{}, fmt.Errorf("%w: days is 1 to 3650", ErrInvalid)
	}
	now := s.now().Unix()
	return s.changeTerm(ctx, actor, rep, func(old int64) int64 { return max(old, now) + days*86400 }, mode, grace)
}

func (s *Store) changeTerm(ctx context.Context, actor, rep string, next func(int64) int64, mode string, grace int64) (RepTerm, error) {
	if err := checkTermMode(mode, max(grace, 0)); err != nil {
		return RepTerm{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RepTerm{}, err
	}
	defer tx.Rollback()
	var old int64
	var oldMode string
	var oldGrace int64
	if err := tx.QueryRowContext(ctx, `SELECT term_until,term_mode,term_grace FROM reps WHERE id=?`, rep).Scan(&old, &oldMode, &oldGrace); err != nil {
		return RepTerm{}, ErrNotFound
	}
	if mode == "" {
		mode = oldMode
	}
	if grace < 0 {
		grace = oldGrace
	}
	until := next(old)
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, `UPDATE reps SET term_until=?, term_mode=?, term_grace=?, term_told=0 WHERE id=?`, until, mode, grace, rep); err != nil {
		return RepTerm{}, err
	}
	// Running again (or no longer limited): customers switched off for the
	// term come back on.
	var restored int64
	if until == 0 || until > now {
		r, err := tx.ExecContext(ctx, `UPDATE clients SET enabled=1 WHERE email IN (SELECT email FROM rep_term_off WHERE rep_id=?)`, rep)
		if err != nil {
			return RepTerm{}, err
		}
		restored, _ = r.RowsAffected()
		if _, err := tx.ExecContext(ctx, `DELETE FROM rep_term_off WHERE rep_id=?`, rep); err != nil {
			return RepTerm{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,note) VALUES(?,?,?,?,?)`,
		now, rep, actor, "term", termNote(old, until)); err != nil {
		return RepTerm{}, err
	}
	if err := tx.Commit(); err != nil {
		return RepTerm{}, err
	}
	t, err := s.RepTermOf(ctx, rep)
	if err == nil && (until == 0 || until > now) && until != old {
		data := map[string]any{"expires_unix": until, "previous_unix": old, "on_expiry": t.OnExpiry, "customers_back": restored}
		s.termEvent(ctx, rep, "renewed", data)
	}
	return t, err
}

func termNote(old, until int64) string {
	day := func(u int64) string {
		if u == 0 {
			return "none"
		}
		return time.Unix(u, 0).UTC().Format("2006-01-02")
	}
	return "term: " + day(old) + " → " + day(until)
}

// repStopQ is why a reseller may not spend credit now, or nil: an overdue
// bill, or its term (or its parent's) has ended.
func repStopQ(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, rep string, now int64) error {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_bills WHERE rep_id=? AND status='open' AND due_unix<=?`, rep, now).Scan(&n)
	if n > 0 {
		return fmt.Errorf("%w: an overdue bill is unpaid; pay it to sell again", ErrPolicy)
	}
	var until int64
	var parent string
	if q.QueryRowContext(ctx, `SELECT term_until,parent_id FROM reps WHERE id=?`, rep).Scan(&until, &parent) != nil {
		return nil
	}
	if until > 0 && until <= now {
		return fmt.Errorf("%w: the panel's term ended on %s; renew it to sell again", ErrPolicy, time.Unix(until, 0).UTC().Format("2006-01-02"))
	}
	if parent != "" {
		if q.QueryRowContext(ctx, `SELECT term_until FROM reps WHERE id=?`, parent).Scan(&until) == nil && until > 0 && until <= now {
			return fmt.Errorf("%w: the term of the reseller above this panel ended; it renews to sell again", ErrPolicy)
		}
	}
	return nil
}

// termEvent tells the reseller (term.<what>) and the partner that made the
// panel (panel.<what>).
func (s *Store) termEvent(ctx context.Context, rep, what string, data map[string]any) {
	data["id"] = rep
	s.emitAPIEvent(ctx, repTarget(rep), "term."+what, data)
	var key, ref string
	if s.ro.QueryRowContext(ctx, `SELECT key_id,external_ref FROM partner_reps WHERE rep_id=?`, rep).Scan(&key, &ref) == nil {
		pd := map[string]any{"external_ref": ref}
		for k, v := range data {
			pd[k] = v
		}
		s.emitAPIEvent(ctx, partnerTarget(key), "panel."+what, pd)
	}
}

// repTermTick reminds, ends and suspends terms; RunAPIWebhooks runs it
// about once a minute.
func repTermTick(ctx context.Context, s *Store) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,name,term_until,term_mode,term_grace,term_told FROM reps WHERE term_until>0`)
	if err != nil {
		return
	}
	type due struct {
		id, name, mode     string
		until, grace, told int64
	}
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.name, &d.until, &d.mode, &d.grace, &d.told) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	now := s.now().Unix()
	date := func(u int64) string { return time.Unix(u, 0).UTC().Format("2006-01-02") }
	for _, d := range list {
		stage := d.told
		left := d.until - now
		switch {
		case left > 0 && left <= 86400 && stage < 2:
			stage = 2
			s.tellRep(ctx, d.id, fmt.Sprintf("⏳ دورهٔ پنل نمایندگی %s فردا (%s) تمام می‌شود. برای ادامهٔ فروش تمدید کنید.", d.name, date(d.until)))
			s.termEvent(ctx, d.id, "expiring", map[string]any{"expires_unix": d.until, "days_left": 1})
		case left > 86400 && left <= 3*86400 && stage < 1:
			stage = 1
			s.tellRep(ctx, d.id, fmt.Sprintf("⏳ دورهٔ پنل نمایندگی %s تا %d روز دیگر (%s) تمام می‌شود.", d.name, (left+86399)/86400, date(d.until)))
			s.termEvent(ctx, d.id, "expiring", map[string]any{"expires_unix": d.until, "days_left": (left + 86399) / 86400})
		case left <= 0 && stage < 3:
			stage = 3
			then := "مشتری‌ها تا پایان سرویس خودشان وصل می‌مانند."
			if d.mode == "suspend" {
				then = fmt.Sprintf("اگر تا %d روز تمدید نشود، مشتری‌ها هم قطع می‌شوند.", d.grace)
			}
			s.tellRep(ctx, d.id, fmt.Sprintf("⛔️ دورهٔ پنل نمایندگی %s تمام شد؛ فروش و تمدید تا تمدید پنل بسته است. %s", d.name, then))
			notifyOwners(ctx, fmt.Sprintf("⛔️ دورهٔ پنل نمایندگی %s (%s) تمام شد.", d.name, d.id))
			s.termEvent(ctx, d.id, "expired", map[string]any{"expires_unix": d.until, "on_expiry": d.mode})
		}
		if left <= 0 && d.mode == "suspend" && stage < 4 && now >= d.until+d.grace*86400 {
			n, err := s.suspendRepClients(ctx, d.id)
			if err != nil {
				log.Printf("reseller term %s: %v", d.id, err)
				continue
			}
			stage = 4
			s.tellRep(ctx, d.id, fmt.Sprintf("🔌 %d مشتری پنل %s قطع شد چون دورهٔ پنل تمدید نشد. با تمدید پنل خودشان دوباره وصل می‌شوند.", n, d.name))
			notifyOwners(ctx, fmt.Sprintf("🔌 %d مشتری نمایندهٔ %s (%s) با پایان دورهٔ پنل قطع شد.", n, d.name, d.id))
			s.termEvent(ctx, d.id, "suspended", map[string]any{"expires_unix": d.until, "customers_off": n})
		}
		if stage != d.told {
			// Only when the term was not changed meanwhile.
			_, _ = s.db.ExecContext(ctx, `UPDATE reps SET term_told=? WHERE id=? AND term_until=?`, stage, d.id, d.until)
		}
	}
}

// suspendRepClients switches off the customers that are on, of a panel and
// its sub-sellers, and remembers them to switch back on at renewal.
func (s *Store) suspendRepClients(ctx context.Context, rep string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO rep_term_off(email,rep_id,unix)
SELECT email,?,? FROM clients WHERE enabled=1 AND (rep_id=? OR rep_id IN (SELECT id FROM reps WHERE parent_id=? AND parent_id<>''))`, rep, now, rep, rep); err != nil {
		return 0, err
	}
	r, err := tx.ExecContext(ctx, `UPDATE clients SET enabled=0 WHERE email IN (SELECT email FROM rep_term_off WHERE rep_id=?) AND enabled=1`, rep)
	if err != nil {
		return 0, err
	}
	n, _ := r.RowsAffected()
	return n, tx.Commit()
}

// RepTermRow is one panel's term in the owner's list.
type RepTermRow struct {
	RepID string `json:"rep_id"`
	RepTerm
}

// RepTerms lists every panel that has a term.
func (s *Store) RepTerms(ctx context.Context) ([]RepTermRow, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id FROM reps WHERE term_until>0 ORDER BY term_until`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []RepTermRow{}
	for _, id := range ids {
		if t, err := s.RepTermOf(ctx, id); err == nil {
			out = append(out, RepTermRow{RepID: id, RepTerm: t})
		}
	}
	return out, nil
}

// RepTermIn changes a term: expires_unix sets it (0 = no term), or
// add_days extends it; on_expiry and grace_days change what happens at its
// end (left out: kept).
type RepTermIn struct {
	Expires   *int64 `json:"expires_unix"`
	AddDays   int64  `json:"add_days"`
	OnExpiry  string `json:"on_expiry"`
	GraceDays *int64 `json:"grace_days"`
}

func (s *Store) applyTermIn(ctx context.Context, actor, rep string, in RepTermIn) (RepTerm, error) {
	grace := int64(-1)
	if in.GraceDays != nil {
		grace = *in.GraceDays
		if grace < 0 {
			return RepTerm{}, fmt.Errorf("%w: grace_days is 0 to 365", ErrInvalid)
		}
	}
	switch {
	case in.AddDays != 0:
		return s.ExtendRepTerm(ctx, actor, rep, in.AddDays, in.OnExpiry, grace)
	case in.Expires != nil:
		return s.SetRepTerm(ctx, actor, rep, *in.Expires, in.OnExpiry, grace)
	default:
		cur, err := s.RepTermOf(ctx, rep)
		if err != nil {
			return cur, err
		}
		return s.SetRepTerm(ctx, actor, rep, cur.Until, in.OnExpiry, grace)
	}
}

// repTermRoutes are the owner's: every term, and one panel's.
func (s *Store) repTermRoutes(mux routeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v2/admin/rep-terms", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.RepTerms(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/term", guard(func(w http.ResponseWriter, r *http.Request) {
		t, err := s.RepTermOf(r.Context(), r.PathValue("id"))
		reply(w, t, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/term", guard(func(w http.ResponseWriter, r *http.Request) {
		var in RepTermIn
		if !decode(w, r, &in) {
			return
		}
		t, err := s.applyTermIn(r.Context(), who(r).User, r.PathValue("id"), in)
		reply(w, t, err)
	}))
}
