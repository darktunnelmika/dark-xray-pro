//go:build linux

package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
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
