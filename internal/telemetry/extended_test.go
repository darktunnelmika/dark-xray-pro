package telemetry

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestOptionalNodeMetricsAreNeverFabricatedWhenStale(t *testing.T) {
	now := time.Date(2026, 10, 17, 5, 0, 0, 0, time.UTC)
	s := NewStore(15*time.Second, func() time.Time { return now })
	a := baseline()
	diskTotal, diskUsed := uint64(100<<30), uint64(30<<30)
	load1, load5, load15 := 0.7, 1.2, 1.5
	uptime := uint64(72 * 3600)
	sockets := uint64(10)
	a.DiskTotalBytes = &diskTotal
	a.DiskUsedBytes = &diskUsed
	a.Load1 = &load1
	a.Load5 = &load5
	a.Load15 = &load15
	a.UptimeSeconds = &uptime
	a.TCPInUse = &sockets
	if err := s.Record(a); err != nil {
		t.Fatal(err)
	}
	current := s.Snapshot().Nodes[0]
	if current.DiskTotalBytes == nil || *current.DiskUsedBytes != diskUsed ||
		current.Load1 == nil || *current.Load15 != load15 ||
		current.TCPInUse == nil || *current.TCPInUse != sockets ||
		current.UptimeSeconds == nil || *current.UptimeSeconds != uptime {
		t.Fatalf("lost authenticated metrics: %+v", current)
	}
	now = now.Add(18 * time.Second)
	stale := s.Snapshot().Nodes[0]
	if stale.Status != "offline" || stale.MetricsCurrent ||
		stale.DiskUsedBytes != nil || stale.DiskTotalBytes != nil || stale.Load1 != nil || stale.Load5 != nil ||
		stale.Load15 != nil || stale.TCPInUse != nil || stale.UptimeSeconds != nil {
		t.Fatalf("stale optional data appears as live: %+v", stale)
	}
}
func TestOptionalHostMetricValidationRejectsFakeValuesAndSupportsLegacyAgents(t *testing.T) {
	s := NewStore(15*time.Second, nil)
	a := baseline()
	if err := s.Record(a); err != nil {
		t.Fatal("legacy agent sample rejected", err)
	}
	for _, modify := range []func(*Sample){
		func(v *Sample) { zero := uint64(0); v.DiskTotalBytes = &zero },
		func(v *Sample) {
			total := uint64(100)
			used := uint64(101)
			v.DiskTotalBytes = &total
			v.DiskUsedBytes = &used
		},
		func(v *Sample) { load := math.NaN(); v.Load1 = &load; v.Load5 = &load; v.Load15 = &load },
		func(v *Sample) { load := float64(1); v.Load1 = &load },
		func(v *Sample) { tcp := uint64(100000001); v.TCPInUse = &tcp },
	} {
		bad := baseline()
		modify(&bad)
		bad.Sequence = 2
		if err := s.Record(bad); !errors.Is(err, ErrInvalidSample) {
			t.Fatalf("accepted fabricated host field: %+v %v", bad, err)
		}
	}
	sample := baseline()
	sample.Sequence = 2
	sample.CPUPercent = 25
	if err := s.Record(sample); err != nil {
		t.Fatal("rejected correct metrics after invalid data", err)
	}
}
