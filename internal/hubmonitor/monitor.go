// Package hubmonitor samples the NEW PRO HUB's own Linux host and process.
// Unlike Node fleet telemetry, it reads only its local /proc filesystem.
// No billing, Xray connection data, passwords, client IDs or network calls.
package hubmonitor

import (
	"context"
	"errors"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/hostmetrics"
)

const (
	Period    = 2 * time.Second
	FreshFor  = 7 * time.Second
	MaxPoints = 90 // At 2-second cadence: at most three minutes in process RAM
)

// Point represents an actual local sample (no interpolation or fabricated zero).
type Point struct {
	TSUnix          int64    `json:"ts_unix"`
	CPUPercent      *float64 `json:"cpu_percent"`
	RXBitsPerSecond *float64 `json:"rx_bits_per_second"`
	TXBitsPerSecond *float64 `json:"tx_bits_per_second"`
}

type Snapshot struct {
	Scope           string     `json:"scope"`
	Source          string     `json:"source"`
	SampledAt       *time.Time `json:"sampled_at"`
	MetricsCurrent  bool       `json:"metrics_current"`
	CPUCores        int        `json:"cpu_cores"`
	CPUPercent      *float64   `json:"cpu_percent"`
	RAMUsedBytes    *uint64    `json:"ram_used_bytes"`
	RAMTotalBytes   *uint64    `json:"ram_total_bytes"`
	DiskUsedBytes   *uint64    `json:"disk_used_bytes"`
	DiskTotalBytes  *uint64    `json:"disk_total_bytes"`
	Load1           *float64   `json:"load_1"`
	Load5           *float64   `json:"load_5"`
	Load15          *float64   `json:"load_15"`
	UptimeSeconds   *uint64    `json:"host_uptime_seconds"`
	TCPInUse        *uint64    `json:"tcp_inuse"`
	Interface       string     `json:"interface"`
	RXBitsPerSecond *float64   `json:"rx_bits_per_second"`
	TXBitsPerSecond *float64   `json:"tx_bits_per_second"`
	// Hub uptime restarts with the current HUB process, not the server reboot.
	HubProcessUptimeSeconds uint64  `json:"hub_process_uptime_seconds"`
	HubProcessRSSBytes      *uint64 `json:"hub_process_rss_bytes"`
	History                 []Point `json:"history"`
}

type Monitor struct {
	mu       sync.RWMutex
	read     func() (hostmetrics.Raw, error)
	rss      func() (*uint64, error)
	now      func() time.Time
	started  time.Time
	last     hostmetrics.Raw
	lastAt   time.Time
	hasLast  bool
	snapshot Snapshot
	points   []Point
}

func New(read func() (hostmetrics.Raw, error), rss func() (*uint64, error), clock func() time.Time) *Monitor {
	if read == nil {
		read = hostmetrics.Read
	}
	if rss == nil {
		rss = processRSS
	}
	if clock == nil {
		clock = time.Now
	}
	return &Monitor{read: read, rss: rss, now: clock, started: clock().UTC()}
}

func processRSS() (*uint64, error) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 || parts[2] != "kB" {
			return nil, errors.New("invalid VmRSS field")
		}
		n, e := strconv.ParseUint(parts[1], 10, 64)
		if e != nil || n > math.MaxInt64/1024 {
			return nil, errors.New("invalid VmRSS value")
		}
		value := n * 1024
		return &value, nil
	}
	return nil, errors.New("missing VmRSS")
}

func validRaw(r hostmetrics.Raw) bool {
	if r.TotalCPU == 0 || r.IdleCPU > r.TotalCPU || r.RAMTotal == 0 ||
		r.RAMTotal > math.MaxInt64 || r.RAMUsed > r.RAMTotal ||
		r.Interface == "" || len(r.Interface) > 32 {
		return false
	}
	if (r.DiskTotal == nil) != (r.DiskUsed == nil) {
		return false
	}
	if r.DiskTotal != nil && (*r.DiskTotal == 0 || *r.DiskTotal > math.MaxInt64 || *r.DiskUsed > *r.DiskTotal) {
		return false
	}
	for _, f := range []*float64{r.Load1, r.Load5, r.Load15} {
		if f != nil && (math.IsNaN(*f) || math.IsInf(*f, 0) || *f < 0 || *f > 1e6) {
			return false
		}
	}
	if r.UptimeSec != nil && *r.UptimeSec > 1e12 {
		return false
	}
	if r.TCPInUse != nil && *r.TCPInUse > 1e8 {
		return false
	}
	return true
}

