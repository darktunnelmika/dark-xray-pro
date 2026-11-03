//go:build linux

package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"time"
)

func (s *Store) DarkRestoreAdmissionEpoch(ctx context.Context, key []byte, token, client, planSHA string, renew bool) (uc.Epoch, error) {
	return s.darkRestoreAdmissionEpoch(ctx, key, token, "", "", client, planSHA, renew)
}
func (s *Store) DarkRestoreAdmissionEpochForNode(ctx context.Context, key []byte, node, installation, client, planSHA string, renew bool) (uc.Epoch, error) {
	if !idRE.MatchString(node) || !routeID.MatchString(installation) {
		return uc.Epoch{}, ErrOwnershipDenied
	}
	return s.darkRestoreAdmissionEpoch(ctx, key, "", node, installation, client, planSHA, renew)
}
func (s *Store) darkRestoreAdmissionEpoch(ctx context.Context, key []byte, token, node, installation, client, planSHA string, renew bool) (uc.Epoch, error) {
	var zero uc.Epoch
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor := ""
	if token != "" {
		actor, err = ownershipOwner(ctx, tx, token)
		if err != nil {
			return zero, err
		}
	} else if node == "" || installation == "" {
		return zero, ErrOwnershipDenied
	}
	present, err := admissionSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if !present {
		return zero, ErrNotFound
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_admission_v1 WHERE plan_sha=?", planSHA).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, ErrNotFound
		}
		return zero, err
	}
	var permit DarkRestoreAdmission
	if json.Unmarshal(raw, &permit) != nil || (actor != "" && permit.ActorID != actor) || (node != "" && (permit.Registered.Epoch.NodeID != node || permit.Registered.Epoch.InstallationID != installation)) || permit.Snapshot.ClientID != client {
		return zero, ErrDarkRestoreCutover
	}
	var issuer bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM auth_users a JOIN representatives r ON r.id=a.representative_id WHERE a.id=? AND a.role='owner' AND a.representative_id='owner' AND a.disabled=0 AND r.enabled=1)", permit.ActorID).Scan(&issuer); err != nil || !issuer {
		return zero, ErrOwnershipDenied
	}
	sha, err := permit.Registered.Plan.Digest()
	if err != nil || sha != planSHA {
		return zero, ErrDarkRestoreCutover
	}
	snap, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, permit.Snapshot.SourceSHA, permit.Snapshot.SourceID)
	if err != nil || snap.DeploymentState != "pending_node_apply" || snap.Archived || snap.ExpiresUnix <= time.Now().Unix() || !snap.NativeBaselineComplete || snap.NativeUplink == nil || snap.NativeDownlink == nil || snap.NativeHolds == nil || *snap.NativeHolds != 0 {
		return zero, ErrDarkRestoreCutover
	}
	var enabled bool
	if err = tx.QueryRowContext(ctx, "SELECT enabled FROM representatives WHERE id=?", snap.RepresentativeID).Scan(&enabled); err != nil || !enabled {
		return zero, ErrDarkRestoreCutover
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil || evidence == nil {
		return zero, ErrDarkRestoreCutover
	}
	if err = verifyCutoverHistoricalBudget(ctx, tx, evidence); err != nil {
		return zero, err
	}
	if _, err = cutoverRemaining(snap.CurrentEntitlement, evidence.SettledHistoricalBytes, *snap.NativeUplink, *snap.NativeDownlink); err != nil {
		return zero, err
	}
	// Compare the original identity and entitlement while preserving separately
	// validated monotonic native counters.
	snap.DeploymentState = "disabled"
	snap.NativeUplink = permit.Snapshot.NativeUplink
	snap.NativeDownlink = permit.Snapshot.NativeDownlink
	snap.NativeHolds = permit.Snapshot.NativeHolds
	snap, err = hashDarkRestoreCutoverSnapshotV1(snap)
	if err != nil || snap.StateSHA != permit.Snapshot.StateSHA {
		return zero, ErrDarkRestoreCutover
	}
	prepared, err := readCutoverReceipt(ctx, tx, client)
	if err != nil || verifyPreparedPlanReceipt(ctx, tx, prepared) != nil {
		return zero, ErrDarkRestoreCutover
	}
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM pro_usage_epochs WHERE epoch_id=?", permit.Registered.Epoch.ID).Scan(&state); err != nil || state != "open" {
		return zero, ErrDarkRestoreCutover
	}
	if renew {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_receipts WHERE epoch_id=?", permit.Registered.Epoch.ID).Scan(&n); err != nil || n == 0 {
			return zero, ErrDarkRestoreCutover
		}
	} else if time.Now().UnixNano() >= permit.Registered.Birth.Lease.Expires {
		return zero, ErrDarkRestoreCutover
	}
	return permit.Registered.Epoch, nil
}

// DarkRestoreRegisteredEpochForNode returns only the immutable accounting
// identity for replay after restart. The caller authenticates the paired node.
func (s *Store) DarkRestoreRegisteredEpochForNode(ctx context.Context, node, installation, planSHA string) (uc.Epoch, error) {
	var zero uc.Epoch
	if !routeSHA.MatchString(planSHA) {
		return zero, ErrInvalid
	}
	present, err := birthSchema(ctx, s.db)
	if err != nil {
		return zero, err
	}
	if !present {
		return zero, ErrNotFound
	}
	var raw []byte
	if err = s.db.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_birth_v1 WHERE plan_sha=?", planSHA).Scan(&raw); err != nil {
		return zero, err
	}
	var registered DarkRestoreRegisteredBirth
	if json.Unmarshal(raw, &registered) != nil {
		return zero, ErrUnavailable
	}
	sha, err := registered.Plan.Digest()
	if err != nil || sha != planSHA || registered.Epoch.NodeID != node || registered.Epoch.InstallationID != installation || registered.Plan.NodeID != node || registered.Plan.InstallationID != installation {
		return zero, ErrOwnershipDenied
	}
	if _, _, err = uc.CanonicalEpoch(registered.Epoch); err != nil {
		return zero, ErrUnavailable
	}
	return registered.Epoch, nil
}
