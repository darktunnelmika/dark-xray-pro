package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// AdminHandler serves the owner API under /api/v2/admin/ and public
// subscriptions under /sub/<token>. adminToken guards the owner API until the
// panel's session login is attached in front of it.
func AdminHandler(s *Store, adminToken string) http.Handler {
	return AdminHandlerWith(s, adminToken, "", nil)
}

// AdminHandlerWith also serves the panel UI at / and fills each client's
// public subscription URL from subBase (e.g. https://sub.example.com).
func AdminHandlerWith(s *Store, adminToken, subBase string, ui http.Handler, extra ...func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()
	for _, f := range extra {
		f(mux)
	}
	if ui != nil {
		mux.Handle("GET /", ui)
	}
	// member lets owners and representatives in; handlers read the caller
	// with who(r). guard is owner-only and is the default for everything else.
	member := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p, ok := authorize(s, adminToken, r)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		}
	}
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return member(func(w http.ResponseWriter, r *http.Request) {
			if who(r).Role != "owner" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		})
	}
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		if !decode(w, r, &in) {
			return
		}
		tok, err := s.Login(r.Context(), in.Username, in.Password)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		setSessionCookie(w, r, tok, int(sessionTTL.Seconds()))
		reply(w, map[string]string{"username": in.Username}, nil)
	})
	mux.HandleFunc("POST /api/v2/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			_ = s.Logout(r.Context(), c.Value)
		}
		setSessionCookie(w, r, "", -1)
		reply(w, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("GET /api/v2/auth/me", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		out := map[string]any{"user": p.User, "role": p.Role, "rep_id": p.RepID}
		if p.Role == "rep" {
			reps, _ := s.Reps(r.Context())
			for _, rp := range reps {
				if rp.ID == p.RepID {
					out["volume_credit"], out["unlimited_units"] = rp.VolumeCredit, rp.UnlimitedUnits
					pol := rp.Policy
					pol.PriceGB, pol.PriceUnit = 0, 0 // sale prices are the owner's business
					out["policy"] = pol
					out["can_sell"] = rp.ParentID == ""
					var mp bool
					_ = s.ro.QueryRowContext(r.Context(), `SELECT manage_products FROM reps WHERE id=?`, rp.ID).Scan(&mp)
					out["manage_products"] = mp
				}
			}
		}
		reply(w, out, nil)
	}))
	mux.HandleFunc("POST /api/v2/auth/password", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Old, New string }
		if !decode(w, r, &in) {
			return
		}
		err := s.ChangePassword(r.Context(), who(r).User, in.Old, in.New)
		if err == nil {
			setSessionCookie(w, r, "", -1)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Reps(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name     string
			Enabled  bool
			Username string
			Password string
		}
		if !decode(w, r, &in) {
			return
		}
		id := r.PathValue("id")
		err := s.PutRep(r.Context(), Rep{ID: id, Name: in.Name, Enabled: in.Enabled})
		if err == nil && in.Username != "" && in.Password != "" {
			err = s.SetRepLogin(r.Context(), in.Username, in.Password, id)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/reps/{id}/credit", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			VolumeGB float64
			Units    int64
			Note     string
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.AdjustCredit(r.Context(), who(r).User, r.PathValue("id"), int64(in.VolumeGB*(1<<30)), in.Units, in.Note)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/policy", guard(func(w http.ResponseWriter, r *http.Request) {
		var p RepPolicy
		if !decode(w, r, &p) {
			return
		}
		err := s.SetRepPolicy(r.Context(), r.PathValue("id"), p)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/reps/{id}/payment", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Amount int64
			Note   string
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.RecordPayment(r.Context(), who(r).User, r.PathValue("id"), in.Amount, in.Note)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/finance", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		var since int64
		if days > 0 {
			since = time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
		}
		rows, err := s.Finance(r.Context(), since)
		reply(w, rows, err)
	}))
	// Activity: owners see every representative (or ?rep=), a representative only itself.
	mux.HandleFunc("GET /api/v2/rep/ledger", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		rep := r.URL.Query().Get("rep")
		if p.Role == "rep" && !(rep != "" && s.IsSellerOf(r.Context(), p.RepID, rep)) {
			rep = p.RepID
		}
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		list, err := s.Ledger(r.Context(), rep, before, 100)
		reply(w, list, err)
	}))
	// Sub-sellers, managed by their parent representative.
	repOnly := func(h http.HandlerFunc) http.HandlerFunc {
		return member(func(w http.ResponseWriter, r *http.Request) {
			if who(r).Role != "rep" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		})
	}
	mux.HandleFunc("POST /api/v2/rep/telegram", repOnly(func(w http.ResponseWriter, r *http.Request) {
		code, err := s.NewTgLinkCode(r.Context(), who(r).RepID)
		bot := s.Setting(r.Context(), "tg_username")
		link := ""
		if bot != "" {
			link = "https://t.me/" + bot + "?start=rep_" + code
		}
		reply(w, map[string]string{"code": code, "link": link, "bot": bot}, err)
	}))
	mux.HandleFunc("GET /api/v2/rep/sellers", repOnly(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Sellers(r.Context(), who(r).RepID)
		for i := range list {
			list[i].Policy.PriceGB, list[i].Policy.PriceUnit = 0, 0
		}
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/rep/sellers/{id}", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Username, Password string
			Enabled                  bool
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.PutSeller(r.Context(), who(r).RepID, Rep{ID: r.PathValue("id"), Name: in.Name, Enabled: in.Enabled}, in.Username, in.Password)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/sellers/{id}/credit", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			VolumeGB float64
			Units    int64
			Note     string
		}
		if !decode(w, r, &in) {
			return
		}
		p := who(r)
		err := s.TransferCredit(r.Context(), p.User, p.RepID, r.PathValue("id"), int64(in.VolumeGB*(1<<30)), in.Units, in.Note)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/rep/sellers/{id}/policy", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var pol RepPolicy
		if !decode(w, r, &pol) {
			return
		}
		err := s.SetSellerPolicy(r.Context(), who(r).RepID, r.PathValue("id"), pol)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	// Shop: owners see everything; a representative its own products/orders.
	mux.HandleFunc("GET /api/v2/shop/products", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Products(r.Context(), who(r), r.URL.Query().Has("published"))
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/shop/products/{id}", member(func(w http.ResponseWriter, r *http.Request) {
		var p Product
		if !decode(w, r, &p) {
			return
		}
		p.ID = r.PathValue("id")
		err := s.PutProduct(r.Context(), who(r), p)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/shop/orders", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Orders(r.Context(), who(r), r.URL.Query().Get("status"), 200)
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/shop/orders", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ProductID   string `json:"product_id"`
			Buyer       string `json:"buyer"`
			ClientEmail string `json:"client_email"`
			Note        string `json:"note"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, err := s.CreateOrder(r.Context(), who(r), in.ProductID, in.Buyer, in.ClientEmail, in.Note)
		reply(w, map[string]int64{"id": id}, err)
	}))
	mux.HandleFunc("POST /api/v2/shop/orders/{id}/fulfill", member(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		email, err := s.FulfillOrder(r.Context(), who(r), id)
		reply(w, map[string]string{"client_email": email}, err)
	}))
	mux.HandleFunc("POST /api/v2/shop/orders/{id}/cancel", member(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		err := s.CancelOrder(r.Context(), who(r), id)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/products", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Enabled bool }
		if !decode(w, r, &in) {
			return
		}
		err := s.SetManageProducts(r.Context(), r.PathValue("id"), in.Enabled)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/daily", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.ClientDailyUsage(r.Context(), r.PathValue("email"), days)
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/ips", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		list, err := s.ClientIPs(r.Context(), r.PathValue("email"))
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/resets", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		list, err := s.UsageResets(r.Context(), r.PathValue("email"))
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/reset", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, s.ResetUsage(r.Context(), r.PathValue("email"), "manual"))
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}/reset-period", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Period string `json:"period"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.SetResetPeriod(r.Context(), r.PathValue("email"), in.Period))
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/nodes", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.ClientNodeUsage(r.Context(), r.PathValue("email"), days)
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/devices", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		list, err := s.Devices(r.Context(), r.PathValue("email"))
		reply(w, list, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/clients/{email}/devices/{hwid}", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		reply(w, map[string]bool{"ok": true}, s.DeleteDevice(r.Context(), r.PathValue("email"), r.PathValue("hwid")))
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}/hwid-limit", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Limit int `json:"limit"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.SetHWIDLimit(r.Context(), r.PathValue("email"), in.Limit))
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}/next-plan", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Plan *NextPlan `json:"plan"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.SetNextPlan(r.Context(), r.PathValue("email"), in.Plan))
	}))
	mux.HandleFunc("GET /api/v2/admin/templates", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Templates(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/templates/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in ClientTemplate
		if !decode(w, r, &in) {
			return
		}
		in.ID = r.PathValue("id")
		reply(w, map[string]bool{"ok": true}, s.PutTemplate(r.Context(), in))
	}))
	mux.HandleFunc("DELETE /api/v2/admin/templates/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, s.DeleteTemplate(r.Context(), r.PathValue("id")))
	}))
	mux.HandleFunc("PUT /api/v2/admin/settings/hwid-strict", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			On bool `json:"on"`
		}
		if !decode(w, r, &in) {
			return
		}
		v := ""
		if in.On {
			v = "1"
		}
		reply(w, map[string]bool{"ok": true}, s.SetSetting(r.Context(), "hwid_strict", v))
	}))
	mux.HandleFunc("GET /api/v2/admin/insights", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		in, err := s.Insights(r.Context(), days)
		reply(w, in, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/overview", guard(func(w http.ResponseWriter, r *http.Request) {
		o, err := s.Overview(r.Context())
		reply(w, o, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/inbounds", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Inbounds(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/export", guard(func(w http.ResponseWriter, r *http.Request) {
		recs, err := s.ExportClients(r.Context())
		if err == nil {
			w.Header().Set("Content-Disposition", `attachment; filename="dark-clients-`+time.Now().UTC().Format("20060102")+`.json"`)
			w.Header().Set("Cache-Control", "no-store")
		}
		reply(w, recs, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/import", guard(func(w http.ResponseWriter, r *http.Request) {
		var recs []ClientRecord
		if !decode(w, r, &recs) {
			return
		}
		n, err := s.ImportClients(r.Context(), recs)
		reply(w, map[string]int{"imported": n}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/backup", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="dark-v2-`+time.Now().UTC().Format("20060102-1504")+`.sqlite.gz"`)
		w.Header().Set("Cache-Control", "no-store")
		if err := s.WriteBackup(r.Context(), s.tmpDir(), w); err != nil {
			log.Printf("backup download: %v", err)
		}
	}))
	mux.HandleFunc("GET /api/v2/admin/nodes/{id}/history", guard(func(w http.ResponseWriter, r *http.Request) {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		pts, err := s.NodeHistory(r.Context(), r.PathValue("id"), hours)
		reply(w, pts, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/nodes/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		err := s.RenameNode(r.Context(), r.PathValue("id"), in.Name)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/nodes/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteNode(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/bulk", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Emails  []string
			Group   string
			AddDays int64
			AddGB   float64
			Enable  *bool
		}
		if !decode(w, r, &in) {
			return
		}
		n, err := s.BulkExtend(r.Context(), BulkRequest{Emails: in.Emails, Group: in.Group, AddDays: in.AddDays, AddBytes: int64(in.AddGB * (1 << 30)), Enable: in.Enable})
		reply(w, map[string]int{"changed": n}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/transfer", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ RepID string }
		if !decode(w, r, &in) {
			return
		}
		err := s.TransferClient(r.Context(), r.PathValue("email"), in.RepID)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}/note", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Note string }
		if !decode(w, r, &in) {
			return
		}
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		err := s.SetClientNote(r.Context(), r.PathValue("email"), in.Note)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/groups", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Groups(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/groups/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var g Group
		if !decode(w, r, &g) {
			return
		}
		g.ID = r.PathValue("id")
		err := s.PutGroup(r.Context(), g)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/groups/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteGroup(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/rep/inbounds", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Inbounds(r.Context())
		if p := who(r); p.Role == "rep" && err == nil {
			pol, perr := s.RepPolicyOf(r.Context(), p.RepID)
			if perr != nil {
				reply(w, nil, perr)
				return
			}
			if len(pol.Inbounds) > 0 {
				ok := map[string]bool{}
				for _, t := range pol.Inbounds {
					ok[t] = true
				}
				kept := list[:0]
				for _, ib := range list {
					if ok[ib.Tag] {
						kept = append(kept, ib)
					}
				}
				list = kept
			}
		}
		for i := range list {
			list[i].Meta = nil // representatives only need names to assign
		}
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/inbounds/{tag}", guard(func(w http.ResponseWriter, r *http.Request) {
		raw, err := s.InboundRaw(r.Context(), r.PathValue("tag"))
		reply(w, raw, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/inbounds/{tag}/enabled", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Enabled bool }
		if !decode(w, r, &in) {
			return
		}
		err := s.SetInboundEnabled(r.Context(), r.PathValue("tag"), in.Enabled)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/inbounds/{tag}/routes", guard(func(w http.ResponseWriter, r *http.Request) {
		routes, err := s.Routes(r.Context(), r.PathValue("tag"))
		reply(w, routes, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/inbounds/{tag}/routes", guard(func(w http.ResponseWriter, r *http.Request) {
		var routes []Route
		if !decode(w, r, &routes) {
			return
		}
		err := s.PutRoutes(r.Context(), r.PathValue("tag"), routes)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	// compose: create a profile from a preset and place it on several nodes
	// in one request; nothing is left behind if placement fails.
	mux.HandleFunc("POST /api/v2/admin/inbounds/compose", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PresetRequest
			Routes []Route `json:"routes"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.ComposeInbound(r.Context(), in.PresetRequest, in.Routes)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/swaps", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Swaps(r.Context(), r.URL.Query().Get("inbound"))
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/swaps/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var sw Swap
		if !decode(w, r, &sw) {
			return
		}
		sw.ID = r.PathValue("id")
		err := s.PutSwap(r.Context(), sw)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/swaps/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteSwap(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/certs", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Certs(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/certs/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Cert, Key string }
		if !decode(w, r, &in) {
			return
		}
		err := s.PutCert(r.Context(), r.PathValue("id"), in.Cert, in.Key)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/certs/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteCert(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/presets", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, Presets, nil)
	}))
	mux.HandleFunc("POST /api/v2/admin/inbounds/preset", guard(func(w http.ResponseWriter, r *http.Request) {
		var p PresetRequest
		if !decode(w, r, &p) {
			return
		}
		err := s.CreatePreset(r.Context(), p)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/inbounds/{tag}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteInbound(r.Context(), r.PathValue("tag"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/nodes", guard(func(w http.ResponseWriter, r *http.Request) {
		nodes, err := s.Nodes(r.Context())
		type view struct {
			NodeStatus
			Online bool `json:"online"`
		}
		out := make([]view, 0, len(nodes))
		now := time.Now()
		for _, n := range nodes {
			out = append(out, view{n, n.Online(now)})
		}
		reply(w, out, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID, Name string }
		if !decode(w, r, &in) {
			return
		}
		tok, err := s.CreateNodeInstall(r.Context(), in.ID, in.Name)
		reply(w, map[string]string{"id": in.ID, "install": "curl -fsSL " + publicBase(subBase, r) + "/install/" + tok + " | bash"}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/sniscan", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Domains []string `json:"domains"`
			Subnet  bool     `json:"subnet"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, err := s.StartSNIScan(r.Context(), r.PathValue("id"), in.Domains, in.Subnet)
		reply(w, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/update", guard(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.StartNodeUpdate(r.Context(), r.PathValue("id"))
		reply(w, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/acme", guard(func(w http.ResponseWriter, r *http.Request) {
		certs, err := s.ACMECerts(r.Context())
		reply(w, map[string]any{"settings": s.ACMESettings(r.Context()), "certs": certs}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/acme/settings", guard(func(w http.ResponseWriter, r *http.Request) {
		var in ACMESettings
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.PutACMESettings(r.Context(), in))
	}))
	mux.HandleFunc("PUT /api/v2/admin/acme/certs/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Domains []string `json:"domains"`
			Method  string   `json:"method"`
			NodeID  string   `json:"node_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.PutACMECert(r.Context(), r.PathValue("id"), in.Domains, in.Method, in.NodeID))
	}))
	mux.HandleFunc("POST /api/v2/admin/acme/certs/{id}/renew", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, s.RenewACMECert(r.Context(), r.PathValue("id")))
	}))
	mux.HandleFunc("DELETE /api/v2/admin/acme/certs/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, s.DeleteACMECert(r.Context(), r.PathValue("id")))
	}))
	mux.HandleFunc("GET /api/v2/admin/routing", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"routing": s.Routing(r.Context()), "status": s.RoutingStatus(r.Context()), "geo": s.GeoInfo(r.Context())}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/routing", guard(func(w http.ResponseWriter, r *http.Request) {
		var in Routing
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.PutRouting(r.Context(), in))
	}))
	mux.HandleFunc("POST /api/v2/admin/routing/warp", guard(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := RegisterWARP(r.Context())
		reply(w, map[string]json.RawMessage{"config": cfg}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/routing/geo/update", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.UpdateGeo(r.Context())
		reply(w, s.GeoInfo(r.Context()), err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/routing/geo/{name}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.SetGeoURL(r.Context(), r.PathValue("name"), in.URL))
	}))
	mux.HandleFunc("GET /api/v2/admin/xray/releases", guard(func(w http.ResponseWriter, r *http.Request) {
		rel, err := XrayReleases(r.Context())
		reply(w, rel, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/xray", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version string `json:"version"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, err := s.StartXrayUpdate(r.Context(), r.PathValue("id"), in.Version)
		reply(w, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/xray/overlay", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.XrayOverlay(r.Context()))
	}))
	mux.HandleFunc("PUT /api/v2/admin/xray/overlay", guard(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		reply(w, map[string]bool{"ok": true}, s.PutXrayOverlay(r.Context(), raw))
	}))
	mux.HandleFunc("GET /api/v2/admin/nodes/{id}/config", guard(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := s.RenderNode(r.Context(), r.PathValue("id"))
		if err != nil {
			reply(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(cfg)
	}))
	mux.HandleFunc("GET /api/v2/admin/tasks/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		t, err := s.TaskByID(r.Context(), r.PathValue("id"))
		reply(w, t, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/install", guard(func(w http.ResponseWriter, r *http.Request) {
		tok, err := s.NewInstallToken(r.Context(), r.PathValue("id"))
		reply(w, map[string]string{"install": "curl -fsSL " + publicBase(subBase, r) + "/install/" + tok + " | bash"}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/inbounds/{tag}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			NodeID   string          `json:"node_id"`
			Template json.RawMessage `json:"template"`
			Meta     json.RawMessage `json:"meta"`
			Enabled  *bool           `json:"enabled"`
		}
		if !decode(w, r, &in) {
			return
		}
		enabled := in.Enabled == nil || *in.Enabled
		err := s.PutInboundMeta(r.Context(), r.PathValue("tag"), in.NodeID, in.Template, in.Meta, enabled)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.scopedClients(r.Context(), who(r))
		type view struct {
			ClientView
			SubURL string
		}
		out := make([]view, 0, len(list))
		for _, c := range list {
			out = append(out, view{c, subURL(subBase, r, c.SubToken)})
		}
		reply(w, out, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/qr.png", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.scopedClients(r.Context(), who(r))
		if err != nil {
			reply(w, nil, err)
			return
		}
		for _, c := range list {
			if c.Email == r.PathValue("email") {
				png, err := qrcode.Encode(subURL(subBase, r, c.SubToken), qrcode.Medium, 320)
				if err != nil {
					reply(w, nil, err)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(png)
				return
			}
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Client
			Plan string
		}
		if !decode(w, r, &in) {
			return
		}
		c := in.Client
		c.Email = r.PathValue("email")
		p := who(r)
		var err error
		if p.Role == "rep" {
			c.UUID, c.Secret, c.SubToken = "", "", "" // representatives never set credentials
			err = s.RepPutClient(r.Context(), p.User, p.RepID, c, in.Plan)
		} else {
			err = s.PutClient(r.Context(), c)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/clients/{email}", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" {
			b, u, err := s.RepDeleteClient(r.Context(), p.User, p.RepID, r.PathValue("email"))
			reply(w, map[string]any{"ok": err == nil, "refund_bytes": b, "refund_units": u}, err)
			return
		}
		err := s.DeleteClient(r.Context(), r.PathValue("email"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.Handle("GET /sub/{token}", SubHandler(s))
	return mux
}

func b64(s string) string { return sub.Base64([]string{s}) }

// SubHandler serves only public subscriptions, so it can listen on a public
// address without exposing the owner API.
func SubHandler(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sub/{token}", func(w http.ResponseWriter, r *http.Request) {
		email, err := s.EmailBySubToken(r.Context(), r.PathValue("token"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := s.CheckDevice(r.Context(), email, r.Header.Get("X-Hwid"), r.Header.Get("X-Device-Os"), r.Header.Get("X-Device-Model"), r.UserAgent()); err != nil {
			if errors.Is(err, ErrDeviceLimit) {
				http.Error(w, "device limit reached: remove a device in the panel or ask your seller", http.StatusForbidden)
				return
			}
			reply(w, nil, err)
			return
		}
		w.Header().Set("Profile-Update-Interval", "12")
		w.Header().Set("Profile-Title", "base64:"+b64(email))
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Query().Has("raw") {
			links, info, err := s.Subscription(r.Context(), email)
			if err != nil {
				reply(w, nil, err)
				return
			}
			w.Header().Set("Subscription-Userinfo", info)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(strings.Join(links, "\n")))
			return
		}
		body, ctype, info, err := s.SubscriptionBody(r.Context(), email, sub.Format(r.URL.Query().Get("format"), r.UserAgent()))
		if err != nil {
			reply(w, nil, err)
			return
		}
		w.Header().Set("Subscription-Userinfo", info)
		w.Header().Set("Content-Type", ctype)
		_, _ = w.Write(body)
	})
	return mux
}

func publicBase(base string, r *http.Request) string {
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return strings.TrimRight(base, "/")
}

func subURL(base string, r *http.Request, tok string) string {
	return publicBase(base, r) + "/sub/" + tok
}

type principalKey struct{}

func who(r *http.Request) Principal {
	p, _ := r.Context().Value(principalKey{}).(Principal)
	return p
}

// scopedClients: owners see everyone, a representative only its own.
func (s *Store) scopedClients(ctx context.Context, p Principal) ([]ClientView, error) {
	all, err := s.Clients(ctx)
	if err != nil || p.Role == "owner" {
		return all, err
	}
	out := all[:0]
	for _, c := range all {
		if c.RepID == p.RepID && p.RepID != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Store) ownsClient(ctx context.Context, repID, email string) bool {
	var got string
	return repID != "" && s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, email).Scan(&got) == nil && got == repID
}
