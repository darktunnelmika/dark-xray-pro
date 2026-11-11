//go:build linux

package inboundruntime

import (
	"context"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// The worker mutex remains held, and state is immutable until the first write
// joins. Only the engine barrier runs concurrently with journal I/O. No receipt
// is sent and no second write starts before the first write completes.
func (w *Worker) saveServing(write func() error) error {
	done := make(chan error, 1)
	go func() { done <- write() }()
	timer := time.NewTimer(time.Until(w.deadline) - 16*time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
	}
	// One bounded attempt per write. The signed authority is required to
	// advance, and the original write remains the only journal mutation.
	if w.journalRenewal == nil || (time.Until(w.deadline) > 12*time.Second && w.journalRenewal()) {
		timer.Reset(time.Until(w.deadline) - 12*time.Second)
		select {
		case err := <-done:
			return err
		case <-timer.C:
		}
	}
	nonce := w.state.DrainNonce
	var err error
	if nonce == "" {
		nonce, err = randomID(32)
	}
	if err != nil {
		if e := <-done; e != nil {
			return e
		}
		return err
	}
	w.cfg.Diagnostics.record(diagnosticDrain, diagnosticLeaseRunway)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	values, captureErr := w.process.Final(ctx, nonce)
	cancel()
	w.cfg.Diagnostics.recordError(diagnosticFinalCapture, captureErr)
	// Joining is mandatory even on capture failure: a later save must never race
	// or overwrite an older journal write which is still in flight.
	if err = <-done; err != nil {
		return err
	}
	if captureErr != nil {
		// The core may have accepted this nonce and permanently closed admission
		// before its response timed out. Retain that exact identity after joining
		// the prior write; a new nonce would conflict with the core's sealed gate.
		// No final values or receipt are inferred from this ambiguous response.
		w.state.DrainNonce = nonce
		w.state.Phase = "draining"
		return write()
	}
	if err = w.validateInitialFinal(values); err != nil {
		return err
	}
	w.state.DrainNonce = nonce
	w.state.LastCounters = values
	if w.state.Pending != nil {
		w.state.Phase = "sealed_waiting_receipt"
	} else {
		w.state.Sequence++
		report := w.makeReport(values, true)
		w.state.Pending = &report
		w.state.Phase = "final_pending"
	}
	// The engine is sealed now. Persist the final even if the hard lease timer
	// stops the child meanwhile. A failed save still fences all report delivery.
	err = write()
	if err == nil {
		w.cfg.Diagnostics.record(diagnosticFinalCapture, diagnosticSucceeded)
	} else {
		w.cfg.Diagnostics.recordError(diagnosticFinalCapture, err)
	}
	return err
}

func (w *Worker) renewWhileSaving(lifetime context.Context, x Exchange) bool {
	if lifetime.Err() != nil || w.persistFailed || w.state.Phase != "active" ||
		w.state.Epoch == nil || w.state.Bundle == nil || w.process == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(lifetime, 3*time.Second)
	defer cancel()
	d, err := x.Poll(ctx, PollInput{NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Capabilities: w.Capabilities()})
	if err != nil || ctx.Err() != nil || d == nil || d.Clearance != nil ||
		VerifyDelivery(w.cfg.Key, *d, w.cfg.NodeID, w.cfg.InstallationID, time.Now()) != nil ||
		ix.Digest(d.Bundle) != ix.Digest(*w.state.Bundle) {
		return false
	}
	deadline := verifiedLeaseDeadline(d.Lease, time.Now())
	if !deadline.After(w.deadline) || time.Until(deadline) <= 16*time.Second {
		return false
	}
	if w.renewPublicAliases(ctx, deadline) != nil || !w.process.Renew(deadline) {
		return false
	}
	w.deadline = deadline
	return true
}
