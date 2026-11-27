package hubcore

import (
	"html"
	"net/http"
	"strconv"
	"strings"
)

// Management in the Mini App: customers (search, a direct message, wallet,
// ban), the bot's admins (invite by role, change, remove) and each
// sub-seller's limits and a message to it.

func registerMiniAdmin5(mux *http.ServeMux, s *Store,
	mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool,
	read func(http.ResponseWriter, *http.Request, any) bool) {
	ok := func(w http.ResponseWriter, err error) {
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]bool{"ok": true}, nil)
	}

	// ---------- customers ----------
	mux.HandleFunc("GET /app/api/m/customers", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		list, total, err := s.SellerCustomers(r.Context(), m.Seller, r.URL.Query().Get("q"), 200)
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]any{"customers": list, "total": total}, nil)
	})
	mux.HandleFunc("POST /app/api/m/customer", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		var in struct {
			Chat   int64  `json:"chat"`
			Action string `json:"action"` // message | ban | unban | wallet
			Text   string `json:"text"`
			Delta  int64  `json:"delta"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if !s.IsCustomer(ctx, m.Seller, in.Chat) {
			miniErr(w, ErrNotFound)
			return
		}
		switch in.Action {
		case "message":
			t := strings.TrimSpace(in.Text)
			if t == "" || len([]rune(t)) > 3000 {
				http.Error(w, "متن پیام را بنویسید.", http.StatusBadRequest)
				return
			}
			s.tellFrom(ctx, m.Seller, in.Chat, "💬 <b>پیام از پشتیبانی</b>\n"+html.EscapeString(t))
			ok(w, nil)
		case "ban", "unban":
			ok(w, s.SetTgBan(ctx, m.Seller, in.Chat, in.Action == "ban"))
		case "wallet":
			if in.Delta == 0 || in.Delta > maxToman || in.Delta < -maxToman {
				http.Error(w, "مبلغ را بنویسید.", http.StatusBadRequest)
				return
			}
			bal, err := s.TomanAdjust(ctx, m.Seller, in.Chat, in.Delta, strings.TrimSpace(in.Text), m.P.User)
			if err != nil {
				miniErr(w, err)
				return
			}
			reply(w, map[string]int64{"balance": bal}, nil)
		default:
			http.Error(w, "bad action", http.StatusBadRequest)
		}
	})

	// ---------- the bot's admins (the reseller itself only) ----------
	repOwner := func(w http.ResponseWriter, m miniMgr) bool {
		if m.Owner || m.Role != "owner" {
			http.Error(w, "فقط خود نماینده ادمین‌ها را مدیریت می‌کند.", http.StatusForbidden)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /app/api/m/admins", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || !repOwner(w, m) {
			return
		}
		ctx := r.Context()
		list, err := s.TgRepAdmins(ctx, m.Seller)
		if err != nil {
			miniErr(w, err)
			return
		}
		type admin struct {
			Chat     int64    `json:"chat"`
			Role     string   `json:"role"`
			Name     string   `json:"name"`
			Username string   `json:"username"`
			Perms    []string `json:"perms"`
		}
		out := []admin{}
		for _, a := range list {
			var user string
			_ = s.ro.QueryRowContext(ctx, `SELECT username FROM tg_users WHERE chat_id=?`, a.ChatID).Scan(&user)
			out = append(out, admin{Chat: a.ChatID, Role: a.Role, Name: firstNonEmpty(a.Name, s.TgName(ctx, a.ChatID)), Username: user, Perms: a.Perms})
		}
		reply(w, map[string]any{"admins": out, "bot": s.BotUsernameFor(ctx, m.Seller), "all_perms": AdminPerms}, nil)
	})
	mux.HandleFunc("POST /app/api/m/admins/invite", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || !repOwner(w, m) {
			return
		}
		var in struct {
			Role string `json:"role"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		code, err := s.NewTgAdminInvite(ctx, m.Seller, in.Role)
		if err != nil {
			http.Error(w, "نقش را انتخاب کنید.", http.StatusBadRequest)
			return
		}
		bot := s.BotUsernameFor(ctx, m.Seller)
		reply(w, map[string]string{"link": "https://t.me/" + bot + "?start=adm_" + code}, nil)
	})
	mux.HandleFunc("POST /app/api/m/admins/set", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || !repOwner(w, m) {
			return
		}
		var in struct {
			Chat int64  `json:"chat"`
			Role string `json:"role"` // manager | sales | support | "" = remove
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if in.Role == "" {
			ok(w, s.RemoveTgRepAdmin(ctx, m.Seller, in.Chat))
			return
		}
		if in.Role != "manager" && in.Role != "sales" && in.Role != "support" {
			http.Error(w, "نقش نامعتبر است.", http.StatusBadRequest)
			return
		}
		res, err := s.db.ExecContext(ctx, `UPDATE rep_tg_admins SET role=? WHERE rep_id=? AND chat_id=?`, in.Role, m.Seller, in.Chat)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				err = ErrNotFound
			}
		}
		ok(w, err)
	})

	mux.HandleFunc("POST /app/api/m/admins/perms", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || !repOwner(w, m) {
			return
		}
		var in struct {
			Chat  int64    `json:"chat"`
			Perms []string `json:"perms"`
		}
		if !read(w, r, &in) {
			return
		}
		ok(w, s.SetTgRepAdminPerms(r.Context(), m.Seller, in.Chat, in.Perms))
	})

	// ---------- a sub-seller's limits, and a message to it ----------
	mux.HandleFunc("GET /app/api/m/sellers/policy", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		ctx := r.Context()
		id := r.URL.Query().Get("id")
		if _, mine := s.mgrSellerOf(ctx, m, id); !mine {
			miniErr(w, ErrNotFound)
			return
		}
		p, err := s.RepPolicyOf(ctx, id)
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]any{"max_clients": p.MaxClients, "max_days": p.MaxDays, "max_client_gb": p.MaxClientGB, "allow_unlimited": p.AllowUnlimited,
			"can_delete": p.CanDelete, "deny_create": p.DenyCreate, "deny_renew": p.DenyRenew}, nil)
	})
	mux.HandleFunc("POST /app/api/m/sellers/policy", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		var in struct {
			ID             string `json:"id"`
			MaxClients     int64  `json:"max_clients"`
			MaxDays        int64  `json:"max_days"`
			MaxClientGB    int64  `json:"max_client_gb"`
			AllowUnlimited bool   `json:"allow_unlimited"`
			CanDelete      bool   `json:"can_delete"`
			DenyCreate     bool   `json:"deny_create"`
			DenyRenew      bool   `json:"deny_renew"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if _, mine := s.mgrSellerOf(ctx, m, in.ID); !mine {
			miniErr(w, ErrNotFound)
			return
		}
		p, err := s.RepPolicyOf(ctx, in.ID)
		if err != nil {
			miniErr(w, err)
			return
		}
		p.MaxClients, p.MaxDays, p.MaxClientGB, p.AllowUnlimited, p.CanDelete, p.DenyCreate, p.DenyRenew = in.MaxClients, in.MaxDays, in.MaxClientGB, in.AllowUnlimited, in.CanDelete, in.DenyCreate, in.DenyRenew
		if m.Owner {
			ok(w, s.SetRepPolicy(ctx, in.ID, p))
			return
		}
		ok(w, s.SetSellerPolicy(ctx, m.Seller, in.ID, p))
	})
	mux.HandleFunc("POST /app/api/m/sellers/message", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		var in struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		t := strings.TrimSpace(in.Text)
		if t == "" || len([]rune(t)) > 3000 {
			http.Error(w, "متن پیام را بنویسید.", http.StatusBadRequest)
			return
		}
		if _, mine := s.mgrSellerOf(ctx, m, in.ID); !mine {
			miniErr(w, ErrNotFound)
			return
		}
		chats := s.TgRepOwners(ctx, in.ID)
		if len(chats) == 0 {
			http.Error(w, "این نماینده تلگرام وصل‌شده‌ای ندارد.", http.StatusBadRequest)
			return
		}
		from := "مدیر پنل"
		if !m.Owner {
			from = "نمایندهٔ بالادست (" + html.EscapeString(m.Seller) + ")"
		}
		for _, c := range chats {
			s.tellFrom(ctx, m.Seller, c, "📩 <b>پیام از "+from+"</b>\n"+html.EscapeString(t))
		}
		reply(w, map[string]string{"sent": strconv.Itoa(len(chats))}, nil)
	})
}
