package hubcore

import (
	"context"
	"errors"
	"testing"
)

// A reseller is made in one go, or not at all; its password survives a
// stray space; several Telegram accounts are the reseller on a par.
func TestCreateRepAndTelegramOwners(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.SetAdmin(ctx, "boss", "a long owner password"); err != nil {
		t.Fatal(err)
	}
	pol := RepPolicy{CanDelete: true, RefundUnused: true}
	base := RepCreate{ID: "arda", Name: "Arda", Username: "ardavpn", Password: "  correct-horse-1  ", Policy: &pol, CreditGB: 1024, TgOwners: []int64{111, 222, 111}}

	// Nothing is left behind by a refused request.
	for _, bad := range []func(*RepCreate){
		func(r *RepCreate) { r.Password = "short" },
		func(r *RepCreate) { r.Username = "boss" },
		func(r *RepCreate) { r.ID = "Bad Id" },
		func(r *RepCreate) {
			r.TgOwners = make([]int64, 0)
			for i := int64(1); i <= 11; i++ {
				r.TgOwners = append(r.TgOwners, i)
			}
		},
	} {
		in := base
		bad(&in)
		if err := s.CreateRep(ctx, "boss", in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("want a refusal, got %v", err)
		}
		if reps, _ := s.Reps(ctx); len(reps) != 0 {
			t.Fatalf("left behind: %+v", reps)
		}
		if l := s.RepLogins(ctx, "arda"); len(l) != 0 {
			t.Fatalf("login left behind: %v", l)
		}
	}
	if _, err := s.Login(ctx, "boss", "a long owner password"); err != nil {
		t.Fatal("the owner's login must survive a clash", err)
	}

	if err := s.CreateRep(ctx, "boss", base); err != nil {
		t.Fatal(err)
	}
	reps, _ := s.Reps(ctx)
	if len(reps) != 1 || reps[0].VolumeCredit != 1024<<30 || !reps[0].Policy.RefundUnused || reps[0].Policy.AllowUnlimited {
		t.Fatalf("%+v", reps)
	}
	// The password was stored without the spaces and is accepted with them.
	for _, p := range []string{"correct-horse-1", " correct-horse-1\n"} {
		if _, err := s.Login(ctx, "ardavpn", p); err != nil {
			t.Fatalf("%q: %v", p, err)
		}
	}
	if err := s.CreateRep(ctx, "boss", base); !errors.Is(err, ErrInvalid) {
		t.Fatal("twice", err)
	}

	// Both accounts are the reseller.
	if o := s.TgRepOwners(ctx, "arda"); len(o) != 2 || o[0] != 111 || o[1] != 222 {
		t.Fatal(o)
	}
	for _, c := range []int64{111, 222} {
		if rep, role := s.TgRepRole(ctx, c); rep != "arda" || role != "owner" {
			t.Fatal(c, rep, role)
		}
	}
	if c := s.TgRepChats(ctx, "arda"); len(c) != 2 {
		t.Fatal(c)
	}
	if c := s.TgAllRepChats(ctx); len(c) != 2 {
		t.Fatal(c)
	}
	// An account moves with the list: 222 goes to another panel, 333 joins by code.
	if err := s.CreateRep(ctx, "boss", RepCreate{ID: "sara", Password: "another-pass-9", TgOwners: []int64{222}}); err != nil {
		t.Fatal(err)
	}
	if rep, _ := s.TgRepRole(ctx, 222); rep != "sara" {
		t.Fatal(rep)
	}
	if o := s.TgRepOwners(ctx, "arda"); len(o) != 1 || o[0] != 111 {
		t.Fatal(o)
	}
	code, _ := s.NewTgLinkCode(ctx, "arda")
	if rep, err := s.ClaimTgLinkCode(ctx, code, 333); err != nil || rep != "arda" {
		t.Fatal(rep, err)
	}
	if o := s.TgRepOwners(ctx, "arda"); len(o) != 2 || o[0] != 111 || o[1] != 333 {
		t.Fatal("a code adds an account, it does not replace one", o)
	}
	if err := s.SetTgRepOwners(ctx, "arda", nil); err != nil {
		t.Fatal(err)
	}
	if rep, _ := s.TgRepRole(ctx, 111); rep != "" {
		t.Fatal("removed", rep)
	}

	// A new password for an existing reseller; nobody else's login is touched.
	if err := s.SetRepLogin(ctx, "ardavpn", "brand-new-pass-7", "arda"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "ardavpn", "correct-horse-1"); err == nil {
		t.Fatal("old password still works")
	}
	if err := s.SetRepLogin(ctx, "boss", "brand-new-pass-7", "arda"); !errors.Is(err, ErrInvalid) {
		t.Fatal("took over the owner's login", err)
	}
	if err := s.SetRepLogin(ctx, "sara", "brand-new-pass-7", "arda"); !errors.Is(err, ErrInvalid) {
		t.Fatal("took over another reseller's login", err)
	}
}

