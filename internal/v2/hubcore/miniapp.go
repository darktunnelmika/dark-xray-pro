package hubcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Telegram Mini App is the customer store inside the bot: services
// with usage and links, the catalog, orders, and linking an existing
// subscription. It is served next to /sub (the public address) under
// /app/. Every API call carries Telegram's signed initData, checked
// against the bot token, so the chat id is trusted without any login.

//go:embed miniapp
var miniappFS embed.FS

// The store's fonts are WOFF; nosniff needs the right type.
func init() { _ = mime.AddExtensionType(".woff", "font/woff") }

var tgToken, miniBase string

// invoiceLinker makes Telegram Stars invoice links (set by the bot).
var invoiceLinker func(ctx context.Context, title, desc, payload string, stars int64) (string, error)

// SetInvoiceLinker lets the Mini App sell for Stars.
func SetInvoiceLinker(f func(ctx context.Context, title, desc, payload string, stars int64) (string, error)) {
	invoiceLinker = f
}

// SetPublicBase is the public subscription address (links in the Mini App).
func SetPublicBase(b string) { miniBase = strings.TrimRight(b, "/") }

// SetTelegramToken enables the Mini App API (the bot token signs initData).
func SetTelegramToken(tok string) { tgToken = strings.TrimSpace(tok) }

// TgUser is the Telegram user a verified initData names.
type TgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
	Lang      string `json:"language_code"`
}

var errInitData = errors.New("telegram init data rejected")

// VerifyInitData checks Telegram WebApp initData (HMAC-SHA256 with the
// key HMAC("WebAppData", bot token)) and its age.
func VerifyInitData(initData, botToken string, now time.Time, maxAge time.Duration) (TgUser, error) {
	if botToken == "" || initData == "" {
		return TgUser{}, errInitData
	}
	q, err := url.ParseQuery(initData)
	if err != nil {
		return TgUser{}, errInitData
	}
	hash := q.Get("hash")
	q.Del("hash")
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+q.Get(k))
	}
	km := hmac.New(sha256.New, []byte("WebAppData"))
	km.Write([]byte(botToken))
	m := hmac.New(sha256.New, km.Sum(nil))
	m.Write([]byte(strings.Join(lines, "\n")))
	want := hex.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(strings.ToLower(hash))) {
		return TgUser{}, errInitData
	}
	at, _ := strconv.ParseInt(q.Get("auth_date"), 10, 64)
	if at == 0 || now.Sub(time.Unix(at, 0)) > maxAge {
		return TgUser{}, fmt.Errorf("%w: expired — reopen the app from the bot", errInitData)
	}
	var u TgUser
	if json.Unmarshal([]byte(q.Get("user")), &u) != nil || u.ID == 0 {
		return TgUser{}, errInitData
	}
	return u, nil
}

// MiniAppService is one of the customer's services.
type MiniAppService struct {
	Email     string  `json:"email"`
	Enabled   bool    `json:"enabled"`
	Status    string  `json:"status"` // active | ended | expired | disabled
	Used      int64   `json:"used"`
	Total     int64   `json:"total"` // -1 = unlimited
	Until     int64   `json:"until"` // 0 = no expiry yet
	DaysOnUse int64   `json:"days_on_use"`
	Online    bool    `json:"online"`
	SubURL    string  `json:"sub_url"`
	Days      []int64 `json:"days"` // last 7 days of traffic, oldest first
}

type miniOrder struct {
	ID      int64  `json:"id"`
	Product string `json:"product"`
	Amount  int64  `json:"amount"`
	Status  string `json:"status"`
	Renew   string `json:"renew"`
	Created int64  `json:"created"`
}

