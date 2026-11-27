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

func TestMiniAdmin(t *testing.T) {
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
	for chat, role := range map[int64]string{700: "sales", 800: "support"} {
		code, _ := s.NewTgAdminInvite(ctx, "ali", role)
		if _, _, err := s.ClaimTgAdminInvite(ctx, code, chat, "x"); err != nil {
			t.Fatal(err)
		}
	}
	h := SubHandler(s)
	call := func(tok string, chat, method, path string, body any) (int, map[string]any) {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(b))
		req.Header.Set("X-Init-Data", signInitData(tok, `{"id":`+chat+`,"first_name":"A"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	// Who manages: the reseller (own bot), not a stranger; the store may be closed.
	if code, r := call(aliTok, "600", "GET", "/app/api/role", nil); code != 200 || r["manage"] != true {
		t.Fatalf("role %d %v", code, r)
	}
	if _, r := call(aliTok, "999", "GET", "/app/api/role", nil); r["manage"] != false {
		t.Fatalf("stranger %v", r)
	}
	if code, _ := call(aliTok, "999", "GET", "/app/api/m/home", nil); code != http.StatusForbidden {
		t.Fatal("stranger home")
	}
	// Create in one call: an automatic name, a link and a QR; credit is charged.
	code, r := call(aliTok, "600", "POST", "/app/api/m/create", map[string]any{"gb": 10, "days": 30, "daily_gb": 2, "note": "Reza"})
	if code != 200 || r["name"] == "" || !strings.Contains(r["sub"].(string), "/sub/") || !strings.HasPrefix(r["qr"].(string), "data:image/png;base64,") {
		t.Fatalf("create %d %v", code, r)
	}
	name := r["name"].(string)
	c, _ := s.ClientByEmail(ctx, name)
	if c.RepID != "ali" || c.VolumeBytes != 10<<30 || c.DailyCapBytes != 2<<30 || c.Note != "Reza" {
		t.Fatalf("client %+v", c)
	}
	_, home := call(aliTok, "600", "GET", "/app/api/m/home", nil)
	if home["credit"].(float64) != float64(90<<30) || home["stats"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("home %v", home)
	}
	// Bulk create with a prefix.
	if code, r := call(aliTok, "600", "POST", "/app/api/m/create", map[string]any{"gb": 1, "days": 7, "name": "shop", "count": 3}); code != 200 || len(r["names"].([]any)) != 3 {
		t.Fatalf("bulk %d %v", code, r)
	}
	// The list and its filter, renew, switch off.
	if _, r := call(aliTok, "600", "GET", "/app/api/m/clients?q=reza", nil); r["count"].(float64) != 1 {
		t.Fatalf("search %v", r)
	}
	if code, r := call(aliTok, "600", "POST", "/app/api/m/renew", map[string]any{"emails": []string{name}, "gb": 20, "days": 30}); code != 200 || r["done"].(float64) != 1 {
		t.Fatalf("renew %d %v", code, r)
	}
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/set", map[string]any{"emails": []string{name}, "action": "off"}); code != 200 {
		t.Fatal("off")
	}
	if c, _ := s.ClientByEmail(ctx, name); c.Enabled {
		t.Fatal("still on")
	}
	// Roles: sales creates but does not delete; support only looks.
	if code, _ := call(aliTok, "700", "POST", "/app/api/m/create", map[string]any{"gb": 1, "days": 7}); code != 200 {
		t.Fatal("sales create")
	}
	if code, _ := call(aliTok, "700", "POST", "/app/api/m/set", map[string]any{"emails": []string{name}, "action": "delete"}); code != http.StatusForbidden {
		t.Fatal("sales delete")
	}
	if code, _ := call(aliTok, "800", "POST", "/app/api/m/create", map[string]any{"gb": 1, "days": 7}); code != http.StatusForbidden {
		t.Fatal("support create")
	}
	if code, _ := call(aliTok, "800", "GET", "/app/api/m/clients", nil); code != 200 {
		t.Fatal("support view")
	}
	// A reseller never touches another's client.
	if code, r := call(aliTok, "600", "POST", "/app/api/m/set", map[string]any{"emails": []string{"alice"}, "action": "off"}); code != 200 || r["done"].(float64) != 0 {
		t.Fatalf("foreign client %d %v", code, r)
	}
	if code, _ := call(aliTok, "600", "GET", "/app/api/m/client?e=alice", nil); code != http.StatusNotFound {
		t.Fatal("foreign detail")
	}
	// The owner, from the main bot, manages the owner's world.
	if code, r := call(mainTok, "500", "POST", "/app/api/m/create", map[string]any{"gb": 5, "days": 10, "name": "boss-1"}); code != 200 || r["name"] != "boss-1" {
		t.Fatalf("owner create %d %v", code, r)
	}
	if c, _ := s.ClientByEmail(ctx, "boss-1"); c.RepID != "" || c.VolumeBytes != 5<<30 {
		t.Fatalf("owner client %+v", c)
	}
	if code, _ := call(aliTok, "500", "GET", "/app/api/m/home", nil); code != http.StatusForbidden {
		t.Fatal("the owner is not a manager in a reseller's bot")
	}
	if code, _ := call(aliTok, "600", "GET", "/app/api/m/inbox", nil); code != 200 {
		t.Fatal("inbox")
	}
	// Delete gives unused volume back (by the reseller's rules) and removes it.
	if code, _ := call(aliTok, "600", "POST", "/app/api/m/set", map[string]any{"emails": []string{name}, "action": "delete"}); code != 200 {
		t.Fatal("delete")
	}
	if _, err := s.ClientByEmail(ctx, name); err == nil {
		t.Fatal("not deleted")
	}
}
