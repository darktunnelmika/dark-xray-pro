//go:build linux

package managedxray

import (
	"bytes"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestDarkRestoreSystemProbeJournalBindsNonbillableEpochAndReceipt(t *testing.T) {
	key := bytes.Repeat([]byte{0x73}, 32)
	now := time.Now()
	candidate := testCandidate(t, 27612, "probe")
	probeID := "system-probe-test"
	email, err := uc.CounterEmail(probeID)
	if err != nil {
		t.Fatal(err)
	}
	epoch := uc.Epoch{
		NodeID: "target", InstallationID: testInstall, ID: strings32("d"),
		ConfigSHA:    candidate.SHA256,
		KernelBootID: "00000000-0000-0000-0000-000000000001",
		PID:          123, ProcessStartTicks: 456,
		Bindings: []uc.Binding{{ClientID: "customer-1", Email: "customer@pro.invalid", Billable: true}, {
			ClientID: probeID, Email: email, Role: uc.SystemProbeRole,
		}},
	}
	epochSHA, err := MeteredEpochSHA(epoch)
	if err != nil {
		t.Fatal(err)
	}
	q := ProtocolHealthRequest{
		Version: InitialApplyProtocolHealthVersion, ID: strings32("a"),
		NodeID: epoch.NodeID, InstallationID: epoch.InstallationID,
		RuntimeInstance: strings32("b"), StageID: strings32("c"),
		ApplyCommandID: strings32("e"), InboundID: 7612, Port: 27612,
		CredentialSHA: strings64("a"), SystemProbeID: probeID,
		EndpointSHA: strings64("b"), InitialApplySHA: strings64("c"),
		Expected: RuntimeCondition{Highwater: 3, SavedSequence: 3, SavedSHA: candidate.SHA256, ActiveSequence: 3, ActiveSHA: candidate.SHA256, ProcessReady: true},
		Usage:    MeteredCondition{EpochID: epoch.ID, EpochSHA: epochSHA, AdmissionSHA: strings64("f")},
		Issued:   now.UnixNano(), Expires: now.Add(8 * time.Second).UnixNano(),
	}
	q, err = SignProtocolHealthRequest(key, q)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ProtocolHealthReceipt{Version: q.Version, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: q.Issued + 1}
	receipt.Signature = transactionMAC(key, protocolHealthMACDomain("RECEIPT", q.Version), receipt)
	j := journal{
		Version: 4, Highwater: 3, Good: freeze(Revision{Sequence: 3, Candidate: candidate}),
		Usage:       &observedUsage{Epoch: epoch, StatsEndpoint: MeteredStatsEndpoint},
		SystemProbe: &darkRestoreSystemProbeAttempt{Request: q, Receipt: &receipt},
	}
	if !validDarkRestoreSystemProbeJournal(j) {
		t.Fatal("valid durable system-probe attempt rejected")
	}
	runtime := &Runtime{state: j}
	saved := runtime.SavedDarkRestoreSystemProbeReceipt()
	saved.ResponseSHA = strings64("0")
	if got := runtime.SavedDarkRestoreSystemProbeReceipt(); got == nil || got.ResponseSHA != receipt.ResponseSHA {
		t.Fatal("returned receipt aliases durable journal")
	}

	for name, edit := range map[string]func(*journal){
		"billable probe":        func(v *journal) { v.Usage.Epoch.Bindings[1].Billable = true },
		"foreign active config": func(v *journal) { v.SystemProbe.Request.Expected.ActiveSHA = strings64("0") },
		"foreign epoch":         func(v *journal) { v.SystemProbe.Request.Usage.EpochID = strings32("0") },
		"foreign response":      func(v *journal) { v.SystemProbe.Receipt.ResponseSHA = strings64("0") },
		"expired completion":    func(v *journal) { v.SystemProbe.Receipt.Completed = v.SystemProbe.Request.Expires },
	} {
		t.Run(name, func(t *testing.T) {
			bad := cloneSystemProbeJournal(j)
			edit(&bad)
			if validDarkRestoreSystemProbeJournal(bad) {
				t.Fatal("unbound journal mutation accepted")
			}
		})
	}
	pending := cloneSystemProbeJournal(j)
	pending.SystemProbe.Receipt = nil
	if !validDarkRestoreSystemProbeJournal(pending) {
		t.Fatal("persisted pre-I/O unknown attempt discarded")
	}
}

func cloneSystemProbeJournal(in journal) journal {
	out := in
	good := *in.Good
	good.Raw = append([]byte(nil), in.Good.Raw...)
	good.Listeners = append([]xrayconfig.Listener(nil), in.Good.Listeners...)
	out.Good = &good
	usage := *in.Usage
	usage.Epoch.Bindings = append([]uc.Binding(nil), in.Usage.Epoch.Bindings...)
	out.Usage = &usage
	attempt := *in.SystemProbe
	if in.SystemProbe.Receipt != nil {
		receipt := *in.SystemProbe.Receipt
		attempt.Receipt = &receipt
	}
	out.SystemProbe = &attempt
	return out
}

func strings32(s string) string { return repeatForSystemProbe(s, 32) }
func strings64(s string) string { return repeatForSystemProbe(s, 64) }
func repeatForSystemProbe(s string, n int) string {
	out := ""
	for len(out) < n {
		out += s
	}
	return out[:n]
}
