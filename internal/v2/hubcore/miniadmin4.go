package hubcore

import (
	"net/http"
	"strings"
)

// Management in the Mini App: the bot's settings — welcome text, text above
// the plans, free trial, required channels, the store's look (resellers),
// discount codes and a message to every customer.

func registerMiniAdmin4(mux *http.ServeMux, s *Store,
	mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool,
	read func(http.ResponseWriter, *http.Request, any) bool) {
	ok := func(w http.ResponseWriter, err error) {
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]bool{"ok": true}, nil)
	}
	mux.HandleFunc("GET /app/api/m/settings", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		ctx := r.Context()
		sub := !m.Owner && s.IsSubSeller(ctx, m.Seller)
		out := map[string]any{"welcome": s.Welcome(ctx, m.Seller), "shop_text": s.ShopText(ctx, m.Seller), "join": s.JoinPolicy(ctx, m.Seller),
			"owner": m.Owner, "sub_seller": sub, "theme": s.Setting(ctx, "theme:"+m.Seller)}
		if !sub {
			out["trial"] = s.TrialPolicyFor(ctx, m.Seller)
		}
		if !m.Owner {
			b := s.RepBrand(ctx, m.Seller)
			out["brand"] = map[string]string{"title": b.Title, "announce": b.Announce, "accent": b.Accent, "telegram": b.Telegram, "support_url": b.SupportURL, "logo": b.Logo}
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/m/settings", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		var in struct {
			Welcome  *string      `json:"welcome"`
			Theme    *string      `json:"theme"`
			ShopText *string      `json:"shop_text"`
			Join     *JoinPolicy  `json:"join"`
			Trial    *TrialPolicy `json:"trial"`
			Brand    *struct {
				Title      string  `json:"title"`
				Announce   string  `json:"announce"`
				Accent     string  `json:"accent"`
				Telegram   string  `json:"telegram"`
				SupportURL string  `json:"support_url"`
				Logo       *string `json:"logo"` // nil = keep, "" = remove
			} `json:"brand"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if in.Theme != nil {
			switch *in.Theme {
			case "", "teal", "emerald", "rose", "coral":
			default:
				http.Error(w, "تم نامعتبر است.", http.StatusBadRequest)
				return
			}
			if err := s.SetSetting(ctx, "theme:"+m.Seller, *in.Theme); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Welcome != nil {
			if err := s.PutWelcome(ctx, m.Seller, *in.Welcome); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.ShopText != nil {
			if err := s.PutShopText(ctx, m.Seller, *in.ShopText); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Join != nil {
			if err := s.PutJoinPolicy(ctx, m.Seller, *in.Join); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Trial != nil {
			t := *in.Trial
			t.Inbounds = s.TrialPolicyFor(ctx, m.Seller).Inbounds // not shown in the app: kept
			if err := s.PutTrialPolicyFor(ctx, m.Seller, t); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Brand != nil && !m.Owner {
			b := s.RepBrand(ctx, m.Seller)
			b.Title, b.Announce, b.Accent, b.Telegram, b.SupportURL = in.Brand.Title, in.Brand.Announce, strings.ToLower(strings.TrimSpace(in.Brand.Accent)), in.Brand.Telegram, in.Brand.SupportURL
			if in.Brand.Logo != nil {
				b.Logo = *in.Brand.Logo
			}
			if err := s.PutRepBrand(ctx, m.Seller, b); err != nil {
				miniErr(w, err)
				return
			}
		}
		ok(w, nil)
	})

	// ---------- discount codes ----------
	noCodes := func(w http.ResponseWriter, r *http.Request, m miniMgr) bool {
		if !m.Owner && s.IsSubSeller(r.Context(), m.Seller) {
			http.Error(w, "زیرمجموعه‌ها پلن خودشان را ندارند و کد تخفیف هم ندارند.", http.StatusForbidden)
			return true
		}
		return false
	}
	mux.HandleFunc("GET /app/api/m/discounts", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") || noCodes(w, r, m) {
			return
		}
		list, err := s.DiscountsOf(r.Context(), m.Seller)
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, list, nil)
	})
	mux.HandleFunc("POST /app/api/m/discount", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") || noCodes(w, r, m) {
			return
		}
		var d Discount
		if !read(w, r, &d) {
			return
		}
		ok(w, s.PutDiscountFor(r.Context(), m.Seller, d))
	})
	mux.HandleFunc("POST /app/api/m/discount/delete", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") || noCodes(w, r, m) {
			return
		}
		var in struct {
			Code string `json:"code"`
		}
		if !read(w, r, &in) {
			return
		}
		ok(w, s.DeleteDiscountFor(r.Context(), m.Seller, in.Code))
	})

	// ---------- a message to every customer ----------
	mux.HandleFunc("GET /app/api/m/broadcast", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		if appBot.Audience == nil {
			reply(w, map[string]any{"on": false}, nil)
			return
		}
		a := appBot.Audience(r.Context(), m.Seller)
		reply(w, map[string]any{"on": a != nil, "audience": a}, nil)
	})
	mux.HandleFunc("POST /app/api/m/broadcast", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "settings") {
			return
		}
		var in struct {
			Text     string `json:"text"`
			Audience string `json:"audience"`
		}
		if !read(w, r, &in) {
			return
		}
		in.Text = strings.TrimSpace(in.Text)
		switch in.Audience {
		case "all", "active", "expiring", "expired", "none", "test":
		default:
			http.Error(w, "گروه گیرنده را انتخاب کنید.", http.StatusBadRequest)
			return
		}
		if in.Text == "" || len([]rune(in.Text)) > 3500 {
			http.Error(w, "متن پیام را بنویسید (تا ۳۵۰۰ حرف).", http.StatusBadRequest)
			return
		}
		if appBot.Broadcast == nil {
			http.Error(w, "ربات خاموش است.", http.StatusServiceUnavailable)
			return
		}
		n, err := appBot.Broadcast(r.Context(), m.Seller, m.Chat, in.Text, in.Audience)
		if err != nil {
			http.Error(w, "ربات شما روشن نیست؛ اول در «ربات من» روشنش کنید.", http.StatusServiceUnavailable)
			return
		}
		reply(w, map[string]int{"count": n}, nil)
	})
}
