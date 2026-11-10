package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"strings"
	"testing"
	"time"
)

func TestNodeRemovalRetainedTelemetryDoesNotPoisonHealthyDashboard(t *testing.T) {
	s := enabledOwner(t)
	now := time.Now()
	s.fleet = telemetry.NewStore(time.Minute, func() time.Time { return now })
	samples := []telemetry.Sample{}
	for i, id := range []string{"removed-one", "healthy-two"} {
		install := strings.Repeat(string(rune('a'+i)), 32)
		token := strings.Repeat(string(rune('c'+i)), 64)
		ticket, e := s.registry.Issue(id, id)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.registry.Claim(ticket.PairCode, id, install, token); e != nil {
			t.Fatal(e)
		}
		sample := telemetry.Sample{NodeID: id, Name: id, InstallationID: install, BootID: strings.Repeat("e", 32), Sequence: 1, Interface: "eth0", CPUCores: 2, RAMTotal: 1024, RAMUsed: 512, RXBytes: 1000, TXBytes: 1000}
		if e = s.fleet.Record(sample); e != nil {
			t.Fatal(e)
		}
		samples = append(samples, sample)
	}
	now = now.Add(time.Second)
	for i := range samples {
		samples[i].Sequence++
		samples[i].RXBytes += uint64(100 * (i + 1))
		samples[i].TXBytes += uint64(50 * (i + 1))
		if e := s.fleet.Record(samples[i]); e != nil {
			t.Fatal(e)
		}
	}
	before := s.snapshot()
	if !before.RatesComplete || before.Total != 2 || before.InterfaceRXBps != 2400 {
		t.Fatal(before)
	}
	in := noderegistry.LifecycleInput{EventID: strings.Repeat("f", 32), InstallationID: strings.Repeat("a", 32), ExpectedRevision: 1, Action: "remove"}
	if _, e := s.registry.BeginLifecycle("removed-one", "owner", in); e != nil {
		t.Fatal(e)
	}
	if _, e := s.registry.FinishLifecycle(in.EventID, nil); e != nil {
		t.Fatal(e)
	}
	check := func() {
		t.Helper()
		got := s.snapshot()
		if got.Total != 1 || got.Online != 1 || got.Offline != 0 || !got.RatesComplete || got.InterfaceRXBps != 1600 || got.InterfaceTXBps != 800 || got.Nodes[0].NodeID != "healthy-two" {
			t.Fatal("removed telemetry affected healthy dashboard", got)
		}
	}
	check()
	// Even an authentic old heartbeat/sample cannot reinsert a removed identity.
	now = now.Add(time.Second)
	samples[0].Sequence++
	samples[0].RXBytes += 999999
	if e := s.fleet.Record(samples[0]); e != nil {
		t.Fatal(e)
	}
	check()
}
