//go:build linux

package usagecheckpoint

import (
	"strings"
	"testing"
)

func TestFinalTCPMarkerRejectsAmbiguousProof(t *testing.T) {
	name := FinalTCPProtocol + "sealed/" + strings.Repeat("a", 64)
	marker := `{"name":"` + name + `","value":"1"}`
	for _, raw := range []string{`{}`, `{"stat":null}`, `{"stat":[]}`, `{"stat":[` + marker + `],"stat":[]}`, `{"stat":[` + marker + `,` + marker + `]}`, `{"stat":[{"name":"` + name + `","value":"0"}]}`, `{"stat":[{"name":"` + name + `","value":"1","value":"1"}]}`, `{"stat":[` + marker + `]} trailing`, `{"stat":[{"name":"` + FinalTCPProtocol + `sealed/` + strings.Repeat("b", 64) + `","value":"1"}]}`} {
		if _, err := stripFinalMarker([]byte(raw), name); err == nil {
			t.Fatalf("accepted ambiguous proof %s", raw)
		}
	}
	for _, value := range []string{`"1"`, `1`} {
		rest, err := stripFinalMarker([]byte(`{"stat":[{"name":"`+name+`","value":`+value+`}]}`), name)
		if err != nil || string(rest) != `{"stat":[]}` {
			t.Fatalf("valid proof: %s %v", rest, err)
		}
	}
}

func TestFinalTCPProofCannotInventMissingSeenCounters(t *testing.T) {
	epoch := Epoch{NodeID: "n", InstallationID: strings.Repeat("1", 32), ID: strings.Repeat("2", 32), ConfigSHA: strings.Repeat("3", 64), KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456, Bindings: []Binding{{ClientID: "one", Email: "one@pro.invalid", Billable: true}, {ClientID: "two", Email: "two@pro.invalid", Billable: true}}}
	baseline, err := makeReport(epoch, 1, []Usage{{ClientID: "one"}, {ClientID: "two"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFreshCounters(epoch, baseline, Receipt{EpochID: epoch.ID, Sequence: 1, SHA: baseline.SHA})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.parse([]byte(`{"stat":[{"name":"user>>>one@pro.invalid>>>traffic>>>uplink","value":"20"},{"name":"user>>>one@pro.invalid>>>traffic>>>downlink","value":"40"}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.parse([]byte(`{"stat":[]}`)); err == nil {
		t.Fatal("sealed marker cannot justify disappearance")
	}
	values, err := f.parse([]byte(`{"stat":[{"name":"user>>>one@pro.invalid>>>traffic>>>uplink","value":"21"},{"name":"user>>>one@pro.invalid>>>traffic>>>downlink","value":"45"}]}`))
	if err != nil || values[0].Uplink != 21 || values[1].Uplink != 0 || values[1].Downlink != 0 {
		t.Fatal(values, err)
	}
}