func (s *Store) miniServices(ctx context.Context, chat int64, subBase, seller string) ([]MiniAppService, error) {
	emails, err := s.TgClients(ctx, chat)
	if seller != "" {
		emails, err = s.TgClientsOf(ctx, chat, seller)
	}
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	out := []MiniAppService{}
	for _, e := range emails {
		c, err := s.ClientByEmail(ctx, e)
		if err != nil {
			continue
		}
		sv := MiniAppService{Email: c.Email, Enabled: c.Enabled, Used: c.Usage.Up + c.Usage.Down, Total: c.VolumeBytes,
			Until: c.EffectiveUntil, Online: c.Online, SubURL: strings.TrimRight(s.SubBaseFor(ctx, c.Email, subBase), "/") + "/sub/" + c.SubToken, Status: "active"}
		if c.EffectiveUntil == 0 {
			sv.DaysOnUse = c.DurationSeconds / 86400
		}
		if d, err := s.ClientDailyUsage(ctx, c.Email, 7); err == nil {
			for _, x := range d {
				sv.Days = append(sv.Days, x.Up+x.Down)
			}
		}
		switch {
		case !c.Enabled:
			sv.Status = "disabled"
		case c.Remaining == 0:
			sv.Status = "ended"
		case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
			sv.Status = "expired"
		}
		out = append(out, sv)
	}
	return out, nil
}

