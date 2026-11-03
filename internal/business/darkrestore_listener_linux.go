//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// An Agent attestation of one local listener installation, after the durable
// zero receipt. It is historical evidence, never serving health or authority.
type DarkRestoreListenerApply struct {
	Version          int
	PlanSHA          string
	EpochID          string
	CoreSHA          string
	InitialReportSHA string
	Process          uc.ProcessToken
	AppliedUnixNano  int64
	Signature        []byte
}

func (a DarkRestoreListenerApply) Digest() string {
	raw, _ := json.Marshal(a)
	return admissionBytesSHA(raw)
}

func (a DarkRestoreListenerApply) signingBytes() []byte {
	a.Signature = nil
	raw, _ := json.Marshal(a)
	return append([]byte("DARK-PRO-LISTENER-APPLY-v1\x00"), raw...)
}

func (a DarkRestoreListenerApply) verify(r DarkRestoreRegisteredBirth) bool {
	sha, err := r.Plan.Digest()
	initial, e := initialAdmissionReportSHA(r.Epoch)
	return err == nil && e == nil && a.Version == 1 && a.PlanSHA == sha && a.EpochID == r.Epoch.ID &&
		routeSHA.MatchString(a.CoreSHA) && a.CoreSHA == r.Birth.CoreSHA &&
		a.InitialReportSHA == initial && a.Process == r.Birth.Process &&
		r.Birth.Verify(ed25519.PublicKey(r.Signer), sha, r.Birth.Lease, r.Birth.ObservedUnixNano) &&
		r.Epoch.NodeID == r.Plan.NodeID && r.Epoch.InstallationID == r.Plan.InstallationID &&
		r.Epoch.ConfigSHA == r.Plan.ConfigSHA && r.Epoch.KernelBootID == a.Process.BootID &&
		r.Epoch.PID == a.Process.PID && r.Epoch.ProcessStartTicks == a.Process.StartTicks &&
		a.AppliedUnixNano > r.Birth.ObservedUnixNano && a.AppliedUnixNano < r.Birth.Lease.Expires &&
		len(r.Signer) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(r.Signer), a.signingBytes(), a.Signature)
}

// Called only after ActivateUsageAdmission has installed and observed the real
// listeners. VerifyOwnedStatsEndpoint ties the observation to the birth process.
func (r DarkRestoreRegisteredBirth) ObserveListenerApply(ctx context.Context, runtime *mx.Runtime, signer ed25519.PrivateKey) (DarkRestoreListenerApply, error) {
	var zero DarkRestoreListenerApply
	if runtime == nil || len(signer) != ed25519.PrivateKeySize || !bytes.Equal(signer.Public().(ed25519.PublicKey), r.Signer) {
		return zero, ErrDarkRestoreCutover
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	state := runtime.Snapshot()
	if state.Phase != "ready_observed_admission" || !state.ProcessReady || state.NoListeners ||
		state.ActiveSHA != r.Plan.ConfigSHA || state.ActiveSequence != r.Birth.Lease.Sequence ||
		state.LeaseExpires != r.Birth.Lease.Expires || time.Now().UnixNano() >= state.LeaseExpires ||
		state.Usage == nil || !state.Usage.Running || state.Usage.Receipts < 1 || state.Usage.Failure != "" ||
		state.DataPlaneVerified || state.SubscriptionPublished || runtime.BinarySHA256() != r.Birth.CoreSHA {
		return zero, ErrDarkRestoreCutover
	}
	if err := uc.VerifyOwnedStatsEndpoint(r.Epoch, mx.MeteredStatsEndpoint); err != nil {
		return zero, err
	}
	initial, err := initialAdmissionReportSHA(r.Epoch)
	if err != nil {
		return zero, err
	}
	a := DarkRestoreListenerApply{Version: 1, PlanSHA: r.Birth.PlanSHA, EpochID: r.Epoch.ID, CoreSHA: runtime.BinarySHA256(), InitialReportSHA: initial, Process: r.Birth.Process, AppliedUnixNano: time.Now().UnixNano()}
	a.Signature = ed25519.Sign(signer, a.signingBytes())
	if !a.verify(r) {
		return zero, ErrDarkRestoreCutover
	}
	return a, nil
}

// The same validation is used for live registration, reopen and offline backup.
func verifyListenerApplyTx(ctx context.Context, q sellerQueryV2, a DarkRestoreListenerApply) error {
	var birthRaw, admissionRaw []byte
	if err := q.QueryRowContext(ctx, "SELECT b.receipt_json,a.receipt_json FROM darkrestore_native_birth_v1 b JOIN darkrestore_native_admission_v1 a ON a.plan_sha=b.plan_sha AND a.epoch_id=b.epoch_id WHERE b.plan_sha=? AND b.epoch_id=?", a.PlanSHA, a.EpochID).Scan(&birthRaw, &admissionRaw); err != nil {
		return ErrDarkRestoreCutover
	}
	var r DarkRestoreRegisteredBirth
	var grant DarkRestoreAdmission
	if len(birthRaw) > 131072 || len(admissionRaw) > 131072 || json.Unmarshal(birthRaw, &r) != nil || json.Unmarshal(admissionRaw, &grant) != nil || !a.verify(r) {
		return ErrDarkRestoreCutover
	}
	expected, _ := json.Marshal(grant.Registered)
	canonical, _ := json.Marshal(r)
	if !bytes.Equal(expected, birthRaw) || !bytes.Equal(canonical, birthRaw) {
		return ErrDarkRestoreCutover
	}
	var initial string
	var quiesced int
	if err := q.QueryRowContext(ctx, "SELECT sha256,quiesced FROM pro_usage_receipts WHERE epoch_id=? AND sequence='1'", a.EpochID).Scan(&initial, &quiesced); err != nil || initial != a.InitialReportSHA || quiesced != 0 {
		return ErrDarkRestoreCutover
	}
	var n, up, down int64
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(uplink),0),COALESCE(SUM(downlink),0) FROM pro_usage_deltas WHERE epoch_id=? AND sequence='1' AND client_id=?", a.EpochID, r.Plan.ClientID).Scan(&n, &up, &down); err != nil || n != 1 || up != 0 || down != 0 {
		return ErrDarkRestoreCutover
	}
	return verifyDarkRestoreInitialProbeDelta(ctx, q, r)
}

