package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCustomPlan(t *testing.T) {
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
	if code, body := call("950", "GET", "/app/api/custom", nil); code != 200 || !strings.Contains(body, `"enabled":false`) {
		t.Fatalf("off %d %s", code, body)
	}
	if code, body := call("600", "POST", "/app/api/m/custom", map[string]any{"enabled": true, "price_gb": 3000, "price_10d": 10000, "min_gb": 10, "max_gb": 100, "min_days": 10, "max_days": 60}); code != 200 {
		t.Fatalf("set %d %s", code, body)
	}
	// 50 GB for 30 days: 50×3000 + 3×10000.
	code, body := call("950", "POST", "/app/api/custom", map[string]any{"gb": 50, "days": 30, "quote": true})
	if code != 200 || !strings.Contains(body, `"price":180000`) || !strings.Contains(body, `"seller":"ali"`) {
		t.Fatalf("quote %d %s", code, body)
	}
	if code, _ := call("950", "POST", "/app/api/custom", map[string]any{"gb": 500, "days": 30}); code != 400 {
		t.Fatal("out of range")
	}
	if code, body := call("950", "POST", "/app/api/custom", map[string]any{"gb": 50, "days": 30}); code != 200 || !strings.Contains(body, `"id"`) {
		t.Fatalf("order %d %s", code, body)
	}
	o, err := s.Orders(ctx, Principal{Role: "owner"}, "pending", 10)
	if err != nil || len(o) != 1 || o[0].Seller != "ali" || o[0].Amount != 180000 || !isCustomProduct(o[0].ProductID) {
		t.Fatalf("order %+v %v", o, err)
	}
	// The sizes never show up as plans.
	ps, _ := s.Products(ctx, Principal{Role: "owner"}, false)
	for _, p := range ps {
		if isCustomProduct(p.ID) {
			t.Fatal("custom product listed")
		}
	}
	// Delivered like any order: a 50 GB, 30-day client on ali's credit.
	email, err := s.FulfillOrder(ctx, Principal{Role: "owner"}, o[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.ClientByEmail(ctx, email)
	if c.RepID != "ali" || c.VolumeBytes != 50<<30 || s.RepCredit(ctx, "ali") != 50<<30 {
		t.Fatalf("client %+v credit %d", c.Client, s.RepCredit(ctx, "ali")>>30)
	}
}
