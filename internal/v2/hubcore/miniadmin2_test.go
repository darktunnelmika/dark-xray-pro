package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniAdminStage2(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const mainTok = "111:main-bot-token-main-bot-token-000"
	const aliTok = "222333:ali-bot-token-ali-bot-token-abcdef"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	_ = s.SetSetting(ctx, "tg_owners", "500")
	_ = s.SetSetting(ctx, "tg_username", "mainbot")
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{600}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: aliTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for chat, role := range map[int64]string{700: "sales", 800: "support"} {
		code, _ := s.NewTgAdminInvite(ctx, "ali", role)
		if _, _, err := s.ClaimTgAdminInvite(ctx, code, chat, "x"); err != nil {
			t.Fatal(err)
		}
	}
	h := SubHandler(s)
	raw := func(tok, chat, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(b))
		if tok != "" {
			req.Header.Set("X-Init-Data", signInitData(tok, `{"id":`+chat+`,"first_name":"A"}`, *now))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	call := func(tok, chat, method, path string, body any) (int, map[string]any) {
		t.Helper()
		rec := raw(tok, chat, method, path, body)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	list := func(tok, chat, path string) (int, []any) {
		t.Helper()
		rec := raw(tok, chat, "GET", path, nil)
		var out []any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// The dashboard says what the role may open.
	_, home := call(aliTok, "600", "GET", "/app/api/m/home", nil)
	if c := home["can"].(map[string]any); c["sellers"] != true || c["bot"] != true || c["plans"] != true || c["tickets"] != true {
		t.Fatalf("manager can %v", c)
	}
	_, home = call(aliTok, "800", "GET", "/app/api/m/home", nil)
	if c := home["can"].(map[string]any); c["sellers"] != false || c["tickets"] != true || c["cards"] != false {
		t.Fatalf("support can %v", c)
	}

	// Credit and its history.
	code, w := call(aliTok, "600", "GET", "/app/api/m/wallet", nil)
	if code != 200 || w["credit"].(float64) != float64(100<<30) || len(w["packages"].([]any)) != 0 {
		t.Fatalf("wallet %d %v", code, w)
	}
	// A top-up from the owner's packages: an order that waits for its receipt.
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, Packages: []RepPackage{{ID: "p1", Name: "500", GB: 500, Price: 1000000}}}); err != nil {
		t.Fatal(err)
	}
	_, w = call(aliTok, "600", "GET", "/app/api/m/wallet", nil)
	if len(w["packages"].([]any)) != 1 {
		t.Fatalf("packages %v", w)
	}
	code, r := call(aliTok, "600", "POST", "/app/api/m/topup", map[string]any{"package": "p1"})
	if code != 200 || r["bot"] != "mainbot" || r["card"].(map[string]any)["number"] != "6037991234567890" {
		t.Fatalf("topup %d %v", code, r)
	}
	if o, err := s.RepOrderByID(ctx, int64(r["id"].(float64))); err != nil || o.Kind != "topup" || o.RepID != "ali" || o.Status != "await_receipt" || o.Chat != 600 {
		t.Fatalf("rep order %+v %v", o, err)
	}
	if code, _ := call(aliTok, "800", "GET", "/app/api/m/wallet", nil); code != http.StatusForbidden {
		t.Fatal("support saw the wallet")
	}

	// Sub-sellers: new with credit from mine, more credit, take back, off.
	if code, _ := call(aliTok, "700", "GET", "/app/api/m/sellers", nil); code != http.StatusForbidden {
		t.Fatal("sales opened sellers")
	}
	code, r = call(aliTok, "600", "POST", "/app/api/m/sellers/new", map[string]any{"id": "ali-sub", "name": "Sub", "credit_gb": 10, "tg": 901})
	if code != 200 || len(r["password"].(string)) < 10 {
		t.Fatalf("new seller %d %v", code, r)
	}
	if s.RepCredit(ctx, "ali") != 90<<30 || s.RepCredit(ctx, "ali-sub") != 10<<30 {
		t.Fatal("first credit")
	}
	if rec := raw(aliTok, "600", "POST", "/app/api/m/sellers/new", map[string]any{"id": "ali-sub"}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "گرفته شده") {
		t.Fatalf("taken %d %s", rec.Code, rec.Body.String())
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/sellers/credit", map[string]any{"id": "ali-sub", "gb": 5}); code != 200 || s.RepCredit(ctx, "ali-sub") != 15<<30 {
		t.Fatal("give")
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/sellers/credit", map[string]any{"id": "ali-sub", "gb": -15}); code != 200 || s.RepCredit(ctx, "ali-sub") != 0 || s.RepCredit(ctx, "ali") != 100<<30 {
		t.Fatal("take back")
	}
	if rec := raw(aliTok, "600", "POST", "/app/api/m/sellers/credit", map[string]any{"id": "ali-sub", "gb": 1000}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "اعتبار کافی نیست") {
		t.Fatalf("too much %d %s", rec.Code, rec.Body.String())
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/sellers/credit", map[string]any{"id": "nope", "gb": 1}); code != 404 {
		t.Fatal("stranger seller")
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/sellers/enable", map[string]any{"id": "ali-sub", "on": false}); code != 200 {
		t.Fatal("off")
	}
	code, r = call(aliTok, "600", "GET", "/app/api/m/sellers", nil)
	if sl := r["sellers"].([]any); code != 200 || len(sl) != 1 || sl[0].(map[string]any)["enabled"] != false {
		t.Fatalf("sellers %d %v", code, r)
	}
	if code, r := call(aliTok, "600", "POST", "/app/api/m/sellers/invite", nil); code != 200 || !strings.Contains(r["link"].(string), "?start=rsinv_") {
		t.Fatalf("invite %d %v", code, r)
	}
	// The sub-seller manages its panel from the main bot, but has no sellers.
	if code, _ := call(mainTok, "901", "GET", "/app/api/m/sellers", nil); code != http.StatusForbidden {
		t.Fatalf("sub-seller sellers %d", code)
	}

	// Own bot: status without the token; a wrong token is refused in Persian.
	code, r = call(aliTok, "600", "GET", "/app/api/m/bot", nil)
	if b := r["bot"].(map[string]any); code != 200 || b["has_token"] != true || b["token"] != nil {
		t.Fatalf("bot %d %v", code, r)
	}
	if rec := raw(aliTok, "600", "POST", "/app/api/m/bot", map[string]any{"token": "nope", "enabled": true}); rec.Code != 400 || !strings.Contains(rec.Body.String(), "BotFather") {
		t.Fatalf("bad token %d %s", rec.Code, rec.Body.String())
	}

	// Cards and the safe-card options.
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/cards", map[string]any{"cards": []map[string]any{{"number": "6219 8610 0000 0001", "holder": "Ali", "enabled": true}}, "safety": map[string]any{"ask_last4": true}}); code != 200 {
		t.Fatal("cards")
	}
	_, r = call(aliTok, "600", "GET", "/app/api/m/cards", nil)
	if cs := r["cards"].([]any); len(cs) != 1 || cs[0].(map[string]any)["number"] != "6219861000000001" || r["safety"].(map[string]any)["ask_last4"] != true {
		t.Fatalf("cards %v", r)
	}

	// Plans: new, edit, delete.
	code, r = call(aliTok, "600", "POST", "/app/api/m/plan", map[string]any{"name": "30G", "plan": "volume", "gb": 30, "days": 30, "price": 150000})
	if code != 200 || r["id"] == "" {
		t.Fatalf("plan %d %v", code, r)
	}
	pid := r["id"].(string)
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/plan", map[string]any{"id": pid, "name": "30G+", "plan": "volume", "gb": 35, "days": 30, "price": 160000}); code != 200 {
		t.Fatal("edit plan")
	}
	_, r = call(aliTok, "600", "GET", "/app/api/m/plans", nil)
	if ps := r["plans"].([]any); r["edit"] != true || len(ps) != 1 || ps[0].(map[string]any)["gb"].(float64) != 35 || ps[0].(map[string]any)["status"] != "published" {
		t.Fatalf("plans %v", r)
	}
	if code, _ := call(mainTok, "500", "POST", "/app/api/m/plan/delete", map[string]any{"id": pid}); code != 404 {
		t.Fatal("the owner deleted a reseller's plan from the app")
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/plan/delete", map[string]any{"id": pid}); code != 200 {
		t.Fatal("delete plan")
	}

	// Tickets: support answers and closes.
	tid, _, err := s.CustomerTicket(ctx, "ali", 900, "Cust", "it does not connect")
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = s.CustomerTicket(ctx, "", 902, "X", "the owner's customer") // not ali's
	_, home = call(aliTok, "800", "GET", "/app/api/m/home", nil)
	if home["tickets"].(float64) != 1 {
		t.Fatalf("open tickets %v", home["tickets"])
	}
	if code, ts := list(aliTok, "800", "/app/api/m/tickets"); code != 200 || len(ts) != 1 {
		t.Fatalf("tickets %d %v", code, ts)
	}
	if code, _ := call(aliTok, "800", "POST", "/app/api/m/ticket", map[string]any{"id": tid, "text": "try again", "close": true}); code != 200 {
		t.Fatal("reply")
	}
	if tk, _ := s.TicketByID(ctx, Principal{Role: "owner"}, tid); len(tk.Msgs) < 2 || tk.Status != "closed" {
		t.Fatalf("ticket %+v", tk)
	}

	// Reports and the one-time Excel link.
	if code, r := call(aliTok, "600", "GET", "/app/api/m/report?days=7", nil); code != 200 || len(r["days"].([]any)) != 7 {
		t.Fatalf("report %d %v", code, r)
	}
	code, r = call(aliTok, "600", "POST", "/app/api/m/report.link", map[string]any{"days": 30})
	if code != 200 {
		t.Fatal("link")
	}
	u := strings.TrimPrefix(r["url"].(string), "https://hub.example")
	if rec := raw("", "", "GET", u, nil); rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("xlsx %d", rec.Code)
	}
	if rec := raw("", "", "GET", u, nil); rec.Code != http.StatusGone {
		t.Fatal("the link works twice")
	}

	// The owner: resellers, with credit from nowhere.
	code, r = call(mainTok, "500", "GET", "/app/api/m/sellers", nil)
	if code != 200 || len(r["sellers"].([]any)) != 1 {
		t.Fatalf("owner sellers %d %v", code, r)
	}
	if code, _ := call(mainTok, "500", "POST", "/app/api/m/sellers/credit", map[string]any{"id": "ali", "gb": 10}); code != 200 || s.RepCredit(ctx, "ali") != 110<<30 {
		t.Fatal("owner credit")
	}
	if code, _ := call(mainTok, "500", "POST", "/app/api/m/sellers/credit", map[string]any{"id": "ali-sub", "gb": 10}); code != 404 {
		t.Fatal("the owner reached a sub-seller directly")
	}
	if code, _ := call(mainTok, "500", "GET", "/app/api/m/wallet", nil); code != 400 {
		t.Fatal("owner wallet")
	}
}
