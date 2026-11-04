//go:build linux

package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func servingHealthPins(e DarkRestoreServingHealthEvidenceV1) (DarkRestoreServingHealthPinsV1, error) {
	qRaw, err := json.Marshal(e.Request)
	if err != nil {
		return DarkRestoreServingHealthPinsV1{}, err
	}
	rRaw, err := json.Marshal(e.Receipt)
	if err != nil {
		return DarkRestoreServingHealthPinsV1{}, err
	}
	p := DarkRestoreServingHealthPinsV1{
		ClientID: e.ClientID, TransitionSHA: e.TransitionSHA, PlanSHA: e.PlanSHA,
		ConfigSHA: e.ConfigSHA, EpochID: e.EpochID,
		AdmissionEvidenceSHA:    e.AdmissionEvidenceSHA,
		ListenerApplyReceiptSHA: e.ListenerApplyReceiptSHA,
		EndpointSHA:             e.EndpointSHA, RequestSHA: admissionBytesSHA(qRaw),
		ReceiptSHA: admissionBytesSHA(rRaw),
	}
	p.DocumentSHA, err = cutoverSnapshotDigest("DARK-PRO-SERVING-HEALTH-EVIDENCE-v1", e)
	return p, err
}

func servingHealthSourcesTx(ctx context.Context, tx *sql.Tx, client string) (DarkRestoreNativeAdmissionEvidenceV1, DarkRestoreRegisteredBirth, DarkRestoreListenerApply, error) {
	var admission DarkRestoreNativeAdmissionEvidenceV1
	var registered DarkRestoreRegisteredBirth
	var listener DarkRestoreListenerApply
	admission, err := projectDarkRestoreNativeAdmissionEvidenceTx(ctx, tx, client)
	if err != nil || !admission.ListenerApplyRecorded {
		return admission, registered, listener, ErrDarkRestoreCutover
	}
	var birthRaw, listenerRaw []byte
	if err = tx.QueryRowContext(ctx, "SELECT b.receipt_json,l.receipt_json FROM darkrestore_native_birth_v1 b JOIN darkrestore_listener_apply_v1 l ON l.plan_sha=b.plan_sha WHERE b.client_id=? AND b.plan_sha=?", client, admission.PlanSHA).Scan(&birthRaw, &listenerRaw); err != nil {
		return admission, registered, listener, ErrDarkRestoreCutover
	}
	if len(birthRaw) == 0 || len(birthRaw) > 131072 || len(listenerRaw) == 0 || len(listenerRaw) > 16384 ||
		json.Unmarshal(birthRaw, &registered) != nil || json.Unmarshal(listenerRaw, &listener) != nil {
		return admission, registered, listener, ErrDarkRestoreCutover
	}
	canonicalBirth, _ := json.Marshal(registered)
	canonicalListener, _ := json.Marshal(listener)
	planSHA, err := registered.Plan.Digest()
	if err != nil || !bytes.Equal(canonicalBirth, birthRaw) || !bytes.Equal(canonicalListener, listenerRaw) ||
		planSHA != admission.PlanSHA || listener.PlanSHA != admission.PlanSHA ||
		listener.EpochID != admission.EpochID || listener.Digest() != admission.ListenerApplyReceiptSHA ||
		verifyListenerApplyTx(ctx, tx, listener) != nil {
		return admission, registered, listener, ErrDarkRestoreCutover
	}
	return admission, registered, listener, nil
}

