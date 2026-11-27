package hubcore

import (
	"context"
	"testing"
)

func TestSubDomainsDefaultPickAndMove(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.PutRep(ctx, Rep{ID: "sara", Name: "Sara", Enabled: true})
	if s.defaultBase(ctx, "https://hub.example.com") != "https://hub.example.com" {
		t.Fatal("no list: the configured address")
	}
	if err := s.PutSubDomains(ctx, []SubDomain{{URL: "A.example.com:2096", Label: "a", Default: true}, {URL: "https://b.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if s.defaultBase(ctx, "https://hub.example.com") != "https://a.example.com:2096" {
		t.Fatalf("default: %s", s.defaultBase(ctx, "x"))
	}
	if s.SubBaseFor(ctx, "alice", "https://hub.example.com") != "https://a.example.com:2096" {
		t.Fatal("owner's clients use the default")
	}
	for _, bad := range [][]SubDomain{
		{{URL: "ftp://x.com"}},
		{{URL: "https://x.com/path"}},
		{{URL: "x.com", Default: true}, {URL: "y.com", Default: true}},
		{{URL: "x.com", Default: true, Retired: true}},
		{{URL: "x.com"}, {URL: "https://X.com"}},
	} {
		if err := s.PutSubDomains(ctx, bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := s.PickRepDomain(ctx, "ali", "https://b.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.PickRepDomain(ctx, "sara", "https://evil.example.com"); err == nil {
		t.Fatal("only listed addresses")
	}
	if _, err := s.db.Exec(`UPDATE clients SET rep_id='ali' WHERE email='bob'`); err != nil {
		t.Fatal(err)
	}
	if s.SubBaseFor(ctx, "bob", "x") != "https://b.example.com" {
		t.Fatal("the reseller's pick")
	}
	if d := s.SubDomains(ctx); d[1].Reps != 1 {
		t.Fatalf("use count: %+v", d)
	}
	// b gets filtered: move everyone on b (and the default if it were b) to a.
	n, err := s.MoveSubDomain(ctx, "b.example.com", "a.example.com:2096")
	if err != nil || n != 1 {
		t.Fatalf("move: %d %v", n, err)
	}
	if s.SubBaseFor(ctx, "bob", "x") != "https://a.example.com:2096" {
		t.Fatal("moved")
	}
	d := s.SubDomains(ctx)
	if !d[1].Retired || d[0].Reps != 1 {
		t.Fatalf("after move: %+v", d)
	}
	if len(s.SubDomainChoices(ctx)) != 1 {
		t.Fatal("a retired address is not offered")
	}
	// Moving the default carries the default along.
	_ = s.PutSubDomains(ctx, []SubDomain{{URL: "a.example.com:2096", Default: true}, {URL: "c.example.com"}})
	if _, err := s.MoveSubDomain(ctx, "a.example.com:2096", "c.example.com"); err != nil {
		t.Fatal(err)
	}
	if s.defaultBase(ctx, "x") != "https://c.example.com" {
		t.Fatalf("default moved: %s", s.defaultBase(ctx, "x"))
	}
	if _, err := s.MoveSubDomain(ctx, "c.example.com", "nowhere.example.com"); err == nil {
		t.Fatal("target must be listed")
	}
}
