package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniAdminSaleAndHelp(t *testing.T) {
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
		req.Header.Set("X-Init-Data", signInitData(tok, `{"id":`+chat+`,"first_name":"Cust"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	if code, body := call(aliTok, "600", "POST", "/app/api/m/sale", map[string]any{"autoclean": map[string]any{"on": true, "days": 45}, "quick": []string{" آپدیت کنید ", ""}}); code != 200 {
		t.Fatalf("sale %d %s", code, body)
	}
	if a := s.AutoCleanFor(ctx, "ali"); !a.On || a.Days != 45 {
		t.Fatalf("autoclean %+v", a)
	}
	if q := s.QuickReplies(ctx, "ali"); len(q) != 1 || q[0] != "آپدیت کنید" {
		t.Fatalf("quick %v", q)
	}
	if code, body := call(aliTok, "600", "GET", "/app/api/m/sale", nil); code != 200 || !strings.Contains(body, "domains") {
		t.Fatalf("sale view %s", body)
	}
	if code, body := call(aliTok, "600", "GET", "/app/api/m/home", nil); code != 200 || !strings.Contains(body, `"month"`) || !strings.Contains(body, "toman_today") {
		t.Fatalf("home %s", body)
	}
	// A customer of ali's bot can't connect: a ticket for ali, with the device.
	code, body := call(aliTok, "950", "POST", "/app/api/help", map[string]any{"service": "someone-else", "device": "آیفون", "app": "Streisand", "note": "خطای timeout"})
	if code != 200 {
		t.Fatalf("help %d %s", code, body)
	}
	ts, _ := s.Tickets(ctx, Principal{Role: "owner"}, "active", 10)
	if len(ts) != 1 || ts[0].Seller != "ali" || !strings.Contains(ts[0].Last, "Streisand") || strings.Contains(ts[0].Last, "someone-else") {
		t.Fatalf("ticket %+v", ts)
	}
}
