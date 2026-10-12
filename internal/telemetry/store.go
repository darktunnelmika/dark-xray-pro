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
    ErrOutOfOrder = errors.New("out-of-order telemetry sequence")
    idPattern = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$")
    bootPattern = regexp.MustCompile("^[a-f0-9]{32}$")
)

const DefaultOfflineAfter = 15 * time.Second

// Sample contains physical interface counters, NOT user-billable Xray traffic.
type Sample struct {
    NodeID       string  `json:"node_id"`
    Name         string  `json:"name"`
    BootID       string  `json:"boot_id"`
    Sequence     uint64  `json:"sequence"`
    Interface    string  `json:"interface"`
    CPUCores     int     `json:"cpu_cores"`
    CPUPercent   float64 `json:"cpu_percent"`
    RAMTotal     uint64  `json:"ram_total_bytes"`
    RAMUsed      uint64  `json:"ram_used_bytes"`
    RXBytes      uint64  `json:"rx_bytes"`
    TXBytes      uint64  `json:"tx_bytes"`
}

type Node struct {
    NodeID       string    `json:"node_id"`
    Name         string    `json:"name"`
    Interface    string    `json:"interface"`
    Status       string    `json:"status"`
    CPUCores     int       `json:"cpu_cores"`
    CPUPercent   float64   `json:"cpu_percent"`
    RAMTotal     uint64    `json:"ram_total_bytes"`
    RAMUsed      uint64    `json:"ram_used_bytes"`
    RXBitsPerSec *float64  `json:"rx_bits_per_second"`
    TXBitsPerSec *float64  `json:"tx_bits_per_second"`
    LastSeen     time.Time `json:"last_seen"`
}

type Fleet struct {
    AsOf           time.Time `json:"as_of"`
    Total          int       `json:"total_nodes"`
    Online         int       `json:"online_nodes"`
    Offline        int       `json:"offline_nodes"`
    PhysicalRXBps  float64   `json:"physical_rx_bits_per_second"`
    PhysicalTXBps  float64   `json:"physical_tx_bits_per_second"`
    RatesComplete  bool      `json:"physical_rates_complete"`
    MetricScope    string    `json:"metric_scope"`
    Nodes          []Node    `json:"nodes"`
}

type lastSample struct {
    sample Sample
    seen   time.Time
    rxBps  float64
    txBps  float64
    rateReady bool
}

type Store struct {
    mu           sync.RWMutex
    clock        func() time.Time
    offlineAfter time.Duration
    nodes        map[string]lastSample
}

// NewStore allows a deterministic clock for tests and offline-state transitions.
func NewStore(offlineAfter time.Duration, clock func() time.Time) *Store {
    if offlineAfter <= 0 {
        offlineAfter = DefaultOfflineAfter
    }
    if clock == nil {
        clock = time.Now
    }
    return &Store{clock: clock, offlineAfter: offlineAfter, nodes: make(map[string]lastSample)}
}

func validate(s Sample) bool {
    if !idPattern.MatchString(s.NodeID) || !bootPattern.MatchString(s.BootID) || s.Sequence == 0 {
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
    return true
}

// Record uses the HUB's receive time, never an untrusted agent wall-clock.
// Duplicates cannot artificially keep a disconnected Node online.
func (s *Store) Record(sample Sample) error {
    if !validate(sample) {
        return ErrInvalidSample
    }
    s.mu.Lock()
    defer s.mu.Unlock()
    now := s.clock().UTC()
    last, exists := s.nodes[sample.NodeID]
    next := lastSample{sample: sample, seen: now}
    if exists && sample.BootID == last.sample.BootID {
        if sample.Sequence <= last.sample.Sequence {
            return ErrOutOfOrder
        }
        elapsed := now.Sub(last.seen).Seconds()
        if elapsed > 0 && sample.RXBytes >= last.sample.RXBytes &&
           sample.TXBytes >= last.sample.TXBytes {
            next.rxBps = float64(sample.RXBytes-last.sample.RXBytes) * 8 / elapsed
            next.txBps = float64(sample.TXBytes-last.sample.TXBytes) * 8 / elapsed
            next.rateReady = true
        }
        // Counter reset, process reboot, or time reversal: start a fresh rate baseline.
    }
    s.nodes[sample.NodeID] = next
    return nil
}

// Snapshot is O(number of Nodes) and never reads customer ledgers or the network.
func (s *Store) Snapshot() Fleet {
    s.mu.RLock()
    defer s.mu.RUnlock()
    now := s.clock().UTC()
    fleet := Fleet{AsOf: now, MetricScope: "physical_interface_not_billable", RatesComplete: true, Nodes: make([]Node, 0, len(s.nodes))}
    for _, item := range s.nodes {
        sample := item.sample
        node := Node{
            NodeID: sample.NodeID, Name: sample.Name, Interface: sample.Interface,
            CPUCores: sample.CPUCores, CPUPercent: sample.CPUPercent,
            RAMTotal: sample.RAMTotal, RAMUsed: sample.RAMUsed, LastSeen: item.seen,
            Status: "online",
        }
        if now.Sub(item.seen) > s.offlineAfter {
            node.Status = "offline"
            fleet.Offline++
        } else {
            if item.rateReady {
                rx, tx := item.rxBps, item.txBps
                node.RXBitsPerSec, node.TXBitsPerSec = &rx, &tx
                fleet.PhysicalRXBps += item.rxBps
                fleet.PhysicalTXBps += item.txBps
            } else {
                fleet.RatesComplete = false
            }
            fleet.Online++
        }
        fleet.Nodes = append(fleet.Nodes, node)
    }
    sort.Slice(fleet.Nodes, func(i, j int) bool {
        return fleet.Nodes[i].NodeID < fleet.Nodes[j].NodeID
    })
    fleet.Total = len(fleet.Nodes)
    if fleet.Online == 0 { fleet.RatesComplete = false }
    return fleet
}
