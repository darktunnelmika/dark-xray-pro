package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The rest of PasarGuard's user API, under its own paths and shapes, for a
// seller's token (the owner's or a reseller's). Every change still goes
// through the reseller rules (credit, policy, refunds):
//
//	/api/user/{username}, /api/user/by-username/{username}, /api/user/by-id/{id}
//	    GET, PUT, DELETE, and .../reset, /revoke_sub, /active_next,
//	    /sub_update, /usage, /disabled, /set_owner
//	GET  /api/user/{id}/subscription/{client_type}
//	POST /api/user/from_template              PUT /api/user/from_template/{username|by-username/..|by-id/..}
//	GET  /api/users/simple, /api/users/usage, /api/users/counts/{metric}
//	GET, DELETE /api/users/expired            POST /api/users/reset (owner)
//	POST /api/users/bulk/{delete,reset,disable,enable,revoke_sub,expire,data_limit,from_template,apply_template}
//	PUT  /api/users/bulk/set_owner (owner)
//	/api/user_template(s)...                  the seller's shop products
//	GET  /api/groups/simple, /api/group/{id}, POST /api/groups/bulk/{add,remove}
//	GET  /api/inbounds, /api/admins, /api/admins/simple
//	GET  /sub/{token}/info, /sub/{token}/usage, /sub/{token}/{client_type}
//
// What has no counterpart here is answered plainly: a reseller cannot set a
// periodic reset or a next plan (both give service nobody paid for), only
// the owner can move users between sellers, and Dark keeps usage per day,
// so usage charts come in day buckets whatever period is asked.

type pgAuthed func(http.ResponseWriter, *http.Request, string)

type pgSrv struct {
	s    *Store
	base func(*http.Request, string) string
}

// pgFind is the seller's client named by a username, or by a user id.
func (s *Store) pgFind(ctx context.Context, seller, key string, byID bool) (ClientView, error) {
	email := key
	if byID {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || s.ro.QueryRowContext(ctx, `SELECT email FROM clients WHERE rowid=?`, id).Scan(&email) != nil {
			return ClientView{}, ErrNotFound
		}
	}
	c, err := s.clientView(ctx, email)
	if err != nil || c.RepID != seller {
		return ClientView{}, ErrNotFound
	}
	return c, nil
}

func (p *pgSrv) view(w http.ResponseWriter, r *http.Request, seller, email string) {
	p.viewCode(w, r, seller, email, http.StatusOK)
}

func (p *pgSrv) viewCode(w http.ResponseWriter, r *http.Request, seller, email string, code int) {
	c, err := p.s.clientView(r.Context(), email)
	if err != nil {
		pgFail(w, err)
		return
	}
	groups, _ := p.s.pgGroups(r.Context(), seller)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(p.s.pgView(r.Context(), r, p.base(r, seller), c, groups, true))
}

func pgDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		pgErr(w, http.StatusUnprocessableEntity, "invalid JSON")
		return false
	}
	return true
}

func pgOwnerOnly(w http.ResponseWriter, seller string) bool {
	if seller != "" {
		pgErr(w, http.StatusForbidden, "You're not allowed")
		return false
	}
	return true
}

// ---- extra user fields: reset strategy, device limit, next plan, on-hold timeout ----

type pgNext struct {
	UserTemplateID      *int64 `json:"user_template_id"`
	DataLimit           *int64 `json:"data_limit"`
	Expire              *int64 `json:"expire"`
	AddRemainingTraffic bool   `json:"add_remaining_traffic"`
}

var pgStrategies = map[string]string{"no_reset": "", "day": "day", "week": "week", "month": "month"}

func pgStrategyOf(period string) string {
	if period == "" {
		return "no_reset"
	}
	return period
}

// pgCheckExtras refuses, before anything is saved or charged, what the
// seller may not set. cur is the client being changed (nil for a new one):
// a reseller's bot that reads a user and sends it back whole may repeat what
// the owner set on it.
func (s *Store) pgCheckExtras(ctx context.Context, seller string, cur *ClientView, in pgIn) error {
	if in.DataLimitResetStrategy != nil {
		period, ok := pgStrategies[*in.DataLimitResetStrategy]
		if !ok {
			return fmt.Errorf("%w: data_limit_reset_strategy %q is not supported (no_reset, day, week, month)", ErrInvalid, *in.DataLimitResetStrategy)
		}
		if seller != "" && period != "" && (cur == nil || cur.ResetPeriod != period) {
			return fmt.Errorf("%w: a periodic usage reset is set by the panel owner", ErrPolicy)
		}
	}
	if in.HWIDLimit != nil && (*in.HWIDLimit < 0 || *in.HWIDLimit > 100) {
		return fmt.Errorf("%w: hwid_limit must be 0-100", ErrInvalid)
	}
	if n := strings.TrimSpace(string(in.NextPlan)); n != "" && n != "null" {
		np, err := s.pgNextPlan(ctx, seller, in.NextPlan)
		if err != nil && seller == "" {
			return err
		}
		if seller != "" && (err != nil || !sameNextPlan(cur, np)) {
			return fmt.Errorf("%w: a next plan is set by the panel owner", ErrPolicy)
		}
	}
	if _, _, err := pgExpire(in.OnHoldTimeout); err != nil {
		return fmt.Errorf("%w: on_hold_timeout", ErrInvalid)
	}
	return nil
}

// sameNextPlan tells whether np repeats the client's own next plan (as the
// user view shows it: volume and days).
func sameNextPlan(cur *ClientView, np *NextPlan) bool {
	if cur == nil || cur.NextPlan == nil || np == nil {
		return false
	}
	return math.Abs(cur.NextPlan.GB-np.GB) < 1e-6 && cur.NextPlan.Days == np.Days
}

// pgNextPlan turns PasarGuard's next plan (bytes and seconds, or a template)
// into Dark's.
func (s *Store) pgNextPlan(ctx context.Context, seller string, raw json.RawMessage) (*NextPlan, error) {
	var in pgNext
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("%w: next_plan", ErrInvalid)
	}
	np := &NextPlan{}
	if in.UserTemplateID != nil {
		pr, err := s.pgProduct(ctx, seller, *in.UserTemplateID)
		if err != nil {
			return nil, err
		}
		np.GB, np.Days, np.FirstUse = float64(pr.GB), int(pr.Days), pr.FirstUse
		return np, nil
	}
	if in.DataLimit != nil && *in.DataLimit > 0 {
		np.GB = float64(*in.DataLimit) / (1 << 30)
	}
	if in.Expire != nil && *in.Expire > 0 {
		np.Days = int((*in.Expire + 86399) / 86400)
	}
	if np.GB == 0 && np.Days == 0 {
		return nil, fmt.Errorf("%w: a next plan needs data_limit or expire", ErrInvalid)
	}
	return np, nil
}

// pgSetExtras stores the extra fields after the user itself was saved.
func (s *Store) pgSetExtras(ctx context.Context, seller, email string, in pgIn) error {
	if in.Note != nil {
		if err := s.SetClientNote(ctx, email, clip(*in.Note, 2000)); err != nil {
			return err
		}
	}
	if in.DataLimitResetStrategy != nil {
		if err := s.SetResetPeriod(ctx, email, pgStrategies[*in.DataLimitResetStrategy]); err != nil {
			return err
		}
	}
	if in.HWIDLimit != nil {
		if err := s.SetHWIDLimit(ctx, email, *in.HWIDLimit); err != nil {
			return err
		}
	}
	if n := strings.TrimSpace(string(in.NextPlan)); n == "null" {
		if err := s.SetNextPlan(ctx, email, nil); err != nil {
			return err
		}
	} else if n != "" && seller == "" {
		np, err := s.pgNextPlan(ctx, seller, in.NextPlan)
		if err != nil {
			return err
		}
		if err := s.SetNextPlan(ctx, email, np); err != nil {
			return err
		}
	}
	if at, set, _ := pgExpire(in.OnHoldTimeout); set {
		if _, err := s.db.ExecContext(ctx, `UPDATE clients SET start_by_unix=? WHERE email=?`, max(at, 0), email); err != nil {
			return err
		}
	}
	return nil
}

// pgModify applies a change (PUT) to one of the seller's clients.
func (p *pgSrv) modify(ctx context.Context, seller string, cur ClientView, in pgIn) error {
	if err := p.s.pgCheckExtras(ctx, seller, &cur, in); err != nil {
		return err
	}
	c := cur.Client
	c.UUID, c.Secret, c.SubToken = "", "", "" // credentials stay as they are
	if err := p.s.pgApply(ctx, seller, &c, in, false); err != nil {
		return err
	}
	if err := p.s.pgSave(ctx, seller, c); err != nil {
		return err
	}
	return p.s.pgSetExtras(ctx, seller, c.Email, in)
}

