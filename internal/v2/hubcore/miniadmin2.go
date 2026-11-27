package hubcore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Management in the Mini App, stage 2 (miniadmin.go is stage 1): the
// reseller's credit and its history with a top-up from the owner's packages,
// sub-sellers (new, credit in and out, on/off, invite link), its own bot,
// payment cards, plans and prices, customer tickets and reports — all of
// what a reseller used to sign in to the web panel for.

// MiniLedger is one line of credit history.
type MiniLedger struct {
	ID     int64  `json:"id"`
	Unix   int64  `json:"unix"`
	Kind   string `json:"kind"`
	Client string `json:"client"`
	Volume int64  `json:"volume"`
	Units  int64  `json:"units"`
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
}

// MiniSeller is one sub-seller (for the owner: one reseller).
type MiniSeller struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Credit  int64  `json:"credit"`
	Units   int64  `json:"units"`
	Clients int    `json:"clients"`
	Used    int64  `json:"used"`
}

// mgrSellers lists who the manager hands credit to: a reseller's
// sub-sellers, or the owner's resellers (never connected shops).
func (s *Store) mgrSellers(ctx context.Context, m miniMgr) ([]MiniSeller, error) {
	var list []Rep
	var err error
	if m.Owner {
		all, e := s.Reps(ctx)
		err = e
		for _, r := range all {
			if _, shop := tenantOfRep(r.ID); r.ParentID == "" && !shop {
				list = append(list, r)
			}
		}
	} else {
		list, err = s.Sellers(ctx, m.Seller)
	}
	out := []MiniSeller{}
	for _, r := range list {
		out = append(out, MiniSeller{ID: r.ID, Name: r.Name, Enabled: r.Enabled, Credit: r.VolumeCredit, Units: r.UnlimitedUnits, Clients: r.Clients, Used: r.UsedHere})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Clients > out[j].Clients })
	return out, err
}

// mgrSellerOf tells whether id is one of the manager's sellers.
func (s *Store) mgrSellerOf(ctx context.Context, m miniMgr, id string) (Rep, bool) {
	all, err := s.Reps(ctx)
	if err != nil {
		return Rep{}, false
	}
	for _, r := range all {
		if r.ID != id {
			continue
		}
		if m.Owner {
			_, shop := tenantOfRep(r.ID)
			return r, r.ParentID == "" && !shop
		}
		return r, r.ParentID != "" && r.ParentID == m.Seller
	}
	return Rep{}, false
}

// MiniDay is one day of the sales report.
type MiniDay struct {
	Day    string `json:"day"` // jalali date
	Sales  int64  `json:"sales"`
	Sold   int64  `json:"sold"`  // bytes of credit sold
	Back   int64  `json:"back"`  // bytes refunded
	Toman  int64  `json:"toman"` // delivered shop orders (card and wallet)
	Orders int64  `json:"orders"`
}

// mgrReport is the manager's sales of the last days, by Tehran day.
func (s *Store) mgrReport(ctx context.Context, m miniMgr, days int) ([]MiniDay, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 7
	}
	today := s.tehranToday()
	from := today - int64(days-1)*86400
	byDay := map[int64]*MiniDay{}
	keys := []int64{}
	for i := 0; i < days; i++ {
		u := from + int64(i)*86400
		k := tehranDayKey(u)
		byDay[k] = &MiniDay{Day: jalaliDate(u)}
		keys = append(keys, k)
	}
	q := `SELECT unix,kind,volume_delta FROM credit_ledger WHERE unix>=?`
	args := []any{from}
	if m.Owner {
		q += ` AND substr(rep_id,1,1)<>'~'`
	} else {
		q += ` AND rep_id=?`
		args = append(args, m.Seller)
	}
	rows, err := s.ro.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u, v int64
		var k string
		if rows.Scan(&u, &k, &v) != nil {
			continue
		}
		d := byDay[tehranDayKey(u)]
		if d == nil {
			continue
		}
		switch k {
		case "charge":
			d.Sales++
			d.Sold += -v
		case "refund":
			d.Back += v
		}
	}
	rows.Close()
	rows, err = s.ro.QueryContext(ctx, `SELECT created_unix,amount FROM orders WHERE status='fulfilled' AND method<>'stars' AND created_unix>=? AND seller=?`, from, m.Seller)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u, amt int64
		if rows.Scan(&u, &amt) != nil {
			continue
		}
		if d := byDay[tehranDayKey(u)]; d != nil {
			d.Toman += amt
			d.Orders++
		}
	}
	rows.Close()
	out := make([]MiniDay, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byDay[k])
	}
	return out, nil
}

