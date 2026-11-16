package hubcore

import (
	"context"
	"testing"
)

func TestRenewStartsNewPeriod(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	c := Client{Email: "r1", Enabled: true, VolumeBytes: 30 << 30, ExpiresUnix: now.Unix() + 20*86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('r1',?,?,1,1)`, int64(5<<30), int64(20<<30)); err != nil {
		t.Fatal(err)
	}
	rep := Principal{User: "ali", Role: "rep", RepID: "ali"}
	res, err := s.RenewClient(ctx, rep, "r1", "", 30, 30)
	if err != nil {
		t.Fatal(err)
	}
	if res.ChargedBytes != 30<<30 || res.RefundBytes != 5<<30 {
		t.Fatalf("charge 30, refund the unused 5: %+v", res)
	}
	reps, _ := s.Reps(ctx)
	if reps[0].VolumeCredit != 45<<30 { // 100 - 30 (create) - 30 + 5
		t.Fatal("credit", reps[0].VolumeCredit>>30)
	}
	if reps[0].UsedBytes != 25<<30 {
		t.Fatal("the reseller's lifetime usage keeps the old period", reps[0].UsedBytes>>30)
	}
	list, _ := s.Clients(ctx)
	var got ClientView
	for _, x := range list {
		if x.Email == "r1" {
			got = x
		}
	}
	if got.Usage.Up+got.Usage.Down != 0 || got.VolumeBytes != 30<<30 {
		t.Fatal("usage from zero, volume is the new package", got.Usage, got.VolumeBytes)
	}
	if d := got.ExpiresUnix - now.Unix(); d != 30*86400 {
		t.Fatal("the term starts today, nothing added on top", d/86400)
	}
	// Another seller cannot renew it; the owner renews without charging.
	if _, err := s.RenewClient(ctx, Principal{User: "x", Role: "rep", RepID: "other"}, "r1", "", 10, 30); err == nil {
		t.Fatal("another seller's client is invisible")
	}
	if _, err := s.RenewClient(ctx, Principal{User: "owner", Role: "owner"}, "r1", "", 10, 30); err != nil {
		t.Fatal(err)
	}
	reps, _ = s.Reps(ctx)
	if reps[0].VolumeCredit != 45<<30 {
		t.Fatal("owner renewals are free for the reseller", reps[0].VolumeCredit>>30)
	}
	// A never-used first-connection service starts at its first connection.
	n := Client{Email: "r2", Enabled: true, VolumeBytes: 10 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", n, "volume"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenewClient(ctx, rep, "r2", "", 10, 30); err != nil {
		t.Fatal(err)
	}
	list, _ = s.Clients(ctx)
	for _, x := range list {
		if x.Email == "r2" && (x.DurationSeconds != 30*86400 || x.ExpiresUnix != 0) {
			t.Fatal("unused: 30 days from the first connection, not 60", x.DurationSeconds/86400, x.ExpiresUnix)
		}
	}
	// Not enough credit: nothing changes.
	if _, err := s.RenewClient(ctx, rep, "r1", "", 1000, 30); err == nil {
		t.Fatal("over credit must fail")
	}
}

func TestSubSellerBrandIsTheirOwn(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "boss", Name: "Boss", Enabled: true})
	_ = s.PutRep(ctx, Rep{ID: "kid", Name: "Kid", Enabled: true, ParentID: "boss"})
	_ = s.AdjustCredit(ctx, "owner", "kid", 50<<30, 0, "")
	if err := s.PutRepBrand(ctx, "boss", RepBrand{Title: "Boss VPN", SupportURL: "https://t.me/boss"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepBrand(ctx, "kid", RepBrand{Title: "Kid Shop", Telegram: "@kid_shop"}); err != nil {
		t.Fatal(err)
	}
	c := Client{Email: "k1", Enabled: true, VolumeBytes: 1 << 30, ExpiresUnix: 0, DurationSeconds: 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "kid", "kid", c, "volume"); err != nil {
		t.Fatal(err)
	}
	set, _ := s.brandFor(ctx, "k1")
	if set.Title != "Kid Shop" || set.SupportURL != "https://t.me/kid_shop" {
		t.Fatalf("sub-seller's own brand and chat: %+v", set)
	}
	if !s.IsSubSeller(ctx, "kid") || s.IsSubSeller(ctx, "boss") {
		t.Fatal("sub-seller detection")
	}
	if err := s.PutRepBrand(ctx, "kid", RepBrand{Telegram: "bad id!"}); err == nil {
		t.Fatal("a bad Telegram id is refused")
	}
}