// pgSetEnabled turns a client on or off. For a reseller it costs nothing but
// needs the right to change settings; it also works on an expired client.
func (s *Store) pgSetEnabled(ctx context.Context, seller, email string, on bool) error {
	if seller != "" {
		pol, err := s.RepPolicyOf(ctx, seller)
		if err != nil {
			return err
		}
		if pol.DenyEdit {
			return fmt.Errorf("%w: changing client settings is not allowed for this representative", ErrPolicy)
		}
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET enabled=? WHERE email=? AND rep_id=?`, on, email, seller)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// pgDelete deletes a client the way its seller may.
func (s *Store) pgDelete(ctx context.Context, seller, email string) error {
	if seller == "" {
		return s.DeleteClient(ctx, email)
	}
	_, _, err := s.RepDeleteClient(ctx, "mirza", seller, email)
	return err
}

func (s *Store) pgReset(ctx context.Context, seller, email string) error {
	if seller == "" {
		return s.ResetUsage(ctx, email, "api")
	}
	return s.pgResetRep(ctx, seller, email)
}

// pgRepOf reads an admin name as PasarGuard bots know it: a reseller id,
// its API username (mirza_<id>), or "owner".
func (s *Store) pgRepOf(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "owner" || name == "mirza" {
		return "", nil
	}
	if id, ok := strings.CutPrefix(name, "mirza_"); ok {
		name = id
	}
	var n int
	if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, name).Scan(&n) != nil || n != 1 {
		return "", fmt.Errorf("%w: admin not found", ErrNotFound)
	}
	return name, nil
}

// ---- /api/user/... ----

func (p *pgSrv) user(w http.ResponseWriter, r *http.Request, seller string) {
	ctx := r.Context()
	segs := strings.Split(strings.Trim(r.PathValue("rest"), "/"), "/")
	if segs[0] == "from_template" {
		if r.Method != http.MethodPut || len(segs) < 2 {
			pgErr(w, http.StatusNotFound, "Not Found")
			return
		}
		key, byID := segs[1], false
		if (segs[1] == "by-id" || segs[1] == "by-username") && len(segs) == 3 {
			key, byID = segs[2], segs[1] == "by-id"
		} else if len(segs) != 2 {
			pgErr(w, http.StatusNotFound, "Not Found")
			return
		}
		c, err := p.s.pgFind(ctx, seller, key, byID)
		if err != nil {
			pgFail(w, err)
			return
		}
		p.renewFromTemplate(w, r, seller, c)
		return
	}
	if r.Method == http.MethodGet && len(segs) == 3 && segs[1] == "subscription" {
		c, err := p.s.pgFind(ctx, seller, segs[0], true)
		if err != nil {
			pgFail(w, err)
			return
		}
		p.subscription(w, r, c.Email, segs[2])
		return
	}
	key, byID, action := segs[0], false, segs[1:]
	if segs[0] == "by-id" || segs[0] == "by-username" {
		if len(segs) < 2 {
			pgErr(w, http.StatusNotFound, "Not Found")
			return
		}
		key, byID, action = segs[1], segs[0] == "by-id", segs[2:]
	}
	if len(action) > 1 {
		pgErr(w, http.StatusNotFound, "Not Found")
		return
	}
	act := ""
	if len(action) == 1 {
		act = action[0]
	}
	c, err := p.s.pgFind(ctx, seller, key, byID)
	if err != nil && !byID && key == PGAllGroups && r.Method == http.MethodGet && act == "" {
		// Not a real user: every inbound the seller may give, for a bot
		// that learns its groups by reading one user.
		groups, _ := p.s.pgGroups(ctx, seller)
		u := pgUser{Username: PGAllGroups, Status: "disabled", DataLimitResetStrategy: "no_reset", GroupIDs: []int64{}, Links: []string{},
			ProxySettings: map[string]any{}, Admin: map[string]any{"id": nil, "username": or2(seller, "owner")}}
		for _, g := range groups {
			if !g.IsDisabled {
				u.GroupIDs = append(u.GroupIDs, g.ID)
			}
		}
		pgJSON(w, u)
		return
	}
	if err != nil {
		pgFail(w, err)
		return
	}
	switch r.Method + " " + act {
	case "GET ":
		p.view(w, r, seller, c.Email)
	case "PUT ":
		var in pgIn
		if !pgDecode(w, r, &in) {
			return
		}
		if err := p.modify(ctx, seller, c, in); err != nil {
			pgFail(w, err)
			return
		}
		p.view(w, r, seller, c.Email)
	case "DELETE ":
		if err := p.s.pgDelete(ctx, seller, c.Email); err != nil {
			pgFail(w, err)
			return
		}
		// PasarGuard now answers 204; the Marzban-style answer is kept
		// because the sales bots already in use read it.
		pgJSON(w, map[string]string{"detail": "User successfully deleted"})
	case "POST reset":
		if err := p.s.pgReset(ctx, seller, c.Email); err != nil {
			pgFail(w, err)
			return
		}
		p.view(w, r, seller, c.Email)
	case "POST revoke_sub":
		if err := p.s.RotateClient(ctx, c.Email, true, true); err != nil {
			pgFail(w, err)
			return
		}
		p.view(w, r, seller, c.Email)
	case "POST active_next":
		if c.NextPlan == nil {
			pgErr(w, http.StatusNotFound, "User doesn't have next plan")
			return
		}
		if err := p.s.applyNextPlan(ctx, c.Email, *c.NextPlan); err != nil {
			pgFail(w, err)
			return
		}
		p.view(w, r, seller, c.Email)
	case "PUT disabled":
		var in struct {
			Disabled *bool `json:"disabled"`
		}
		if !pgDecode(w, r, &in) {
			return
		}
		if in.Disabled == nil {
			pgErr(w, http.StatusUnprocessableEntity, "disabled: field required")
			return
		}
		if err := p.s.pgSetEnabled(ctx, seller, c.Email, !*in.Disabled); err != nil {
			pgFail(w, err)
			return
		}
		p.view(w, r, seller, c.Email)
	case "PUT set_owner":
		if !pgOwnerOnly(w, seller) {
			return
		}
		to, err := p.s.pgRepOf(ctx, r.URL.Query().Get("admin_username"))
		if err != nil {
			pgErr(w, http.StatusNotFound, "Admin not found")
			return
		}
		if err := p.s.TransferClient(ctx, c.Email, to); err != nil {
			pgFail(w, err)
			return
		}
		p.view(w, r, to, c.Email)
	case "GET sub_update":
		p.subUpdates(w, r, c.Email)
	case "GET usage":
		p.usage(w, r, "c.email=?", c.Email)
	default:
		pgErr(w, http.StatusNotFound, "Not Found")
	}
}

func (p *pgSrv) subUpdates(w http.ResponseWriter, r *http.Request, email string) {
	type upd struct {
		CreatedAt string  `json:"created_at"`
		UserAgent string  `json:"user_agent"`
		IP        *string `json:"ip"`
		HWID      *string `json:"hwid"`
	}
	ups := []upd{}
	rows, err := p.s.ro.QueryContext(r.Context(), `SELECT last_unix,agent,hwid FROM client_devices WHERE email=? ORDER BY last_unix DESC LIMIT 10`, email)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var at int64
			var agent, hwid string
			if rows.Scan(&at, &agent, &hwid) == nil {
				u := upd{CreatedAt: *pgTime(max(at, 1)), UserAgent: agent}
				if hwid != "" {
					u.HWID = &hwid
				}
				ups = append(ups, u)
			}
		}
	}
	pgJSON(w, map[string]any{"updates": ups, "count": len(ups)})
}

// subscription serves one of PasarGuard's client types.
func (p *pgSrv) subscription(w http.ResponseWriter, r *http.Request, email, clientType string) {
	ctx := r.Context()
	if clientType == "links" {
		links, info, err := p.s.Subscription(ctx, email)
		if err != nil {
			pgFail(w, err)
			return
		}
		w.Header().Set("Subscription-Userinfo", info)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strings.Join(links, "\n")))
		return
	}
	format := map[string]string{"links_base64": "base64", "xray": "xray", "sing_box": "singbox", "clash": "clash", "clash_meta": "clash"}[clientType]
	if format == "" {
		pgErr(w, http.StatusNotFound, "Not Found")
		return
	}
	body, ctype, info, err := p.s.SubscriptionBody(ctx, email, format)
	if err != nil {
		pgFail(w, err)
		return
	}
	w.Header().Set("Subscription-Userinfo", info)
	w.Header().Set("Content-Type", ctype)
	_, _ = w.Write(body)
}

// ---- usage charts ----

// pgRange reads start and end (default: the last 30 days).
func pgRange(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	q := r.URL.Query()
	end, start := now.UTC(), now.UTC().AddDate(0, 0, -30)
	if v := q.Get("end"); v != "" {
		t, ok := pgParseTime(v)
		if !ok {
			return start, end, fmt.Errorf("%w: end", ErrInvalid)
		}
		end = t.UTC()
	}
	if v := q.Get("start"); v != "" {
		t, ok := pgParseTime(v)
		if !ok {
			return start, end, fmt.Errorf("%w: start", ErrInvalid)
		}
		start = t.UTC()
	}
	if !start.Before(end) {
		return start, end, fmt.Errorf("%w: start must be before end", ErrInvalid)
	}
	return start, end, nil
}

// pgParseTime reads an ISO time (a "+" in a query often arrives as a space)
// or a Unix time.
func pgParseTime(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Unix(n, 0), true
	}
	if i := strings.LastIndex(v, " "); i > 10 {
		v = v[:i] + "+" + v[i+1:]
	}
	for _, f := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(f, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// usage answers a usage chart for the clients matched by where (with the
// seller's scope inside it), per day, as one series (-1) or per node.
func (p *pgSrv) usage(w http.ResponseWriter, r *http.Request, where string, args ...any) {
	ctx := r.Context()
	start, end, err := pgRange(r, p.s.now())
	if err != nil {
		pgFail(w, err)
		return
	}
	q := r.URL.Query()
	byNode := q.Get("group_by_node") == "true"
	nodeFilter := q.Get("node_id")
	a := append([]any{}, args...)
	a = append(a, start.Format("2006-01-02"), end.Format("2006-01-02"))
	rows, err := p.s.ro.QueryContext(ctx, `SELECT d.day,COALESCE(n.rowid,0),SUM(d.up+d.down) FROM usage_daily d JOIN clients c ON c.email=d.email
LEFT JOIN nodes n ON n.id=d.node_id WHERE `+where+` AND d.day>=? AND d.day<=? GROUP BY d.day,d.node_id ORDER BY d.day`, a...)
	if err != nil {
		pgFail(w, err)
		return
	}
	defer rows.Close()
	type stat struct {
		PeriodStart  string `json:"period_start"`
		TotalTraffic int64  `json:"total_traffic"`
	}
	series := map[string]map[string]int64{}
	for rows.Next() {
		var day string
		var node, total int64
		if rows.Scan(&day, &node, &total) != nil {
			continue
		}
		if nodeFilter != "" && strconv.FormatInt(node, 10) != nodeFilter {
			continue
		}
		key := "-1"
		if byNode {
			key = strconv.FormatInt(node, 10)
		}
		if series[key] == nil {
			series[key] = map[string]int64{}
		}
		series[key][day] += total
	}
	out := map[string][]stat{}
	for key, days := range series {
		list := []stat{}
		for day, total := range days {
			list = append(list, stat{day + "T00:00:00Z", total})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].PeriodStart < list[j].PeriodStart })
		out[key] = list
	}
	if len(out) == 0 && !byNode {
		out["-1"] = []stat{}
	}
	pgJSON(w, map[string]any{"period": "day", "start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339), "stats": out})
}

// ---- lists and filters ----

// pgQueryList reads a list parameter given repeated or comma-separated.
func pgQueryList(r *http.Request, keys ...string) []string {
	var out []string
	for _, k := range keys {
		for _, v := range r.URL.Query()[k] {
			for _, part := range strings.Split(v, ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
		}
	}
	return out
}

// pgRow is a client with what lists filter and sort on.
type pgRow struct {
	c         ClientView
	id        int64
	created   int64
	expire    int64 // effective expiry, 0 = none
	status    string
	hwidCount int
}

func (s *Store) pgRows(ctx context.Context, seller string) ([]pgRow, error) {
	list, err := s.clientViews(ctx, "c.rep_id=?", seller)
	if err != nil {
		return nil, err
	}
	ids := map[string][2]int64{}
	rows, err := s.ro.QueryContext(ctx, `SELECT email,rowid,created_unix FROM clients WHERE rep_id=?`, seller)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e string
		var id, created int64
		if rows.Scan(&e, &id, &created) == nil {
			ids[e] = [2]int64{id, created}
		}
	}
	rows.Close()
	hw := map[string]int{}
	if rows, err := s.ro.QueryContext(ctx, `SELECT d.email,COUNT(*) FROM client_devices d JOIN clients c ON c.email=d.email WHERE c.rep_id=? GROUP BY d.email`, seller); err == nil {
		for rows.Next() {
			var e string
			var n int
			if rows.Scan(&e, &n) == nil {
				hw[e] = n
			}
		}
		rows.Close()
	}
	now := s.now().Unix()
	out := make([]pgRow, 0, len(list))
	for _, c := range list {
		x := pgRow{c: c, id: ids[c.Email][0], created: ids[c.Email][1], status: pgStatus(c, now), hwidCount: hw[c.Email]}
		switch {
		case c.ExpiresUnix > 0:
			x.expire = c.ExpiresUnix
		case c.DurationSeconds > 0 && c.Usage.FirstSeen > 0:
			x.expire = c.Usage.FirstSeen + c.DurationSeconds
		}
		out = append(out, x)
	}
	return out, nil
}

// pgFilter keeps the rows a GET /api/users query asks for.
func pgFilter(r *http.Request, rows []pgRow, groups []pgGroup) ([]pgRow, error) {
	q := r.URL.Query()
	set := func(keys ...string) map[string]bool {
		m := map[string]bool{}
		for _, v := range pgQueryList(r, keys...) {
			m[strings.ToLower(v)] = true
		}
		return m
	}
	statuses, names, ids, gids := set("status"), set("username", "usernames"), set("ids"), set("group", "group_ids")
	search := strings.ToLower(strings.TrimSpace(q.Get("search")))
	tagOf := map[string]bool{}
	for _, g := range groups {
		if gids[strconv.FormatInt(g.ID, 10)] {
			tagOf[g.Name] = true
		}
	}
	num := func(k string) (int64, bool) {
		v, err := strconv.ParseInt(q.Get(k), 10, 64)
		return v, err == nil
	}
	when := func(k string) (int64, bool, error) {
		v := q.Get(k)
		if v == "" {
			return 0, false, nil
		}
		t, ok := pgParseTime(v)
		if !ok {
			return 0, false, fmt.Errorf("%w: %s", ErrInvalid, k)
		}
		return t.Unix(), true, nil
	}
	var bounds [4]int64
	var has [4]bool
	for i, k := range []string{"expire_after", "expire_before", "online_after", "online_before"} {
		v, ok, err := when(k)
		if err != nil {
			return nil, err
		}
		bounds[i], has[i] = v, ok
	}
	dlMin, hasMin := num("data_limit_min")
	dlMax, hasMax := num("data_limit_max")
	online, noLimit, noExpire, noGroup := q.Get("online") == "true", q.Get("no_data_limit") == "true", q.Get("no_expire") == "true", q.Get("no_group") == "true"
	out := rows[:0:0]
	for _, x := range rows {
		c := x.c
		name := strings.ToLower(c.Email)
		switch {
		case len(statuses) > 0 && !statuses[x.status],
			len(names) > 0 && !names[name] && !(len(names) == 1 && q.Get("usernames") == "" && strings.Contains(name, pgFirst(names))),
			len(ids) > 0 && !ids[strconv.FormatInt(x.id, 10)],
			search != "" && !strings.Contains(name, search) && !strings.Contains(strings.ToLower(c.Note), search),
			online && !c.Online,
			noLimit && c.VolumeBytes > 0,
			hasMin && (c.VolumeBytes <= 0 || c.VolumeBytes < dlMin),
			hasMax && (c.VolumeBytes <= 0 || c.VolumeBytes > dlMax),
			noExpire && x.expire > 0,
			has[0] && (x.expire == 0 || x.expire < bounds[0]),
			has[1] && (x.expire == 0 || x.expire > bounds[1]),
			has[2] && c.Usage.LastSeen < bounds[2],
			has[3] && (c.Usage.LastSeen == 0 || c.Usage.LastSeen > bounds[3]),
			noGroup && len(c.Inbounds) > 0:
			continue
		}
		if len(gids) > 0 {
			in := false
			for _, t := range c.Inbounds {
				in = in || tagOf[t]
			}
			if !in {
				continue
			}
		}
		out = append(out, x)
	}
	return out, nil
}

func pgFirst(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}

// pgSort orders rows by PasarGuard's sort keys ("-" = descending). With no
// key the newest come first.
func pgSort(rows []pgRow, keys []string) {
	if len(keys) == 0 {
		return
	}
	cmp := func(a, b pgRow, key string) int {
		var x, y int64
		switch key {
		case "username", "name":
			return strings.Compare(strings.ToLower(a.c.Email), strings.ToLower(b.c.Email))
		case "used_traffic":
			x, y = a.c.Usage.Up+a.c.Usage.Down, b.c.Usage.Up+b.c.Usage.Down
		case "data_limit":
			x, y = max(a.c.VolumeBytes, 0), max(b.c.VolumeBytes, 0)
		case "expire":
			x, y = a.expire, b.expire
		case "created_at", "edit_at":
			x, y = a.created, b.created
			if x == y {
				x, y = a.id, b.id
			}
		case "id":
			x, y = a.id, b.id
		case "online_at":
			x, y = a.c.Usage.LastSeen, b.c.Usage.LastSeen
		case "hwid_count":
			x, y = int64(a.hwidCount), int64(b.hwidCount)
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for _, k := range keys {
			desc := strings.HasPrefix(k, "-")
			v := cmp(rows[i], rows[j], strings.TrimPrefix(k, "-"))
			if desc {
				v = -v
			}
			if v != 0 {
				return v < 0
			}
		}
		return false
	})
}

func pgPage(r *http.Request, n int) (from, to int) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	from = min(max(offset, 0), n)
	to = n
	if limit > 0 && from+limit < n {
		to = from + limit
	}
	return from, to
}

func (p *pgSrv) users(w http.ResponseWriter, r *http.Request, seller string) {
	ctx := r.Context()
	rows, err := p.s.pgRows(ctx, seller)
	if err != nil {
		pgFail(w, err)
		return
	}
	groups, _ := p.s.pgGroups(ctx, seller)
	if rows, err = pgFilter(r, rows, groups); err != nil {
		pgFail(w, err)
		return
	}
	pgSort(rows, pgQueryList(r, "sort"))
	from, to := pgPage(r, len(rows))
	links := r.URL.Query().Get("load_sub") == "true"
	b := p.base(r, seller)
	users := []pgUser{}
	for _, x := range rows[from:to] {
		u := p.s.pgView(ctx, r, b, x.c, groups, links)
		n := x.hwidCount
		u.HWIDCount = &n
		users = append(users, u)
	}
	pgJSON(w, map[string]any{"users": users, "total": len(rows)})
}

func (p *pgSrv) usersSimple(w http.ResponseWriter, r *http.Request, seller string) {
	rows, err := p.s.pgRows(r.Context(), seller)
	if err != nil {
		pgFail(w, err)
		return
	}
	search := strings.ToLower(r.URL.Query().Get("search"))
	ids, names := map[string]bool{}, map[string]bool{}
	for _, v := range pgQueryList(r, "ids") {
		ids[v] = true
	}
	for _, v := range pgQueryList(r, "usernames") {
		names[strings.ToLower(v)] = true
	}
	kept := rows[:0:0]
	for _, x := range rows {
		n := strings.ToLower(x.c.Email)
		if (search != "" && !strings.Contains(n, search)) || (len(ids) > 0 && !ids[strconv.FormatInt(x.id, 10)]) || (len(names) > 0 && !names[n]) {
			continue
		}
		kept = append(kept, x)
	}
	keys := pgQueryList(r, "sort")
	if len(keys) == 0 {
		keys = []string{"id"}
	}
	pgSort(kept, keys)
	from, to := 0, len(kept)
	if r.URL.Query().Get("all") != "true" {
		from, to = pgPage(r, len(kept))
	}
	type simple struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	out := []simple{}
	for _, x := range kept[from:to] {
		out = append(out, simple{x.id, x.c.Email})
	}
	pgJSON(w, map[string]any{"users": out, "total": len(kept)})
}

// counts answers /api/users/counts/{metric} per day: users with traffic
// (online), users whose term ended that day (expired), and the users used up
// now (limited; Dark keeps no history of it).
func (p *pgSrv) counts(w http.ResponseWriter, r *http.Request, seller string) {
	ctx := r.Context()
	metric := r.PathValue("metric")
	start, end, err := pgRange(r, p.s.now())
	if err != nil {
		pgFail(w, err)
		return
	}
	type stat struct {
		PeriodStart string `json:"period_start"`
		Count       int    `json:"count"`
	}
	stats := []stat{}
	total := 0
	switch metric {
	case "online":
		rows, err := p.s.ro.QueryContext(ctx, `SELECT d.day,COUNT(DISTINCT d.email) FROM usage_daily d JOIN clients c ON c.email=d.email
WHERE c.rep_id=? AND d.day>=? AND d.day<=? AND d.up+d.down>0 GROUP BY d.day ORDER BY d.day`, seller, start.Format("2006-01-02"), end.Format("2006-01-02"))
		if err != nil {
			pgFail(w, err)
			return
		}
		for rows.Next() {
			var day string
			var n int
			if rows.Scan(&day, &n) == nil {
				stats = append(stats, stat{day + "T00:00:00Z", n})
			}
		}
		rows.Close()
		_ = p.s.ro.QueryRowContext(ctx, `SELECT COUNT(DISTINCT d.email) FROM usage_daily d JOIN clients c ON c.email=d.email
WHERE c.rep_id=? AND d.day>=? AND d.day<=? AND d.up+d.down>0`, seller, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&total)
	case "expired", "limited":
		rows, err := p.s.pgRows(ctx, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		perDay := map[string]int{}
		for _, x := range rows {
			if metric == "expired" && x.expire > 0 && x.expire >= start.Unix() && x.expire <= end.Unix() {
				perDay[time.Unix(x.expire, 0).UTC().Format("2006-01-02")]++
				total++
			}
			if metric == "limited" && x.status == "limited" {
				perDay[end.Format("2006-01-02")]++
				total++
			}
		}
		for day, n := range perDay {
			stats = append(stats, stat{day + "T00:00:00Z", n})
		}
		sort.Slice(stats, func(i, j int) bool { return stats[i].PeriodStart < stats[j].PeriodStart })
	default:
		pgErr(w, http.StatusUnprocessableEntity, "metric: one of online, expired, limited")
		return
	}
	pgJSON(w, map[string]any{"period": "day", "start": start.Format(time.RFC3339), "end": end.Format(time.RFC3339),
		"metric": metric, "count_during_period": total, "stats": map[string]any{"-1": stats}})
}

// cleanupTargets lists the users /api/users/expired picks. Dark keeps no
// time of a status change, so for limited, on_hold and disabled the dates
// are not used.
func (p *pgSrv) cleanupTargets(r *http.Request, seller string) ([]string, error) {
	q := r.URL.Query()
	target := or2(q.Get("target"), "expired")
	after, before := int64(0), int64(math.MaxInt64)
	if v := or2(q.Get("expired_after"), q.Get("expire_after")); v != "" {
		t, ok := pgParseTime(v)
		if !ok {
			return nil, fmt.Errorf("%w: expired_after", ErrInvalid)
		}
		after = t.Unix()
	}
	if v := or2(q.Get("expired_before"), q.Get("expire_before")); v != "" {
		t, ok := pgParseTime(v)
		if !ok {
			return nil, fmt.Errorf("%w: expired_before", ErrInvalid)
		}
		before = t.Unix()
	} else if target == "inactive" {
		return nil, fmt.Errorf("%w: expired_before is required when target is inactive", ErrInvalid)
	}
	rows, err := p.s.pgRows(r.Context(), seller)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, x := range rows {
		ok := false
		switch target {
		case "expired":
			ok = x.status == "expired" && x.expire >= after && x.expire <= before
		case "limited", "on_hold", "disabled":
			ok = x.status == target
		case "inactive":
			last := or3(x.c.Usage.LastSeen, x.created)
			ok = last >= after && last <= before
		default:
			return nil, fmt.Errorf("%w: target: one of expired, limited, on_hold, disabled, inactive", ErrInvalid)
		}
		if ok {
			out = append(out, x.c.Email)
		}
	}
	return out, nil
}

func or3(a, b int64) int64 {
	if a > 0 {
		return a
	}
	return b
}

// ---- bulk ----

// pgPick turns user ids into the seller's usernames; any id that is not the
// seller's makes the whole call a 404, before anything changes.
func (s *Store) pgPick(ctx context.Context, seller string, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: ids: at least one user", ErrInvalid)
	}
	seen := map[int64]bool{}
	out := []string{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var email, rep string
		if s.ro.QueryRowContext(ctx, `SELECT email,rep_id FROM clients WHERE rowid=?`, id).Scan(&email, &rep) != nil || rep != seller {
			return nil, ErrNotFound
		}
		out = append(out, email)
	}
	return out, nil
}

// bulkFail answers a bulk change stopped part way: what was done stays done.
func bulkFail(w http.ResponseWriter, done int, err error) {
	if done == 0 {
		pgFail(w, err)
		return
	}
	code := http.StatusConflict
	if errors.Is(err, ErrCredit) {
		code = http.StatusBadRequest
	}
	pgErr(w, code, fmt.Sprintf("stopped after %d users: %v", done, err))
}

func (p *pgSrv) bulkSelect(action string) pgAuthed {
	return func(w http.ResponseWriter, r *http.Request, seller string) {
		ctx := r.Context()
		var in struct {
			IDs []int64 `json:"ids"`
		}
		if !pgDecode(w, r, &in) {
			return
		}
		names, err := p.s.pgPick(ctx, seller, in.IDs)
		if err != nil {
			pgFail(w, err)
			return
		}
		done := []string{}
		for _, e := range names {
			var err error
			switch action {
			case "delete":
				err = p.s.pgDelete(ctx, seller, e)
			case "reset":
				err = p.s.pgReset(ctx, seller, e)
			case "revoke_sub":
				err = p.s.RotateClient(ctx, e, true, true)
			case "disable", "enable":
				c, cerr := p.s.clientView(ctx, e)
				if cerr != nil || c.Enabled == (action == "enable") {
					continue // already so
				}
				err = p.s.pgSetEnabled(ctx, seller, e, action == "enable")
			}
			if err != nil {
				bulkFail(w, len(done), err)
				return
			}
			done = append(done, e)
		}
		pgJSON(w, map[string]any{"users": done, "count": len(done)})
	}
}

// pgBulkFilter is PasarGuard's selection for bulk expire, data limit and
// group changes. admins is ignored: a token only reaches its own users.
type pgBulkFilter struct {
	Amount        int64           `json:"amount"`
	DryRun        bool            `json:"dry_run"`
	Admins        []int64         `json:"admins"`
	GroupIDs      []int64         `json:"group_ids"`
	HasGroupIDs   []int64         `json:"has_group_ids"`
	HasNoGroup    bool            `json:"has_no_group"`
	Users         []int64         `json:"users"`
	Status        []string        `json:"status"`
	ExpireAfter   json.RawMessage `json:"expire_after"`
	ExpireBefore  json.RawMessage `json:"expire_before"`
	ExpiredAfter  json.RawMessage `json:"expired_after"`
	ExpiredBefore json.RawMessage `json:"expired_before"`
}

func (p *pgSrv) bulkTargets(ctx context.Context, seller string, f pgBulkFilter, groupKey []int64) ([]pgRow, error) {
	if len(f.Admins) > 0 {
		// A token reaches only its own users: they belong to the seller's
		// admin id (a reseller's row id; the owner has none).
		var self int64
		if seller != "" {
			_ = p.s.ro.QueryRowContext(ctx, `SELECT rowid FROM reps WHERE id=?`, seller).Scan(&self)
		}
		mine := false
		for _, id := range f.Admins {
			mine = mine || (self > 0 && id == self)
		}
		if !mine {
			return nil, nil
		}
	}
	rows, err := p.s.pgRows(ctx, seller)
	if err != nil {
		return nil, err
	}
	groups, _ := p.s.pgGroups(ctx, seller)
	tagOf := map[int64]string{}
	for _, g := range groups {
		tagOf[g.ID] = g.Name
	}
	tags := map[string]bool{}
	for _, id := range groupKey {
		tags[tagOf[id]] = true
	}
	users, statuses := map[int64]bool{}, map[string]bool{}
	for _, id := range f.Users {
		users[id] = true
	}
	for _, st := range f.Status {
		statuses[st] = true
	}
	after, aset, err := pgExpire(or4(f.ExpireAfter, f.ExpiredAfter))
	if err != nil {
		return nil, err
	}
	before, bset, err := pgExpire(or4(f.ExpireBefore, f.ExpiredBefore))
	if err != nil {
		return nil, err
	}
	out := rows[:0:0]
	for _, x := range rows {
		if (len(users) > 0 && !users[x.id]) || (len(statuses) > 0 && !statuses[x.status]) ||
			(aset && after > 0 && x.expire < after) || (bset && before > 0 && (x.expire == 0 || x.expire > before)) {
			continue
		}
		if len(tags) > 0 || f.HasNoGroup {
			in := false
			for _, t := range x.c.Inbounds {
				in = in || tags[t]
			}
			if !(in || (f.HasNoGroup && len(x.c.Inbounds) == 0)) {
				continue
			}
		}
		out = append(out, x)
	}
	return out, nil
}

func or4(a, b json.RawMessage) json.RawMessage {
	if len(a) > 0 {
		return a
	}
	return b
}

// bulkLimit adds amount (seconds or bytes, negative to take away) to every
// finite expiry or data limit in the selection.
func (p *pgSrv) bulkLimit(field string) pgAuthed {
	return func(w http.ResponseWriter, r *http.Request, seller string) {
		ctx := r.Context()
		var f pgBulkFilter
		if !pgDecode(w, r, &f) {
			return
		}
		rows, err := p.bulkTargets(ctx, seller, f, f.GroupIDs)
		if err != nil {
			pgFail(w, err)
			return
		}
		var list []Client
		for _, x := range rows {
			c := x.c.Client
			c.UUID, c.Secret, c.SubToken = "", "", ""
			switch {
			case field == "expire" && c.ExpiresUnix > 0:
				c.ExpiresUnix += f.Amount
			case field == "expire" && c.DurationSeconds > 0 && x.c.Usage.FirstSeen > 0:
				c.DurationSeconds = max(c.DurationSeconds+f.Amount, 1)
			case field == "data_limit" && c.VolumeBytes > 0 && c.VolumeBytes+f.Amount > 0:
				c.VolumeBytes += f.Amount
			default:
				continue
			}
			list = append(list, c)
		}
		if f.DryRun {
			pgJSON(w, map[string]any{"dry_run": true, "affected_users": len(list)})
			return
		}
		for i, c := range list {
			if err := p.s.pgSave(ctx, seller, c); err != nil {
				bulkFail(w, i, err)
				return
			}
		}
		pgJSON(w, map[string]string{"detail": fmt.Sprintf("operation has been successfuly done on %d users", len(list))})
	}
}

// bulkGroups adds or removes groups (inbounds) on the selected users.
func (p *pgSrv) bulkGroups(add bool) pgAuthed {
	return func(w http.ResponseWriter, r *http.Request, seller string) {
		ctx := r.Context()
		var f pgBulkFilter
		if !pgDecode(w, r, &f) {
			return
		}
		tags, err := p.s.pgTags(ctx, seller, f.GroupIDs)
		if err != nil || len(f.GroupIDs) == 0 {
			pgFail(w, fmt.Errorf("%w: group_ids", ErrInvalid))
			return
		}
		rows, err := p.bulkTargets(ctx, seller, f, f.HasGroupIDs)
		if err != nil {
			pgFail(w, err)
			return
		}
		var list []Client
		for _, x := range rows {
			have := map[string]bool{}
			for _, t := range x.c.Inbounds {
				have[t] = true
			}
			changed := false
			for _, t := range tags {
				if have[t] != add {
					have[t], changed = add, true
				}
			}
			if !changed {
				continue
			}
			c := x.c.Client
			c.UUID, c.Secret, c.SubToken = "", "", ""
			c.Inbounds = []string{}
			for _, t := range x.c.Inbounds {
				if have[t] {
					c.Inbounds = append(c.Inbounds, t)
					delete(have, t)
				}
			}
			for _, t := range tags {
				if have[t] {
					c.Inbounds = append(c.Inbounds, t)
				}
			}
			if len(c.Inbounds) == 0 {
				continue // a user keeps at least one group
			}
			list = append(list, c)
		}
		if f.DryRun {
			pgJSON(w, map[string]any{"dry_run": true, "affected_users": len(list)})
			return
		}
		for i, c := range list {
			if err := p.s.pgSave(ctx, seller, c); err != nil {
				bulkFail(w, i, err)
				return
			}
		}
		pgJSON(w, map[string]string{"detail": fmt.Sprintf("operation has been successfuly done on %d users", len(list))})
	}
}

// ---- user templates: the seller's shop products ----

type pgTemplate struct {
	ID                     int64          `json:"id"`
	Name                   string         `json:"name"`
	DataLimit              int64          `json:"data_limit"`
	HWIDLimit              *int           `json:"hwid_limit"`
	ExpireDuration         int64          `json:"expire_duration"`
	UsernamePrefix         *string        `json:"username_prefix"`
	UsernameSuffix         *string        `json:"username_suffix"`
	GroupIDs               []int64        `json:"group_ids"`
	ExtraSettings          map[string]any `json:"extra_settings"`
	Status                 string         `json:"status"`
	ResetUsages            bool           `json:"reset_usages"`
	OnHoldTimeout          *int64         `json:"on_hold_timeout"`
	DataLimitResetStrategy string         `json:"data_limit_reset_strategy"`
	IsDisabled             bool           `json:"is_disabled"`
}

// pgProducts lists the seller's own products that are not archived, each
// with its number (the row id).
func (s *Store) pgProducts(ctx context.Context, seller string) ([]Product, []int64, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT rowid,id FROM products WHERE seller=? AND status<>'archived' ORDER BY sort,rowid`, seller)
	if err != nil {
		return nil, nil, err
	}
	type ref struct {
		n  int64
		id string
	}
	var refs []ref
	for rows.Next() {
		var x ref
		if rows.Scan(&x.n, &x.id) == nil {
			refs = append(refs, x)
		}
	}
	rows.Close()
	by := pgPrincipal(seller)
	var list []Product
	var nums []int64
	for _, x := range refs {
		if p, err := s.ProductByID(ctx, by, x.id); err == nil {
			list, nums = append(list, p), append(nums, x.n)
		}
	}
	return list, nums, nil
}

