//go:build linux

package managedxray

import (
	"context"
	"errors"
	"os"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const (
	pretrafficNeverInstalled     = "never_installed_v1"
	pretrafficActivationPossible = "activation_possible_v1"
)

func validPretrafficJournal(j journal) bool {
	if j.Admission == nil {
		return false
	}
	a := j.Admission
	if a.TrafficBarrier != pretrafficNeverInstalled && a.TrafficBarrier != pretrafficActivationPossible {
		return false
	}
	if a.Settlement != nil && (a.TrafficBarrier != pretrafficNeverInstalled || j.Usage == nil ||
		!uc.ValidReceipt(*a.Settlement) || a.Settlement.EpochID != j.Usage.Epoch.ID) {
		return false
	}
	return true
}

func (r *Runtime) provenUnopenedLocked() bool {
	return r.state.Version == 4 && r.state.Admission != nil && r.state.Usage != nil &&
		r.state.Admission.TrafficBarrier == pretrafficNeverInstalled && r.observer == nil
}

func (r *Runtime) pretrafficSettledLocked() bool {
	return r.provenUnopenedLocked() && r.state.Admission.Settlement != nil && r.process.dead()
}

func (r *Runtime) canPrepareUsageLocked() bool {
	if r.closed || r.fenced || r.observer != nil || r.process != nil {
		return false
	}
	if r.state.Highwater == 0 && r.state.Good == nil && r.state.Usage == nil && r.state.Admission == nil {
		return true
	}
	return r.pretrafficSettledLocked()
}

// SettleUnopenedUsage settles ONLY a registered API-only birth whose encrypted
// v4 intent proves that AddInbound could never have been attempted. It is not
// a drain primitive for a serving core. Legacy/active/ambiguous epochs remain
// fenced. An unchanged counter or successful RemoveInbound is not proof.
func (r *Runtime) SettleUnopenedUsage(ctx context.Context, sender *uc.Sender) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settleUnopenedUsageLocked(ctx, sender)
}

func (r *Runtime) settleUnopenedUsageLocked(ctx context.Context, sender *uc.Sender) (int, error) {
	if ctx == nil || ctx.Err() != nil || sender == nil {
		return 0, ErrInvalid
	}
	if r.closed || r.fenced {
		return 0, ErrClosed
	}
	if !r.provenUnopenedLocked() {
		return 0, ErrUsageSettlement
	}
	if r.pretrafficSettledLocked() {
		return 0, nil
	}
	next := r.state
	next.Phase = "usage_pretraffic_settling"
	if err := r.persist(next); err != nil {
		return 0, err
	}
	// Completion, not merely a stop request, precedes the terminal report.
	r.process.stop()
	r.process = nil
	r.active = nil
	epoch := r.state.Usage.Epoch
	directory := usageDirectory(r.options.Directory, epoch.ID)
	_, err := os.Lstat(directory)
	create := errors.Is(err, os.ErrNotExist)
	if err != nil && !create {
		return 0, err
	}
	box, err := uc.Open(uc.Options{Directory: directory, Key: usageKey(r.options.StateKey, epoch.ID), Epoch: epoch, Create: create})
	if err != nil {
		return 0, err
	}
	defer box.Close()
	last, exists, err := box.Last()
	if err != nil {
		return 0, err
	}
	if exists {
		for _, u := range last.Users {
			if u.Uplink != 0 || u.Downlink != 0 {
				return 0, uc.ErrConflict
			}
		}
	}
	zeros := make([]uc.Usage, len(epoch.Bindings))
	for i, b := range epoch.Bindings {
		zeros[i].ClientID = b.ClientID
	}
	final, err := box.Observe(zeros, true)
	if err != nil {
		return 0, err
	}
	sent := 0
	for i := 0; i < uc.MaxPending; i++ {
		ok, err := sender.FlushOne(ctx, box)
		if err != nil {
			return sent, err
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
	receipt := uc.Receipt{EpochID: final.EpochID, Sequence: final.Sequence, SHA: final.SHA}
	// A quiesced report may still have an uncertain ledger tail. Only this
	// separate authenticated confirmation proves the exact epoch was sealed.
	if err = sender.ConfirmSettlement(ctx, epoch, receipt); err != nil {
		return sent, err
	}
	next = r.state
	admission := *next.Admission
	admission.Settlement = &receipt
	next.Admission = &admission
	next.Phase = "usage_pretraffic_settled"
	if err = r.persist(next); err != nil {
		return sent, err
	}
	return sent, nil
}
