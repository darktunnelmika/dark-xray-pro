package hubcore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Management in the Mini App, stage 2.5: what Tishtar's app has that ours
// still lacked — adding volume or days without a reset, panel sale (the
// special reseller's sold panels, its wallet and bills, its prices; the
// owner's packages), panel orders in the payments inbox, and buying a
// reseller panel from the customer store.

// mgrAdd adds volume and/or days to a client without starting a new term.
func (s *Store) mgrAdd(ctx context.Context, m miniMgr, c ClientView, addBytes, addDays int64) error {
	if m.Owner {
		_, err := s.BulkExtend(ctx, BulkRequest{Emails: []string{c.Email}, AddDays: addDays, AddBytes: addBytes})
		return err
	}
	cl := c.Client
	plan := c.Plan
	if plan == "" {
		plan = map[bool]string{true: "unlimited", false: "volume"}[c.VolumeBytes < 0]
	}
	if addBytes > 0 {
		if cl.VolumeBytes < 0 {
			return fmt.Errorf("%w: an unlimited client has no volume to add", ErrInvalid)
		}
		cl.VolumeBytes += addBytes
	}
	if addDays > 0 {
		now := s.now().Unix()
		switch {
		case c.EffectiveUntil > 0:
			cl.ExpiresUnix = max(c.EffectiveUntil, now) + addDays*86400
		case cl.DurationSeconds > 0:
			cl.DurationSeconds += addDays * 86400
		default:
			cl.ExpiresUnix = now + addDays*86400
		}
	}
	return s.RepPutClient(ctx, m.P.User, m.Seller, cl, plan)
}

