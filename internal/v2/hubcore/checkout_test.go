package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCouponAndTheme(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const mainTok = "111:main-bot-token-main-bot-token-000"
	const aliTok = "222333:ali-bot-token-ali-bot-token-abcdef"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	_ = s.SetSetting(ctx, "miniapp", "1")
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{600}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: aliTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProduct(ctx, Principal{Role: "owner"}, Product{ID: "alis", Seller: "ali", Name: "A", Plan: "volume", GB: 10, Days: 30, Price: 100000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDiscountFor(ctx, "ali", Discount{Code: "OFF20", Percent: 20, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h := SubHandler(s)
	call := func(chat, method, path string, body any) (int, string) {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(b))
		req.Header.Set("X-Init-Data", signInitData(aliTok, `{"id":`+chat+`,"first_name":"C"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := call("950", "POST", "/app/api/coupon", map[string]any{"code": "off20", "product": "alis"}); code != 200 || !strings.Contains(body, `"price":80000`) {
		t.Fatalf("coupon %d %s", code, body)
	}
	if code, body := call("950", "POST", "/app/api/coupon", map[string]any{"code": "NOPE", "product": "alis"}); code != 400 || !strings.Contains(body, "وجود ندارد") {
		t.Fatalf("bad code %d %s", code, body)
	}
	// The held code lands on the next order.
	if code, body := call("950", "POST", "/app/api/order", map[string]any{"product": "alis"}); code != 200 {
		t.Fatalf("order %d %s", code, body)
	}
	o, _ := s.Orders(ctx, Principal{Role: "owner"}, "pending", 5)
	if len(o) != 1 || o[0].Amount != 80000 || o[0].DiscountCode != "OFF20" {
		t.Fatalf("order %+v", o)
	}
	// The store's theme.
	if code, body := call("600", "POST", "/app/api/m/settings", map[string]any{"theme": "emerald"}); code != 200 {
		t.Fatalf("theme %d %s", code, body)
	}
	if code, _ := call("600", "POST", "/app/api/m/settings", map[string]any{"theme": "neon"}); code != 400 {
		t.Fatal("bad theme")
	}
	if code, body := call("950", "GET", "/app/api/me", nil); code != 200 || !strings.Contains(body, `"theme":"emerald"`) {
		t.Fatalf("me %d %s", code, body)
	}
}
