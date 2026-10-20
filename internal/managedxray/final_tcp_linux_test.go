//go:build linux

package managedxray

import (
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestFinalTCPJournalRejectsLegacyOrIncompleteProof(t *testing.T) {
	epoch := strings.Repeat("a", 32)
	receipt := uc.Receipt{EpochID: epoch, Sequence: 3, SHA: strings.Repeat("b", 64)}
	j := journal{Version: 5, Usage: &observedUsage{Epoch: uc.Epoch{ID: epoch}}, Admission: &usageAdmission{FinalCounterProtocol: uc.FinalTCPProtocol, TrafficBarrier: pretrafficActivationPossible, DrainNonce: strings.Repeat("c", 64), FinalReceipt: &receipt}}
	if !validFinalTCPJournal(j) || !validPretrafficJournal(j) {
		t.Fatal("valid pending final receipt rejected")
	}
	for _, mode := range []string{"legacy", "wrong-protocol", "missing-nonce", "uppercase-nonce", "pretraffic", "foreign-epoch", "bad-receipt", "wrong-seal"} {
		t.Run(mode, func(t *testing.T) {
			bad := j
			a := *j.Admission
			bad.Admission = &a
			switch mode {
			case "legacy":
				bad.Version = 4
			case "wrong-protocol":
				a.FinalCounterProtocol = "unknown"
			case "missing-nonce":
				a.DrainNonce = ""
			case "uppercase-nonce":
				a.DrainNonce = strings.Repeat("C", 64)
			case "pretraffic":
				a.TrafficBarrier = pretrafficNeverInstalled
			case "foreign-epoch":
				r := receipt
				r.EpochID = strings.Repeat("d", 32)
				a.FinalReceipt = &r
			case "bad-receipt":
				r := receipt
				r.Sequence = 0
				a.FinalReceipt = &r
			case "wrong-seal":
				r := receipt
				r.Sequence++
				a.Settlement = &r
			}
			if validFinalTCPJournal(bad) && validPretrafficJournal(bad) {
				t.Fatal("invalid settlement proof accepted")
			}
		})
	}
}
