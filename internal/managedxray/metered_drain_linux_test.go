//go:build linux

package managedxray

import (
	"bytes"
	"strings"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestMeteredDrainRequiresStoppedExactSealedEpoch(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	now := time.Now().UnixNano()
	c := TransactionCommand{Version: 2, RuntimeInstance: strings.Repeat("1", 32), ID: strings.Repeat("2", 32), StageID: strings.Repeat("3", 32), NodeID: "drain-node", InstallationID: testInstall, Serial: 3, RuntimeSequence: 2, Action: "drain", PayloadSHA: strings.Repeat("a", 64), BinarySHA: strings.Repeat("b", 64), Issued: now, Expires: now + int64(time.Minute), Expected: RuntimeCondition{Highwater: 2, SavedSequence: 2, SavedSHA: strings.Repeat("e", 64), ActiveSequence: 2, ActiveSHA: strings.Repeat("e", 64), ProcessReady: true}, Usage: &MeteredCondition{EpochID: strings.Repeat("4", 32), EpochSHA: strings.Repeat("c", 64), AdmissionSHA: strings.Repeat("d", 64)}}
	var err error
	c, err = SignTransactionCommand(key, c)
	if err != nil {
		t.Fatal(err)
	}
	seal := uc.Receipt{EpochID: c.Usage.EpochID, Sequence: 9, SHA: strings.Repeat("f", 64)}
	r := TransactionReceipt{Version: 2, CommandID: c.ID, CommandSHA: hashJSON(c), Serial: c.Serial, Action: c.Action, StageID: c.StageID, NodeID: c.NodeID, InstallationID: c.InstallationID, RuntimeInstance: c.RuntimeInstance, Outcome: "drained", ObservedAt: now, Observed: Snapshot{Highwater: 2, SavedSequence: 2, SavedSHA: c.Expected.SavedSHA, Usage: &UsageObservationStatus{Required: true, Settled: true}}, Metered: &MeteredEvidence{After: MeteredCondition{EpochID: c.Usage.EpochID, EpochSHA: c.Usage.EpochSHA, AdmissionSHA: strings.Repeat("6", 64), Settlement: seal}, PriorSettlement: seal}}
	r = signTransactionReceipt(key, r)
	if !VerifyMeteredTransactionReceipt(key, c, r) || VerifyTransactionReceipt(key, c, r) {
		t.Fatal("drain proof rejected or downgraded")
	}
	for _, mode := range []string{"ready", "active", "unsettled", "missing-seal", "changed-epoch", "changed-revision"} {
		bad := cloneMeteredReceipt(r)
		switch mode {
		case "ready":
			bad.Observed.ProcessReady = true
		case "active":
			bad.Observed.ActiveSequence = 2
		case "unsettled":
			bad.Observed.Usage.Settled = false
		case "missing-seal":
			bad.Metered.PriorSettlement = uc.Receipt{}
		case "changed-epoch":
			bad.Metered.After.EpochID = strings.Repeat("7", 32)
		case "changed-revision":
			bad.Observed.Highwater++
		}
		bad = signTransactionReceipt(key, bad)
		if VerifyMeteredTransactionReceipt(key, c, bad) {
			t.Fatal("accepted unsafe drain", mode)
		}
	}
	bad := c
	bad.Version = 1
	bad.Usage = nil
	if _, err := SignTransactionCommand(key, bad); err == nil {
		t.Fatal("unmetered drain accepted")
	}
	bad = c
	bad.Lease = Lease{Sequence: 2}
	if _, err := SignTransactionCommand(key, bad); err == nil {
		t.Fatal("drain granted runtime lease")
	}
}
