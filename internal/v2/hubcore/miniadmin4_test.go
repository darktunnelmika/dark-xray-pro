package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniAdminSettings(t *testing.T) {
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
	call := func(tok, chat, method, path string, body any) (int, string) {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(b))
		req.Header.Set("X-Init-Data", signInitData(tok, `{"id":`+chat+`,"first_name":"A"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// Texts, channels, trial and look are the reseller's own.
	code, body := call(aliTok, "600", "POST", "/app/api/m/settings", map[string]any{
		"welcome": "به فروشگاه علی خوش آمدید", "shop_text": "همه با پشتیبانی",
		"join":    map[string]any{"enabled": true, "channels": []string{"t.me/alichan"}},
		"trial":   map[string]any{"enabled": true, "mb": 300, "hours": 12, "max_ips": 1},
		"brand":   map[string]any{"title": "Ali Shop", "accent": "#FF0000", "telegram": "@ali_support"},
	})
	if code != 200 {
		t.Fatalf("settings %d %s", code, body)
	}
	if s.Welcome(ctx, "ali") != "به فروشگاه علی خوش آمدید" || s.Welcome(ctx, "") != "" || s.ShopText(ctx, "ali") != "همه با پشتیبانی" {
		t.Fatal("texts")
	}
	if j := s.JoinPolicy(ctx, "ali"); !j.Enabled || j.Channels[0] != "@alichan" {
		t.Fatalf("join %+v", j)
	}
	if tp := s.TrialPolicyFor(ctx, "ali"); !tp.Enabled || tp.MB != 300 || tp.Hours != 12 {
		t.Fatalf("trial %+v", tp)
	}
	if b := s.RepBrand(ctx, "ali"); b.Title != "Ali Shop" || b.Accent != "#ff0000" || b.Telegram != "ali_support" {
		t.Fatalf("brand %+v", b)
	}
	if code, body := call(aliTok, "600", "POST", "/app/api/m/settings", map[string]any{"join": map[string]any{"enabled": true, "channels": []string{"bad name!"}}}); code != 400 || !strings.Contains(body, "@mychannel") {
		t.Fatalf("bad channel %d %s", code, body)
	}

	// Discount codes: the reseller's own work only on its plans.
	if err := s.PutProduct(ctx, Principal{Role: "owner"}, Product{ID: "own", Name: "Own", Plan: "volume", GB: 10, Days: 30, Price: 100000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProduct(ctx, Principal{Role: "owner"}, Product{ID: "alis", Seller: "ali", Name: "Ali's", Plan: "volume", GB: 10, Days: 30, Price: 100000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if code, body := call(aliTok, "600", "POST", "/app/api/m/discount", map[string]any{"code": "ali20", "percent": 20, "enabled": true}); code != 200 {
		t.Fatalf("code %d %s", code, body)
	}
	if _, err := s.checkDiscount(ctx, s.ro, "ALI20", "alis", "tg:1"); err != nil {
		t.Fatal("on ali's plan", err)
	}
	if _, err := s.checkDiscount(ctx, s.ro, "ALI20", "own", "tg:1"); !errors.Is(err, ErrCodeProduct) {
		t.Fatal("on the owner's plan", err)
	}
	// The owner cannot take it over, nor see it in the app's list.
	if code, _ := call(mainTok, "500", "POST", "/app/api/m/discount", map[string]any{"code": "ALI20", "percent": 50, "enabled": true}); code != 400 {
		t.Fatal("taken over")
	}
	if code, body := call(mainTok, "500", "GET", "/app/api/m/discounts", nil); code != 200 || strings.Contains(body, "ALI20") {
		t.Fatalf("owner list %s", body)
	}
	if code, _ := call(mainTok, "500", "POST", "/app/api/m/discount/delete", map[string]any{"code": "ALI20"}); code != 404 {
		t.Fatal("deleted another's code")
	}
	if code, body := call(aliTok, "600", "GET", "/app/api/m/discounts", nil); code != 200 || !strings.Contains(body, "ALI20") {
		t.Fatalf("ali list %s", body)
	}

	// A message to the customers, through the seller's bot.
	var sent struct {
		seller, text, aud string
		chat              int64
	}
	SetAppBot(AppBot{
		Audience: func(_ context.Context, seller string) map[string]int { return map[string]int{"all": 3} },
		Broadcast: func(_ context.Context, seller string, chat int64, text, aud string) (int, error) {
			sent.seller, sent.chat, sent.text, sent.aud = seller, chat, text, aud
			return 3, nil
		},
	})
	defer SetAppBot(AppBot{})
	if code, body := call(aliTok, "600", "GET", "/app/api/m/broadcast", nil); code != 200 || !strings.Contains(body, `"all":3`) {
		t.Fatalf("audience %s", body)
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/broadcast", map[string]any{"text": "سلام", "audience": "everyone"}); code != 400 {
		t.Fatal("bad audience")
	}
	if code, body := call(aliTok, "600", "POST", "/app/api/m/broadcast", map[string]any{"text": "سلام", "audience": "all"}); code != 200 || sent.seller != "ali" || sent.chat != 600 || sent.aud != "all" {
		t.Fatalf("broadcast %d %s %+v", code, body, sent)
	}
}
