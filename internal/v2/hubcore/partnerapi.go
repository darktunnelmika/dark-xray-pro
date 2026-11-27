package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Partner API routes, all under /api/v2/partner with Bearer dp_<id>_<secret>
// (or X-API-Key). In brackets: the scope a route needs.
//
//	GET    /api/v2/partner                         the key: limits, used, left, scopes
//	GET    /api/v2/partner/packages                packages it may sell
//	GET    /api/v2/partner/stats?days=30           numbers over all its panels [panels:read]
//	GET    /api/v2/partner/ledger?before=&limit=   credit added or taken back, all panels [panels:read]
//	POST   /api/v2/partner/reps                    make a panel [panels:write, credit:add for credit]
//	GET    /api/v2/partner/reps                    its panels: ?q= &enabled= &external_ref= &limit= &offset= (X-Total-Count) [panels:read]
//	GET    /api/v2/partner/reps/{id}               one panel [panels:read]
//	PATCH  /api/v2/partner/reps/{id}               name, note, external_ref, policy, telegram_ids, low_credit_gb, enabled [panels:write]
//	DELETE /api/v2/partner/reps/{id}?clients=1     delete, with its customers when clients=1 [panels:delete]
//	POST   /api/v2/partner/reps/{id}/credit        add credit {credit_gb, units, note} [credit:add]
//	POST   /api/v2/partner/reps/{id}/credit/revoke take unused credit back {credit_gb, units, note} [credit:revoke]
//	GET    /api/v2/partner/reps/{id}/ledger        its credit history [panels:read]
//	GET    /api/v2/partner/reps/{id}/overview      its dashboard numbers [panels:read]
//	GET    /api/v2/partner/reps/{id}/clients       its customers: ?status= &q= &limit= &offset= [clients:read]
//	POST   /api/v2/partner/reps/{id}/enable        turn on [panels:write]
//	POST   /api/v2/partner/reps/{id}/disable       turn off: no sign-in, no sales; customers keep working [panels:write]
//	POST   /api/v2/partner/reps/{id}/password      a new password, random or {password} [panels:write]
//	POST   /api/v2/partner/reps/{id}/sessions/revoke  sign the panel out everywhere [panels:write]
//	POST   /api/v2/partner/reps/{id}/login-link    a one-time sign-in link for 10 minutes [panels:write]
//	GET    /api/v2/partner/events?after=&types=    its events [events]
//	GET|PUT|DELETE /api/v2/partner/webhook, POST …/webhook/test, GET …/webhook/deliveries [events]

// partnerEvents are the events a partner key receives.
var partnerEvents = []string{"panel.created", "panel.updated", "panel.enabled", "panel.disabled", "panel.deleted",
	"panel.credit_added", "panel.credit_revoked", "panel.credit_low", "panel.password_reset"}

func init() { apiEventSources = append(apiEventSources, lowCreditEvents) }

// lowCreditEvents tells a reseller (credit.low) and the partner that made
// it (panel.credit_low) when its credit falls under its threshold.
func lowCreditEvents(ctx context.Context, s *Store) {
	for _, l := range s.DueLowCredit(ctx, "api", "") {
		data := map[string]any{"id": l.RepID, "name": l.Name, "credit_gb": roundGB(float64(l.Credit) / gibF), "threshold_gb": l.Limit >> 30}
		s.emitAPIEvent(ctx, repTarget(l.RepID), "credit.low", data)
		var key, ref string
		if s.ro.QueryRowContext(ctx, `SELECT key_id,external_ref FROM partner_reps WHERE rep_id=?`, l.RepID).Scan(&key, &ref) == nil {
			data["external_ref"] = ref
			s.emitAPIEvent(ctx, partnerTarget(key), "panel.credit_low", data)
		}
	}
}

// PartnerPanel is a reseller panel as the partner sees it.
type PartnerPanel struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Username    string    `json:"username"`
	Enabled     bool      `json:"enabled"`
	CreditGB    float64   `json:"credit_gb"`
	Units       int64     `json:"units"`
	Clients     int       `json:"clients"`
	UsedGB      float64   `json:"used_gb"`
	PanelURL    string    `json:"panel_url"`
	ExternalRef string    `json:"external_ref"`
	Note        string    `json:"note"`
	Package     string    `json:"package"`
	Created     int64     `json:"created_unix"`
	Policy      APIPolicy `json:"policy"`
	TelegramIDs []int64   `json:"telegram_ids"`
	LowCreditGB int64     `json:"low_credit_gb"`      // warn under this credit; 0 = off
	Password    string    `json:"password,omitempty"` // only when just made or reset
	Message     string    `json:"message,omitempty"`  // ready-to-send text, only with a password
}

// panelQuery picks a key's panels.
type panelQuery struct {
	ID, Q, ExternalRef string
	Enabled            *bool
	Page               listPage // Limit 0: all
}

