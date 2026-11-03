//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
	"time"
)

type DarkRestoreRegisteredBirth struct {
	Plan   DarkRestoreQuarantinePlan
	Birth  DarkRestoreQuarantineBirth
	Signer []byte
	Epoch  uc.Epoch
}

// Registering the real API-only process and its billable mapping is atomic.
// This method does not change eligibility, activate listeners or debit a wallet.
func (s *Store) RegisterDarkRestoreBirth(ctx context.Context, key []byte, token, client, planSHA string, pub ed25519.PublicKey, lease mx.Lease, birth DarkRestoreQuarantineBirth) (DarkRestoreRegisteredBirth, error) {
	var zero DarkRestoreRegisteredBirth
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}

	presentBefore, err := birthSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if presentBefore {
		var priorRaw []byte
		e := tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_birth_v1 WHERE plan_sha=?", planSHA).Scan(&priorRaw)
		if e == nil {
			var prior DarkRestoreRegisteredBirth
			if json.Unmarshal(priorRaw, &prior) != nil || prior.Plan.ClientID != client || prior.Birth.Lease != lease || !bytes.Equal(prior.Signer, pub) {
				return zero, ErrConflict
			}
			expected, _ := json.Marshal(birth)
			stored, _ := json.Marshal(prior.Birth)
			sha, e := prior.Plan.Digest()
			if e != nil || sha != planSHA || !bytes.Equal(expected, stored) {
				return zero, ErrConflict
			}
			if err = s.usage.RegisterEpochTx(ctx, tx, prior.Epoch); err != nil {
				return zero, err
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
	if err != nil || sha != planSHA || birth.PlanSHA != sha || birth.Lease != lease || lease.NodeID != plan.NodeID || lease.InstallationID != plan.InstallationID || lease.ConfigSHA != plan.ConfigSHA || lease.ManifestSHA != plan.ManifestSHA {
		return zero, ErrDarkRestoreCutover
	}
	present, err := birthSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if !present {
		for _, ddl := range darkRestoreBirthDDL {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return zero, err
			}
		}
	}
	email, err := uc.CounterEmail(client)
	if err != nil {
		return zero, err
	}
	hash, err := cutoverSnapshotDigest("DARK-PRO-REGISTERED-API-BIRTH-v1", birth)
	if err != nil {
		return zero, err
	}
	epoch := uc.Epoch{ID: hash[:32], NodeID: plan.NodeID, InstallationID: plan.InstallationID, ConfigSHA: plan.ConfigSHA, KernelBootID: birth.Process.BootID, PID: birth.Process.PID, ProcessStartTicks: birth.Process.StartTicks, Bindings: []uc.Binding{{ClientID: client, Email: email, Billable: true}}}
	if _, _, err = uc.CanonicalEpoch(epoch); err != nil {
		return zero, err
	}
	out := DarkRestoreRegisteredBirth{Plan: plan, Birth: birth, Signer: append([]byte(nil), pub...), Epoch: epoch}
	// Registration carries the public immutable plan contract consistently on
	// first acceptance and replay.
	out.Plan.candidate = xrayconfig.Candidate{}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_birth_v1 WHERE plan_sha=?", sha).Scan(&prior)
	if err == nil {
		if !bytes.Equal(raw, prior) {
			return zero, ErrConflict
		}
		if err = s.usage.RegisterEpochTx(ctx, tx, epoch); err != nil {
			return zero, err
		}
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	if !birth.Verify(pub, sha, lease, time.Now().UnixNano()) {
		return zero, ErrDarkRestoreCutover
	}
	if err = s.usage.RegisterEpochTx(ctx, tx, epoch); err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_native_birth_v1(plan_sha,client_id,epoch_id,receipt_json) VALUES(?,?,?,?)", sha, client, epoch.ID, raw); err != nil {
		return zero, err
	}
	return out, tx.Commit()
}
