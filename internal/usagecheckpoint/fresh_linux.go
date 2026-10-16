//go:build linux

package usagecheckpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// FreshCounters is ONLY for an API-only child which had no customer inbounds
// when its complete zero report was durably acknowledged. It is never restored
// for an old process. Generic Collect/ParseStats keep their strict semantics.
// Xray creates user counters lazily. Once a direction appears, its disappearance
// or decrease is an error even if the customer's last recorded value was zero.
type FreshCounters struct {
	mu       sync.Mutex
	epoch    Epoch
	seen     map[string]bool
	previous []Usage
}

func NewFreshCounters(epoch Epoch, baseline Report, receipt Receipt) (*FreshCounters, error) {
	e, err := normalize(epoch)
	if err != nil {
		return nil, err
	}
	if err = validateReport(e, baseline); err != nil {
		return nil, err
	}
	if baseline.Sequence != 1 || baseline.Quiesced || receipt.EpochID != e.ID || receipt.Sequence != 1 || receipt.SHA != baseline.SHA {
		return nil, ErrReceipt
	}
	for _, u := range baseline.Users {
		if u.Uplink != 0 || u.Downlink != 0 {
			return nil, ErrInvalid
		}
	}
	return &FreshCounters{epoch: e, seen: map[string]bool{}, previous: append([]Usage(nil), baseline.Users...)}, nil
}

func (f *FreshCounters) Collect(ctx context.Context, binary, pin, endpoint string) ([]Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := collectRaw(ctx, binary, pin, endpoint, f.epoch)
	if err != nil {
		return nil, err
	}
	return f.parse(raw)
}

// Decode an actual successful QueryStats response. Only the official empty
// object or an array may represent an unseen user; null, truncation, duplicate
// fields, unknown names, invalid directions and one-sided pairs are not zero.
func (f *FreshCounters) parse(raw []byte) ([]Usage, error) {
	if len(raw) == 0 || len(raw) > MaxStatsBytes {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	entries := []json.RawMessage{}
	found := false
	for d.More() {
		key, e := d.Token()
		if e != nil || key != "stat" || found {
			return nil, ErrInvalid
		}
		found = true
		var encoded json.RawMessage
		if d.Decode(&encoded) != nil || len(bytes.TrimSpace(encoded)) == 0 || bytes.TrimSpace(encoded)[0] != '[' {
			return nil, ErrInvalid
		}
		if json.Unmarshal(encoded, &entries) != nil {
			return nil, ErrInvalid
		}
	}
	if t, err = d.Token(); err != nil || t != json.Delim('}') {
		return nil, ErrInvalid
	}
	var extra any
	if !isEOF(d.Decode(&extra)) {
		return nil, ErrInvalid
	}
	if len(entries) > MaxBindings*2+4096 {
		return nil, ErrFull
	}
	actual := map[string]bool{}
	for _, rawEntry := range entries {
		entry, e := strictStat(rawEntry)
		if e != nil {
			return nil, e
		}
		if actual[entry.Name] {
			return nil, ErrConflict
		}
		actual[entry.Name] = true
	}
	for _, b := range f.epoch.Bindings {
		up := "user>>>" + b.Email + ">>>traffic>>>uplink"
		down := "user>>>" + b.Email + ">>>traffic>>>downlink"
		if actual[up] != actual[down] {
			return nil, ErrIncomplete
		}
		for _, name := range []string{up, down} {
			if !actual[name] {
				if f.seen[name] {
					return nil, ErrIncomplete
				}
				entry, _ := json.Marshal(map[string]string{"name": name, "value": "0"})
				entries = append(entries, entry)
			}
		}
	}
	normalized, _ := json.Marshal(map[string]any{"stat": entries})
	values, err := ParseStats(normalized, f.epoch.Bindings)
	if err != nil {
		return nil, err
	}
	for i, u := range values {
		if u.Uplink < f.previous[i].Uplink || u.Downlink < f.previous[i].Downlink {
			return nil, ErrRegression
		}
	}
	// No partially valid sample can advance seen/monotonic state.
	for name := range actual {
		if strings.HasPrefix(name, "user>>>") {
			f.seen[name] = true
		}
	}
	f.previous = append([]Usage(nil), values...)
	return values, nil
}
