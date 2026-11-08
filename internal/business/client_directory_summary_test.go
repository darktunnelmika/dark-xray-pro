package business

import (
	"bytes"
	"context"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"strings"
	"testing"
)

func TestClientDirectorySummaryScopeKnownBudgetAndNoMutation(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{5}, 32)
	newRep(t, s, "summary-rep")
	if _, e := s.db.ExecContext(ctx, "UPDATE representatives SET display_name=? WHERE id=?", "Summary Seller", "summary-rep"); e != nil {
		t.Fatal(e)
	}
	grant(t, s, "summary-rep", "summary-rep-credit-0001", 10000, 10)
	create := func(id, rep string) Client {
		c, e := s.CreateClient(ctx, NewClient{EventID: "summary-create-" + id, ClientID: id, RepresentativeID: rep, Name: id, DurationDays: 30, VolumeBytes: 1000})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	old := create("old-summary", "owner")
	if e := s.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	c := create("known-summary", "summary-rep")
	if _, e := s.db.ExecContext(ctx, "UPDATE clients SET deployment_state='disabled' WHERE id=?", c.ID); e != nil {
		t.Fatal(e)
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.usage.EnableHistoricalBudgetsTx(ctx, tx); e != nil {
		t.Fatal(e)
	}
	if e = s.usage.ReserveHistoricalBudgetTx(ctx, tx, usageledger.HistoricalBudget{ClientID: c.ID, HistoricalBytes: 700, ReceiptSHA: strings.Repeat("a", 64)}); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.ExecContext(ctx, "UPDATE clients SET deployment_state='pending_node_apply' WHERE id=?", c.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateInbound(ctx, key, nativeInput()); e != nil {
		t.Fatal(e)
	}
	if e = s.BindInboundClients(ctx, 10, []string{c.ID, old.ID}, true); e != nil {
		t.Fatal(e)
	}
	before, e := s.ClientUsage(ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.SearchClientsWithInbound(ctx, key, ClientSearch{Summary: true, RepresentativeID: "summary-rep", Limit: 50})
	if e != nil {
		t.Fatal(e)
	}
	if p.Total != 1 || len(p.Clients) != 1 || p.Clients[0].ID != c.ID {
		t.Fatal("scope", p)
	}
	d := p.Clients[0].Directory
	if d == nil || d.Representative == nil || d.Representative.ID != "summary-rep" || d.Representative.DisplayName != "Summary Seller" {
		t.Fatal("representative label scope", d)
	}
	if d == nil || len(d.Inbounds) != 1 || d.Inbounds[0].ID != 10 || d.Usage == nil || *d.Usage != before || !d.Usage.Known || d.Usage.RemainingNativeBudgetBytes != 300 {
		t.Fatal("summary", d)
	}
	p, e = s.SearchClientsWithInbound(ctx, key, ClientSearch{Summary: true, Query: old.ID, Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if p.Clients[0].Directory.Usage.Known || p.Clients[0].Directory.Usage.RemainingNativeBudgetKnown {
		t.Fatal("invented baseline")
	}
	after, e := s.ClientUsage(ctx, c.ID)
	if e != nil || after != before {
		t.Fatal("read changed financial values", e)
	}
	p, e = s.SearchClientsWithInbound(ctx, key, ClientSearch{Limit: 1})
	if e != nil || p.Clients[0].Directory != nil {
		t.Fatal("summary must be opt-in", e)
	}
}
func TestClientDirectorySummaryDisabledLedgerAndManagedKey(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	if _, e := s.CreateClient(ctx, NewClient{EventID: "summary-disabled-create-0001", ClientID: "summary-disabled", RepresentativeID: "owner", Name: "Summary", DurationDays: 30, VolumeBytes: 100}); e != nil {
		t.Fatal(e)
	}
	p, e := s.SearchClientsWithInbound(ctx, nil, ClientSearch{Summary: true, Limit: 1})
	if e != nil || p.Clients[0].Directory.Usage != nil {
		t.Fatal("disabled ledger reported counters", e)
	}
}
