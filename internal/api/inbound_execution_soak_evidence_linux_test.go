//go:build linux

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// These are the actual synthetic Agent request and HUB response bodies, not
// a receipt reconstructed from the expected counters or a PASS log line.
type managedSoakFinalEvidence struct {
	Report         json.RawMessage `json:"report"`
	Receipt        json.RawMessage `json:"receipt"`
	HTTPStatus     int             `json:"http_status"`
	PrincipalID    string          `json:"principal_id"`
	CommittedBytes int64           `json:"committed_bytes"`
}

func (w *managedSoakWitness) recordFinal(report []byte, status int, receipt []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finals = append(w.finals, managedSoakFinalEvidence{
		Report: bytes.Clone(report), Receipt: bytes.Clone(receipt), HTTPStatus: status,
	})
}

func (w *managedSoakWitness) finalEvidence(principal string, want int64) (managedSoakFinalEvidence, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var none managedSoakFinalEvidence
	if w.changed || w.epoch == "" || w.process == "" || w.generation == 0 || len(w.finals) != 1 || principal == "" || want <= 0 {
		return none, fmt.Errorf("missing or ambiguous final receipt/runtime identity")
	}
	evidence := w.finals[0]
	var report ix.UsageReport
	var receipt ix.UsageReceipt
	if evidence.HTTPStatus != 200 || json.Unmarshal(evidence.Report, &report) != nil || json.Unmarshal(evidence.Receipt, &receipt) != nil {
		return none, fmt.Errorf("final request/response was not accepted JSON")
	}
	if !report.Final || !receipt.Final || report.EpochID != w.epoch || report.ProcessIdentity != w.process || report.Generation != w.generation || report.Sequence == 0 || receipt.EpochID != report.EpochID || receipt.Sequence != report.Sequence || receipt.SHA256 != ix.Digest(report) {
		return none, fmt.Errorf("final receipt does not bind the observed epoch and request")
	}
	proof := ix.Digest(struct {
		Process  string       `json:"process"`
		Nonce    string       `json:"nonce"`
		Counters []ix.Counter `json:"counters"`
	}{report.ProcessIdentity, report.FinalNonce, report.Counters})
	if len(report.FinalNonce) != 64 || report.FinalProofSHA != proof {
		return none, fmt.Errorf("missing or inconsistent native final proof")
	}
	found := 0
	for _, counter := range report.Counters {
		if counter.PrincipalID == principal {
			found++
			if counter.Upload > uint64(want) || counter.Download > uint64(want) || counter.Upload+counter.Download != uint64(want) {
				return none, fmt.Errorf("final customer counters differ from committed payload")
			}
		}
	}
	if found != 1 {
		return none, fmt.Errorf("missing or duplicate customer principal")
	}
	evidence.PrincipalID, evidence.CommittedBytes = principal, want
	return evidence, nil
}

func (w *managedSoakWitness) logBoundary(t *testing.T, phase string, at time.Time) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	raw, err := json.Marshal(struct {
		UnixNano        int64  `json:"unix_ns"`
		EpochID         string `json:"epoch_id"`
		Generation      uint64 `json:"generation"`
		ProcessIdentity string `json:"process_identity"`
	}{at.UnixNano(), w.epoch, w.generation, w.process})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MANAGED_SOAK_PROCESS_%s %s", phase, raw)
}

func TestManagedSoakFinalEvidenceRejectsIncompleteProof(t *testing.T) {
	for _, name := range []string{"valid", "missing", "duplicate", "rejected", "malformed", "wrong_epoch", "wrong_process", "wrong_generation", "wrong_sequence", "wrong_hash", "not_final", "wrong_proof", "wrong_principal", "wrong_bytes", "changed_epoch"} {
		t.Run(name, func(t *testing.T) {
			report := ix.UsageReport{EpochID: "fixture-epoch", NodeID: "de", InstallationID: strings.Repeat("b", 32), Generation: 1, Sequence: 3, Final: true, ProcessIdentity: "fixture-process", FinalNonce: strings.Repeat("a", 64), Counters: []ix.Counter{{PrincipalID: "fixture-client", Upload: 120, Download: 250}}}
			report.FinalProofSHA = ix.Digest(struct {
				Process string `json:"process"`
				Nonce string `json:"nonce"`
				Counters []ix.Counter `json:"counters"`
			}{report.ProcessIdentity, report.FinalNonce, report.Counters})
			receipt := ix.UsageReceipt{EpochID: report.EpochID, Sequence: report.Sequence, SHA256: ix.Digest(report), Final: true}
			w := managedSoakWitness{epoch: report.EpochID, process: report.ProcessIdentity, generation: 1}
			principal, want, status := "fixture-client", int64(370), 200
			switch name {
			case "rejected": status = 503
			case "wrong_epoch": receipt.EpochID = "other"
			case "wrong_process": w.process = "other"
			case "wrong_generation": w.generation = 2
			case "wrong_sequence": receipt.Sequence++
			case "wrong_hash": receipt.SHA256 = strings.Repeat("f", 64)
			case "not_final": receipt.Final = false
			case "wrong_proof": report.FinalProofSHA = strings.Repeat("f", 64); receipt.SHA256 = ix.Digest(report)
			case "wrong_principal": principal = "other"
			case "wrong_bytes": want++
			case "changed_epoch": w.changed = true
			}
			rawReport, _ := json.Marshal(report)
			rawReceipt, _ := json.Marshal(receipt)
			if name == "malformed" { rawReceipt = []byte("invalid") }
			if name != "missing" { w.recordFinal(rawReport, status, rawReceipt) }
			if name == "duplicate" { w.recordFinal(rawReport, status, rawReceipt) }
			got, err := w.finalEvidence(principal, want)
			if (err == nil) != (name == "valid") { t.Fatalf("unexpected acceptance: %v", err) }
			if err == nil && (!bytes.Equal(got.Report, rawReport) || !bytes.Equal(got.Receipt, rawReceipt)) { t.Fatal("rewrote wire evidence") }
		})
	}
}
