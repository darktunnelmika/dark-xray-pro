package hubcore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTrialsPerSellerAreMarkedAndPurged(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 10<<30, 0, "")
	const chat = int64(777)
	credit := func() int64 {
		var v int64
		_ = s.db.QueryRow(`SELECT volume_credit FROM reps WHERE id='ali'`).Scan(&v)
		return v
	}

	// Each seller has its own switch; all are off at first.
	if s.TrialPolicyFor(ctx, "ali").Enabled || s.TrialPolicy(ctx).Enabled {
		t.Fatal("trials start off")
	}
	if _, err := s.ClaimTrial(ctx, "ali", chat); !errors.Is(err, ErrNotFound) {
		t.Fatal("a reseller's trial while off", err)
	}
	if err := s.PutTrialPolicyFor(ctx, "ali", TrialPolicy{Enabled: true, GB: 0, Hours: 24}); err == nil {
		t.Fatal("a reseller's trial needs a volume")
	}
	if err := s.PutTrialPolicyFor(ctx, "ali", TrialPolicy{Enabled: true, GB: 2, Hours: 48, MaxIPs: 1}); err != nil {
		t.Fatal(err)
	}
	if s.TrialPolicy(ctx).Enabled {
		t.Fatal("a reseller's switch must not turn the owner's trial on")
	}
	email, err := s.ClaimTrial(ctx, "ali", chat)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ClientByEmail(ctx, email)
	if err != nil || !c.Trial || c.RepID != "ali" || c.VolumeBytes != 2<<30 {
		t.Fatalf("the trial is the reseller's own marked client: %+v %v", c, err)
	}
	if got := credit(); got != 8<<30 {
		t.Fatalf("charged to the reseller: %d", got)
	}
	if _, err := s.ClaimTrial(ctx, "ali", chat); !errors.Is(err, ErrTrialUsed) {
		t.Fatal("once per account and seller", err)
	}
	if credit() != 8<<30 {
		t.Fatal("a refused second trial must not charge")
	}
	// The same Telegram account may still try the owner's shop.
	if err := s.PutTrialPolicy(ctx, TrialPolicy{Enabled: true, GB: 1, Hours: 24, MaxIPs: 1}); err != nil {
		t.Fatal(err)
	}
	own, err := s.ClaimTrial(ctx, "", chat)
	if err != nil || own == email {
		t.Fatal(own, err)
	}
	// A rename keeps the mark; a renewal makes it a paying client.
	if err := s.RenameClient(ctx, own, "reza-phone"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.ClientByEmail(ctx, "reza-phone"); !c.Trial {
		t.Fatal("the mark survives a rename")
	}
	if !s.TrialUsed(ctx, "", chat) {
		t.Fatal("the claim survives a rename")
	}
	if _, err := s.RenewClient(ctx, Principal{User: "owner", Role: "owner"}, "reza-phone", "volume", 5, 30); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.ClientByEmail(ctx, "reza-phone"); c.Trial {
		t.Fatal("a renewed trial is a paying client")
	}
	// Ended trials are removed after a few days; the claim stays.
	*now = now.Add(48*time.Hour + trialKeepDays*24*time.Hour + time.Hour)
	if n := s.PurgeTrials(ctx); n != 1 {
		t.Fatalf("purged %d, want the reseller's ended trial", n)
	}
	if _, err := s.ClientByEmail(ctx, email); err == nil {
		t.Fatal("the ended trial must be gone")
	}
	if _, err := s.ClientByEmail(ctx, "reza-phone"); err != nil {
		t.Fatal("a renewed client is never purged")
	}
	if _, err := s.ClaimTrial(ctx, "ali", chat); !errors.Is(err, ErrTrialUsed) {
		t.Fatal("no second trial after the purge", err)
	}
}
