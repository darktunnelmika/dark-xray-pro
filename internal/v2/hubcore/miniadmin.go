package hubcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// Management inside the Telegram Mini App: the reseller's (and the owner's)
// daily work without the web panel — a dashboard, clients (create, renew,
// on/off, delete, links and QR, daily cap, bulk), orders and wallet top-ups
// with their receipts. Every call carries Telegram's signed initData; who
// may do what follows the bot's roles:
//
//	panel owner (Telegram owners, main bot): everything, on the owner's world
//	reseller's own chats and "manager" admins: everything for that reseller
//	"sales": create, renew, orders, credit, view, links
//	"support": view and links
//
// The web panel stays as the manual way in (the "full panel" button signs
// in through /tgapp/, tgpanel.go).

// miniMgr is who is managing, and for which seller.
type miniMgr struct {
	P      Principal
	Seller string // "" = the owner's world
	Role   string // owner | manager | sales | support
	Chat   int64
	Owner  bool // the panel's owner
}

func (m miniMgr) can(what string) bool {
	switch m.Role {
	case "owner", "manager":
		return true
	case "sales":
		return what == "new" || what == "renew" || what == "orders" || what == "credit" || what == "view" || what == "link" || what == "tickets"
	case "support":
		return what == "view" || what == "link" || what == "tickets"
	}
	return false
}

// miniManager resolves the manager behind initData (ErrTgPanel when the
// chat manages nothing here).
func (s *Store) miniManager(ctx context.Context, initData string) (miniMgr, TgUser, error) {
	u, seller, err := s.verifyMiniUser(ctx, initData)
	if err != nil {
		return miniMgr{}, u, fmt.Errorf("%w: %v", ErrAuth, err)
	}
	actor := "tg:" + strconv.FormatInt(u.ID, 10)
	if seller == "" {
		for _, o := range s.TelegramOwners(ctx) {
			if o == u.ID {
				return miniMgr{P: Principal{User: actor, Role: "owner"}, Role: "owner", Chat: u.ID, Owner: true}, u, nil
			}
		}
	}
	rep, role := s.TgRepRole(ctx, u.ID)
	if rep == "" {
		return miniMgr{}, u, ErrTgPanel
	}
	if seller != "" && rep != seller {
		var parent string
		if s.ro.QueryRowContext(ctx, `SELECT parent_id FROM reps WHERE id=?`, rep).Scan(&parent) != nil || parent != seller {
			return miniMgr{}, u, ErrTgPanel
		}
	}
	return miniMgr{P: Principal{User: actor, Role: "rep", RepID: rep}, Seller: rep, Role: role, Chat: u.ID}, u, nil
}

// MiniClient is one client in the management list.
type MiniClient struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Plan      string `json:"plan"`
	Total     int64  `json:"total"` // bytes, -1 = unlimited
	Used      int64  `json:"used"`
	Until     int64  `json:"until"`      // effective expiry, 0 = none / not started
	StartDays int64  `json:"start_days"` // term that starts at the first connection
	Online    bool   `json:"online"`
	LastSeen  int64  `json:"last_seen"`
	Status    string `json:"status"` // active | soon | ended | off | waiting
	DailyCap  int64  `json:"daily_cap"`
	DailyUsed int64  `json:"daily_used"`
	Note      string `json:"note"`
}

func miniClientOf(c ClientView, now int64) MiniClient {
	m := MiniClient{Name: c.Email, Enabled: c.Enabled, Plan: c.Plan, Total: c.VolumeBytes, Used: c.Usage.Up + c.Usage.Down,
		Until: c.EffectiveUntil, Online: c.Online, LastSeen: c.Usage.LastSeen, DailyCap: c.DailyCapBytes, DailyUsed: c.DailyUsed, Note: c.Note}
	if c.EffectiveUntil == 0 && c.DurationSeconds > 0 {
		m.StartDays = (c.DurationSeconds + 86399) / 86400
	}
	switch {
	case !c.Enabled:
		m.Status = "off"
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now, c.VolumeBytes > 0 && c.Remaining == 0:
		m.Status = "ended"
	case c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400, c.VolumeBytes > 0 && m.Used*10 >= c.VolumeBytes*9:
		m.Status = "soon"
	case m.StartDays > 0 && c.Usage.FirstSeen == 0:
		m.Status = "waiting"
	default:
		m.Status = "active"
	}
	return m
}