func servingHealthMatchesSources(e DarkRestoreServingHealthEvidenceV1, admission DarkRestoreNativeAdmissionEvidenceV1, registered DarkRestoreRegisteredBirth, listener DarkRestoreListenerApply) bool {
	admissionSHA, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", admission)
	epochSHA, epochErr := mx.MeteredEpochSHA(registered.Epoch)
	q := e.Request
	expectedUsage := mx.MeteredCondition{EpochID: registered.Epoch.ID, EpochSHA: epochSHA, AdmissionSHA: registered.Plan.PreparedAdmissionSHA}
	return err == nil && epochErr == nil && registered.Plan.SystemProbeID != "" &&
		routeSHA.MatchString(registered.Plan.SystemProbeCredentialSHA) &&
		routeSHA.MatchString(registered.Plan.PreparedAdmissionSHA) &&
		e.ClientID == admission.ClientID && e.TransitionSHA == admission.TransitionSHA &&
		e.PlanSHA == admission.PlanSHA && e.ConfigSHA == registered.Plan.ConfigSHA &&
		e.EpochID == admission.EpochID && e.AdmissionEvidenceSHA == admissionSHA &&
		e.ListenerApplyReceiptSHA == admission.ListenerApplyReceiptSHA &&
		e.ListenerAppliedUnixNano == listener.AppliedUnixNano &&
		q.NodeID == registered.Plan.NodeID && q.InstallationID == registered.Plan.InstallationID &&
		q.InboundID == registered.Plan.Prepared.InboundID &&
		q.SystemProbeID == registered.Plan.SystemProbeID &&
		q.CredentialSHA == registered.Plan.SystemProbeCredentialSHA &&
		q.InitialApplySHA == admission.ListenerApplyReceiptSHA &&
		q.Expected.ActiveSHA == registered.Plan.ConfigSHA &&
		q.Expected.ActiveSequence == registered.Birth.Lease.Sequence &&
		q.Usage == expectedUsage &&
		q.Issued > listener.AppliedUnixNano && q.Expires <= registered.Birth.Lease.Expires
}

func readDarkRestoreServingHealthTx(ctx context.Context, tx *sql.Tx, client string) (DarkRestoreServingHealthEvidenceV1, error) {
	var zero DarkRestoreServingHealthEvidenceV1
	present, err := darkRestoreServingHealthSchema(ctx, tx)
	if err != nil || !present {
		return zero, ErrDarkRestoreCutover
	}
	var planSHA, requestSHA, receiptSHA string
	var raw []byte
	var recorded int64
	if err = tx.QueryRowContext(ctx, "SELECT plan_sha,request_sha,receipt_sha,evidence_json,recorded_unix_nano FROM darkrestore_serving_health_v1 WHERE client_id=?", client).Scan(&planSHA, &requestSHA, &receiptSHA, &raw, &recorded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, ErrNotFound
		}
		return zero, err
	}
	var e DarkRestoreServingHealthEvidenceV1
	if len(raw) == 0 || len(raw) > 262144 || json.Unmarshal(raw, &e) != nil {
		return zero, ErrUnavailable
	}
	canonical, _ := json.Marshal(e)
	pins, err := servingHealthPins(e)
	if err != nil || !bytes.Equal(canonical, raw) || e.ClientID != client || e.PlanSHA != planSHA ||
		e.RecordedUnixNano != recorded || pins.RequestSHA != requestSHA || pins.ReceiptSHA != receiptSHA {
		return zero, ErrUnavailable
	}
	admission, registered, listener, err := servingHealthSourcesTx(ctx, tx, client)
	if err != nil || !servingHealthMatchesSources(e, admission, registered, listener) {
		return zero, ErrUnavailable
	}
	return e, nil
}

// RecordDarkRestoreServingHealthV1 stores one exact real-probe document. It is
// historical evidence only: no client, wallet, eligibility, listener, native
// apply, subscription publication or activation state is changed.
func (s *Store) RecordDarkRestoreServingHealthV1(ctx context.Context, token, client string, agentKey []byte, endpoint mx.ProtocolHealthEndpoint, request mx.ProtocolHealthRequest, receipt mx.ProtocolHealthReceipt) (DarkRestoreServingHealthEvidenceV1, error) {
	return s.recordDarkRestoreServingHealthV1(ctx, token, client, agentKey, endpoint, request, receipt, time.Now)
}

