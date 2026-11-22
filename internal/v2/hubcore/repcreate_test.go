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
