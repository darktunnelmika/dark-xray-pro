package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Management in the Mini App: the sale settings (link address, cleanup of
// dead configs, quick replies for tickets), and the customer's "can't
// connect" help that turns into a ticket with everything support needs.

func quickKey(seller string) string { return "quick_replies:" + seller }

// QuickReplies are a seller's ready ticket answers.
func (s *Store) QuickReplies(ctx context.Context, seller string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(s.Setting(ctx, quickKey(seller))), &out)
	return out
}

func (s *Store) PutQuickReplies(ctx context.Context, seller string, list []string) error {
	keep := []string{}
	for _, v := range list {
		if v = strings.TrimSpace(v); v != "" {
			if utf8.RuneCountInString(v) > 500 {
				return fmt.Errorf("%w: too long", ErrInvalid)
			}
			keep = append(keep, v)
		}
	}
	if len(keep) > 12 {
		return fmt.Errorf("%w: at most 12", ErrInvalid)
	}
	b, _ := json.Marshal(keep)
	return s.SetSetting(ctx, quickKey(seller), string(b))
}

// mgrMoney is the seller's toman income: delivered card and wallet orders,
// today and this month (Tehran).
func (s *Store) mgrMoney(ctx context.Context, seller string) (today, month, monthOrders int64) {
	day := s.tehranToday()
	ms := monthStart(s.now())
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN created_unix>=? THEN amount END),0),COALESCE(SUM(amount),0),COUNT(*)
FROM orders WHERE seller=? AND status='fulfilled' AND method<>'stars' AND created_unix>=?`, day, seller, min(day, ms)).Scan(&today, &month, &monthOrders)
	return
}

func registerMiniAdmin6(mux *http.ServeMux, s *Store,
	mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool,
	read func(http.ResponseWriter, *http.Request, any) bool, base func(*http.Request) string) {
	mux.HandleFunc("GET /app/api/m/sale", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		ctx := r.Context()
		ac := s.AutoCleanFor(ctx, m.Seller)
		dead, _ := s.DeadClients(ctx, m.Seller, ac.Days)
		out := map[string]any{"autoclean": ac, "dead": len(dead), "quick": s.QuickReplies(ctx, m.Seller)}
		if !m.Owner {
			out["domains"] = s.SubDomainChoices(ctx)
			out["domain"] = s.RepBrand(ctx, m.Seller).BaseURL
			out["default_domain"] = s.defaultBase(ctx, base(r))
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/m/sale", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		var in struct {
			AutoClean *AutoClean `json:"autoclean"`
			Quick     *[]string  `json:"quick"`
			Domain    *string    `json:"domain"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if in.AutoClean != nil {
			if err := s.PutAutoClean(ctx, m.Seller, *in.AutoClean); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Quick != nil {
			if err := s.PutQuickReplies(ctx, m.Seller, *in.Quick); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Domain != nil && !m.Owner {
			if err := s.PickRepDomain(ctx, m.Seller, *in.Domain); err != nil {
				miniErr(w, err)
				return
			}
		}
		reply(w, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("GET /app/api/m/custom", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "plans") {
			return
		}
		reply(w, s.CustomPlanFor(r.Context(), m.Seller), nil)
	})
	mux.HandleFunc("POST /app/api/m/custom", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "plans") {
			return
		}
		var c CustomPlan
		if !read(w, r, &c) {
			return
		}
		if err := s.PutCustomPlanFor(r.Context(), m.Seller, c); err != nil {
			miniErr(w, err)
			return
		}
		reply(w, s.CustomPlanFor(r.Context(), m.Seller), nil)
	})
	// Quick replies are also read by whoever answers tickets.
	mux.HandleFunc("GET /app/api/m/quick", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "tickets") {
			return
		}
		reply(w, s.QuickReplies(r.Context(), m.Seller), nil)
	})

	// The customer's "can't connect": a ticket with the service's state,
	// the device and the app, sent to the seller's support.
	mux.HandleFunc("POST /app/api/help", func(w http.ResponseWriter, r *http.Request) {
		u, seller, err := s.verifyMiniUser(r.Context(), r.Header.Get("X-Init-Data"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in struct {
			Service string `json:"service"`
			Device  string `json:"device"`
			App     string `json:"app"`
			Note    string `json:"note"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if s.TgBanned(ctx, seller, u.ID) {
			http.Error(w, "⛔️ دسترسی شما محدود شده است.", http.StatusForbidden)
			return
		}
		var sb strings.Builder
		sb.WriteString("🆘 وصل نمی‌شوم (از مینی‌اپ)\n")
		if in.Service != "" {
			mine := false
			names, _ := s.TgClients(ctx, u.ID)
			if seller != "" {
				names, _ = s.TgClientsOf(ctx, u.ID, seller)
			}
			for _, n := range names {
				mine = mine || n == in.Service
			}
			if mine {
				if c, err := s.ClientByEmail(ctx, in.Service); err == nil {
					st := miniClientOf(c, s.now().Unix())
					fmt.Fprintf(&sb, "سرویس: %s · وضعیت: %s · مصرف: %.1f از %s گیگ", c.Email, st.Status, float64(st.Used)/(1<<30),
						map[bool]string{true: "نامحدود", false: fmt.Sprintf("%.1f", float64(st.Total)/(1<<30))}[st.Total < 0])
					if st.Until > 0 {
						fmt.Fprintf(&sb, " · %d روز مانده", max(0, (st.Until-s.now().Unix())/86400))
					}
					if st.LastSeen > 0 {
						fmt.Fprintf(&sb, " · آخرین اتصال: %d ساعت پیش", (s.now().Unix()-st.LastSeen)/3600)
					} else {
						sb.WriteString(" · هنوز هیچ اتصالی ثبت نشده")
					}
					sb.WriteString("\n")
				}
			}
		}
		clean := func(v string, n int) string {
			v = strings.TrimSpace(v)
			if r := []rune(v); len(r) > n {
				v = string(r[:n])
			}
			return v
		}
		if d := clean(in.Device, 40); d != "" {
			fmt.Fprintf(&sb, "دستگاه: %s\n", d)
		}
		if a := clean(in.App, 40); a != "" {
			fmt.Fprintf(&sb, "برنامه: %s\n", a)
		}
		if n := clean(in.Note, 1000); n != "" {
			fmt.Fprintf(&sb, "توضیح مشتری: %s\n", n)
		}
		text := strings.TrimSpace(sb.String())
		id, _, err := s.CustomerTicket(ctx, seller, u.ID, u.FirstName, text)
		if err != nil {
			miniErr(w, err)
			return
		}
		alert := fmt.Sprintf("🎫 تیکت #%d از %s\n%s\n\nجواب از مینی‌اپ ← مدیریت ← بیشتر ← تیکت‌ها.", id, html.EscapeString(u.FirstName), html.EscapeString(text))
		if seller == "" {
			notifyOwners(ctx, alert)
		} else {
			for _, c := range s.TgRepChats(ctx, seller) {
				s.tellFrom(ctx, seller, c, alert)
			}
		}
		reply(w, map[string]int64{"ticket": id}, nil)
	})
}
