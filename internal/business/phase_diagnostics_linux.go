//go:build linux

package business

import (
	"sync/atomic"

	"golang.org/x/sys/unix"
)

const phaseDiagnosticJournalPath = "/run/systemd/journal/socket"

type phaseDiagnosticJournal struct {
	fd      int
	address unix.Sockaddr
	closed  atomic.Bool
}

func openPhaseDiagnosticJournal() *phaseDiagnosticJournal {
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	return &phaseDiagnosticJournal{fd: fd, address: &unix.SockaddrUnix{Name: phaseDiagnosticJournalPath}}
}

func (j *phaseDiagnosticJournal) send(packet []byte) bool {
	if j == nil || j.closed.Load() || len(packet) == 0 || len(packet) > diagnosticPacketLimit {
		return false
	}
	return unix.Sendto(j.fd, packet, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL, j.address) == nil
}

func (j *phaseDiagnosticJournal) close() {
	if j != nil && j.closed.CompareAndSwap(false, true) {
		_ = unix.Close(j.fd)
	}
}
