//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// Diagnostics holds bounded, process-lifetime counters. Recording performs no
// I/O and calls no observer. Counters are not persisted with accounting state.
type Diagnostics struct {
	cells          [diagnosticStageCount][diagnosticReasonCount]diagnosticCell
	exporterActive atomic.Bool
}

type diagnosticCell struct {
	count atomic.Uint64
	last  atomic.Int64
}

// DiagnosticCount contains only fixed vocabulary and numeric metadata. Snapshot
// is eventually consistent; the count and timestamp are not an atomic pair.
// Stop-request stages count decisions, not proof that a signal caused an exit.
// final_receipt/succeeded means a matching HUB receipt was received, before the
// subsequent local save. final_capture/succeeded means the final state was saved.
type DiagnosticCount struct {
	Stage        string
	Reason       string
	Count        uint64
	LastUnixNano int64
}

type diagnosticStage uint8

const (
	diagnosticStep diagnosticStage = iota
	diagnosticPoll
	diagnosticRegister
	diagnosticUsage
	diagnosticObserve
	diagnosticCounters
	diagnosticHealth
	diagnosticDrain
	diagnosticFinalCapture
	diagnosticFinalReceipt
	diagnosticWorkerStop
	diagnosticNativeStop
	diagnosticCoreState
	diagnosticStageCount
)

var diagnosticStageNames = [...]string{
	"step", "poll", "register", "usage", "observe", "counters", "health",
	"drain", "final_capture", "final_receipt", "worker_stop_request", "native_stop_request", "core_state",
}

type diagnosticReason uint8

const (
	diagnosticDeadline diagnosticReason = iota
	diagnosticCanceled
	diagnosticTransportTimeout
	diagnosticTransport
	diagnosticProof
	diagnosticIdentity
	diagnosticPersistence
	diagnosticUncertain
	diagnosticRuntime
	diagnosticLeaseError
	diagnosticOther
	diagnosticLeaseRunway
	diagnosticLeaseExpired
	diagnosticDeliveryAbsent
	diagnosticWithdrawal
	diagnosticGenerationChange
	diagnosticBundleChange
	diagnosticCounterFailure
	diagnosticShutdown
	diagnosticWorkerClose
	diagnosticRenewalRefused
	diagnosticAliasFence
	diagnosticRecovery
	diagnosticFinalAccepted
	diagnosticProcessLost
	diagnosticSucceeded
	diagnosticStopRequested
	diagnosticReasonCount
)

var diagnosticReasonNames = [...]string{
	"deadline", "canceled", "transport_timeout", "transport", "proof", "identity",
	"persistence", "uncertain", "runtime", "lease_error", "other", "lease_runway",
	"lease_expired", "delivery_absent", "withdrawal", "generation_change",
	"bundle_change", "counter_failure", "shutdown", "worker_close",
	"renewal_refused", "alias_fence", "recovery", "final_accepted", "process_lost",
	"succeeded", "stop_requested",
}

func (d *Diagnostics) record(stage diagnosticStage, reason diagnosticReason) {
	if d == nil || stage >= diagnosticStageCount || reason >= diagnosticReasonCount {
		return
	}
	cell := &d.cells[stage][reason]
	cell.last.Store(time.Now().UnixNano())
	cell.count.Add(1)
}

func diagnosticErrorReason(err error) diagnosticReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return diagnosticDeadline
	case errors.Is(err, context.Canceled):
		return diagnosticCanceled
	case errors.Is(err, ErrProof):
		return diagnosticProof
	case errors.Is(err, ErrIdentity):
		return diagnosticIdentity
	case errors.Is(err, ErrPersistence):
		return diagnosticPersistence
	case errors.Is(err, ErrUncertain):
		return diagnosticUncertain
	case errors.Is(err, ErrLease):
		return diagnosticLeaseError
	case errors.Is(err, ErrRuntime):
		return diagnosticRuntime
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return diagnosticTransportTimeout
		}
		return diagnosticTransport
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return diagnosticTransport
	}
	// Never call Error(), print a URL, or place an error string in a label.
	return diagnosticOther
}

