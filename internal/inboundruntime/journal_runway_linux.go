//go:build linux

package inboundruntime

import (
	"context"
	"time"
)

// The worker mutex remains held, and state is immutable until the first write
// joins. Only the engine barrier runs concurrently with journal I/O. No receipt
// is sent and no second write starts before the first write completes.
func (w *Worker) saveServing(write func() error) error {
	done := make(chan error, 1)
	go func() { done <- write() }()
	timer := time.NewTimer(time.Until(w.deadline) - 12*time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
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
		return nil
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