func pgPrincipal(seller string) Principal {
	if seller == "" {
		return Principal{User: "mirza", Role: "owner"}
	}
	return Principal{User: "mirza", Role: "rep", RepID: seller}
}

// pgProduct is the seller's product with that number.
func (s *Store) pgProduct(ctx context.Context, seller string, n int64) (Product, error) {
	var id string
	if s.ro.QueryRowContext(ctx, `SELECT id FROM products WHERE rowid=? AND seller=? AND status<>'archived'`, n, seller).Scan(&id) != nil {
		return Product{}, fmt.Errorf("%w: template not found", ErrNotFound)
	}
	return s.ProductByID(ctx, pgPrincipal(seller), id)
}

func (s *Store) pgTemplateOf(ctx context.Context, seller string, pr Product, n int64, groups []pgGroup) pgTemplate {
	t := pgTemplate{ID: n, Name: pr.Name, ExpireDuration: pr.Days * 86400, GroupIDs: []int64{}, Status: "active",
		ResetUsages: true, DataLimitResetStrategy: "no_reset", IsDisabled: pr.Status != "published"}
	if pr.Plan == "volume" {
		t.DataLimit = pr.GB << 30
	}
	if pr.FirstUse {
		t.Status = "on_hold"
	}
	by := map[string]int64{}
	for _, g := range groups {
		by[g.Name] = g.ID
	}
	for _, tag := range pr.Inbounds {
		if id, ok := by[tag]; ok {
			t.GroupIDs = append(t.GroupIDs, id)
		}
	}
	return t
}

