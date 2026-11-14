package nodeagent

import (
	"bytes"
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// IPTracker learns which source addresses each user connects from by
// reading Xray's access log as it is written ("from 1.2.3.4:5678 accepted
// ... email: alice"). Xray's own online list only holds an address for a
// moment after a connection opens, so sampling it misses most users; the
// access log sees every connection.
type IPTracker struct {
	mu     sync.Mutex
	seen   map[string]map[string]time.Time // email -> ip -> last seen
	buf    []byte
	now    func() time.Time
	Window time.Duration // how long an address counts as online (default 75s)
}

func NewIPTracker() *IPTracker {
	return &IPTracker{seen: map[string]map[string]time.Time{}, now: time.Now, Window: 75 * time.Second}
}

var accessRE = regexp.MustCompile(`from (?:tcp:|udp:)?(\[[0-9a-fA-F:.]+\]|[0-9a-fA-F:.]+):\d+ accepted .*email: (\S+)`)

// Write consumes Xray output; it never fails, so Xray is never blocked.
func (t *IPTracker) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	for {
		i := bytes.IndexByte(t.buf, '\n')
		if i < 0 {
			break
		}
		t.line(t.buf[:i])
		t.buf = t.buf[i+1:]
	}
	if len(t.buf) > 64<<10 { // a runaway line without newline
		t.buf = t.buf[:0]
	}
	return len(p), nil
}

func (t *IPTracker) line(b []byte) {
	if !bytes.Contains(b, []byte(" accepted ")) {
		return
	}
	m := accessRE.FindSubmatch(b)
	if m == nil {
		return
	}
	ip := strings.Trim(string(m[1]), "[]")
	email := string(m[2])
	if ip == "127.0.0.1" || ip == "::1" {
		return
	}
	u := t.seen[email]
	if u == nil {
		u = map[string]time.Time{}
		t.seen[email] = u
	}
	u[ip] = t.now()
}

// Online returns, per user, the addresses seen within the window.
func (t *IPTracker) Online() map[string][]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	out := map[string][]string{}
	for email, ips := range t.seen {
		for ip, at := range ips {
			if now.Sub(at) > 10*time.Minute {
				delete(ips, ip)
				continue
			}
			if now.Sub(at) <= t.Window {
				out[email] = append(out[email], ip)
			}
		}
		if len(ips) == 0 {
			delete(t.seen, email)
		}
		sort.Strings(out[email])
	}
	return out
}

// TrackedStats adds access-log addresses to a StatsSource's online list.
type TrackedStats struct {
	StatsSource
	Tracker *IPTracker
}

func (s *TrackedStats) OnlineIPs(ctx context.Context) (map[string][]string, error) {
	out := s.Tracker.Online()
	api, err := s.StatsSource.OnlineIPs(ctx)
	if err == nil {
		for email, ips := range api {
			have := map[string]bool{}
			for _, ip := range out[email] {
				have[ip] = true
			}
			for _, ip := range ips {
				if !have[ip] && ip != "127.0.0.1" {
					out[email] = append(out[email], ip)
				}
			}
			sort.Strings(out[email])
		}
	}
	return out, nil
}
