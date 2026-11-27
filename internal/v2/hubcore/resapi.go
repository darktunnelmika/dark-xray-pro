package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// The reseller API: a reseller's own bot or website sells and runs its
// customers with a dr_ key (reskeys.go). Everything goes through the same
// store paths as the reseller's panel, so credit, policy, refunds and the
// credit ledger behave exactly as there. In brackets: the scope.
//
//	GET    /api/v2/reseller/me                         credit, rules, the key [account:read]
//	GET    /api/v2/reseller/inbounds                   inbounds it may give [account:read]
//	GET    /api/v2/reseller/plans                      its published plans (products) [account:read]
//	POST   /api/v2/reseller/quote                      what a create or renew would cost, without doing it [account:read]
//	GET    /api/v2/reseller/overview                   dashboard numbers [account:read]
//	GET    /api/v2/reseller/ledger?before=&limit=      credit history [account:read]
//	GET    /api/v2/reseller/clients?status=&q=&limit=&offset=  customers [clients:read]
//	POST   /api/v2/reseller/clients                    make one [clients:write]
//	POST   /api/v2/reseller/clients/bulk               make many {prefix, count, …} [clients:write]
//	GET    /api/v2/reseller/clients/{name}             one customer [clients:read]
//	PATCH  /api/v2/reseller/clients/{name}             enabled, max_ips, inbounds, note, device_limit, telegram_id [clients:write]
//	DELETE /api/v2/reseller/clients/{name}             delete; unused credit back when the rules say so [clients:delete]
//	POST   /api/v2/reseller/clients/{name}/renew       a new period {plan?, gb, days} or {product} [clients:write]
//	POST   /api/v2/reseller/clients/{name}/reset       usage back to zero; a volume plan pays what was used [clients:write]
//	POST   /api/v2/reseller/clients/{name}/revoke      new link and credentials [clients:write]
//	POST   /api/v2/reseller/clients/{name}/enable | /disable  [clients:write]
//	GET    /api/v2/reseller/clients/{name}/links       subscription address and every config [clients:read]
//	GET    /api/v2/reseller/clients/{name}/qr.png      QR code of the subscription address [clients:read]
//	GET    /api/v2/reseller/clients/{name}/usage?days= traffic per day [clients:read]
//	GET    /api/v2/reseller/clients/{name}/devices     devices seen; DELETE …/devices/{hwid} [clients:read / clients:write]
//	GET    /api/v2/reseller/clients/{name}/ips         addresses seen [clients:read]
//	GET    /api/v2/reseller/online                     customers online now [clients:read]
//	GET    /api/v2/reseller/sellers                    sub-sellers; POST to make one [sellers]
//	PATCH  /api/v2/reseller/sellers/{id}               name, enabled, policy, password [sellers]
//	POST   /api/v2/reseller/sellers/{id}/credit        move credit to it; …/credit/revoke takes unused credit back [sellers]
//	GET    /api/v2/reseller/events, GET|PUT|DELETE /webhook, POST /webhook/test, GET /webhook/deliveries [events]

// resellerEvents are the events a reseller receives.
var resellerEvents = []string{"client.created", "client.updated", "client.enabled", "client.disabled", "client.deleted",
	"client.renewed", "client.usage_reset", "client.revoked", "client.first_used", "client.expiring", "client.expired",
	"client.volume_low", "client.volume_exhausted", "credit.added", "credit.removed", "credit.low"}

// repClient reads one of a reseller's customers.
func (s *Store) repClient(ctx context.Context, repID, name string) (ClientView, error) {
	list, err := s.clientViews(ctx, "c.email=?", name)
	if err != nil {
		return ClientView{}, err
	}
	if len(list) == 0 || list[0].RepID != repID || repID == "" {
		return ClientView{}, ErrNotFound
	}
	return list[0], nil
}

// repBalance is a reseller's credit now.
func (s *Store) repBalance(ctx context.Context, repID string) map[string]any {
	var vol, units int64
	_ = s.db.QueryRowContext(ctx, `SELECT volume_credit,unlimited_units FROM reps WHERE id=?`, repID).Scan(&vol, &units)
	return map[string]any{"credit_gb": roundGB(float64(vol) / gibF), "units": units}
}

// lastCharge is the newest charge row for a client since a ledger id.
func (s *Store) lastCharge(ctx context.Context, repID, email string, since int64) (bytes, units int64) {
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(-volume_delta),0),COALESCE(SUM(-units_delta),0) FROM credit_ledger
WHERE rep_id=? AND client_email=? AND kind='charge' AND id>?`, repID, email, since).Scan(&bytes, &units)
	return
}

func (s *Store) ledgerTop(ctx context.Context) int64 {
	var id int64
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM credit_ledger`).Scan(&id)
	return id
}

// clientEdit is a change that costs no credit.
type clientEdit struct {
	Enabled  *bool
	MaxIPs   *int
	Inbounds *[]string
}

