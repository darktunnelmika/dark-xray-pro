//go:build linux

package usagecheckpoint

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/darktunnelmika/dark-xray-pro/internal/corebarrier"
)

const FinalTCPProtocol = corebarrier.Protocol

// VerifyFinalTCPCapability must run on the exact API-only birth before admitting
// customers. A stock core's empty pattern match is never a capability receipt.
func VerifyFinalTCPCapability(ctx context.Context, binary, pin, endpoint string, epoch Epoch) error {
	raw, err := collectPattern(ctx, binary, pin, endpoint, epoch, FinalTCPProtocol+"capability")
	if err != nil {
		return err
	}
	rest, err := stripFinalMarker(raw, FinalTCPProtocol+"scope/vless-direct-tcp")
	if err != nil {
		return err
	}
	if string(rest) != `{"stat":[]}` {
		return ErrConflict
	}
	return nil
}

// FinalTCP returns counters only after the opt-in core has irrevocably closed
// VLESS admission and joined all tracked direct-TCP work, including late splice
// accounting. The ordinary same-birth, endpoint-owner and binary-pin checks
// apply before AND after the call. This does not itself seal a HUB epoch.
func (f *FreshCounters) FinalTCP(ctx context.Context, binary, pin, endpoint, nonce string) ([]Usage, error) {
	if !hexID(nonce, 64) {
		return nil, ErrInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := collectPattern(ctx, binary, pin, endpoint, f.epoch, FinalTCPProtocol+"drain/"+nonce)
	if err != nil {
		return nil, err
	}
	raw, err = stripFinalMarker(raw, FinalTCPProtocol+"sealed/"+nonce)
	if err != nil {
		return nil, err
	}
	return f.parse(raw)
}

func stripFinalMarker(raw []byte, name string) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxStatsBytes {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, e := d.Token(); e != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	if !d.More() {
		return nil, ErrIncomplete
	}
	if k, e := d.Token(); e != nil || k != "stat" {
		return nil, ErrInvalid
	}
	var entries []json.RawMessage
	if d.Decode(&entries) != nil || len(entries) == 0 || len(entries) > 2*MaxBindings+1 {
		return nil, ErrIncomplete
	}
	if d.More() {
		return nil, ErrInvalid
	}
	if t, e := d.Token(); e != nil || t != json.Delim('}') {
		return nil, ErrInvalid
	}
	var extra any
	if !isEOF(d.Decode(&extra)) {
		return nil, ErrInvalid
	}
	remaining := make([]json.RawMessage, 0, len(entries)-1)
	found := false
	for _, encoded := range entries {
		s, e := strictStat(encoded)
		if e != nil {
			return nil, e
		}
		if s.Name == name {
			if found || !(string(s.Value) == `"1"` || string(s.Value) == "1") {
				return nil, ErrConflict
			}
			found = true
		} else {
			remaining = append(remaining, encoded)
		}
	}
	if !found {
		return nil, ErrIncomplete
	}
	return json.Marshal(map[string]any{"stat": remaining})
}