// mgrClients is every client the manager sees.
func (s *Store) mgrClients(ctx context.Context, m miniMgr) ([]ClientView, error) {
	if m.Owner {
		return s.Clients(ctx)
	}
	return s.clientViews(ctx, "c.rep_id=?", m.Seller)
}

// mgrOwns tells whether the manager may touch a client.
func (s *Store) mgrOwns(ctx context.Context, m miniMgr, email string) (ClientView, bool) {
	c, err := s.ClientByEmail(ctx, email)
	if err != nil {
		return c, false
	}
	if m.Owner {
		_, shop := tenantOfRep(c.RepID)
		return c, !shop
	}
	return c, c.RepID == m.Seller
}

// MiniPlan is a ready plan offered for quick creation.
type MiniPlan struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Plan     string `json:"plan"`
	GB       int64  `json:"gb"`
	Days     int64  `json:"days"`
	IPs      int    `json:"ips"`
	FirstUse bool   `json:"first_use"`
	Price    int64  `json:"price"`
	NoRenew  bool   `json:"no_renew"`
}

func (s *Store) mgrPlans(ctx context.Context, m miniMgr) []MiniPlan {
	prods, _ := s.Products(ctx, m.P, true)
	out := []MiniPlan{}
	for _, p := range prods {
		if p.Seller != m.Seller {
			continue
		}
		out = append(out, MiniPlan{ID: p.ID, Name: p.Name, Plan: p.Plan, GB: p.GB, Days: p.Days, IPs: p.MaxIPs, FirstUse: p.FirstUse, Price: p.Price, NoRenew: p.NoRenew})
	}
	return out
}

// mgrHome is the dashboard.
func (s *Store) mgrHome(ctx context.Context, m miniMgr) (map[string]any, error) {
	now := s.now().Unix()
	all, err := s.mgrClients(ctx, m)
	if err != nil {
		return nil, err
	}
	var total, active, online, soon, ended int
	for _, c := range all {
		mc := miniClientOf(c, now)
		total++
		if mc.Online {
			online++
		}
		switch mc.Status {
		case "active", "waiting":
			active++
		case "soon":
			active++
			soon++
		case "ended":
			ended++
		}
	}
	out := map[string]any{"role": m.Role, "owner": m.Owner, "seller": m.Seller,
		"can": map[string]bool{"new": m.can("new"), "renew": m.can("renew"), "edit": m.can("edit"), "delete": m.can("delete"), "orders": m.can("orders"), "credit": m.can("credit"),
			"sellers": m.can("sellers"), "bot": m.can("bot") && !m.Owner, "cards": m.can("cards"), "plans": m.can("plans"), "tickets": m.can("tickets"), "reports": m.can("reports"), "settings": m.can("settings")},
		"stats": map[string]int{"total": total, "active": active, "online": online, "soon": soon, "ended": ended},
		"plans": s.mgrPlans(ctx, m)}
	today := s.tehranToday()
	ledger := `SELECT COALESCE(SUM(CASE WHEN kind='charge' THEN -volume_delta END),0),COALESCE(SUM(CASE WHEN kind='charge' THEN 1 END),0) FROM credit_ledger WHERE unix>=?`
	args := []any{today}
	if !m.Owner {
		ledger += ` AND rep_id=?`
		args = append(args, m.Seller)
		var r Rep
		if reps, err := s.Reps(ctx); err == nil {
			for _, x := range reps {
				if x.ID == m.Seller {
					r = x
				}
			}
		}
		out["name"] = firstNonEmpty(r.Name, r.ID)
		out["credit"] = r.VolumeCredit
		out["units"] = r.UnlimitedUnits
		out["sub_seller"] = r.ParentID != ""
		out["forecast"] = s.RepForecast(ctx, m.Seller, r.VolumeCredit)
		out["policy"] = map[string]any{"max_days": r.Policy.MaxDays, "max_client_gb": r.Policy.MaxClientGB, "allow_unlimited": r.Policy.AllowUnlimited,
			"max_clients": r.Policy.MaxClients, "can_delete": r.Policy.CanDelete}
		out["custom"] = r.ParentID == "" // a sub-seller sells only its parent's plans
	} else {
		out["name"] = s.PanelName(ctx)
		out["custom"] = true
	}
	var sold, sales int64
	_ = s.ro.QueryRowContext(ctx, ledger, args...).Scan(&sold, &sales)
	out["today"] = map[string]int64{"sold": sold, "sales": sales}
	orders, _ := s.Orders(ctx, m.P, "pending", 200)
	pend := 0 // only what has a receipt waits for the seller; an order without one waits for its buyer
	for _, o := range orders {
		if o.Seller == m.Seller && o.Method == "manual" && o.HasReceipt {
			pend++
		}
	}
	tops, _ := s.Topups(ctx, m.Seller, "review", 200)
	panels := 0
	if m.can("sellers") {
		po, _ := s.pendingRepOrders(ctx, m.Seller)
		panels = len(po)
	}
	out["pending"] = map[string]int{"orders": pend, "topups": len(tops), "panels": panels}
	out["resale"] = m.can("sellers") && (m.Owner || (!s.IsSubSeller(ctx, m.Seller) && s.ResaleOf(ctx, m.Seller).Allowed))
	if m.can("tickets") {
		open := 0
		if ts, err := s.Tickets(ctx, m.P, "open", 200); err == nil {
			for _, t := range ts {
				if t.Seller == m.Seller {
					open++
				}
			}
		}
		out["tickets"] = open
	}
	return out, nil
}

