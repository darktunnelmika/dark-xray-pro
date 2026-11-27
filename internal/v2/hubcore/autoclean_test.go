package hubcore

import (
	"context"
	"testing"
	"time"
)

func TestAutoCleanDeletesOnlyDeadConfigs(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	seed(t, s)
	day := int64(86400)
	n := now.Unix()
	for _, c := range []Client{
		{Email: "ended-long-ago", Enabled: true, VolumeBytes: -1, ExpiresUnix: n - 40*day, Inbounds: []string{"de-vless"}},
		{Email: "ended-recently", Enabled: true, VolumeBytes: -1, ExpiresUnix: n - 10*day, Inbounds: []string{"de-vless"}},
		{Email: "used-up-quiet", Enabled: true, VolumeBytes: 100, Inbounds: []string{"de-vless"}},
		{Email: "used-up-busy", Enabled: true, VolumeBytes: 100, Inbounds: []string{"de-vless"}},
		{Email: "never-used-waiting", Enabled: true, VolumeBytes: 1 << 30, DurationSeconds: 30 * day, Inbounds: []string{"de-vless"}},
		{Email: "rep-ended", Enabled: true, VolumeBytes: -1, ExpiresUnix: n - 40*day, Inbounds: []string{"de-vless"}},
	} {
		if err := s.PutClient(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if _, err := s.db.Exec(`UPDATE clients SET rep_id='ali' WHERE email='rep-ended'`); err != nil {
		t.Fatal(err)
	}
	for e, last := range map[string]int64{"used-up-quiet": n - 35*day, "used-up-busy": n - 2*day} {
		if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,60,60,?,?)`, e, last-day, last); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.runAutoClean(ctx); got != 0 {
		t.Fatal("off by default")
	}
	dead, _ := s.DeadClients(ctx, "", 30)
	if len(dead) != 2 {
		t.Fatalf("dead: %v", dead)
	}
	if err := s.PutAutoClean(ctx, "", AutoClean{On: true, Days: 30}); err != nil {
		t.Fatal(err)
	}
	autoCleanLast = 0
	if got := s.runAutoClean(ctx); got != 2 {
		t.Fatalf("deleted %d", got)
	}
	left := map[string]bool{}
	views, _ := s.Clients(ctx)
	for _, v := range views {
		left[v.Email] = true
	}
	for _, e := range []string{"ended-recently", "used-up-busy", "never-used-waiting", "rep-ended", "alice", "bob"} {
		if !left[e] {
			t.Fatalf("%s must stay", e)
		}
	}
	if left["ended-long-ago"] || left["used-up-quiet"] {
		t.Fatal("dead configs must go")
	}
	if a := s.AutoCleanFor(ctx, ""); a.Total != 2 || a.LastCount != 2 || a.LastRun == 0 {
		t.Fatalf("%+v", a)
	}
	// At most once an hour.
	if got := s.runAutoClean(ctx); got != 0 {
		t.Fatal("hourly")
	}
	if err := s.PutAutoClean(ctx, "ali", AutoClean{On: true, Days: 3}); err == nil {
		t.Fatal("at least 7 days")
	}
}