// No client, wallet, lease or publication state is changed by this receipt.
func (s *Store) RegisterDarkRestoreListenerApply(ctx context.Context, node, installation, planSHA string, a DarkRestoreListenerApply) (string, error) {
	if !routeSHA.MatchString(planSHA) || a.PlanSHA != planSHA || a.AppliedUnixNano > time.Now().UnixNano() {
		return "", ErrDarkRestoreCutover
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var ownerNode, ownerInstall string
	if err = tx.QueryRowContext(ctx, "SELECT node_id,installation_id FROM pro_usage_epochs WHERE epoch_id=?", a.EpochID).Scan(&ownerNode, &ownerInstall); err != nil || ownerNode != node || ownerInstall != installation {
		return "", ErrOwnershipDenied
	}
	present, err := listenerApplySchema(ctx, tx)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	if present {
		var prior []byte
		e := tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_listener_apply_v1 WHERE plan_sha=?", planSHA).Scan(&prior)
		if e == nil {
			if !bytes.Equal(raw, prior) {
				return "", ErrConflict
			}
			if err = verifyListenerApplyTx(ctx, tx, a); err != nil {
				return "", err
			}
			return admissionBytesSHA(prior), tx.Commit()
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return "", e
		}
	}
	if err = verifyListenerApplyTx(ctx, tx, a); err != nil {
		return "", err
	}
	if !present {
		for _, ddl := range darkRestoreListenerDDL {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return "", err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_listener_apply_v1(plan_sha,epoch_id,receipt_json) VALUES(?,?,?)", a.PlanSHA, a.EpochID, raw); err != nil {
		return "", err
	}
	return admissionBytesSHA(raw), tx.Commit()
}

func validateDarkRestoreListeners(ctx context.Context, tx *sql.Tx) error {
	present, err := listenerApplySchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT plan_sha,epoch_id,receipt_json FROM darkrestore_listener_apply_v1")
	if err != nil {
		return err
	}
	type record struct {
		plan, epoch string
		raw         []byte
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.plan, &r.epoch, &r.raw); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range records {
		var a DarkRestoreListenerApply
		if len(r.raw) > 16384 || json.Unmarshal(r.raw, &a) != nil || a.PlanSHA != r.plan || a.EpochID != r.epoch {
			return ErrUnavailable
		}
		canonical, _ := json.Marshal(a)
		if !bytes.Equal(canonical, r.raw) || verifyListenerApplyTx(ctx, tx, a) != nil {
			return ErrUnavailable
		}
	}
	return nil
}