func deltaBits(previous, current uint64, seconds float64) *float64 {
	if seconds <= 0 || seconds > 10 || current < previous {
		return nil
	}
	n := float64(current-previous) * 8 / seconds
	if math.IsNaN(n) || math.IsInf(n, 0) || n > 1e15 {
		return nil
	}
	return &n
}

// Sample reads host counters once. Failure invalidates "current" but never
// stops HUB operations or mixes the previous reading into a new live rate.
func (m *Monitor) Sample() {
	raw, err := m.read()
	now := m.now().UTC()
	var rss *uint64
	if value, e := m.rss(); e == nil {
		rss = value
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil || !validRaw(raw) {
		m.hasLast = false
		m.snapshot.MetricsCurrent = false
		return
	}
	n := Snapshot{
		Scope:     "hub_host_physical_not_billable",
		Source:    "local_linux",
		SampledAt: &now, MetricsCurrent: true,
		CPUCores: runtime.NumCPU(), RAMUsedBytes: &raw.RAMUsed, RAMTotalBytes: &raw.RAMTotal,
		DiskUsedBytes: raw.DiskUsed, DiskTotalBytes: raw.DiskTotal,
		Load1: raw.Load1, Load5: raw.Load5, Load15: raw.Load15,
		UptimeSeconds: raw.UptimeSec, TCPInUse: raw.TCPInUse, Interface: raw.Interface,
		HubProcessRSSBytes: rss,
	}
	if m.hasLast {
		elapsed := now.Sub(m.lastAt).Seconds()
		if elapsed > 0 && elapsed <= 10 {
			if cpu, ok := hostmetrics.CPUPercent(m.last, raw); ok {
				n.CPUPercent = &cpu
			}
			if m.last.Interface == raw.Interface {
				n.RXBitsPerSecond = deltaBits(m.last.RXBytes, raw.RXBytes, elapsed)
				n.TXBitsPerSecond = deltaBits(m.last.TXBytes, raw.TXBytes, elapsed)
			}
		}
	}
	m.snapshot = n
	m.last = raw
	m.lastAt = now
	m.hasLast = true
	m.points = append(m.points, Point{TSUnix: now.Unix(), CPUPercent: n.CPUPercent,
		RXBitsPerSecond: n.RXBitsPerSecond, TXBitsPerSecond: n.TXBitsPerSecond})
	if len(m.points) > MaxPoints {
		m.points = append([]Point(nil), m.points[len(m.points)-MaxPoints:]...)
	}
}

func (m *Monitor) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := m.snapshot
	n.Scope = "hub_host_physical_not_billable"
	n.Source = "local_linux"
	if elapsed := m.now().UTC().Sub(m.started); elapsed > 0 {
		n.HubProcessUptimeSeconds = uint64(elapsed.Seconds())
	}
	n.History = append(make([]Point, 0, len(m.points)), m.points...)
	if n.SampledAt == nil || !n.MetricsCurrent || m.now().UTC().Sub(*n.SampledAt) > FreshFor {
		n.MetricsCurrent = false
		n.CPUPercent = nil
		n.RAMUsedBytes = nil
		n.RAMTotalBytes = nil
		n.DiskUsedBytes = nil
		n.DiskTotalBytes = nil
		n.Load1 = nil
		n.Load5 = nil
		n.Load15 = nil
		n.UptimeSeconds = nil
		n.TCPInUse = nil
		n.RXBitsPerSecond = nil
		n.TXBitsPerSecond = nil
		n.HubProcessRSSBytes = nil
	}
	return n
}

func (m *Monitor) Run(ctx context.Context) {
	m.Sample()
	ticker := time.NewTicker(Period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Sample()
		}
	}
}
