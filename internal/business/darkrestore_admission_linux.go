//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"reflect"
	"time"
)

type DarkRestoreAdmission struct {
	EventID    string
	ActorID    string
	Registered DarkRestoreRegisteredBirth
	Snapshot   DarkRestoreCutoverSnapshotV1
}

func (s *Store) AuthorizeDarkRestoreAdmission(ctx context.Context, key []byte, token, client, planSHA, event string) (DarkRestoreAdmission, error) {
	var zero DarkRestoreAdmission
	if !eventRE.MatchString(event) {
		return zero, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}

	presentBefore, err := admissionSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if presentBefore {
		var existing []byte
		e := tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_admission_v1 WHERE event_id=?", event).Scan(&existing)
		if e == nil {
			var prior DarkRestoreAdmission
			if json.Unmarshal(existing, &prior) != nil || prior.ActorID != actor || prior.Snapshot.ClientID != client {
				return zero, ErrConflict
			}
			sha, e := prior.Registered.Plan.Digest()
			if e != nil || sha != planSHA {
				return zero, ErrConflict
			}
			return prior, tx.Commit()
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return zero, e
		}
	}
	plan, err := quarantinePlanTx(ctx, tx, key, actor, client)
	if err != nil {
		return zero, err
	}
	sha, err := plan.Digest()
	if err != nil || sha != planSHA {
		return zero, ErrDarkRestoreCutover
	}
	birthPresent, err := birthSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if !birthPresent {
		return zero, ErrDarkRestoreCutover
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_birth_v1 WHERE plan_sha=?", sha).Scan(&raw); err != nil {
		return zero, err
	}
	var registered DarkRestoreRegisteredBirth
	if json.Unmarshal(raw, &registered) != nil || registered.Birth.Lease.Expires <= time.Now().UnixNano() {
		return zero, ErrDarkRestoreCutover
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	snap, _, err := cutoverSnapshotMatchesEvidence(ctx, tx, evidence)
	if err != nil {
		return zero, err
	}
	present, err := admissionSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if !present {
		for _, ddl := range darkRestoreAdmissionDDL {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return zero, err
			}
		}
	}
	out := DarkRestoreAdmission{EventID: event, ActorID: actor, Registered: registered, Snapshot: snap}
	raw, err = json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_native_admission_v1(epoch_id,plan_sha,event_id,receipt_json) VALUES(?,?,?,?)", registered.Epoch.ID, sha, event, raw); err != nil {
		return zero, err
	}
	// A pending record is not a serving-health acknowledgment. Only the exact
	// registered zero-report gate below can release this metered process.
	if _, err = tx.ExecContext(ctx, "UPDATE clients SET deployment_state='pending_node_apply' WHERE id=? AND deployment_state='disabled'", client); err != nil {
		return zero, err
	}
	return out, tx.Commit()
}
func (s *Store) CheckDarkRestoreAdmission(ctx context.Context, q usageledger.AdmissionQuery, principal usageledger.Principal, report uc.Report) error {
	present, err := birthSchema(ctx, q)
	if err != nil || !present {
		return err
	}
	var client string
	err = q.QueryRowContext(ctx, "SELECT client_id FROM darkrestore_native_birth_v1 WHERE epoch_id=?", report.EpochID).Scan(&client)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Actual traffic reports must remain payable even after expiry/revocation.
	// Only the initial zero acknowledgment authorizes opening the listener.
	if report.Sequence != 1 {
		return nil
	}
	present, err = admissionSchema(ctx, q)
	if err != nil || !present {
		return usageledger.ErrAuthority
	}
	var raw []byte
	if err = q.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_admission_v1 WHERE epoch_id=?", report.EpochID).Scan(&raw); err != nil {
		return usageledger.ErrAuthority
	}
	var permit DarkRestoreAdmission
	if json.Unmarshal(raw, &permit) != nil || permit.Registered.Epoch.ID != report.EpochID || permit.Registered.Epoch.NodeID != principal.NodeID || permit.Registered.Epoch.InstallationID != principal.InstallationID || time.Now().UnixNano() >= permit.Registered.Birth.Lease.Expires || report.Quiesced || len(report.Users) != 1 || report.Users[0].ClientID != client || report.Users[0].Uplink != 0 || report.Users[0].Downlink != 0 {
		return usageledger.ErrAuthority
	}
	snap, err := readDarkRestoreCutoverSnapshotV1(ctx, q, permit.Snapshot.SourceSHA, permit.Snapshot.SourceID)
	if err != nil || snap.DeploymentState != "pending_node_apply" {
		return usageledger.ErrAuthority
	}
	snap.DeploymentState = "disabled"
	snap, err = hashDarkRestoreCutoverSnapshotV1(snap)
	if err != nil || snap.StateSHA != permit.Snapshot.StateSHA {
		return usageledger.ErrAuthority
	}
	prepared, err := readCutoverReceipt(ctx, q, client)
	if err != nil || verifyPreparedPlanReceipt(ctx, q, prepared) != nil {
		return usageledger.ErrAuthority
	}
	var stored []byte
	if err = q.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_birth_v1 WHERE epoch_id=?", report.EpochID).Scan(&stored); err != nil {
		return usageledger.ErrAuthority
	}
	original, _ := json.Marshal(permit.Registered)
	if !bytes.Equal(original, stored) {
		return usageledger.ErrAuthority
	}
	return nil
}

