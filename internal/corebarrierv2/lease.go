package corebarrierv2

import (
	"context"
	"os"
	"strconv"
	"time"
)

const AutonomousLeaseCapability = Protocol + "autonomous-lease-final-v1"
const FinalRecordVersion = 1
const FinalRecordLimit = 8 << 20
const LeaseFinalRunway = 12 * time.Second

// FinalRecord is issued only after admission/resources and all counted I/O have
// joined. The kernel seals its anonymous capture descriptor against every write.
// This is a process-lifetime counter capture, never a durable accounting journal.
type FinalRecord struct {
	Version    int              `json:"version"`
	Final      bool             `json:"final"`
	PID        int              `json:"pid"`
	Binding    string           `json:"binding"`
	ConfigSHA  string           `json:"config_sha"`
	Nonce      string           `json:"nonce"`
	Autonomous bool             `json:"autonomous"`
	Counters   map[string]int64 `json:"counters"`
}
type finalCapture interface {
	Publish(FinalRecord) error
	Binding() string
	ConfigSHA() string
}
type nativeLease struct {
	hard         time.Time
	wireDeadline int64
	renewNonce   string
	serial       uint64
	timer        *time.Timer
	read         func() map[string]int64
	capture      finalCapture
	sealQueued   bool
}

func (g *gate) configureLeaseFromEnvironment() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.leaseConfigured {
		return
	}
	g.leaseConfigured = true
	fd := os.Getenv("DARK_PRO_NATIVE_FINAL_FD")
	binding := os.Getenv("DARK_PRO_NATIVE_FINAL_BINDING")
	config := os.Getenv("DARK_PRO_NATIVE_CONFIG_SHA256")
	deadline := os.Getenv("DARK_PRO_NATIVE_LEASE_DEADLINE")
	if fd == "" && binding == "" && config == "" && deadline == "" {
		return
	}
	n, err := strconv.ParseInt(deadline, 10, 64)
	now := time.Now()
	duration := time.Unix(0, n).Sub(now)
	if err != nil || !validNonce(binding) || !validNonce(config) || duration <= 0 || duration > 31*time.Second {
		g.leaseErr = ErrBarrier
		return
	}
	capture, err := openFinalCapture(fd, binding, config)
	if err != nil {
		g.leaseErr = ErrBarrier
		return
	}
	g.lease = &nativeLease{hard: now.Add(duration), wireDeadline: n, capture: capture}
	g.armLeaseLocked()
}
func AutonomousLeaseEnabled() bool {
	g := processGate
	g.configureLeaseFromEnvironment()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lease != nil && g.leaseErr == nil
}
func RegisterLeaseSnapshotter(read func() map[string]int64) {
	g := processGate
	g.configureLeaseFromEnvironment()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lease != nil && g.lease.read == nil && read != nil {
		g.lease.read = read
	}
}
func (g *gate) armLeaseLocked() {
	l := g.lease
	if l.timer != nil {
		l.timer.Stop()
	}
	l.serial++
	serial := l.serial
	l.timer = time.AfterFunc(time.Until(l.hard.Add(-LeaseFinalRunway)), func() {
		g.mu.Lock()
		if g.lease == l && l.serial == serial && !l.sealQueued {
			l.sealQueued = true
			g.mu.Unlock()
			g.sealLease(serial)
			return
		}
		g.mu.Unlock()
	})
}
func (g *gate) leasePermitsLocked() bool {
	if g.leaseErr != nil {
		return false
	}
	l := g.lease
	if l == nil {
		return true
	}
	if time.Now().Before(l.hard.Add(-LeaseFinalRunway)) {
		return true
	}
	if !l.sealQueued {
		l.sealQueued = true
		serial := l.serial
		go g.sealLease(serial)
	}
	return false
}
func (g *gate) sealLease(serial uint64) {
	g.mu.Lock()
	l := g.lease
	if l == nil || l.serial != serial {
		g.mu.Unlock()
		return
	}
	read := l.read
	g.mu.Unlock()
	_, _ = g.snapshotInternal(context.Background(), "", read, true)
}
func RenewAutonomousLease(ctx context.Context, deadline int64, nonce string) error {
	return processGate.renewAutonomousLease(ctx, deadline, nonce)
}
func (g *gate) renewAutonomousLease(ctx context.Context, deadline int64, nonce string) error {
	if ctx == nil || ctx.Err() != nil || !validNonce(nonce) {
		return ErrBarrier
	}
	now := time.Now()
	duration := time.Unix(0, deadline).Sub(now)
	if duration <= 0 || duration > 31*time.Second {
		return ErrBarrier
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	l := g.lease
	if l == nil || g.closing || !g.leasePermitsLocked() {
		return ErrBarrier
	}
	if l.renewNonce == nonce {
		if l.wireDeadline == deadline {
			return nil
		}
		return ErrBarrier
	}
	l.hard = now.Add(duration)
	l.wireDeadline = deadline
	l.renewNonce = nonce
	g.armLeaseLocked()
	return nil
}
func (g *gate) publishFinalLocked() {
	if g.lease == nil || g.values == nil || g.err != nil {
		return
	}
	r := FinalRecord{Version: FinalRecordVersion, Final: true, PID: os.Getpid(), Binding: g.lease.capture.Binding(), ConfigSHA: g.lease.capture.ConfigSHA(), Nonce: g.nonce, Autonomous: g.autoSealed, Counters: clone(g.values)}
	// Capture failure never invents counters. A live authenticated RPC may still
	// return the genuine in-memory snapshot; exit recovery requires kernel seals.
	_ = g.lease.capture.Publish(r)
}
