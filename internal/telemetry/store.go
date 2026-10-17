package telemetry

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidSample = errors.New("invalid telemetry sample")
	ErrOutOfOrder    = errors.New("out-of-order heartbeat or telemetry sequence")
	idPattern        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	bootPattern      = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

const DefaultOfflineAfter = 15 * time.Second
const metricsFreshAfter = 12 * time.Second
const maxRetiredBoots = 32

// Pulse proves that the Agent connection is alive. The Agent sends it
// independently of slow/failed CPU, memory, Xray or network sampling.
type Pulse struct {
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	BootID         string `json:"boot_id"`
	Sequence       uint64 `json:"sequence"`
}

// Sample contains default-route interface counters, NOT billable user traffic.
type Sample struct {
	NodeID         string   `json:"node_id"`
	InstallationID string   `json:"installation_id"`
	Name           string   `json:"name"`
	BootID         string   `json:"boot_id"`
	Sequence       uint64   `json:"sequence"`
	Interface      string   `json:"interface"`
	CPUCores       int      `json:"cpu_cores"`
	CPUPercent     float64  `json:"cpu_percent"`
	RAMTotal       uint64   `json:"ram_total_bytes"`
	RAMUsed        uint64   `json:"ram_used_bytes"`
	RXBytes        uint64   `json:"rx_bytes"`
	TXBytes        uint64   `json:"tx_bytes"`
	DiskTotalBytes *uint64  `json:"disk_total_bytes,omitempty"`
	DiskUsedBytes  *uint64  `json:"disk_used_bytes,omitempty"`
	Load1          *float64 `json:"load_1,omitempty"`
	Load5          *float64 `json:"load_5,omitempty"`
	Load15         *float64 `json:"load_15,omitempty"`
	UptimeSeconds  *uint64  `json:"uptime_seconds,omitempty"`
	TCPInUse       *uint64  `json:"tcp_inuse,omitempty"`
}

type Node struct {
	NodeID         string     `json:"node_id"`
	Name           string     `json:"name"`
	Interface      string     `json:"interface"`
	Status         string     `json:"status"`
	CPUCores       int        `json:"cpu_cores"`
	CPUPercent     float64    `json:"cpu_percent"`
	RAMTotal       uint64     `json:"ram_total_bytes"`
	RAMUsed        uint64     `json:"ram_used_bytes"`
	RXBitsPerSec   *float64   `json:"rx_bits_per_second"`
	TXBitsPerSec   *float64   `json:"tx_bits_per_second"`
	MetricsCurrent bool       `json:"metrics_current"`
	MetricsAt      *time.Time `json:"metrics_at"`
	DiskTotalBytes *uint64    `json:"disk_total_bytes,omitempty"`
	DiskUsedBytes  *uint64    `json:"disk_used_bytes,omitempty"`
	Load1          *float64   `json:"load_1,omitempty"`
	Load5          *float64   `json:"load_5,omitempty"`
	Load15         *float64   `json:"load_15,omitempty"`
	UptimeSeconds  *uint64    `json:"uptime_seconds,omitempty"`
	TCPInUse       *uint64    `json:"tcp_inuse,omitempty"`
	LastSeen       *time.Time `json:"last_seen"`
}

type Fleet struct {
	AsOf           time.Time `json:"as_of"`
	Total          int       `json:"total_nodes"`
	Online         int       `json:"online_nodes"`
	Offline        int       `json:"offline_nodes"`
	InterfaceRXBps float64   `json:"interface_rx_bits_per_second"`
	InterfaceTXBps float64   `json:"interface_tx_bits_per_second"`
	RatesComplete  bool      `json:"physical_rates_complete"`
	MetricScope    string    `json:"metric_scope"`
	Nodes          []Node    `json:"nodes"`
}

type lastSample struct {
	sample    Sample
	seen      time.Time
	rxBps     float64
	txBps     float64
	rateReady bool
	retired   []string
}

type lastPulse struct {
	pulse   Pulse
	seen    time.Time
	retired []string
}

type Store struct {
	mu           sync.RWMutex
	clock        func() time.Time
	offlineAfter time.Duration
	nodes        map[string]lastSample
	pulses       map[string]lastPulse
}