// Report downloads: Telegram's WebView cannot send the initData header with
// a download, so the app asks for a one-time link that lives a few minutes.
type xlsxTicket struct {
	rep  string
	days int
	exp  time.Time
}

var miniXLSX sync.Map // token → xlsxTicket

func newXLSXTicket(rep string, days int, now time.Time) string {
	miniXLSX.Range(func(k, v any) bool { // drop old ones
		if now.After(v.(xlsxTicket).exp) {
			miniXLSX.Delete(k)
		}
		return true
	})
	t := randomHex(16)
	miniXLSX.Store(t, xlsxTicket{rep: rep, days: days, exp: now.Add(5 * time.Minute)})
	return t
}

func takeXLSXTicket(t string, now time.Time) (xlsxTicket, bool) {
	v, ok := miniXLSX.LoadAndDelete(t)
	if !ok || now.After(v.(xlsxTicket).exp) {
		return xlsxTicket{}, false
	}
	return v.(xlsxTicket), true
}

// miniFa turns the store's English errors into short Persian ones.
func miniFa(err error) (string, int) {
	msg := err.Error()
	switch {
	case errors.Is(err, ErrFunds):
		return "موجودی کیف پول شما در پنل اصلی برای قیمت عمده کافی نیست؛ اول کیف پول را شارژ کنید.", http.StatusBadRequest
	case strings.Contains(msg, "logo must be"):
		return "لوگو باید عکس PNG یا JPG زیر ۱۵۰ کیلوبایت باشد.", http.StatusBadRequest
	case strings.Contains(msg, "custom plan ranges"):
		return "بازه‌ها را درست کنید (کمترین از بیشترین بزرگ‌تر نباشد).", http.StatusBadRequest
	case strings.Contains(msg, "price per GB"):
		return "قیمت هر گیگ را بنویسید.", http.StatusBadRequest
	case strings.Contains(msg, "code is taken"):
		return "این کد قبلاً گرفته شده؛ کد دیگری بنویسید.", http.StatusBadRequest
	case strings.Contains(msg, "code: 3 to 32"):
		return "کد ۳ تا ۳۲ حرف انگلیسی، عدد، - یا _ باشد.", http.StatusBadRequest
	case strings.Contains(msg, "either a percent"):
		return "یا درصد (۱ تا ۱۰۰) بدهید یا مبلغ تخفیف.", http.StatusBadRequest
	case strings.Contains(msg, "public username"):
		return "کانال باید آیدی عمومی مثل @mychannel باشد.", http.StatusBadRequest
	case strings.Contains(msg, "at most three channels"):
		return "حداکثر سه کانال.", http.StatusBadRequest
	case strings.Contains(msg, "add a channel first"):
		return "اول یک کانال اضافه کنید.", http.StatusBadRequest
	case strings.Contains(msg, "trial is 0-1000"), strings.Contains(msg, "trial needs a volume"):
		return "تست رایگان: حجم (مثلاً ۲۰۰ مگ یا ۱ گیگ) و ۱ تا ۷۲۰ ساعت.", http.StatusBadRequest
	case strings.Contains(msg, "inbounds a trial"):
		return "اینباندی برای تست پیدا نشد؛ از پنل وب انتخاب کنید.", http.StatusBadRequest
	case strings.Contains(msg, "Telegram id looks like"):
		return "آیدی تلگرام باید مثل @myshop باشد.", http.StatusBadRequest
	case strings.Contains(msg, "support link must"):
		return "لینک پشتیبانی باید با https:// شروع شود.", http.StatusBadRequest
	case strings.Contains(msg, "colour must"):
		return "رنگ باید مثل #2de2e6 باشد.", http.StatusBadRequest
	case strings.Contains(msg, "at most 1000 characters"), strings.Contains(msg, "too long"):
		return "متن خیلی بلند است.", http.StatusBadRequest
	case strings.Contains(msg, "below the wholesale price"):
		return "قیمت فروش نمی‌تواند کمتر از قیمت عمده باشد.", http.StatusBadRequest
	case strings.Contains(msg, "already decided"):
		return "این سفارش قبلاً بررسی شده.", http.StatusBadRequest
	case strings.Contains(msg, "amount is out of range"):
		return "مبلغ باید بین ۱۰ هزار تا ۱۰ میلیارد تومان باشد.", http.StatusBadRequest
	case strings.Contains(msg, "has no card set"), strings.Contains(msg, "set the payment card first"):
		return "کارت پرداخت مدیر پنل هنوز تعریف نشده.", http.StatusBadRequest
	case strings.Contains(msg, "panel sale is not allowed"):
		return "فروش پنل برای شما فعال نیست.", http.StatusForbidden
	case strings.Contains(msg, "define a package first"):
		return "اول دست‌کم یک بسته تعریف کنید.", http.StatusBadRequest
	case strings.Contains(msg, "a package is 1 GB"):
		return "هر بسته دست‌کم ۱ گیگ و قیمت داشته باشد.", http.StatusBadRequest
	case strings.Contains(msg, "unlimited client has no volume"):
		return "کاربر نامحدود حجم ندارد که اضافه شود.", http.StatusBadRequest
	case errors.Is(err, ErrCredit), strings.Contains(msg, "insufficient credit"):
		return "اعتبار کافی نیست.", http.StatusBadRequest
	case strings.Contains(msg, "overdue bill"):
		return "یک صورتحساب سررسیدشده پرداخت نشده؛ اول آن را پرداخت کنید.", http.StatusBadRequest
	case strings.Contains(msg, "already exists"), strings.Contains(msg, "is taken"), strings.Contains(msg, "already used by another account"), strings.Contains(msg, "username is taken"):
		return "این نام قبلاً گرفته شده.", http.StatusBadRequest
	case strings.Contains(msg, "the id may only"), strings.Contains(msg, "sign-in name may only"):
		return "نام فقط حروف کوچک انگلیسی، عدد و . _ - باشد.", http.StatusBadRequest
	case strings.Contains(msg, "password needs"):
		return "رمز دست‌کم ۱۰ حرف باشد.", http.StatusBadRequest
	case strings.Contains(msg, "bot token"):
		return "این توکن شبیه توکن ربات از @BotFather نیست.", http.StatusBadRequest
	case strings.Contains(msg, "already used by another reseller"):
		return "این ربات برای نمایندهٔ دیگری ثبت شده.", http.StatusBadRequest
	case strings.Contains(msg, "main bot"):
		return "این توکنِ ربات اصلی پنل است.", http.StatusBadRequest
	case strings.Contains(msg, "managing products is not allowed"):
		return "ساخت پلن برای شما فعال نیست؛ از مدیر پنل بخواهید.", http.StatusForbidden
	case strings.Contains(msg, "sub-sellers"):
		return "زیرمجموعه‌ها این کار را ندارند؛ با نمایندهٔ بالادست هماهنگ کنید.", http.StatusForbidden
	case strings.Contains(msg, "unlimited plans are not allowed"):
		return "فروش نامحدود برای شما فعال نیست.", http.StatusBadRequest
	case strings.Contains(msg, "longest term"):
		return "مدت بیشتر از سقف مجاز شماست.", http.StatusBadRequest
	case strings.Contains(msg, "largest volume"):
		return "حجم بیشتر از سقف مجاز شماست.", http.StatusBadRequest
	case strings.Contains(msg, "at most"):
		return "تعداد بیش از حد مجاز است.", http.StatusBadRequest
	case strings.Contains(msg, "card"):
		return "شمارهٔ کارت معتبر نیست.", http.StatusBadRequest
	case strings.Contains(msg, "unknown package"):
		return "این بسته دیگر موجود نیست.", http.StatusBadRequest
	case errors.Is(err, ErrNotFound):
		return "پیدا نشد.", http.StatusNotFound
	case errors.Is(err, ErrPolicy):
		return "این کار برای شما مجاز نیست.", http.StatusForbidden
	case errors.Is(err, ErrInvalid):
		return "مقدارها درست نیست.", http.StatusBadRequest
	}
	return "الان ممکن نشد؛ کمی بعد دوباره امتحان کنید.", http.StatusServiceUnavailable
}