// pgTemplateIn is a template a bot sends.
type pgTemplateIn struct {
	Name           *string `json:"name"`
	DataLimit      *int64  `json:"data_limit"`
	ExpireDuration *int64  `json:"expire_duration"`
	GroupIDs       []int64 `json:"group_ids"`
	Status         *string `json:"status"`
	IsDisabled     *bool   `json:"is_disabled"`
}

func (s *Store) pgTemplatePut(ctx context.Context, seller string, pr Product, in pgTemplateIn) (Product, error) {
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return pr, fmt.Errorf("%w: name can't be empty", ErrInvalid)
		}
		pr.Name = strings.TrimSpace(*in.Name)
	}
	if in.DataLimit != nil {
		if *in.DataLimit > 0 {
			pr.Plan, pr.GB = "volume", max((*in.DataLimit+(1<<30)-1)>>30, 1)
		} else {
			pr.Plan, pr.GB = "unlimited", 0
		}
	}
	if in.ExpireDuration != nil {
		pr.Days = (*in.ExpireDuration + 86399) / 86400
	}
	if pr.Days <= 0 {
		return pr, fmt.Errorf("%w: expire_duration: a template sells a fixed term", ErrInvalid)
	}
	if in.GroupIDs != nil {
		tags, err := s.pgTags(ctx, seller, in.GroupIDs)
		if err != nil {
			return pr, err
		}
		pr.Inbounds = tags
	}
	if in.Status != nil {
		switch *in.Status {
		case "active":
			pr.FirstUse = false
		case "on_hold":
			pr.FirstUse = true
		default:
			return pr, fmt.Errorf("%w: status: active or on_hold", ErrInvalid)
		}
	}
	if in.IsDisabled != nil {
		pr.Status = map[bool]string{true: "draft", false: "published"}[*in.IsDisabled]
	}
	if err := s.PutProduct(ctx, pgPrincipal(seller), pr); err != nil {
		return pr, err
	}
	return s.ProductByID(ctx, pgPrincipal(seller), pr.ID)
}

