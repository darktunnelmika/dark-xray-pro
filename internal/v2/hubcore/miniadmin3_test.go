package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMiniAdminStage25(t *testing.T) {
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

	// Add volume and days on the current term: only the volume is charged.
	_, r, _ := call(aliTok, "600", "POST", "/app/api/m/create", map[string]any{"name": "cust", "gb": 10, "days": 30})
	if r["name"] != "cust" {
		t.Fatalf("create %v", r)
	}
	before, _ := s.ClientByEmail(ctx, "cust")
	if code, r, body := call(aliTok, "600", "POST", "/app/api/m/add", map[string]any{"emails": []string{"cust"}, "gb": 5, "days": 10}); code != 200 || r["done"].(float64) != 1 {
		t.Fatalf("add %d %s", code, body)
	}
	c, _ := s.ClientByEmail(ctx, "cust")
	if c.VolumeBytes != 15<<30 || c.ExpiresUnix != before.ExpiresUnix+10*86400 || s.RepCredit(ctx, "ali") != 85<<30 {
		t.Fatalf("after add %d %d credit %d", c.VolumeBytes, c.ExpiresUnix-before.ExpiresUnix, s.RepCredit(ctx, "ali")>>30)
	}
	if code, _, _ := call(aliTok, "600", "POST", "/app/api/m/add", map[string]any{"emails": []string{"cust"}}); code != 400 {
		t.Fatal("empty add")
	}
	// The client sheet lists each config apart.
	if _, r, _ := call(aliTok, "600", "GET", "/app/api/m/client?e=cust", nil); r == nil {
		t.Fatal("client")
	} else if _, has := r["links"]; !has {
		t.Fatalf("links %v", r)
	}

	// Panel sale is off until the owner allows it.
	if _, r, _ := call(aliTok, "600", "GET", "/app/api/m/resale", nil); r["allowed"] != false {
		t.Fatalf("resale before %v", r)
	}
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, WholesaleTB: 1_024_000, Packages: []RepPackage{{ID: "p1", Name: "100", GB: 100, Price: 300000, MaxClients: 50}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResaleOwner(ctx, "ali", true, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPayCards(ctx, "ali", []PayCard{{Number: "6219861000000001", Holder: "Ali", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	_, r, _ = call(aliTok, "600", "GET", "/app/api/m/resale", nil)
	if r["allowed"] != true {
		t.Fatalf("resale %v", r)
	}
	_, home, _ := call(aliTok, "600", "GET", "/app/api/m/home", nil)
	if home["resale"] != true {
		t.Fatal("home resale")
	}
	// Prices: never below the wholesale price (100 GB = 100,000 toman here).
	if code, _, body := call(aliTok, "600", "POST", "/app/api/m/resale/prices", map[string]any{"enabled": true, "prices": map[string]int64{"p1": 50000}}); code != 400 {
		t.Fatalf("cheap %d %s", code, body)
	}
	if code, _, _ := call(aliTok, "600", "POST", "/app/api/m/resale/prices", map[string]any{"enabled": true, "prices": map[string]int64{"p1": 400000}}); code != 200 {
		t.Fatal("prices")
	}
	// Selling by hand: the wallet is empty, so a bill pays the wholesale price.
	code, r, body := call(aliTok, "600", "POST", "/app/api/m/resale/sell", map[string]any{"name": "hand-shop", "package": "p1"})
	if code != 200 || r["username"] != "hand-shop" || len(r["password"].(string)) < 10 || r["bill"].(float64) == 0 {
		t.Fatalf("sell %d %s", code, body)
	}
	if !s.IsSellerOf(ctx, "ali", "hand-shop") || s.RepCredit(ctx, "hand-shop") != 100<<30 {
		t.Fatal("sold panel")
	}
	if code, r, _ := call(aliTok, "600", "POST", "/app/api/m/resale/panel", map[string]any{"id": "hand-shop", "password": true}); code != 200 || r["password"] == "" {
		t.Fatal("new password")
	}

	// A customer of ali's bot buys a panel from the store; ali approves it in the inbox.
	_, r, _ = call(aliTok, "950", "GET", "/app/api/panels", nil)
	if r["on"] != true || r["packages"].([]any)[0].(map[string]any)["price"].(float64) != 400000 {
		t.Fatalf("panels %v", r)
	}
	if _, r, _ := call(aliTok, "600", "GET", "/app/api/panels", nil); r["on"] != false {
		t.Fatal("the reseller buys from itself")
	}
	code, r, body = call(aliTok, "950", "POST", "/app/api/panel", map[string]any{"package": "p1", "name": "bot-shop"})
	if code != 200 || r["card"].(map[string]any)["number"] != "6219861000000001" || r["price"].(float64) != 400000 {
		t.Fatalf("panel order %d %s", code, body)
	}
	oid := int64(r["id"].(float64))
	if _, err := s.AttachRepReceipt(ctx, oid, 950, "photo-1"); err != nil {
		t.Fatal(err)
	}
	var found bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "https://hub.example/app/api/m/inbox", nil)
	req.Header.Set("X-Init-Data", signInitData(aliTok, `{"id":600,"first_name":"A"}`, *now))
	h.ServeHTTP(rec, req)
	var inbox []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inbox)
	for _, it := range inbox {
		if it["kind"] == "panel" && int64(it["id"].(float64)) == oid && it["receipt"] == true {
			found = true
		}
	}
	if !found {
		t.Fatalf("inbox %s", rec.Body.String())
	}
	// One open bill at a time: the wallet now pays (and settles the first bill).
	if code, _, body := call(aliTok, "600", "POST", "/app/api/m/decide", map[string]any{"kind": "panel", "id": oid, "ok": true}); code != 400 || !bytes.Contains([]byte(body), []byte("کیف پول")) {
		t.Fatalf("no funds %d %s", code, body)
	}
	if _, _, err := s.RepWalletAdjust(ctx, "owner", "ali", 1_000_000, "paid"); err != nil {
		t.Fatal(err)
	}
	if code, _, body := call(aliTok, "600", "POST", "/app/api/m/decide", map[string]any{"kind": "panel", "id": oid, "ok": true}); code != 200 {
		t.Fatalf("decide %d %s", code, body)
	}
	if !s.IsSellerOf(ctx, "ali", "bot-shop") {
		t.Fatal("the bought panel")
	}
	if code, _, _ := call(aliTok, "600", "POST", "/app/api/m/decide", map[string]any{"kind": "panel", "id": oid, "ok": true}); code == 200 {
		t.Fatal("decided twice")
	}

	// The owner edits the packages; settings it cannot see are kept.
	if code, _, body := call(mainTok, "500", "POST", "/app/api/m/resale/sale", map[string]any{"enabled": true, "packages": []map[string]any{{"id": "p1", "name": "100G", "gb": 100, "price": 350000}, {"name": "1T", "gb": 1000, "price": 2000000}}}); code != 200 {
		t.Fatalf("sale %d %s", code, body)
	}
	sale := s.RepSale(ctx)
	if len(sale.Packages) != 2 || sale.Packages[0].MaxClients != 50 || sale.Packages[0].Price != 350000 || sale.WholesaleTB != 1_024_000 {
		t.Fatalf("sale %+v", sale)
	}
	if code, _, _ := call(aliTok, "600", "POST", "/app/api/m/resale/sale", map[string]any{"enabled": false}); code != 403 {
		t.Fatal("a reseller changed the owner's packages")
	}
}