func NewStore(offlineAfter time.Duration, clock func() time.Time) *Store {
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	if clock == nil {
		clock = time.Now
	}
	return &Store{
		clock: clock, offlineAfter: offlineAfter,
		nodes: make(map[string]lastSample), pulses: make(map[string]lastPulse),
	}
}

func validIdentity(id, installation, boot string, sequence uint64) bool {
	return idPattern.MatchString(id) && bootPattern.MatchString(installation) &&
		bootPattern.MatchString(boot) && sequence > 0
}

func validPulse(p Pulse) bool {
	return validIdentity(p.NodeID, p.InstallationID, p.BootID, p.Sequence)
}

func validate(s Sample) bool {
	if !validIdentity(s.NodeID, s.InstallationID, s.BootID, s.Sequence) {
		return false
	}
	if strings.TrimSpace(s.Name) == "" || utf8.RuneCountInString(s.Name) > 80 ||
		strings.TrimSpace(s.Interface) == "" || len(s.Interface) > 32 {
		return false
	}
	for _, r := range s.Name + s.Interface {
		if unicode.IsControl(r) {
			return false
		}
	}
	if s.CPUCores < 1 || s.CPUCores > 1024 || math.IsNaN(s.CPUPercent) ||
		math.IsInf(s.CPUPercent, 0) || s.CPUPercent < 0 || s.CPUPercent > 100 ||
		s.RAMTotal == 0 || s.RAMUsed > s.RAMTotal {
		return false
	}
	if (s.DiskTotalBytes == nil) != (s.DiskUsedBytes == nil) ||
		(s.DiskTotalBytes != nil && (*s.DiskTotalBytes == 0 || *s.DiskTotalBytes > math.MaxInt64 || *s.DiskUsedBytes > *s.DiskTotalBytes)) ||
		(s.Load1 == nil) != (s.Load5 == nil) || (s.Load1 == nil) != (s.Load15 == nil) {
		return false
	}
	for _, v := range []*float64{s.Load1, s.Load5, s.Load15} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 1e6) {
			return false
		}
	}
	if s.UptimeSeconds != nil && *s.UptimeSeconds > 1e12 {
		return false
	}
	if s.TCPInUse != nil && *s.TCPInUse > 1e8 {
		return false
	}
	return true
}

func seenBoot(history []string, boot string) bool {
	for _, known := range history {
		if boot == known {
			return true
		}
	}
	return false
}

func retireBoot(history []string, boot string) []string {
	next := append(append([]string(nil), history...), boot)
	if len(next) > maxRetiredBoots {
		next = next[len(next)-maxRetiredBoots:]
	}
	return next
}

