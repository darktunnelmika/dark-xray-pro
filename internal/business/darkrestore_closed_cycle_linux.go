//go:build linux

package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const DarkRestoreNativeClosedCycleEvidenceScopeV1 = "darkrestore_native_closed_cycle_evidence_v1"

// DarkRestoreNativeClosedCycleEvidenceV1 proves only one immutable, already
// sealed destination epoch. It does not turn a past observed execution into
// authority for a future apply, subscription publication, activation, source
// retirement, or inventory-completeness claim.
type DarkRestoreNativeClosedCycleEvidenceV1 struct {
	Scope                    string `json:"scope"`
	ClientID                 string `json:"client_id"`
	AdmissionEvidenceSHA     string `json:"admission_evidence_sha256"`
	TransitionSHA            string `json:"transition_sha256"`
	PreparedContentSHA       string `json:"prepared_content_sha256"`
	PreparedAuthoritySHA     string `json:"prepared_authority_sha256"`
	PlanSHA                  string `json:"plan_sha256"`
	EpochID                  string `json:"epoch_id"`
	FinalSequence            uint64 `json:"final_sequence"`
	FinalReportSHA           string `json:"final_report_sha256"`
	FinalReceiptReceivedUnix int64  `json:"final_receipt_received_unix"`
	CycleUplink              int64  `json:"cycle_uplink"`
	CycleDownlink            int64  `json:"cycle_downlink"`
	FinalReceiptRecorded     bool   `json:"final_receipt_recorded"`
	SealedCycleRecorded      bool   `json:"sealed_cycle_recorded"`
	ObservedTrafficRecorded  bool   `json:"observed_traffic_recorded"`
	NativeApplyRecorded      bool   `json:"native_apply_recorded"`
	ServingHealthRecorded    bool   `json:"serving_health_recorded"`
	AuthorizesMutation       bool   `json:"authorizes_mutation"`
}

type DarkRestoreNativeClosedCyclePinsV1 struct {
	ClientID                 string
	AdmissionEvidenceSHA     string
	TransitionSHA            string
	PreparedContentSHA       string
	PreparedAuthoritySHA     string
	PlanSHA                  string
	EpochID                  string
	FinalSequence            uint64
	FinalReportSHA           string
	FinalReceiptReceivedUnix int64
	CycleUplink              int64
	CycleDownlink            int64
	DocumentSHA              string
}

type DarkRestoreNativeClosedCycleValidationV1 struct {
	Scope                   string `json:"scope"`
	EvidenceSHA             string `json:"evidence_sha256"`
	FinalReceiptRecorded    bool   `json:"final_receipt_recorded"`
	SealedCycleRecorded     bool   `json:"sealed_cycle_recorded"`
	ObservedTrafficRecorded bool   `json:"observed_traffic_recorded"`
	NativeApplyRecorded     bool   `json:"native_apply_recorded"`
	ServingHealthRecorded   bool   `json:"serving_health_recorded"`
	AuthorizesMutation      bool   `json:"authorizes_mutation"`
}

func ValidateDarkRestoreNativeClosedCycleEvidenceV1(e DarkRestoreNativeClosedCycleEvidenceV1, p DarkRestoreNativeClosedCyclePinsV1) (DarkRestoreNativeClosedCycleValidationV1, error) {
	var zero DarkRestoreNativeClosedCycleValidationV1
	if e.Scope != DarkRestoreNativeClosedCycleEvidenceScopeV1 ||
		!idRE.MatchString(e.ClientID) ||
		!routeSHA.MatchString(e.AdmissionEvidenceSHA) ||
		!routeSHA.MatchString(e.TransitionSHA) ||
		!routeSHA.MatchString(e.PreparedContentSHA) ||
		!routeSHA.MatchString(e.PreparedAuthoritySHA) ||
		!routeSHA.MatchString(e.PlanSHA) ||
		!routeID.MatchString(e.EpochID) ||
		e.FinalSequence <= 1 ||
		!routeSHA.MatchString(e.FinalReportSHA) ||
		e.FinalReceiptReceivedUnix <= 0 ||
		e.CycleUplink < 0 || e.CycleDownlink < 0 ||
		(e.CycleUplink == 0 && e.CycleDownlink == 0) ||
		e.CycleUplink > math.MaxInt64-e.CycleDownlink ||
		!e.FinalReceiptRecorded || !e.SealedCycleRecorded || !e.ObservedTrafficRecorded ||
		e.NativeApplyRecorded || e.ServingHealthRecorded || e.AuthorizesMutation ||
		p.ClientID != e.ClientID ||
		p.AdmissionEvidenceSHA != e.AdmissionEvidenceSHA ||
		p.TransitionSHA != e.TransitionSHA ||
		p.PreparedContentSHA != e.PreparedContentSHA ||
		p.PreparedAuthoritySHA != e.PreparedAuthoritySHA ||
		p.PlanSHA != e.PlanSHA ||
		p.EpochID != e.EpochID ||
		p.FinalSequence != e.FinalSequence ||
		p.FinalReportSHA != e.FinalReportSHA ||
		p.FinalReceiptReceivedUnix != e.FinalReceiptReceivedUnix ||
		p.CycleUplink != e.CycleUplink ||
		p.CycleDownlink != e.CycleDownlink ||
		!routeSHA.MatchString(p.DocumentSHA) {
		return zero, ErrDarkRestoreCutover
	}
	sha, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-CLOSED-CYCLE-EVIDENCE-v1", e)
	if err != nil || sha != p.DocumentSHA {
		return zero, ErrDarkRestoreCutover
	}
	return DarkRestoreNativeClosedCycleValidationV1{
		Scope:                   DarkRestoreNativeClosedCycleEvidenceScopeV1,
		EvidenceSHA:             sha,
		FinalReceiptRecorded:    true,
		SealedCycleRecorded:     true,
		ObservedTrafficRecorded: true,
	}, nil
}

