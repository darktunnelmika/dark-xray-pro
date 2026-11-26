package hubcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
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
	mux := &docMux{ServeMux: http.NewServeMux()}
	for _, f := range extra {
		f(mux.ServeMux)
	}
	if ui != nil {
		mux.Handle("GET /", ui)
	}
	pgRoutes(s, mux.ServeMux, subBase) // PasarGuard-compatible API for sales bots (pgapi.go)
	partnerRoutes(s, mux.ServeMux)     // keys that make reseller panels (partner.go)
	// member lets owners and representatives in; handlers read the caller
	// with who(r). guard is owner-only and is the default for everything else.
	member := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p, ok := authorize(s, adminToken, r)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !p.allowed(r) {
				http.Error(w, "forbidden: your role cannot do this", http.StatusForbidden)
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					s.Audit(r.Context(), p, r, http.StatusForbidden)
				}
				return
			}
			r, ok = s.tenantGuard(w, r, p) // a connected shop's client (tenant.go)
			if !ok {
				return
			}
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				h(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
				return
			}
			sw := &statusWriter{ResponseWriter: w}
			h(sw, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
			s.Audit(r.Context(), p, r, sw.status)
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
		var in struct{ Username, Password, Code string }
		if !decode(w, r, &in) {
			return
		}
		in.Username = s.canonicalUser(r.Context(), strings.TrimSpace(in.Username))
		ip := realIP(r)
		if wait := s.logins.blocked(ip, in.Username, s.now()); wait > 0 {
			s.Audit(r.Context(), Principal{User: in.Username, Role: "login"}, r, http.StatusTooManyRequests)
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			http.Error(w, fmt.Sprintf("too many wrong passwords from this address; try again in %d minutes", int(wait.Minutes())+1), http.StatusTooManyRequests)
			return
		}
		tok, err := s.LoginTOTP(r.Context(), in.Username, in.Password, in.Code)
		if errors.Is(err, ErrTOTPRequired) {
			// Password was right: ask for the authenticator code (no session yet).
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"need_code":true}`))
			return
		}
		lp := Principal{User: in.Username, Role: "login"}
		if err != nil {
			s.Audit(r.Context(), lp, r, http.StatusUnauthorized)
			if d := s.logins.fail(ip, in.Username, s.now()); d > 0 {
				notifyOwners(r.Context(), lockText(ip, in.Username, d))
			}
			time.Sleep(500 * time.Millisecond)
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		s.logins.success(ip, in.Username)
		s.noteSession(r.Context(), tok, r)
		s.noteLogin(r.Context(), in.Username, r)
		s.Audit(r.Context(), lp, r, http.StatusOK)
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
	cookieTok := func(r *http.Request) string {
		if c, err := r.Cookie(sessionCookie); err == nil {
			return c.Value
		}
		return ""
	}
	mux.HandleFunc("GET /api/v2/auth/sessions", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		user := p.User
		if r.URL.Query().Get("all") == "1" && p.Role == "owner" && p.Level == "" {
			user = ""
		}
		list, err := s.Sessions(r.Context(), user, cookieTok(r))
		reply(w, list, err)
	}))
	mux.HandleFunc("DELETE /api/v2/auth/sessions/{id}", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		user := p.User
		if p.Role == "owner" && p.Level == "" {
			user = "" // owners may end anyone's session
		}
		err := s.EndSession(r.Context(), user, r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/auth/sessions/end-others", member(func(w http.ResponseWriter, r *http.Request) {
		n, err := s.EndOtherSessions(r.Context(), who(r).User, cookieTok(r))
		reply(w, map[string]any{"ended": n}, err)
	}))
	telegramRoutes(s, mux, guard, func() []int64 { return tgDefaultOwners })
	mux.HandleFunc("POST /api/v2/admin/routing/test", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Node string `json:"node"`
			Tag  string `json:"tag"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, err := s.StartOutboundTest(r.Context(), in.Node, in.Tag)
		reply(w, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/update-hub", guard(func(w http.ResponseWriter, r *http.Request) {
		from, to, err := StageHubUpdate(r.Context(), http.MaxBytesReader(w, r.Body, 200<<20))
		if err == nil {
			notifyOwners(r.Context(), "⬆️ هاب از "+from+" به "+to+" به‌روز شد و ری‌استارت می‌شود.")
			RestartHub()
		}
		reply(w, map[string]string{"from": from, "to": to}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/update-hub/release", guard(func(w http.ResponseWriter, r *http.Request) {
		rel, err := LatestHubRelease(r.Context())
		reply(w, rel, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/update-hub/release", guard(func(w http.ResponseWriter, r *http.Request) {
		from, to, nodeOK, err := s.InstallHubRelease(r.Context())
		if err == nil {
			notifyOwners(r.Context(), "⬆️ هاب از "+from+" به "+to+" (نسخهٔ رسمی) به‌روز شد و ری‌استارت می‌شود.")
			defer RestartHub()
		}
		reply(w, map[string]any{"from": from, "to": to, "node_updated": nodeOK}, err)
	}))
	// The panel's own name: public (the sign-in page shows it), owner-set.
	mux.HandleFunc("GET /api/v2/panel-name", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		reply(w, map[string]string{"name": s.PanelName(r.Context())}, nil)
	})
	mux.HandleFunc("PUT /api/v2/admin/panel-name", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetPanelName(r.Context(), in.Name)
		reply(w, map[string]string{"name": s.PanelName(r.Context())}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/panel-path", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"path": s.Setting(r.Context(), "panel_path")}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/panel-path", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetPanelPath(r.Context(), in.Path)
		if err == nil {
			if p := s.Setting(r.Context(), "panel_path"); p != "" {
				setGate(w, r, p) // this browser keeps its way in
				notifyOwners(r.Context(), "🔐 آدرس مخفی پنل عوض شد: "+publicBase("", r)+"/"+p+"/\nاین پیام را نگه دارید؛ بدون این آدرس صفحهٔ ورود پیدا نمی‌شود.")
			} else {
				notifyOwners(r.Context(), "🔓 آدرس مخفی پنل خاموش شد.")
			}
		}
		reply(w, map[string]string{"path": s.Setting(r.Context(), "panel_path")}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/system", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.System(r.Context()), nil)
	}))
	mux.HandleFunc("GET /api/v2/admin/online", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Online(r.Context())
		if err == nil && who(r).Role == "rep" {
			mine, _ := s.scopedClients(r.Context(), who(r))
			keep := map[string]bool{}
			for _, c := range mine {
				keep[c.Email] = true
			}
			kept := list[:0]
			for _, o := range list {
				if keep[o.Email] {
					kept = append(kept, o)
				}
			}
			list = kept
		}
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/nodes/traffic", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.NodeTraffic(r.Context(), days)
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/security/lockouts", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.logins.list(s.now()), nil)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/security/lockouts", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]int{"unlocked": s.logins.unlock(r.URL.Query().Get("ip"))}, nil)
	}))
	mux.HandleFunc("GET /api/v2/auth/me", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		out := map[string]any{"user": p.User, "role": p.Role, "rep_id": p.RepID, "level": p.Level}
		var totp bool
		_ = s.ro.QueryRowContext(r.Context(), `SELECT totp_secret<>'' FROM admins WHERE username=?`, p.User).Scan(&totp)
		out["totp"] = totp
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
					out["manage_products"] = mp && rp.ParentID == ""
					out["sub_seller"] = rp.ParentID != ""
				}
			}
		}
		reply(w, out, nil)
	}))
	mux.HandleFunc("POST /api/v2/auth/2fa/setup", member(func(w http.ResponseWriter, r *http.Request) {
		secret, uri, err := s.TOTPSetup(r.Context(), who(r).User)
		if err != nil {
			reply(w, nil, err)
			return
		}
		png, err := qrcode.Encode(uri, qrcode.Medium, 256)
		reply(w, map[string]string{"secret": secret, "uri": uri, "qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}, err)
	}))
	mux.HandleFunc("POST /api/v2/auth/2fa/enable", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code string }
		if !decode(w, r, &in) {
			return
		}
		err := s.TOTPEnable(r.Context(), who(r).User, in.Code)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/auth/2fa/disable", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Password, Code string }
		if !decode(w, r, &in) {
			return
		}
		err := s.TOTPDisable(r.Context(), who(r).User, in.Password, in.Code)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/staff/{user}/2fa", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.ClearTOTP(r.Context(), r.PathValue("user"))
		reply(w, map[string]bool{"ok": err == nil}, err)
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
	mux.HandleFunc("POST /api/v2/admin/reps", guard(func(w http.ResponseWriter, r *http.Request) {
		var in RepCreate
		if !decode(w, r, &in) {
			return
		}
		err := s.CreateRep(r.Context(), who(r).User, in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/login", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"usernames": s.RepLogins(r.Context(), r.PathValue("id")), "telegram": s.TgRepOwners(r.Context(), r.PathValue("id"))}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/bot-owners", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Owners []int64 `json:"owners"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetTgRepOwners(r.Context(), r.PathValue("id"), in.Owners)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/login", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		if !decode(w, r, &in) {
			return
		}
		id := r.PathValue("id")
		var n int
		_ = s.ro.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reps WHERE id=?`, id).Scan(&n)
		if n == 0 {
			reply(w, nil, ErrNotFound)
			return
		}
		user := strings.ToLower(strings.TrimSpace(in.Username))
		if user == "" {
			if l := s.RepLogins(r.Context(), id); len(l) > 0 {
				user = l[0]
			} else {
				user = id
			}
		}
		err := s.SetRepLogin(r.Context(), user, in.Password, id)
		reply(w, map[string]any{"ok": err == nil, "username": user}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/reps/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		n, err := s.DeleteRep(r.Context(), r.PathValue("id"), r.URL.Query().Get("clients") == "1")
		reply(w, map[string]any{"ok": err == nil, "clients": n}, err)
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
	mux.HandleFunc("GET /api/v2/tickets", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Tickets(r.Context(), who(r), r.URL.Query().Get("status"), 300)
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/tickets/{id}", member(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		t, err := s.TicketByID(r.Context(), who(r), id)
		reply(w, t, err)
	}))
	mux.HandleFunc("POST /api/v2/tickets/{id}/reply", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text  string `json:"text"`
			Close bool   `json:"close"`
		}
		if !decode(w, r, &in) {
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		_, err := s.TicketReply(r.Context(), who(r), id, who(r).User, in.Text, false)
		if err == nil && in.Close {
			_, err = s.TicketClose(r.Context(), who(r), id, false)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/tickets/{id}/close", member(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		_, err := s.TicketClose(r.Context(), who(r), id, true)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/nodes/{id}/tunnels/{iface}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Hidden bool `json:"hidden"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.HideTunnel(r.Context(), r.PathValue("id"), r.PathValue("iface"), in.Hidden)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/analytics", member(func(w http.ResponseWriter, r *http.Request) {
		a, err := s.Analytics(r.Context(), who(r), r.URL.Query().Get("seller"))
		reply(w, a, err)
	}))
	mux.HandleFunc("POST /api/v2/analytics/remind/{email}", member(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := s.RemindRenewal(r.Context(), who(r), r.PathValue("email"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reports/sales", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		rep, err := s.SalesReport(r.Context(), days)
		reply(w, rep, err)
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
	mux.HandleFunc("GET /api/v2/rep/overview", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		rep := r.URL.Query().Get("rep")
		if p.Role == "rep" && !(rep != "" && s.IsSellerOf(r.Context(), p.RepID, rep)) {
			rep = p.RepID
		}
		if rep == "" {
			reply(w, nil, ErrNotFound)
			return
		}
		list, err := s.RepOverviews(r.Context(), rep, true)
		if err == nil && len(list) == 0 {
			err = ErrNotFound
		}
		if err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, list[0], nil)
	}))
	mux.HandleFunc("GET /api/v2/rep/alert", member(func(w http.ResponseWriter, r *http.Request) {
		if who(r).Role != "rep" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		reply(w, map[string]int64{"low_gb": s.RepAlertGB(r.Context(), who(r).RepID)}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/rep/alert", member(func(w http.ResponseWriter, r *http.Request) {
		if who(r).Role != "rep" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var in struct {
			LowGB int64 `json:"low_gb"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.SetRepAlertGB(r.Context(), who(r).RepID, in.LowGB))
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/alert", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]int64{"low_gb": s.RepAlertGB(r.Context(), r.PathValue("id"))}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/alert", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			LowGB int64 `json:"low_gb"`
		}
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.SetRepAlertGB(r.Context(), r.PathValue("id"), in.LowGB))
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/overview", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.RepOverviews(r.Context(), "", false)
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
		bot := s.BotUsernameFor(r.Context(), who(r).RepID)
		link := ""
		if bot != "" {
			link = "https://t.me/" + bot + "?start=rep_" + code
		}
		reply(w, map[string]string{"code": code, "link": link, "bot": bot}, err)
	}))
	mux.HandleFunc("GET /api/v2/rep/brand", repOnly(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.RepBrand(r.Context(), who(r).RepID), nil)
	}))
	mux.HandleFunc("PUT /api/v2/rep/brand", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var b RepBrand
		if !decode(w, r, &b) {
			return
		}
		err := s.PutRepBrand(r.Context(), who(r).RepID, b)
		reply(w, s.RepBrand(r.Context(), who(r).RepID), err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/brand", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.RepBrand(r.Context(), r.PathValue("id")), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/brand", guard(func(w http.ResponseWriter, r *http.Request) {
		var b RepBrand
		if !decode(w, r, &b) {
			return
		}
		err := s.PutRepBrand(r.Context(), r.PathValue("id"), b)
		reply(w, s.RepBrand(r.Context(), r.PathValue("id")), err)
	}))
	mux.HandleFunc("GET /api/v2/rep/sellers", repOnly(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Sellers(r.Context(), who(r).RepID)
		for i := range list {
			list[i].Policy.PriceGB, list[i].Policy.PriceUnit = 0, 0
		}
		reply(w, list, err)
	}))
	// What a reseller can hand to a new sub-seller: its own credit and limits.
	mux.HandleFunc("GET /api/v2/rep/sellers-limits", repOnly(func(w http.ResponseWriter, r *http.Request) {
		rep := who(r).RepID
		pol, err := s.RepPolicyOf(r.Context(), rep)
		pol.PriceGB, pol.PriceUnit = 0, 0
		var units int64
		_ = s.ro.QueryRowContext(r.Context(), `SELECT unlimited_units FROM reps WHERE id=?`, rep).Scan(&units)
		reply(w, map[string]any{"VolumeCredit": s.RepCredit(r.Context(), rep), "UnlimitedUnits": units, "Policy": pol}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/sellers", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var in RepCreate
		if !decode(w, r, &in) {
			return
		}
		in.ManageProducts = nil // sub-sellers sell their reseller's plans
		p := who(r)
		err := s.CreateSeller(r.Context(), p.User, p.RepID, in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/rep/sellers/{id}/login", repOnly(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !s.IsSellerOf(r.Context(), who(r).RepID, id) {
			reply(w, nil, ErrNotFound)
			return
		}
		reply(w, map[string]any{"usernames": s.RepLogins(r.Context(), id), "telegram": s.TgRepOwners(r.Context(), id)}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/rep/sellers/{id}/login", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		id := r.PathValue("id")
		if !decode(w, r, &in) {
			return
		}
		if !s.IsSellerOf(r.Context(), who(r).RepID, id) {
			reply(w, nil, ErrNotFound)
			return
		}
		user := strings.ToLower(strings.TrimSpace(in.Username))
		if user == "" {
			if l := s.RepLogins(r.Context(), id); len(l) > 0 {
				user = l[0]
			} else {
				user = id
			}
		}
		err := s.SetRepLogin(r.Context(), user, in.Password, id)
		reply(w, map[string]any{"ok": err == nil, "username": user}, err)
	}))
	mux.HandleFunc("PUT /api/v2/rep/sellers/{id}/bot-owners", repOnly(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Owners []int64 `json:"owners"`
		}
		id := r.PathValue("id")
		if !decode(w, r, &in) {
			return
		}
		if !s.IsSellerOf(r.Context(), who(r).RepID, id) {
			reply(w, nil, ErrNotFound)
			return
		}
		err := s.SetTgRepOwners(r.Context(), id, in.Owners)
		reply(w, map[string]bool{"ok": err == nil}, err)
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
	mux.HandleFunc("DELETE /api/v2/shop/products/{id}", member(func(w http.ResponseWriter, r *http.Request) {
		gone, err := s.DeleteProduct(r.Context(), who(r), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil, "deleted": gone}, err)
	}))
	mux.HandleFunc("PUT /api/v2/shop/products-order", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Seller string   `json:"seller"`
			IDs    []string `json:"ids"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.ReorderProducts(r.Context(), who(r), in.Seller, in.IDs)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/shop/categories", member(func(w http.ResponseWriter, r *http.Request) {
		seller := r.URL.Query().Get("seller")
		if p := who(r); p.Role == "rep" && (seller == "" || !s.childOf(r.Context(), seller, p.RepID)) {
			seller = p.RepID
		}
		reply(w, s.ShopCategories(r.Context(), seller), nil)
	}))
	mux.HandleFunc("PUT /api/v2/shop/categories", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Seller     string         `json:"seller"`
			Categories []ShopCategory `json:"categories"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.PutShopCategories(r.Context(), who(r), in.Seller, in.Categories)
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
	mux.HandleFunc("GET /api/v2/admin/sub-settings", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.SubSettings(r.Context()), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/sub-settings", guard(func(w http.ResponseWriter, r *http.Request) {
		var in SubSettings
		if !decode(w, r, &in) {
			return
		}
		reply(w, map[string]bool{"ok": true}, s.PutSubSettings(r.Context(), in))
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
	mux.HandleFunc("GET /api/v2/admin/clients/export.csv", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="dark-clients-`+time.Now().UTC().Format("20060102")+`.csv"`)
		w.Header().Set("Cache-Control", "no-store")
		if err := s.WriteClientsCSV(r.Context(), w, publicBase("", r)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
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
	mux.HandleFunc("GET /api/v2/admin/staff", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Staff(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/staff/{user}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Role, Password string }
		if !decode(w, r, &in) {
			return
		}
		err := s.PutStaff(r.Context(), r.PathValue("user"), in.Role, in.Password)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/staff/{user}", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("user") == who(r).User {
			reply(w, nil, fmt.Errorf("%w: you cannot delete your own login", ErrInvalid))
			return
		}
		err := s.DeleteStaff(r.Context(), r.PathValue("user"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/apikeys", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.APIKeys(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/apikeys", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Level string
			Days        int
		}
		if !decode(w, r, &in) {
			return
		}
		key, err := s.CreateAPIKey(r.Context(), in.Name, in.Level, who(r).User, in.Days)
		reply(w, map[string]string{"key": key}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/apikeys/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteAPIKey(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/webhook", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.Webhook(r.Context()), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/webhook", guard(func(w http.ResponseWriter, r *http.Request) {
		var in WebhookSettings
		if !decode(w, r, &in) {
			return
		}
		err := s.PutWebhook(r.Context(), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/webhook/test", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.TestWebhook(r.Context())
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/panel-https", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.PanelHTTPSStatus(r.Context()), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/panel-https", guard(func(w http.ResponseWriter, r *http.Request) {
		var in PanelHTTPS
		if !decode(w, r, &in) {
			return
		}
		err := s.PutPanelHTTPS(r.Context(), in)
		reply(w, s.PanelHTTPSStatus(r.Context()), err)
	}))
	mux.HandleFunc("POST /api/v2/admin/panel-https/restart", guard(func(w http.ResponseWriter, r *http.Request) {
		RestartHub()
		reply(w, map[string]bool{"ok": true}, nil)
	}))
	mux.HandleFunc("GET /api/v2/admin/routing/traffic", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.OutboundTraffic(r.Context(), days)
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/settings/trusted-ips", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string][]string{"ips": s.TrustedIPs(r.Context())}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/settings/trusted-ips", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IPs []string `json:"ips"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.PutTrustedIPs(r.Context(), in.IPs)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	// Reseller panels sold in the main bot: the switch and the packages.
	mux.HandleFunc("GET /api/v2/admin/rep-sale", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.RepSale(r.Context()), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/rep-sale", guard(func(w http.ResponseWriter, r *http.Request) {
		var in RepSale
		if !decode(w, r, &in) {
			return
		}
		err := s.PutRepSale(r.Context(), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/tg/commerce", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"trial": s.TrialPolicy(r.Context()), "referral": s.ReferralPolicy(r.Context()), "card": s.Card(r.Context())}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/pay/card", guard(func(w http.ResponseWriter, r *http.Request) {
		var in CardInfo
		if !decode(w, r, &in) {
			return
		}
		err := s.PutCard(r.Context(), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	// Toman sales per seller: card, wallets, top-ups, the reseller's own bot.
	sellerOf := func(r *http.Request) (string, bool) {
		p := who(r)
		if p.Role == "rep" {
			return p.RepID, p.RepID != ""
		}
		return r.URL.Query().Get("seller"), true
	}
	mux.HandleFunc("GET /api/v2/shop/card", member(func(w http.ResponseWriter, r *http.Request) {
		seller, ok := sellerOf(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(w, s.CardFor(r.Context(), seller), nil)
	}))
	mux.HandleFunc("PUT /api/v2/shop/card", member(func(w http.ResponseWriter, r *http.Request) {
		seller, ok := sellerOf(r)
		var in CardInfo
		if !ok || !decode(w, r, &in) {
			return
		}
		err := s.PutCardFor(r.Context(), seller, in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/shop/topups", member(func(w http.ResponseWriter, r *http.Request) {
		seller, ok := sellerOf(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		list, err := s.Topups(r.Context(), seller, r.URL.Query().Get("status"), 200)
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/shop/topups/{id}/{verb}", member(func(w http.ResponseWriter, r *http.Request) {
		seller, ok := sellerOf(r)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		verb := r.PathValue("verb")
		if !ok || (verb != "approve" && verb != "reject") {
			http.NotFound(w, r)
			return
		}
		t, err := s.DecideTopup(r.Context(), seller, id, verb == "approve", "panel:"+who(r).User)
		if err == nil {
			msg := fmt.Sprintf("✅ شارژ کیف پول تأیید شد: %d تومان\n💰 موجودی: %d تومان", t.Amount, t.Balance)
			if verb != "approve" {
				msg = "❌ رسید شارژ کیف پول رد شد. اگر فکر می‌کنید اشتباهی شده، با پشتیبانی تماس بگیرید."
			}
			s.tellFrom(r.Context(), t.Seller, t.Chat, msg)
		}
		reply(w, t, err)
	}))
	mux.HandleFunc("POST /api/v2/shop/wallet", member(func(w http.ResponseWriter, r *http.Request) {
		seller, ok := sellerOf(r)
		var in struct {
			Chat  int64  `json:"chat"`
			Delta int64  `json:"delta"`
			Note  string `json:"note"`
		}
		if !ok || !decode(w, r, &in) {
			return
		}
		bal, err := s.TomanAdjust(r.Context(), seller, in.Chat, in.Delta, in.Note, who(r).User)
		reply(w, map[string]int64{"balance": bal}, err)
	}))
	mux.HandleFunc("GET /api/v2/shop/wallets", member(func(w http.ResponseWriter, r *http.Request) {
		seller, ok := sellerOf(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		list, err := s.TomanWallets(r.Context(), seller)
		reply(w, list, err)
	}))
	// The look of the bots' main menu: the owner's main bot, a reseller's own.
	botLookRoutes(s, mux, "/api/v2/admin/telegram", guard, func(*http.Request) (string, bool) { return "", true })
	botLookRoutes(s, mux, "/api/v2/rep/bot", member, func(r *http.Request) (string, bool) {
		p := who(r)
		return p.RepID, p.Role == "rep" && p.RepID != ""
	})
	// Partner keys: other bots or sites making reseller panels (partner.go).
	mux.HandleFunc("GET /api/v2/admin/partner-keys", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.PartnerKeys(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/partner-keys", guard(func(w http.ResponseWriter, r *http.Request) {
		var in PartnerKeyIn
		if !decode(w, r, &in) {
			return
		}
		k, err := s.CreatePartnerKey(r.Context(), in)
		reply(w, k, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/partner-keys/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in PartnerKeyIn
		if !decode(w, r, &in) {
			return
		}
		k, err := s.UpdatePartnerKey(r.Context(), r.PathValue("id"), in)
		reply(w, k, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/partner-keys/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeletePartnerKey(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	// Connecting a PasarGuard-made sales bot (Mirza): the owner's, a reseller's own.
	pgAdminRoutes(s, mux, "/api/v2/admin/pg-api", guard, func(*http.Request) (string, bool) { return "", true })
	pgAdminRoutes(s, mux, "/api/v2/rep/pg-api", member, func(r *http.Request) (string, bool) {
		p := who(r)
		return p.RepID, p.Role == "rep" && p.RepID != ""
	})
	mux.HandleFunc("GET /api/v2/rep/bot", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role != "rep" {
			http.NotFound(w, r)
			return
		}
		reply(w, s.RepBotOf(r.Context(), p.RepID), nil)
	}))
	mux.HandleFunc("PUT /api/v2/rep/bot", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		var in RepBot
		if p.Role != "rep" || !decode(w, r, &in) {
			return
		}
		err := s.PutRepBot(r.Context(), p.RepID, in)
		reply(w, s.RepBotOf(r.Context(), p.RepID), err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/bot", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.RepBotOf(r.Context(), r.PathValue("id")), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/bot", guard(func(w http.ResponseWriter, r *http.Request) {
		var in RepBot
		if !decode(w, r, &in) {
			return
		}
		err := s.PutRepBot(r.Context(), r.PathValue("id"), in)
		reply(w, s.RepBotOf(r.Context(), r.PathValue("id")), err)
	}))
	mux.HandleFunc("POST /api/v2/admin/inbounds/{tag}/reset-usage", guard(func(w http.ResponseWriter, r *http.Request) {
		n, err := s.ResetInboundUsage(r.Context(), r.PathValue("tag"))
		reply(w, map[string]int{"reset": n}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/cleanup", guard(func(w http.ResponseWriter, r *http.Request) {
		var in CleanupRequest
		if !decode(w, r, &in) {
			return
		}
		list, err := s.CleanupClients(r.Context(), in)
		if list == nil {
			list = []string{}
		}
		reply(w, map[string]any{"clients": list, "count": len(list), "dry_run": in.DryRun}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/clone", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.CloneClient(r.Context(), r.PathValue("email"), in.Email)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/discounts", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Discounts(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/discounts", guard(func(w http.ResponseWriter, r *http.Request) {
		var in Discount
		if !decode(w, r, &in) {
			return
		}
		err := s.PutDiscount(r.Context(), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/discounts/{code}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteDiscount(r.Context(), r.PathValue("code"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/tg/trial", guard(func(w http.ResponseWriter, r *http.Request) {
		var in TrialPolicy
		if !decode(w, r, &in) {
			return
		}
		err := s.PutTrialPolicy(r.Context(), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/tg/referral", guard(func(w http.ResponseWriter, r *http.Request) {
		var in ReferralPolicy
		if !decode(w, r, &in) {
			return
		}
		err := s.PutReferralPolicy(r.Context(), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/tg/referral/stats", guard(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.ReferralStatsAll(r.Context(), 10)
		reply(w, st, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/tg/wallet/{chat}", guard(func(w http.ResponseWriter, r *http.Request) {
		chat, _ := strconv.ParseInt(r.PathValue("chat"), 10, 64)
		hist, err := s.WalletHistory(r.Context(), chat, 50)
		reply(w, map[string]any{"balance": s.WalletBalance(r.Context(), chat), "history": hist}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/tg/wallet/{chat}", guard(func(w http.ResponseWriter, r *http.Request) {
		chat, _ := strconv.ParseInt(r.PathValue("chat"), 10, 64)
		var in struct {
			Delta int64  `json:"delta"`
			Note  string `json:"note"`
		}
		if !decode(w, r, &in) {
			return
		}
		bal, err := s.WalletAdjust(r.Context(), chat, in.Delta, in.Note)
		reply(w, map[string]int64{"balance": bal}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/audit", guard(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		list, err := s.AuditLog(r.Context(), q.Get("actor"), q.Get("q"), before, limit)
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/restore", guard(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<30))
		if err != nil {
			reply(w, nil, fmt.Errorf("%w: upload failed or larger than 1 GB", ErrInvalid))
			return
		}
		p, err := s.StageRestore(r.Context(), data, r.Header.Get("X-Backup-Password"))
		reply(w, p, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/restore/apply", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.ApplyRestore()
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/restore", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.CancelRestore()
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/migrate", guard(func(w http.ResponseWriter, r *http.Request) {
		// Source: the old panel's database on this server (path), or an
		// uploaded copy (body, when Content-Type is application/octet-stream).
		q := r.URL.Query()
		opt := MigrateOptions{DryRun: q.Get("dry_run") != "0", IncludeArchived: q.Get("archived") == "1"}
		for _, t := range strings.Split(q.Get("inbounds"), ",") {
			if t = strings.TrimSpace(t); t != "" {
				opt.Inbounds = append(opt.Inbounds, t)
			}
		}
		dir, err := os.MkdirTemp(s.tmpDir(), "migrate-")
		if err != nil {
			reply(w, nil, err)
			return
		}
		defer os.RemoveAll(dir)
		var src string
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/octet-stream") {
			src = filepath.Join(dir, "upload.sqlite")
			f, err := os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, 2<<30))
				if cerr := f.Close(); err == nil {
					err = cerr
				}
			}
			if err != nil {
				reply(w, nil, fmt.Errorf("%w: upload failed: %v", ErrInvalid, err))
				return
			}
		} else {
			path := q.Get("path")
			if path == "" {
				path = DefaultOldPanelDB
			}
			if src, err = CopyOldDatabase(path, dir); err != nil {
				reply(w, nil, fmt.Errorf("%w: cannot read %s: %v", ErrInvalid, path, err))
				return
			}
		}
		report, err := s.MigrateFromOld(r.Context(), src, opt)
		reply(w, report, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/migrate/foreign", guard(func(w http.ResponseWriter, r *http.Request) {
		// Body: the other panel's SQLite file (x-ui.db, db.sqlite3).
		q := r.URL.Query()
		opt := ForeignOptions{DryRun: q.Get("dry_run") != "0", Node: q.Get("node"), Address: q.Get("address"), Prefix: q.Get("prefix")}
		for _, t := range strings.Split(q.Get("inbounds"), ",") {
			if t = strings.TrimSpace(t); t != "" {
				opt.Inbounds = append(opt.Inbounds, t)
			}
		}
		dir, err := os.MkdirTemp(s.tmpDir(), "foreign-")
		if err != nil {
			reply(w, nil, err)
			return
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "panel.sqlite")
		f, err := os.OpenFile(src, os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, 2<<30))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			reply(w, nil, fmt.Errorf("%w: upload failed: %v", ErrInvalid, err))
			return
		}
		report, err := s.ImportForeign(r.Context(), src, opt)
		reply(w, report, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/backup", guard(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="dark-v2-`+time.Now().UTC().Format("20060102-1504")+`.sqlite.gz"`)
		w.Header().Set("Cache-Control", "no-store")
		if err := s.WriteBackup(r.Context(), s.tmpDir(), w); err != nil {
			log.Printf("backup download: %v", err)
		}
	}))
	mux.HandleFunc("GET /api/v2/admin/backup/drill", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"last": s.LastDrill(r.Context()), "has_password": s.Setting(r.Context(), "tg_backup_pass") != ""}, nil)
	}))
	mux.HandleFunc("POST /api/v2/admin/backup/drill", guard(func(w http.ResponseWriter, r *http.Request) {
		pass := s.Setting(r.Context(), "tg_backup_pass")
		if pass == "" {
			reply(w, nil, fmt.Errorf("%w: set a backup password for the Telegram backups first", ErrInvalid))
			return
		}
		reply(w, s.BackupDrill(r.Context(), pass), nil)
	}))
	mux.HandleFunc("POST /api/v2/admin/backup/full", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
		}
		if !decode(w, r, &in) {
			return
		}
		data, err := s.FullBackup(r.Context(), in.Password)
		if err != nil {
			reply(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="dark-v2-full-`+time.Now().UTC().Format("20060102-1504")+`.dkb"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}))
	mux.HandleFunc("POST /api/v2/admin/migrate/darkold/start", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]string{"command": s.DarkOldStart(publicBase(subBase, r))}, nil)
	}))
	mux.HandleFunc("GET /api/v2/admin/migrate/darkold", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.DarkOldStatus(r.Context()), nil)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/migrate/darkold", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"ok": true}, s.DarkOldDiscard())
	}))
	mux.HandleFunc("POST /api/v2/admin/migrate/darkold/run", guard(func(w http.ResponseWriter, r *http.Request) {
		var in DarkOldOptions
		if !decode(w, r, &in) {
			return
		}
		res, err := s.ImportDarkOld(r.Context(), in)
		reply(w, res, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/hub-address", guard(func(w http.ResponseWriter, r *http.Request) {
		a, _ := s.NodeHubAddr(r.Context())
		reply(w, a, nil)
	}))
	mux.HandleFunc("POST /api/v2/admin/hub-address", guard(func(w http.ResponseWriter, r *http.Request) {
		var in NodeHubAddr
		if !decode(w, r, &in) {
			return
		}
		res, err := s.MoveNodes(r.Context(), in.URL, in.Name)
		reply(w, res, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/nodes/{id}/history", guard(func(w http.ResponseWriter, r *http.Request) {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		pts, err := s.NodeHistory(r.Context(), r.PathValue("id"), hours)
		reply(w, pts, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/nodes/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name           string
			TrafficPercent *int    `json:"traffic_percent"`
			PublicIP       *string `json:"public_ip"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.RenameNode(r.Context(), r.PathValue("id"), in.Name)
		if err == nil && in.TrafficPercent != nil {
			err = s.SetNodeTrafficPercent(r.Context(), r.PathValue("id"), *in.TrafficPercent)
		}
		if err == nil && in.PublicIP != nil {
			err = s.SetNodePublicIP(r.Context(), r.PathValue("id"), *in.PublicIP)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/nodes/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteNode(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/bulk-create", guard(func(w http.ResponseWriter, r *http.Request) {
		var in BulkCreate
		if !decode(w, r, &in) {
			return
		}
		emails, err := s.BulkCreateClients(r.Context(), in)
		reply(w, map[string]any{"emails": emails}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/telegram", guard(func(w http.ResponseWriter, r *http.Request) {
		chats, err := s.ClientTelegram(r.Context(), r.PathValue("email"))
		reply(w, map[string]any{"chats": chats}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}/telegram", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Chat int64 `json:"chat"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetClientTelegram(r.Context(), r.PathValue("email"), in.Chat)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}/on-hold", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Days int64 }
		if !decode(w, r, &in) {
			return
		}
		err := s.SetOnHold(r.Context(), r.PathValue("email"), in.Days)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/bulk", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Emails         []string
			Group          string
			AddDays        int64
			AddGB          float64
			Enable         *bool
			AddInbounds    []string `json:"add_inbounds"`
			RemoveInbounds []string `json:"remove_inbounds"`
			MaxIPs         *int     `json:"max_ips"`
			RepID          *string  `json:"rep_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		n, err := s.BulkExtend(r.Context(), BulkRequest{Emails: in.Emails, Group: in.Group, AddDays: in.AddDays, AddBytes: int64(in.AddGB * (1 << 30)), Enable: in.Enable,
			AddInbounds: in.AddInbounds, RemoveInbounds: in.RemoveInbounds, MaxIPs: in.MaxIPs, RepID: in.RepID})
		reply(w, map[string]int{"changed": n}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/renew", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Plan string  `json:"plan"`
			GB   float64 `json:"gb"`
			Days int64   `json:"days"`
		}
		if !decode(w, r, &in) {
			return
		}
		res, err := s.RenewClient(r.Context(), who(r), r.PathValue("email"), in.Plan, in.GB, in.Days)
		reply(w, res, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/transfer", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ RepID string }
		if !decode(w, r, &in) {
			return
		}
		err := s.TransferClient(r.Context(), r.PathValue("email"), in.RepID)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/rename", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		err := s.RenameClient(r.Context(), r.PathValue("email"), strings.TrimSpace(in.Name))
		reply(w, map[string]any{"ok": err == nil, "email": strings.TrimSpace(in.Name)}, err)
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
	// draft: a ready inbound (keys made) for the panel's form to start from;
	// nothing is stored.
	mux.HandleFunc("POST /api/v2/admin/inbounds/draft", guard(func(w http.ResponseWriter, r *http.Request) {
		var p PresetRequest
		if !decode(w, r, &p) {
			return
		}
		if p.Port == 0 {
			p.Port = 443
		}
		p.Endpoints = []sub.Endpoint{{Name: "Direct", Address: "0.0.0.0", Port: p.Port}}
		if p.Cert == "" {
			p.Cert = "pending" // TLS drafts: the form picks the certificate
		}
		if p.Preset == "dokodemo" && p.Path == "" {
			p.Path = "127.0.0.1:80"
		}
		tpl, meta, err := BuildPreset(p)
		reply(w, map[string]json.RawMessage{"template": tpl, "meta": meta}, err)
	}))
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
	mux.HandleFunc("POST /api/v2/admin/ssh-install", guard(func(w http.ResponseWriter, r *http.Request) {
		var in SSHInstall
		if !decode(w, r, &in) {
			return
		}
		base := publicBase(subBase, r)
		id, err := s.StartSSHInstall(r.Context(), in, func(tok string) string { return base + "/install/" + tok })
		reply(w, map[string]string{"job": id}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/ssh-install/{job}", guard(func(w http.ResponseWriter, r *http.Request) {
		j, ok := SSHInstallJob(r.PathValue("job"))
		if !ok {
			reply(w, nil, ErrNotFound)
			return
		}
		reply(w, j, nil)
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
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/logs", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Lines  int    `json:"lines"`
			Errors bool   `json:"errors"`
			Grep   string `json:"grep"`
		}
		if !decode(w, r, &in) {
			return
		}
		if len(in.Grep) > 100 {
			in.Grep = in.Grep[:100]
		}
		id, err := s.QueueTask(r.Context(), r.PathValue("id"), wire.TaskXrayLog, in)
		reply(w, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/restart", guard(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.QueueTask(r.Context(), r.PathValue("id"), wire.TaskXrayRestart, nil)
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
		reply(w, map[string]any{"routing": s.Routing(r.Context()), "status": s.RoutingStatus(r.Context()), "geo": s.GeoInfo(r.Context()),
			"paths": s.NodePaths(r.Context()), "warp_default_sites": WARPDefaultSites}, nil)
	}))
	// The simple page: one node, one path, one choice; saved at once.
	mux.HandleFunc("PUT /api/v2/admin/routing/warp-mode", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Node string `json:"node"`
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetNodeWARP(r.Context(), in.Node, in.Path, in.Mode)
		reply(w, map[string]any{"routing": s.Routing(r.Context())}, err)
	}))
	// A WARP test for one node; the node gets its own account first.
	mux.HandleFunc("POST /api/v2/admin/routing/warp-test", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Node string `json:"node"`
		}
		if !decode(w, r, &in) {
			return
		}
		tag, err := s.EnsureWARP(r.Context(), in.Node)
		if err != nil {
			reply(w, nil, err)
			return
		}
		id, err := s.StartOutboundTest(r.Context(), in.Node, tag)
		reply(w, map[string]string{"id": id}, err)
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
	mux.HandleFunc("POST /api/v2/admin/routing/from-link", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Link string `json:"link"`
			Tag  string `json:"tag"`
		}
		if !decode(w, r, &in) {
			return
		}
		o, err := OutboundFromLink(in.Link, in.Tag)
		reply(w, o, err)
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
		err := s.PutInboundChecked(r.Context(), r.PathValue("tag"), in.NodeID, in.Template, in.Meta, enabled)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/inbounds/{tag}/limits", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CapGB float64 `json:"cap_gb"`
			Ends  int64   `json:"ends_unix"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetInboundLimits(r.Context(), r.PathValue("tag"), int64(in.CapGB*(1<<30)), in.Ends)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/inbounds/{tag}/speed", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Kbps int `json:"kbps"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.SetInboundSpeed(r.Context(), r.PathValue("tag"), in.Kbps)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/inbounds/{tag}/clone", guard(func(w http.ResponseWriter, r *http.Request) {
		var in CloneRequest
		if !decode(w, r, &in) {
			return
		}
		err := s.CloneInbound(r.Context(), r.PathValue("tag"), in)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/keys", guard(func(w http.ResponseWriter, r *http.Request) {
		var in KeyRequest
		if !decode(w, r, &in) {
			return
		}
		k, err := GenerateKeys(in)
		reply(w, k, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/clients/{email}/rotate", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Credentials bool `json:"credentials"`
			Link        bool `json:"link"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.RotateClient(r.Context(), r.PathValue("email"), in.Credentials, in.Link)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/links", member(func(w http.ResponseWriter, r *http.Request) {
		if !s.canSeeClient(r, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
		list, err := s.ClientLinks(r.Context(), r.PathValue("email"))
		reply(w, list, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/qr.png", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Text == "" || len(in.Text) > 2800 {
			reply(w, nil, fmt.Errorf("%w: text too long for a QR code", ErrInvalid))
			return
		}
		lvl := qrcode.Medium
		if len(in.Text) > 1200 {
			lvl = qrcode.Low
		}
		png, err := qrcode.Encode(in.Text, lvl, 512)
		if err != nil {
			reply(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(png)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.scopedClients(r.Context(), who(r))
		type view struct {
			ClientView
			SubURL string
		}
		out := make([]view, 0, len(list))
		bases := map[string]string{} // one lookup per reseller, not per client
		for _, c := range list {
			base := subBase
			if c.RepID != "" {
				b, ok := bases[c.RepID]
				if !ok {
					b = s.repBase(r.Context(), c.RepID)
					bases[c.RepID] = b
				}
				if b != "" {
					base = b
				}
			}
			out = append(out, view{c, subURL(base, r, c.SubToken)})
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
	sh := SubHandler(s)
	mux.Handle("GET /sub/{token}", sh)
	mux.Handle("GET /sub/{token}/links", sh)
	mux.Handle("GET /api/v1/", sh) // old-panel subscription URLs
	mux.Handle("GET /app/", sh)    // Telegram Mini App
	mux.Handle("POST /app/", sh)
	routes := mux.apiRoutes()
	mux.HandleFunc("GET /api/v2/admin/docs", member(func(w http.ResponseWriter, r *http.Request) {
		reply(w, routes, nil)
	}))
	return gzipJSON(s.panelGate(mux.ServeMux))
}

func b64(s string) string { return sub.Base64([]string{s}) }

// SubHandler serves only public subscriptions, so it can listen on a public
// address without exposing the owner API.
func SubHandler(s *Store) http.Handler {
	mux := http.NewServeMux()
	registerMiniApp(mux, s)
	var serveSub http.HandlerFunc
	// Subscription URLs from the old panel keep working after a migration:
	// they answer with the same client's v2 subscription.
	legacy := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			tok, err := s.legacySubToken(r.Context(), kind, r.PathValue("token"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			r.SetPathValue("token", tok)
			serveSub(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/inbound-subscriptions/{token}", legacy("managed"))
	mux.HandleFunc("GET /api/v1/subscriptions/{token}", legacy("lab"))
	serveSub = func(w http.ResponseWriter, r *http.Request) {
		email, err := s.EmailBySubToken(r.Context(), r.PathValue("token"))
		if err != nil {
			// A link from Marzban/PasarGuard moved here keeps working.
			tok, ferr := s.foreignSubToken(r.Context(), r.PathValue("token"))
			if ferr != nil {
				http.NotFound(w, r)
				return
			}
			r.SetPathValue("token", tok)
			if email, err = s.EmailBySubToken(r.Context(), tok); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		if err := s.CheckDevice(r.Context(), email, r.Header.Get("X-Hwid"), r.Header.Get("X-Device-Os"), r.Header.Get("X-Device-Model"), r.UserAgent()); err != nil {
			if errors.Is(err, ErrDeviceLimit) {
				http.Error(w, "device limit reached: remove a device in the panel or ask your seller", http.StatusForbidden)
				return
			}
			reply(w, nil, err)
			return
		}
		pageURL := publicBase("", r) + "/sub/" + r.PathValue("token")
		if r.URL.Query().Get("qr") == "1" {
			if c := r.URL.Query().Get("cfg"); c != "" {
				n, _ := strconv.Atoi(c)
				s.configQR(r.Context(), w, email, n)
				return
			}
			serveSubQR(w, pageURL)
			return
		}
		if wantsPage(r) {
			s.serveSubPage(w, r, email, pageURL)
			return
		}
		set, _ := s.brandFor(r.Context(), email)
		w.Header().Set("Profile-Update-Interval", "12")
		w.Header().Set("Profile-Title", "base64:"+b64(or2(set.Title, email)))
		w.Header().Set("Profile-Web-Page-Url", pageURL)
		if set.SupportURL != "" {
			w.Header().Set("Support-Url", set.SupportURL)
		}
		if set.Announce != "" {
			w.Header().Set("Announce", "base64:"+b64(set.Announce))
		}
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
	}
	mux.HandleFunc("GET /sub/{token}", serveSub)
	// PasarGuard's plain list of configs, which sales bots read after creating a user.
	mux.HandleFunc("GET /sub/{token}/links", func(w http.ResponseWriter, r *http.Request) {
		email, err := s.EmailBySubToken(r.Context(), r.PathValue("token"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		links, info, err := s.Subscription(r.Context(), email)
		if err != nil {
			reply(w, nil, err)
			return
		}
		w.Header().Set("Subscription-Userinfo", info)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(strings.Join(links, "\n")))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Custom subscription paths of a 3x-ui panel moved here.
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/sub/") && !strings.HasPrefix(r.URL.Path, "/app/") {
			if tok, format, ok := s.legacyPath(r); ok {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/sub/" + tok
				if format != "" {
					q := r2.URL.Query()
					q.Set("format", format)
					r2.URL.RawQuery = q.Encode()
				}
				mux.ServeHTTP(w, r2)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
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
