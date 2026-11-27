package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAdminPermsAndTicketPhoto(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const mainTok = "111:main-bot-token-main-bot-token-000"
	const aliTok = "222333:ali-bot-token-ali-bot-token-abcdef"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{600}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: aliTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	code, _ := s.NewTgAdminInvite(ctx, "ali", "support")
	if _, _, err := s.ClaimTgAdminInvite(ctx, code, 700, "Sara"); err != nil {
		t.Fatal(err)
	}
	h := SubHandler(s)
	call := func(chat, method, path string, body any) int {
		t.Helper()
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(b))
		req.Header.Set("X-Init-Data", signInitData(aliTok, `{"id":`+chat+`,"first_name":"A"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// Support may not create; with "new" ticked it may, but still not delete.
	if c := call("700", "POST", "/app/api/m/create", map[string]any{"gb": 1, "days": 7}); c != 403 {
		t.Fatalf("support created %d", c)
	}
	if c := call("600", "POST", "/app/api/m/admins/perms", map[string]any{"chat": 700, "perms": []string{"view", "new", "bogus"}}); c != 200 {
		t.Fatalf("perms %d", c)
	}
	if _, role := s.TgRepRole(ctx, 700); role != "custom:view,new" {
		t.Fatalf("role %q", role)
	}
	if c := call("700", "POST", "/app/api/m/create", map[string]any{"gb": 1, "days": 7, "name": "p1"}); c != 200 {
		t.Fatalf("ticked create %d", c)
	}
	if c := call("700", "POST", "/app/api/m/set", map[string]any{"emails": []string{"p1"}, "action": "delete"}); c != 403 {
		t.Fatalf("deleted without the right %d", c)
	}
	if ok, custom := CustomCan("custom:settings", "shop"); !ok || !custom {
		t.Fatal("settings gives the bot's shop menu")
	}
	// Nothing ticked: back to the role.
	if c := call("600", "POST", "/app/api/m/admins/perms", map[string]any{"chat": 700, "perms": []string{}}); c != 200 {
		t.Fatal("reset")
	}
	if _, role := s.TgRepRole(ctx, 700); role != "support" {
		t.Fatalf("role after reset %q", role)
	}

	// A picture in a ticket: listed as a photo, fetched only by its seller.
	id, _, err := s.CustomerTicketPhoto(ctx, "ali", 950, "C", "", "file-1")
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := s.TicketByID(ctx, Principal{Role: "owner"}, id)
	if len(tk.Msgs) != 1 || !tk.Msgs[0].Photo || tk.Msgs[0].Text != "📷 عکس" {
		t.Fatalf("ticket %+v", tk.Msgs)
	}
	if seller, file, err := s.TicketPhoto(ctx, tk.Msgs[0].ID); err != nil || seller != "ali" || file != "file-1" {
		t.Fatalf("photo %s %s %v", seller, file, err)
	}
	if c := call("600", "GET", "/app/api/m/ticket/photo?msg=999", nil); c != 404 {
		t.Fatalf("unknown photo %d", c)
	}
}
