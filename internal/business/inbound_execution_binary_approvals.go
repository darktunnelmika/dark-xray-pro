package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type executionBinaryApproval struct {
	NodeID         string            `json:"node_id"`
	InstallationID string            `json:"installation_id"`
	Generation     uint64            `json:"generation"`
	ConfigSHA      string            `json:"config_sha"`
	ManifestSHA    string            `json:"manifest_sha"`
	Pins           map[string]string `json:"pins"`
}

func executionPinsMatchBundle(b ix.Bundle, pins map[string]string) bool {
	engines := map[string]bool{}
	if len(b.NativeConfig) > 0 {
		engines["xray"] = true
	}
	for _, c := range b.Components {
		engines[c.Engine] = true
	}
	if len(pins) != len(engines) {
		return false
	}
	for engine := range engines {
		if !routeSHA.MatchString(pins[engine]) {
			return false
		}
	}
	return true
}

// RecordInboundExecutionBinaryApproval persists the approved engines of a
// concrete lease before the lease is delivered. The authenticated controller
// checks current release pins first. This audit gives a lost closed birth its
// historical provenance after an upgrade, without permitting old admission.
func (s *Store) RecordInboundExecutionBinaryApproval(ctx context.Context, key []byte, node, install string, generation uint64, pins map[string]string) error {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return err
	}
	if !executionPinsMatchBundle(b, pins) {
		return ErrInvalid
	}
	if err = executionBundleAuthority(ctx, tx, key, b); err != nil {
		return err
	}
	pinsSHA := ix.Digest(pins)
	want := executionBinaryApproval{NodeID: node, InstallationID: install, Generation: generation, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, Pins: pins}
	aad := executionAAD("binary-approval", node+"/"+install+"/"+pinsSHA, generation)
	var enc []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_enc FROM inbound_execution_binary_approvals WHERE node_id=? AND installation_id=? AND generation=? AND pins_sha=?`, node, install, generation, pinsSHA).Scan(&enc)
	if err == nil {
		var old executionBinaryApproval
		if openPlanValue(key, aad, enc, &old) != nil || ix.Digest(old) != ix.Digest(want) {
			return ErrUnavailable
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	enc, err = sealPlanValue(key, aad, want)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_binary_approvals(node_id,installation_id,generation,pins_sha,config_sha,manifest_sha,payload_enc,created) VALUES(?,?,?,?,?,?,?,?)`, node, install, generation, pinsSHA, b.ConfigSHA, b.ManifestSHA, enc, time.Now().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

// LookupInboundExecutionBinaryApproval proves only that these exact engines
// were approved for this historical bundle. It must not replace the current
// pin or entitlement checks for Register or a renewed execution lease.
func (s *Store) LookupInboundExecutionBinaryApproval(ctx context.Context, key []byte, in ix.EpochInput) (bool, error) {
	if !validExecutionEpochInput(in) {
		return false, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	b, err := readExecutionBundle(ctx, tx, key, in.NodeID, in.InstallationID, in.Generation)
	if err != nil {
		return false, err
	}
	if ix.ValidateEpochIdentity(b, in) != nil || !executionPinsMatchBundle(b, in.BinarySHAs) {
		return false, ErrInvalid
	}
	pinsSHA := ix.Digest(in.BinarySHAs)
	var enc []byte
	var configSHA, manifestSHA string
	err = tx.QueryRowContext(ctx, `SELECT config_sha,manifest_sha,payload_enc FROM inbound_execution_binary_approvals WHERE node_id=? AND installation_id=? AND generation=? AND pins_sha=?`, in.NodeID, in.InstallationID, in.Generation, pinsSHA).Scan(&configSHA, &manifestSHA, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	want := executionBinaryApproval{NodeID: in.NodeID, InstallationID: in.InstallationID, Generation: in.Generation, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, Pins: in.BinarySHAs}
	var old executionBinaryApproval
	if configSHA != b.ConfigSHA || manifestSHA != b.ManifestSHA || openPlanValue(key, executionAAD("binary-approval", in.NodeID+"/"+in.InstallationID+"/"+pinsSHA, in.Generation), enc, &old) != nil || ix.Digest(old) != ix.Digest(want) {
		return false, ErrUnavailable
	}
	return true, tx.Commit()
}
