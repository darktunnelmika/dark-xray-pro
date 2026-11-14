package nodeagent

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sniscan"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// TaskFunc runs one HUB task and returns its JSON result.
type TaskFunc func(ctx context.Context, args json.RawMessage) (any, error)

// taskBox tracks one-off tasks: each ID runs once, and its result is
// resent with every sync until one succeeds.
type taskBox struct {
	mu      sync.Mutex
	path    string // results survive a restart (self-update restarts us)
	seen    map[string]time.Time
	results map[string]wire.TaskResult
	run     map[string]TaskFunc
	restart func()
}

// Restart asks the box to persist the result, then call the agent's
// restart hook (the process exits and systemd starts the new binary).
type Restart struct{ Value any }

type savedTasks struct {
	Seen    map[string]time.Time       `json:"seen"`
	Results map[string]wire.TaskResult `json:"results"`
}

func newTaskBox(path string) *taskBox {
	b := &taskBox{path: path, seen: map[string]time.Time{}, results: map[string]wire.TaskResult{}, run: map[string]TaskFunc{}}
	if path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			var st savedTasks
			if json.Unmarshal(raw, &st) == nil {
				for k, v := range st.Seen {
					b.seen[k] = v
				}
				for k, v := range st.Results {
					b.results[k] = v
				}
			}
		}
	}
	b.run = map[string]TaskFunc{
		wire.TaskSNIScan: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var a sniscan.Args
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &a); err != nil {
					return nil, err
				}
			}
			if len(a.Domains) == 0 && !a.Subnet {
				a.Domains = sniscan.Defaults
			}
			return sniscan.Scanner{}.Run(ctx, a), nil
		},
	}
	return b
}

// save persists seen IDs and undelivered results; called with mu held.
func (b *taskBox) save() {
	if b.path == "" {
		return
	}
	raw, err := json.Marshal(savedTasks{Seen: b.seen, Results: b.results})
	if err == nil {
		_ = writeFileAtomic(b.path, raw)
	}
}

// SetRestart sets what a Restart result triggers (main: graceful exit).
func (a *Agent) SetRestart(f func()) {
	a.tasks.mu.Lock()
	a.tasks.restart = f
	a.tasks.mu.Unlock()
}

// SetTask registers or replaces a task kind (tests).
func (a *Agent) SetTask(kind string, f TaskFunc) {
	a.tasks.mu.Lock()
	a.tasks.run[kind] = f
	a.tasks.mu.Unlock()
}

func (b *taskBox) pending() []wire.TaskResult {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]wire.TaskResult, 0, len(b.results))
	for _, r := range b.results {
		out = append(out, r)
	}
	return out
}

func (b *taskBox) delivered(list []wire.TaskResult) {
	if len(list) == 0 {
		return
	}
	b.mu.Lock()
	for _, r := range list {
		delete(b.results, r.ID)
	}
	b.save()
	b.mu.Unlock()
}

func (b *taskBox) start(ctx context.Context, list []wire.Task, logf func(string, ...any)) {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, at := range b.seen {
		if now.Sub(at) > 6*time.Hour {
			delete(b.seen, id)
		}
	}
	changed := false
	defer func() {
		if changed {
			b.save()
		}
	}()
	for _, t := range list {
		if t.ID == "" || !b.seen[t.ID].IsZero() {
			continue
		}
		b.seen[t.ID] = now
		changed = true
		f := b.run[t.Kind]
		if f == nil {
			b.results[t.ID] = wire.TaskResult{ID: t.ID, Error: "unsupported task " + t.Kind}
			continue
		}
		go func(t wire.Task) {
			tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			res := wire.TaskResult{ID: t.ID}
			v, err := f(tctx, t.Args)
			rs, again := v.(Restart)
			if again {
				v = rs.Value
			}
			if err != nil {
				res.Error = err.Error()
			} else if res.Result, err = json.Marshal(v); err != nil {
				res.Error = err.Error()
			}
			logf("task %s (%s) finished", t.ID, t.Kind)
			b.mu.Lock()
			b.results[t.ID] = res
			b.save()
			restart := b.restart
			b.mu.Unlock()
			if again && err == nil && restart != nil {
				restart()
			}
		}(t)
	}
}
