package hubcore

import (
	"context"
	"errors"
	"net/http"
)

// A new panel's first steps in one place (the Mini App's "راه‌اندازی پنل"):
// brand, support, card, plans, its own bot and its admins. Each step says
// whether it is done; the panel can hide the list once it does not need it.

// SetupStep is one step of a panel's setup.
type SetupStep struct {
	Key  string `json:"key"` // brand | support | card | plans | bot | admins
	Done bool   `json:"done"`
}

func setupHideKey(rep string) string { return "setup_hide:" + rep }

// PanelSetup lists the steps this manager can take on its panel.
func (s *Store) PanelSetup(ctx context.Context, m miniMgr) []SetupStep {
	rep := m.Seller
	b := s.RepBrand(ctx, rep)
	out := []SetupStep{}
	add := func(key string, ok, done bool) {
		if ok {
			out = append(out, SetupStep{Key: key, Done: done})
		}
	}
	add("brand", m.can("settings"), b.Title != "")
	add("support", m.can("settings"), b.Telegram != "" || b.SupportURL != "")
	cards := false
	if list, err := s.PayCards(ctx, rep); err == nil {
		for _, c := range list {
			cards = cards || c.Enabled && c.Number != ""
		}
	}
	add("card", m.can("cards"), cards)
	var plans int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE seller=? AND status='published'`, rep).Scan(&plans)
	add("plans", m.can("plans") && s.canManageProducts(ctx, m.P) == nil, plans > 0)
	add("bot", m.can("bot"), s.RepBotOf(ctx, rep).HasToken)
	admins, _ := s.TgRepAdmins(ctx, rep)
	add("admins", m.Role == "owner", len(admins) > 0)
	return out
}

// SuggestPlans gives a panel without plans the main store's plans (same
// volume, term and price) to start from; it edits them afterwards.
func (s *Store) SuggestPlans(ctx context.Context, m miniMgr) (int, error) {
	var have int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE seller=? AND status<>'archived'`, m.Seller).Scan(&have); err != nil {
		return 0, err
	}
	if have > 0 {
		return 0, ErrConflict
	}
	all, err := s.Products(ctx, Principal{Role: "owner"}, true)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range all {
		if p.Seller != "" || p.Plan != "volume" || p.Price <= 0 {
			continue
		}
		q := Product{ID: "m" + randomHex(4), Seller: m.Seller, Name: p.Name, Plan: p.Plan, GB: p.GB, Days: p.Days, MaxIPs: p.MaxIPs,
			Price: p.Price, Status: "published", FirstUse: p.FirstUse, Sort: p.Sort}
		if err := s.PutProduct(ctx, m.P, q); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func registerPanelSetup(mux *http.ServeMux, s *Store, mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool) {
	may := func(w http.ResponseWriter, r *http.Request) (miniMgr, bool) {
		m, ok := mgr(w, r)
		if !ok {
			return m, false
		}
		if m.Owner || (m.Role != "owner" && m.Role != "manager") {
			http.Error(w, "این بخش برای مالک و مدیر پنل است.", http.StatusForbidden)
			return m, false
		}
		return m, true
	}
	mux.HandleFunc("GET /app/api/m/setup", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		b := s.RepBrand(ctx, m.Seller)
		reply(w, map[string]any{"steps": s.PanelSetup(ctx, m), "hidden": s.Setting(ctx, setupHideKey(m.Seller)) != "",
			"brand": map[string]string{"title": b.Title, "announce": b.Announce, "accent": b.Accent, "telegram": b.Telegram, "support_url": b.SupportURL, "logo": b.Logo}}, nil)
	})
	mux.HandleFunc("POST /app/api/m/setup/hide", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok {
			return
		}
		var in struct {
			Hide bool `json:"hide"`
		}
		if !decode(w, r, &in) {
			return
		}
		v := ""
		if in.Hide {
			v = "1"
		}
		reply(w, map[string]bool{"ok": true}, s.SetSetting(r.Context(), setupHideKey(m.Seller), v))
	})
	mux.HandleFunc("POST /app/api/m/setup/plans", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok || deny(w, m, "plans") {
			return
		}
		n, err := s.SuggestPlans(r.Context(), m)
		if errors.Is(err, ErrConflict) {
			http.Error(w, "پنل شما پلن دارد؛ از «پلن‌ها و قیمت‌ها» ویرایششان کنید.", http.StatusConflict)
			return
		}
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]int{"made": n}, nil)
	})
}
