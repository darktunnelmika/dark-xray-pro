package hubcore

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTgPanelLogin(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	const mainTok = "111:main-bot-token-main-bot-token-000"
	const aliTok = "222333:ali-bot-token-ali-bot-token-abcdef"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	if err := s.SetAdmin(ctx, "boss", "a-long-password-1"); err != nil {
		t.Fatal(err)
	}
	_ = s.SetSetting(ctx, "tg_owners", "500")
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.PutRep(ctx, Rep{ID: "kid", Name: "Kid", Enabled: true})
	_, _ = s.db.Exec(`UPDATE reps SET parent_id='ali' WHERE id='kid'`)
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: aliTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTgRepOwners(ctx, "ali", []int64{600}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTgRepOwners(ctx, "kid", []int64{650}); err != nil {
		t.Fatal(err)
	}
	add := func(chat int64, role string) {
		code, err := s.NewTgAdminInvite(ctx, "ali", role)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.ClaimTgAdminInvite(ctx, code, chat, "x"); err != nil {
			t.Fatal(err)
		}
	}
	add(700, "manager")
	add(800, "sales")
	user := func(id string) string { return `{"id":` + id + `,"first_name":"A"}` }
	login := func(tok, id string) (string, error) {
		_, u, err := s.TgPanelLogin(ctx, signInitData(tok, user(id), *now))
		return u, err
	}
	// The owner, from the main bot.
	if u, err := login(mainTok, "500"); err != nil || u != "boss" {
		t.Fatalf("owner: %q %v", u, err)
	}
	// The reseller, from its own bot: a Telegram-only login is made.
	if u, err := login(aliTok, "600"); err != nil || u != "tg-ali" {
		t.Fatalf("reseller: %q %v", u, err)
	}
	if p, _ := s.principal(ctx, "tg-ali"); p.Role != "rep" || p.RepID != "ali" {
		t.Fatalf("principal %+v", p)
	}
	// ...and from the main bot too.
	if u, err := login(mainTok, "600"); err != nil || u != "tg-ali" {
		t.Fatalf("reseller in the main bot: %q %v", u, err)
	}
	// A manager gets in, a sales admin does not, a stranger does not.
	if u, err := login(aliTok, "700"); err != nil || u != "tg-ali" {
		t.Fatalf("manager: %q %v", u, err)
	}
	if _, err := login(aliTok, "800"); err == nil {
		t.Fatal("sales admin")
	}
	if _, err := login(aliTok, "999"); err == nil {
		t.Fatal("stranger")
	}
	// The owner's chat in a reseller's bot is not the owner there.
	if _, err := login(aliTok, "500"); err == nil {
		t.Fatal("owner in a reseller's bot")
	}
	// A sub-seller in its parent's bot.
	if u, err := login(aliTok, "650"); err != nil || u != "tg-kid" {
		t.Fatalf("sub-seller: %q %v", u, err)
	}
	// A wrong signature, or an old one.
	if _, err := login("999:wrong-wrong-wrong-wrong-wrong-wrong", "600"); err == nil {
		t.Fatal("wrong bot")
	}
	if _, _, err := s.TgPanelLogin(ctx, signInitData(aliTok, user("600"), now.Add(-2*time.Hour))); err == nil {
		t.Fatal("old initData")
	}
	// The session works like a password login's.
	tok, _, err := s.TgPanelLogin(ctx, signInitData(aliTok, user("600"), *now))
	if err != nil {
		t.Fatal(err)
	}
	if u, err := s.SessionUser(ctx, tok); err != nil || u != "tg-ali" {
		t.Fatalf("session: %q %v", u, err)
	}
	// Two-step on: no Telegram sign-in.
	_, _ = s.db.Exec(`UPDATE admins SET totp_secret='ABC' WHERE username='boss'`)
	if _, err := login(mainTok, "500"); err == nil {
		t.Fatal("two-step login must use the password")
	}
	if gated(httptest.NewRequest("GET", "/tgapp/", nil)) || gated(httptest.NewRequest("POST", "/tgapp/login", nil)) {
		t.Fatal("/tgapp/ must not be behind the secret path")
	}
}