// mgrSubURL is a client's subscription address.
func (s *Store) mgrSubURL(ctx context.Context, c ClientView, def string) string {
	return strings.TrimRight(s.SubBaseFor(ctx, c.Email, def), "/") + "/sub/" + c.SubToken
}

func qrDataURL(text string) string {
	png, err := qrcode.Encode(text, qrcode.Medium, 320)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// miniCreate is a new client from the Mini App.
type miniCreate struct {
	Name      string  `json:"name"`
	PlanID    string  `json:"plan_id"`
	GB        float64 `json:"gb"` // 0 with Unlimited
	Days      int64   `json:"days"`
	IPs       int     `json:"ips"`
	FirstUse  bool    `json:"first_use"`
	Unlimited bool    `json:"unlimited"`
	DailyGB   float64 `json:"daily_gb"`
	Note      string  `json:"note"`
	Count     int     `json:"count"` // bulk: how many with the name as prefix
}

func (s *Store) mgrResolvePlan(ctx context.Context, m miniMgr, in *miniCreate) error {
	if in.PlanID == "" {
		if !m.Owner && s.IsSubSeller(ctx, m.Seller) {
			return fmt.Errorf("%w: pick one of the plans your reseller made for you", ErrPolicy)
		}
		return nil
	}
	for _, p := range s.mgrPlans(ctx, m) {
		if p.ID == in.PlanID {
			in.GB, in.Days, in.IPs, in.FirstUse, in.Unlimited = float64(p.GB), p.Days, p.IPs, p.FirstUse, p.Plan == "unlimited"
			return nil
		}
	}
	return ErrNotFound
}

var miniNameRE = idRE

// freeClientName picks an unused client name from a prefix.
func (s *Store) freeClientName(ctx context.Context, prefix string) string {
	prefix = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(prefix), "-"), "-")
	if prefix == "" {
		prefix = "u"
	}
	if len(prefix) > 40 {
		prefix = prefix[:40]
	}
	for i := 0; i < 30; i++ {
		n := prefix + "-" + strconv.Itoa(1000+int(randomHexInt()%9000))
		if _, err := s.ClientByEmail(ctx, n); err != nil {
			return n
		}
	}
	return prefix + "-" + randomHex(4)
}

func randomHexInt() uint64 {
	v, _ := strconv.ParseUint(randomHex(4), 16, 64)
	return v
}

