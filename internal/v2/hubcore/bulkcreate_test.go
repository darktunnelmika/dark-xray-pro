package hubcore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBulkCreateAndOnHold(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutGroup(ctx, Group{ID: "shop", Name: "Shop"}); err != nil {
		t.Fatal(err)
	}
	emails, err := s.BulkCreateClients(ctx, BulkCreate{Prefix: "shop-", Start: 1, Count: 12, Pad: 3, Volume: 30 << 30, Days: 30, FirstUse: true, OnHoldDays: 7,
		MaxIPs: 2, Inbounds: []string{"de-vless"}, Group: "shop", Note: "batch A"})
	if err != nil || len(emails) != 12 || emails[0] != "shop-001" || emails[11] != "shop-012" {
		t.Fatal(emails, err)
	}
	if _, err := s.BulkCreateClients(ctx, BulkCreate{Prefix: "shop-", Start: 12, Count: 3, Pad: 3, Days: 30}); !errors.Is(err, ErrInvalid) {
		t.Fatal("an existing name must stop the whole batch")
	}
	all, _ := s.Clients(ctx)
	count, onhold := 0, 0
	for _, c := range all {
		if len(c.Email) > 5 && c.Email[:5] == "shop-" {
			count++
			if c.StartBy > 0 && c.Note == "batch A" && c.DurationSeconds == 30*86400 && len(c.Inbounds) == 1 {
				onhold++
			}
		}
	}
	if count != 12 || onhold != 12 {
		t.Fatalf("created %d, on hold %d (the failed batch must leave nothing)", count, onhold)
	}
	groups, _ := s.Groups(ctx)
	for _, g := range groups {
		if g.ID == "shop" && len(g.Members) != 12 {
			t.Fatal("group members", g.Members)
		}
	}
	// shop-001 connects in time; shop-002 never does.
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('shop-001',1,1,?,?)`, now.Add(24*time.Hour).Unix(), now.Add(24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	start := now.Unix()
	*now = now.Add(8 * 24 * time.Hour)
	if n := s.runOnHold(ctx); n != 11 {
		t.Fatalf("started %d", n)
	}
	c1, _ := s.ClientByEmail(ctx, "shop-001")
	c2, _ := s.ClientByEmail(ctx, "shop-002")
	if c1.ExpiresUnix != 0 || c1.StartBy != 0 {
		t.Fatalf("a used client keeps its first-use term: %+v", c1.Client)
	}
	if c2.ExpiresUnix != start+7*86400+30*86400 {
		t.Fatalf("unused client must start at the on-hold deadline: %d", c2.ExpiresUnix)
	}
	if err := s.SetOnHold(ctx, "nobody", 3); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
