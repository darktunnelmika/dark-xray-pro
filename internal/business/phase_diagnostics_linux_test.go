//go:build linux

package business

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var phaseDiagnosticTestSocketSequence atomic.Uint64

// Abstract Unix sockets are owned solely by this test and never touch the
// system journal, service configuration, filesystem, network or application DB.
func testPhaseDiagnosticJournal(t *testing.T) (int, *phaseDiagnosticJournal) {
	t.Helper()
	receiver, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal("test receiver socket failed")
	}
	t.Cleanup(func() { _ = unix.Close(receiver) })
	address := &unix.SockaddrUnix{Name: fmt.Sprintf("@dark-pro-phase-test-%d-%d", os.Getpid(), phaseDiagnosticTestSocketSequence.Add(1))}
	if unix.Bind(receiver, address) != nil {
		t.Fatal("test socket bind failed")
	}
	if unix.SetsockoptInt(receiver, unix.SOL_SOCKET, unix.SO_RCVBUF, 1024) != nil {
		t.Fatal("test socket buffer bound failed")
	}
	sender, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal("test sender socket failed")
	}
	journal := &phaseDiagnosticJournal{fd: sender, address: address}
	t.Cleanup(journal.close)
	return receiver, journal
}

func fillPhaseDiagnosticSocket(t *testing.T, journal *phaseDiagnosticJournal) {
	t.Helper()
	packet := make([]byte, diagnosticPacketLimit)
	for n := 0; n < 4096; n++ {
		err := unix.Sendto(journal.fd, packet, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL, journal.address)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return
		}
		if err != nil {
			t.Fatal("unexpected test socket send failure")
		}
	}
	t.Fatal("owned socket did not reach bounded backpressure fixture")
}

func TestBusinessPhaseDiagnosticsFullSocketDoesNotBlock(t *testing.T) {
	_, journal := testPhaseDiagnosticJournal(t)
	fillPhaseDiagnosticSocket(t, journal)
	s := diagnosticTestStore()
	d := s.phaseDiagnostics
	for n := 0; n < diagnosticQueueCapacity; n++ {
		d.record(diagnosticTestRecord())
	}
	completed := make(chan struct{})
	go func() {
		_, span := s.BeginDiagnostic(context.Background(), DiagnosticOperationUsage, DiagnosticSourceHubLocal)
		span.Enter(DiagnosticPhaseCommit)
		span.End(nil)
		d.flushEvents(journal)
		close(completed)
	}()
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("recording or nonblocking send waited for the receiver")
	}
	stats := d.snapshot()
	if stats.transportDropped != diagnosticBatchLimit || stats.sent != 0 || stats.queueFull != 5 || stats.active != 0 {
		t.Fatal("full sink loss/current accounting is incorrect")
	}
}

func TestBusinessPhaseDiagnosticsExporterCancellation(t *testing.T) {
	_, journal := testPhaseDiagnosticJournal(t)
	fillPhaseDiagnosticSocket(t, journal)
	d := newPhaseDiagnostics(nil)
	for n := 0; n < diagnosticQueueCapacity; n++ {
		d.record(diagnosticTestRecord())
	}
	if !d.startExporter(journal) || d.startExporter(journal) {
		t.Fatal("exporter start was not singular")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.stopExporter(ctx)
	select {
	case <-d.done:
	case <-ctx.Done():
		t.Fatal("full sink prevented exporter cancellation")
	}
	d.stopExporter(ctx)
	stats := d.snapshot()
	if !stats.closed || stats.active != 0 || stats.pending != 0 || stats.shutdownDropped+stats.transportDropped != diagnosticQueueCapacity || !journal.closed.Load() || d.startExporter(journal) {
		t.Fatal("cancellation did not discard bounded output and close only its socket")
	}
}

func TestBusinessPhaseDiagnosticsConcurrentShutdownAccounting(t *testing.T) {
	_, journal := testPhaseDiagnosticJournal(t)
	d := newPhaseDiagnostics(nil)
	if !d.startExporter(journal) {
		t.Fatal("test exporter start failed")
	}
	var producers sync.WaitGroup
	start := make(chan struct{})
	for n := 0; n < 8; n++ {
		producers.Add(1)
		go func() {
			defer producers.Done()
			<-start
			for k := 0; k < 1000; k++ {
				d.record(diagnosticTestRecord())
			}
		}()
	}
	close(start)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.stopExporter(ctx)
	producers.Wait()
	select {
	case <-d.done:
	case <-ctx.Done():
		t.Fatal("exporter did not join")
	}
	s := d.snapshot()
	if s.attempted != 8000 || s.producers != 0 || s.pending > diagnosticQueueCapacity || s.attempted != s.enqueued+s.queueFull+s.closedDropped || s.enqueued != s.sent+s.transportDropped+s.formatDropped+s.shutdownDropped+uint64(s.pending) {
		t.Fatal("completed producer accounting lost a record during shutdown")
	}
}

func TestBusinessPhaseDiagnosticsCanceledStopIsBounded(t *testing.T) {
	// Model an exporter not yet scheduled. Stop must honor cancellation without
	// waiting for that goroutine, a socket, a pool snapshot or any output drain.
	d := newPhaseDiagnostics(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.stopExporter(ctx)
	if !d.closed.Load() || d.snapshot().stopTimeout != 1 {
		t.Fatal("canceled stop was not explicitly incomplete")
	}
	close(d.done)
	d.stopExporter(context.Background())
	if d.snapshot().stopTimeout != 1 {
		t.Fatal("completed idempotent stop added a timeout")
	}
}
