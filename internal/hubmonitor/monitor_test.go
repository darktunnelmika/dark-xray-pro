package hubmonitor

import (
	"errors"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/hostmetrics"
)

func u64(n uint64) *uint64   { return &n }
func f64(n float64) *float64 { return &n }
func baseline() hostmetrics.Raw {
	return hostmetrics.Raw{
		TotalCPU: 1000, IdleCPU: 600, RAMTotal: 8 << 30, RAMUsed: 3 << 30,
		RXBytes: 1000, TXBytes: 2000, Interface: "eth0",
		DiskTotal: u64(80 << 30), DiskUsed: u64(25 << 30),
		Load1: f64(1.25), Load5: f64(0.7), Load15: f64(0.5),
		UptimeSec: u64(86401), TCPInUse: u64(21),
	}
}
func TestHUBRealRatesFirstUnknownThenMeasured(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	raw := baseline()
	monitor := New(func() (hostmetrics.Raw, error) { return raw, nil },
		func() (*uint64, error) { return u64(35 << 20), nil }, func() time.Time { return now })
	initial := monitor.Snapshot()
	if initial.MetricsCurrent || initial.CPUPercent != nil || initial.RXBitsPerSecond != nil {
		t.Fatalf("fabricated before first sample: %+v", initial)
	}
	monitor.Sample()
	first := monitor.Snapshot()
	if !first.MetricsCurrent || first.CPUPercent != nil || first.RXBitsPerSecond != nil || first.TXBitsPerSecond != nil ||
		first.RAMTotalBytes == nil || *first.RAMTotalBytes != 8<<30 || first.HubProcessRSSBytes == nil || *first.HubProcessRSSBytes != 35<<20 {
		t.Fatalf("fabricated first rates/missing real counters: %+v", first)
	}
	now = now.Add(2 * time.Second)
	raw.TotalCPU = 1200
	raw.IdleCPU = 750 // 25% CPU of an actual 200-tick interval
	raw.RXBytes += 2000
	raw.TXBytes += 1000
	monitor.Sample()
	second := monitor.Snapshot()
	if second.CPUPercent == nil || *second.CPUPercent != 25 ||
		second.RXBitsPerSecond == nil || *second.RXBitsPerSecond != 8000 ||
		second.TXBitsPerSecond == nil || *second.TXBitsPerSecond != 4000 ||
		second.HubProcessUptimeSeconds != 2 || len(second.History) != 2 {
		t.Fatalf("computed wrong host rates: %+v", second)
	}
	if second.Scope != "hub_host_physical_not_billable" || second.Source != "local_linux" {
		t.Fatal("incorrect source or billing label")
	}
	// Reading older NIC counters cannot be interpreted as 0 or a negative rate.
	now = now.Add(2 * time.Second)
	raw.RXBytes = 1
	monitor.Sample()
	restarted := monitor.Snapshot()
	if restarted.RXBitsPerSecond != nil || restarted.TXBitsPerSecond == nil {
		t.Fatal("RX counter reset affected a valid TX interval")
	}
}

func TestFailureAndStalenessCannotAdvertiseLiveReadings(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	fail := false
	raw := baseline()
	monitor := New(func() (hostmetrics.Raw, error) {
		if fail {
			return hostmetrics.Raw{}, errors.New("sensor unavailable")
		}
		return raw, nil
	},
		func() (*uint64, error) { return nil, errors.New("rss unavailable") }, func() time.Time { return now })
	monitor.Sample()
	now = now.Add(2 * time.Second)
	raw.TotalCPU += 200
	raw.IdleCPU += 100
	monitor.Sample()
	if monitor.Snapshot().CPUPercent == nil {
		t.Fatal("missing real CPU delta")
	}
	now = now.Add(8 * time.Second)
	stale := monitor.Snapshot()
	if stale.MetricsCurrent || stale.CPUPercent != nil || stale.RAMUsedBytes != nil || stale.HubProcessRSSBytes != nil {
		t.Fatalf("old sample represented as current: %+v", stale)
	}
	fail = true
	monitor.Sample()
	if monitor.Snapshot().MetricsCurrent {
		t.Fatal("failure falsely marked current")
	}
	fail = false
	now = now.Add(2 * time.Second)
	monitor.Sample()
	recovered := monitor.Snapshot()
	if !recovered.MetricsCurrent || recovered.CPUPercent != nil || recovered.RXBitsPerSecond != nil {
		t.Fatalf("first after failure must reset baseline: %+v", recovered)
	}
}

func TestBoundedHistoryNoFabricatedPointsAndInvalidSample(t *testing.T) {
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	raw := baseline()
	m := New(func() (hostmetrics.Raw, error) { return raw, nil }, func() (*uint64, error) { return nil, nil }, func() time.Time { return now })
	for i := 0; i < MaxPoints+100; i++ {
		raw.TotalCPU += 200
		raw.IdleCPU += 100
		raw.RXBytes += 500
		now = now.Add(2 * time.Second)
		m.Sample()
	}
	snap := m.Snapshot()
	if len(snap.History) != MaxPoints || snap.History[0].TSUnix != now.Add(-time.Duration(MaxPoints-1)*2*time.Second).Unix() {
		t.Fatal("history does not cap old samples")
	}
	raw.RAMUsed = raw.RAMTotal + 1
	now = now.Add(2 * time.Second)
	m.Sample()
	bad := m.Snapshot()
	if bad.MetricsCurrent || len(bad.History) != MaxPoints {
		t.Fatal("invalid sample was kept as current or added a fake point")
	}
}
