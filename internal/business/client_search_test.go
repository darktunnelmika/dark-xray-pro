package business

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSearchClientsGlobalScopedAndStableRealPagination(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "privateRep")
	newRep(t, s, "otherRep")
	grant(t, s, "privateRep", "search-credit-000000001", 20000, 2000)
	grant(t, s, "otherRep", "search-credit-000000002", 20000, 2000)
	for i := 0; i < 126; i++ {
		owner := "owner"
		if i >= 120 && i < 123 {
			owner = "privateRep"
		}
		if i >= 123 {
			owner = "otherRep"
		}
		name := fmt.Sprintf("Customer %03d", i)
		if i == 120 {
			name = "Alpha_PERCENT%_ literal"
		}
		if i == 121 {
			name = "نام خصوصی تست"
		}
		if i == 123 {
			name = "Other rep secret customer"
		}
		c := NewClient{
			EventID: fmt.Sprintf("search-client-create-%05d", i), ClientID: fmt.Sprintf("search-client-%03d", i),
			RepresentativeID: owner, Name: name, IPCount: 0,
			DurationDays: 30, VolumeBytes: 100,
		}
		if _, err := s.CreateClient(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	total, err := s.SearchClients(ctx, ClientSearch{Limit: 200, Sort: "name"})
	if err != nil || total.Total != 126 || len(total.Clients) != 126 || total.HasMore {
		t.Fatalf("global count invalid %+v %v", total, err)
	}
	first, err := s.SearchClients(ctx, ClientSearch{Limit: 25})
	if err != nil || first.Total != 126 || len(first.Clients) != 25 || !first.HasMore {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := s.SearchClients(ctx, ClientSearch{Limit: 25, Offset: 25})
	if err != nil || len(second.Clients) != 25 || second.Total != 126 || second.Clients[0].ID == first.Clients[0].ID {
		t.Fatalf("unstable or incomplete pagination: %+v %v", second, err)
	}
	owner, err := s.SearchClients(ctx, ClientSearch{RepresentativeID: "owner", Limit: 200})
	if err != nil || owner.Total != 120 || len(owner.Clients) != 120 {
		t.Fatalf("owner scope: %+v %v", owner, err)
	}
	rep, err := s.SearchClients(ctx, ClientSearch{RepresentativeID: "privateRep", Limit: 200, Sort: "name"})
	if err != nil || rep.Total != 3 || len(rep.Clients) != 3 {
		t.Fatalf("representative scope: %+v %v", rep, err)
	}
	for _, item := range rep.Clients {
		if item.RepresentativeID != "privateRep" || strings.Contains(item.Name, "Other rep secret") {
			t.Fatalf("cross-rep client exposed: %+v", item)
		}
	}
	matched, err := s.SearchClients(ctx, ClientSearch{RepresentativeID: "privateRep", Query: "PERCENT%_", Limit: 25})
	if err != nil || matched.Total != 1 || matched.Clients[0].ID != "search-client-120" {
		t.Fatalf("literal metacharacter search failed: %+v %v", matched, err)
	}
	notWild, err := s.SearchClients(ctx, ClientSearch{RepresentativeID: "privateRep", Query: "%does-not-exist", Limit: 25})
	if err != nil || notWild.Total != 0 {
		t.Fatalf("SQL metacharacter unexpectedly treated as wildcard %+v %v", notWild, err)
	}
	foreign, err := s.SearchClients(ctx, ClientSearch{RepresentativeID: "privateRep", Query: "secret", Limit: 25})
	if err != nil || foreign.Total != 0 || len(foreign.Clients) != 0 {
		t.Fatalf("name leaked across scoped query: %+v %v", foreign, err)
	}
	persian, err := s.SearchClients(ctx, ClientSearch{Query: "خصوصی", Limit: 25})
	if err != nil || persian.Total != 1 {
		t.Fatalf("Persian substring search failed %+v %v", persian, err)
	}
	sorted, err := s.SearchClients(ctx, ClientSearch{Sort: "expiry_late", Limit: 100, RepresentativeID: "owner"})
	if err != nil || sorted.Total != 120 || len(sorted.Clients) != 100 || !sorted.HasMore {
		t.Fatalf("sorted page %+v %v", sorted, err)
	}
	more, err := s.SearchClients(ctx, ClientSearch{Sort: "expiry_late", Limit: 100, Offset: 100, RepresentativeID: "owner"})
	if err != nil || len(more.Clients) != 20 || more.HasMore {
		t.Fatalf("second 100 page %+v %v", more, err)
	}

	if _, err = s.SetClientState(ctx, SetClientState{
		EventID: "search-disable-00000001", ClientID: "search-client-120", Action: "disable", Reason: "test disabled",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetClientState(ctx, SetClientState{
		EventID: "search-archive-00000001", ClientID: "search-client-122", Action: "archive", Reason: "test archived",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE clients SET expires_unix=? WHERE id=?", time.Now().Unix()-5, "search-client-121"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		state string
		id    string
	}{
		{"disabled", "search-client-120"}, {"expired", "search-client-121"}, {"archived", "search-client-122"},
	} {
		data, e := s.SearchClients(ctx, ClientSearch{RepresentativeID: "privateRep", State: tc.state, Limit: 25})
		if e != nil || data.Total != 1 || data.Clients[0].ID != tc.id {
			t.Fatalf("state=%s %+v %v", tc.state, data, e)
		}
	}
	available, err := s.SearchClients(ctx, ClientSearch{RepresentativeID: "privateRep", State: "pending", Limit: 25})
	if err != nil || available.Total != 0 {
		t.Fatalf("inactive clients treated as pending %+v %v", available, err)
	}
	_, err = s.SearchClients(ctx, ClientSearch{Query: "%", State: "bad';drop table clients;--", Limit: 25})
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("untrusted status sort admitted", err)
	}
	for _, bad := range []ClientSearch{
		{Limit: 0}, {Limit: 201}, {Limit: 50, Offset: -1}, {Limit: 50, Offset: 1_000_001},
		{Limit: 25, Sort: "id;DROP TABLE clients"}, {Limit: 25, Query: strings.Repeat("x", 129)},
		{Limit: 25, Query: "line\nfeed"}, {Limit: 25, RepresentativeID: "owner OR 1=1"},
		{Limit: 25, Plan: "free"}, {Limit: 25, State: "enabled"},
	} {
		if _, e := s.SearchClients(ctx, bad); !errors.Is(e, ErrInvalid) {
			t.Fatalf("unsafe search options accepted: %+v error=%v", bad, e)
		}
	}
	final, err := s.SearchClients(ctx, ClientSearch{Limit: 25})
	if err != nil || final.Total != 126 {
		t.Fatal("rejected search changed client history", err)
	}
}
