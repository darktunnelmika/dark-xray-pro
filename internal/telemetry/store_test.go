package telemetry

import (
    "errors"
    "math"
    "testing"
    "time"
)

func baseline() Sample {
    return Sample{
        NodeID: "de-01", Name: "Germany 01", InstallationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BootID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        Sequence: 1, Interface: "eth0", CPUCores: 4, CPUPercent: 30,
        RAMTotal: 8 << 30, RAMUsed: 3 << 30,
        RXBytes: 1000000, TXBytes: 2000000,
    }
}

func TestFleetFreshnessAndPhysicalCounters(t *testing.T) {
    now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
    store := NewStore(15*time.Second, func() time.Time { return now })
    a := baseline()
    if err := store.Record(a); err != nil { t.Fatal(err) }
    first := store.Snapshot()
    if first.Total != 1 || first.Online != 1 || first.Nodes[0].RXBitsPerSec != nil || first.RatesComplete {
        t.Fatalf("bad first snapshot: %+v", first)
    }
    now = now.Add(3*time.Second)
    a.Sequence++
    a.RXBytes += 1500000
    a.TXBytes += 500000
    if err := store.Record(a); err != nil { t.Fatal(err) }
    second := store.Snapshot()
    if !second.RatesComplete || second.Nodes[0].RXBitsPerSec == nil { t.Fatal("second sample must establish physical network rates") }
    if math.Abs(second.InterfaceRXBps-4000000) > 0.01 {
        t.Fatalf("RX rate should be 4 Mbps, got %.2f", second.InterfaceRXBps)
    }
    if err := store.Record(a); !errors.Is(err, ErrOutOfOrder) {
        t.Fatalf("replay not rejected: %v", err)
    }
    now = now.Add(16*time.Second)
    stale := store.Snapshot()
    if stale.Total != 1 || stale.Online != 0 || stale.Offline != 1 ||
       stale.Nodes[0].Status != "offline" || stale.InterfaceRXBps != 0 || stale.RatesComplete {
        t.Fatalf("disconnected node must remain visible but uncounted: %+v", stale)
    }
    a.BootID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    a.Sequence = 1
    a.RXBytes = 120
    a.TXBytes = 240
    if err := store.Record(a); err != nil { t.Fatal(err) }
    resumed := store.Snapshot()
    if resumed.Online != 1 || resumed.RatesComplete || resumed.Nodes[0].RXBitsPerSec != nil {
        t.Fatalf("new boot must reset baseline: %+v", resumed)
    }
}

func TestInvalidSamplesNeverEnterFleet(t *testing.T) {
    store := NewStore(0, nil)
    samples := []Sample{}
    bad := baseline(); bad.CPUCores = 0; samples = append(samples, bad)
    bad = baseline(); bad.RAMUsed = bad.RAMTotal + 1; samples = append(samples, bad)
    bad = baseline(); bad.NodeID = "../outside"; samples = append(samples, bad)
    bad = baseline(); bad.Sequence = 0; samples = append(samples, bad)
    bad = baseline(); bad.Name = "bad\nname"; samples = append(samples, bad)
    for _, sample := range samples {
        if err := store.Record(sample); !errors.Is(err, ErrInvalidSample) {
            t.Errorf("expected invalid sample, got %v", err)
        }
    }
    if store.Snapshot().Total != 0 { t.Fatal("invalid sample created a Node") }
}

func TestFleetOrderingAndDistinctNodes(t *testing.T) {
    store := NewStore(0, nil)
    b := baseline()
    b.NodeID = "us-02"
    if err := store.Record(b); err != nil { t.Fatal(err) }
    b.NodeID = "de-01"
    if err := store.Record(b); err != nil { t.Fatal(err) }
    nodes := store.Snapshot().Nodes
    if len(nodes) != 2 || nodes[0].NodeID != "de-01" || nodes[1].NodeID != "us-02" {
        t.Fatalf("unexpected ordering %v", nodes)
    }
}