func (d *Diagnostics) recordError(stage diagnosticStage, err error) {
	if d != nil && err != nil {
		d.record(stage, diagnosticErrorReason(err))
	}
}

// Snapshot returns cumulative counters, never resetting them. A slow or missing
// reader cannot consume, queue, or block a runtime event.
func (d *Diagnostics) Snapshot() []DiagnosticCount {
	if d == nil {
		return nil
	}
	var out []DiagnosticCount
	for stage := diagnosticStage(0); stage < diagnosticStageCount; stage++ {
		for reason := diagnosticReason(0); reason < diagnosticReasonCount; reason++ {
			cell := &d.cells[stage][reason]
			if count := cell.count.Load(); count != 0 {
				out = append(out, DiagnosticCount{
					Stage: diagnosticStageNames[stage], Reason: diagnosticReasonNames[reason],
					Count: count, LastUnixNano: cell.last.Load(),
				})
			}
		}
	}
	return out
}

const (
	diagnosticJournalPath = "/run/systemd/journal/socket"
	diagnosticPacketLimit = 512
	diagnosticInterval    = time.Second
)

// This concrete transport has no arbitrary writer callback. The production
// destination is fixed, local, and nonblocking; it cannot hold up lease handling.
type diagnosticJournal struct {
	fd   int
	addr unix.Sockaddr
}

func openDiagnosticJournal() (*diagnosticJournal, error) {
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return &diagnosticJournal{fd: fd, addr: &unix.SockaddrUnix{Name: diagnosticJournalPath}}, nil
}

func (j *diagnosticJournal) send(packet []byte) bool {
	if len(packet) == 0 || len(packet) > diagnosticPacketLimit {
		return false
	}
	return unix.Sendto(j.fd, packet, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL, j.addr) == nil
}

func diagnosticPacket(stage diagnosticStage, reason diagnosticReason, count uint64, last int64) []byte {
	if stage >= diagnosticStageCount || reason >= diagnosticReasonCount {
		return nil
	}
	return fmt.Appendf(nil,
		"PRIORITY=5\nSYSLOG_IDENTIFIER=dark-pro-inbound-runtime\nMESSAGE=managed_inbound_runtime stage=%s reason=%s count=%d last_unix_nano=%d\n",
		diagnosticStageNames[stage], diagnosticReasonNames[reason], count, last)
}

type diagnosticSent [diagnosticStageCount][diagnosticReasonCount]uint64

func (d *Diagnostics) flushJournal(j *diagnosticJournal, sent *diagnosticSent) {
	for stage := diagnosticStage(0); stage < diagnosticStageCount; stage++ {
		for reason := diagnosticReason(0); reason < diagnosticReasonCount; reason++ {
			cell := &d.cells[stage][reason]
			count := cell.count.Load()
			if count == sent[stage][reason] {
				continue
			}
			if j.send(diagnosticPacket(stage, reason, count, cell.last.Load())) {
				sent[stage][reason] = count
			}
			// Failed sends retain only cumulative counters. No event queue grows.
		}
	}
}

func (d *Diagnostics) runJournal(ctx context.Context, j *diagnosticJournal) {
	tick := time.NewTicker(diagnosticInterval)
	defer tick.Stop()
	var sent diagnosticSent
	for {
		select {
		case <-ctx.Done():
			// A fixed number of nonblocking sends also captures shutdown events.
			d.flushJournal(j, &sent)
			return
		case <-tick.C:
			d.flushJournal(j, &sent)
		}
	}
}

// startJournal owns exactly one exporter until Run finishes. It neither uses the
// global logger nor waits on arbitrary output. Close/shutdown events are recorded
// before the final nonblocking flush, then the goroutine and socket are joined.
func (d *Diagnostics) startJournal() func() {
	if d == nil || !d.exporterActive.CompareAndSwap(false, true) {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		journal, err := openDiagnosticJournal()
		if err != nil {
			return
		}
		defer unix.Close(journal.fd)
		d.runJournal(ctx, journal)
	}()
	var stop sync.Once
	return func() {
		stop.Do(func() {
			cancel()
			<-done
			d.exporterActive.Store(false)
		})
	}
}
