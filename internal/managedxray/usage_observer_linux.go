//go:build linux

package managedxray

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

var ErrUsageSettlement = errors.New("previous observed Xray epoch requires explicit usage settlement")

// UsageObservation is an INTERNAL supervisor hook after the HUB has registered
// this exact epoch. It does not authorize traffic, register a baseline, declare
// user isolation, or lift the public metered-runtime admission fence.
type UsageObservation struct {
	Epoch         uc.Epoch
	StatsEndpoint string
	Sender        *uc.Sender
	Interval      time.Duration
	MaxSampleGap  time.Duration
	MaxPendingAge time.Duration
}
type observedUsage struct {
	Epoch         uc.Epoch `json:"epoch"`
	StatsEndpoint string   `json:"stats_endpoint"`
}
type UsageObservationStatus struct {
	Required                 bool   `json:"required"`
	Running                  bool   `json:"running"`
	Samples                  uint64 `json:"samples"`
	Receipts                 uint64 `json:"receipts"`
	Pending                  int    `json:"pending"`
	LastSavedUnixNano        int64  `json:"last_saved_unix_nano"`
	Failure                  string `json:"failure,omitempty"`
	InterruptionAcknowledged bool   `json:"interruption_acknowledged"`
	SettlementRequired       bool   `json:"settlement_required"`
}
type usageObserver struct {
 fresh *uc.FreshCounters
 ready chan struct{}
 readyOnce sync.Once
	mu           sync.Mutex
	status       UsageObservationStatus
	lastSaved    time.Time
	pendingSince time.Time
	cancel       context.CancelFunc
	done         chan struct{}
	process      *child
	box          *uc.Outbox
	options      UsageObservation
	binary, pin  string
}

func usageKey(key []byte, epoch string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("DARK-PRO-RUNTIME-USAGE-OUTBOX-v1\x00" + epoch))
	return m.Sum(nil)
}
func usageDirectory(dir, epoch string) string { return filepath.Join(dir, "usage", epoch) }
func validateObservation(o UsageObservation) (UsageObservation, error) {
	epoch, _, err := uc.CanonicalEpoch(o.Epoch)
	if err != nil {
		return o, err
	}
	o.Epoch = epoch
	if _, err = uc.StatsArguments(o.StatsEndpoint); err != nil {
		return o, err
	}
	if o.Sender == nil {
		return o, ErrInvalid
	}
	if o.Interval == 0 {
		o.Interval = 2 * time.Second
	}
	if o.MaxSampleGap == 0 {
		o.MaxSampleGap = 12 * time.Second
	}
	if o.MaxPendingAge == 0 {
		o.MaxPendingAge = 20 * time.Second
	}
	if o.Interval < 50*time.Millisecond || o.Interval > 5*time.Second || o.MaxSampleGap < 2*o.Interval || o.MaxSampleGap < time.Second || o.MaxSampleGap > 15*time.Second || o.MaxPendingAge < time.Second || o.MaxPendingAge > 40*time.Second {
		return o, ErrInvalid
	}
	return o, nil
}