func (s *Store) pgProductNum(ctx context.Context, id string) int64 {
	var n int64
	_ = s.ro.QueryRowContext(ctx, `SELECT rowid FROM products WHERE id=?`, id).Scan(&n)
	return n
}

// clientFromProduct makes a new client the product describes.
func (s *Store) clientFromProduct(ctx context.Context, seller, email string, pr Product) (Client, string, error) {
	c := Client{Email: email, Enabled: true, VolumeBytes: -1, MaxIPs: pr.MaxIPs, Inbounds: pr.Inbounds}
	if pr.Plan == "volume" {
		c.VolumeBytes = pr.GB << 30
	}
	if pr.FirstUse {
		c.DurationSeconds = pr.Days * 86400
	} else {
		c.ExpiresUnix = s.now().Unix() + pr.Days*86400
	}
	if len(c.Inbounds) == 0 {
		tags, err := s.pgTags(ctx, seller, nil)
		if err != nil {
			return c, "", err
		}
		c.Inbounds = tags
	}
	if len(c.Inbounds) == 0 {
		return c, "", fmt.Errorf("%w: no inbound is available for this user", ErrInvalid)
	}
	return c, pr.Plan, nil
}

// createFromTemplate makes one user from a template, charged like any other.
func (p *pgSrv) createFromTemplate(ctx context.Context, seller, username string, tmpl int64, note *string) error {
	pr, err := p.s.pgProduct(ctx, seller, tmpl)
	if err != nil {
		return err
	}
	if pr.Status != "published" {
		return fmt.Errorf("%w: this template is disabled", ErrPolicy)
	}
	if len(username) < 3 || !emailRE.MatchString(username) {
		return fmt.Errorf("%w: username: 3-128 letters, digits, . _ @ + -", ErrInvalid)
	}
	var x int
	if p.s.ro.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=?`, username).Scan(&x) == nil {
		return errPGExists
	}
	c, _, err := p.s.clientFromProduct(ctx, seller, username, pr)
	if err != nil {
		return err
	}
	if err := p.s.pgSave(ctx, seller, c); err != nil {
		return err
	}
	if note != nil {
		_ = p.s.SetClientNote(ctx, username, clip(*note, 2000))
	}
	_, _ = p.s.db.ExecContext(ctx, `UPDATE pg_api SET made=made+1 WHERE seller=?`, seller)
	return nil
}

var errPGExists = errors.New("User already exists")

// renewFromTemplate gives an existing user a template's package: a new
// period from today, usage back to zero, charged like a renewal.
func (p *pgSrv) renewFromTemplate(w http.ResponseWriter, r *http.Request, seller string, c ClientView) {
	ctx := r.Context()
	var in struct {
		UserTemplateID int64   `json:"user_template_id"`
		Note           *string `json:"note"`
	}
	if !pgDecode(w, r, &in) {
		return
	}
	if err := p.applyTemplate(ctx, seller, c.Email, in.UserTemplateID, in.Note); err != nil {
		pgFailT(w, err)
		return
	}
	p.view(w, r, seller, c.Email)
}

func (p *pgSrv) applyTemplate(ctx context.Context, seller, email string, tmpl int64, note *string) error {
	pr, err := p.s.pgProduct(ctx, seller, tmpl)
	if err != nil {
		return err
	}
	if pr.Status != "published" {
		return fmt.Errorf("%w: this template is disabled", ErrPolicy)
	}
	gb := float64(0)
	if pr.Plan == "volume" {
		gb = float64(pr.GB)
	}
	if _, err := p.s.RenewClient(ctx, pgPrincipal(seller), email, pr.Plan, gb, pr.Days); err != nil {
		return err
	}
	if len(pr.Inbounds) > 0 {
		cur, err := p.s.clientView(ctx, email)
		if err == nil && !sameSet(cur.Inbounds, pr.Inbounds) {
			c := cur.Client
			c.UUID, c.Secret, c.SubToken = "", "", ""
			c.Inbounds = pr.Inbounds
			if err := p.s.pgSave(ctx, seller, c); err != nil {
				return err
			}
		}
	}
	if note != nil {
		_ = p.s.SetClientNote(ctx, email, clip(*note, 2000))
	}
	return nil
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

// pgFailT answers a template-route error.
func pgFailT(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPGExists):
		pgErr(w, http.StatusConflict, "User already exists")
	case errors.Is(err, ErrNotFound) && strings.Contains(err.Error(), "template"):
		pgErr(w, http.StatusNotFound, "User Template not found")
	default:
		pgFail(w, err)
	}
}

// ---- routes ----

func (p *pgSrv) routes(mux *http.ServeMux, auth func(func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc) {
	s := p.s
	for _, m := range []string{"GET", "PUT", "POST", "DELETE"} {
		mux.HandleFunc(m+" /api/user/{rest...}", auth(p.user))
	}
	mux.HandleFunc("GET /api/users", auth(p.users))
	mux.HandleFunc("GET /api/users/simple", auth(p.usersSimple))
	mux.HandleFunc("GET /api/users/usage", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		p.usage(w, r, "c.rep_id=?", seller)
	}))
	mux.HandleFunc("GET /api/users/counts/{metric}", auth(p.counts))
	mux.HandleFunc("GET /api/users/expired", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		names, err := p.cleanupTargets(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		pgJSON(w, names)
	}))
	mux.HandleFunc("DELETE /api/users/expired", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		names, err := p.cleanupTargets(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		if r.URL.Query().Get("dry_run") != "true" {
			for i, e := range names {
				if err := s.pgDelete(r.Context(), seller, e); err != nil {
					bulkFail(w, i, err)
					return
				}
			}
		}
		pgJSON(w, map[string]any{"users": names, "count": len(names)})
	}))
	mux.HandleFunc("POST /api/users/reset", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		if !pgOwnerOnly(w, seller) {
			return
		}
		list, err := s.clientViews(r.Context(), "c.rep_id=?", "")
		if err != nil {
			pgFail(w, err)
			return
		}
		for _, c := range list {
			if err := s.ResetUsage(r.Context(), c.Email, "api"); err != nil {
				pgFail(w, err)
				return
			}
		}
		pgJSON(w, map[string]string{"detail": "Users successfully reset."})
	}))
	for _, a := range []string{"delete", "reset", "disable", "enable", "revoke_sub"} {
		mux.HandleFunc("POST /api/users/bulk/"+a, auth(p.bulkSelect(a)))
	}
	mux.HandleFunc("POST /api/users/bulk/expire", auth(p.bulkLimit("expire")))
	mux.HandleFunc("POST /api/users/bulk/data_limit", auth(p.bulkLimit("data_limit")))
	mux.HandleFunc("PUT /api/users/bulk/set_owner", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		if !pgOwnerOnly(w, seller) {
			return
		}
		var in struct {
			IDs           []int64 `json:"ids"`
			AdminUsername string  `json:"admin_username"`
		}
		if !pgDecode(w, r, &in) {
			return
		}
		to, err := s.pgRepOf(r.Context(), in.AdminUsername)
		if err != nil {
			pgErr(w, http.StatusNotFound, "Admin not found")
			return
		}
		names, err := s.pgPick(r.Context(), seller, in.IDs)
		if err != nil {
			pgFail(w, err)
			return
		}
		for i, e := range names {
			if err := s.TransferClient(r.Context(), e, to); err != nil {
				bulkFail(w, i, err)
				return
			}
		}
		pgJSON(w, map[string]any{"users": names, "count": len(names)})
	}))

	// from templates
	mux.HandleFunc("POST /api/user/from_template", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		var in struct {
			UserTemplateID int64   `json:"user_template_id"`
			Username       string  `json:"username"`
			Note           *string `json:"note"`
		}
		if !pgDecode(w, r, &in) {
			return
		}
		name := strings.TrimSpace(in.Username)
		if err := p.createFromTemplate(r.Context(), seller, name, in.UserTemplateID, in.Note); err != nil {
			pgFailT(w, err)
			return
		}
		p.viewCode(w, r, seller, name, http.StatusCreated)
	}))
	mux.HandleFunc("POST /api/users/bulk/from_template", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		var in struct {
			UserTemplateID int64   `json:"user_template_id"`
			Username       string  `json:"username"`
			Note           *string `json:"note"`
			Count          int     `json:"count"`
			Strategy       string  `json:"strategy"`
			StartNumber    *int    `json:"start_number"`
		}
		if !pgDecode(w, r, &in) {
			return
		}
		if in.Count <= 0 || in.Count > 500 {
			pgErr(w, http.StatusUnprocessableEntity, "count: 1-500")
			return
		}
		strategy := or2(in.Strategy, "random")
		if strategy == "sequence" && in.Username == "" {
			pgErr(w, http.StatusUnprocessableEntity, "username is required when strategy is 'sequence'")
			return
		}
		if strategy == "random" && in.Username != "" {
			pgErr(w, http.StatusUnprocessableEntity, "username must be null when strategy is 'random'")
			return
		}
		if strategy != "random" && strategy != "sequence" {
			pgErr(w, http.StatusUnprocessableEntity, "strategy: random or sequence")
			return
		}
		next := 1
		if in.StartNumber != nil {
			next = *in.StartNumber
		}
		base := strings.TrimRightFunc(in.Username, func(r rune) bool { return r >= '0' && r <= '9' })
		urls := []string{}
		for len(urls) < in.Count {
			name := strings.ToLower(randomHex(5))
			if strategy == "sequence" {
				name = base + strconv.Itoa(next)
				next++
			}
			err := p.createFromTemplate(r.Context(), seller, name, in.UserTemplateID, in.Note)
			if errors.Is(err, errPGExists) {
				if strategy == "sequence" && next > in.Count*10+1000 {
					break
				}
				continue
			}
			if err != nil {
				if len(urls) == 0 {
					pgFailT(w, err)
					return
				}
				break // credit ran out: report what was made
			}
			c, _ := s.clientView(r.Context(), name)
			urls = append(urls, subURL(p.base(r, seller), r, c.SubToken))
		}
		pgJSON(w, map[string]any{"subscription_urls": urls, "created": len(urls)})
	}))
	mux.HandleFunc("POST /api/users/bulk/apply_template", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		var in struct {
			IDs            []int64 `json:"ids"`
			UserTemplateID int64   `json:"user_template_id"`
			Note           *string `json:"note"`
		}
		if !pgDecode(w, r, &in) {
			return
		}
		if _, err := s.pgProduct(r.Context(), seller, in.UserTemplateID); err != nil {
			pgFailT(w, err)
			return
		}
		names, err := s.pgPick(r.Context(), seller, in.IDs)
		if err != nil {
			pgFail(w, err)
			return
		}
		for i, e := range names {
			if err := p.applyTemplate(r.Context(), seller, e, in.UserTemplateID, in.Note); err != nil {
				bulkFail(w, i, err)
				return
			}
		}
		pgJSON(w, map[string]any{"users": names, "count": len(names)})
	}))

	// templates
	templates := func(w http.ResponseWriter, r *http.Request, seller string) ([]pgTemplate, bool) {
		list, nums, err := s.pgProducts(r.Context(), seller)
		if err != nil {
			pgFail(w, err)
			return nil, false
		}
		groups, _ := s.pgGroups(r.Context(), seller)
		ids := map[string]bool{}
		for _, v := range pgQueryList(r, "ids") {
			ids[v] = true
		}
		out := []pgTemplate{}
		for i, pr := range list {
			if len(ids) > 0 && !ids[strconv.FormatInt(nums[i], 10)] {
				continue
			}
			out = append(out, s.pgTemplateOf(r.Context(), seller, pr, nums[i], groups))
		}
		return out, true
	}
	mux.HandleFunc("GET /api/user_templates", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		list, ok := templates(w, r, seller)
		if !ok {
			return
		}
		from, to := pgPage(r, len(list))
		pgJSON(w, list[from:to])
	}))
	mux.HandleFunc("GET /api/user_templates/simple", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		list, ok := templates(w, r, seller)
		if !ok {
			return
		}
		search := strings.ToLower(r.URL.Query().Get("search"))
		type simple struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		out := []simple{}
		for _, t := range list {
			if search == "" || strings.Contains(strings.ToLower(t.Name), search) {
				out = append(out, simple{t.ID, t.Name})
			}
		}
		for _, k := range pgQueryList(r, "sort") {
			desc := strings.HasPrefix(k, "-")
			switch strings.TrimPrefix(k, "-") {
			case "name":
				sort.SliceStable(out, func(i, j int) bool { return (strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)) != desc })
			case "id":
				sort.SliceStable(out, func(i, j int) bool { return (out[i].ID < out[j].ID) != desc })
			}
		}
		from, to := 0, len(out)
		if r.URL.Query().Get("all") != "true" {
			from, to = pgPage(r, len(out))
		}
		pgJSON(w, map[string]any{"templates": out[from:to], "total": len(out)})
	}))
	tmplNum := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			pgErr(w, http.StatusNotFound, "User Template not found")
			return 0, false
		}
		return n, true
	}
	mux.HandleFunc("GET /api/user_template/{id}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		n, ok := tmplNum(w, r)
		if !ok {
			return
		}
		pr, err := s.pgProduct(r.Context(), seller, n)
		if err != nil {
			pgFailT(w, err)
			return
		}
		groups, _ := s.pgGroups(r.Context(), seller)
		pgJSON(w, s.pgTemplateOf(r.Context(), seller, pr, n, groups))
	}))
	mux.HandleFunc("POST /api/user_template", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		var in pgTemplateIn
		if !pgDecode(w, r, &in) {
			return
		}
		if in.Name == nil || in.GroupIDs == nil {
			pgErr(w, http.StatusUnprocessableEntity, "name and group_ids are required")
			return
		}
		pr := Product{ID: "pg-" + randomHex(4), Seller: seller, Status: "published", Plan: "unlimited"}
		if in.Status == nil {
			st := "active"
			in.Status = &st
		}
		pr, err := s.pgTemplatePut(r.Context(), seller, pr, in)
		if err != nil {
			pgFailT(w, err)
			return
		}
		groups, _ := s.pgGroups(r.Context(), seller)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(s.pgTemplateOf(r.Context(), seller, pr, s.pgProductNum(r.Context(), pr.ID), groups))
	}))
	mux.HandleFunc("PUT /api/user_template/{id}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		n, ok := tmplNum(w, r)
		if !ok {
			return
		}
		pr, err := s.pgProduct(r.Context(), seller, n)
		if err != nil {
			pgFailT(w, err)
			return
		}
		var in pgTemplateIn
		if !pgDecode(w, r, &in) {
			return
		}
		if pr, err = s.pgTemplatePut(r.Context(), seller, pr, in); err != nil {
			pgFailT(w, err)
			return
		}
		groups, _ := s.pgGroups(r.Context(), seller)
		pgJSON(w, s.pgTemplateOf(r.Context(), seller, pr, n, groups))
	}))
	mux.HandleFunc("DELETE /api/user_template/{id}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		n, ok := tmplNum(w, r)
		if !ok {
			return
		}
		pr, err := s.pgProduct(r.Context(), seller, n)
		if err != nil {
			pgFailT(w, err)
			return
		}
		if _, err := s.DeleteProduct(r.Context(), pgPrincipal(seller), pr.ID); err != nil {
			pgFailT(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, a := range []string{"delete", "disable", "enable"} {
		mux.HandleFunc("POST /api/user_templates/bulk/"+a, auth(func(w http.ResponseWriter, r *http.Request, seller string) {
			var in struct {
				IDs []int64 `json:"ids"`
			}
			if !pgDecode(w, r, &in) {
				return
			}
			if len(in.IDs) == 0 {
				pgErr(w, http.StatusUnprocessableEntity, "ids: at least one template")
				return
			}
			var list []Product
			for _, n := range in.IDs {
				pr, err := s.pgProduct(r.Context(), seller, n)
				if err != nil {
					pgFailT(w, err)
					return
				}
				list = append(list, pr)
			}
			names := []string{}
			for i, pr := range list {
				var err error
				if a == "delete" {
					_, err = s.DeleteProduct(r.Context(), pgPrincipal(seller), pr.ID)
				} else {
					off := a == "disable"
					_, err = s.pgTemplatePut(r.Context(), seller, pr, pgTemplateIn{IsDisabled: &off})
				}
				if err != nil {
					bulkFail(w, i, err)
					return
				}
				names = append(names, pr.Name)
			}
			pgJSON(w, map[string]any{"templates": names, "count": len(names)})
		}))
	}

	// groups
	mux.HandleFunc("GET /api/groups/simple", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		groups, err := s.pgGroups(r.Context(), seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		search := strings.ToLower(r.URL.Query().Get("search"))
		type simple struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		out := []simple{}
		for _, g := range groups {
			if search == "" || strings.Contains(strings.ToLower(g.Name), search) {
				out = append(out, simple{g.ID, g.Name})
			}
		}
		from, to := 0, len(out)
		if r.URL.Query().Get("all") != "true" {
			from, to = pgPage(r, len(out))
		}
		pgJSON(w, map[string]any{"groups": out[from:to], "total": len(out)})
	}))
	mux.HandleFunc("GET /api/group/{id}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		groups, _ := s.pgGroups(r.Context(), seller)
		for _, g := range groups {
			if strconv.FormatInt(g.ID, 10) == r.PathValue("id") {
				pgJSON(w, g)
				return
			}
		}
		pgErr(w, http.StatusNotFound, "Group not found")
	}))
	mux.HandleFunc("POST /api/groups/bulk/add", auth(p.bulkGroups(true)))
	mux.HandleFunc("POST /api/groups/bulk/remove", auth(p.bulkGroups(false)))
	mux.HandleFunc("GET /api/inbounds", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		groups, _ := s.pgGroups(r.Context(), seller)
		tags := []string{}
		for _, g := range groups {
			tags = append(tags, g.Name)
		}
		pgJSON(w, tags)
	}))

	// admins: the owner sees its resellers as admins; a reseller sees itself.
	admins := func(r *http.Request, seller string) []map[string]any {
		out := []map[string]any{}
		if seller != "" {
			var id int64
			_ = s.ro.QueryRowContext(r.Context(), `SELECT rowid FROM reps WHERE id=?`, seller).Scan(&id)
			return append(out, map[string]any{"id": id, "username": seller, "is_sudo": false, "is_disabled": false})
		}
		rows, err := s.ro.QueryContext(r.Context(), `SELECT rowid,id,enabled,(SELECT COUNT(*) FROM clients c WHERE c.rep_id=reps.id) FROM reps ORDER BY rowid`)
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var name string
			var on bool
			var users int
			if rows.Scan(&id, &name, &on, &users) == nil {
				out = append(out, map[string]any{"id": id, "username": name, "is_sudo": false, "is_disabled": !on, "total_users": users})
			}
		}
		return out
	}
	mux.HandleFunc("GET /api/admins", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		list := admins(r, seller)
		from, to := pgPage(r, len(list))
		pgJSON(w, map[string]any{"admins": list[from:to], "total": len(list)})
	}))
	mux.HandleFunc("GET /api/admins/simple", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		list := admins(r, seller)
		out := []map[string]any{}
		for _, a := range list {
			out = append(out, map[string]any{"id": a["id"], "username": a["username"]})
		}
		pgJSON(w, map[string]any{"admins": out, "total": len(out)})
	}))
}

// pgSubRoutes adds PasarGuard's subscription extras next to /sub/{token}.
func pgSubRoutes(s *Store, mux *http.ServeMux, subBase string) {
	p := &pgSrv{s: s, base: func(*http.Request, string) string { return subBase }}
	find := func(w http.ResponseWriter, r *http.Request) (ClientView, bool) {
		email, err := s.EmailBySubToken(r.Context(), r.PathValue("token"))
		if err != nil {
			// A link from a Marzban/PasarGuard panel moved here keeps working.
			if tok, ferr := s.foreignSubToken(r.Context(), r.PathValue("token")); ferr == nil {
				email, err = s.EmailBySubToken(r.Context(), tok)
			}
		}
		if err != nil {
			http.NotFound(w, r)
			return ClientView{}, false
		}
		c, err := s.clientView(r.Context(), email)
		if err != nil {
			http.NotFound(w, r)
			return ClientView{}, false
		}
		return c, true
	}
	mux.HandleFunc("GET /sub/{token}/info", func(w http.ResponseWriter, r *http.Request) {
		c, ok := find(w, r)
		if !ok {
			return
		}
		groups, _ := s.pgGroups(r.Context(), c.RepID)
		u := s.pgView(r.Context(), r, subBase, c, groups, false)
		// A shared link must not show credentials or the seller's notes.
		pgJSON(w, map[string]any{"id": u.ID, "username": u.Username, "status": u.Status, "used_traffic": u.UsedTraffic,
			"lifetime_used_traffic": u.LifetimeUsedTraffic, "data_limit": u.DataLimit, "data_limit_reset_strategy": u.DataLimitResetStrategy,
			"expire": u.Expire, "on_hold_expire_duration": u.OnHoldExpireDuration, "on_hold_timeout": u.OnHoldTimeout,
			"group_ids": u.GroupIDs, "hwid_limit": u.HWIDLimit, "next_plan": u.NextPlan, "online_at": u.OnlineAt,
			"created_at": u.CreatedAt, "edit_at": u.EditAt, "sub_updated_at": u.SubUpdatedAt, "sub_last_user_agent": u.SubLastUserAgent})
	})
	mux.HandleFunc("GET /sub/{token}/usage", func(w http.ResponseWriter, r *http.Request) {
		c, ok := find(w, r)
		if !ok {
			return
		}
		p.usage(w, r, "c.email=?", c.Email)
	})
	mux.HandleFunc("GET /sub/{token}/{client_type}", func(w http.ResponseWriter, r *http.Request) {
		c, ok := find(w, r)
		if !ok {
			return
		}
		// The same device limit as /sub/{token}: an app's configs count as a device.
		if err := s.CheckDevice(r.Context(), c.Email, r.Header.Get("X-Hwid"), r.Header.Get("X-Device-Os"), r.Header.Get("X-Device-Model"), r.UserAgent()); err != nil {
			if errors.Is(err, ErrDeviceLimit) {
				http.Error(w, "device limit reached: remove a device in the panel or ask your seller", http.StatusForbidden)
				return
			}
			pgFail(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		p.subscription(w, r, c.Email, r.PathValue("client_type"))
	})
}
