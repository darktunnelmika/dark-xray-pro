package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestPanelPromo(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const mainTok = "111:main-bot-token-main-bot-token-000"
	const aliTok = "222333:ali-bot-token-ali-bot-token-abcdef"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	_ = s.SetSetting(ctx, "tg_owners", "500")
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{600}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: aliTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, WholesaleTB: 1_024_000, Packages: []RepPackage{{ID: "p1", Name: "100", GB: 100, Price: 300000}}}); err != nil {
		t.Fatal(err)
	}
	h := SubHandler(s)
	call := func(tok, chat, method, path string, body any) (int, map[string]any, string) {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(b))
		req.Header.Set("X-Init-Data", signInitData(tok, `{"id":`+chat+`,"first_name":"Buyer"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out, rec.Body.String()
	}
	pkg0 := func(r map[string]any) map[string]any { return r["packages"].([]any)[0].(map[string]any) }

	// No banner: full price, no promo.
	_, r, _ := call(mainTok, "900", "GET", "/app/api/panels", nil)
	if r["on"] != true || r["promo"] != nil || pkg0(r)["price"].(float64) != 300000 {
		t.Fatalf("panels %v", r)
	}
	// Only managers set it; bad values are refused.
	if code, _, _ := call(mainTok, "900", "POST", "/app/api/m/panelpromo", map[string]any{"on": true, "percent": 20}); code == 200 {
		t.Fatal("a customer set the banner")
	}
	if code, _, _ := call(mainTok, "500", "POST", "/app/api/m/panelpromo", map[string]any{"on": true, "percent": 95}); code != 400 {
		t.Fatal("95%")
	}
	if code, _, _ := call(mainTok, "500", "POST", "/app/api/m/panelpromo", map[string]any{"on": true, "color": "pink"}); code != 400 {
		t.Fatal("colour")
	}
	if code, _, body := call(mainTok, "500", "POST", "/app/api/m/panelpromo", map[string]any{"on": true, "title": "جشنواره", "badge": "فقط امروز", "color": "gold", "percent": 20, "until": now.Unix() + 3600}); code != 200 {
		t.Fatalf("set %d %s", code, body)
	}
	_, r, _ = call(mainTok, "900", "GET", "/app/api/panels", nil)
	if p := r["promo"].(map[string]any); p["title"] != "جشنواره" || p["percent"].(float64) != 20 {
		t.Fatalf("promo %v", r)
	}
	if pkg0(r)["price"].(float64) != 240000 || pkg0(r)["base"].(float64) != 300000 {
		t.Fatalf("discounted %v", r)
	}
	// The order charges the discounted price.
	code, r, body := call(mainTok, "900", "POST", "/app/api/panel", map[string]any{"package": "p1", "name": "promo-shop"})
	if code != 200 || r["price"].(float64) != 240000 {
		t.Fatalf("order %d %s", code, body)
	}
	// After the end time: full price and no banner.
	*now = now.Add(2 * 3600e9)
	_, r, _ = call(mainTok, "900", "GET", "/app/api/panels", nil)
	if r["promo"] != nil || pkg0(r)["price"].(float64) != 300000 {
		t.Fatalf("expired %v", r)
	}

	// A reseller's discount never goes under what it pays wholesale (100 GB = 100,000).
	if err := s.PutResaleOwner(ctx, "ali", true, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPayCards(ctx, "ali", []PayCard{{Number: "6219861000000001", Holder: "Ali", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if code, _, body := call(aliTok, "600", "POST", "/app/api/m/panelpromo", map[string]any{"on": true, "percent": 90}); code != 200 {
		t.Fatalf("ali set %d %s", code, body)
	}
	_, r, _ = call(aliTok, "950", "GET", "/app/api/panels", nil)
	if pkg0(r)["price"].(float64) != 100000 || pkg0(r)["base"].(float64) != 300000 {
		t.Fatalf("floor %v", r)
	}
	// The owner's banner is the owner's: ali's bot shows ali's.
	if s.PanelPromoFor(ctx, "").Percent != 20 || s.PanelPromoFor(ctx, "ali").Percent != 90 {
		t.Fatal("per seller")
	}
	// Selling by hand from the panel is not discounted.
	if _, _, err := s.RepWalletAdjust(ctx, "owner", "ali", 1_000_000, "test"); err != nil {
		t.Fatal(err)
	}
	if o, err := s.createRepOrder(ctx, "ali", 0, "x", "", "hand-two", "p1", true); err != nil || o.Price != 300000 {
		t.Fatalf("manual %+v %v", o, err)
	}
}
