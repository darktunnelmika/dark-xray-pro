package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func validExecutionEpochInput(in ix.EpochInput) bool {
	if !idRE.MatchString(in.EpochID) || !idRE.MatchString(in.NodeID) || !routeID.MatchString(in.InstallationID) || in.Generation == 0 || !routeSHA.MatchString(in.ConfigSHA) || !routeSHA.MatchString(in.ManifestSHA) || len(in.ProcessIdentity) == 0 || len(in.ProcessIdentity) > 65536 || len(in.BinarySHAs) == 0 || len(in.BinarySHAs) > 256 {
		return false
	}
	for name, sha := range in.BinarySHAs {
		if len(name) == 0 || len(name) > 256 || !routeSHA.MatchString(sha) {
			return false
		}
	}
	return true
}

func readExecutionEpochReceipt(ctx context.Context, tx *sql.Tx, key []byte, in ix.EpochInput) (ix.EpochInput, string, bool, error) {
	var old ix.EpochInput
	var hash, state, node, install, process string
	var generation uint64
	var enc []byte
	err := tx.QueryRowContext(ctx, `SELECT identity_sha,payload_enc,state,node_id,installation_id,generation,process_identity FROM inbound_execution_epochs WHERE epoch_id=?`, in.EpochID).Scan(&hash, &enc, &state, &node, &install, &generation, &process)
	if errors.Is(err, sql.ErrNoRows) {
		return old, "", false, nil
	}
	if err != nil {
		return old, "", false, err
	}
	if hash != ix.Digest(in) || node != in.NodeID || install != in.InstallationID || generation != in.Generation || process != in.ProcessIdentity {
		return old, state, true, ErrConflict
	}
	if openPlanValue(key, executionAAD("epoch", in.EpochID, generation), enc, &old) != nil || ix.Digest(old) != hash {
		return old, state, true, ErrUnavailable
	}
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return old, state, true, err
	}
	if ix.ValidateEpochIdentity(b, old) != nil {
		return old, state, true, ErrUnavailable
	}
	return old, state, true, nil
}

// LookupInboundExecutionEpochReceipt authenticates an already recorded birth.
// It does not authorize admission or reopen a sealed epoch. Callers may use
// this exact historical identity before applying the current binary-pin rule
// to NEW registrations; RegisterInboundExecutionEpoch still enforces state.
func (s *Store) LookupInboundExecutionEpochReceipt(ctx context.Context, key []byte, in ix.EpochInput) (ix.EpochInput, bool, error) {
	if !validExecutionEpochInput(in) {
		return ix.EpochInput{}, false, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.EpochInput{}, false, err
	}
	defer tx.Rollback()
	old, _, found, err := readExecutionEpochReceipt(ctx, tx, key, in)
	if err != nil {
		return ix.EpochInput{}, found, err
	}
	return old, found, tx.Commit()
}

// insertExecutionEpoch records a closed birth without granting admission. Its
// caller must either check current authority or hold genuine final evidence /
// explicitly record uncertainty in the same transaction. No baseline is made.
func insertExecutionEpoch(ctx context.Context, tx *sql.Tx, key []byte, b ix.Bundle, in ix.EpochInput) error {
	if !validExecutionEpochInput(in) || ix.ValidateEpochIdentity(b, in) != nil {
		return ErrInvalid
	}
	if b.ConfigSHA != in.ConfigSHA || b.ManifestSHA != in.ManifestSHA || len(b.Principals) == 0 {
		return ErrConflict
	}
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_execution_epochs WHERE node_id=? AND state!='sealed'`, in.NodeID).Scan(&open); err != nil {
		return err
	}
	if open != 0 {
		return ErrConflict
	}
	enc, err := sealPlanValue(key, executionAAD("epoch", in.EpochID, in.Generation), in)
	if err != nil {
		return err
	}
	cursor, err := sealPlanValue(key, executionAAD("usage-cursor", in.EpochID, 0), []ix.Counter{})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_epochs(epoch_id,node_id,installation_id,generation,identity_sha,process_identity,payload_enc,state,sequence,cursor_enc,created) VALUES(?,?,?,?,?,?,?,'open',0,?,?)`, in.EpochID, in.NodeID, in.InstallationID, in.Generation, ix.Digest(in), in.ProcessIdentity, enc, cursor, time.Now().UnixNano())
	return err
}
