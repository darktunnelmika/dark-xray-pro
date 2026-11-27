package hubcore

import (
	"context"
	"testing"
	"time"
)

func TestDailyCapLimitsNodes(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	seed(t, s) // alice: 1000 bytes; bob: unlimited
	limit := func(email string) int64 {
		t.Helper()
		ls, err := s.limits(ctx, "de-1")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			if l.Email == email {
				return l.RemainingBytes
			}
		}
		t.Fatalf("%s not in limits", email)
		return 0
	}
	if limit("bob") != -1 || limit("alice") != 1000 {
		t.Fatal("no cap: unchanged")
	}
	if err := s.SetDailyCap(ctx, "bob", 300); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDailyCap(ctx, "alice", 5000); err != nil {
		t.Fatal(err)
	}
	if limit("bob") != 300 || limit("alice") != 1000 {
		t.Fatalf("capped: bob %d alice %d", limit("bob"), limit("alice"))
	}
	day := now.UTC().Format("2006-01-02")
	if _, err := s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('bob','de-1',?,100,250)`, day); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('bob','de-1','2026-11-26',9999,9999)`); err != nil {
		t.Fatal(err)
	}
	if limit("bob") != 0 {
		t.Fatalf("over today's cap: %d", limit("bob"))
	}
	if d, _ := s.DailyCapOf(ctx, "bob"); !d.Reached || d.UsedToday != 350 {
		t.Fatalf("%+v", d)
	}
	views, _ := s.Clients(ctx)
	for _, v := range views {
		if v.Email == "bob" && (v.DailyCapBytes != 300 || v.DailyUsed != 350) {
			t.Fatalf("view: %+v", v)
		}
	}
	// The next day the cap starts again.
	now = now.Add(24 * time.Hour)
	if limit("bob") != 300 {
		t.Fatalf("next day: %d", limit("bob"))
	}
	if err := s.SetDailyCap(ctx, "bob", 0); err != nil || limit("bob") != -1 {
		t.Fatal("cap removed")
	}
	if err := s.SetDailyCap(ctx, "nobody", 1); err == nil {
		t.Fatal("unknown client")
	}
	if capRemaining(-1, 0, 5) != -1 || capRemaining(10, 100, 95) != 5 || capRemaining(3, 100, 0) != 3 {
		t.Fatal("capRemaining")
	}
}