func (s *Store) recordDarkRestoreServingHealthV1(ctx context.Context, token, client string, agentKey []byte, endpoint mx.ProtocolHealthEndpoint, request mx.ProtocolHealthRequest, receipt mx.ProtocolHealthReceipt, clock func() time.Time) (DarkRestoreServingHealthEvidenceV1, error) {
	var zero DarkRestoreServingHealthEvidenceV1
	if len(agentKey) < 32 || !idRE.MatchString(client) || clock == nil || !endpoint.Valid() || request.EndpointSHA != endpoint.SHA() {
		return zero, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	admission, registered, listener, err := servingHealthSourcesTx(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	present, err := darkRestoreServingHealthSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if present {
		prior, e := readDarkRestoreServingHealthTx(ctx, tx, client)
		if e == nil {
			if prior.Request != request || prior.Receipt != receipt {
				return zero, ErrConflict
			}
			pins, e := servingHealthPins(prior)
			if e != nil {
				return zero, e
			}
			if _, e = ValidateDarkRestoreServingHealthEvidenceV1(prior, pins, agentKey, clock().UnixNano()); e != nil {
				return zero, e
			}
			return prior, tx.Commit()
		}
		if !errors.Is(e, ErrNotFound) {
			return zero, e
		}
	}
	now := clock().UnixNano()
	admissionSHA, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-ADMISSION-EVIDENCE-v1", admission)
	if err != nil {
		return zero, err
	}
	out := DarkRestoreServingHealthEvidenceV1{
		Scope: DarkRestoreServingHealthEvidenceScopeV1, ClientID: client,
		TransitionSHA: admission.TransitionSHA, PlanSHA: admission.PlanSHA,
		ConfigSHA: registered.Plan.ConfigSHA, EpochID: admission.EpochID,
		AdmissionEvidenceSHA:    admissionSHA,
		ListenerApplyReceiptSHA: admission.ListenerApplyReceiptSHA,
		ListenerAppliedUnixNano: listener.AppliedUnixNano,
		EndpointSHA:             request.EndpointSHA, Request: request, Receipt: receipt,
		RecordedUnixNano: now,
	}
	if !servingHealthMatchesSources(out, admission, registered, listener) {
		return zero, ErrDarkRestoreCutover
	}
	pins, err := servingHealthPins(out)
	if err != nil {
		return zero, err
	}
	if _, err = ValidateDarkRestoreServingHealthEvidenceV1(out, pins, agentKey, now); err != nil {
		return zero, err
	}
	if !present {
		for _, ddl := range darkRestoreServingHealthDDL {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return zero, err
			}
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_serving_health_v1(client_id,plan_sha,request_sha,receipt_sha,evidence_json,recorded_unix_nano) VALUES(?,?,?,?,?,?)", client, out.PlanSHA, pins.RequestSHA, pins.ReceiptSHA, raw, now); err != nil {
		return zero, err
	}
	return out, tx.Commit()
}

// Review uses independent exact pins and the paired Agent key. A valid result
// remains read-only evidence and cannot open an issuer or activation path.
func (s *Store) ReviewDarkRestoreServingHealthV1(ctx context.Context, token, client string, agentKey []byte, pins DarkRestoreServingHealthPinsV1) (DarkRestoreServingHealthValidationV1, error) {
	var zero DarkRestoreServingHealthValidationV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	e, err := readDarkRestoreServingHealthTx(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	out, err := ValidateDarkRestoreServingHealthEvidenceV1(e, pins, agentKey, time.Now().UnixNano())
	if err != nil {
		return zero, err
	}
	return out, tx.Commit()
}

func validateDarkRestoreServingHealth(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreServingHealthSchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT client_id FROM darkrestore_serving_health_v1 ORDER BY client_id")
	if err != nil {
		return ErrUnavailable
	}
	var clients []string
	for rows.Next() {
		var client string
		if err = rows.Scan(&client); err != nil {
			rows.Close()
			return ErrUnavailable
		}
		clients = append(clients, client)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return ErrUnavailable
	}
	rows.Close()
	for _, client := range clients {
		if _, err = readDarkRestoreServingHealthTx(ctx, tx, client); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}
