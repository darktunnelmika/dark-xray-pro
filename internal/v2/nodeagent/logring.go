package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// LogRing keeps the last lines Xray printed (stdout and stderr), so the
// panel can show them without shell access to the node.
type LogRing struct {
	mu      sync.Mutex
	lines   []string
	next    int
	full    bool
	partial []byte
}

func NewLogRing(n int) *LogRing { return &LogRing{lines: make([]string, n)} }

func (r *LogRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	buf := append(r.partial, p...)
	for {
		i := strings.IndexByte(string(buf), '\n')
		if i < 0 {
			break
		}
		r.add(strings.TrimRight(string(buf[:i]), "\r"))
		buf = buf[i+1:]
	}
	if len(buf) > 4096 { // a runaway line without a newline
		r.add(string(buf[:4096]))
		buf = nil
	}
	r.partial = append([]byte(nil), buf...)
	return len(p), nil
}

func (r *LogRing) add(line string) {
	if line == "" {
		return
	}
	if len(line) > 1000 {
		line = line[:1000] + "…"
	}
	r.lines[r.next] = line
	r.next = (r.next + 1) % len(r.lines)
	if r.next == 0 {
		r.full = true
	}
}

// Tail returns up to n of the newest lines (oldest first) that contain
// every filter word; errorsOnly keeps warnings and errors.
func (r *LogRing) Tail(n int, errorsOnly bool, grep string) []string {
	r.mu.Lock()
	all := make([]string, 0, len(r.lines))
	if r.full {
		all = append(all, r.lines[r.next:]...)
	}
	all = append(all, r.lines[:r.next]...)
	r.mu.Unlock()
	grep = strings.ToLower(strings.TrimSpace(grep))
	out := []string{}
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		l := all[i]
		low := strings.ToLower(l)
		if errorsOnly && !strings.Contains(low, "[warning]") && !strings.Contains(low, "[error]") && !strings.Contains(low, "failed") && !strings.Contains(low, "xray exited") {
			continue
		}
		if grep != "" && !strings.Contains(low, grep) {
			continue
		}
		out = append(out, l)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// XrayLogTask returns recent Xray output.
func XrayLogTask(r *LogRing) TaskFunc {
	return func(_ context.Context, raw json.RawMessage) (any, error) {
		var a struct {
			Lines  int    `json:"lines"`
			Errors bool   `json:"errors"`
			Grep   string `json:"grep"`
		}
		_ = json.Unmarshal(raw, &a)
		if a.Lines <= 0 || a.Lines > 300 {
			a.Lines = 150
		}
		return map[string]any{"lines": r.Tail(a.Lines, a.Errors, a.Grep)}, nil
	}
}

// XrayRestartTask restarts Xray with its current config.
func XrayRestartTask(s *Supervisor) TaskFunc {
	return func(context.Context, json.RawMessage) (any, error) {
		running, _, _ := s.Status()
		if !running {
			return nil, errors.New("xray is not running (no config applied yet?)")
		}
		s.restart()
		return map[string]bool{"restarted": true}, nil
	}
}
