//go:build linux

package api

import (
	"encoding/json"
	"strings"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
)

func TestManagedSoakFinalEvidenceRejectsInvalidReceipts(t *testing.T) {
	for _, name := range []string{"valid", "missing", "http_error", "malformed", "wrong_epoch", "wrong_sequence", "wrong_hash", "not_final", "bad_seal", "wrong_identity", "wrong_generation", "wrong_total", "hold", "overflow", "duplicate_principal", "conflicting_retry", "replay"} {
		t.Run(name, func(t *testing.T) {
			report := ix.UsageReport{EpochID: "fixture-epoch", NodeID: "de", InstallationID: strings.Repeat("b", 32), Generation: 1, Sequence: 9, ProcessIdentity: "fixture-process", Final: true, FinalNonce: strings.Repeat("a", 64), Counters: []ix.Counter{{PrincipalID: "fixture-principal", Upload: 30, Download: 70}}}
			if name == "overflow" {
				report.Counters[0].Upload = ^uint64(0)
			}
			if name == "duplicate_principal" {
				report.Counters = append(report.Counters, report.Counters[0])
			}
			report.FinalProofSHA = inboundruntime.FinalProofSHA(report.ProcessIdentity, report.FinalNonce, report.Counters)
			if name == "bad_seal" {
				report.FinalProofSHA = "wrong"
			}
			receipt := ix.UsageReceipt{EpochID: report.EpochID, Sequence: report.Sequence, Final: true, SHA256: inboundruntime.ReportSHA(report)}
			switch name {
			case "wrong_epoch":
				receipt.EpochID = "other"
			case "wrong_sequence":
				receipt.Sequence++
			case "wrong_hash":
				receipt.SHA256 = "wrong"
			case "not_final":
				receipt.Final = false
			case "replay":
				receipt.Replayed = true
			}
			raw, _ := json.Marshal(report)
			body, _ := json.Marshal(receipt)
			status := 200
			if name == "http_error" {
				status = 503
			}
			if name == "malformed" {
				body = []byte("{")
			}
			var witness managedFinalWitness
			if name != "missing" {
				witness.capture(raw, body, status)
			}
			if name == "conflicting_retry" {
				report.Sequence++
				receipt.Sequence = report.Sequence
				receipt.SHA256 = inboundruntime.ReportSHA(report)
				raw, _ = json.Marshal(report)
				body, _ = json.Marshal(receipt)
				witness.capture(raw, body, 200)
			}
			process, gen, want, holds := report.ProcessIdentity, report.Generation, int64(100), int64(0)
			if name == "wrong_identity" {
				process = "other"
			}
			if name == "wrong_generation" {
				gen++
			}
			if name == "wrong_total" {
				want++
			}
			if name == "hold" {
				holds = 1
			}
			evidence, err := witness.evidence(report.EpochID, process, gen, want, holds)
			valid := name == "valid" || name == "replay"
			if (err == nil) != valid {
				t.Fatalf("valid=%v err=%v", valid, err)
			}
			if valid && !json.Valid(evidence) {
				t.Fatal("evidence not JSON")
			}
		})
	}
}