func (s *Store) mgrCreateOne(ctx context.Context, m miniMgr, name string, in miniCreate) error {
	if _, err := s.ClientByEmail(ctx, name); err == nil {
		return fmt.Errorf("%w: the name %s is taken", ErrInvalid, name)
	}
	plan := "volume"
	c := Client{Email: name, Enabled: true, MaxIPs: in.IPs, Inbounds: s.SellerInbounds(ctx, m.Seller), VolumeBytes: int64(in.GB * (1 << 30))}
	if in.Unlimited {
		plan, c.VolumeBytes = "unlimited", -1
	} else if c.VolumeBytes <= 0 {
		return fmt.Errorf("%w: a volume is needed", ErrInvalid)
	}
	if in.Days <= 0 || in.Days > 3650 {
		return fmt.Errorf("%w: days: 1 to 3650", ErrInvalid)
	}
	if in.FirstUse {
		c.DurationSeconds = in.Days * 86400
	} else {
		c.ExpiresUnix = s.now().Unix() + in.Days*86400
	}
	var err error
	if m.Owner {
		err = s.PutClient(ctx, c)
		if err == nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE clients SET plan=? WHERE email=?`, plan, name)
		}
	} else {
		err = s.RepPutClient(ctx, m.P.User, m.Seller, c, plan)
	}
	if err != nil {
		return err
	}
	if in.DailyGB > 0 {
		_ = s.SetDailyCap(ctx, name, int64(in.DailyGB*(1<<30)))
	}
	if strings.TrimSpace(in.Note) != "" {
		_ = s.SetClientNote(ctx, name, strings.TrimSpace(in.Note))
	}
	return nil
}

// tgFile downloads a file a bot received (receipts), with that bot's token.
func tgFile(ctx context.Context, token, fileID string) ([]byte, error) {
	if token == "" || fileID == "" {
		return nil, ErrNotFound
	}
	cl := &http.Client{Timeout: 20 * time.Second}
	get := func(u string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		return cl.Do(req)
	}
	res, err := get("https://api.telegram.org/bot" + token + "/getFile?file_id=" + fileID)
	if err != nil {
		return nil, errors.New("telegram: request failed") // never echo the URL: it holds the token
	}
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			Path string `json:"file_path"`
		} `json:"result"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&r)
	res.Body.Close()
	if err != nil || !r.OK || r.Result.Path == "" {
		return nil, ErrNotFound
	}
	res, err = get("https://api.telegram.org/file/bot" + token + "/" + r.Result.Path)
	if err != nil {
		return nil, errors.New("telegram: request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrNotFound
	}
	return io.ReadAll(io.LimitReader(res.Body, 10<<20))
}

func registerMiniAdmin(mux *http.ServeMux, s *Store, base func(*http.Request) string) {
	mgr := func(w http.ResponseWriter, r *http.Request) (miniMgr, bool) {
		m, _, err := s.miniManager(r.Context(), r.Header.Get("X-Init-Data"))
		if err != nil {
			code := http.StatusUnauthorized
			if errors.Is(err, ErrTgPanel) {
				code = http.StatusForbidden
			}
			http.Error(w, "اجازهٔ مدیریت ندارید.", code)
			return m, false
		}
		return m, true
	}
	deny := func(w http.ResponseWriter, m miniMgr, what string) bool {
		if m.can(what) {
			return false
		}
		http.Error(w, "این کار با نقش شما مجاز نیست.", http.StatusForbidden)
		return true
	}
	fail := func(w http.ResponseWriter, err error) {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "credit"):
			msg = "اعتبار کافی نیست."
		case strings.Contains(msg, "longest term"):
			msg = "مدت بیشتر از سقف مجاز شماست."
		case strings.Contains(msg, "largest volume"):
			msg = "حجم بیشتر از سقف مجاز شماست."
		case strings.Contains(msg, "unlimited plans are not allowed"):
			msg = "فروش نامحدود برای شما فعال نیست."
		case strings.Contains(msg, "is taken"):
			msg = "این نام قبلاً استفاده شده."
		case strings.Contains(msg, "sub-sellers"), strings.Contains(msg, "plans your reseller"):
			msg = "فقط پلن‌هایی که نماینده برایتان تعریف کرده مجاز است."
		case errors.Is(err, ErrNotFound):
			msg = "پیدا نشد."
		}
		http.Error(w, msg, http.StatusBadRequest)
	}
	read := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return false
		}
		return true
	}
	// role: whether this chat manages a panel here (works with the store
	// closed: management does not depend on it).
	mux.HandleFunc("GET /app/api/role", func(w http.ResponseWriter, r *http.Request) {
		m, u, err := s.miniManager(r.Context(), r.Header.Get("X-Init-Data"))
		if err != nil && errors.Is(err, ErrAuth) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, seller, _ := s.verifyMiniUser(r.Context(), r.Header.Get("X-Init-Data"))
		reply(w, map[string]any{"manage": err == nil, "role": m.Role, "store": s.MiniAppOn(r.Context(), seller), "first_name": u.FirstName}, nil)
	})
	mux.HandleFunc("GET /app/api/m/home", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok {
			return
		}
		out, err := s.mgrHome(r.Context(), m)
		reply(w, out, err)
	})
	mux.HandleFunc("GET /app/api/m/clients", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "view") {
			return
		}
		all, err := s.mgrClients(r.Context(), m)
		if err != nil {
			reply(w, nil, err)
			return
		}
		now := s.now().Unix()
		f, q := r.URL.Query().Get("f"), strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		list := []MiniClient{}
		for _, c := range all {
			mc := miniClientOf(c, now)
			switch f {
			case "soon", "ended", "off":
				if mc.Status != f {
					continue
				}
			case "online":
				if !mc.Online {
					continue
				}
			}
			if q != "" && !strings.Contains(strings.ToLower(c.Email+" "+c.Note), q) {
				continue
			}
			list = append(list, mc)
		}
		if f == "online" {
			sort.SliceStable(list, func(i, j int) bool { return list[i].LastSeen > list[j].LastSeen })
		}
		n := len(list)
		if len(list) > 300 {
			list = list[:300]
		}
		reply(w, map[string]any{"clients": list, "count": n}, nil)
	})
	mux.HandleFunc("GET /app/api/m/client", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "view") {
			return
		}
		c, mine := s.mgrOwns(r.Context(), m, r.URL.Query().Get("e"))
		if !mine {
			http.NotFound(w, r)
			return
		}
		sub := s.mgrSubURL(r.Context(), c, base(r))
		links, _ := s.ClientLinks(r.Context(), c.Email)
		if len(links) > 40 {
			links = links[:40]
		}
		reply(w, map[string]any{"client": miniClientOf(c, s.now().Unix()), "sub": sub, "qr": qrDataURL(sub), "links": links}, nil)
	})
	mux.HandleFunc("POST /app/api/m/create", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "new") {
			return
		}
		var in miniCreate
		if !read(w, r, &in) {
			return
		}
		if err := s.mgrResolvePlan(r.Context(), m, &in); err != nil {
			fail(w, err)
			return
		}
		in.Name = strings.ToLower(strings.TrimSpace(in.Name))
		if in.Count > 1 {
			if in.Count > 50 {
				in.Count = 50
			}
			made := []string{}
			for i := 0; i < in.Count; i++ {
				n := s.freeClientName(r.Context(), firstNonEmpty(in.Name, "u"))
				if err := s.mgrCreateOne(r.Context(), m, n, in); err != nil {
					if len(made) == 0 {
						fail(w, err)
						return
					}
					break
				}
				made = append(made, n)
			}
			reply(w, map[string]any{"names": made}, nil)
			return
		}
		if in.Name == "" {
			in.Name = s.freeClientName(r.Context(), firstNonEmpty(m.Seller, "u"))
		}
		if !miniNameRE.MatchString(in.Name) {
			http.Error(w, "نام فقط حروف کوچک انگلیسی، عدد و . _ - باشد.", http.StatusBadRequest)
			return
		}
		if err := s.mgrCreateOne(r.Context(), m, in.Name, in); err != nil {
			fail(w, err)
			return
		}
		c, _ := s.ClientByEmail(r.Context(), in.Name)
		sub := s.mgrSubURL(r.Context(), c, base(r))
		reply(w, map[string]any{"name": in.Name, "sub": sub, "qr": qrDataURL(sub)}, nil)
	})
	mux.HandleFunc("POST /app/api/m/renew", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "renew") {
			return
		}
		var in struct {
			miniCreate
			Emails []string `json:"emails"`
		}
		if !read(w, r, &in) {
			return
		}
		if err := s.mgrResolvePlan(r.Context(), m, &in.miniCreate); err != nil {
			fail(w, err)
			return
		}
		plan := "" // keep the client's own
		switch {
		case in.Unlimited:
			plan = "unlimited"
		case in.GB > 0:
			plan = "volume"
		}
		var charged, back int64
		done := 0
		for _, e := range in.Emails {
			if _, mine := s.mgrOwns(r.Context(), m, e); !mine {
				continue
			}
			res, err := s.RenewClient(r.Context(), m.P, e, plan, in.GB, in.Days)
			if err != nil {
				if done == 0 {
					fail(w, err)
					return
				}
				break
			}
			charged += res.ChargedBytes
			back += res.RefundBytes
			done++
		}
		reply(w, map[string]any{"done": done, "charged": charged, "refund": back}, nil)
	})
	mux.HandleFunc("POST /app/api/m/set", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok {
			return
		}
		var in struct {
			Emails  []string `json:"emails"`
			Action  string   `json:"action"` // on | off | delete | daily | note
			DailyGB float64  `json:"daily_gb"`
			Note    string   `json:"note"`
		}
		if !read(w, r, &in) {
			return
		}
		need := map[string]string{"on": "edit", "off": "edit", "delete": "delete", "daily": "edit", "note": "edit"}[in.Action]
		if need == "" || deny(w, m, need) {
			if need == "" {
				http.Error(w, "bad action", http.StatusBadRequest)
			}
			return
		}
		done := 0
		var refund int64
		for _, e := range in.Emails {
			c, mine := s.mgrOwns(r.Context(), m, e)
			if !mine {
				continue
			}
			var err error
			switch in.Action {
			case "on", "off":
				on := in.Action == "on"
				if m.Owner {
					_, err = s.BulkExtend(r.Context(), BulkRequest{Emails: []string{e}, Enable: &on})
				} else {
					cl := c.Client
					cl.Enabled = on
					plan := c.Plan
					if plan == "" {
						plan = map[bool]string{true: "unlimited", false: "volume"}[c.VolumeBytes < 0]
					}
					err = s.RepPutClient(r.Context(), m.P.User, m.Seller, cl, plan)
				}
			case "delete":
				if m.Owner {
					err = s.DeleteClient(r.Context(), e)
				} else {
					var b int64
					b, _, err = s.RepDeleteClient(r.Context(), m.P.User, m.Seller, e)
					refund += b
				}
			case "daily":
				err = s.SetDailyCap(r.Context(), e, int64(in.DailyGB*(1<<30)))
			case "note":
				err = s.SetClientNote(r.Context(), e, strings.TrimSpace(in.Note))
			}
			if err != nil {
				if done == 0 {
					fail(w, err)
					return
				}
				break
			}
			done++
		}
		reply(w, map[string]any{"done": done, "refund": refund}, nil)
	})
	// Orders and wallet top-ups waiting for the seller, with their receipts.
	mux.HandleFunc("GET /app/api/m/inbox", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "orders") {
			return
		}
		orders, _ := s.Orders(r.Context(), m.P, "pending", 200)
		type item struct {
			Kind    string `json:"kind"` // order | topup
			ID      int64  `json:"id"`
			Title   string `json:"title"`
			Amount  int64  `json:"amount"`
			Buyer   string `json:"buyer"`
			Unix    int64  `json:"unix"`
			Receipt bool   `json:"receipt"`
			Renew   string `json:"renew,omitempty"`
			Last4   string `json:"last4,omitempty"`
			Card    string `json:"card,omitempty"`
		}
		out := []item{}
		for _, o := range orders {
			if o.Seller != m.Seller || o.Method != "manual" {
				continue
			}
			it := item{Kind: "order", ID: o.ID, Title: o.ProductName, Amount: o.Amount, Unix: o.CreatedUnix, Receipt: o.HasReceipt, Renew: map[bool]string{true: o.ClientEmail}[o.Renew]}
			if chat, ok := TgChatOfBuyer(o.Buyer); ok {
				it.Buyer = firstNonEmpty(s.TgName(r.Context(), chat), strconv.FormatInt(chat, 10))
			}
			c, l4 := s.PaymentCard(r.Context(), OrderRef(o.ID))
			it.Last4 = l4
			if len(c.Number) >= 4 {
				it.Card = c.Number[len(c.Number)-4:]
			}
			out = append(out, it)
		}
		tops, _ := s.Topups(r.Context(), m.Seller, "", 100)
		for _, t := range tops {
			if t.Status != "review" && t.Status != "awaiting" {
				continue
			}
			it := item{Kind: "topup", ID: t.ID, Title: "شارژ کیف پول", Amount: t.Amount, Unix: t.Created, Receipt: t.HasReceipt,
				Buyer: firstNonEmpty(s.TgName(r.Context(), t.Chat), strconv.FormatInt(t.Chat, 10))}
			c, l4 := s.PaymentCard(r.Context(), TopupRef(t.ID))
			it.Last4 = l4
			if len(c.Number) >= 4 {
				it.Card = c.Number[len(c.Number)-4:]
			}
			out = append(out, it)
		}
		if m.can("sellers") {
			pend, _ := s.pendingRepOrders(r.Context(), m.Seller)
			for _, o := range pend {
				out = append(out, item{Kind: "panel", ID: o.ID, Title: repOrderTitle(o), Amount: o.Price, Unix: o.Unix, Receipt: o.Receipt != "",
					Buyer: firstNonEmpty(o.BuyerName, strconv.FormatInt(o.Chat, 10))})
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Receipt != out[j].Receipt {
				return out[i].Receipt
			}
			return out[i].Unix > out[j].Unix
		})
		reply(w, out, nil)
	})
	mux.HandleFunc("GET /app/api/m/receipt", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "orders") {
			return
		}
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		var file, seller string
		if k := r.URL.Query().Get("k"); k == "panel" {
			o, err := s.RepOrderByID(r.Context(), id)
			file, seller = o.Receipt, o.Seller
			if err != nil || !m.can("sellers") {
				seller = "-"
			}
		} else if k == "topup" {
			t, err := s.TopupByID(r.Context(), id)
			file, seller = t.Receipt, t.Seller
			if err != nil {
				seller = "-"
			}
		} else {
			o, err := s.OrderByID(r.Context(), id)
			file, seller = o.Receipt, o.Seller
			if err != nil {
				seller = "-"
			}
		}
		if seller != m.Seller || file == "" {
			http.NotFound(w, r)
			return
		}
		data, err := tgFile(r.Context(), s.tokenFor(r.Context(), seller), file)
		if err != nil {
			http.Error(w, "عکس رسید در دسترس نیست.", http.StatusNotFound)
			return
		}
		reply(w, map[string]string{"image": "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil)
	})
	mux.HandleFunc("POST /app/api/m/decide", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "orders") {
			return
		}
		var in struct {
			Kind string `json:"kind"`
			ID   int64  `json:"id"`
			OK   bool   `json:"ok"`
		}
		if !read(w, r, &in) {
			return
		}
		if in.Kind == "panel" {
			if deny(w, m, "sellers") {
				return
			}
			if err := s.mgrDecidePanel(r.Context(), m, in.ID, in.OK); err != nil {
				miniErr(w, err)
				return
			}
			reply(w, map[string]bool{"ok": true}, nil)
			return
		}
		if in.Kind == "topup" {
			t, err := s.DecideTopup(r.Context(), m.Seller, in.ID, in.OK, m.P.User)
			if err != nil {
				fail(w, err)
				return
			}
			if in.OK {
				s.tellFrom(r.Context(), t.Seller, t.Chat, fmt.Sprintf("✅ کیف پول شما %d تومان شارژ شد. موجودی: %d تومان", t.Amount, t.Balance))
			} else {
				s.tellFrom(r.Context(), t.Seller, t.Chat, "❌ رسید شارژ کیف پول تأیید نشد. اگر مبلغ را واریز کرده‌اید، با پشتیبانی در تماس باشید.")
			}
			reply(w, map[string]bool{"ok": true}, nil)
			return
		}
		o, err := s.OrderByID(r.Context(), in.ID)
		if err != nil || o.Seller != m.Seller {
			http.NotFound(w, r)
			return
		}
		// The buyer hears from the seller's bot (its delivery announcement).
		if in.OK {
			_, err = s.FulfillOrder(r.Context(), m.P, in.ID)
		} else {
			err = s.CancelOrder(r.Context(), m.P, in.ID)
		}
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, map[string]bool{"ok": true}, nil)
	})
	registerMiniAdmin2(mux, s, base, mgr, deny, read)
	registerMiniAdmin3(mux, s, mgr, deny, read)
}