func (s *Store) miniOrders(ctx context.Context, chat int64) ([]miniOrder, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT o.id,COALESCE(p.name,o.product_id),o.amount,o.status,CASE WHEN o.renew=1 THEN o.client_email ELSE '' END,o.created_unix
FROM orders o LEFT JOIN products p ON p.id=o.product_id WHERE o.buyer=? ORDER BY o.id DESC LIMIT 30`, TgBuyer(chat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []miniOrder{}
	for rows.Next() {
		var o miniOrder
		if err := rows.Scan(&o.ID, &o.Product, &o.Amount, &o.Status, &o.Renew, &o.Created); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// MiniAppURL is where the bot opens the store ("" = off): the owner's
// address, or <sub base>/app/ when the public address is HTTPS.
func (s *Store) MiniAppURL(ctx context.Context, subBase string) string {
	if s.Setting(ctx, "miniapp") != "1" {
		return ""
	}
	if u := s.Setting(ctx, "miniapp_url"); u != "" {
		return u
	}
	if strings.HasPrefix(subBase, "https://") {
		return strings.TrimRight(subBase, "/") + "/app/"
	}
	return ""
}

// registerMiniApp adds /app/ to the public mux.
func registerMiniApp(mux *http.ServeMux, s *Store) {
	base := func(r *http.Request) string {
		if miniBase != "" {
			return miniBase
		}
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		return scheme + "://" + r.Host
	}
	static, _ := fs.Sub(miniappFS, "miniapp")
	files := http.FileServer(http.FS(static))
	mux.Handle("GET /app/", http.StripPrefix("/app/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})))
	// sellerOf is filled per request by user(): "" for the main bot, a
	// reseller id when the Mini App was opened from that reseller's bot
	// (Telegram signs the data with the bot's own token).
	user := func(w http.ResponseWriter, r *http.Request) (TgUser, bool) {
		u, seller, err := s.verifyMiniUser(r.Context(), r.Header.Get("X-Init-Data"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return TgUser{}, false
		}
		if !s.MiniAppOn(r.Context(), seller) {
			http.Error(w, "the store is closed", http.StatusServiceUnavailable)
			return TgUser{}, false
		}
		_ = s.TgTouch(r.Context(), u.ID, u.FirstName, u.Username)
		*r = *r.WithContext(context.WithValue(r.Context(), miniSellerKey{}, seller))
		return u, true
	}
	sellerOf := func(r *http.Request) string { v, _ := r.Context().Value(miniSellerKey{}).(string); return v }
	mine := func(r *http.Request, chat int64) ([]string, error) {
		if sl := sellerOf(r); sl != "" {
			return s.TgClientsOf(r.Context(), chat, sl)
		}
		return s.TgClients(r.Context(), chat)
	}
	owner := Principal{User: "miniapp", Role: "owner"}
	mux.HandleFunc("GET /app/api/me", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		seller := sellerOf(r)
		services, err := s.miniServices(r.Context(), u.ID, base(r), seller)
		if err != nil {
			reply(w, nil, err)
			return
		}
		orders, err := s.miniOrders(r.Context(), u.ID)
		if err != nil {
			reply(w, nil, err)
			return
		}
		all, err := s.Products(r.Context(), owner, true)
		if err != nil {
			reply(w, nil, err)
			return
		}
		products := []Product{}
		for _, p := range all {
			if p.Seller == seller {
				p.Inbounds = nil // internal
				products = append(products, p)
			}
		}
		ss := s.SubSettings(r.Context())
		out := map[string]any{"user": u, "services": services, "orders": orders, "products": products,
			"pay_text": s.Setting(r.Context(), "tg_pay_text"), "support": ss.SupportURL, "title": ss.Title, "announce": ss.Announce,
			"stars": invoiceLinker != nil && seller == "", "wallet": 0, "bot": s.Setting(r.Context(), "tg_username"),
			"toman": s.TomanBalance(r.Context(), seller, u.ID), "accent": ss.Accent, "logo": ss.Logo}
		if seller == "" {
			out["wallet"] = s.WalletBalance(r.Context(), u.ID)
		} else {
			// The reseller's own brand, bot and support; Stars, trials and
			// referrals belong to the main store.
			b := s.RepBrand(r.Context(), seller)
			out["title"] = firstNonEmpty(b.Title, ss.Title)
			out["announce"], out["support"], out["accent"], out["logo"] = b.Announce, b.SupportURL, b.Accent, b.Logo
			out["bot"] = s.RepBotOf(r.Context(), seller).Username
			out["pay_text"] = ""
		}
		if c := s.CardFor(r.Context(), seller); c.Number != "" {
			out["card"] = c
		}
		if tp := s.TrialPolicy(r.Context()); seller == "" && tp.Enabled && !s.TrialUsed(r.Context(), u.ID) {
			out["trial"] = map[string]int64{"gb": tp.GB, "hours": tp.Hours}
		}
		if rp := s.ReferralPolicy(r.Context()); seller == "" && rp.Enabled {
			if sum, err := s.ReferralSummary(r.Context(), u.ID); err == nil {
				out["referral"] = map[string]any{"code": sum.Code, "invited": sum.Invited, "earned": sum.Earned, "percent": rp.Percent, "fixed": rp.Fixed, "first_only": rp.FirstOnly}
			}
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/order", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		var in struct {
			Product string `json:"product"`
			Renew   string `json:"renew"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Renew != "" {
			list, _ := mine(r, u.ID)
			found := false
			for _, e := range list {
				found = found || e == in.Renew
			}
			if !found {
				http.Error(w, "not your service", http.StatusForbidden)
				return
			}
		}
		// One open order per product at a time keeps double taps harmless.
		var open int64
		_ = s.ro.QueryRowContext(r.Context(), `SELECT id FROM orders WHERE buyer=? AND product_id=? AND client_email=? AND status='pending' ORDER BY id DESC LIMIT 1`,
			TgBuyer(u.ID), in.Product, in.Renew).Scan(&open)
		if open > 0 {
			reply(w, map[string]any{"id": open, "existing": true}, nil)
			return
		}
		if !s.productOf(r.Context(), in.Product, sellerOf(r)) {
			http.Error(w, "this plan is not sold here", http.StatusForbidden)
			return
		}
		id, err := s.CreateOrder(r.Context(), owner, in.Product, TgBuyer(u.ID), in.Renew, "mini app")
		reply(w, map[string]any{"id": id}, err)
	})
	mux.HandleFunc("POST /app/api/pay", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		var in struct {
			Product string `json:"product"`
			Renew   string `json:"renew"`
			Method  string `json:"method"` // stars | wallet
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Renew != "" {
			list, _ := mine(r, u.ID)
			found := false
			for _, e := range list {
				found = found || e == in.Renew
			}
			if !found {
				http.Error(w, "not your service", http.StatusForbidden)
				return
			}
		}
		if !s.productOf(r.Context(), in.Product, sellerOf(r)) {
			http.Error(w, "this plan is not sold here", http.StatusForbidden)
			return
		}
		if sellerOf(r) != "" && in.Method != "toman" {
			http.Error(w, "this store takes wallet (toman) or card payments", http.StatusBadRequest)
			return
		}
		if in.Method == "wallet" {
			email, left, err := s.WalletPay(r.Context(), u.ID, in.Product, in.Renew)
			reply(w, map[string]any{"email": email, "wallet": left}, err)
			return
		}
		if in.Method == "toman" {
			email, left, err := s.TomanPay(r.Context(), u.ID, in.Product, in.Renew)
			reply(w, map[string]any{"email": email, "toman": left}, err)
			return
		}
		if invoiceLinker == nil {
			http.Error(w, "Stars payments are not available", http.StatusServiceUnavailable)
			return
		}
		id, stars, title, err := s.StarsCheckout(r.Context(), u.ID, in.Product, in.Renew)
		if err != nil {
			reply(w, nil, err)
			return
		}
		link, err := invoiceLinker(r.Context(), title, "Dark VPN", "order:"+strconv.FormatInt(id, 10), stars)
		reply(w, map[string]any{"invoice": link, "order": id}, err)
	})
	// A toman top-up: the customer then sends the receipt photo to the bot.
	mux.HandleFunc("POST /app/api/topup", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		var in struct {
			Amount int64 `json:"amount"`
		}
		if !decode(w, r, &in) {
			return
		}
		if s.CardFor(r.Context(), sellerOf(r)).Number == "" {
			http.Error(w, "card payment is not set up", http.StatusServiceUnavailable)
			return
		}
		id, err := s.CreateTopup(r.Context(), sellerOf(r), u.ID, in.Amount)
		reply(w, map[string]any{"id": id}, err)
	})
	mux.HandleFunc("POST /app/api/trial", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		if sellerOf(r) != "" {
			http.Error(w, "no free trial here", http.StatusForbidden)
			return
		}
		email, err := s.ClaimTrial(r.Context(), u.ID)
		if errors.Is(err, ErrTrialUsed) {
			http.Error(w, "the free trial was already used", http.StatusConflict)
			return
		}
		reply(w, map[string]string{"email": email}, err)
	})
	mux.HandleFunc("POST /app/api/cancel/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var buyer, status string
		if s.ro.QueryRowContext(r.Context(), `SELECT buyer,status FROM orders WHERE id=?`, id).Scan(&buyer, &status) != nil || buyer != TgBuyer(u.ID) {
			http.NotFound(w, r)
			return
		}
		err := s.CancelOrder(r.Context(), owner, id)
		reply(w, map[string]bool{"ok": err == nil}, err)
	})
	mux.HandleFunc("POST /app/api/link", func(w http.ResponseWriter, r *http.Request) {
		u, ok := user(w, r)
		if !ok {
			return
		}
		var in struct {
			Link string `json:"link"`
		}
		if !decode(w, r, &in) {
			return
		}
		email, err := s.TgLinkByToken(r.Context(), u.ID, in.Link)
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "this link was not found", http.StatusNotFound)
			return
		}
		reply(w, map[string]string{"email": email}, err)
	})
}

