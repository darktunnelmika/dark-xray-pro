package nodeagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestTasksRunOnceAndResultsAreResentUntilDelivered(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	hub := &fakeHub{resp: wire.SyncResponse{ConfigRevision: 1, Config: json.RawMessage(testConfig),
		Tasks: []wire.Task{{ID: "t1", Kind: "echo", Args: json.RawMessage(`{"v":7}`)}, {ID: "t2", Kind: "nope"}}}}
	a := newTestAgent(t, t.TempDir(), hub, &fakeXray{}, &fakeStats{}, &now)
	runs := make(chan struct{}, 4)
	a.SetTask("echo", func(_ context.Context, args json.RawMessage) (any, error) {
		runs <- struct{}{}
		var in struct{ V int }
		_ = json.Unmarshal(args, &in)
		return map[string]int{"v": in.V * 2}, nil
	})
	a.Exchange(ctx)
	<-runs
	// The HUB keeps listing the task until it has a result; it must not rerun.
	deadline := time.Now().Add(2 * time.Second)
	for len(a.tasks.pending()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	hub.mu.Lock()
	hub.down = true
	hub.mu.Unlock()
	a.Exchange(ctx) // failed sync: results stay pending
	if n := len(a.tasks.pending()); n != 2 {
		t.Fatalf("pending after failed sync = %d", n)
	}
	hub.mu.Lock()
	hub.down = false
	hub.results = nil
	hub.mu.Unlock()
	a.Exchange(ctx)
	select {
	case <-runs:
		t.Fatal("task ran twice")
	case <-time.After(50 * time.Millisecond):
	}
	got := map[string]wire.TaskResult{}
	hub.mu.Lock()
	for _, r := range hub.results {
		got[r.ID] = r
	}
	hub.mu.Unlock()
	if string(got["t1"].Result) != `{"v":14}` || got["t2"].Error == "" {
		t.Fatalf("results %+v", got)
	}
	if n := len(a.tasks.pending()); n != 0 {
		t.Fatalf("pending after delivery = %d", n)
	}
}
