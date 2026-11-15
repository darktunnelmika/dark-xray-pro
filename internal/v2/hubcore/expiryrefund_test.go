package hubcore

import (
	"context"
	"testing"
	"time"
)

func TestExpiryRefund(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	c := Client{Email: "r1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('r1',?,?,1,1)`, int64(1<<30), int64(2<<30)); err != nil {
		t.Fatal(err)
	}
	credit := func() int64 {
		reps, _ := s.Reps(ctx)
		return reps[0].VolumeCredit
	}
	if n := s.runExpiryRefunds(ctx); n != 0 || credit() != 90<<30 {
		t.Fatal("nothing back before expiry", n, credit())
	}
	*now = now.Add(86400*time.Second + 5*time.Minute)
	if n := s.runExpiryRefunds(ctx); n != 0 {
		t.Fatal("a short grace leaves time for a renewal", n)
	}
	*now = now.Add(10 * time.Minute)
	if n := s.runExpiryRefunds(ctx); n != 1 || credit() != 97<<30 {
		t.Fatal("unused 7 GB back after expiry", n, credit())
	}
	if n := s.runExpiryRefunds(ctx); n != 0 || credit() != 97<<30 {
		t.Fatal("refunded once only", n, credit())
	}
	// Renewing with days only gives nothing new; adding volume is paid.
	c.ExpiresUnix = now.Unix() + 30*86400
	c.VolumeBytes = (3 << 30) + (10 << 30)
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	if credit() != 87<<30 {
		t.Fatal("renewal charges the new volume", credit())
	}
	// The owner's own clients are never touched.
	if err := s.PutClient(ctx, Client{Email: "own", Enabled: true, VolumeBytes: 5 << 30, ExpiresUnix: now.Unix() - 86400}); err != nil {
		t.Fatal(err)
	}
	if n := s.runExpiryRefunds(ctx); n != 0 {
		t.Fatal(n)
	}
	// Deleting keeps the reseller's lifetime usage.
	if _, _, err := s.RepDeleteClient(ctx, "ali", "ali", "r1"); err != nil {
		t.Fatal(err)
	}
	if reps, _ := s.Reps(ctx); reps[0].UsedBytes != 3<<30 {
		t.Fatal("lifetime usage", reps[0].UsedBytes)
	}
}

func TestDeleteRep(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	if err := s.SetRepLogin(ctx, "ali", "pass-12345678", "ali"); err != nil {
		t.Fatal(err)
	}
	c := Client{Email: "r1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRep(ctx, "ali", false); err == nil {
		t.Fatal("a reseller with clients needs withClients")
	}
	if n, err := s.DeleteRep(ctx, "ali", true); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if reps, _ := s.Reps(ctx); len(reps) != 0 {
		t.Fatal(reps)
	}
	if list, _ := s.Clients(ctx); len(list) != 0 {
		t.Fatal("clients gone", list)
	}
	if _, err := s.Login(ctx, "ali", "pass-12345678"); err == nil {
		t.Fatal("login gone")
	}
}
