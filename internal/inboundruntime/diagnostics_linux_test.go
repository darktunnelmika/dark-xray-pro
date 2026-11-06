//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type diagnosticHostileError struct{}

func (diagnosticHostileError) Error() string { panic("diagnostics formatted a sensitive error") }

type diagnosticTimeoutError struct{ diagnosticHostileError }

func (diagnosticTimeoutError) Timeout() bool   { return true }
func (diagnosticTimeoutError) Temporary() bool { return true }

func diagnosticCount(d *Diagnostics, stage diagnosticStage, reason diagnosticReason) uint64 {
	return d.cells[stage][reason].count.Load()
}

func TestDiagnosticsRedactErrorsAndBoundVocabulary(t *testing.T) {
	d := &Diagnostics{}
	tests := []struct {
		err  error
		want diagnosticReason
	}{
		{&url.Error{Op: "GET", URL: "https://private.invalid/customer?token=secret", Err: context.DeadlineExceeded}, diagnosticDeadline},
		{context.Canceled, diagnosticCanceled},
		{diagnosticTimeoutError{}, diagnosticTransportTimeout},
		{diagnosticHostileError{}, diagnosticOther},
		{ErrProof, diagnosticProof},
		{ErrPersistence, diagnosticPersistence},
	}
	for _, tc := range tests {
		d.recordError(diagnosticPoll, tc.err)
		if diagnosticCount(d, diagnosticPoll, tc.want) != 1 {
			t.Fatalf("wrong fixed classification: %d", tc.want)
		}
	}
	d.recordError(diagnosticPoll, nil)
	before := d.Snapshot()
	d.record(diagnosticStage(255), diagnosticOther)
	d.record(diagnosticStep, diagnosticReason(255))
	if len(d.Snapshot()) != len(before) {
		t.Fatal("invalid vocabulary expanded diagnostics")
	}
	for stage := diagnosticStage(0); stage < diagnosticStageCount; stage++ {
		for reason := diagnosticReason(0); reason < diagnosticReasonCount; reason++ {
			p := diagnosticPacket(stage, reason, math.MaxUint64, math.MinInt64)
			if len(p) == 0 || len(p) > diagnosticPacketLimit {
				t.Fatal("unbounded diagnostic packet")
			}
			for _, private := range []string{"private.invalid", "customer", "token=", "secret", "node_id", "installation_id", "epoch_id", "config="} {
				if strings.Contains(string(p), private) {
					t.Fatal("private data entered fixed diagnostic packet")
				}
			}
		}
	}
	for _, item := range before {
		if item.Count != 1 || item.LastUnixNano <= 0 {
			t.Fatal("invalid cumulative snapshot")
		}
	}
}

func TestDiagnosticsCountersDoNotDependOnReader(t *testing.T) {
	d := &Diagnostics{}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 1000 {
				d.record(diagnosticDrain, diagnosticLeaseRunway)
			}
		}()
	}
	workers.Wait()
	for range 3 {
		s := d.Snapshot()
		if len(s) != 1 || s[0].Count != 8000 {
			t.Fatal("snapshot lost or consumed counters")
		}
	}
}

func diagnosticSocketPair(t *testing.T) (*diagnosticJournal, int) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fds[0]); unix.Close(fds[1]) })
	return &diagnosticJournal{fd: fds[0]}, fds[1]
}

func TestDiagnosticsMissingSinkRetainsCounters(t *testing.T) {
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	sink := &diagnosticJournal{fd: fd, addr: &unix.SockaddrUnix{Name: filepath.Join(t.TempDir(), "missing.socket")}}
	d := &Diagnostics{}
	d.record(diagnosticDrain, diagnosticCounterFailure)
	var sent diagnosticSent
	d.flushJournal(sink, &sent)
	if sent[diagnosticDrain][diagnosticCounterFailure] != 0 || diagnosticCount(d, diagnosticDrain, diagnosticCounterFailure) != 1 {
		t.Fatal("missing journal consumed counters")
	}
}

func TestDiagnosticsFullSinkNeverBlocksAndRecoversCumulativeCount(t *testing.T) {
	sink, receiver := diagnosticSocketPair(t)
	if err := unix.SetsockoptInt(sink.fd, unix.SOL_SOCKET, unix.SO_SNDBUF, 1024); err != nil {
		t.Fatal(err)
	}
	full := false
	for range 8192 {
		err := unix.Sendto(sink.fd, []byte("filler"), unix.MSG_DONTWAIT, nil)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			full = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !full {
		t.Fatal("fixture did not fill the socket")
	}
	d := &Diagnostics{}
	for range 7 {
		d.record(diagnosticDrain, diagnosticLeaseRunway)
	}
	var sent diagnosticSent
	done := make(chan struct{})
	go func() {
		d.flushJournal(sink, &sent)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("full journal blocked diagnostics")
	}
	if sent[diagnosticDrain][diagnosticLeaseRunway] != 0 || diagnosticCount(d, diagnosticDrain, diagnosticLeaseRunway) != 7 {
		t.Fatal("full sink consumed accounting-independent counters")
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { d.runJournal(ctx, sink); close(stopped) }()
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("full journal blocked exporter shutdown")
	}
	buf := make([]byte, diagnosticPacketLimit)
	for {
		_, _, err := unix.Recvfrom(receiver, buf, unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	d.flushJournal(sink, &sent)
	n, _, err := unix.Recvfrom(receiver, buf, unix.MSG_DONTWAIT)
	if err != nil || !strings.Contains(string(buf[:n]), "count=7 ") {
		t.Fatal("recovered sink did not receive cumulative total", err)
	}
	d.flushJournal(sink, &sent)
	if _, _, err := unix.Recvfrom(receiver, buf, unix.MSG_DONTWAIT); !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
		t.Fatal("unchanged counters were emitted again", err)
	}
}

func TestDiagnosticsExporterCancellationWithUnusableSink(t *testing.T) {
	d := &Diagnostics{}
	d.record(diagnosticDrain, diagnosticShutdown)
	// An invalid descriptor is permanently unavailable and cannot be waited on.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.runJournal(ctx, &diagnosticJournal{fd: -1})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("unavailable sink blocked exporter shutdown")
	}
	if diagnosticCount(d, diagnosticDrain, diagnosticShutdown) != 1 {
		t.Fatal("shutdown discarded cumulative diagnostic counter")
	}
}

func TestDiagnosticsOnlyOneExporterAndJoinedLifetime(t *testing.T) {
	d := &Diagnostics{}
	stopFirst := d.startJournal()
	if !d.exporterActive.Load() {
		t.Fatal("exporter ownership absent")
	}
	stopDuplicate := d.startJournal()
	stopDuplicate()
	if !d.exporterActive.Load() {
		t.Fatal("duplicate Run stopped the original exporter")
	}
	done := make(chan struct{})
	go func() { stopFirst(); stopFirst(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exporter was not joined")
	}
	if d.exporterActive.Load() {
		t.Fatal("joined exporter retained ownership")
	}
	// This exporter has no events, so it never sends to the system journal.
	stopNext := d.startJournal()
	stopNext()
	if d.exporterActive.Load() {
		t.Fatal("later owner was not released")
	}
}
