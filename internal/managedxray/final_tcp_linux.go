//go:build linux

package managedxray

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

// UsageBootstrap validates native VLESS/Reality admission but permits extra
// policy/outbound settings. Final-counter proof requires a narrower grammar:
// no alternate transports, outbound mux, proxy chains, detached contexts,
// fragment/noise workers, sniffing or unreviewed settings.
func finalTCPScope(raw []byte) bool {
	var c struct {
		Log    json.RawMessage `json:"log"`
		Stats  json.RawMessage `json:"stats"`
		API    json.RawMessage `json:"api"`
		Policy *struct {
			Levels map[string]struct {
				Up   bool `json:"statsUserUplink"`
				Down bool `json:"statsUserDownlink"`
			} `json:"levels"`
			System *struct {
				Up   bool `json:"statsInboundUplink"`
				Down bool `json:"statsInboundDownlink"`
			} `json:"system,omitempty"`
		} `json:"policy"`
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings *struct {
				FinalRules []struct {
					Action  string   `json:"action"`
					Network string   `json:"network"`
					IP      []string `json:"ip"`
					Port    string   `json:"port"`
				} `json:"finalRules,omitempty"`
			} `json:"settings,omitempty"`
		} `json:"outbounds"`
		Inbounds []struct {
			Listen   string          `json:"listen"`
			Port     uint16          `json:"port"`
			Tag      string          `json:"tag"`
			Protocol string          `json:"protocol"`
			Settings json.RawMessage `json:"settings"`
			Stream   struct {
				Network  string          `json:"network"`
				Security string          `json:"security"`
				Reality  json.RawMessage `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if usagewire.Decode(raw, &c) != nil || len(c.Inbounds) == 0 || c.Policy == nil || len(c.Policy.Levels) != 1 || !c.Policy.Levels["0"].Up || !c.Policy.Levels["0"].Down || len(c.Outbounds) != 1 {
		return false
	}
	out := c.Outbounds[0]
	if out.Tag != "direct" || out.Protocol != "freedom" {
		return false
	}
	if out.Settings != nil {
		for _, rule := range out.Settings.FinalRules {
			if (rule.Action != "allow" && rule.Action != "block") || rule.Network != "tcp" {
				return false
			}
		}
	}
	for _, in := range c.Inbounds {
		if in.Protocol != "vless" || in.Stream.Network != "tcp" || in.Stream.Security != "reality" {
			return false
		}
	}
	return true
}

func validFinalTCPJournal(j journal) bool {
	a := j.Admission
	if j.Version != 5 {
		return a.FinalCounterProtocol == "" && a.DrainNonce == "" && a.FinalReceipt == nil
	}
	if a.FinalCounterProtocol != uc.FinalTCPProtocol {
		return false
	}
	if a.DrainNonce != "" {
		b, e := hex.DecodeString(a.DrainNonce)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != a.DrainNonce || a.TrafficBarrier != pretrafficActivationPossible || j.Usage == nil {
			return false
		}
	}
	if a.FinalReceipt != nil && (a.DrainNonce == "" || j.Usage == nil || !uc.ValidReceipt(*a.FinalReceipt) || a.FinalReceipt.EpochID != j.Usage.Epoch.ID) {
		return false
	}
	return true
}

func (r *Runtime) hasFinalTCPProofLocked() bool {
	return r.state.Version == 5 && r.state.Admission != nil && r.state.Usage != nil && r.state.Admission.FinalReceipt != nil && validFinalTCPJournal(r.state)
}
func (r *Runtime) finalTCPSettledLocked() bool {
	return r.hasFinalTCPProofLocked() && r.state.Admission.Settlement != nil && *r.state.Admission.Settlement == *r.state.Admission.FinalReceipt && r.process.dead() && r.observer == nil
}

type finalTCPRequest struct {
	ctx    context.Context
	nonce  string
	save   func(uc.Report) error
	result chan error
}

// finishTCP runs on the collector goroutine. It owns the outbox and serializes
// the final sample after all ordinary samples; the independent guard stays on.
func (m *usageObserver) finishTCP(request finalTCPRequest) {
	values, err := m.fresh.FinalTCP(request.ctx, m.binary, m.pin, m.options.StatsEndpoint, request.nonce)
	if err == nil {
		m.mu.Lock()
		if m.status.Failure != "" || !m.status.Running {
			err = ErrUsageSettlement
		} else {
			var final uc.Report
			final, err = m.box.Observe(values, true)
			if err == nil {
				err = request.save(final)
			}
			if err == nil {
				m.finalSaved = true
			}
		}
		m.mu.Unlock()
	}
	if err != nil {
		m.fail("final_counter_barrier_failed")
	} else {
		m.cancel()
		m.process.stop()
		m.mu.Lock()
		m.status.Running = false
		m.mu.Unlock()
	}
	request.result <- err
}

// SettleActiveTCP is an INTERNAL, opt-in supervisor operation. The configured
// core must have proved its direct-TCP capability before the first listener.
// It never treats stock RemoveInbound, stable samples, or an empty socket list
// as final-counter evidence. Failed or interrupted drains retain uncertainty.
func (r *Runtime) SettleActiveTCP(ctx context.Context, sender *uc.Sender) (int, error) {
	if ctx == nil || ctx.Err() != nil || sender == nil {
		return 0, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.fenced {
		return 0, ErrClosed
	}
	if r.hasFinalTCPProofLocked() {
		return r.replayFinalTCPLocked(ctx, sender)
	}
	m := r.observer
	if r.state.Version != 5 || r.state.Admission == nil || r.state.Admission.FinalCounterProtocol != uc.FinalTCPProtocol ||
		r.state.Admission.TrafficBarrier != pretrafficActivationPossible || m == nil || m.fresh == nil || !m.healthy() || r.process.dead() {
		return 0, ErrUsageSettlement
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return 0, err
	}
	next := r.state
	a := *next.Admission
	a.DrainNonce = hex.EncodeToString(token[:])
	next.Admission = &a
	next.Phase = "usage_active_draining"
	if err := r.persist(next); err != nil {
		return 0, err
	}
	request := finalTCPRequest{ctx: ctx, nonce: a.DrainNonce, result: make(chan error, 1), save: func(final uc.Report) error {
		// r.mu remains owned by this call until the collector has finished.
		// The final report is already fsynced. Persist its exact identity before
		// suppressing interruption notification or stopping the child.
		next := r.state
		a := *next.Admission
		a.FinalReceipt = &uc.Receipt{EpochID: final.EpochID, Sequence: final.Sequence, SHA: final.SHA}
		next.Admission = &a
		next.Phase = "usage_active_final_saved"
		return r.persist(next)
	}}
	select {
	case m.settle <- request:
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-m.done:
		return 0, ErrUsageSettlement
	}
	// Do not release r.mu while save() can still touch the journal. The RPC,
	// child lease and observer age limits remain independently bounded.
	err := <-request.result
	<-m.done
	r.process.stop()
	r.process = nil
	r.active = nil
	r.observer = nil
	if err != nil {
		return 0, err
	}
	return r.replayFinalTCPLocked(ctx, sender)
}

func (r *Runtime) replayFinalTCPLocked(ctx context.Context, sender *uc.Sender) (int, error) {
	if ctx == nil || ctx.Err() != nil || sender == nil {
		return 0, ErrInvalid
	}
	if r.closed || r.fenced || r.observer != nil || !r.hasFinalTCPProofLocked() {
		return 0, ErrUsageSettlement
	}
	if r.finalTCPSettledLocked() {
		return 0, nil
	}
	r.process.stop()
	r.process = nil
	r.active = nil
	epoch := r.state.Usage.Epoch
	box, err := uc.Open(uc.Options{Directory: usageDirectory(r.options.Directory, epoch.ID), Key: usageKey(r.options.StateKey, epoch.ID), Epoch: epoch, Create: false})
	if err != nil {
		return 0, err
	}
	defer box.Close()
	final, exists, err := box.Last()
	receipt := *r.state.Admission.FinalReceipt
	if err != nil || !exists || !final.Quiesced || uc.VerifyReport(epoch, final) != nil || receipt != (uc.Receipt{EpochID: final.EpochID, Sequence: final.Sequence, SHA: final.SHA}) {
		return 0, uc.ErrConflict
	}
	sent := 0
	for i := 0; i < uc.MaxPending; i++ {
		ok, e := sender.FlushOne(ctx, box)
		if e != nil {
			return sent, e
		}
		if !ok {
			break
		}
		sent++
	}
	pending, err := box.Pending()
	if err != nil {
		return sent, err
	}
	if len(pending) != 0 {
		return sent, uc.ErrReceipt
	}
	if err = sender.ConfirmSettlement(ctx, epoch, receipt); err != nil {
		return sent, err
	}
	next := r.state
	a := *next.Admission
	a.Settlement = &receipt
	next.Admission = &a
	next.Phase = "usage_active_settled"
	if err = r.persist(next); err != nil {
		return sent, err
	}
	return sent, nil
}
