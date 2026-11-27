package hubcore

import (
	"context"
	"testing"
	"time"
)

func TestRepForecastAndRunningOut(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now.Add(-30 * 24 * time.Hour) }
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	s.now = func() time.Time { return now }
	if f := s.RepForecast(ctx, "ali", 100<<30); f.DaysLeft != -1 || f.Package != nil {
		t.Fatalf("not selling: %+v", f)
	}
	// 70 GB charged and 7 GB back over the week: 9 GB a day.
	for i := 0; i < 7; i++ {
		u := now.Add(-time.Duration(i)*24*time.Hour - time.Hour).Unix()
		if _, err := s.db.Exec(`INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,0,'')`, u, "ali", "ali", "charge", -(10 << 30)); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = s.db.Exec(`INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,0,'')`, now.Add(-2*time.Hour).Unix(), "ali", "x", "refund", 7<<30)
	// An old charge (outside the week) does not count.
	_, _ = s.db.Exec(`INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,0,'')`, now.Add(-20*24*time.Hour).Unix(), "ali", "x", "charge", -(500 << 30))
	if err := s.PutCard(ctx, CardInfo{Number: "6037991122334455"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, Packages: []RepPackage{{ID: "s", Name: "S", GB: 100, Price: 1}, {ID: "m", Name: "M", GB: 300, Price: 2}, {ID: "l", Name: "L", GB: 1000, Price: 3}}}); err != nil {
		t.Fatal(err)
	}
	f := s.RepForecast(ctx, "ali", 18<<30)
	if f.DailyBytes != 9<<30 || f.DaysLeft != 2 || f.Package == nil || f.Package.ID != "m" {
		t.Fatalf("%+v %+v", f, f.Package)
	}
	if p := pickPackage([]RepPackage{{ID: "a", GB: 10}, {ID: "b", GB: 20}}, 100<<30); p.ID != "b" {
		t.Fatal("biggest when none covers")
	}
	// Running out: credit for 2 days → warned once.
	if err := s.AdjustCredit(ctx, "owner", "ali", 18<<30, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.DueRunningOut(ctx, "ali"); !ok {
		t.Fatal("due")
	}
	if _, ok := s.DueRunningOut(ctx, "ali"); ok {
		t.Fatal("once")
	}
	// Back over 5 days, then down again: warned again.
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	if _, ok := s.DueRunningOut(ctx, "ali"); ok {
		t.Fatal("plenty")
	}
	_ = s.AdjustCredit(ctx, "owner", "ali", -(100 << 30), 0, "")
	if _, ok := s.DueRunningOut(ctx, "ali"); !ok {
		t.Fatal("due again")
	}
}
