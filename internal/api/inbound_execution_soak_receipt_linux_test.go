//go:build linux

package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
)

// Test-only evidence: preserve the actual final HTTP exchange after the real
// handler commits it. Nothing here issues a receipt or changes worker state.
type managedFinalWitness struct {
	mu                      sync.Mutex
	reportWire, receiptWire []byte
	report                  ix.UsageReport
	receipt                 ix.UsageReceipt
	captured                int64
	err                     error
}

func (w *managedFinalWitness) capture(reportWire, receiptWire []byte, status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var report ix.UsageReport
	var receipt ix.UsageReceipt
	if status != 200 || json.Unmarshal(reportWire, &report) != nil || json.Unmarshal(receiptWire, &receipt) != nil {
		w.err = fmt.Errorf("final exchange was not a valid HTTP 200 receipt")
		return
	}
	if !report.Final || !receipt.Final || report.EpochID == "" || report.ProcessIdentity == "" || report.Generation == 0 || report.Sequence == 0 ||
		receipt.EpochID != report.EpochID || receipt.Sequence != report.Sequence || receipt.SHA256 != inboundruntime.ReportSHA(report) ||
		report.FinalNonce == "" || report.FinalProofSHA != inboundruntime.FinalProofSHA(report.ProcessIdentity, report.FinalNonce, report.Counters) {
		w.err = fmt.Errorf("final report/receipt proof mismatch")
		return
	}
	if w.captured != 0 && inboundruntime.ReportSHA(w.report) != inboundruntime.ReportSHA(report) {
		w.err = fmt.Errorf("conflicting final reports")
		return
	}
	w.report, w.receipt = report, receipt
	w.reportWire = append([]byte(nil), reportWire...)
	w.receiptWire = append([]byte(nil), receiptWire...)
	w.captured = time.Now().UnixNano()
}

func (w *managedFinalWitness) evidence(epoch, process string, generation uint64, want int64, holds int64) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return nil, w.err
	}
	if w.captured == 0 {
		return nil, fmt.Errorf("missing actual final HTTP receipt")
	}
	if w.report.EpochID != epoch || w.report.ProcessIdentity != process || w.report.Generation != generation {
		return nil, fmt.Errorf("final receipt belongs to another traffic incarnation")
	}
	if want <= 0 || holds != 0 || len(w.report.Counters) == 0 {
		return nil, fmt.Errorf("unsettled final accounting")
	}
	var total uint64
	seen := map[string]bool{}
	for _, counter := range w.report.Counters {
		if counter.PrincipalID == "" || seen[counter.PrincipalID] {
			return nil, fmt.Errorf("duplicate or absent final principal")
		}
		seen[counter.PrincipalID] = true
		for _, n := range []uint64{counter.Upload, counter.Download} {
			if n > math.MaxInt64-total {
				return nil, fmt.Errorf("final counter overflow")
			}
			total += n
		}
	}
	if total != uint64(want) {
		return nil, fmt.Errorf("final counters differ from committed bytes")
	}
	canonical, err := json.Marshal(w.report)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Version     int    `json:"version"`
		Captured    int64  `json:"captured_unix_ns"`
		ReportWire  string `json:"report_wire_b64"`
		ReceiptWire string `json:"receipt_wire_b64"`
		Canonical   string `json:"report_canonical_b64"`
		Committed   int64  `json:"committed_bytes"`
		Holds       int64  `json:"holds"`
	}{1, w.captured, base64.StdEncoding.EncodeToString(w.reportWire), base64.StdEncoding.EncodeToString(w.receiptWire), base64.StdEncoding.EncodeToString(canonical), want, holds})
}

func (w *managedFinalWitness) emit(t *testing.T, identity *managedSoakWitness, want int64, holds int64) {
	t.Helper()
	identity.mu.Lock()
	epoch, process, generation, changed := identity.epoch, identity.process, identity.generation, identity.changed
	identity.mu.Unlock()
	if changed {
		t.Fatal("final traffic identity changed")
	}
	raw, err := w.evidence(epoch, process, generation, want, holds)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MANAGED_FIXED_VERSION_SOAK_FINAL_EVIDENCE %s", raw)
}
