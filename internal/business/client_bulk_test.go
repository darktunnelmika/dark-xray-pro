package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestAtomicBulkClientStateIdempotencyScopesAndPreservedLedger(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "bulkA")
	newRep(t, s, "bulkB")
	grant(t, s, "bulkA", "bulk-allocation-00000001", 10000, 100)
	grant(t, s, "bulkB", "bulk-allocation-00000002", 10000, 100)
	for i, item := range []struct{ id, rep string }{
		{"bulk-a1", "bulkA"}, {"bulk-a2", "bulkA"},
		{"bulk-a3", "bulkA"}, {"bulk-b1", "bulkB"},
	} {
		if _, err := s.CreateClient(ctx, NewClient{
			EventID: fmt.Sprintf("bulk-created-client-%03d", i), ClientID: item.id,
			RepresentativeID: item.rep, Name: "Test " + item.id, DurationDays: 30,
			VolumeBytes: 100, IPCount: 0,
		}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := s.Representative(ctx, "bulkA")
	if err != nil {
		t.Fatal(err)
	}
	client, err := s.Client(ctx, "bulk-a1")
	if err != nil {
		t.Fatal(err)
	}
	oldUUID := client.UUID
	ops, _ := s.ListClientOperations(ctx, "", 200, 0)
	if len(ops) != 0 {
		t.Fatal("preexisting client ops unexpected")
	}
	foreign := BulkClientState{EventID: "bulk-disable-event-000001", ClientIDs: []string{"bulk-a1", "bulk-b1"}, Action: "disable", Reason: "private test"}
	if _, err = s.BulkSetClientState(ctx, foreign, "bulkA"); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-tenant bulk partially succeeded: %v", err)
	}
	c, err := s.Client(ctx, "bulk-a1")
	if err != nil || c.DeploymentState != "pending_node_apply" {
		t.Fatal("atomic failed scope check changed valid client", err)
	}
	request := BulkClientState{EventID: "bulk-disable-event-000001",
		ClientIDs: []string{"bulk-a2", "bulk-a1"}, Action: "disable", Reason: "operator bulk disable"}
	result, err := s.BulkSetClientState(ctx, request, "bulkA")
	if err != nil || result.Count != 2 || result.Replayed {
		t.Fatalf("first atomic disable %+v %v", result, err)
	}
	for _, id := range []string{"bulk-a1", "bulk-a2"} {
		c, e := s.Client(ctx, id)
		if e != nil || c.DeploymentState != "disabled" || c.Archived {
			t.Fatal("incomplete bulk disable", id, e)
		}
	}
	request.ClientIDs = []string{"bulk-a1", "bulk-a2"} // order must not affect idempotency
	again, err := s.BulkSetClientState(ctx, request, "bulkA")
	if err != nil || !again.Replayed || again.Count != 2 {
		t.Fatalf("identical retry double-mutated: %+v %v", again, err)
	}
	changed := request
	changed.ClientIDs = []string{"bulk-a1", "bulk-a3"}
	if _, err = s.BulkSetClientState(ctx, changed, "bulkA"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same batch nonce applied a DIFFERENT selection: %v", err)
	}
	c, e := s.Client(ctx, "bulk-a3")
	if e != nil || c.DeploymentState != "pending_node_apply" {
		t.Fatal("changed bulk nonce affected third client", e)
	}
	ops, err = s.ListClientOperations(ctx, "bulkA", 200, 0)
	if err != nil || len(ops) != 2 {
		t.Fatalf("duplicate/missing immutable audit: %d %v", len(ops), err)
	}
	for _, op := range ops {
		if op.Kind != "disable" || op.UnlimitedCreditDebit != 0 || op.VolumeCreditDebit != 0 {
			t.Fatal("bulk changed credits", op)
		}
	}
	enable := BulkClientState{EventID: "bulk-enable-event-0000001", ClientIDs: request.ClientIDs, Action: "enable", Reason: "operator restore"}
	if _, err = s.BulkSetClientState(ctx, enable, "bulkA"); err != nil {
		t.Fatal("bulk enable", err)
	}
	c, err = s.Client(ctx, "bulk-a1")
	if err != nil || c.DeploymentState != "pending_node_apply" || c.UUID != oldUUID {
		t.Fatal("enable changed UUID/activated Xray", err)
	}
	archive := BulkClientState{EventID: "bulk-archive-event-000001", ClientIDs: []string{"bulk-a1"}, Action: "archive", Reason: "archive safely"}
	if _, err = s.BulkSetClientState(ctx, archive, "bulkA"); err != nil {
		t.Fatal("bulk archive", err)
	}
	if result, err = s.BulkSetClientState(ctx, archive, "bulkA"); err != nil || !result.Replayed {
		t.Fatal("archived batch replay must not mutate again", err)
	}
	c, err = s.Client(ctx, "bulk-a1")
	if err != nil || !c.Archived || c.UUID != oldUUID || c.VolumeBytes != 100 {
		t.Fatal("archive deleted credit/identity", err)
	}
	for _, bad := range []BulkClientState{
		{EventID: "bad", ClientIDs: []string{"bulk-a3"}, Action: "disable", Reason: "test"},
		{EventID: "bulk-invalid-dupe-00001", ClientIDs: []string{"bulk-a3", "bulk-a3"}, Action: "disable", Reason: "test"},
		{EventID: "bulk-invalid-act-000001", ClientIDs: []string{"bulk-a3"}, Action: "delete", Reason: "test"},
		{EventID: "bulk-invalid-missing-001", ClientIDs: []string{"bulk-a2", "missing-record"}, Action: "disable", Reason: "test"},
	} {
		if _, e := s.BulkSetClientState(ctx, bad, "bulkA"); e == nil {
			t.Fatalf("unsafe bulk action accepted %+v", bad)
		}
	}
	longIDs := make([]string, 51)
	for i := range longIDs {
		longIDs[i] = fmt.Sprintf("unused-%03d", i)
	}
	if _, err = s.BulkSetClientState(ctx, BulkClientState{
		EventID: "bulk-too-many-event-00001", ClientIDs: longIDs, Action: "archive", Reason: "test",
	}, "bulkA"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded 51-client transaction accepted: %v", err)
	}
	after, err := s.Representative(ctx, "bulkA")
	if err != nil || after.VolumeCreditBytes != before.VolumeCreditBytes || after.UnlimitedCreditUnits != before.UnlimitedCreditUnits {
		t.Fatal("bulk erroneously refunded or charged credit")
	}
	events, err := s.ListEvents(ctx, "bulkA", 200, 0)
	if err != nil || len(events) != 4 {
		t.Fatalf("bulk modified original financial event history %d %v", len(events), err)
	}
	ops, err = s.ListClientOperations(ctx, "bulkA", 200, 0)
	if err != nil || len(ops) != 5 {
		t.Fatalf("expected 2 disables, 2 enables, 1 archive, got %d %v", len(ops), err)
	}
}