func miniErr(w http.ResponseWriter, err error) {
	msg, code := miniFa(err)
	http.Error(w, msg, code)
}

func registerMiniAdmin2(mux *http.ServeMux, s *Store, base func(*http.Request) string,
	mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool,
	read func(http.ResponseWriter, *http.Request, any) bool) {
	ok := func(w http.ResponseWriter, err error) {
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]bool{"ok": true}, nil)
	}
	repOnly := func(w http.ResponseWriter, m miniMgr) bool {
		if m.Owner {
			http.Error(w, "این بخش برای نماینده‌هاست.", http.StatusBadRequest)
			return false
		}
		return true
	}

	// ---------- credit: balance, history, top-up ----------
	mux.HandleFunc("GET /app/api/m/wallet", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "credit") || !repOnly(w, m) {
			return
		}
		ctx := r.Context()
		before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
		led, err := s.Ledger(ctx, m.Seller, before, 50)
		if err != nil {
			miniErr(w, err)
			return
		}
		list := make([]MiniLedger, 0, len(led))
		for _, e := range led {
			list = append(list, MiniLedger{ID: e.ID, Unix: e.Unix, Kind: e.Kind, Client: e.ClientEmail, Volume: e.VolumeDelta, Units: e.UnitsDelta, Amount: e.Amount, Note: e.Note})
		}
		credit := s.RepCredit(ctx, m.Seller)
		var units int64
		_ = s.ro.QueryRowContext(ctx, `SELECT unlimited_units FROM reps WHERE id=?`, m.Seller).Scan(&units)
		out := map[string]any{"credit": credit, "units": units, "ledger": list, "forecast": s.RepForecast(ctx, m.Seller, credit),
			"sub_seller": s.IsSubSeller(ctx, m.Seller), "packages": []RepPackage{}}
		if sale := s.RepSale(ctx); sale.Enabled && !s.IsSubSeller(ctx, m.Seller) {
			pk := []map[string]any{}
			for _, p := range sale.Packages {
				pk = append(pk, map[string]any{"id": p.ID, "name": p.Name, "gb": p.GB, "price": p.Price, "units": p.Units})
			}
			out["packages"] = pk
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/m/topup", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "credit") || !repOnly(w, m) {
			return
		}
		var in struct {
			Package string `json:"package"`
			Buyer   string `json:"buyer"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		buyer := firstNonEmpty(strings.TrimSpace(in.Buyer), s.TgName(ctx, m.Chat), m.Seller)
		o, err := s.CreateRepOrder(ctx, "", m.Chat, buyer, m.Seller, "", in.Package)
		if err != nil {
			miniErr(w, err)
			return
		}
		c := s.CardFor(ctx, "")
		reply(w, map[string]any{"id": o.ID, "gb": o.GB, "price": o.Price, "package": o.Package,
			"card": map[string]string{"number": c.Number, "holder": c.Holder, "bank": c.Bank}, "bot": s.BotUsernameFor(ctx, "")}, nil)
	})

	// ---------- sub-sellers (the owner: resellers) ----------
	mux.HandleFunc("GET /app/api/m/sellers", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		ctx := r.Context()
		if !m.Owner && s.IsSubSeller(ctx, m.Seller) {
			http.Error(w, "زیرمجموعه‌ها نمی‌توانند نماینده بسازند.", http.StatusForbidden)
			return
		}
		list, err := s.mgrSellers(ctx, m)
		if err != nil {
			miniErr(w, err)
			return
		}
		out := map[string]any{"sellers": list, "owner": m.Owner}
		if !m.Owner {
			out["credit"] = s.RepCredit(ctx, m.Seller)
			var units int64
			_ = s.ro.QueryRowContext(ctx, `SELECT unlimited_units FROM reps WHERE id=?`, m.Seller).Scan(&units)
			out["units"] = units
			out["requests"] = s.RepRequestsOn(ctx, m.Seller)
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/m/sellers/new", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		var in struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			Password string  `json:"password"`
			CreditGB float64 `json:"credit_gb"`
			Units    int64   `json:"units"`
			Tg       int64   `json:"tg"` // a Telegram id that manages the new panel in the bot
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		in.ID = strings.ToLower(strings.TrimSpace(in.ID))
		pass := strings.TrimSpace(in.Password)
		if pass == "" {
			pass = randomHex(6)
		}
		rc := RepCreate{ID: in.ID, Name: strings.TrimSpace(in.Name), Password: pass, CreditGB: in.CreditGB, Units: in.Units}
		if in.Tg > 0 {
			rc.TgOwners = []int64{in.Tg}
		}
		var err error
		if m.Owner {
			err = s.CreateRep(ctx, m.P.User, rc)
		} else {
			err = s.CreateSeller(ctx, m.P.User, m.Seller, rc)
		}
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]any{"id": in.ID, "username": in.ID, "password": pass}, nil)
	})
	mux.HandleFunc("POST /app/api/m/sellers/credit", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		var in struct {
			ID    string  `json:"id"`
			GB    float64 `json:"gb"` // negative takes credit back
			Units int64   `json:"units"`
			Note  string  `json:"note"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if _, mine := s.mgrSellerOf(ctx, m, in.ID); !mine {
			miniErr(w, ErrNotFound)
			return
		}
		vol := int64(in.GB * (1 << 30))
		if m.Owner {
			ok(w, s.AdjustCredit(ctx, m.P.User, in.ID, vol, in.Units, strings.TrimSpace(in.Note)))
			return
		}
		ok(w, s.TransferCredit(ctx, m.P.User, m.Seller, in.ID, vol, in.Units, strings.TrimSpace(in.Note)))
	})
	mux.HandleFunc("POST /app/api/m/sellers/enable", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") {
			return
		}
		var in struct {
			ID string `json:"id"`
			On bool   `json:"on"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		rp, mine := s.mgrSellerOf(ctx, m, in.ID)
		if !mine {
			miniErr(w, ErrNotFound)
			return
		}
		if m.Owner {
			ok(w, s.PutRep(ctx, Rep{ID: rp.ID, Name: rp.Name, Enabled: in.On}))
			return
		}
		ok(w, s.PutSeller(ctx, m.Seller, Rep{ID: rp.ID, Name: rp.Name, Enabled: in.On}, "", ""))
	})
	mux.HandleFunc("POST /app/api/m/sellers/invite", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !repOnly(w, m) {
			return
		}
		ctx := r.Context()
		code, err := s.NewSellerInvite(ctx, m.Seller)
		u := s.BotUsernameFor(ctx, m.Seller)
		if err == nil && u == "" {
			err = fmt.Errorf("%w: no bot", ErrNotFound)
		}
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]string{"link": "https://t.me/" + u + "?start=rsinv_" + code}, nil)
	})
	mux.HandleFunc("POST /app/api/m/sellers/requests", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "sellers") || !repOnly(w, m) {
			return
		}
		var in struct {
			On bool `json:"on"`
		}
		if !read(w, r, &in) {
			return
		}
		ok(w, s.SetRepRequestsOn(r.Context(), m.Seller, in.On))
	})

	// ---------- the reseller's own bot ----------
	mux.HandleFunc("GET /app/api/m/bot", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "bot") || !repOnly(w, m) {
			return
		}
		b := s.RepBotOf(r.Context(), m.Seller)
		b.Owners = nil
		reply(w, map[string]any{"bot": b, "mirza": s.MirzaMode(r.Context(), m.Seller)}, nil)
	})
	mux.HandleFunc("POST /app/api/m/bot", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "bot") || !repOnly(w, m) {
			return
		}
		var in struct {
			Token   string `json:"token"`
			Enabled bool   `json:"enabled"`
			MiniApp *bool  `json:"miniapp"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if err := s.PutRepBot(ctx, m.Seller, RepBot{Token: in.Token, Enabled: in.Enabled, MiniApp: in.MiniApp}); err != nil {
			miniErr(w, err)
			return
		}
		b := s.RepBotOf(ctx, m.Seller)
		b.Owners = nil
		reply(w, map[string]any{"bot": b}, nil)
	})

	// ---------- payment cards ----------
	mux.HandleFunc("GET /app/api/m/cards", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "cards") {
			return
		}
		cards, err := s.PayCards(r.Context(), m.Seller)
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]any{"cards": cards, "safety": s.CardSafetyFor(r.Context(), m.Seller), "max": MaxPayCards}, nil)
	})
	mux.HandleFunc("POST /app/api/m/cards", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "cards") {
			return
		}
		var in struct {
			Cards  []PayCard   `json:"cards"`
			Safety *CardSafety `json:"safety"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		if in.Cards != nil {
			if err := s.PutPayCards(ctx, m.Seller, in.Cards); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Safety != nil {
			if err := s.PutCardSafety(ctx, m.Seller, *in.Safety); err != nil {
				miniErr(w, err)
				return
			}
		}
		ok(w, nil)
	})

	// ---------- plans and prices ----------
	mux.HandleFunc("GET /app/api/m/plans", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "plans") {
			return
		}
		ctx := r.Context()
		prods, err := s.Products(ctx, m.P, false)
		if err != nil {
			miniErr(w, err)
			return
		}
		list := []Product{}
		for _, p := range prods {
			if p.Seller == m.Seller && p.Status != "archived" {
				list = append(list, p)
			}
		}
		out := map[string]any{"plans": list, "edit": s.canManageProducts(ctx, m.P) == nil}
		if !m.Owner {
			pol, _ := s.RepPolicyOf(ctx, m.Seller)
			out["policy"] = map[string]any{"allow_unlimited": pol.AllowUnlimited, "max_days": pol.MaxDays, "max_client_gb": pol.MaxClientGB}
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/m/plan", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "plans") {
			return
		}
		var p Product
		if !read(w, r, &p) {
			return
		}
		ctx := r.Context()
		if p.ID == "" {
			p.ID = "m" + randomHex(4)
		} else {
			var seller string
			if s.ro.QueryRowContext(ctx, `SELECT seller FROM products WHERE id=?`, p.ID).Scan(&seller) != nil || seller != m.Seller {
				miniErr(w, ErrNotFound)
				return
			}
			// Fields the app does not show keep their values.
			if prods, err := s.Products(ctx, m.P, false); err == nil {
				for _, cur := range prods {
					if cur.ID == p.ID {
						p.Inbounds, p.PriceStars, p.Category, p.PerBuyer, p.Sort = cur.Inbounds, cur.PriceStars, cur.Category, cur.PerBuyer, cur.Sort
					}
				}
			}
		}
		p.Seller = m.Seller
		p.Name = strings.TrimSpace(p.Name)
		if p.Status == "" {
			p.Status = "published"
		}
		if err := s.PutProduct(ctx, m.P, p); err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]string{"id": p.ID}, nil)
	})
	mux.HandleFunc("POST /app/api/m/plan/delete", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "plans") {
			return
		}
		var in struct {
			ID string `json:"id"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		var seller string
		if s.ro.QueryRowContext(ctx, `SELECT seller FROM products WHERE id=?`, in.ID).Scan(&seller) != nil || seller != m.Seller {
			miniErr(w, ErrNotFound)
			return
		}
		_, err := s.DeleteProduct(ctx, m.P, in.ID)
		ok(w, err)
	})

	// ---------- customer tickets ----------
	mux.HandleFunc("GET /app/api/m/tickets", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "tickets") {
			return
		}
		st := r.URL.Query().Get("s")
		if st != "closed" {
			st = "active"
		}
		all, err := s.Tickets(r.Context(), m.P, st, 200)
		if err != nil {
			miniErr(w, err)
			return
		}
		list := []Ticket{}
		for _, t := range all {
			if t.Seller == m.Seller {
				list = append(list, t)
			}
		}
		reply(w, list, nil)
	})
	mux.HandleFunc("GET /app/api/m/ticket", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "tickets") {
			return
		}
		id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		t, err := s.TicketByID(r.Context(), m.P, id)
		if err == nil && t.Seller != m.Seller {
			err = ErrNotFound
		}
		if err != nil {
			miniErr(w, err)
			return
		}
		reply(w, t, nil)
	})
	mux.HandleFunc("POST /app/api/m/ticket", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "tickets") {
			return
		}
		var in struct {
			ID    int64  `json:"id"`
			Text  string `json:"text"`
			Close bool   `json:"close"`
		}
		if !read(w, r, &in) {
			return
		}
		ctx := r.Context()
		t, err := s.TicketByID(ctx, m.P, in.ID)
		if err == nil && t.Seller != m.Seller {
			err = ErrNotFound
		}
		if err != nil {
			miniErr(w, err)
			return
		}
		if txt := strings.TrimSpace(in.Text); txt != "" {
			author := firstNonEmpty(s.TgName(ctx, m.Chat), m.P.User)
			if _, err := s.TicketReply(ctx, m.P, in.ID, author, txt, false); err != nil {
				miniErr(w, err)
				return
			}
		}
		if in.Close {
			if _, err := s.TicketClose(ctx, m.P, in.ID, true); err != nil {
				miniErr(w, err)
				return
			}
		}
		ok(w, nil)
	})

	// ---------- reports ----------
	mux.HandleFunc("GET /app/api/m/report", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "reports") {
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.mgrReport(r.Context(), m, days)
		if err != nil {
			miniErr(w, err)
			return
		}
		var sales, sold, back, toman, orders int64
		for _, d := range list {
			sales, sold, back, toman, orders = sales+d.Sales, sold+d.Sold, back+d.Back, toman+d.Toman, orders+d.Orders
		}
		reply(w, map[string]any{"days": list, "total": map[string]int64{"sales": sales, "sold": sold, "back": back, "toman": toman, "orders": orders}}, nil)
	})
	mux.HandleFunc("POST /app/api/m/report.link", func(w http.ResponseWriter, r *http.Request) {
		m, okm := mgr(w, r)
		if !okm || deny(w, m, "reports") {
			return
		}
		var in struct {
			Days int `json:"days"`
		}
		if !read(w, r, &in) {
			return
		}
		if in.Days != 7 && in.Days != 90 {
			in.Days = 30
		}
		t := newXLSXTicket(m.Seller, in.Days, s.now())
		reply(w, map[string]string{"url": strings.TrimRight(base(r), "/") + "/app/api/m/report.xlsx?t=" + t,
			"name": fmt.Sprintf("report-%dd-%s.xlsx", in.Days, s.now().Format("20060102"))}, nil)
	})
	mux.HandleFunc("GET /app/api/m/report.xlsx", func(w http.ResponseWriter, r *http.Request) {
		tk, good := takeXLSXTicket(r.URL.Query().Get("t"), s.now())
		if !good {
			http.Error(w, "این لینک منقضی شده؛ دوباره از مینی‌اپ بگیرید.", http.StatusGone)
			return
		}
		sheets, err := s.ReportWorkbook(r.Context(), tk.rep, tk.days)
		if err != nil {
			miniErr(w, err)
			return
		}
		name := "dark-report"
		if tk.rep != "" {
			name += "-" + tk.rep
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%dd-%s.xlsx"`, name, tk.days, s.now().Format("20060102")))
		_ = WriteXLSX(w, sheets)
	})
}