// Verify the epoch against this runtime, not an arbitrary same-host PID or a
// subset of the configured user counter names. Stats aggregate counters never
// substitute for a missing user. An idle missing counter remains incomplete.
func (r *Runtime) checkObservationLocked(o UsageObservation) error {
	if r.closed || r.fenced || r.active == nil || r.active.NoListeners || r.process.dead() || !time.Now().Before(r.deadline) {
		return uc.ErrEpoch
	}
	e := o.Epoch
	if e.NodeID != r.options.NodeID || e.InstallationID != r.options.InstallationID || e.ConfigSHA != r.active.SHA {
		return uc.ErrEpoch
	}
	p, err := uc.Process(r.process.cmd.Process.Pid)
	if err != nil || p.PID != e.PID || p.BootID != e.KernelBootID || p.StartTicks != e.ProcessStartTicks {
		return uc.ErrEpoch
	}
	var config struct {
		API struct {
			Listen   string   `json:"listen"`
			Services []string `json:"services"`
		} `json:"api"`
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					Email string `json:"email"`
				} `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(r.active.Raw, &config) != nil || config.API.Listen != o.StatsEndpoint {
		return uc.ErrEpoch
	}
	service := false
	for _, v := range config.API.Services {
		service = service || v == "StatsService"
	}
	if !service {
		return uc.ErrEpoch
	}
	names := map[string]bool{}
	for _, in := range config.Inbounds {
		if in.Protocol != "vless" {
			return uc.ErrEpoch
		}
		for _, u := range in.Settings.Clients {
			if u.Email == "" {
				return uc.ErrEpoch
			}
			names[u.Email] = true
		}
	}
	if len(names) != len(e.Bindings) {
		return uc.ErrEpoch
	}
	for _, binding := range e.Bindings {
		name, err := uc.CounterEmail(binding.ClientID)
		if err != nil || !binding.Billable || binding.Email != name || !names[binding.Email] {
			return uc.ErrEpoch
		}
	}
	return nil
}
func (r *Runtime) StartUsageObservation(ctx context.Context, o UsageObservation) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrInvalid
	}
	normalized, err := validateObservation(o)
	if err != nil {
		return err
	}
	o = normalized
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Admission != nil || r.state.Usage != nil || r.observer != nil {
		return ErrUsageSettlement
	}
	if err = r.checkObservationLocked(o); err != nil {
		return err
	}
	// Durable intent BEFORE creating a new outbox. A crash in either operation
	// must leave a recovery fence, not create a fresh zero-based epoch.
	next := r.state
	next.Version = 2
	next.Usage = &observedUsage{Epoch: o.Epoch, StatsEndpoint: o.StatsEndpoint}
	if err = r.persist(next); err != nil {
		return err
	}
	box, err := uc.Open(uc.Options{Directory: usageDirectory(r.options.Directory, o.Epoch.ID), Key: usageKey(r.options.StateKey, o.Epoch.ID), Epoch: o.Epoch, Create: true})
	if err != nil {
		r.process.stop()
		r.process = nil
		r.active = nil
		r.state.Phase = "usage_outbox_failed_stopped"
		return err
	}
	observerCtx, cancel := context.WithCancel(ctx)
	observer := &usageObserver{options: o, process: r.process, box: box, binary: r.options.Binary, pin: r.options.BinarySHA, cancel: cancel, done: make(chan struct{}), lastSaved: time.Now(), status: UsageObservationStatus{Required: true, Running: true, SettlementRequired: true}}
	r.observer = observer
	go observer.run(observerCtx)
	return nil
}
func (m *usageObserver) snapshot() UsageObservationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}
func (m *usageObserver) healthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.Running && m.status.Failure == "" && time.Since(m.lastSaved) < m.options.MaxSampleGap && (m.pendingSince.IsZero() || time.Since(m.pendingSince) < m.options.MaxPendingAge)
}
func (m *usageObserver) fail(reason string) {
	m.mu.Lock()
	if m.status.Failure == "" {
		m.status.Failure = reason
	}
	m.mu.Unlock()
	m.cancel()
	// The child MUST exit before Running=false becomes observable. Otherwise
	// concurrent status readers can see a stopped observer while its Xray
	// listener is still accepting traffic. This is a fail-closed receipt, not
	// merely a requested termination. Never kill any unrelated process.
	m.process.stop()
	m.mu.Lock()
	m.status.Running = false
	m.mu.Unlock()
}
func (m *usageObserver) pending(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Pending = n
	if n == 0 {
		m.pendingSince = time.Time{}
	} else if m.pendingSince.IsZero() {
		m.pendingSince = time.Now()
	}
}
func (m *usageObserver) run(ctx context.Context) {
	guardDone := make(chan struct{})
	go func() {
		defer close(guardDone)
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				m.fail("observer_stopped")
				return
			case <-m.process.done:
				m.fail("process_exited")
				return
			case <-tick.C:
				m.mu.Lock()
				sampleStale := time.Since(m.lastSaved) >= m.options.MaxSampleGap
				pendingStale := !m.pendingSince.IsZero() && time.Since(m.pendingSince) >= m.options.MaxPendingAge
				m.mu.Unlock()
				if sampleStale {
					m.fail("sample_deadline")
					return
				}
				if pendingStale {
					m.fail("receipt_deadline")
					return
				}
			}
		}
	}()
	defer func() {
		m.cancel()
		<-guardDone
		call, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := m.options.Sender.NotifyInterrupted(call, m.options.Epoch)
		cancel()
		if err == nil {
			m.mu.Lock()
			m.status.InterruptionAcknowledged = true
			m.mu.Unlock()
		}
		_ = m.box.Close()
		close(m.done)
	}()
	tick := time.NewTicker(m.options.Interval)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		var values []uc.Usage
        var err error
        if m.fresh!=nil { values,err=m.fresh.Collect(ctx,m.binary,m.pin,m.options.StatsEndpoint) } else { values,err=uc.Collect(ctx,m.binary,m.pin,m.options.StatsEndpoint,m.options.Epoch) }
		if err != nil {
			if ctx.Err() == nil {
				m.fail("observation_failed")
			}
			return
		}
		if _, err = m.box.Observe(values, false); err != nil {
			m.fail("checkpoint_failed")
			return
		}
		m.mu.Lock()
		m.lastSaved = time.Now()
		m.status.LastSavedUnixNano = m.lastSaved.UnixNano()
		m.status.Samples++
		m.mu.Unlock()
        if m.ready!=nil{m.readyOnce.Do(func(){close(m.ready)})}
		queue, err := m.box.Pending()
		if err != nil {
			m.fail("outbox_failed")
			return
		}
		m.pending(len(queue))
		// A short network deadline keeps slow HUB responses from monopolizing the
		// sampling loop. The independent guard still enforces both age bounds.
		sendCtx, cancel := context.WithTimeout(ctx, time.Second)
		var sendErr error
		for i := 0; i < uc.MaxPending && sendCtx.Err() == nil; i++ {
			var sent bool
			sent, sendErr = m.options.Sender.FlushOne(sendCtx, m.box)
			if sendErr != nil || !sent {
				break
			}
			m.mu.Lock()
			m.status.Receipts++
			m.mu.Unlock()
		}
		cancel()
		if sendErr == nil {
			queue, err = m.box.Pending()
			if err != nil {
				m.fail("outbox_failed")
				return
			}
			m.pending(len(queue))
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (r *Runtime) usageStatusLocked() UsageObservationStatus {
	if r.state.Usage == nil {
        if r.state.Admission!=nil{return UsageObservationStatus{Required:true,SettlementRequired:true,Failure:"prepared_admission_incomplete"}}
		return UsageObservationStatus{}
	}
	if r.observer != nil {
		return r.observer.snapshot()
	}
	return UsageObservationStatus{Required: true, SettlementRequired: true, Failure: "previous_epoch_requires_settlement"}
}
func (r *Runtime) UsageStatus() UsageObservationStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.usageStatusLocked()
}

// ReplayObservedUsage only drains PREVIOUSLY persisted observations after a
// restart. It does not read dead Xray counters, fabricate a final sample, seal
// the epoch, clear the settlement fence, or authorize a recovered listener.
func (r *Runtime) ReplayObservedUsage(ctx context.Context, sender *uc.Sender) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx == nil || sender == nil || r.closed || r.fenced || r.observer != nil || !r.process.dead() || r.state.Usage == nil {
		return 0, ErrInvalid
	}
	u := r.state.Usage
	if err := sender.NotifyInterrupted(ctx, u.Epoch); err != nil {
		return 0, err
	}
	box, err := uc.Open(uc.Options{Directory: usageDirectory(r.options.Directory, u.Epoch.ID), Key: usageKey(r.options.StateKey, u.Epoch.ID), Epoch: u.Epoch, Create: false})
	if err != nil {
		return 0, err
	}
	defer box.Close()
	sent := 0
	for i := 0; i < uc.MaxPending; i++ {
		ok, err := sender.FlushOne(ctx, box)
		if err != nil {
			return sent, err
		}
		if !ok {
			return sent, nil
		}
		sent++
	}
	return sent, nil
}