func TestFreshPulseKeepsNodeOnlineWhenStatsCollectorIsStale(t *testing.T) {
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	store := NewStore(15*time.Second, func() time.Time { return now })
	pulse := Pulse{
		NodeID: "de-01", InstallationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BootID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Sequence: 1,
	}
	if err := store.Heartbeat(pulse); err != nil { t.Fatal(err) }
	live := store.Snapshot()
	if live.Total != 1 || live.Online != 1 || live.Offline != 0 ||
		live.Nodes[0].MetricsCurrent || live.Nodes[0].LastSeen == nil {
		t.Fatalf("heartbeat-only Node did not appear Online with UNKNOWN metrics: %+v", live)
	}
	sample := baseline()
	sample.BootID = pulse.BootID
	if err := store.Record(sample); err != nil { t.Fatal(err) }
	now = now.Add(13*time.Second)
	pulse.Sequence = 2
	if err := store.Heartbeat(pulse); err != nil { t.Fatal(err) }
	stale := store.Snapshot()
	if stale.Online != 1 || stale.Nodes[0].Status != "online" ||
		stale.Nodes[0].MetricsCurrent || stale.Nodes[0].CPUPercent != 0 ||
		stale.Nodes[0].RXBitsPerSec != nil || stale.RatesComplete {
		t.Fatalf("stats failure falsely put Node Offline or reported stale CPU: %+v", stale)
	}
	// A duplicate pulse after 16s cannot extend the last authentic heartbeat.
	now = now.Add(16*time.Second)
	if err := store.Heartbeat(pulse); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("replayed heartbeat renewed Online: %v", err)
	}
	offline := store.Snapshot()
	if offline.Total != 1 || offline.Offline != 1 || offline.Nodes[0].Status != "offline" {
		t.Fatalf("missed heartbeat did not mark Node Offline: %+v", offline)
	}
}

func TestRetiredBootReplayAndInterfaceChangeNeverForgeRates(t *testing.T) {
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	store := NewStore(15*time.Second, func() time.Time { return now })
	a := baseline()
	if err := store.Record(a); err != nil { t.Fatal(err) }
	now = now.Add(5*time.Second)
	a.Interface = "ens3"
	a.Sequence++
	a.RXBytes += 200
	if err := store.Record(a); err != nil { t.Fatal(err) }
	snapshot := store.Snapshot()
	if snapshot.RatesComplete || snapshot.Nodes[0].RXBitsPerSec != nil {
		t.Fatal("switching network interfaces fabricated a live rate")
	}
	now = now.Add(5*time.Second)
	a.BootID = "cccccccccccccccccccccccccccccccc"
	a.Sequence = 1
	if err := store.Record(a); err != nil { t.Fatal(err) }
	a.BootID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	a.Sequence = 4
	if err := store.Record(a); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("old boot sequence was replayed: %v", err)
	}
}

func TestPulseRebootRejectsRetiredBootAndWrongIdentity(t *testing.T) {
	now := time.Now()
	store := NewStore(0, func() time.Time { return now })
	p := Pulse{NodeID:"lab",InstallationID:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BootID:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",Sequence:1}
	if err:=store.Heartbeat(p);err!=nil {t.Fatal(err)}
	p.BootID="cccccccccccccccccccccccccccccccc"
	p.Sequence=1
	if err:=store.Heartbeat(p);err!=nil {t.Fatal(err)}
	p.BootID="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	p.Sequence=99
	if err:=store.Heartbeat(p);!errors.Is(err,ErrOutOfOrder){t.Fatalf("old boot replay accepted: %v",err)}
	p.BootID="cccccccccccccccccccccccccccccccc"
	p.InstallationID="dddddddddddddddddddddddddddddddd"
	if err:=store.Heartbeat(p);!errors.Is(err,ErrOutOfOrder){t.Fatalf("installation switched without Pair: %v",err)}
}
