package business

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func executionBundleAuthority(ctx context.Context, tx *sql.Tx, key []byte, b ix.Bundle) error {
	var generation uint64
	var install string
	if err := tx.QueryRowContext(ctx, `SELECT installation_id,desired_generation FROM inbound_execution_nodes WHERE node_id=?`, b.NodeID).Scan(&install, &generation); err != nil {
		return err
	}
	if install != b.InstallationID || generation != b.Generation {
		return ErrConflict
	}
	if len(b.Listeners) > 0 || len(b.Components) > 0 {
		required, err := executionResumeRequired(ctx, tx, b.NodeID, b.InstallationID)
		if err != nil {
			return err
		}
		if required {
			return ErrConflict
		}
	}
	for _, p := range b.Principals {
		if p.ClientID == "" {
			continue
		}
		var eligible int
		if tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, p.ClientID).Scan(&eligible) != nil || eligible != 1 {
			return ErrConflict
		}
		if stale, err := executionClientHasStaleEpoch(ctx, tx, p.ClientID, time.Now().UnixNano()); err != nil || stale {
			return ErrConflict
		}
	}
	for _, r := range b.Revisions {
		var desired uint64
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT desired_revision,desired_state FROM inbound_execution_heads WHERE inbound_id=?`, r.InboundID).Scan(&desired, &state); err != nil {
			return err
		}
		if desired != r.Revision || state != "active" {
			return ErrConflict
		}
		snapshot, err := readExecutionSnapshot(ctx, tx, key, r.InboundID, r.Revision)
		if err != nil {
			return err
		}
		for _, id := range snapshot.EffectiveClientIDs {
			var eligible int
			if tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, id).Scan(&eligible) != nil || eligible != 1 {
				return ErrConflict
			}
			if stale, err := executionClientHasStaleEpoch(ctx, tx, id, time.Now().UnixNano()); err != nil || stale {
				return ErrConflict
			}
		}
	}
	return nil
}

// InboundExecutionAuthority is rechecked before every renewed execution lease.
// Accounting acceptance deliberately remains available after revocation.
func (s *Store) InboundExecutionAuthority(ctx context.Context, key []byte, node, install string, generation uint64) error {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return err
	}
	if err = executionBundleAuthority(ctx, tx, key, b); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RegisterInboundExecutionEpoch(ctx context.Context, key []byte, in ix.EpochInput) (ix.EpochInput, error) {
	if !validExecutionEpochInput(in) {
		return ix.EpochInput{}, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.EpochInput{}, err
	}
	defer tx.Rollback()
	old, state, found, err := readExecutionEpochReceipt(ctx, tx, key, in)
	if err != nil {
		return ix.EpochInput{}, err
	}
	if found {
		if state != "open" {
			return ix.EpochInput{}, ErrConflict
		}
		return old, tx.Commit()
	}
	b, err := readExecutionBundle(ctx, tx, key, in.NodeID, in.InstallationID, in.Generation)
	if err != nil {
		return ix.EpochInput{}, err
	}
	// An exact existing receipt remains replayable after withdrawal or a
	// binary upgrade. A new birth still requires current execution authority.
	if err = executionBundleAuthority(ctx, tx, key, b); err != nil {
		return ix.EpochInput{}, err
	}
	if err = insertExecutionEpoch(ctx, tx, key, b, in); err != nil {
		return ix.EpochInput{}, err
	}
	return in, tx.Commit()
}

func executionFinalSHA(in ix.UsageReport) string {
	return ix.Digest(struct {
		Process  string       `json:"process"`
		Nonce    string       `json:"nonce"`
		Counters []ix.Counter `json:"counters"`
	}{in.ProcessIdentity, in.FinalNonce, in.Counters})
}

func executionAccountingFreshUntil(ctx context.Context, tx *sql.Tx, node, install string, generation uint64, epoch string, now int64) (int64, error) {
	var received int64
	err := tx.QueryRowContext(ctx, `SELECT latest.created FROM inbound_execution_epochs e JOIN inbound_execution_usage_receipts baseline ON baseline.epoch_id=e.epoch_id AND baseline.sequence=1 JOIN inbound_execution_usage_receipts latest ON latest.epoch_id=e.epoch_id AND latest.sequence=e.sequence WHERE e.epoch_id=? AND e.node_id=? AND e.installation_id=? AND e.generation=? AND e.state='open' AND baseline.final=0 AND latest.final=0`, epoch, node, install, generation).Scan(&received)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, err
	}
	until := received + int64(15*time.Second)
	if received <= 0 || received > now+int64(5*time.Second) || until <= now {
		return 0, ErrConflict
	}
	return until, nil
}

// AcceptInboundExecutionUsage accepts only complete cumulative counter sets
// registered from an authenticated bundle. Receipt, deltas, quota totals and
// cursor commit atomically. Final reports remain admissible after desired
// generation changes, so revoked traffic is never silently discarded.
func (s *Store) AcceptInboundExecutionUsage(ctx context.Context, key []byte, in ix.UsageReport) (ix.UsageReceipt, error) {
	var none ix.UsageReceipt
	if !idRE.MatchString(in.EpochID) || !idRE.MatchString(in.NodeID) || !routeID.MatchString(in.InstallationID) || in.Generation == 0 || in.Sequence == 0 || in.Sequence >= routeMax || len(in.Counters) == 0 || len(in.Counters) > 50000 || len(in.ProcessIdentity) == 0 || len(in.ProcessIdentity) > 65536 {
		return none, ErrInvalid
	}
	if in.Final {
		if !routeSHA.MatchString(in.FinalNonce) || !routeSHA.MatchString(in.FinalProofSHA) || in.FinalProofSHA != executionFinalSHA(in) {
			return none, ErrInvalid
		}
	} else if in.FinalNonce != "" || in.FinalProofSHA != "" {
		return none, ErrInvalid
	}
	for i, c := range in.Counters {
		if c.PrincipalID == "" || c.Upload > math.MaxInt64 || c.Download > math.MaxInt64 || c.Upload > math.MaxInt64-c.Download || i > 0 && in.Counters[i-1].PrincipalID >= c.PrincipalID {
			return none, ErrInvalid
		}
	}
	if e := in.InitialEpoch; e != nil {
		if !validExecutionEpochInput(*e) || !in.Final || in.Sequence != 1 || e.EpochID != in.EpochID || e.NodeID != in.NodeID || e.InstallationID != in.InstallationID || e.Generation != in.Generation || e.ProcessIdentity != in.ProcessIdentity {
			return none, ErrInvalid
		}
		for _, c := range in.Counters {
			if c.Upload != 0 || c.Download != 0 {
				return none, ErrConflict
			}
		}
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()
	var node, install, process, state, identity string
	var generation, sequence uint64
	var epochEnc, cursorEnc []byte
	readEpoch := func() error {
		return tx.QueryRowContext(ctx, `SELECT node_id,installation_id,generation,process_identity,state,sequence,identity_sha,payload_enc,cursor_enc FROM inbound_execution_epochs WHERE epoch_id=?`, in.EpochID).Scan(&node, &install, &generation, &process, &state, &sequence, &identity, &epochEnc, &cursorEnc)
	}
	err = readEpoch()
	if errors.Is(err, sql.ErrNoRows) && in.InitialEpoch != nil {
		// This path accepts a real closed-gate final, never an inferred zero.
		// Current entitlement is intentionally irrelevant to settling its exact
		// historical bytes. The authenticated API checks approved binary pins.
		b, e := readExecutionBundle(ctx, tx, key, in.NodeID, in.InstallationID, in.Generation)
		if e != nil {
			return none, e
		}
		if e = insertExecutionEpoch(ctx, tx, key, b, *in.InitialEpoch); e != nil {
			return none, e
		}
		err = readEpoch()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return none, ErrNotFound
	}
	if err != nil {
		return none, err
	}
	if node != in.NodeID || install != in.InstallationID || generation != in.Generation || process != in.ProcessIdentity {
		return none, ErrConflict
	}
	if in.InitialEpoch != nil && ix.Digest(*in.InitialEpoch) != identity {
		return none, ErrConflict
	}
	var epoch ix.EpochInput
	if openPlanValue(key, executionAAD("epoch", in.EpochID, generation), epochEnc, &epoch) != nil || ix.Digest(epoch) != identity {
		return none, ErrUnavailable
	}
	ownerLoss := false
	if state == "sealed" {
		clearance, found, e := readExecutionClearance(ctx, tx, key, in.EpochID)
		if e != nil {
			return none, e
		}
		if found {
			if clearance.EpochSHA != identity {
				return none, ErrUnavailable
			}
			ownerLoss = true
		}
	}
	hash := ix.Digest(in)
	var old string
	var final bool
	err = tx.QueryRowContext(ctx, `SELECT sha256,final FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=?`, in.EpochID, in.Sequence).Scan(&old, &final)
	if err == nil {
		if old != hash || final != in.Final {
			return none, ErrConflict
		}
		return ix.UsageReceipt{EpochID: in.EpochID, Sequence: in.Sequence, SHA256: hash, Final: in.Final, Replayed: true}, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return none, err
	}
	if (state != "open" && state != "uncertain" && !ownerLoss) || in.Sequence != sequence+1 {
		return none, ErrConflict
	}
	if (state == "uncertain" || ownerLoss) && sequence > 0 {
		var terminal bool
		if err = tx.QueryRowContext(ctx, `SELECT final FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=?`, in.EpochID, sequence).Scan(&terminal); err != nil {
			return none, err
		}
		if terminal {
			// A final receipt is terminal even when an independent unknown-tail
			// hold remains. No later sample may reinterpret that receipt.
			return none, ErrConflict
		}
	}
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return none, err
	}
	if b.ConfigSHA != epoch.ConfigSHA || b.ManifestSHA != epoch.ManifestSHA || len(b.Principals) != len(in.Counters) {
		return none, ErrConflict
	}
	// Receipt acceptance records observed bytes; it does not authorize
	// admission. A registered zero baseline remains admissible after revoke,
	// allowing an ambiguous lost zero response to be replayed and followed by
	// a real final seal. The supervisor rechecks a fresh lease before Activate.
	previous := []ix.Counter{}
	if openPlanValue(key, executionAAD("usage-cursor", in.EpochID, sequence), cursorEnc, &previous) != nil {
		return none, ErrUnavailable
	}
	if sequence > 0 && len(previous) != len(in.Counters) {
		return none, ErrUnavailable
	}
	for i, c := range in.Counters {
		if c.PrincipalID != b.Principals[i].ID {
			return none, ErrConflict
		}
		if sequence == 0 && (c.Upload != 0 || c.Download != 0) {
			return none, ErrConflict
		}
		if sequence > 0 && (previous[i].PrincipalID != c.PrincipalID || c.Upload < previous[i].Upload || c.Download < previous[i].Download) {
			return none, ErrConflict
		}
	}
	enc, err := sealPlanValue(key, executionAAD("usage-report", in.EpochID, in.Sequence), in)
	if err != nil {
		return none, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_usage_receipts(epoch_id,sequence,sha256,final,report_enc,created) VALUES(?,?,?,?,?,?)`, in.EpochID, in.Sequence, hash, in.Final, enc, time.Now().UnixNano()); err != nil {
		return none, err
	}
	dirty := false
	for i, c := range in.Counters {
		p := b.Principals[i]
		up, down := c.Upload, c.Download
		if sequence > 0 {
			up -= previous[i].Upload
			down -= previous[i].Download
		}
		if p.Billable {
			if p.ClientID == "" || p.Role != "customer" {
				return none, ErrUnavailable
			}
			var oldUp, oldDown int64
			var before, after int
			if err = tx.QueryRowContext(ctx, `SELECT a.uplink,a.downlink,e.eligible FROM pro_usage_accounts a JOIN pro_usage_eligibility e ON e.client_id=a.client_id WHERE a.client_id=?`, p.ClientID).Scan(&oldUp, &oldDown, &before); err != nil {
				return none, err
			}
			if uint64(oldUp) > math.MaxInt64-up || uint64(oldDown) > math.MaxInt64-down || uint64(oldUp)+up > math.MaxInt64-(uint64(oldDown)+down) {
				return none, ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `UPDATE pro_usage_accounts SET uplink=?,downlink=? WHERE client_id=?`, oldUp+int64(up), oldDown+int64(down), p.ClientID); err != nil {
				return none, err
			}
			if err = tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, p.ClientID).Scan(&after); err != nil {
				return none, err
			}
			dirty = dirty || before != after
		} else {
			if p.Role != "owner_aggregate" && p.Role != "transit" && p.Role != "system_probe" {
				return none, ErrUnavailable
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_aggregate_totals(principal_id,node_id,inbound_id,role,upload,download) VALUES(?,?,?,?,?,?) ON CONFLICT(principal_id) DO UPDATE SET upload=upload+excluded.upload,download=download+excluded.download`, p.ID, node, p.InboundID, p.Role, int64(up), int64(down)); err != nil {
				return none, err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_usage_deltas(epoch_id,sequence,principal_id,client_id,role,upload,download) VALUES(?,?,?,?,?,?,?)`, in.EpochID, in.Sequence, p.ID, p.ClientID, p.Role, int64(up), int64(down)); err != nil {
			return none, err
		}
	}
	if in.Final && state != "uncertain" {
		settled, err := settleExecutionLiveness(ctx, tx, in.EpochID, time.Now().UnixNano())
		if err != nil {
			return none, err
		}
		dirty = dirty || settled
		state = "sealed"
	}
	if dirty {
		if _, err = tx.ExecContext(ctx, `UPDATE native_config_generation SET value=value+1 WHERE id=1 AND value<9007199254740991`); err != nil {
			return none, err
		}
	}
	cursorEnc, err = sealPlanValue(key, executionAAD("usage-cursor", in.EpochID, in.Sequence), in.Counters)
	if err != nil {
		return none, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE inbound_execution_epochs SET sequence=?,cursor_enc=?,state=? WHERE epoch_id=? AND sequence=? AND (state IN('open','uncertain') OR (state='sealed' AND EXISTS(SELECT 1 FROM inbound_execution_reconciliations r WHERE r.epoch_id=inbound_execution_epochs.epoch_id)))`, in.Sequence, cursorEnc, state, in.EpochID, sequence)
	if err != nil {
		return none, err
	}
	if changed, e := result.RowsAffected(); e != nil || changed != 1 {
		return none, ErrConflict
	}
	return ix.UsageReceipt{EpochID: in.EpochID, Sequence: in.Sequence, SHA256: hash, Final: in.Final}, tx.Commit()
}

func (s *Store) ObserveInboundExecution(ctx context.Context, key []byte, in ix.Observation) error {
	if !idRE.MatchString(in.NodeID) || !routeID.MatchString(in.InstallationID) || in.Generation == 0 || in.ObservedUnixNano <= 0 || in.ObservedUnixNano > time.Now().Add(5*time.Second).UnixNano() {
		return ErrInvalid
	}
	switch in.State {
	case "staging", "prepared", "active", "ready", "stopped", "failed", "blocked", "draining", "degraded":
	default:
		return ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b, err := readExecutionBundle(ctx, tx, key, in.NodeID, in.InstallationID, in.Generation)
	if err != nil {
		return err
	}
	if b.OperationID != in.OperationID || b.ConfigSHA != in.ConfigSHA || b.ManifestSHA != in.ManifestSHA {
		return ErrConflict
	}
	uncertain := in.ErrorCode == "accounting_tail_uncertain" || in.AccountingState == "uncertain"
	if e := in.InitialEpoch; e != nil {
		if !uncertain || in.ProcessReady || !validExecutionEpochInput(*e) || e.EpochID != in.EpochID || e.NodeID != in.NodeID || e.InstallationID != in.InstallationID || e.Generation != in.Generation || e.ConfigSHA != in.ConfigSHA || e.ManifestSHA != in.ManifestSHA {
			return ErrInvalid
		}
		_, _, found, err := readExecutionEpochReceipt(ctx, tx, key, *e)
		if err != nil {
			return err
		}
		if !found {
			// An authenticated lost birth is recorded with no measured baseline.
			// The following uncertainty transition places its exact client holds
			// in this same transaction; this never grants execution authority.
			if err = insertExecutionEpoch(ctx, tx, key, b, *e); err != nil {
				return err
			}
		}
	}
	if uncertain {
		if in.EpochID == "" {
			return ErrInvalid
		}
		if err = markExecutionEpochUncertain(ctx, tx, key, in.NodeID, in.InstallationID, in.EpochID, "accounting_tail_uncertain"); err != nil {
			return err
		}
	}
	var desired, observed uint64
	var oldEnc []byte
	if err = tx.QueryRowContext(ctx, `SELECT desired_generation,observed_generation,observation_enc FROM inbound_execution_nodes WHERE node_id=? AND installation_id=?`, in.NodeID, in.InstallationID).Scan(&desired, &observed, &oldEnc); err != nil {
		return err
	}
	if desired != in.Generation || observed > in.Generation {
		if uncertain {
			return tx.Commit()
		}
		return ErrConflict
	}
	if len(oldEnc) > 0 {
		var old ix.Observation
		if openPlanValue(key, executionAAD("observation", in.NodeID+"/"+in.InstallationID, observed), oldEnc, &old) != nil {
			return ErrUnavailable
		}
		if in.ObservedUnixNano < old.ObservedUnixNano {
			return ErrConflict
		}
		if in.ObservedUnixNano == old.ObservedUnixNano && ix.Digest(in) != ix.Digest(old) {
			return ErrConflict
		}
	}
	ready := in.State == "active" || in.State == "ready"
	if ready {
		if !in.ProcessReady || in.LeaseExpires <= time.Now().UnixNano() || in.LeaseExpires > time.Now().Add(35*time.Second).UnixNano() || (in.HealthState != "healthy" && in.HealthState != "engine_ready") || in.AccountingState != "accepted" {
			return ErrConflict
		}
		if err = executionBundleAuthority(ctx, tx, key, b); err != nil {
			return err
		}
		if _, err = executionAccountingFreshUntil(ctx, tx, in.NodeID, in.InstallationID, in.Generation, in.EpochID, time.Now().UnixNano()); err != nil {
			return err
		}
	}
	if in.State == "stopped" {
		if in.ProcessReady || len(b.Listeners) > 0 {
			return ErrConflict
		}
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_execution_epochs WHERE node_id=? AND state!='sealed'`, in.NodeID).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	enc, err := sealPlanValue(key, executionAAD("observation", in.NodeID+"/"+in.InstallationID, in.Generation), in)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE inbound_execution_nodes SET observed_generation=?,observation_enc=? WHERE node_id=? AND installation_id=? AND desired_generation=?`, in.Generation, enc, in.NodeID, in.InstallationID, in.Generation); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT o.id FROM inbound_execution_operation_nodes n JOIN inbound_execution_operations o ON o.id=n.operation_id WHERE n.node_id=? AND n.installation_id=? AND n.generation=? AND o.state!='superseded' ORDER BY o.id`, in.NodeID, in.InstallationID, in.Generation)
	if err != nil {
		return err
	}
	operations := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		operations = append(operations, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range operations {
		if err = completeExecutionOperation(ctx, tx, key, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func completeExecutionOperation(ctx context.Context, tx *sql.Tx, key []byte, operation string) error {
	var inbound, revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT inbound_id,revision FROM inbound_execution_operations WHERE id=?`, operation).Scan(&inbound, &revision); err != nil {
		return err
	}
	op, err := readExecutionOperation(ctx, tx, key, inbound, operation)
	if err != nil {
		return err
	}
	complete := true
	failed := false
	for _, t := range op.Targets {
		if t.ObservedGeneration != t.DesiredGeneration || (t.State != "active" && t.State != "ready" && t.State != "stopped") {
			complete = false
		}
		if t.State == "failed" || t.State == "blocked" || t.State == "degraded" {
			failed = true
		}
	}
	state := "applying"
	if failed {
		state = "blocked"
	}
	if complete {
		state = "active"
		var desiredState string
		if err = tx.QueryRowContext(ctx, `SELECT desired_state FROM inbound_execution_heads WHERE inbound_id=? AND pending_operation=?`, inbound, operation).Scan(&desiredState); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if desiredState != "" {
			state = desiredState
		}
		active := revision
		snapshot, e := readExecutionSnapshot(ctx, tx, key, inbound, revision)
		if e != nil {
			return e
		}
		if snapshot.Dormant {
			state = "blocked"
		}
		if state != "active" {
			active = 0
		}
		if _, err = tx.ExecContext(ctx, `UPDATE inbound_execution_heads SET active_revision=?,observed_state=?,active_operation=?,pending_operation='' WHERE inbound_id=? AND desired_revision=? AND pending_operation=?`, active, state, operation, inbound, revision, operation); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE inbound_execution_operations SET state=?,updated=? WHERE id=?`, state, time.Now().UnixNano(), operation); err != nil {
		return err
	}
	return nil
}
