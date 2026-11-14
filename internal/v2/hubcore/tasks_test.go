package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestNodeUpdateAndDist(t *testing.T) {
	s, _ := newStore(t)
	tok := seed(t, s)
	ctx := context.Background()
	if _, err := s.StartNodeUpdate(ctx, "de-1"); err == nil {
		t.Fatal("update without a dist binary")
	}
	dir := t.TempDir()
	bin := []byte("node-binary-v2")
	if err := os.WriteFile(filepath.Join(dir, "dark-node"), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	s.SetDistDir(dir)
	sum := sha256.Sum256(bin)
	want := hex.EncodeToString(sum[:])
	if s.DistSHA() != want {
		t.Fatal("dist sha")
	}
	s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", AgentSHA: "old"})
	ns, _ := s.Nodes(ctx)
	if !ns[0].UpdateAvailable {
		t.Fatalf("update not offered: %+v", ns[0])
	}
	id, err := s.StartNodeUpdate(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", AgentSHA: "old"})
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != id || !strings.Contains(string(resp.Tasks[0].Args), want) {
		t.Fatalf("tasks %+v", resp.Tasks)
	}
	s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1", AgentSHA: want, Results: []wire.TaskResult{{ID: id, Result: json.RawMessage(`{}`)}}})
	if ns, _ := s.Nodes(ctx); ns[0].UpdateAvailable {
		t.Fatal("still offered after update")
	}
	// Dist download needs the node credential and serves only known files.
	h := NodeHandler(s)
	get := func(file, token string) int {
		r := httptest.NewRequest("GET", wire.PathDist+file, nil)
		r.Header.Set(wire.HeaderNodeID, "de-1")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := get("dark-node", "wrong"); c != 401 {
		t.Fatalf("unauthenticated dist: %d", c)
	}
	if c := get("dark-node", tok); c != 200 {
		t.Fatalf("dist: %d", c)
	}
	if c := get("..%2Fsecret", tok); c != 404 {
		t.Fatalf("unknown file: %d", c)
	}
}
