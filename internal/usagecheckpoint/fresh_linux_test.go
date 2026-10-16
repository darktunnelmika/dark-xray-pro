//go:build linux

package usagecheckpoint

import (
	"encoding/json"
	"errors"
	"testing"
)

func freshFixture(t *testing.T) *FreshCounters {
	t.Helper()
	epoch := testEpoch(t)
	zero := make([]Usage, len(epoch.Bindings))
	for i, b := range epoch.Bindings {
		zero[i].ClientID = b.ClientID
	}
	report, err := makeReport(epoch, 1, zero, false)
	if err != nil {
		t.Fatal(err)
	}
	tracker, err := NewFreshCounters(epoch, report, receipt(report))
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}
func freshSample(up, down string) []byte {
	entries := []map[string]string{{"name": "user>>>one@pro.invalid>>>traffic>>>uplink", "value": up}, {"name": "user>>>one@pro.invalid>>>traffic>>>downlink", "value": down}}
	raw, _ := json.Marshal(map[string]any{"stat": entries})
	return raw
}
func TestFreshCountersRequireAcknowledgedZeroAndRetainStrictOldParsing(t *testing.T) {
	f := freshFixture(t)
	for _, raw := range []string{`{}`, `{"stat":[]}`} {
		values, err := f.parse([]byte(raw))
		if err != nil || len(values) != 2 || values[0].Uplink != 0 || values[1].Downlink != 0 {
			t.Fatal("fresh idle counter rejected", err)
		}
		if _, err = ParseStats([]byte(raw), f.epoch.Bindings); err == nil {
			t.Fatal("generic incomplete sample became zero")
		}
	}
	report, _ := makeReport(f.epoch, 1, []Usage{{ClientID: "one"}, {ClientID: "two"}}, false)
	for _, bad := range []Receipt{{EpochID: f.epoch.ID, Sequence: 2, SHA: report.SHA}, {EpochID: f.epoch.ID, Sequence: 1, SHA: "wrong"}} {
		if _, err := NewFreshCounters(f.epoch, report, bad); err == nil {
			t.Fatal("unacknowledged baseline accepted")
		}
	}
	nonzero, _ := makeReport(f.epoch, 1, []Usage{{ClientID: "one", Uplink: 1}, {ClientID: "two"}}, false)
	if _, err := NewFreshCounters(f.epoch, nonzero, receipt(nonzero)); err == nil {
		t.Fatal("old usage accepted as virgin")
	}
}
func TestFreshCountersLazyFirstUseIsExactAndNeverForgetsSeenDirections(t *testing.T) {
	f := freshFixture(t)
	values, err := f.parse(freshSample("9007199254740993", "1"))
	if err != nil || values[0].Uplink != 9007199254740993 || values[1].Uplink != 0 {
		t.Fatal("precision/idle mapping changed", err)
	}
	if _, err = f.parse([]byte(`{}`)); !errors.Is(err, ErrIncomplete) {
		t.Fatal("disappeared counters guessed zero", err)
	}
	if _, err = f.parse(freshSample("1", "1")); !errors.Is(err, ErrRegression) {
		t.Fatal("same-process reset adopted", err)
	}
	zero := freshFixture(t)
	if _, err = zero.parse(freshSample("0", "0")); err != nil {
		t.Fatal(err)
	}
	if _, err = zero.parse([]byte(`{}`)); !errors.Is(err, ErrIncomplete) {
		t.Fatal("previously seen zero counters forgotten", err)
	}
}
func TestFreshCountersRejectMalformedWithoutAdvancingState(t *testing.T) {
	for _, raw := range []string{`null`, `{"stat":null}`, `{"stat":[],"stat":[]}`, `{"STAT":[]}`, `{} {}`, `{"stat":{},"x":1}`, `{"stat":[{"name":"user>>>one@pro.invalid>>>traffic>>>uplink","value":"1"}]}`, `{"stat":[{"name":"user>>>other@pro.invalid>>>traffic>>>uplink","value":"1"}]}`, `{"stat":[{"name":"user>>>one@pro.invalid>>>traffic>>>uplink","value":"1","value":"2"}]}`} {
		t.Run(raw, func(t *testing.T) {
			f := freshFixture(t)
			if _, err := f.parse([]byte(raw)); err == nil {
				t.Fatal("malformed response accepted")
			}
			if len(f.seen) != 0 {
				t.Fatal("bad sample mutated tracker")
			}
			if _, err := f.parse([]byte(`{}`)); err != nil {
				t.Fatal("bad response advanced state", err)
			}
		})
	}
}