// pendingRepOrders are the panel orders (new panels, panel top-ups and, for
// the owner, resellers' wallet top-ups) that wait for this seller.
func (s *Store) pendingRepOrders(ctx context.Context, seller string) ([]RepOrder, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+repOrderCols+` FROM rep_orders WHERE seller=? AND status='pending' AND (kind IN ('new','topup') OR (?='' AND kind='wallet')) ORDER BY id DESC LIMIT 100`, seller, seller)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepOrder{}
	for rows.Next() {
		o, err := scanRepOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func repOrderTitle(o RepOrder) string {
	switch o.Kind {
	case "new":
		return fmt.Sprintf("پنل نمایندگی %s (%d گیگ)", o.RepID, o.GB)
	case "topup":
		return fmt.Sprintf("شارژ پنل %s (%d گیگ)", o.RepID, o.GB)
	}
	return "شارژ کیف پول نماینده " + o.RepID
}

// mgrDecidePanel approves or refuses a panel order and tells the buyer, as
// the bot does.
func (s *Store) mgrDecidePanel(ctx context.Context, m miniMgr, id int64, ok bool) error {
	o, err := s.RepOrderByID(ctx, id)
	if err != nil || o.Seller != m.Seller || o.Status != "pending" || (o.Kind == "wallet" && !m.Owner) {
		return ErrNotFound
	}
	if !ok {
		if _, err := s.RejectRepOrder(ctx, m.P.User, id); err != nil {
			return err
		}
		s.tellFrom(ctx, o.Seller, o.Chat, fmt.Sprintf("❌ رسید سفارش #%d تأیید نشد. اگر اشتباهی شده، با پشتیبانی در تماس باشید.", id))
		return nil
	}
	res, err := s.ApproveRepOrder(ctx, m.P.User, m.Seller, id)
	if err != nil {
		return err
	}
	switch o.Kind {
	case "wallet":
		s.tell(ctx, o.Chat, fmt.Sprintf("✅ کیف پول شما %s تومان شارژ شد. موجودی: %s تومان.", thousands(o.Price), thousands(res.Wallet)))
	case "topup":
		msg := fmt.Sprintf("✅ پنل شما شارژ شد: %d گیگ اضافه شد.\nاعتبار فعلی: %d گیگ", o.GB, s.RepCredit(ctx, o.RepID)>>30)
		if res.Converted {
			msg += "\n\n🎉 پنل تستی شما دائمی شد و همهٔ کاربرهایش دوباره روشن شدند."
		}
		s.tellFrom(ctx, o.Seller, o.Chat, msg)
	default:
		s.tellFrom(ctx, o.Seller, o.Chat, fmt.Sprintf("🎉 پنل نمایندگی شما آماده است\n🌐 آدرس: %s\n👤 نام کاربری: %s\n🔑 رمز موقت: %s\n💳 اعتبار: %d گیگ\n\nهمین ربات هم برای شما به حالت نمایندگی رفته است: /start را بزنید و «📱 پنل مدیریت» را باز کنید.",
			miniBase, o.RepID, res.Password, o.GB))
	}
	return nil
}

func registerMiniAdmin3(mux *http.ServeMux, s *Store,
	mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool,
	read func(http.ResponseWriter, *http.Request, any) bool) {
	ok := func(w http.ResponseWriter, err error) {
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]bool{"ok": true}, nil)
	}

	// ---------- add volume / days ----------
	mux.HandleFunc("POST /app/api/m/add", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "renew") {
			return
		}
		var in struct {
			Emails []string `json:"emails"`
			GB     float64  `json:"gb"`
			Days   int64    `json:"days"`
		}
		if !read(w, r, &in) {
			return
		}
		if in.GB < 0 || in.Days < 0 || in.Days > 3650 || in.GB > 100000 || (in.GB == 0 && in.Days == 0) {
			http.Error(w, "حجم یا روز را بنویسید.", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		done := 0
		for _, e := range in.Emails {
			c, mine := s.mgrOwns(ctx, m, e)
			if !mine {
				continue
			}
			if err := s.mgrAdd(ctx, m, c, int64(in.GB*(1<<30)), in.Days); err != nil {
				if done == 0 {
					miniErr(w, err)
					return
				}
				break
			}
			done++
		}
		reply(w, map[string]int{"done": done}, nil)
	})

	// ---------- panel sale ----------
	resaleRep := func(w http.ResponseWriter, r *http.Request, m miniMgr) bool {
		if m.Owner || s.IsSubSeller(r.Context(), m.Seller) || !s.ResaleOf(r.Context(), m.Seller).Allowed {
			http.Error(w, "فروش پنل برای شما فعال نیست؛ از مدیر پنل بخواهید.", http.StatusForbidden)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /app/api/m/resale", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		ctx := r.Context()
		if m.Owner {
			sale := s.RepSale(ctx)
			pend, _ := s.pendingRepOrders(ctx, "")
			reply(w, map[string]any{"owner": true, "enabled": sale.Enabled, "packages": sale.Packages, "wholesale_tb": sale.WholesaleTB,
				"has_card": s.Card(ctx).Number != "", "pending": len(pend)}, nil)
			return
		}
		if s.IsSubSeller(ctx, m.Seller) || !s.ResaleOf(ctx, m.Seller).Allowed {
			reply(w, map[string]any{"allowed": false}, nil)
			return
		}
		panels, st, err := s.SoldPanels(ctx, m.Seller)
		if err != nil {
			miniErr(w, err)
			return
		}
		pend, _ := s.pendingRepOrders(ctx, m.Seller)
		reply(w, map[string]any{"allowed": true, "view": s.ResaleViewOf(ctx, m.Seller), "panels": panels, "stats": st, "pending": len(pend)}, nil)
	})
	mux.HandleFunc("POST /app/api/m/resale/sell", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !resaleRep(w, r, m) {
			return
		}
		var in struct {
			Target  string `json:"target"`
			Name    string `json:"name"`
			Buyer   string `json:"buyer"`
			Package string `json:"package"`
		}
		if !read(w, r, &in) {
			return
		}
		res, err := s.SellPanel(r.Context(), m.P.User, m.Seller, in.Target, strings.ToLower(strings.TrimSpace(in.Name)), strings.TrimSpace(in.Buyer), in.Package)
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]any{"username": res.Order.RepID, "password": res.Password, "bill": res.Bill, "cost": res.Order.Cost, "address": miniBase}, nil)
	})
	mux.HandleFunc("POST /app/api/m/resale/prices", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !resaleRep(w, r, m) {
			return
		}
		var in struct {
			Enabled bool             `json:"enabled"`
			Prices  map[string]int64 `json:"prices"`
		}
		if !read(w, r, &in) {
			return
		}
		ok(w, s.PutResaleRep(r.Context(), m.Seller, in.Enabled, in.Prices))
	})
	mux.HandleFunc("POST /app/api/m/resale/panel", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !resaleRep(w, r, m) {
			return
		}
		var in struct {
			ID       string `json:"id"`
			On       bool   `json:"on"`
			Password bool   `json:"password"` // a new password instead
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if !s.IsSellerOf(ctx, m.Seller, in.ID) {
			miniErr(w, ErrNotFound)
			return
		}
		if !in.Password {
			ok(w, s.SetSoldPanelEnabled(ctx, m.Seller, in.ID, in.On))
			return
		}
		user := in.ID
		if l := s.RepLogins(ctx, in.ID); len(l) > 0 {
			user = l[0]
		}
		pass := randomHex(6)
		if err := s.SetRepLogin(ctx, user, pass, in.ID); err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]string{"username": user, "password": pass}, nil)
	})
	mux.HandleFunc("POST /app/api/m/resale/bill", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !resaleRep(w, r, m) {
			return
		}
		var in struct {
			ID int64 `json:"id"`
		}
		if !read(w, r, &in) {
			return
		}
		ok(w, s.PayRepBill(r.Context(), m.Seller, in.ID))
	})
	mux.HandleFunc("POST /app/api/m/resale/wallet", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !resaleRep(w, r, m) {
			return
		}
		var in struct {
			Amount int64 `json:"amount"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		o, err := s.CreateRepWalletOrder(ctx, m.Chat, firstNonEmpty(s.TgName(ctx, m.Chat), m.Seller), m.Seller, in.Amount)
		if err != nil {
			miniErr(w, err)
			return
		}
		c := s.CardFor(ctx, "")
		reply(w, map[string]any{"id": o.ID, "price": o.Price, "card": map[string]string{"number": c.Number, "holder": c.Holder, "bank": c.Bank}, "bot": s.BotUsernameFor(ctx, "")}, nil)
	})
	// The owner's panel packages: on/off, name, GB and price (the rest of
	// each package's settings are kept; new ones start from the defaults).
	mux.HandleFunc("POST /app/api/m/resale/sale", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		if !m.Owner {
			http.Error(w, "این بخش برای مدیر پنل است.", http.StatusForbidden)
			return
		}
		var in struct {
			Enabled  bool `json:"enabled"`
			Packages []struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				GB    int64  `json:"gb"`
				Price int64  `json:"price"`
			} `json:"packages"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		cur := s.RepSale(ctx)
		old := map[string]RepPackage{}
		for _, p := range cur.Packages {
			old[p.ID] = p
		}
		next := RepSale{Enabled: in.Enabled, WholesaleTB: cur.WholesaleTB, Packages: []RepPackage{}}
		for _, p := range in.Packages {
			np, had := old[p.ID]
			if !had {
				np = RepPackage{ID: p.ID, AllowUnlimited: DefaultRepPolicy.AllowUnlimited, CanDelete: DefaultRepPolicy.CanDelete}
			}
			np.Name, np.GB, np.Price = strings.TrimSpace(p.Name), p.GB, p.Price
			next.Packages = append(next.Packages, np)
		}
		ok(w, s.PutRepSale(ctx, next))
	})

	// ---------- the customer store: buying a reseller panel ----------
	buyer := func(w http.ResponseWriter, r *http.Request) (TgUser, string, bool) {
		u, seller, err := s.verifyMiniUser(r.Context(), r.Header.Get("X-Init-Data"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return u, "", false
		}
		if s.TgBanned(r.Context(), seller, u.ID) {
			http.Error(w, "⛔️ دسترسی شما محدود شده است.", http.StatusForbidden)
			return u, "", false
		}
		return u, seller, true
	}
	panelSaleOn := func(ctx context.Context, seller string) bool {
		if seller == "" {
			return s.RepSale(ctx).Enabled
		}
		return s.ResaleOn(ctx, seller)
	}
	// mayBuy: as in the bot, a reseller and its bot admins buy nothing in
	// its own bot; in the main bot, a chat that only helps run someone
	// else's panel buys nothing.
	mayBuy := func(ctx context.Context, seller string, chat int64) bool {
		rep := s.TgRep(ctx, chat)
		role, _ := s.TgRepRole(ctx, chat)
		if seller != "" {
			return rep != seller && role != seller
		}
		return !(rep == "" && role != "")
	}
	// trialFor: the main bot's free trial panel, for a chat that runs or
	// helps run no panel and has not had one.
	trialFor := func(ctx context.Context, seller string, chat int64) (RepTrial, bool) {
		t := s.RepTrialPolicy(ctx)
		if seller != "" || !t.On || s.RepTrialUsed(ctx, chat) || slices.Contains(s.TelegramOwners(ctx), chat) {
			return t, false
		}
		rep, role := s.TgRepRole(ctx, chat)
		return t, rep == "" && role == "" && s.TgRep(ctx, chat) == ""
	}
	mux.HandleFunc("GET /app/api/panels", func(w http.ResponseWriter, r *http.Request) {
		u, seller, okb := buyer(w, r)
		if !okb {
			return
		}
		ctx := r.Context()
		if !panelSaleOn(ctx, seller) || !mayBuy(ctx, seller, u.ID) {
			if t, ok := trialFor(ctx, seller, u.ID); ok { // the free trial without the sale
				reply(w, map[string]any{"on": true, "packages": []any{}, "trial": map[string]any{"gb": t.GB, "days": t.Days, "max_clients": t.MaxClients}}, nil)
				return
			}
			reply(w, map[string]any{"on": false}, nil)
			return
		}
		pk := s.RepSale(ctx).Packages
		if seller != "" {
			pk = s.ResalePackages(ctx, seller)
		}
		list := []map[string]any{}
		for _, p := range s.RepSale(ctx).Packages {
			if seller != "" && !hasPkg(pk, p.ID) {
				continue
			}
			price, base := s.PanelPrice(ctx, seller, p)
			list = append(list, map[string]any{"id": p.ID, "name": p.Name, "gb": p.GB, "price": price, "base": base, "unlimited": p.AllowUnlimited, "units": p.Units,
				"max_clients": p.MaxClients, "max_days": p.MaxDays, "can_delete": p.CanDelete})
		}
		lim := s.PanelLimitsOf(ctx)
		out := map[string]any{"on": len(list) > 0, "packages": list, "promo": s.promoView(ctx, seller),
			"owned": s.BuyerPanels(ctx, u.ID), "max": lim.PerBuyer}
		if seller != "" && s.SellerPanels(ctx, seller) >= lim.PerSeller {
			out["full"] = true // the reseller sold all it may: top-ups only
		}
		if t, ok := trialFor(ctx, seller, u.ID); ok {
			out["trial"] = map[string]any{"gb": t.GB, "days": t.Days, "max_clients": t.MaxClients}
			out["on"] = true
		}
		reply(w, out, nil)
	})
	// The free trial panel from the Mini App (main bot), as the bot's "tpn".
	mux.HandleFunc("POST /app/api/paneltrial", func(w http.ResponseWriter, r *http.Request) {
		u, seller, okb := buyer(w, r)
		if !okb {
			return
		}
		ctx := r.Context()
		if _, ok := trialFor(ctx, seller, u.ID); !ok {
			http.Error(w, "پنل تستی برای این حساب در دسترس نیست.", http.StatusForbidden)
			return
		}
		np, err := s.ClaimRepTrial(ctx, u.ID, strings.TrimSpace(u.FirstName), u.Username)
		if err != nil {
			if errors.Is(err, ErrTrialUsed) {
				http.Error(w, "شما قبلاً پنل تستی گرفته‌اید.", http.StatusConflict)
				return
			}
			miniErr(w, err)
			return
		}
		go notifyOwners(context.Background(), fmt.Sprintf("🎁 پنل تستی نمایندگی از مینی‌اپ برای %s ساخته شد (%s).", u.FirstName, np.RepID))
		reply(w, map[string]any{"username": np.Username, "password": np.Password, "address": miniBase, "trial": s.RepTrialStateOf(ctx, np.RepID)}, nil)
	})
	// The owner's trial-panel settings.
	mux.HandleFunc("GET /app/api/m/reptrial", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		if !m.Owner {
			http.Error(w, "فقط مدیر پنل.", http.StatusForbidden)
			return
		}
		reply(w, map[string]any{"policy": s.RepTrialPolicy(r.Context()), "stats": s.RepTrialStats(r.Context())}, nil)
	})
	mux.HandleFunc("POST /app/api/m/reptrial", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		if !m.Owner {
			http.Error(w, "فقط مدیر پنل.", http.StatusForbidden)
			return
		}
		var in RepTrial
		if !read(w, r, &in) {
			return
		}
		if err := s.PutRepTrialPolicy(r.Context(), in); err != nil {
			http.Error(w, "اعتبار ۱ تا ۱۰۰ گیگ، مدت ۰ تا ۶۰ روز و سقف کاربر ۰ تا ۱۰۰۰.", http.StatusBadRequest)
			return
		}
		reply(w, map[string]any{"ok": true}, nil)
	})
	mux.HandleFunc("POST /app/api/panel", func(w http.ResponseWriter, r *http.Request) {
		u, seller, okb := buyer(w, r)
		if !okb {
			return
		}
		var in struct {
			Package string `json:"package"`
			Name    string `json:"name"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if !panelSaleOn(ctx, seller) || !mayBuy(ctx, seller, u.ID) {
			http.Error(w, "فروش پنل نمایندگی فعلاً فعال نیست.", http.StatusForbidden)
			return
		}
		name := strings.TrimSpace(u.FirstName)
		o, err := s.CreateRepOrder(ctx, seller, u.ID, name, "", strings.ToLower(strings.TrimSpace(in.Name)), in.Package)
		if err != nil {
			miniErr(w, err)
			return
		}
		c := s.CardFor(ctx, seller)
		reply(w, map[string]any{"id": o.ID, "gb": o.GB, "price": o.Price, "username": o.RepID,
			"card": map[string]string{"number": c.Number, "holder": c.Holder, "bank": c.Bank}, "bot": s.BotUsernameFor(ctx, seller)}, nil)
	})
	registerAppReceipt(mux, s)
	registerPanelPromo(mux, s, mgr, deny)
	registerPanelLimits(mux, s, mgr, deny)
	registerChatPanels(mux, s, mgr)
	registerMiniAdmin4(mux, s, mgr, deny, read)
	registerMiniAdmin5(mux, s, mgr, deny, read)
}

func hasPkg(list []RepPackage, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}