func likeArg(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// partnerPanels lists a key's panels, newest first, and how many match.
func (s *Store) partnerPanels(ctx context.Context, keyID string, q panelQuery) ([]PartnerPanel, int, error) {
	where := []string{"p.key_id=?"}
	args := []any{keyID}
	if q.ID != "" {
		where, args = append(where, "r.id=?"), append(args, q.ID)
	}
	if q.ExternalRef != "" {
		where, args = append(where, "p.external_ref=?"), append(args, q.ExternalRef)
	}
	if q.Enabled != nil {
		where, args = append(where, "r.enabled=?"), append(args, *q.Enabled)
	}
	if t := strings.TrimSpace(q.Q); t != "" {
		l := likeArg(t)
		where = append(where, `(r.id LIKE ? ESCAPE '\' OR r.name LIKE ? ESCAPE '\' OR p.external_ref LIKE ? ESCAPE '\' OR p.note LIKE ? ESCAPE '\'
OR EXISTS(SELECT 1 FROM admins a WHERE a.role='rep' AND a.rep_id=r.id AND a.username LIKE ? ESCAPE '\'))`)
		args = append(args, l, l, l, l, l)
	}
	from := ` FROM partner_reps p JOIN reps r ON r.id=p.rep_id WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Page.Limit
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT r.id,r.name,r.enabled,r.volume_credit,r.unlimited_units,
(SELECT COUNT(*) FROM clients c WHERE c.rep_id=r.id),
r.used_base+COALESCE((SELECT SUM(u.up+u.down) FROM client_usage u JOIN clients c ON c.email=u.email WHERE c.rep_id=r.id),0),
p.external_ref,p.note,p.package,p.created_unix,
COALESCE((SELECT a.username FROM admins a WHERE a.role='rep' AND a.rep_id=r.id ORDER BY a.created_unix,a.username LIMIT 1),''),
`+policyCols+from+` ORDER BY p.created_unix DESC, r.id LIMIT ? OFFSET ?`, append(args, limit, q.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	out := []PartnerPanel{}
	for rows.Next() {
		var p PartnerPanel
		var credit, used int64
		pol, err := scanPolicy(rows, &p.ID, &p.Name, &p.Enabled, &credit, &p.Units, &p.Clients, &used, &p.ExternalRef, &p.Note, &p.Package, &p.Created, &p.Username)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		p.CreditGB, p.UsedGB, p.Policy = roundGB(float64(credit)/gibF), roundGB(float64(used)/gibF), apiPolicyOf(pol)
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].TelegramIDs = s.TgRepOwners(ctx, out[i].ID)
		out[i].LowCreditGB = s.RepAlertGB(ctx, out[i].ID)
	}
	return out, total, nil
}

// partnerPanel is one of a key's panels.
func (s *Store) partnerPanel(ctx context.Context, keyID, id string) (PartnerPanel, error) {
	if id == "" {
		return PartnerPanel{}, ErrNotFound
	}
	list, _, err := s.partnerPanels(ctx, keyID, panelQuery{ID: id})
	if err != nil {
		return PartnerPanel{}, err
	}
	if len(list) == 0 {
		return PartnerPanel{}, ErrNotFound
	}
	return list[0], nil
}

// partnerPackages are the reseller-sale packages a key may sell.
func (s *Store) partnerPackages(ctx context.Context, k PartnerKey) []RepPackage {
	all := s.RepSale(ctx).Packages
	if len(k.Packages) == 0 {
		return all
	}
	out := []RepPackage{}
	for _, p := range all {
		for _, id := range k.Packages {
			if p.ID == id {
				out = append(out, p)
			}
		}
	}
	return out
}

// partnerBase is the policy a key's new panel starts from and can never
// be loosened past: the package's, the template reseller's, or the default.
func (s *Store) partnerBase(ctx context.Context, k PartnerKey, pkg *RepPackage) RepPolicy {
	base := DefaultRepPolicy
	if k.Template != "" {
		if pol, err := s.RepPolicyOf(ctx, k.Template); err == nil {
			base = pol
		}
	}
	if pkg != nil {
		pp := pkg.Policy()
		pp.PriceGB, pp.PriceUnit = base.PriceGB, base.PriceUnit
		base = pp
	}
	base.Inbounds = append([]string{}, base.Inbounds...)
	return base
}

// partnerCeiling is the policy a panel was made with (its limit for later
// changes); panels made before it was kept use the key's base.
func (s *Store) partnerCeiling(ctx context.Context, k PartnerKey, repID string) RepPolicy {
	var raw string
	_ = s.ro.QueryRowContext(ctx, `SELECT ceiling FROM partner_reps WHERE rep_id=? AND key_id=?`, repID, k.ID).Scan(&raw)
	var p RepPolicy
	if raw != "" && json.Unmarshal([]byte(raw), &p) == nil {
		return p
	}
	return s.partnerBase(ctx, k, nil)
}

// panelOrigin is the scheme and host of the panel address.
func (s *Store) panelOrigin(r *http.Request) string {
	u, err := url.Parse(s.panelAddress(r))
	if err != nil {
		return strings.TrimRight(publicBase("", r), "/")
	}
	return u.Scheme + "://" + u.Host
}

func partnerMessage(url, user, pass, name string) string {
	return fmt.Sprintf("پنل نمایندگی %s آماده است ✅\n\n🌐 آدرس پنل:\n%s\n\n👤 نام کاربری: %s\n🔑 رمز: %s\n\nبعد از اولین ورود، رمز را از تنظیمات عوض کنید.", name, url, user, pass)
}

// partnerActor names who made a ledger row for a partner: itself, the
// panel's own logins and API keys by name, the owner as "owner".
func partnerActor(actor string, logins []string) string {
	if strings.HasPrefix(actor, "partner:") || strings.HasPrefix(actor, "key:") {
		return actor
	}
	for _, l := range logins {
		if l == actor {
			return actor
		}
	}
	return "owner"
}

// partnerLedger lists credit movements of a key's panels, newest first:
// one panel (repID) or all of them, then only additions and take-backs.
func (s *Store) partnerLedger(ctx context.Context, keyID, repID string, before int64, limit int) ([]APILedgerRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if before <= 0 {
		before = 1 << 62
	}
	q := `SELECT id,unix,rep_id,actor,kind,client_email,volume_delta,units_delta,note FROM credit_ledger WHERE kind<>'payment' AND id<? AND `
	args := []any{before}
	if repID != "" {
		q += `rep_id=?`
		args = append(args, repID)
	} else {
		q += `kind='adjust' AND rep_id IN (SELECT rep_id FROM partner_reps WHERE key_id=?)`
		args = append(args, keyID)
	}
	rows, err := s.ro.QueryContext(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	var list []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.Unix, &e.RepID, &e.Actor, &e.Kind, &e.ClientEmail, &e.VolumeDelta, &e.UnitsDelta, &e.Note); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, e)
	}
	rows.Close()
	logins := map[string][]string{}
	out := []APILedgerRow{}
	for _, e := range list {
		if _, ok := logins[e.RepID]; !ok {
			logins[e.RepID] = s.RepLogins(ctx, e.RepID)
		}
		row := apiLedgerRow(e, partnerActor(e.Actor, logins[e.RepID]))
		if repID != "" {
			row.Panel = ""
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// PartnerStats sums a key's panels.
type PartnerStats struct {
	Panels   int     `json:"panels"`
	Enabled  int     `json:"enabled"`
	Clients  int     `json:"clients"`
	UsedGB   float64 `json:"used_gb"`
	CreditGB float64 `json:"credit_gb"` // credit left in its panels now
	Units    int64   `json:"units"`
	// Over the last Days days:
	Days            int     `json:"days"`
	NewPanels       int     `json:"new_panels"`
	CreditAddedGB   float64 `json:"credit_added_gb"`
	UnitsAdded      int64   `json:"units_added"`
	CreditRevokedGB float64 `json:"credit_revoked_gb"`
	UnitsRevoked    int64   `json:"units_revoked"`
	SoldGB          float64 `json:"sold_gb"` // volume its panels sold to customers
	SoldUnits       int64   `json:"sold_units"`
	RefundedGB      float64 `json:"refunded_gb"`
	RefundedUnits   int64   `json:"refunded_units"`
}

func (s *Store) partnerStats(ctx context.Context, keyID string, days int) (PartnerStats, error) {
	if days <= 0 || days > 3650 {
		days = 30
	}
	st := PartnerStats{Days: days}
	since := s.now().Unix() - int64(days)*86400
	var credit, used int64
	err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(r.enabled),0),COALESCE(SUM(r.volume_credit),0),COALESCE(SUM(r.unlimited_units),0),
COALESCE(SUM(p.created_unix>=?),0),
COALESCE(SUM((SELECT COUNT(*) FROM clients c WHERE c.rep_id=r.id)),0),
COALESCE(SUM(r.used_base+COALESCE((SELECT SUM(u.up+u.down) FROM client_usage u JOIN clients c ON c.email=u.email WHERE c.rep_id=r.id),0)),0)
FROM partner_reps p JOIN reps r ON r.id=p.rep_id WHERE p.key_id=?`, since, keyID).
		Scan(&st.Panels, &st.Enabled, &credit, &st.Units, &st.NewPanels, &st.Clients, &used)
	if err != nil {
		return st, err
	}
	var added, revoked, sold, refunded int64
	err = s.ro.QueryRowContext(ctx, `SELECT
COALESCE(SUM(CASE WHEN kind='adjust' AND volume_delta>0 THEN volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='adjust' AND units_delta>0 THEN units_delta END),0),
COALESCE(SUM(CASE WHEN kind='adjust' AND volume_delta<0 THEN -volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='adjust' AND units_delta<0 THEN -units_delta END),0),
COALESCE(SUM(CASE WHEN kind='charge' THEN -volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='charge' THEN -units_delta END),0),
COALESCE(SUM(CASE WHEN kind='refund' THEN volume_delta END),0),
COALESCE(SUM(CASE WHEN kind='refund' THEN units_delta END),0)
FROM credit_ledger WHERE unix>=? AND rep_id IN (SELECT rep_id FROM partner_reps WHERE key_id=?)`, since, keyID).
		Scan(&added, &st.UnitsAdded, &revoked, &st.UnitsRevoked, &sold, &st.SoldUnits, &refunded, &st.RefundedUnits)
	gb := func(b int64) float64 { return roundGB(float64(b) / gibF) }
	st.CreditGB, st.UsedGB = gb(credit), gb(used)
	st.CreditAddedGB, st.CreditRevokedGB, st.SoldGB, st.RefundedGB = gb(added), gb(revoked), gb(sold), gb(refunded)
	return st, err
}

// APIOverview is a reseller's dashboard numbers.
type APIOverview struct {
	ID           string           `json:"id"`
	CreditGB     float64          `json:"credit_gb"`
	Units        int64            `json:"units"`
	UsedGB       float64          `json:"used_gb"`
	Clients      int              `json:"clients"`
	Active       int              `json:"active"`
	Expiring     int              `json:"expiring"` // ends within 3 days or under 10% volume left
	Ended        int              `json:"ended"`
	Disabled     int              `json:"disabled"`
	Online       int              `json:"online"`
	MonthSoldGB  float64          `json:"month_sold_gb"`
	MonthBackGB  float64          `json:"month_back_gb"`
	MonthTopupGB float64          `json:"month_topup_gb"`
	MonthSales   int              `json:"month_sales"`
	LastLogin    int64            `json:"last_login"`
	Days         []APIOverviewDay `json:"days"` // the last 30, oldest first
}

type APIOverviewDay struct {
	Day       string  `json:"day"`
	SoldGB    float64 `json:"sold_gb"`
	BackGB    float64 `json:"back_gb"`
	TrafficGB float64 `json:"traffic_gb"`
}

func (s *Store) apiOverview(ctx context.Context, repID string) (APIOverview, error) {
	list, err := s.RepOverviews(ctx, repID, true)
	if err != nil {
		return APIOverview{}, err
	}
	if len(list) == 0 {
		return APIOverview{}, ErrNotFound
	}
	o := list[0]
	gb := func(b int64) float64 { return roundGB(float64(b) / gibF) }
	out := APIOverview{ID: o.RepID, CreditGB: gb(o.VolumeCredit), Units: o.UnlimitedUnits, UsedGB: gb(o.UsedBytes), Clients: o.Clients,
		Active: o.Active, Expiring: o.Expiring, Ended: o.Ended, Disabled: o.Disabled, Online: o.Online, MonthSoldGB: gb(o.MonthSold),
		MonthBackGB: gb(o.MonthBack), MonthTopupGB: gb(o.MonthTopup), MonthSales: o.MonthSales, LastLogin: o.LastLogin, Days: []APIOverviewDay{}}
	for _, d := range o.Days {
		out.Days = append(out.Days, APIOverviewDay{Day: d.Day, SoldGB: gb(d.Sold), BackGB: gb(d.Back), TrafficGB: gb(d.Traffic)})
	}
	return out, nil
}

// endRepSessions signs every login of a reseller out.
func (s *Store) endRepSessions(ctx context.Context, repID string) (int64, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE username IN (SELECT username FROM admins WHERE role='rep' AND rep_id=?)`, repID)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func panelEvent(p PartnerPanel, extra map[string]any) map[string]any {
	m := map[string]any{"id": p.ID, "name": p.Name, "username": p.Username, "external_ref": p.ExternalRef,
		"credit_gb": p.CreditGB, "units": p.Units, "enabled": p.Enabled}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func partnerRoutes(s *Store, mux *http.ServeMux) {
	auth := s.partnerCaller
	route := func(pattern, scope string, h extHandler) { mux.HandleFunc(pattern, s.ext(auth, scope, h)) }
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, ErrNotFound):
			apiErr(w, http.StatusNotFound, codeNotFound, "panel not found")
		case errors.Is(err, ErrInvalid):
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, errDetail(err))
		case errors.Is(err, ErrPolicy):
			apiErr(w, http.StatusUnprocessableEntity, codePolicy, errDetail(err))
		default:
			apiFail(w, err)
		}
	}
	limitUsed := func(w http.ResponseWriter) {
		apiErr(w, http.StatusForbidden, codeCreditLimit, "this key's credit limit is used up")
	}
	needScope := func(w http.ResponseWriter, c apiCaller, scope string) bool {
		if c.can(scope) {
			return true
		}
		apiErr(w, http.StatusForbidden, codeScopeMissing, "this key does not have the "+scope+" permission")
		return false
	}
	key := func(w http.ResponseWriter, r *http.Request, c apiCaller) (PartnerKey, bool) {
		k, err := s.partnerKey(r.Context(), c.KeyID)
		if err != nil {
			apiErr(w, http.StatusUnauthorized, codeUnauthorized, errKeyInvalid.Error())
			return k, false
		}
		return k, true
	}
	// one reads the panel in the path, with its sign-in address.
	one := func(w http.ResponseWriter, r *http.Request, c apiCaller) (PartnerPanel, bool) {
		p, err := s.partnerPanel(r.Context(), c.KeyID, r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return p, false
		}
		p.PanelURL = s.panelAddress(r)
		return p, true
	}
	answer := func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		if p, ok := one(w, r, c); ok {
			apiJSON(w, http.StatusOK, p)
		}
	}
	type creditIn struct {
		CreditGB float64 `json:"credit_gb"`
		Units    int64   `json:"units"`
		Note     string  `json:"note"`
	}
	readCredit := func(w http.ResponseWriter, r *http.Request, verb string) (creditIn, bool) {
		var in creditIn
		if !readJSON(w, r, &in) {
			return in, false
		}
		if in.CreditGB < 0 || in.Units < 0 || (in.CreditGB == 0 && in.Units == 0) || in.CreditGB > 1e6 || in.Units > 1e6 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "credit_gb or units: more than 0 (credit can only be "+verb+")")
			return in, false
		}
		if len(in.Note) > 200 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "note: up to 200 characters")
			return in, false
		}
		return in, true
	}

	route("GET /api/v2/partner", "", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		if k, ok := key(w, r, c); ok {
			k.LastIP = ""
			apiJSON(w, http.StatusOK, k)
		}
	})
	route("GET /api/v2/partner/packages", "", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		k, ok := key(w, r, c)
		if !ok {
			return
		}
		type pkgOut struct {
			ID     string    `json:"id"`
			Name   string    `json:"name"`
			GB     int64     `json:"credit_gb"`
			Units  int64     `json:"units"`
			Price  int64     `json:"price"` // the owner's price in toman, for reference
			Policy APIPolicy `json:"policy"`
		}
		out := []pkgOut{}
		for _, p := range s.partnerPackages(r.Context(), k) {
			out = append(out, pkgOut{ID: p.ID, Name: p.Name, GB: p.GB, Units: p.Units, Price: p.Price, Policy: apiPolicyOf(s.partnerBase(r.Context(), k, &p))})
		}
		apiJSON(w, http.StatusOK, out)
	})
	route("GET /api/v2/partner/stats", scopePanelsRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		st, err := s.partnerStats(r.Context(), c.KeyID, days)
		if err != nil {
			fail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, st)
	})
	ledger := func(w http.ResponseWriter, r *http.Request, c apiCaller, repID string) {
		q := r.URL.Query()
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		list, err := s.partnerLedger(r.Context(), c.KeyID, repID, before, limit)
		if err != nil {
			fail(w, err)
			return
		}
		var next int64
		if len(list) > 0 {
			next = list[len(list)-1].ID
		}
		apiJSON(w, http.StatusOK, map[string]any{"items": list, "next_before": next})
	}
	route("GET /api/v2/partner/ledger", scopePanelsRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		ledger(w, r, c, "")
	})

	route("GET /api/v2/partner/reps", scopePanelsRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		qv := r.URL.Query()
		q := panelQuery{Q: qv.Get("q"), ExternalRef: qv.Get("external_ref"), Page: readPage(r, 0, 500)}
		if e := qv.Get("enabled"); e != "" {
			on := e == "1" || e == "true"
			q.Enabled = &on
		}
		list, total, err := s.partnerPanels(r.Context(), c.KeyID, q)
		if err != nil {
			fail(w, err)
			return
		}
		u := s.panelAddress(r)
		for i := range list {
			list[i].PanelURL = u
		}
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
		apiJSON(w, http.StatusOK, list)
	})
	route("GET /api/v2/partner/reps/{id}", scopePanelsRead, answer)
	route("POST /api/v2/partner/reps", scopePanelsWrite, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		var in struct {
			ID          string       `json:"id"`
			Name        string       `json:"name"`
			Username    string       `json:"username"`
			Password    string       `json:"password"`
			CreditGB    float64      `json:"credit_gb"`
			Units       int64        `json:"units"`
			Package     string       `json:"package"`
			Policy      *APIPolicyIn `json:"policy"`
			TelegramIDs []int64      `json:"telegram_ids"`
			ExternalRef string       `json:"external_ref"`
			Note        string       `json:"note"`
			LowCreditGB *int64       `json:"low_credit_gb"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		ctx := r.Context()
		in.ExternalRef, in.Note, in.Package = strings.TrimSpace(in.ExternalRef), strings.TrimSpace(in.Note), strings.TrimSpace(in.Package)
		if in.CreditGB < 0 || in.Units < 0 || in.CreditGB > 1e6 || in.Units > 1e6 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "credit cannot be negative")
			return
		}
		if len(in.ExternalRef) > 128 || len(in.Note) > 500 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "external_ref: up to 128 characters, note: up to 500")
			return
		}
		k, ok := key(w, r, c)
		if !ok {
			return
		}
		var pkg *RepPackage
		if in.Package != "" {
			if in.CreditGB > 0 || in.Units > 0 {
				apiErr(w, http.StatusUnprocessableEntity, codeValidation, "give a package or credit_gb/units, not both")
				return
			}
			for _, p := range s.partnerPackages(ctx, k) {
				if p.ID == in.Package {
					p := p
					pkg = &p
				}
			}
			if pkg == nil {
				apiErr(w, http.StatusUnprocessableEntity, codeValidation, "unknown package "+strconv.Quote(in.Package))
				return
			}
			in.CreditGB, in.Units = float64(pkg.GB), pkg.Units
		}
		if (in.CreditGB > 0 || in.Units > 0) && !needScope(w, c, scopeCreditAdd) {
			return
		}
		if in.ExternalRef != "" {
			var other string
			if s.ro.QueryRowContext(ctx, `SELECT rep_id FROM partner_reps WHERE key_id=? AND external_ref=?`, k.ID, in.ExternalRef).Scan(&other) == nil {
				apiErr(w, http.StatusConflict, codeConflict, "panel "+other+" already has this external_ref")
				return
			}
		}
		base := s.partnerBase(ctx, k, pkg)
		pol, err := in.Policy.over(base)
		if err == nil {
			pol, err = s.clampPolicy(ctx, pol, base)
		}
		if err != nil {
			fail(w, err)
			return
		}
		in.ID = strings.ToLower(strings.TrimSpace(in.ID))
		if in.ID == "" {
			for range 5 {
				if id := "p" + randomHex(3); s.RepNameFree(ctx, id) {
					in.ID = id
					break
				}
			}
		}
		if in.Password == "" {
			in.Password = RandomPassword()
		}
		bytes := int64(in.CreditGB * gibF)
		if !s.partnerGive(ctx, k.ID, bytes, in.Units) {
			limitUsed(w)
			return
		}
		rc := RepCreate{ID: in.ID, Name: in.Name, Username: in.Username, Password: in.Password, CreditGB: in.CreditGB, Units: in.Units,
			Policy: &pol, TgOwners: in.TelegramIDs}
		if err := s.CreateRep(ctx, "partner:"+k.Name, rc); err != nil {
			s.partnerUngive(ctx, k.ID, bytes, in.Units)
			fail(w, err)
			return
		}
		ceiling, _ := json.Marshal(base)
		pkgID := ""
		if pkg != nil {
			pkgID = pkg.ID
		}
		_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO partner_reps(rep_id,key_id,created_unix,external_ref,note,package,ceiling) VALUES(?,?,?,?,?,?,?)`,
			rc.ID, k.ID, s.now().Unix(), in.ExternalRef, in.Note, pkgID, string(ceiling))
		if in.LowCreditGB != nil {
			_ = s.SetRepAlertGB(ctx, rc.ID, *in.LowCreditGB)
		}
		p, err := s.partnerPanel(ctx, k.ID, rc.ID)
		if err != nil {
			fail(w, err)
			return
		}
		p.PanelURL, p.Password = s.panelAddress(r), strings.TrimSpace(in.Password)
		p.Message = partnerMessage(p.PanelURL, p.Username, p.Password, p.Name)
		s.emitAPIEvent(ctx, partnerTarget(k.ID), "panel.created", panelEvent(p, map[string]any{"package": p.Package}))
		apiJSON(w, http.StatusOK, p)
	})
	route("PATCH /api/v2/partner/reps/{id}", scopePanelsWrite, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		var in struct {
			Name        *string      `json:"name"`
			Note        *string      `json:"note"`
			ExternalRef *string      `json:"external_ref"`
			Enabled     *bool        `json:"enabled"`
			Policy      *APIPolicyIn `json:"policy"`
			TelegramIDs *[]int64     `json:"telegram_ids"`
			LowCreditGB *int64       `json:"low_credit_gb"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		ctx := r.Context()
		k, ok := key(w, r, c)
		if !ok {
			return
		}
		name, note, ref, enabled := p.Name, p.Note, p.ExternalRef, p.Enabled
		if in.Name != nil {
			if name = strings.TrimSpace(*in.Name); name == "" || len(name) > 64 {
				apiErr(w, http.StatusUnprocessableEntity, codeValidation, "name: 1 to 64 characters")
				return
			}
		}
		if in.Note != nil {
			note = strings.TrimSpace(*in.Note)
		}
		if in.ExternalRef != nil {
			ref = strings.TrimSpace(*in.ExternalRef)
		}
		if len(ref) > 128 || len(note) > 500 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "external_ref: up to 128 characters, note: up to 500")
			return
		}
		if ref != "" && ref != p.ExternalRef {
			var other string
			if s.ro.QueryRowContext(ctx, `SELECT rep_id FROM partner_reps WHERE key_id=? AND external_ref=? AND rep_id<>?`, k.ID, ref, p.ID).Scan(&other) == nil {
				apiErr(w, http.StatusConflict, codeConflict, "panel "+other+" already has this external_ref")
				return
			}
		}
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		if in.Policy != nil {
			cur, err := s.RepPolicyOf(ctx, p.ID)
			if err == nil {
				cur, err = in.Policy.over(cur)
			}
			if err == nil {
				cur, err = s.clampPolicy(ctx, cur, s.partnerCeiling(ctx, k, p.ID))
			}
			if err == nil {
				err = s.SetRepPolicy(ctx, p.ID, cur)
			}
			if err != nil {
				fail(w, err)
				return
			}
		}
		if in.TelegramIDs != nil {
			if err := s.SetTgRepOwners(ctx, p.ID, *in.TelegramIDs); err != nil {
				fail(w, err)
				return
			}
		}
		if in.LowCreditGB != nil {
			if err := s.SetRepAlertGB(ctx, p.ID, *in.LowCreditGB); err != nil {
				fail(w, err)
				return
			}
		}
		if name != p.Name || enabled != p.Enabled {
			if err := s.PutRep(ctx, Rep{ID: p.ID, Name: name, Enabled: enabled}); err != nil {
				fail(w, err)
				return
			}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE partner_reps SET note=?, external_ref=? WHERE rep_id=? AND key_id=?`, note, ref, p.ID, k.ID); err != nil {
			fail(w, err)
			return
		}
		np, ok := one(w, r, c)
		if !ok {
			return
		}
		s.emitAPIEvent(ctx, partnerTarget(k.ID), "panel.updated", panelEvent(np, nil))
		if np.Enabled != p.Enabled {
			ev := "panel.disabled"
			if np.Enabled {
				ev = "panel.enabled"
			}
			s.emitAPIEvent(ctx, partnerTarget(k.ID), ev, panelEvent(np, nil))
		}
		apiJSON(w, http.StatusOK, np)
	})
	route("DELETE /api/v2/partner/reps/{id}", scopePanelsDelete, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		ctx := r.Context()
		withClients := r.URL.Query().Get("clients") == "1" || r.URL.Query().Get("clients") == "true"
		var leftBytes, leftUnits int64
		_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_credit),0),COALESCE(SUM(unlimited_units),0) FROM reps WHERE id=? OR parent_id=?`, p.ID, p.ID).
			Scan(&leftBytes, &leftUnits)
		n, err := s.DeleteRep(ctx, p.ID, withClients)
		if err != nil {
			if !withClients && errors.Is(err, ErrInvalid) && n > 0 {
				apiErr(w, http.StatusConflict, codeConflict, fmt.Sprintf("the panel still has %d customers; delete with ?clients=1 to remove them too", n))
				return
			}
			fail(w, err)
			return
		}
		_, _ = s.db.ExecContext(ctx, `DELETE FROM partner_reps WHERE rep_id=?`, p.ID)
		// Credit never used goes back to what the key may hand out.
		s.partnerUngive(ctx, c.KeyID, leftBytes, leftUnits)
		out := map[string]any{"ok": true, "id": p.ID, "clients_deleted": n, "released_gb": roundGB(float64(leftBytes) / gibF), "released_units": leftUnits}
		s.emitAPIEvent(ctx, partnerTarget(c.KeyID), "panel.deleted", panelEvent(p, out))
		apiJSON(w, http.StatusOK, out)
	})
	route("POST /api/v2/partner/reps/{id}/credit", scopeCreditAdd, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		in, ok := readCredit(w, r, "added")
		if !ok {
			return
		}
		ctx := r.Context()
		bytes := int64(in.CreditGB * gibF)
		if !s.partnerGive(ctx, c.KeyID, bytes, in.Units) {
			limitUsed(w)
			return
		}
		note := "partner API"
		if in.Note != "" {
			note += ": " + in.Note
		}
		if err := s.AdjustCredit(ctx, "partner:"+c.Name, p.ID, bytes, in.Units, note); err != nil {
			s.partnerUngive(ctx, c.KeyID, bytes, in.Units)
			fail(w, err)
			return
		}
		np, ok := one(w, r, c)
		if !ok {
			return
		}
		s.emitAPIEvent(ctx, partnerTarget(c.KeyID), "panel.credit_added", panelEvent(np, map[string]any{"added_gb": in.CreditGB, "added_units": in.Units}))
		apiJSON(w, http.StatusOK, np)
	})
	route("POST /api/v2/partner/reps/{id}/credit/revoke", scopeCreditRevoke, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		in, ok := readCredit(w, r, "taken back")
		if !ok {
			return
		}
		ctx := r.Context()
		bytes := int64(in.CreditGB * gibF)
		note := "partner API take-back"
		if in.Note != "" {
			note += ": " + in.Note
		}
		if err := s.AdjustCredit(ctx, "partner:"+c.Name, p.ID, -bytes, -in.Units, note); err != nil {
			if errors.Is(err, ErrCredit) {
				apiErr(w, http.StatusPaymentRequired, codeCredit, "the panel does not have that much unused credit")
				return
			}
			fail(w, err)
			return
		}
		s.partnerUngive(ctx, c.KeyID, bytes, in.Units)
		np, ok := one(w, r, c)
		if !ok {
			return
		}
		s.emitAPIEvent(ctx, partnerTarget(c.KeyID), "panel.credit_revoked", panelEvent(np, map[string]any{"revoked_gb": in.CreditGB, "revoked_units": in.Units}))
		apiJSON(w, http.StatusOK, np)
	})
	route("GET /api/v2/partner/reps/{id}/ledger", scopePanelsRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		if p, ok := one(w, r, c); ok {
			ledger(w, r, c, p.ID)
		}
	})
	route("GET /api/v2/partner/reps/{id}/overview", scopePanelsRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		o, err := s.apiOverview(r.Context(), p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, o)
	})
	route("GET /api/v2/partner/reps/{id}/clients", scopeClientsRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		list, err := s.clientViews(r.Context(), "c.rep_id=?", p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		q := r.URL.Query()
		now := s.now().Unix()
		all := []APIClient{}
		for _, cv := range list {
			if a := apiClientOf(cv, now); clientMatches(a, q.Get("status"), q.Get("q")) {
				all = append(all, a)
			}
		}
		apiJSON(w, http.StatusOK, pageOf(all, readPage(r, 100, 500)))
	})
	for _, on := range []bool{true, false} {
		verb, ev := "disable", "panel.disabled"
		if on {
			verb, ev = "enable", "panel.enabled"
		}
		route("POST /api/v2/partner/reps/{id}/"+verb, scopePanelsWrite, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
			p, ok := one(w, r, c)
			if !ok {
				return
			}
			if err := s.PutRep(r.Context(), Rep{ID: p.ID, Name: p.Name, Enabled: on}); err != nil {
				fail(w, err)
				return
			}
			np, ok := one(w, r, c)
			if !ok {
				return
			}
			if np.Enabled != p.Enabled {
				s.emitAPIEvent(r.Context(), partnerTarget(c.KeyID), ev, panelEvent(np, nil))
			}
			apiJSON(w, http.StatusOK, np)
		})
	}
	route("POST /api/v2/partner/reps/{id}/password", scopePanelsWrite, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		var in struct {
			Password string `json:"password"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		pass := strings.TrimSpace(in.Password)
		if pass == "" {
			pass = RandomPassword()
		}
		if p.Username == "" {
			p.Username = p.ID
		}
		if err := s.SetRepLogin(r.Context(), p.Username, pass, p.ID); err != nil {
			fail(w, err)
			return
		}
		p.Password = pass
		p.Message = partnerMessage(p.PanelURL, p.Username, pass, p.Name)
		s.emitAPIEvent(r.Context(), partnerTarget(c.KeyID), "panel.password_reset", panelEvent(p, nil))
		apiJSON(w, http.StatusOK, p)
	})
	route("POST /api/v2/partner/reps/{id}/sessions/revoke", scopePanelsWrite, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		n, err := s.endRepSessions(r.Context(), p.ID)
		if err != nil {
			fail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{"ok": true, "ended": n})
	})
	route("POST /api/v2/partner/reps/{id}/login-link", scopePanelsWrite, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		p, ok := one(w, r, c)
		if !ok {
			return
		}
		if !p.Enabled {
			apiErr(w, http.StatusConflict, codeDisabled, "the panel is turned off")
			return
		}
		tok, exp, err := s.NewLoginLink(r.Context(), p.Username, "partner:"+c.Name)
		if err != nil {
			fail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{"url": s.panelOrigin(r) + loginLinkPath + tok, "expires_unix": exp, "username": p.Username})
	})
	s.eventRoutes(mux, "/api/v2/partner", auth, func(c apiCaller) string { return partnerTarget(c.KeyID) }, partnerEvents)
	s.loginLinkRoute(mux)
}
