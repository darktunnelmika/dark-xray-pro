package api

import (
	"encoding/json"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestNodeChannelObservationLifetimeNeverRefreshesOldEvidence(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	channel := ix.TargetState{
		NodeID: "qa-node", InstallationID: "installation-private",
		HealthState: "engine_ready", AccountingState: "uncertain",
		ReportedAt: now.Add(-12 * time.Second).UnixNano(),
	}
	first := observedNodeChannels(channel, now)
	if !first.ExecutionCurrent || first.ExecutionFreshForMS != 3000 ||
		first.CoreState != "engine_ready" || first.AccountingState != "uncertain" {
		t.Fatalf("independent report or remaining lifetime lost: %+v", first)
	}
	repeated := observedNodeChannels(channel, now.Add(2*time.Second))
	if !repeated.ExecutionCurrent || repeated.ExecutionFreshForMS != 1000 {
		t.Fatalf("roster refresh renewed old evidence: %+v", repeated)
	}
	expired := observedNodeChannels(channel, now.Add(3*time.Second))
	if expired.ExecutionCurrent || expired.ExecutionFreshForMS != 0 ||
		expired.ExecutionReportedAt == nil || expired.AccountingState != "uncertain" {
		t.Fatalf("expiry lost evidence or fabricated freshness: %+v", expired)
	}
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var public map[string]any
	if err = json.Unmarshal(data, &public); err != nil {
		t.Fatal(err)
	}
	if public["execution_fresh_for_ms"] != float64(3000) || public["ExecutionInstallation"] != nil {
		t.Fatalf("public lifetime contract: %s", data)
	}
}

func TestNodeChannelObservationMissingAndFutureTimes(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		reported int64
		current  bool
		lifetime int64
	}{
		{"absent", 0, false, 0},
		{"negative", -1, false, 0},
		{"expired", now.Add(-15 * time.Second).UnixNano(), false, 0},
		{"far_future", now.Add(2 * time.Second).UnixNano(), false, 0},
		{"bounded_clock_skew", now.Add(500 * time.Millisecond).UnixNano(), true, 15000},
		{"recent", now.Add(-time.Second).UnixNano(), true, 14000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := observedNodeChannels(ix.TargetState{ReportedAt: tc.reported, HealthState: "healthy", AccountingState: "accepted"}, now)
			if got.ExecutionCurrent != tc.current || got.ExecutionFreshForMS != tc.lifetime {
				t.Fatalf("unexpected observation validity: %+v", got)
			}
			if tc.reported <= 0 && got.ExecutionReportedAt != nil {
				t.Fatal("missing report acquired a timestamp")
			}
		})
	}
}
