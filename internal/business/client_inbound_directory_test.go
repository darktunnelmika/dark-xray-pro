package business

import (
	"bytes"
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
)

func TestClientInboundDirectoryStaticScopeAndPaging(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{5}, 32)
	newRep(t, s, "filterRep")
	grant(t, s, "filterRep", "inbound-filter-credit-0001", 10000, 100)
	for _, id := range []string{"filter-a", "filter-b", "filter-c"} {
		rep := "owner"
		if id == "filter-b" {
			rep = "filterRep"
		}
		if _, e := s.CreateClient(ctx, NewClient{EventID: "inbound-filter-create-" + id, ClientID: id, RepresentativeID: rep, Name: id, DurationDays: 30, VolumeBytes: 100}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.CreateInbound(ctx, key, nativeInput()); e != nil {
		t.Fatal(e)
	}
	if e := s.BindInboundClients(ctx, 10, []string{"filter-a", "filter-b"}, true); e != nil {
		t.Fatal(e)
	}
	p, e := s.SearchClientsWithInbound(ctx, key, ClientSearch{InboundID: 10, Limit: 1, Sort: "name"})
	if e != nil || p.Total != 2 || len(p.Clients) != 1 || !p.HasMore {
		t.Fatalf("first %+v %v", p, e)
	}
	q, e := s.SearchClientsWithInbound(ctx, key, ClientSearch{InboundID: 10, Limit: 1, Offset: 1, Sort: "name"})
	if e != nil || q.Total != 2 || len(q.Clients) != 1 || q.HasMore || q.Clients[0].ID == p.Clients[0].ID {
		t.Fatalf("second %+v %v", q, e)
	}
	q, e = s.SearchClientsWithInbound(ctx, key, ClientSearch{InboundID: 10, RepresentativeID: "filterRep", Limit: 50})
	if e != nil || q.Total != 1 || q.Clients[0].ID != "filter-b" {
		t.Fatalf("scope %+v %v", q, e)
	}
	q, e = s.SearchClientsWithInbound(ctx, key, ClientSearch{InboundID: 999, Limit: 50})
	if e != nil || q.Total != 0 {
		t.Fatalf("unknown %+v %v", q, e)
	}
	choices, e := s.ClientInboundChoices(ctx, key, "filterRep")
	if e != nil || len(choices) != 1 || choices[0].Clients != 1 {
		t.Fatalf("choices %+v %v", choices, e)
	}
	choices, e = s.ClientInboundChoices(ctx, key, "nobody")
	if e != nil || len(choices) != 0 {
		t.Fatalf("foreign options %+v %v", choices, e)
	}
}

func TestClientInboundDirectoryManagedBindingsAndWrongKey(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	_, e := f.s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{EventID: "filter-managed-bind-0001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Filter test"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := f.s.SearchClientsWithInbound(ctx, f.key, ClientSearch{InboundID: 501, Limit: 50})
	if e != nil || p.Total != 1 || p.Clients[0].ID != f.client.ID {
		t.Fatalf("managed %+v %v", p, e)
	}
	choices, err := f.s.ClientInboundChoices(ctx, f.key, "")
	if err != nil || len(choices) != 1 || choices[0].ID != 501 || choices[0].Clients != 1 {
		t.Fatalf("managed options %+v %v", choices, err)
	}
	if _, e = f.s.SearchClientsWithInbound(ctx, bytes.Repeat([]byte{99}, 32), ClientSearch{InboundID: 501, Limit: 50}); e == nil {
		t.Fatal("wrong key accepted")
	}
}
