package hubcore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnalytics(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s) // alice, bob: no end date
	day := int64(86400)
	put := func(email string, expires int64) {
		if err := s.PutClient(ctx, Client{Email: email, Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: expires, Inbounds: []string{"de-vless"}}); err != nil {
			t.Fatal(err)
		}
	}
	n := now.Unix()
	put("soon", n+3*day)    // ends in 3 days: at risk
	put("gone", n-10*day)   // ended 10 days ago: lapsed, churned
	put("renewer", n-1*day) // ended yesterday, then renewed below
	if _, err := s.RenewClient(ctx, Principal{User: "o", Role: "owner"}, "renewer", "volume", 10, 30); err != nil {
		t.Fatal(err)
	}
	// A reseller's customer, outside the reseller view of others.
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 100<<30, 0, "")
	if err := s.RepPutClient(ctx, "ali", "ali", Client{Email: "ali-c", Enabled: true, VolumeBytes: 5 << 30, DurationSeconds: 2 * day, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Analytics(ctx, Principal{Role: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Total != 6 || a.Active != 5 || a.Renewed30d != 1 || a.Lapsed30d != 1 || a.RenewRate != 0.5 {
		t.Fatalf("totals: %+v", a)
	}
	if a.Expiring7d < 1 || len(a.Months) != 6 || len(a.Daily) != 30 {
		t.Fatalf("shape: exp %d months %d daily %d", a.Expiring7d, len(a.Months), len(a.Daily))
	}
	risk := map[string]bool{}
	for _, r := range a.AtRisk {
		risk[r.Email] = true
	}
	if !risk["soon"] || risk["gone"] || risk["alice"] {
		t.Fatalf("at risk: %+v", a.AtRisk)
	}
	var churned, renewed int64
	for _, m := range a.Months {
		churned += m.Churned
		renewed += m.Renewed
	}
	if churned != 1 || renewed != 1 {
		t.Fatalf("months: %+v", a.Months)
	}
	// The reseller sees only its customer.
	r, err := s.Analytics(ctx, Principal{Role: "rep", RepID: "ali"}, "")
	if err != nil || r.Total != 1 {
		t.Fatalf("reseller view: %+v %v", r, err)
	}
	if _, err := s.Analytics(ctx, Principal{Role: "viewer"}, ""); err == nil {
		t.Fatal("no role, no report")
	}
	// Reminders: only with Telegram, at most twice a day, never for others' customers.
	if _, _, err := s.RemindRenewal(ctx, Principal{Role: "owner"}, "soon"); !errors.Is(err, ErrInvalid) {
		t.Fatal("reminded without Telegram", err)
	}
	if err := s.TgLink(ctx, 555, "soon"); err != nil {
		t.Fatal(err)
	}
	if _, chats, err := s.RemindRenewal(ctx, Principal{Role: "owner"}, "soon"); err != nil || len(chats) != 1 {
		t.Fatal(err, chats)
	}
	if _, _, err := s.RemindRenewal(ctx, Principal{Role: "owner"}, "soon"); err == nil {
		t.Fatal("reminded twice")
	}
	if _, _, err := s.RemindRenewal(ctx, Principal{Role: "rep", RepID: "ali"}, "soon"); !errors.Is(err, ErrNotFound) {
		t.Fatal("reseller reminded the owner's customer", err)
	}
	p := s.PendingReminders(ctx)
	if len(p) != 1 || p[0].Email != "soon" || p[0].Seller != "" {
		t.Fatalf("pending: %+v", p)
	}
	s.ReminderSent(ctx, p[0].ID)
	if len(s.PendingReminders(ctx)) != 0 {
		t.Fatal("sent once")
	}
	*now = now.Add(13 * time.Hour)
	if _, _, err := s.RemindRenewal(ctx, Principal{Role: "owner"}, "soon"); err != nil {
		t.Fatal("next day reminder", err)
	}
}
