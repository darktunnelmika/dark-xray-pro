package hubcore

import (
	"context"
	"errors"
	"testing"
)

func TestRepGrowth(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{10}}); err != nil {
		t.Fatal(err)
	}
	// Invite link: the opener becomes ali's sub-seller with a login.
	code, err := s.NewSellerInvite(ctx, "ali")
	if err != nil {
		t.Fatal(err)
	}
	np, err := s.ClaimSellerInvite(ctx, code, 20, "Sara K", "sara_k")
	if err != nil || np.Parent != "ali" || np.RepID == "" || len(np.Password) < 10 {
		t.Fatalf("claim: %+v %v", np, err)
	}
	if !s.IsSellerOf(ctx, "ali", np.RepID) {
		t.Fatal("not a sub-seller")
	}
	if rep, role := s.TgRepRole(ctx, 20); rep != np.RepID || role != "owner" {
		t.Fatalf("telegram owner: %s %s", rep, role)
	}
	if _, _, err := s.verifyPassword(ctx, np.Username, np.Password); err != nil {
		t.Fatal("login")
	}
	// The same chat cannot take a second panel; a sub-seller cannot invite.
	if _, err := s.ClaimSellerInvite(ctx, code, 20, "Sara", "sara_k"); err == nil {
		t.Fatal("second panel")
	}
	if _, err := s.NewSellerInvite(ctx, np.RepID); err == nil {
		t.Fatal("sub-seller invite")
	}
	if _, err := s.ClaimSellerInvite(ctx, "nope", 21, "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("bad code")
	}
	// Requests: off by default; on for ali; accepting makes a sub-seller.
	if ok, err := s.AddRepRequest(ctx, "ali", 30, "Reza", ""); err == nil || ok {
		t.Fatal("requests are off by default")
	}
	_ = s.SetRepRequestsOn(ctx, "ali", true)
	if err := s.SetRepRequestsOn(ctx, np.RepID, true); err == nil {
		t.Fatal("a sub-seller has no request button")
	}
	if ok, err := s.AddRepRequest(ctx, "ali", 30, "Reza", ""); err != nil || !ok {
		t.Fatalf("request: %v", err)
	}
	if ok, _ := s.AddRepRequest(ctx, "ali", 30, "Reza", ""); ok {
		t.Fatal("asked twice")
	}
	if np2, err := s.DecideRepRequest(ctx, "ali", 30, true, "tg:10"); err != nil || !s.IsSellerOf(ctx, "ali", np2.RepID) {
		t.Fatalf("accept: %+v %v", np2, err)
	}
	if _, err := s.DecideRepRequest(ctx, "ali", 30, true, "tg:10"); !errors.Is(err, ErrNotFound) {
		t.Fatal("decided twice")
	}
	// The owner's requests make top-level resellers; refusing makes none.
	_ = s.SetRepRequestsOn(ctx, "", true)
	_, _ = s.AddRepRequest(ctx, "", 40, "Mina", "mina")
	if np3, err := s.DecideRepRequest(ctx, "", 40, true, "tg:1"); err != nil || np3.Parent != "" || s.IsSubSeller(ctx, np3.RepID) {
		t.Fatalf("owner accept: %+v %v", np3, err)
	}
	_, _ = s.AddRepRequest(ctx, "", 41, "No", "")
	if np4, err := s.DecideRepRequest(ctx, "", 41, false, "tg:1"); err != nil || np4.RepID != "" {
		t.Fatal("refuse")
	}
	if rep, _ := s.TgRepRole(ctx, 41); rep != "" {
		t.Fatal("refused chat got a panel")
	}
	// Trial panel: off by default, once per chat, with its credit.
	if _, err := s.ClaimRepTrial(ctx, 50, "T", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("trial off by default")
	}
	if err := s.PutRepTrialPolicy(ctx, RepTrial{On: true, GB: 500}); err == nil {
		t.Fatal("at most 100 GB")
	}
	_ = s.PutRepTrialPolicy(ctx, RepTrial{On: true, GB: 3})
	tp, err := s.ClaimRepTrial(ctx, 50, "Tester", "tester")
	if err != nil || !s.IsTrialPanel(ctx, tp.RepID) {
		t.Fatalf("trial: %+v %v", tp, err)
	}
	reps, _ := s.Reps(ctx)
	for _, r := range reps {
		if r.ID == tp.RepID && r.VolumeCredit != 3<<30 {
			t.Fatalf("trial credit %d", r.VolumeCredit)
		}
	}
	if _, err := s.ClaimRepTrial(ctx, 50, "Tester", "tester"); !errors.Is(err, ErrTrialUsed) {
		t.Fatal("once per chat")
	}
}