// Heartbeat updates liveness without requiring or modifying any telemetry data.
// A duplicate pulse does not refresh last_seen, preventing replayed "online".
func (s *Store) Heartbeat(p Pulse) error {
	if !validPulse(p) {
		return ErrInvalidSample
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	last, exists := s.pulses[p.NodeID]
	next := lastPulse{pulse: p, seen: now}
	if exists {
		if p.InstallationID != last.pulse.InstallationID {
			return ErrOutOfOrder
		}
		next.retired = last.retired
		if p.BootID == last.pulse.BootID {
			if p.Sequence <= last.pulse.Sequence {
				return ErrOutOfOrder
			}
		} else {
			if seenBoot(last.retired, p.BootID) {
				return ErrOutOfOrder
			}
			next.retired = retireBoot(last.retired, last.pulse.BootID)
		}
	}
	s.pulses[p.NodeID] = next
	return nil
}

// Record is independent of Node Online state. A slow stats collector cannot
// prevent Heartbeat from keeping a healthy Agent Online.
func (s *Store) Record(sample Sample) error {
	if !validate(sample) {
		return ErrInvalidSample
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock().UTC()
	if pulse, exists := s.pulses[sample.NodeID]; exists {
		if pulse.pulse.InstallationID != sample.InstallationID ||
			pulse.pulse.BootID != sample.BootID {
			return ErrOutOfOrder
		}
	}
	last, exists := s.nodes[sample.NodeID]
	next := lastSample{sample: sample, seen: now}
	if exists {
		next.retired = last.retired
		if sample.InstallationID != last.sample.InstallationID {
			return ErrOutOfOrder
		}
		if sample.BootID == last.sample.BootID {
			if sample.Sequence <= last.sample.Sequence {
				return ErrOutOfOrder
			}
			elapsed := now.Sub(last.seen).Seconds()
			if elapsed > 0 && sample.Interface == last.sample.Interface &&
				sample.RXBytes >= last.sample.RXBytes && sample.TXBytes >= last.sample.TXBytes {
				next.rxBps = float64(sample.RXBytes-last.sample.RXBytes) * 8 / elapsed
				next.txBps = float64(sample.TXBytes-last.sample.TXBytes) * 8 / elapsed
				next.rateReady = true
			}
		} else {
			if seenBoot(last.retired, sample.BootID) {
				return ErrOutOfOrder
			}
			next.retired = retireBoot(last.retired, last.sample.BootID)
		}
	}
	s.nodes[sample.NodeID] = next
	return nil
}

// Snapshot is O(number of Nodes), never fetches a Node or queries a ledger.
// Online = fresh authentic heartbeat/sample; metrics_current = independent
// freshness of the last successful stats collection. Stale is NOT "Offline".
func (s *Store) Snapshot() Fleet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.clock().UTC()
	fleet := Fleet{
		AsOf: now, MetricScope: "default_route_interface_not_billable",
		RatesComplete: true, Nodes: make([]Node, 0, len(s.nodes)+len(s.pulses)),
	}
	all := make(map[string]bool, len(s.nodes)+len(s.pulses))
	for id := range s.nodes {
		all[id] = true
	}
	for id := range s.pulses {
		all[id] = true
	}

	for id := range all {
		sample, hasSample := s.nodes[id]
		pulse, hasPulse := s.pulses[id]
		node := Node{NodeID: id, Status: "offline"}
		var liveAt time.Time
		if hasSample {
			node.Name = sample.sample.Name
			node.Interface = sample.sample.Interface
			node.CPUCores = sample.sample.CPUCores
			node.RAMTotal = sample.sample.RAMTotal
			seen := sample.seen
			node.MetricsAt = &seen
			liveAt = sample.seen
		}
		if hasPulse && pulse.seen.After(liveAt) {
			liveAt = pulse.seen
		}
		if !liveAt.IsZero() {
			seen := liveAt
			node.LastSeen = &seen
		}
		online := !liveAt.IsZero() && now.Sub(liveAt) <= s.offlineAfter
		if online {
			node.Status = "online"
			fleet.Online++
			freshFor := metricsFreshAfter
			if s.offlineAfter < freshFor {
				freshFor = s.offlineAfter
			}
			if hasSample && now.Sub(sample.seen) < freshFor {
				node.MetricsCurrent = true
				node.CPUPercent = sample.sample.CPUPercent
				node.RAMUsed = sample.sample.RAMUsed
				node.DiskTotalBytes = sample.sample.DiskTotalBytes
				node.DiskUsedBytes = sample.sample.DiskUsedBytes
				node.Load1 = sample.sample.Load1
				node.Load5 = sample.sample.Load5
				node.Load15 = sample.sample.Load15
				node.UptimeSeconds = sample.sample.UptimeSeconds
				node.TCPInUse = sample.sample.TCPInUse
				if sample.rateReady {
					rx, tx := sample.rxBps, sample.txBps
					node.RXBitsPerSec, node.TXBitsPerSec = &rx, &tx
					fleet.InterfaceRXBps += rx
					fleet.InterfaceTXBps += tx
				} else {
					fleet.RatesComplete = false
				}
			} else {
				fleet.RatesComplete = false
			}
		} else {
			fleet.Offline++
		}
		fleet.Nodes = append(fleet.Nodes, node)
	}
	sort.Slice(fleet.Nodes, func(i, j int) bool {
		return fleet.Nodes[i].NodeID < fleet.Nodes[j].NodeID
	})
	fleet.Total = len(fleet.Nodes)
	if fleet.Online == 0 {
		fleet.RatesComplete = false
	}
	return fleet
}