// repEditClient changes a reseller's customer's on/off, IP limit or
// inbounds under its policy (deny_edit, allowed inbounds; a sub-seller
// cannot raise an IP limit). It works on expired customers too.
func (s *Store) repEditClient(ctx context.Context, repID, email string, e clientEdit) (changed bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	pol, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=? AND enabled=1`, repID))
	if err != nil {
		return false, ErrAuth
	}
	var owner string
	var enabled bool
	var maxIPs int
	if err := tx.QueryRowContext(ctx, `SELECT rep_id,enabled,max_ips FROM clients WHERE email=?`, email).Scan(&owner, &enabled, &maxIPs); err != nil || owner != repID {
		return false, ErrNotFound
	}
	var tags []string
	if e.Inbounds != nil {
		rows, err := tx.QueryContext(ctx, `SELECT tag FROM client_inbounds WHERE email=? ORDER BY tag`, email)
		if err != nil {
			return false, err
		}
		var cur []string
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				cur = append(cur, t)
			}
		}
		rows.Close()
		for _, t := range *e.Inbounds {
			if t = strings.TrimSpace(t); t != "" && !slices.Contains(tags, t) {
				tags = append(tags, t)
			}
		}
		slices.Sort(tags)
		if len(tags) == 0 {
			return false, fmt.Errorf("%w: a customer needs at least one inbound", ErrInvalid)
		}
		if slices.Equal(tags, cur) {
			e.Inbounds = nil
		}
	}
	if e.Enabled != nil && *e.Enabled == enabled {
		e.Enabled = nil
	}
	if e.MaxIPs != nil && *e.MaxIPs == maxIPs {
		e.MaxIPs = nil
	}
	if e.Enabled == nil && e.MaxIPs == nil && e.Inbounds == nil {
		return false, nil
	}
	if pol.DenyEdit {
		return false, fmt.Errorf("%w: changing client settings is not allowed for this representative", ErrPolicy)
	}
	if e.MaxIPs != nil {
		if *e.MaxIPs < 0 || *e.MaxIPs > 1000 {
			return false, fmt.Errorf("%w: max_ips is 0 (no limit) to 1000", ErrInvalid)
		}
		if s.IsSubSeller(ctx, repID) && (*e.MaxIPs > maxIPs || *e.MaxIPs == 0 && maxIPs > 0) {
			return false, fmt.Errorf("%w: sub-sellers cannot raise an IP limit", ErrPolicy)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET max_ips=? WHERE email=?`, *e.MaxIPs, email); err != nil {
			return false, err
		}
	}
	if e.Enabled != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET enabled=? WHERE email=?`, *e.Enabled, email); err != nil {
			return false, err
		}
	}
	if e.Inbounds != nil {
		for _, t := range tags {
			if len(pol.Inbounds) > 0 && !slices.Contains(pol.Inbounds, t) {
				return false, fmt.Errorf("%w: inbound %q is not allowed", ErrPolicy, t)
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM client_inbounds WHERE email=?`, email); err != nil {
			return false, err
		}
		for _, t := range tags {
			if _, err := tx.ExecContext(ctx, `INSERT INTO client_inbounds(email,tag) VALUES(?,?)`, email, t); err != nil {
				return false, fmt.Errorf("%w: inbound %q does not exist", ErrInvalid, t)
			}
		}
	}
	return true, tx.Commit()
}