type miniSellerKey struct{}

// verifyMiniUser checks Telegram's signature with the main bot's token,
// then with each reseller bot's; the matching bot tells whose store it is.
func (s *Store) verifyMiniUser(ctx context.Context, initData string) (TgUser, string, error) {
	u, err := VerifyInitData(initData, tgToken, s.now(), 24*time.Hour)
	if err == nil {
		return u, "", nil
	}
	for _, b := range s.RepBotsAll(ctx) {
		if ru, rerr := VerifyInitData(initData, b.Token, s.now(), 24*time.Hour); rerr == nil {
			return ru, b.Rep, nil
		}
	}
	return TgUser{}, "", err
}

// MiniAppOn: the store is open for the main bot when the owner turned it
// on; for a reseller's bot also unless the reseller turned it off.
func (s *Store) MiniAppOn(ctx context.Context, seller string) bool {
	if s.Setting(ctx, "miniapp") != "1" {
		return false
	}
	return seller == "" || s.Setting(ctx, "rep_miniapp:"+seller) != "0"
}

// RepMiniAppURL is the Mini App address for a reseller's bot ("" = off).
func (s *Store) RepMiniAppURL(ctx context.Context, seller, subBase string) string {
	if !s.MiniAppOn(ctx, seller) {
		return ""
	}
	return s.MiniAppURL(ctx, subBase)
}

func (s *Store) productOf(ctx context.Context, id, seller string) bool {
	var got string
	return s.ro.QueryRowContext(ctx, `SELECT seller FROM products WHERE id=?`, id).Scan(&got) == nil && got == seller
}
