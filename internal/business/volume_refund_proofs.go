package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Every process which could bill this identity must have a genuine final
// receipt. In particular, a zero-sequence registered birth still owns a gate;
// a lease timeout, archived client, or reconciliation flag is not a final.
func volumeRefundFinalProofs(ctx context.Context, tx *sql.Tx, key []byte, clientID string) ([]VolumeExpiryRefundProof, string, error) {
	proofs := []VolumeExpiryRefundProof{}
	type nativeRow struct {
		id, node, install, bindingSHA, state string
		epoch, cursor                        []byte
	}
	rows, err := tx.QueryContext(ctx, `SELECT epoch_id,node_id,installation_id,binding_sha,state,epoch_json,cursor_json
 FROM pro_usage_epochs e WHERE EXISTS(SELECT 1 FROM json_each(e.epoch_json,'$.bindings') b
 WHERE json_extract(b.value,'$.client_id')=? AND json_extract(b.value,'$.billable')=1) ORDER BY epoch_id`, clientID)
	if err != nil {
		return nil, "", err
	}
	native := []nativeRow{}
	for rows.Next() {
		var r nativeRow
		if err = rows.Scan(&r.id, &r.node, &r.install, &r.bindingSHA, &r.state, &r.epoch, &r.cursor); err != nil {
			rows.Close()
			return nil, "", err
		}
		native = append(native, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	for _, r := range native {
		var epoch uc.Epoch
		if json.Unmarshal(r.epoch, &epoch) != nil {
			return nil, "", ErrUnavailable
		}
		canonical, hash, e := uc.CanonicalEpoch(epoch)
		if e != nil || hash != r.bindingSHA || canonical.ID != r.id || canonical.NodeID != r.node || canonical.InstallationID != r.install {
			return nil, "", ErrUnavailable
		}
		if r.state != "sealed" {
			return nil, "native_epoch_unsettled", nil
		}
		var cursor uc.Cursor
		if json.Unmarshal(r.cursor, &cursor) != nil {
			return nil, "", ErrUnavailable
		}
		if e = meteredSeal(ctx, tx, r.node, r.install, uc.Receipt{EpochID: r.id, Sequence: cursor.Sequence, SHA: cursor.ReportSHA}); e != nil {
			if errors.Is(e, ErrConflict) {
				return nil, "native_final_receipt_missing", nil
			}
			return nil, "", e
		}
		proofs = append(proofs, VolumeExpiryRefundProof{Kind: "native", EpochID: r.id, NodeID: r.node, InstallationID: r.install, FinalSequence: cursor.Sequence, FinalSHA: cursor.ReportSHA})
	}
	managed, blocker, err := volumeRefundManagedProofs(ctx, tx, key, clientID)
	if err != nil || blocker != "" {
		return nil, blocker, err
	}
	return append(proofs, managed...), "", nil
}

func volumeRefundManagedPresent(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name='inbound_execution_epochs'`).Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM inbound_execution_meta WHERE id=1`).Scan(&version); err != nil || version != 1 {
		return false, ErrUnavailable
	}
	return true, nil
}

func volumeRefundManagedProofs(ctx context.Context, tx *sql.Tx, key []byte, clientID string) ([]VolumeExpiryRefundProof, string, error) {
	present, err := volumeRefundManagedPresent(ctx, tx)
	if err != nil || !present {
		return nil, "", err
	}
	type epochRow struct {
		id, node, install, identitySHA, process, state string
		generation, sequence                           uint64
		epochEnc, cursorEnc                            []byte
	}
	rows, err := tx.QueryContext(ctx, `SELECT epoch_id,node_id,installation_id,generation,identity_sha,process_identity,state,sequence,payload_enc,cursor_enc
 FROM inbound_execution_epochs ORDER BY epoch_id`)
	if err != nil {
		return nil, "", err
	}
	epochs := []epochRow{}
	for rows.Next() {
		var e epochRow
		if err = rows.Scan(&e.id, &e.node, &e.install, &e.generation, &e.identitySHA, &e.process, &e.state, &e.sequence, &e.epochEnc, &e.cursorEnc); err != nil {
			rows.Close()
			return nil, "", err
		}
		epochs = append(epochs, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	proofs := []VolumeExpiryRefundProof{}
	for _, e := range epochs {
		b, err := readExecutionBundle(ctx, tx, key, e.node, e.install, e.generation)
		if err != nil {
			return nil, "", err
		}
		contains := false
		for _, p := range b.Principals {
			contains = contains || p.Billable && p.ClientID == clientID
		}
		if !contains {
			continue
		}
		var epoch ix.EpochInput
		if openPlanValue(key, executionAAD("epoch", e.id, e.generation), e.epochEnc, &epoch) != nil || ix.Digest(epoch) != e.identitySHA || epoch.EpochID != e.id || epoch.NodeID != e.node || epoch.InstallationID != e.install || epoch.Generation != e.generation || epoch.ProcessIdentity != e.process || ix.ValidateEpochIdentity(b, epoch) != nil {
			return nil, "", ErrUnavailable
		}
		var ownerLoss bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbound_execution_reconciliations WHERE epoch_id=?)`, e.id).Scan(&ownerLoss); err != nil {
			return nil, "", err
		}
		// Owner-loss reconciliation deliberately seals administrative state
		// while retaining an unmeasured tail. It can never authorize a refund.
		if ownerLoss {
			return nil, "managed_unknown_tail", nil
		}
		if e.state != "sealed" || e.sequence == 0 {
			return nil, "managed_epoch_unsettled", nil
		}
		var hash string
		var final bool
		var reportEnc []byte
		err = tx.QueryRowContext(ctx, `SELECT sha256,final,report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=?`, e.id, e.sequence).Scan(&hash, &final, &reportEnc)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !final {
			return nil, "managed_final_receipt_missing", nil
		}
		if err != nil {
			return nil, "", err
		}
		var report ix.UsageReport
		var cursor []ix.Counter
		if openPlanValue(key, executionAAD("usage-report", e.id, e.sequence), reportEnc, &report) != nil || ix.Digest(report) != hash ||
			!report.Final || report.EpochID != e.id || report.NodeID != e.node || report.InstallationID != e.install || report.Generation != e.generation || report.Sequence != e.sequence || report.ProcessIdentity != e.process ||
			!routeSHA.MatchString(report.FinalNonce) || report.FinalProofSHA != executionFinalSHA(report) ||
			openPlanValue(key, executionAAD("usage-cursor", e.id, e.sequence), e.cursorEnc, &cursor) != nil || ix.Digest(cursor) != ix.Digest(report.Counters) || len(cursor) != len(b.Principals) {
			return nil, "", ErrUnavailable
		}
		for i, p := range b.Principals {
			if cursor[i].PrincipalID != p.ID {
				return nil, "", ErrUnavailable
			}
		}
		proofs = append(proofs, VolumeExpiryRefundProof{Kind: "managed", EpochID: e.id, NodeID: e.node, InstallationID: e.install, FinalSequence: e.sequence, FinalSHA: hash})
	}
	return proofs, "", nil
}

// A never-issued identity is a separate positive proof, not a missing report
// interpreted as zero. The immutable trusted creation seed must match exactly;
// any potentially issued runtime authority makes this path unavailable.
func volumeRefundNeverIssued(ctx context.Context, tx *sql.Tx, key []byte, c Client, used int64) (VolumeExpiryRefundProof, string, error) {
	none := VolumeExpiryRefundProof{}
	if used != 0 {
		return none, "final_epoch_missing", nil
	}
	sum := sha256.Sum256([]byte("new-native-usage:" + c.ID + ":" + c.UUID))
	expected := usageledger.Seed{ClientID: c.ID, EventID: "new-usage-" + hex.EncodeToString(sum[:]), Complete: true, Reason: "New PRO identity, before first activation"}
	var raw []byte
	var created int64
	err := tx.QueryRowContext(ctx, `SELECT body_json,created_unix FROM pro_usage_seeds WHERE client_id=? AND event_id=?`, c.ID, expected.EventID).Scan(&raw, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return none, "fresh_identity_proof_missing", nil
	}
	if err != nil {
		return none, "", err
	}
	var seed usageledger.Seed
	if json.Unmarshal(raw, &seed) != nil || seed != expected || created < c.CreatedUnix {
		return none, "fresh_identity_proof_missing", nil
	}
	// Native delivery stores the latest issuance per installation without a
	// historical customer manifest. If anything was issued since creation,
	// require genuine epoch settlement unless prospective immutable tracking
	// proves this new identity was never potentially exposed to native use.
	unexposed, err := volumeRefundTrackedUnexposed(ctx, tx, c)
	if err != nil {
		return none, "", err
	}
	var issued bool
	if !unexposed {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM native_runtime_delivery WHERE issued>=?)
 OR EXISTS(SELECT 1 FROM native_route_commands WHERE created>=?)
 OR EXISTS(SELECT 1 FROM native_metered_route_commands WHERE created>=?)`, created*int64(time.Second), created*int64(time.Second), created*int64(time.Second)).Scan(&issued); err != nil {
			return none, "", err
		}
	}
	if issued {
		return none, "runtime_issuance_unsettled", nil
	}
	present, err := volumeRefundManagedPresent(ctx, tx)
	if err != nil {
		return none, "", err
	}
	if present {
		// Immutable bundles preserve historical membership, including a bundle
		// that was delivered but whose closed birth was not acknowledged yet.
		type bundleID struct {
			node, install string
			generation    uint64
		}
		rows, err := tx.QueryContext(ctx, `SELECT node_id,installation_id,generation FROM inbound_execution_bundles ORDER BY node_id,generation`)
		if err != nil {
			return none, "", err
		}
		ids := []bundleID{}
		for rows.Next() {
			var id bundleID
			if err = rows.Scan(&id.node, &id.install, &id.generation); err != nil {
				rows.Close()
				return none, "", err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return none, "", err
		}
		for _, id := range ids {
			b, err := readExecutionBundle(ctx, tx, key, id.node, id.install, id.generation)
			if err != nil {
				return none, "", err
			}
			for _, p := range b.Principals {
				if p.Billable && p.ClientID == c.ID {
					return none, "runtime_issuance_unsettled", nil
				}
			}
		}
	}
	seedSHA := sha256.Sum256(raw)
	return VolumeExpiryRefundProof{Kind: "never_issued", FinalSHA: hex.EncodeToString(seedSHA[:])}, "", nil
}