// repResetUsage sets a reseller's customer's usage back to zero. The used
// volume of a volume plan becomes usable again, so it is charged (with its
// ledger row); a reseller that may not renew may not reset either.
func (s *Store) repResetUsage(ctx context.Context, actor, repID, email string) (charged int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	pol, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=? AND enabled=1`, repID))
	if err != nil {
		return 0, ErrAuth
	}
	var plan string
	var used int64
	if err := tx.QueryRowContext(ctx, `SELECT c.plan,COALESCE(u.up+u.down,0) FROM clients c LEFT JOIN client_usage u ON u.email=c.email WHERE c.email=? AND c.rep_id=?`,
		email, repID).Scan(&plan, &used); err != nil {
		return 0, ErrNotFound
	}
	if pol.DenyRenew {
		return 0, fmt.Errorf("%w: renewing clients is not allowed for this representative", ErrPolicy)
	}
	now := s.now().Unix()
	if plan == "volume" && used > 0 {
		r, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit-? WHERE id=? AND volume_credit>=?`, used, repID, used)
		if err != nil {
			return 0, err
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return 0, ErrCredit
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,note) VALUES(?,?,?,?,?,?,?)`,
			now, repID, actor, "charge", email, -used, "usage reset"); err != nil {
			return 0, err
		}
		charged = used
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if err := s.ResetUsage(ctx, email, "api"); err != nil {
		if charged > 0 { // give it back, on the record
			_, _ = s.db.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+? WHERE id=?`, charged, repID)
			_, _ = s.db.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,note) VALUES(?,?,?,?,?,?,?)`,
				s.now().Unix(), repID, actor, "refund", email, charged, "usage reset failed")
		}
		return 0, err
	}
	return charged, nil
}

// resClientIn is a new customer, or a renewal (plan, gb, days, product).
type resClientIn struct {
	Name            string   `json:"name"`
	Plan            string   `json:"plan"` // volume (default) | unlimited
	GB              float64  `json:"gb"`
	Days            int64    `json:"days"`
	StartOnFirstUse *bool    `json:"start_on_first_use"` // default: true
	MaxIPs          int      `json:"max_ips"`
	DeviceLimit     int      `json:"device_limit"`
	Inbounds        []string `json:"inbounds"`
	Note            string   `json:"note"`
	TelegramID      int64    `json:"telegram_id"`
	Product         string   `json:"product"` // one of its plans, instead of plan/gb/days/max_ips/inbounds
}

// fromProduct fills a request from one of the reseller's published plans.
func (s *Store) fromProduct(ctx context.Context, repID string, in *resClientIn, renew bool) error {
	if in.Product == "" {
		return nil
	}
	list, err := s.Products(ctx, Principal{Role: "rep", RepID: repID}, true)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.ID == in.Product && p.Seller == repID {
			if renew && p.NoRenew {
				return fmt.Errorf("%w: plan %q is for new customers only", ErrPolicy, p.ID)
			}
			in.Plan, in.GB, in.Days, in.MaxIPs = p.Plan, float64(p.GB), p.Days, p.MaxIPs
			if len(p.Inbounds) > 0 {
				in.Inbounds = p.Inbounds
			}
			first := p.FirstUse
			in.StartOnFirstUse = &first
			return nil
		}
	}
	return fmt.Errorf("%w: unknown plan %q", ErrInvalid, in.Product)
}

// newClient turns a request into a client for RepPutClient.
func (s *Store) newClient(ctx context.Context, repID string, in resClientIn) (Client, string, error) {
	plan := strings.ToLower(strings.TrimSpace(in.Plan))
	if plan == "" {
		plan = "volume"
	}
	if plan != "volume" && plan != "unlimited" {
		return Client{}, "", fmt.Errorf("%w: plan is volume or unlimited", ErrInvalid)
	}
	if in.Days < 1 || in.Days > 3650 {
		return Client{}, "", fmt.Errorf("%w: days is 1 to 3650", ErrInvalid)
	}
	if in.MaxIPs < 0 || in.MaxIPs > 1000 || in.DeviceLimit < 0 || in.DeviceLimit > 1000 {
		return Client{}, "", fmt.Errorf("%w: max_ips and device_limit are 0 (no limit) to 1000", ErrInvalid)
	}
	c := Client{Email: strings.TrimSpace(in.Name), Enabled: true, MaxIPs: in.MaxIPs, Inbounds: in.Inbounds}
	if plan == "volume" {
		if in.GB <= 0 || in.GB > 1e6 {
			return Client{}, "", fmt.Errorf("%w: a volume plan needs gb above 0", ErrInvalid)
		}
		c.VolumeBytes = int64(in.GB * gibF)
	} else {
		c.VolumeBytes = -1
	}
	if in.StartOnFirstUse == nil || *in.StartOnFirstUse {
		c.DurationSeconds = in.Days * 86400
	} else {
		c.ExpiresUnix = s.now().Unix() + in.Days*86400
	}
	if len(c.Inbounds) == 0 {
		c.Inbounds = s.SellerInbounds(ctx, repID)
	}
	return c, plan, nil
}

// QuoteIn asks what a create or renew would cost.
type QuoteIn struct {
	Action string `json:"action"` // create | renew (name: the customer)
	resClientIn
}

// Quote is the answer: the charge, the refund and whether it would pass.
type Quote struct {
	Allowed         bool    `json:"allowed"`
	Code            string  `json:"code,omitempty"`   // why not, as an error code
	Reason          string  `json:"reason,omitempty"` // why not, in words
	ChargeGB        float64 `json:"charge_gb"`
	ChargeUnits     int64   `json:"charge_units"`
	RefundGB        float64 `json:"refund_gb"` // renew: unused volume of the old period that comes back
	CreditGB        float64 `json:"credit_gb"`
	Units           int64   `json:"units"`
	CreditAfterGB   float64 `json:"credit_after_gb"`
	UnitsAfter      int64   `json:"units_after"`
	ExpiresUnix     int64   `json:"expires_unix"` // 0: starts at the first connection
	TermDays        int64   `json:"term_days"`
	VolumeGB        float64 `json:"volume_gb"` // -1: unlimited
	StartOnFirstUse bool    `json:"start_on_first_use"`
}

// QuoteFor prices a create or renew without doing it.
func (s *Store) QuoteFor(ctx context.Context, repID string, in QuoteIn) (Quote, error) {
	var q Quote
	renew := in.Action == "renew"
	if !renew && in.Action != "create" && in.Action != "" {
		return q, fmt.Errorf("%w: action is create or renew", ErrInvalid)
	}
	if err := s.fromProduct(ctx, repID, &in.resClientIn, renew); err != nil {
		return q, err
	}
	var vol, units int64
	if err := s.ro.QueryRowContext(ctx, `SELECT volume_credit,unlimited_units FROM reps WHERE id=?`, repID).Scan(&vol, &units); err != nil {
		return q, ErrNotFound
	}
	q.CreditGB, q.Units = roundGB(float64(vol)/gibF), units
	refuse := func(err error) (Quote, error) {
		q.Allowed = false
		switch {
		case errors.Is(err, ErrCredit):
			q.Code, q.Reason = codeCredit, "not enough credit"
		case errors.Is(err, ErrPolicy):
			q.Code, q.Reason = codePolicy, errDetail(err)
		case errors.Is(err, ErrInvalid):
			q.Code, q.Reason = codeValidation, errDetail(err)
		case errors.Is(err, ErrNotFound):
			q.Code, q.Reason = codeNotFound, "customer not found"
		default:
			return q, err
		}
		return q, nil
	}
	if renew {
		cv, err := s.repClient(ctx, repID, strings.TrimSpace(in.Name))
		if err != nil {
			return refuse(err)
		}
		in.Name = cv.Email
		if in.Plan == "" {
			in.Plan = cv.Plan
		}
		if in.MaxIPs == 0 {
			in.MaxIPs = cv.MaxIPs
		}
		in.Inbounds = cv.Inbounds
		c, plan, err := s.newClient(ctx, repID, in.resClientIn)
		if err != nil {
			return refuse(err)
		}
		// A renewal starts today unless the old period never started.
		if never := cv.Usage.FirstSeen == 0 && cv.Usage.Up+cv.Usage.Down == 0 && cv.ExpiresUnix == 0 && cv.DurationSeconds > 0; never {
			c.ExpiresUnix, c.DurationSeconds = 0, in.Days*86400
		} else {
			c.ExpiresUnix, c.DurationSeconds = s.now().Unix()+in.Days*86400, 0
		}
		used := cv.Usage.Up + cv.Usage.Down
		var refund int64
		if cv.Plan != "unlimited" && cv.VolumeBytes > 0 && cv.VolumeBytes > used {
			refund = cv.VolumeBytes - used
		}
		q.fill(c, plan, in.Days, refund)
		pol, err := s.RepPolicyOf(ctx, repID)
		if err != nil {
			return q, err
		}
		if pol.DenyRenew {
			return refuse(fmt.Errorf("%w: renewing clients is not allowed for this representative", ErrPolicy))
		}
		if err := s.subSellerPlan(ctx, repID, plan, c.VolumeBytes, in.Days, -1, true); err != nil {
			return refuse(err)
		}
		if err := s.quotePolicy(ctx, repID, c, plan, false); err != nil {
			return refuse(err)
		}
	} else {
		c, plan, err := s.newClient(ctx, repID, in.resClientIn)
		if err != nil {
			return refuse(err)
		}
		q.fill(c, plan, in.Days, 0)
		if err := s.subSellerPlan(ctx, repID, plan, c.VolumeBytes, in.Days, c.MaxIPs, false); err != nil {
			return refuse(err)
		}
		if err := s.quotePolicy(ctx, repID, c, plan, true); err != nil {
			return refuse(err)
		}
	}
	net := int64((q.ChargeGB - q.RefundGB) * gibF)
	q.CreditAfterGB, q.UnitsAfter = roundGB(float64(vol-net)/gibF), units-q.ChargeUnits
	if vol-max(net, 0) < 0 || q.UnitsAfter < 0 {
		return refuse(ErrCredit)
	}
	q.Allowed = true
	return q, nil
}

func (q *Quote) fill(c Client, plan string, days, refund int64) {
	q.TermDays, q.ExpiresUnix, q.StartOnFirstUse = days, c.ExpiresUnix, c.DurationSeconds > 0
	if plan == "volume" {
		q.ChargeGB, q.VolumeGB = roundGB(float64(c.VolumeBytes)/gibF), roundGB(float64(c.VolumeBytes)/gibF)
	} else {
		q.ChargeUnits, q.VolumeGB = unitsFor(days), -1
	}
	q.RefundGB = roundGB(float64(refund) / gibF)
}

// quotePolicy runs the policy check in a transaction that changes nothing.
func (s *Store) quotePolicy(ctx context.Context, repID string, c Client, plan string, isNew bool) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkPolicy(ctx, tx, repID, c, plan, isNew, s.now().Unix())
}

// resellerActor names a ledger row's author for a reseller: its own logins
// and keys by name, anyone above it as "owner".
func resellerActor(actor string, logins []string) string {
	if strings.HasPrefix(actor, "key:") || slices.Contains(logins, actor) {
		return actor
	}
	return "owner"
}

// APISeller is a sub-seller as its reseller sees it.
type APISeller struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Username string    `json:"username"`
	Enabled  bool      `json:"enabled"`
	CreditGB float64   `json:"credit_gb"`
	Units    int64     `json:"units"`
	Clients  int       `json:"clients"`
	UsedGB   float64   `json:"used_gb"`
	Policy   APIPolicy `json:"policy"`
	Password string    `json:"password,omitempty"` // only when just made or set
}

func (s *Store) apiSellers(ctx context.Context, parent, id string) ([]APISeller, error) {
	list, err := s.Sellers(ctx, parent)
	if err != nil {
		return nil, err
	}
	out := []APISeller{}
	for _, r := range list {
		if id != "" && r.ID != id {
			continue
		}
		user := ""
		if l := s.RepLogins(ctx, r.ID); len(l) > 0 {
			user = l[0]
		}
		out = append(out, APISeller{ID: r.ID, Name: r.Name, Username: user, Enabled: r.Enabled, CreditGB: roundGB(float64(r.VolumeCredit) / gibF),
			Units: r.UnlimitedUnits, Clients: r.Clients, UsedGB: roundGB(float64(r.UsedBytes) / gibF), Policy: apiPolicyOf(r.Policy)})
	}
	if id != "" && len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func resellerRoutes(s *Store, mux routeMux, subBase string) {
	auth := s.resellerCaller
	route := func(pattern, scope string, h extHandler) { mux.HandleFunc(pattern, s.ext(auth, scope, h)) }
	const P = "/api/v2/reseller"
	actor := func(c apiCaller) string { return "key:" + c.Name }
	target := func(c apiCaller) string { return repTarget(c.RepID) }
	fail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, ErrNotFound) {
			apiErr(w, http.StatusNotFound, codeNotFound, "customer not found")
			return
		}
		apiFail(w, err)
	}
	base := func(r *http.Request, repID string) string {
		if b := s.repBase(r.Context(), repID); b != "" {
			return b
		}
		return subBase
	}
	full := func(r *http.Request, c apiCaller, cv ClientView) APIClient {
		a := apiClientOf(cv, s.now().Unix())
		a.SubURL = subURL(base(r, c.RepID), r, cv.SubToken)
		a.Inbounds = cv.Inbounds
		if a.Inbounds == nil {
			a.Inbounds = []string{}
		}
		return a
	}
	// mine reads the customer in the path.
	mine := func(w http.ResponseWriter, r *http.Request, c apiCaller) (ClientView, bool) {
		cv, err := s.repClient(r.Context(), c.RepID, r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return cv, false
		}
		return cv, true
	}
	show := func(w http.ResponseWriter, r *http.Request, c apiCaller, status int, extra map[string]any) {
		cv, err := s.repClient(r.Context(), c.RepID, r.PathValue("name"))
		if err != nil {
			fail(w, err)
			return
		}
		if extra == nil {
			apiJSON(w, status, full(r, c, cv))
			return
		}
		extra["client"] = full(r, c, cv)
		extra["balance"] = s.repBalance(r.Context(), c.RepID)
		apiJSON(w, status, extra)
	}
	gbOf := func(b int64) float64 { return roundGB(float64(b) / gibF) }

	route("GET "+P+"/me", scopeAccountRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		ctx := r.Context()
		var name, parent string
		var vol, units int64
		if err := s.ro.QueryRowContext(ctx, `SELECT name,volume_credit,unlimited_units,parent_id FROM reps WHERE id=?`, c.RepID).Scan(&name, &vol, &units, &parent); err != nil {
			apiFail(w, err)
			return
		}
		pol, _ := s.RepPolicyOf(ctx, c.RepID)
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE rep_id=?`, c.RepID).Scan(&n)
		apiJSON(w, http.StatusOK, map[string]any{"id": c.RepID, "name": name, "credit_gb": gbOf(vol), "units": units, "clients": n,
			"policy": apiPolicyOf(pol), "sub_seller": parent != "", "sub_base": publicBase(base(r, c.RepID), r) + "/sub/",
			"key": map[string]any{"id": c.KeyID, "name": c.Name, "scopes": scopeList(c.Scopes), "expires_unix": c.Expires}})
	})
	route("GET "+P+"/inbounds", scopeAccountRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		ctx := r.Context()
		list, err := s.Inbounds(ctx)
		if err != nil {
			apiFail(w, err)
			return
		}
		allowed := s.SellerInbounds(ctx, c.RepID)
		type ib struct {
			Tag      string   `json:"tag"`
			Protocol string   `json:"protocol"`
			Network  string   `json:"network"`
			Security string   `json:"security"`
			Enabled  bool     `json:"enabled"`
			Nodes    []string `json:"nodes"`
		}
		out := []ib{}
		for _, i := range list {
			if slices.Contains(allowed, i.Tag) {
				out = append(out, ib{Tag: i.Tag, Protocol: i.Protocol, Network: i.Network, Security: i.Security, Enabled: i.Enabled, Nodes: i.Nodes})
			}
		}
		apiJSON(w, http.StatusOK, out)
	})
	route("GET "+P+"/plans", scopeAccountRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		list, err := s.Products(r.Context(), Principal{Role: "rep", RepID: c.RepID}, true)
		if err != nil {
			apiFail(w, err)
			return
		}
		type plan struct {
			ID              string   `json:"id"`
			Name            string   `json:"name"`
			Plan            string   `json:"plan"`
			GB              int64    `json:"gb"`
			Days            int64    `json:"days"`
			MaxIPs          int      `json:"max_ips"`
			Inbounds        []string `json:"inbounds"`
			Price           int64    `json:"price"`
			Category        string   `json:"category"`
			StartOnFirstUse bool     `json:"start_on_first_use"`
			NewOnly         bool     `json:"new_only"`
		}
		out := []plan{}
		for _, p := range list {
			if p.Seller == c.RepID {
				out = append(out, plan{ID: p.ID, Name: p.Name, Plan: p.Plan, GB: p.GB, Days: p.Days, MaxIPs: p.MaxIPs, Inbounds: p.Inbounds,
					Price: p.Price, Category: p.Category, StartOnFirstUse: p.FirstUse, NewOnly: p.NoRenew})
			}
		}
		apiJSON(w, http.StatusOK, out)
	})
	route("POST "+P+"/quote", scopeAccountRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		var in QuoteIn
		if !readJSON(w, r, &in) {
			return
		}
		q, err := s.QuoteFor(r.Context(), c.RepID, in)
		if err != nil {
			apiFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, q)
	})
	route("GET "+P+"/overview", scopeAccountRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		o, err := s.apiOverview(r.Context(), c.RepID)
		if err != nil {
			apiFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, o)
	})
	route("GET "+P+"/ledger", scopeAccountRead, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		q := r.URL.Query()
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		list, err := s.Ledger(r.Context(), c.RepID, before, limit)
		if err != nil {
			apiFail(w, err)
			return
		}
		logins := s.RepLogins(r.Context(), c.RepID)
		out := []APILedgerRow{}
		var next int64
		for _, e := range list {
			next = e.ID
			if e.Kind == "payment" {
				continue
			}
			row := apiLedgerRow(e, resellerActor(e.Actor, logins))
			row.Panel = ""
			out = append(out, row)
		}
		apiJSON(w, http.StatusOK, map[string]any{"items": out, "next_before": next})
	})

	// ---- customers ----
	route("GET "+P+"/clients", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		list, err := s.clientViews(r.Context(), "c.rep_id=?", c.RepID)
		if err != nil {
			apiFail(w, err)
			return
		}
		q := r.URL.Query()
		all := []APIClient{}
		for _, cv := range list {
			if a := full(r, c, cv); clientMatches(a, q.Get("status"), q.Get("q")) {
				all = append(all, a)
			}
		}
		apiJSON(w, http.StatusOK, pageOf(all, readPage(r, 100, 500)))
	})
	create := func(ctx context.Context, c apiCaller, in resClientIn) (string, int64, int64, error) {
		cl, plan, err := s.newClient(ctx, c.RepID, in)
		if err != nil {
			return "", 0, 0, err
		}
		if cl.Email == "" {
			for range 5 {
				name := "u" + randomHex(4)
				var x int
				if s.ro.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=?`, name).Scan(&x) != nil {
					cl.Email = name
					break
				}
			}
		}
		if !emailRE.MatchString(cl.Email) {
			return "", 0, 0, fmt.Errorf("%w: name: letters, digits and . _ @ + - (up to 128)", ErrInvalid)
		}
		var x int
		if s.db.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=?`, cl.Email).Scan(&x) == nil {
			return cl.Email, 0, 0, errNameTaken
		}
		top := s.ledgerTop(ctx)
		if err := s.RepPutClient(ctx, actor(c), c.RepID, cl, plan); err != nil {
			return cl.Email, 0, 0, err
		}
		b, u := s.lastCharge(ctx, c.RepID, cl.Email, top)
		if in.Note != "" {
			_ = s.SetClientNote(ctx, cl.Email, in.Note)
		}
		if in.DeviceLimit > 0 {
			_ = s.SetHWIDLimit(ctx, cl.Email, in.DeviceLimit)
		}
		if in.TelegramID != 0 {
			_ = s.SetClientTelegram(ctx, cl.Email, in.TelegramID)
		}
		return cl.Email, b, u, nil
	}
	createFail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, errNameTaken) {
			apiErr(w, http.StatusConflict, codeConflict, "this name is taken")
			return
		}
		apiFail(w, err)
	}
	route("POST "+P+"/clients", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		var in resClientIn
		if !readJSON(w, r, &in) {
			return
		}
		if len(in.Note) > 500 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "note: up to 500 characters")
			return
		}
		ctx := r.Context()
		if err := s.fromProduct(ctx, c.RepID, &in, false); err != nil {
			apiFail(w, err)
			return
		}
		name, b, u, err := create(ctx, c, in)
		if err != nil {
			createFail(w, err)
			return
		}
		r.SetPathValue("name", name)
		show(w, r, c, http.StatusCreated, map[string]any{"charge": map[string]any{"gb": gbOf(b), "units": u}})
	})
	route("POST "+P+"/clients/bulk", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		var in struct {
			resClientIn
			Prefix string `json:"prefix"`
			Count  int    `json:"count"`
			Start  int    `json:"start"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		ctx := r.Context()
		in.Prefix = strings.TrimSpace(in.Prefix)
		if in.Count < 1 || in.Count > 200 || in.Prefix == "" || !emailRE.MatchString(in.Prefix) || len(in.Prefix) > 100 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "prefix (letters, digits, . _ @ + -) and count 1 to 200 are needed")
			return
		}
		if in.Start <= 0 {
			in.Start = 1
		}
		if err := s.fromProduct(ctx, c.RepID, &in.resClientIn, false); err != nil {
			apiFail(w, err)
			return
		}
		// Know before making anything that every name is free and the credit covers all.
		q, err := s.QuoteFor(ctx, c.RepID, QuoteIn{Action: "create", resClientIn: in.resClientIn})
		if err != nil {
			apiFail(w, err)
			return
		}
		if !q.Allowed && q.Code != codeCredit {
			status := http.StatusForbidden
			switch q.Code {
			case codeValidation:
				status = http.StatusUnprocessableEntity
			case codeNotFound:
				status = http.StatusNotFound
			}
			apiErr(w, status, q.Code, q.Reason)
			return
		}
		var vol, units int64
		_ = s.ro.QueryRowContext(ctx, `SELECT volume_credit,unlimited_units FROM reps WHERE id=?`, c.RepID).Scan(&vol, &units)
		if int64(q.ChargeGB*gibF)*int64(in.Count) > vol || q.ChargeUnits*int64(in.Count) > units {
			apiErr(w, http.StatusPaymentRequired, codeCredit, fmt.Sprintf("not enough credit for %d customers", in.Count))
			return
		}
		names := make([]string, in.Count)
		for i := range names {
			names[i] = in.Prefix + "-" + strconv.Itoa(in.Start+i)
			var x int
			if s.ro.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=?`, names[i]).Scan(&x) == nil {
				apiErr(w, http.StatusConflict, codeConflict, "the name "+names[i]+" is taken; pick another prefix or start")
				return
			}
		}
		made := []APIClient{}
		var paidB, paidU int64
		var stop error
		for _, name := range names {
			one := in.resClientIn
			one.Name = name
			_, b, u, err := create(ctx, c, one)
			if err != nil {
				stop = err
				break
			}
			paidB, paidU = paidB+b, paidU+u
			if cv, err := s.repClient(ctx, c.RepID, name); err == nil {
				made = append(made, full(r, c, cv))
			}
		}
		if len(made) == 0 && stop != nil {
			createFail(w, stop)
			return
		}
		out := map[string]any{"created": made, "charge": map[string]any{"gb": gbOf(paidB), "units": paidU}, "balance": s.repBalance(ctx, c.RepID)}
		if stop != nil {
			out["stopped"] = map[string]string{"detail": errDetail(stop), "after": made[len(made)-1].Name}
		}
		apiJSON(w, http.StatusCreated, out)
	})
	route("GET "+P+"/clients/{name}", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		show(w, r, c, http.StatusOK, nil)
	})
	route("PATCH "+P+"/clients/{name}", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		var in struct {
			Enabled     *bool     `json:"enabled"`
			MaxIPs      *int      `json:"max_ips"`
			Inbounds    *[]string `json:"inbounds"`
			Note        *string   `json:"note"`
			DeviceLimit *int      `json:"device_limit"`
			TelegramID  *int64    `json:"telegram_id"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		ctx := r.Context()
		if in.Note != nil && len(*in.Note) > 500 {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "note: up to 500 characters")
			return
		}
		if in.DeviceLimit != nil && (*in.DeviceLimit < 0 || *in.DeviceLimit > 1000) {
			apiErr(w, http.StatusUnprocessableEntity, codeValidation, "device_limit is 0 (no limit) to 1000")
			return
		}
		changed, err := s.repEditClient(ctx, c.RepID, cv.Email, clientEdit{Enabled: in.Enabled, MaxIPs: in.MaxIPs, Inbounds: in.Inbounds})
		if err != nil {
			fail(w, err)
			return
		}
		if in.Note != nil && *in.Note != cv.Note {
			if err := s.SetClientNote(ctx, cv.Email, *in.Note); err != nil {
				fail(w, err)
				return
			}
			changed = true
		}
		if in.DeviceLimit != nil && *in.DeviceLimit != cv.HWIDLimit {
			if err := s.SetHWIDLimit(ctx, cv.Email, *in.DeviceLimit); err != nil {
				fail(w, err)
				return
			}
			changed = true
		}
		if in.TelegramID != nil {
			if err := s.SetClientTelegram(ctx, cv.Email, *in.TelegramID); err != nil {
				fail(w, err)
				return
			}
			changed = true
		}
		if changed {
			s.emitAPIEvent(ctx, target(c), "client.updated", map[string]any{"name": cv.Email})
			if in.Enabled != nil && *in.Enabled != cv.Enabled {
				ev := "client.disabled"
				if *in.Enabled {
					ev = "client.enabled"
				}
				s.emitAPIEvent(ctx, target(c), ev, map[string]any{"name": cv.Email})
			}
		}
		show(w, r, c, http.StatusOK, nil)
	})
	route("DELETE "+P+"/clients/{name}", scopeResClientsDel, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		b, u, err := s.RepDeleteClient(r.Context(), actor(c), c.RepID, cv.Email)
		if err != nil {
			fail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{"ok": true, "name": cv.Email, "refund": map[string]any{"gb": gbOf(b), "units": u},
			"balance": s.repBalance(r.Context(), c.RepID)})
	})
	route("POST "+P+"/clients/{name}/renew", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		var in resClientIn
		if !readJSON(w, r, &in) {
			return
		}
		ctx := r.Context()
		if err := s.fromProduct(ctx, c.RepID, &in, true); err != nil {
			apiFail(w, err)
			return
		}
		res, err := s.RenewClient(ctx, Principal{User: actor(c), Role: "rep", RepID: c.RepID}, cv.Email, in.Plan, in.GB, in.Days)
		if err != nil {
			fail(w, err)
			return
		}
		show(w, r, c, http.StatusOK, map[string]any{"charge": map[string]any{"gb": gbOf(res.ChargedBytes), "units": res.ChargedUnits},
			"refund": map[string]any{"gb": gbOf(res.RefundBytes)}})
	})
	route("POST "+P+"/clients/{name}/reset", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		charged, err := s.repResetUsage(r.Context(), actor(c), c.RepID, cv.Email)
		if err != nil {
			fail(w, err)
			return
		}
		show(w, r, c, http.StatusOK, map[string]any{"charge": map[string]any{"gb": gbOf(charged), "units": 0}})
	})
	route("POST "+P+"/clients/{name}/revoke", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		if err := s.RotateClient(r.Context(), cv.Email, true, true); err != nil {
			fail(w, err)
			return
		}
		s.emitAPIEvent(r.Context(), target(c), "client.revoked", map[string]any{"name": cv.Email})
		show(w, r, c, http.StatusOK, nil)
	})
	for _, on := range []bool{true, false} {
		verb, ev := "disable", "client.disabled"
		if on {
			verb, ev = "enable", "client.enabled"
		}
		route("POST "+P+"/clients/{name}/"+verb, scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
			cv, ok := mine(w, r, c)
			if !ok {
				return
			}
			changed, err := s.repEditClient(r.Context(), c.RepID, cv.Email, clientEdit{Enabled: &on})
			if err != nil {
				fail(w, err)
				return
			}
			if changed {
				s.emitAPIEvent(r.Context(), target(c), ev, map[string]any{"name": cv.Email})
			}
			show(w, r, c, http.StatusOK, nil)
		})
	}
	route("GET "+P+"/clients/{name}/links", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		links, err := s.ClientLinks(r.Context(), cv.Email)
		if err != nil {
			fail(w, err)
			return
		}
		if links == nil {
			links = []ClientLink{}
		}
		u := subURL(base(r, c.RepID), r, cv.SubToken)
		apiJSON(w, http.StatusOK, map[string]any{"name": cv.Email, "sub_url": u, "links_url": u + "/links", "links": links})
	})
	route("GET "+P+"/clients/{name}/qr.png", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size < 128 || size > 1024 {
			size = 320
		}
		png, err := qrcode.Encode(subURL(base(r, c.RepID), r, cv.SubToken), qrcode.Medium, size)
		if err != nil {
			apiFail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	})
	route("GET "+P+"/clients/{name}/usage", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.ClientDailyUsage(r.Context(), cv.Email, days)
		if err != nil {
			fail(w, err)
			return
		}
		type day struct {
			Day  string `json:"day"`
			Up   int64  `json:"up_bytes"`
			Down int64  `json:"down_bytes"`
		}
		out := []day{}
		for _, d := range list {
			out = append(out, day{Day: d.Day, Up: d.Up, Down: d.Down})
		}
		apiJSON(w, http.StatusOK, map[string]any{"name": cv.Email, "days": out})
	})
	route("GET "+P+"/clients/{name}/devices", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		list, err := s.Devices(r.Context(), cv.Email)
		if err != nil {
			fail(w, err)
			return
		}
		if list == nil {
			list = []Device{}
		}
		apiJSON(w, http.StatusOK, map[string]any{"name": cv.Email, "limit": cv.HWIDLimit, "items": list})
	})
	route("DELETE "+P+"/clients/{name}/devices/{hwid}", scopeResClientsW, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		if err := s.DeleteDevice(r.Context(), cv.Email, r.PathValue("hwid")); err != nil {
			fail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	route("GET "+P+"/clients/{name}/ips", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		cv, ok := mine(w, r, c)
		if !ok {
			return
		}
		list, err := s.ClientIPs(r.Context(), cv.Email)
		if err != nil {
			fail(w, err)
			return
		}
		if list == nil {
			list = []ClientIP{}
		}
		apiJSON(w, http.StatusOK, map[string]any{"name": cv.Email, "max_ips": cv.MaxIPs, "items": list})
	})
	route("GET "+P+"/online", scopeResClientsR, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		ctx := r.Context()
		list, err := s.Online(ctx)
		if err != nil {
			apiFail(w, err)
			return
		}
		mineSet := map[string]bool{}
		rows, err := s.ro.QueryContext(ctx, `SELECT email FROM clients WHERE rep_id=?`, c.RepID)
		if err != nil {
			apiFail(w, err)
			return
		}
		for rows.Next() {
			var e string
			if rows.Scan(&e) == nil {
				mineSet[e] = true
			}
		}
		rows.Close()
		type on struct {
			Name  string   `json:"name"`
			Node  string   `json:"node"`
			IPs   []string `json:"ips"`
			Seen  int64    `json:"seen"`
			Today int64    `json:"today_bytes"`
		}
		out := []on{}
		for _, o := range list {
			if mineSet[o.Email] {
				out = append(out, on{Name: o.Email, Node: o.NodeID, IPs: o.IPs, Seen: o.Seen, Today: o.Today})
			}
		}
		apiJSON(w, http.StatusOK, out)
	})

	// ---- sub-sellers ----
	sellerFail := func(w http.ResponseWriter, err error) {
		if errors.Is(err, ErrNotFound) {
			apiErr(w, http.StatusNotFound, codeNotFound, "seller not found")
			return
		}
		apiFail(w, err)
	}
	notSub := func(w http.ResponseWriter, r *http.Request, c apiCaller) bool {
		if s.IsSubSeller(r.Context(), c.RepID) {
			apiErr(w, http.StatusForbidden, codePolicy, "sub-sellers cannot open sellers")
			return false
		}
		return true
	}
	route("GET "+P+"/sellers", scopeSellers, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		list, err := s.apiSellers(r.Context(), c.RepID, "")
		if err != nil {
			sellerFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, list)
	})
	route("POST "+P+"/sellers", scopeSellers, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		if !notSub(w, r, c) {
			return
		}
		var in struct {
			ID          string       `json:"id"`
			Name        string       `json:"name"`
			Username    string       `json:"username"`
			Password    string       `json:"password"`
			CreditGB    float64      `json:"credit_gb"`
			Units       int64        `json:"units"`
			Policy      *APIPolicyIn `json:"policy"`
			TelegramIDs []int64      `json:"telegram_ids"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		ctx := r.Context()
		pp, err := s.RepPolicyOf(ctx, c.RepID)
		if err != nil {
			apiFail(w, err)
			return
		}
		pol, err := in.Policy.over(pp)
		if err != nil {
			apiFail(w, err)
			return
		}
		in.ID = strings.ToLower(strings.TrimSpace(in.ID))
		if in.ID == "" {
			for range 5 {
				if id := "s" + randomHex(3); s.RepNameFree(ctx, id) {
					in.ID = id
					break
				}
			}
		}
		if in.Password == "" {
			in.Password = RandomPassword()
		}
		if err := s.CreateSeller(ctx, actor(c), c.RepID, RepCreate{ID: in.ID, Name: in.Name, Username: in.Username, Password: in.Password,
			CreditGB: in.CreditGB, Units: in.Units, Policy: &pol, TgOwners: in.TelegramIDs}); err != nil {
			sellerFail(w, err)
			return
		}
		list, err := s.apiSellers(ctx, c.RepID, in.ID)
		if err != nil {
			sellerFail(w, err)
			return
		}
		list[0].Password = strings.TrimSpace(in.Password)
		apiJSON(w, http.StatusCreated, list[0])
	})
	route("PATCH "+P+"/sellers/{id}", scopeSellers, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		ctx := r.Context()
		list, err := s.apiSellers(ctx, c.RepID, r.PathValue("id"))
		if err != nil {
			sellerFail(w, err)
			return
		}
		cur := list[0]
		var in struct {
			Name     *string      `json:"name"`
			Enabled  *bool        `json:"enabled"`
			Policy   *APIPolicyIn `json:"policy"`
			Password string       `json:"password"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		name, enabled := cur.Name, cur.Enabled
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		user := ""
		if in.Password != "" {
			if user = cur.Username; user == "" {
				user = cur.ID
			}
		}
		if err := s.PutSeller(ctx, c.RepID, Rep{ID: cur.ID, Name: name, Enabled: enabled}, user, in.Password); err != nil {
			sellerFail(w, err)
			return
		}
		if in.Policy != nil {
			pol, err := s.RepPolicyOf(ctx, cur.ID)
			if err == nil {
				pol, err = in.Policy.over(pol)
			}
			if err == nil {
				err = s.SetSellerPolicy(ctx, c.RepID, cur.ID, pol)
			}
			if err != nil {
				sellerFail(w, err)
				return
			}
		}
		list, err = s.apiSellers(ctx, c.RepID, cur.ID)
		if err != nil {
			sellerFail(w, err)
			return
		}
		list[0].Password = in.Password
		apiJSON(w, http.StatusOK, list[0])
	})
	for _, back := range []bool{false, true} {
		path := P + "/sellers/{id}/credit"
		if back {
			path += "/revoke"
		}
		route("POST "+path, scopeSellers, func(w http.ResponseWriter, r *http.Request, c apiCaller) {
			var in struct {
				CreditGB float64 `json:"credit_gb"`
				Units    int64   `json:"units"`
				Note     string  `json:"note"`
			}
			if !readJSON(w, r, &in) {
				return
			}
			if in.CreditGB < 0 || in.Units < 0 || (in.CreditGB == 0 && in.Units == 0) || in.CreditGB > 1e6 {
				apiErr(w, http.StatusUnprocessableEntity, codeValidation, "credit_gb or units: more than 0")
				return
			}
			vol, units := int64(in.CreditGB*gibF), in.Units
			if back {
				vol, units = -vol, -units
			}
			ctx := r.Context()
			if err := s.TransferCredit(ctx, actor(c), c.RepID, r.PathValue("id"), vol, units, in.Note); err != nil {
				sellerFail(w, err)
				return
			}
			list, err := s.apiSellers(ctx, c.RepID, r.PathValue("id"))
			if err != nil {
				sellerFail(w, err)
				return
			}
			apiJSON(w, http.StatusOK, map[string]any{"seller": list[0], "balance": s.repBalance(ctx, c.RepID)})
		})
	}
	s.eventRoutes(mux, P, auth, target, resellerEvents)
}

var errNameTaken = errors.New("name taken")