// ProjectDarkRestoreNativeClosedCycleEvidenceV1 is a read-only projection. The
// owner, admission, sealed cursor and both ledgers are checked in one snapshot.
func (s *Store) ProjectDarkRestoreNativeClosedCycleEvidenceV1(ctx context.Context, token, client string) (DarkRestoreNativeClosedCycleEvidenceV1, error) {
	var zero DarkRestoreNativeClosedCycleEvidenceV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	admission, err := projectDarkRestoreNativeAdmissionEvidenceTx(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	admissionSHA, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", admission)
	if err != nil {
		return zero, err
	}
	var state string
	var epochRaw, cursorRaw []byte
	if err = tx.QueryRowContext(ctx, "SELECT state,epoch_json,cursor_json FROM pro_usage_epochs WHERE epoch_id=?", admission.EpochID).Scan(&state, &epochRaw, &cursorRaw); err != nil || state != "sealed" {
		return zero, ErrDarkRestoreCutover
	}
	var epoch uc.Epoch
	var cursor uc.Cursor
	if len(epochRaw) > 131072 || json.Unmarshal(epochRaw, &epoch) != nil || epoch.ID != admission.EpochID ||
		len(cursorRaw) > 131072 || json.Unmarshal(cursorRaw, &cursor) != nil ||
		cursor.EpochID != admission.EpochID || cursor.Sequence <= 1 || !cursor.Quiesced {
		return zero, ErrDarkRestoreCutover
	}
	report, err := initialAdmissionReport(epoch)
	if err != nil || report.SHA != admission.InitialReportSHA {
		return zero, ErrDarkRestoreCutover
	}
	report.Sequence, report.Quiesced, report.Users, report.SHA = cursor.Sequence, true, cursor.Users, cursor.ReportSHA
	if uc.VerifyReport(epoch, report) != nil {
		return zero, ErrDarkRestoreCutover
	}
	var customer uc.Usage
	for i, binding := range epoch.Bindings {
		user := cursor.Users[i]
		if binding.ClientID != user.ClientID {
			return zero, ErrDarkRestoreCutover
		}
		if binding.Billable {
			if binding.ClientID != client {
				return zero, ErrDarkRestoreCutover
			}
			customer = user
			continue
		}
		var total, matching, up, down int64
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(CASE WHEN probe_id=? THEN 1 END),COALESCE(SUM(uplink),0),COALESCE(SUM(downlink),0) FROM pro_probe_deltas WHERE epoch_id=?`, binding.ClientID, epoch.ID).Scan(&total, &matching, &up, &down); err != nil ||
			total == 0 || total != matching || up != user.Uplink || down != user.Downlink {
			return zero, ErrDarkRestoreCutover
		}
	}
	var finalSHA string
	var quiesced int
	var received int64
	if err = tx.QueryRowContext(ctx, "SELECT sha256,quiesced,received_unix FROM pro_usage_receipts WHERE epoch_id=? AND sequence=?", admission.EpochID, cursor.Sequence).Scan(&finalSHA, &quiesced, &received); err != nil ||
		finalSHA != cursor.ReportSHA || quiesced != 1 || received < admission.InitialReceiptReceivedUnix {
		return zero, ErrDarkRestoreCutover
	}
	var terminalCount int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_receipts WHERE epoch_id=? AND quiesced=1", admission.EpochID).Scan(&terminalCount); err != nil || terminalCount != 1 {
		return zero, ErrDarkRestoreCutover
	}
	var rows, clients int64
	var onlyClient string
	var uplink, downlink int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(DISTINCT client_id),COALESCE(MIN(client_id),''),COALESCE(SUM(uplink),0),COALESCE(SUM(downlink),0) FROM pro_usage_deltas WHERE epoch_id=?`, admission.EpochID).Scan(&rows, &clients, &onlyClient, &uplink, &downlink); err != nil ||
		rows < 2 || clients != 1 || onlyClient != client || customer.ClientID != client ||
		uplink != customer.Uplink || downlink != customer.Downlink ||
		uplink < 0 || downlink < 0 || (uplink == 0 && downlink == 0) ||
		uplink > math.MaxInt64-downlink {
		return zero, ErrDarkRestoreCutover
	}
	out := DarkRestoreNativeClosedCycleEvidenceV1{
		Scope:                    DarkRestoreNativeClosedCycleEvidenceScopeV1,
		ClientID:                 client,
		AdmissionEvidenceSHA:     admissionSHA,
		TransitionSHA:            admission.TransitionSHA,
		PreparedContentSHA:       admission.PreparedContentSHA,
		PreparedAuthoritySHA:     admission.PreparedAuthoritySHA,
		PlanSHA:                  admission.PlanSHA,
		EpochID:                  admission.EpochID,
		FinalSequence:            cursor.Sequence,
		FinalReportSHA:           finalSHA,
		FinalReceiptReceivedUnix: received,
		CycleUplink:              uplink,
		CycleDownlink:            downlink,
		FinalReceiptRecorded:     true,
		SealedCycleRecorded:      true,
		ObservedTrafficRecorded:  true,
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}
