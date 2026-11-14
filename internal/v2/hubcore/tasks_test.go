package hubcore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestNodeTasksLifecycle(t *testing.T) {
	s, now := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "fi-1", "Finland"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartSNIScan(ctx, "nope", nil, false); err != ErrNotFound {
		t.Fatalf("unknown node: %v", err)
	}
	id, err := s.StartSNIScan(ctx, "de-1", []string{" www.example.com ", ""}, false)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1"})
	if err != nil || len(resp.Tasks) != 1 || resp.Tasks[0].ID != id || resp.Tasks[0].Kind != wire.TaskSNIScan {
		t.Fatalf("tasks %+v %v", resp.Tasks, err)
	}
	var args struct{ Domains []string }
	_ = json.Unmarshal(resp.Tasks[0].Args, &args)
	if len(args.Domains) != 1 || args.Domains[0] != "www.example.com" {
		t.Fatalf("args %s", resp.Tasks[0].Args)
	}
	// Another node cannot answer it.
	if _, err := s.Sync(ctx, "fi-1", wire.SyncRequest{NodeID: "fi-1", Results: []wire.TaskResult{{ID: id, Result: json.RawMessage(`{"x":1}`)}}}); err != nil {
		t.Fatal(err)
	}
	if tk, _ := s.TaskByID(ctx, id); tk.Status != "queued" {
		t.Fatalf("foreign result accepted: %+v", tk)
	}
	resp, _ = s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", Results: []wire.TaskResult{{ID: id, Result: json.RawMessage(`{"candidates":[]}`)}}})
	if len(resp.Tasks) != 0 {
		t.Fatalf("finished task still sent: %+v", resp.Tasks)
	}
	tk, err := s.TaskByID(ctx, id)
	if err != nil || tk.Status != "done" || string(tk.Result) != `{"candidates":[]}` {
		t.Fatalf("task %+v %v", tk, err)
	}
	// A resent result does not overwrite.
	s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", Results: []wire.TaskResult{{ID: id, Error: "late"}}})
	if tk, _ := s.TaskByID(ctx, id); tk.Status != "done" {
		t.Fatalf("overwritten: %+v", tk)
	}
	// Unanswered tasks expire.
	id2, _ := s.StartSNIScan(ctx, "de-1", nil, true)
	*now = now.Add(11 * time.Minute)
	if tk, _ := s.TaskByID(ctx, id2); tk.Status != "expired" {
		t.Fatalf("expiry: %+v", tk)
	}
	if resp, _ := s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1"}); len(resp.Tasks) != 0 {
		t.Fatalf("expired task sent: %+v", resp.Tasks)
	}
}