// A reseller opens a sub-seller in one go, paying its first credit from its own.
func TestCreateSeller(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	pol := RepPolicy{CanDelete: true, MaxDays: 60}
	if err := s.CreateRep(ctx, "boss", RepCreate{ID: "parent", Password: "parent-pass-99", Policy: &pol, CreditGB: 100}); err != nil {
		t.Fatal(err)
	}
	want := RepPolicy{CanDelete: true, RefundUnused: true, AllowUnlimited: true, MaxDays: 365}
	in := RepCreate{ID: "kid", Password: "kid-password-1", Policy: &want, CreditGB: 500, TgOwners: []int64{71, 72}}
	if err := s.CreateSeller(ctx, "parent", "parent", in); !errors.Is(err, ErrCredit) {
		t.Fatal("more than the parent has", err)
	}
	if subs, _ := s.Sellers(ctx, "parent"); len(subs) != 0 || len(s.RepLogins(ctx, "kid")) != 0 {
		t.Fatal("left behind", subs)
	}
	in.CreditGB = 30
	bad := in
	bad.Password = "short"
	if err := s.CreateSeller(ctx, "parent", "parent", bad); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if subs, _ := s.Sellers(ctx, "parent"); len(subs) != 0 {
		t.Fatal("left behind", subs)
	}
	if err := s.CreateSeller(ctx, "parent", "parent", in); err != nil {
		t.Fatal(err)
	}
	subs, _ := s.Sellers(ctx, "parent")
	if len(subs) != 1 || subs[0].VolumeCredit != 30<<30 {
		t.Fatalf("%+v", subs)
	}
	// Never more than the parent may itself.
	if p := subs[0].Policy; p.MaxDays != 60 || p.RefundUnused || p.AllowUnlimited || !p.CanDelete {
		t.Fatalf("%+v", p)
	}
	if c := s.RepCredit(ctx, "parent"); c != 70<<30 {
		t.Fatal(c)
	}
	if _, err := s.Login(ctx, "kid", "kid-password-1"); err != nil {
		t.Fatal(err)
	}
	if rep, role := s.TgRepRole(ctx, 72); rep != "kid" || role != "owner" {
		t.Fatal(rep, role)
	}
	if err := s.CreateSeller(ctx, "kid", "kid", RepCreate{ID: "grandkid", Password: "grand-pass-123"}); err == nil {
		t.Fatal("a sub-seller cannot open sellers")
	}
}

// A look is checked before it is saved; a custom banner needs its picture.
func TestBotLook(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, bad := range []BotLook{
		{Buttons: []BotLookButton{{Key: "nope"}}},
		{Buttons: []BotLookButton{{Key: "shop"}, {Key: "shop"}}},
		{Buttons: []BotLookButton{{Key: "shop", Style: "pink"}}},
		{Buttons: []BotLookButton{{Key: "shop", Hidden: true}}},
		{Buttons: []BotLookButton{{Key: "shop", Label: "a\nb"}}},
		{Banner: "custom"},
	} {
		if err := s.PutBotLook(ctx, "", bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	if err := s.PutBotBanner(ctx, "rep1", []byte("GIF89a....")); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	if err := s.PutBotBanner(ctx, "rep1", png); err != nil {
		t.Fatal(err)
	}
	look := BotLook{Buttons: []BotLookButton{{Key: "shop", Label: " خرید ", Style: "success", Wide: true}, {Key: "me", Hidden: true}}, Banner: "custom"}
	if err := s.PutBotLook(ctx, "rep1", look); err != nil {
		t.Fatal(err)
	}
	got := s.BotLook(ctx, "rep1")
	if len(got.Buttons) != 2 || got.Buttons[0].Label != "خرید" || got.Banner != "custom" {
		t.Fatalf("%+v", got)
	}
	if l := s.BotLook(ctx, ""); len(l.Buttons) != 0 || l.Banner != "" {
		t.Fatal("each seller has its own look", l)
	}
	s.SetBotBannerFileID(ctx, "rep1", "file-1")
	if _, id, ok := s.BotBanner(ctx, "rep1"); !ok || id != "file-1" {
		t.Fatal(id, ok)
	}
	// A new picture forgets the old Telegram file; removing it ends "custom".
	_ = s.PutBotBanner(ctx, "rep1", png)
	if _, id, _ := s.BotBanner(ctx, "rep1"); id != "" {
		t.Fatal(id)
	}
	if err := s.DeleteBotBanner(ctx, "rep1"); err != nil {
		t.Fatal(err)
	}
	if l := s.BotLook(ctx, "rep1"); l.Banner != "" || len(l.Buttons) != 2 {
		t.Fatalf("%+v", l)
	}
	if err := s.PutBotLook(ctx, "rep1", BotLook{}); err != nil || s.Setting(ctx, "tg_look:rep1") != "" {
		t.Fatal("an empty look clears the setting", err)
	}
}

// WARP reaches the nodes with its endpoint as an IPv4 address.
func TestPinWARPEndpoint(t *testing.T) {
	out := map[string]any{"settings": map[string]any{"peers": []any{
		map[string]any{"endpoint": "engage.cloudflareclient.com:2408"},
		map[string]any{"endpoint": "198.51.100.7:500"},
	}}}
	pinWARPEndpoint(out)
	peers := out["settings"].(map[string]any)["peers"].([]any)
	if a, b := peers[0].(map[string]any)["endpoint"], peers[1].(map[string]any)["endpoint"]; a != "162.159.192.1:2408" || b != "198.51.100.7:500" {
		t.Fatal(a, b)
	}
	pinWARPEndpoint(map[string]any{}) // nothing to do, nothing to break
}
