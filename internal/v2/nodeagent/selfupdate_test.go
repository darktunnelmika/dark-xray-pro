package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

type fakeFetch struct{ body []byte }

func (f fakeFetch) Fetch(_ context.Context, file string, w io.Writer) error {
	_, err := w.Write(f.body)
	return err
}

func shaOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestSelfUpdateVerifiesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "dark-node")
	old := []byte("#!/bin/sh\necho old\n")
	if err := os.WriteFile(exe, old, 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := []byte("#!/bin/sh\necho dark-node new\n")
	args := func(sum string) json.RawMessage { b, _ := json.Marshal(map[string]string{"sha256": sum}); return b }

	// Wrong checksum: nothing replaced.
	bad := SelfUpdateTask(fakeFetch{newBin}, exe)
	if _, err := bad(context.Background(), args(strings.Repeat("a", 64))); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(old) {
		t.Fatal("binary replaced despite bad checksum")
	}
	// A binary that does not run: nothing replaced.
	broken := []byte("#!/bin/sh\nexit 3\n")
	if _, err := SelfUpdateTask(fakeFetch{broken}, exe)(context.Background(), args(shaOf(broken))); err == nil {
		t.Fatal("broken binary accepted")
	}
	if b, _ := os.ReadFile(exe); string(b) != string(old) {
		t.Fatal("binary replaced by a broken build")
	}
	// Good update.
	v, err := SelfUpdateTask(fakeFetch{newBin}, exe)(context.Background(), args(shaOf(newBin)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(Restart); !ok {
		t.Fatalf("want Restart, got %T", v)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(newBin) {
		t.Fatal("not replaced")
	}
	if b, _ := os.ReadFile(exe + ".prev"); string(b) != string(old) {
		t.Fatal("previous binary not kept")
	}
	// Already current: no restart.
	v, err = SelfUpdateTask(fakeFetch{newBin}, exe)(context.Background(), args(shaOf(newBin)))
	if _, isRestart := v.(Restart); err != nil || isRestart {
		t.Fatalf("current binary: %v %T", err, v)
	}
}

func TestTaskBoxSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	b := newTaskBox(path)
	restarted := make(chan struct{})
	b.restart = func() { close(restarted) }
	b.run["up"] = func(context.Context, json.RawMessage) (any, error) { return Restart{Value: "ok"}, nil }
	b.start(context.Background(), []wire.Task{{ID: "u1", Kind: "up"}}, func(string, ...any) {})
	<-restarted
	// A new process: the result is still pending and the task is not rerun.
	b2 := newTaskBox(path)
	ran := false
	b2.run["up"] = func(context.Context, json.RawMessage) (any, error) { ran = true; return nil, nil }
	if p := b2.pending(); len(p) != 1 || string(p[0].Result) != `"ok"` {
		t.Fatalf("pending after restart %+v", p)
	}
	b2.start(context.Background(), []wire.Task{{ID: "u1", Kind: "up"}}, func(string, ...any) {})
	if ran {
		t.Fatal("task rerun after restart")
	}
	b2.delivered(b2.pending())
	if p := newTaskBox(path).pending(); len(p) != 0 {
		t.Fatalf("delivered result came back: %+v", p)
	}
}
