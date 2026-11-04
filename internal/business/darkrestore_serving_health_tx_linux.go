//go:build linux

package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// recordDarkRestoreServingHealthTx is the transaction-scoped finalizer used by
// the node-authenticated persisted round. It changes only immutable historical
// evidence. The caller must bind the node, installation, request and endpoint
// before calling it.
func recordDarkRestoreServingHealthTx(ctx context.Context, tx *sql.Tx, client string, agentKey []byte, endpoint mx.ProtocolHealthEndpoint, request mx.ProtocolHealthRequest, receipt mx.ProtocolHealthReceipt, now int64) (DarkRestoreServingHealthEvidenceV1, error) {
	var zero DarkRestoreServingHealthEvidenceV1
	if len(agentKey) < 32 || !idRE.MatchString(client) || !endpoint.Valid() || request.EndpointSHA != endpoint.SHA() {
		return zero, ErrInvalid
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
		prior, readErr := readDarkRestoreServingHealthTx(ctx, tx, client)
		if readErr == nil {
			if prior.Request != request || prior.Receipt != receipt {
				return zero, ErrConflict
			}
			pins, pinErr := servingHealthPins(prior)
			if pinErr != nil {
				return zero, pinErr
			}
			if _, pinErr = ValidateDarkRestoreServingHealthEvidenceV1(prior, pins, agentKey, now); pinErr != nil {
				return zero, pinErr
			}
			return prior, nil
		}
		if !errors.Is(readErr, ErrNotFound) {
			return zero, readErr
		}
	}
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
	return out, nil
}