const DarkRestoreNativeAdmissionEvidenceScopeV1 = "darkrestore_native_admission_evidence_v1"

// This read-only projection binds the exact prepared delivery, signed process
// birth, owner grant and accepted zero checkpoint. It neither proves long-lived
// serving health nor grants state-transition authority.
type DarkRestoreNativeAdmissionEvidenceV1 struct {
	Scope                      string `json:"scope"`
	ClientID                   string `json:"client_id"`
	TransitionSHA              string `json:"transition_sha256"`
	PreparedContentSHA         string `json:"prepared_content_sha256"`
	PreparedAuthoritySHA       string `json:"prepared_authority_sha256"`
	PlanSHA                    string `json:"plan_sha256"`
	BirthReceiptSHA            string `json:"birth_receipt_sha256"`
	AdmissionEventID           string `json:"admission_event_id"`
	AdmissionReceiptSHA        string `json:"admission_receipt_sha256"`
	EpochID                    string `json:"epoch_id"`
	InitialReportSHA           string `json:"initial_report_sha256"`
	InitialReceiptReceivedUnix int64  `json:"initial_receipt_received_unix"`
	InitialZeroReceiptRecorded bool   `json:"initial_zero_receipt_recorded"`
	NativeApplyRecorded        bool   `json:"native_apply_recorded"`
	ServingHealthRecorded      bool   `json:"serving_health_recorded"`
	AuthorizesMutation         bool   `json:"authorizes_mutation"`
}

type DarkRestoreNativeAdmissionPinsV1 struct {
	ClientID             string
	TransitionSHA        string
	PreparedContentSHA   string
	PreparedAuthoritySHA string
	PlanSHA              string
	BirthReceiptSHA      string
	AdmissionEventID     string
	AdmissionReceiptSHA  string
	EpochID              string
	InitialReportSHA     string
	DocumentSHA          string
}

type DarkRestoreNativeAdmissionValidationV1 struct {
	Scope                      string `json:"scope"`
	EvidenceSHA                string `json:"evidence_sha256"`
	InitialZeroReceiptRecorded bool   `json:"initial_zero_receipt_recorded"`
	NativeApplyRecorded        bool   `json:"native_apply_recorded"`
	ServingHealthRecorded      bool   `json:"serving_health_recorded"`
	AuthorizesMutation         bool   `json:"authorizes_mutation"`
}

func admissionBytesSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func initialAdmissionReportSHA(epoch uc.Epoch) (string, error) {
	canonical, binding, err := uc.CanonicalEpoch(epoch)
	if err != nil || len(canonical.Bindings) != 1 || !canonical.Bindings[0].Billable {
		return "", ErrDarkRestoreCutover
	}
	report := uc.Report{
		Version: 1, EpochID: canonical.ID, BindingSHA: binding, Sequence: 1,
		Users: []uc.Usage{{ClientID: canonical.Bindings[0].ClientID}},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return "", err
	}
	return admissionBytesSHA(raw), nil
}

func ValidateDarkRestoreNativeAdmissionEvidenceV1(e DarkRestoreNativeAdmissionEvidenceV1, p DarkRestoreNativeAdmissionPinsV1) (DarkRestoreNativeAdmissionValidationV1, error) {
	var zero DarkRestoreNativeAdmissionValidationV1
	if e.Scope != DarkRestoreNativeAdmissionEvidenceScopeV1 || !idRE.MatchString(e.ClientID) ||
		!routeSHA.MatchString(e.TransitionSHA) || !routeSHA.MatchString(e.PreparedContentSHA) ||
		!routeSHA.MatchString(e.PreparedAuthoritySHA) || !routeSHA.MatchString(e.PlanSHA) ||
		!routeSHA.MatchString(e.BirthReceiptSHA) || !eventRE.MatchString(e.AdmissionEventID) ||
		!routeSHA.MatchString(e.AdmissionReceiptSHA) || !routeID.MatchString(e.EpochID) ||
		!routeSHA.MatchString(e.InitialReportSHA) || e.InitialReceiptReceivedUnix <= 0 ||
		!e.InitialZeroReceiptRecorded || e.NativeApplyRecorded || e.ServingHealthRecorded ||
		e.AuthorizesMutation || p.ClientID != e.ClientID || p.TransitionSHA != e.TransitionSHA ||
		p.PreparedContentSHA != e.PreparedContentSHA || p.PreparedAuthoritySHA != e.PreparedAuthoritySHA ||
		p.PlanSHA != e.PlanSHA || p.BirthReceiptSHA != e.BirthReceiptSHA ||
		p.AdmissionEventID != e.AdmissionEventID || p.AdmissionReceiptSHA != e.AdmissionReceiptSHA ||
		p.EpochID != e.EpochID || p.InitialReportSHA != e.InitialReportSHA ||
		!routeSHA.MatchString(p.DocumentSHA) {
		return zero, ErrDarkRestoreCutover
	}
	sha, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", e)
	if err != nil || sha != p.DocumentSHA {
		return zero, ErrDarkRestoreCutover
	}
	return DarkRestoreNativeAdmissionValidationV1{
		Scope: DarkRestoreNativeAdmissionEvidenceScopeV1, EvidenceSHA: sha,
		InitialZeroReceiptRecorded: true,
	}, nil
}

