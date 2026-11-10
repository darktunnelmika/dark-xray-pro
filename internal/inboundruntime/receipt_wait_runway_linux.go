//go:build linux

package inboundruntime

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"time"
)

// A local HUB commit may remain blocked after its request context expires.
// Preserve the genuine final before the hard lease kills admission. The worker
// mutex stays held; only the immutable ordinary report enters the exchange
// goroutine. Capture persists a separate final sample, never replacing that
// ordinary outbox or extending its accounting authority.
func (w *Worker) usageWithRunway(ctx context.Context, x Exchange, report ix.UsageReport) (ix.UsageReceipt, error) {
	if w.process == nil || w.state.Epoch == nil || report.Final ||
		w.state.FinalAccepted || w.state.Phase != "active" || w.deadline.IsZero() {
		return x.Usage(ctx, report)
	}
	type outcome struct {
		receipt ix.UsageReceipt
		err     error
	}
	done := make(chan outcome, 1)
	go func() { receipt, err := x.Usage(ctx, report); done <- outcome{receipt, err} }()
	timer := time.NewTimer(time.Until(w.deadline) - 12*time.Second)
	defer timer.Stop()
	select {
	case result := <-done:
		return result.receipt, result.err
	case <-timer.C:
	}
	finalCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	captureErr := w.captureFinal(finalCtx, diagnosticLeaseRunway)
	cancel()
	// Join even when capture fails. Do not send another report while the first
	// may still commit, and do not let the exchange outlive its worker state.
	result := <-done
	if captureErr != nil {
		return ix.UsageReceipt{}, captureErr
	}
	return result.receipt, result.err
}
