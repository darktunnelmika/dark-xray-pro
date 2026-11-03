//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func validateDarkRestoreBirths(ctx context.Context, tx *sql.Tx) error {
	present, err := birthSchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT plan_sha,client_id,epoch_id,receipt_json FROM darkrestore_native_birth_v1")
	if err != nil {
		return err
	}
	type record struct {
		sha, id, epoch string
		raw            []byte
	}
	var records []record
	for rows.Next() {
		var r record
		if err = rows.Scan(&r.sha, &r.id, &r.epoch, &r.raw); err != nil {
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
		var v DarkRestoreRegisteredBirth
		if len(r.raw) > 65536 || json.Unmarshal(r.raw, &v) != nil {
			return ErrUnavailable
		}
		sha, err := v.Plan.Digest()
		expected, _ := uc.CounterEmail(r.id)
		if err != nil || sha != r.sha || v.Plan.ClientID != r.id || v.Plan.ConfigSHA != v.Birth.Lease.ConfigSHA || v.Plan.ManifestSHA != v.Birth.Lease.ManifestSHA || v.Plan.NodeID != v.Birth.Lease.NodeID || v.Plan.InstallationID != v.Birth.Lease.InstallationID || !v.Birth.Verify(ed25519.PublicKey(v.Signer), sha, v.Birth.Lease, v.Birth.ObservedUnixNano) || v.Epoch.ID != r.epoch || v.Epoch.NodeID != v.Plan.NodeID || v.Epoch.InstallationID != v.Plan.InstallationID || v.Epoch.ConfigSHA != v.Plan.ConfigSHA || v.Epoch.KernelBootID != v.Birth.Process.BootID || v.Epoch.PID != v.Birth.Process.PID || v.Epoch.ProcessStartTicks != v.Birth.Process.StartTicks || len(v.Epoch.Bindings) != 1 || v.Epoch.Bindings[0] != (uc.Binding{ClientID: r.id, Email: expected, Billable: true}) {
			return ErrUnavailable
		}
		var stored []byte
		if err = tx.QueryRowContext(ctx, "SELECT epoch_json FROM pro_usage_epochs WHERE epoch_id=?", r.epoch).Scan(&stored); err != nil {
			return ErrUnavailable
		}
		canonical, _, err := uc.CanonicalEpoch(v.Epoch)
		raw, _ := json.Marshal(canonical)
		if err != nil || !bytes.Equal(stored, raw) {
			return ErrUnavailable
		}
	}
	return nil
}
