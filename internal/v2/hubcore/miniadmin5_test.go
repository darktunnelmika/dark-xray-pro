package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMiniAdminCustomersAdminsLimits(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const mainTok = "111:main-bot-token-main-bot-token-000"
	const aliTok = "222333:ali-bot-token-ali-bot-token-abcdef"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	_ = s.SetSetting(ctx, "tg_owners", "500")
	_ = s.SetSetting(ctx, "miniapp", "1")
	_ = s.SetSetting(ctx, "tg_username", "mainbot")
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{600}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: aliTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	told := map[int64]string{}
	SetCustomerNotify(func(_ context.Context, chat int64, text string) { mu.Lock(); told[chat] = text; mu.Unlock() })
	defer SetCustomerNotify(nil)
	waitTold := func(chat int64) string {
		for i := 0; i < 100; i++ {
			mu.Lock()
			v := told[chat]
			mu.Unlock()
			if v != "" {
				return v
			}
			time.Sleep(10 * time.Millisecond)
		}
		return ""
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

	// Customers: those who started ali's bot, not the main bot's.
	_ = s.TgTouch(ctx, 901, "Reza Ahmadi", "reza")
	s.TgTouchBot(ctx, "ali", 901)
	_ = s.TgTouch(ctx, 902, "Main Only", "")
	s.TgTouchBot(ctx, "", 902)
	code, body := call(aliTok, "600", "GET", "/app/api/m/customers?q=rez", nil)
	if code != 200 || !strings.Contains(body, "Reza Ahmadi") || strings.Contains(body, "Main Only") {
		t.Fatalf("customers %d %s", code, body)
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/customer", map[string]any{"chat": 902, "action": "ban"}); code != 404 {
		t.Fatal("banned someone else's customer")
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/customer", map[string]any{"chat": 901, "action": "message", "text": "سلام <b>"}); code != 200 {
		t.Fatal("message")
	}
	got := waitTold(901)
	if !strings.Contains(got, "&lt;b&gt;") {
		t.Fatalf("message not escaped: %q", got)
	}
	if code, body := call(aliTok, "600", "POST", "/app/api/m/customer", map[string]any{"chat": 901, "action": "wallet", "delta": 50000}); code != 200 || !strings.Contains(body, "50000") {
		t.Fatalf("wallet %d %s", code, body)
	}
	// A banned customer gets nothing from ali's store, but still the owner's.
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/customer", map[string]any{"chat": 901, "action": "ban"}); code != 200 || !s.TgBanned(ctx, "ali", 901) {
		t.Fatal("ban")
	}
	if code, _ := call(aliTok, "901", "GET", "/app/api/me", nil); code != 403 {
		t.Fatalf("banned store %d", code)
	}
	if code, _ := call(mainTok, "901", "GET", "/app/api/me", nil); code == 403 {
		t.Fatal("banned in the main store too")
	}

	// Admins: invite by role, change it, remove; only the reseller itself.
	code, body = call(aliTok, "600", "POST", "/app/api/m/admins/invite", map[string]any{"role": "sales"})
	if code != 200 || !strings.Contains(body, "?start=adm_") {
		t.Fatalf("invite %d %s", code, body)
	}
	var inv struct{ Link string }
	_ = json.Unmarshal([]byte(body), &inv)
	codeStr := inv.Link[strings.Index(inv.Link, "adm_")+4:]
	if _, _, err := s.ClaimTgAdminInvite(ctx, codeStr, 700, "Sara"); err != nil {
		t.Fatal(err)
	}
	if code, body := call(aliTok, "600", "GET", "/app/api/m/admins", nil); code != 200 || !strings.Contains(body, `"role":"sales"`) {
		t.Fatalf("admins %s", body)
	}
	if code, _ := call(aliTok, "700", "GET", "/app/api/m/admins", nil); code != 403 {
		t.Fatal("an admin managed admins")
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/admins/set", map[string]any{"chat": 700, "role": "manager"}); code != 200 {
		t.Fatal("role")
	}
	if _, role := s.TgRepRole(ctx, 700); role != "manager" {
		t.Fatal("role not changed", role)
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/admins/set", map[string]any{"chat": 700, "role": ""}); code != 200 {
		t.Fatal("remove")
	}
	if rep, _ := s.TgRepRole(ctx, 700); rep != "" {
		t.Fatal("not removed")
	}

	// A sub-seller's limits and a message to it.
	if code, body := call(aliTok, "600", "POST", "/app/api/m/sellers/new", map[string]any{"id": "ali-sub", "credit_gb": 5, "tg": 801}); code != 200 {
		t.Fatalf("sub %s", body)
	}
	if code, body := call(aliTok, "600", "POST", "/app/api/m/sellers/policy", map[string]any{"id": "ali-sub", "max_clients": 20, "deny_create": true, "allow_unlimited": false}); code != 200 {
		t.Fatalf("policy %s", body)
	}
	if p, _ := s.RepPolicyOf(ctx, "ali-sub"); p.MaxClients != 20 || !p.DenyCreate || p.AllowUnlimited {
		t.Fatalf("policy %+v", p)
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/sellers/message", map[string]any{"id": "ali-sub", "text": "تمدید یادت نره"}); code != 200 {
		t.Fatal("message to seller")
	}
	got = waitTold(801)
	if !strings.Contains(got, "تمدید یادت نره") {
		t.Fatalf("seller message %q", got)
	}
}
