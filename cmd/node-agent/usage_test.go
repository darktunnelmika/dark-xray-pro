package main

import (
	"context"
	"errors"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

type replayProbe struct {
	status managedxray.UsageObservationStatus
	calls  int
	err    error
}

func (p *replayProbe) UsageStatus() managedxray.UsageObservationStatus { return p.status }
func (p *replayProbe) ReplayObservedUsage(context.Context, *uc.Sender) (int, error) {
	p.calls++
	return 2, p.err
}
func TestAgentReplayGateNeverTreatsRecordedBytesAsRuntimePermission(t *testing.T) {
	failure := errors.New("test storage failure")
	for _, tc := range []struct {
		name                       string
		required, running, blocked bool
		err                        error
	}{
		{"ordinary-unmetered-lab", false, false, false, nil},
		{"existing-observer", true, true, false, nil},
		{"successful-historical-replay", true, false, true, nil},
		{"failed-historical-replay", true, false, true, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &replayProbe{status: managedxray.UsageObservationStatus{Required: tc.required, Running: tc.running}, err: tc.err}
			blocked, err := replayUsageBeforeSync(context.Background(), p, nil)
			if blocked != tc.blocked || !errors.Is(err, tc.err) {
				t.Fatal("incorrect admission decision", blocked, err)
			}
			expected := 0
			if tc.blocked {
				expected = 1
			}
			if p.calls != expected {
				t.Fatal("incorrect replay dispatch", p.calls)
			}
		})
	}
}

func TestAgentReplayGateRequiresExplicitSettlement(t *testing.T) {
	for _, required := range []bool{false, true} {
		p := &replayProbe{status: managedxray.UsageObservationStatus{Required: true, Settled: true, SettlementRequired: required}}
		blocked, err := replayUsageBeforeSync(context.Background(), p, nil)
		if err != nil || blocked != required {
			t.Fatal("settlement gate", blocked, err)
		}
	}
}