// Projection and validation are deliberately separate: the database proves
// consistency of declared local receipts, while independent pins decide which
// exact receipt set an operator intended to review.
func (s *Store) ProjectDarkRestoreNativeAdmissionEvidenceV1(ctx context.Context, token, client string) (DarkRestoreNativeAdmissionEvidenceV1, error) {
	var zero DarkRestoreNativeAdmissionEvidenceV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	present, err := admissionSchema(ctx, tx)
	if err != nil || !present {
		return zero, ErrDarkRestoreCutover
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM darkrestore_native_admission_v1 a JOIN darkrestore_native_birth_v1 b ON b.plan_sha=a.plan_sha AND b.epoch_id=a.epoch_id WHERE b.client_id=?`, client).Scan(&count); err != nil || count != 1 {
		return zero, ErrDarkRestoreCutover
	}
	var event, planSHA string
	var admissionRaw, birthRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT a.event_id,a.plan_sha,a.receipt_json,b.receipt_json FROM darkrestore_native_admission_v1 a JOIN darkrestore_native_birth_v1 b ON b.plan_sha=a.plan_sha AND b.epoch_id=a.epoch_id WHERE b.client_id=?`, client).Scan(&event, &planSHA, &admissionRaw, &birthRaw); err != nil {
		return zero, err
	}
	var permit DarkRestoreAdmission
	var registered DarkRestoreRegisteredBirth
	if len(admissionRaw) == 0 || len(admissionRaw) > 131072 || json.Unmarshal(admissionRaw, &permit) != nil ||
		len(birthRaw) == 0 || len(birthRaw) > 131072 || json.Unmarshal(birthRaw, &registered) != nil ||
		permit.EventID != event || permit.Registered.Epoch.ID != registered.Epoch.ID ||
		permit.Snapshot.ClientID != client || registered.Plan.ClientID != client {
		return zero, ErrDarkRestoreCutover
	}
	canonicalAdmission, err := json.Marshal(permit)
	if err != nil || !bytes.Equal(canonicalAdmission, admissionRaw) {
		return zero, ErrDarkRestoreCutover
	}
	canonicalBirth, err := json.Marshal(registered)
	if err != nil || !bytes.Equal(canonicalBirth, birthRaw) {
		return zero, ErrDarkRestoreCutover
	}
	computedPlanSHA, err := registered.Plan.Digest()
	if err != nil || computedPlanSHA != planSHA ||
		!reflect.DeepEqual(permit.Registered, registered) ||
		permit.Snapshot.StateSHA != registered.Plan.StateSHA {
		return zero, ErrDarkRestoreCutover
	}
	final, err := readCutoverReceipt(ctx, tx, client)
	if err != nil || final == nil || final.SubscriptionDeliveryScope != DarkRestorePreparedDeliveryScopeV1 ||
		verifyPreparedPlanReceipt(ctx, tx, final) != nil {
		return zero, ErrDarkRestoreCutover
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil || evidence == nil || evidence.TransitionSHA != final.TransitionSHA ||
		evidence.ClientID != client || final.ClientID != client {
		return zero, ErrDarkRestoreCutover
	}
	expectedReportSHA, err := initialAdmissionReportSHA(registered.Epoch)
	if err != nil {
		return zero, err
	}
	var reportSHA string
	var quiesced int
	var received int64
	if err = tx.QueryRowContext(ctx, `SELECT sha256,quiesced,received_unix FROM pro_usage_receipts WHERE epoch_id=? AND sequence='1'`, registered.Epoch.ID).Scan(&reportSHA, &quiesced, &received); err != nil ||
		reportSHA != expectedReportSHA || quiesced != 0 || received <= 0 {
		return zero, ErrDarkRestoreCutover
	}
	var deltas, uplink, downlink int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(uplink),0),COALESCE(SUM(downlink),0) FROM pro_usage_deltas WHERE epoch_id=? AND sequence='1' AND client_id=?`, registered.Epoch.ID, client).Scan(&deltas, &uplink, &downlink); err != nil ||
		deltas != 1 || uplink != 0 || downlink != 0 {
		return zero, ErrDarkRestoreCutover
	}
	out := DarkRestoreNativeAdmissionEvidenceV1{
		Scope: DarkRestoreNativeAdmissionEvidenceScopeV1, ClientID: client,
		TransitionSHA: final.TransitionSHA, PreparedContentSHA: final.Prepared.ContentSHA,
		PreparedAuthoritySHA: final.Prepared.AuthoritySHA, PlanSHA: planSHA,
		BirthReceiptSHA: admissionBytesSHA(birthRaw), AdmissionEventID: event,
		AdmissionReceiptSHA: admissionBytesSHA(admissionRaw), EpochID: registered.Epoch.ID,
		InitialReportSHA: reportSHA, InitialReceiptReceivedUnix: received,
		InitialZeroReceiptRecorded: true,
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}
