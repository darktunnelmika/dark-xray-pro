package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// Automatic cleanup of dead configs, switched on per seller (the owner for
// its own clients, each reseller for its own). A config is dead when it
// ended — its term is over or its volume is used up — and it has been quiet
// for the chosen number of days (30 by default). A config that has not
// ended is never touched, even if it was never connected (it may be waiting
// for its first use), and connected shops' clients are never in the list.

// AutoClean is a seller's cleanup choice and what the last runs did.
type AutoClean struct {
	On        bool  `json:"on"`
	Days      int64 `json:"days"`                 // quiet days after the end, 7–365
	LastRun   int64 `json:"last_run,omitempty"`   // read only
	LastCount int   `json:"last_count,omitempty"` // read only: deleted in the last run that deleted any
	Total     int   `json:"total,omitempty"`      // read only: deleted so far
}

const (
	autoCleanDefaultDays = 30
	autoCleanMaxPerRun   = 500 // per seller and run: a wrong setting cannot empty a panel at once
)

func (s *Store) AutoCleanFor(ctx context.Context, seller string) AutoClean {
	a := AutoClean{Days: autoCleanDefaultDays}
	_ = json.Unmarshal([]byte(s.Setting(ctx, cardKey("autoclean", seller))), &a)
	if a.Days < 7 || a.Days > 365 {
		a.Days = autoCleanDefaultDays
	}
	return a
}

// PutAutoClean saves the choice (the run counters are kept).
func (s *Store) PutAutoClean(ctx context.Context, seller string, in AutoClean) error {
	if in.Days == 0 {
		in.Days = autoCleanDefaultDays
	}
	if in.Days < 7 || in.Days > 365 {
		return fmt.Errorf("%w: days: 7 to 365", ErrInvalid)
	}
	cur := s.AutoCleanFor(ctx, seller)
	cur.On, cur.Days = in.On, in.Days
	b, _ := json.Marshal(cur)
	return s.SetSetting(ctx, cardKey("autoclean", seller), string(b))
}

// deadClient tells whether a client ended at least days ago and has been
// quiet since then.
func deadClient(c ClientView, days, now int64) bool {
	cut := now - days*86400
	if c.Usage.LastSeen > cut {
		return false
	}
	switch {
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= cut:
		return true
	case c.VolumeBytes > 0 && c.Remaining == 0 && c.Usage.LastSeen > 0:
		return true
	}
	return false
}

// DeadClients lists a seller's clients the cleanup would delete now.
func (s *Store) DeadClients(ctx context.Context, seller string, days int64) ([]string, error) {
	all, err := s.Clients(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	out := []string{}
	for _, c := range all {
		if c.RepID == seller && deadClient(c, days, now) {
			out = append(out, c.Email)
		}
	}
	return out, nil
}

var autoCleanMu sync.Mutex
var autoCleanLast int64

// runAutoClean deletes dead configs for every seller that switched the
// cleanup on, at most once an hour.
func (s *Store) runAutoClean(ctx context.Context) int {
	now := s.now().Unix()
	autoCleanMu.Lock()
	if now-autoCleanLast < 3600 {
		autoCleanMu.Unlock()
		return 0
	}
	autoCleanLast = now
	autoCleanMu.Unlock()
	rows, err := s.ro.QueryContext(ctx, `SELECT key,value FROM settings WHERE key='autoclean' OR key LIKE 'autoclean:%'`)
	if err != nil {
		return 0
	}
	on := map[string]AutoClean{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		var a AutoClean
		if json.Unmarshal([]byte(v), &a) == nil && a.On {
			if a.Days < 7 || a.Days > 365 {
				a.Days = autoCleanDefaultDays
			}
			on[strings.TrimPrefix(strings.TrimPrefix(k, "autoclean"), ":")] = a
		}
	}
	rows.Close()
	if len(on) == 0 {
		return 0
	}
	all, err := s.Clients(ctx)
	if err != nil {
		return 0
	}
	hit := map[string][]string{}
	for _, c := range all {
		if a, ok := on[c.RepID]; ok && deadClient(c, a.Days, now) && len(hit[c.RepID]) < autoCleanMaxPerRun {
			hit[c.RepID] = append(hit[c.RepID], c.Email)
		}
	}
	total := 0
	for seller, a := range on {
		n := 0
		for _, e := range hit[seller] {
			if err := s.DeleteClient(ctx, e); err != nil {
				log.Printf("autoclean %s: %v", e, err)
				continue
			}
			n++
		}
		a.LastRun = now
		if n > 0 {
			a.LastCount, a.Total = n, a.Total+n
			log.Printf("autoclean: %d dead config(s) of %q deleted", n, seller)
		}
		b, _ := json.Marshal(a)
		_ = s.SetSetting(ctx, cardKey("autoclean", seller), string(b))
		total += n
	}
	return total
}
