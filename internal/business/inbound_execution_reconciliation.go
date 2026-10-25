package business

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type executionReconciliationRecord struct {
	Input   ix.ReconciliationInput   `json:"input"`
	Receipt ix.ReconciliationReceipt `json:"receipt"`
}

type executionReconciliationView struct {
	State    ix.ReconciliationState
	Epoch    ix.EpochInput
	Counters []ix.Counter
}

func normalizeExecutionReconciliation(in ix.ReconciliationInput) (ix.ReconciliationInput, error) {
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.EpochID) || in.ExpectedRevision >= routeMax || in.Action != "accept_owner_loss" || !validName(in.Reason, 512) || in.ReviewSHA != "" && !routeSHA.MatchString(in.ReviewSHA) {
		return in, ErrInvalid
	}
	return in, nil
}

func readExecutionReconciliationView(ctx context.Context, tx *sql.Tx, key []byte, inbound uint64, id string) (executionReconciliationView, error) {
	v := executionReconciliationView{State: ix.ReconciliationState{EpochID: id, KnownUsage: []ix.ReconciliationUsage{}, AffectedClientIDs: []string{}, AffectedInboundIDs: []uint64{}, AllowedActions: []string{}, Blockers: []ix.Blocker{}, UnknownUsagePolicy: "unmeasured"}, Counters: []ix.Counter{}}
	var enc, cursor []byte
	var epochSHA string
	err := tx.QueryRowContext(ctx, `SELECT node_id,installation_id,generation,state,sequence,identity_sha,payload_enc,cursor_enc FROM inbound_execution_epochs WHERE epoch_id=?`, id).Scan(&v.State.NodeID, &v.State.InstallationID, &v.State.Generation, &v.State.State, &v.State.Revision, &epochSHA, &enc, &cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if openPlanValue(key, executionAAD("epoch", id, v.State.Generation), enc, &v.Epoch) != nil || ix.Digest(v.Epoch) != epochSHA {
		return v, ErrUnavailable
	}
	b, err := readExecutionBundle(ctx, tx, key, v.State.NodeID, v.State.InstallationID, v.State.Generation)
	if err != nil {
		return v, err
	}
	if ix.ValidateEpochIdentity(b, v.Epoch) != nil {
		return v, ErrUnavailable
	}
	present := false
	for _, r := range b.Revisions {
		v.State.AffectedInboundIDs = append(v.State.AffectedInboundIDs, r.InboundID)
		present = present || r.InboundID == inbound
	}
	if !present {
		return v, ErrNotFound
	}
	if v.State.State != "uncertain" {
		return v, ErrConflict
	}
	if openPlanValue(key, executionAAD("usage-cursor", id, v.State.Revision), cursor, &v.Counters) != nil {
		return v, ErrUnavailable
	}
	if v.State.Revision == 0 && len(v.Counters) != 0 || v.State.Revision > 0 && len(v.Counters) != len(b.Principals) {
		return v, ErrUnavailable
	}
	if v.State.Revision > 0 {
		var terminal bool
		if err = tx.QueryRowContext(ctx, `SELECT final FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=?`, id, v.State.Revision).Scan(&terminal); err != nil {
			return v, err
		}
		// A terminal sample can coexist with a permanent uncertainty hold;
		// it remains measured evidence, never evidence for the missing tail.
		v.State.HasMeasuredUsage = true
	}
	held := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT h.client_id FROM inbound_execution_usage_holds h LEFT JOIN inbound_execution_usage_hold_resolutions r ON r.epoch_id=h.epoch_id AND r.client_id=h.client_id WHERE h.epoch_id=? AND r.epoch_id IS NULL ORDER BY h.client_id`, id)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var client string
		if err = rows.Scan(&client); err != nil {
			rows.Close()
			return v, err
		}
		held[client] = true
		v.State.AffectedClientIDs = append(v.State.AffectedClientIDs, client)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	for i, p := range b.Principals {
		if p.Billable && (p.ClientID == "" || !held[p.ClientID]) {
			return v, ErrUnavailable
		}
		if v.State.Revision == 0 {
			continue
		}
		c := v.Counters[i]
		if c.PrincipalID != p.ID {
			return v, ErrUnavailable
		}
		v.State.KnownUsage = append(v.State.KnownUsage, ix.ReconciliationUsage{PrincipalID: p.ID, InboundID: p.InboundID, ClientID: p.ClientID, Role: p.Role, Upload: c.Upload, Download: c.Download, Held: p.Billable && held[p.ClientID]})
	}
	v.State.ConfigSHA, v.State.ManifestSHA = b.ConfigSHA, b.ManifestSHA
	v.State.ProcessIdentitySHA = ix.BytesSHA([]byte(v.Epoch.ProcessIdentity))
	v.State.AllowedActions = []string{"accept_owner_loss"}
	sort.Slice(v.State.AffectedInboundIDs, func(i, j int) bool { return v.State.AffectedInboundIDs[i] < v.State.AffectedInboundIDs[j] })
	return v, nil
}

func executionReconciliationReview(actor string, inbound uint64, in ix.ReconciliationInput, v executionReconciliationView) string {
	in.ReviewSHA = ""
	return fingerprint("managed-inbound-owner-loss-review-v1", struct {
		Actor                 string
		Inbound               uint64
		Input                 ix.ReconciliationInput
		State                 ix.ReconciliationState
		EpochSHA, CountersSHA string
	}{actor, inbound, in, v.State, ix.Digest(v.Epoch), ix.Digest(v.Counters)})
}

func (s *Store) PreviewInboundExecutionReconciliation(ctx context.Context, key []byte, actor string, inbound uint64, input ix.ReconciliationInput) (ix.ReconciliationPreview, error) {
	out := ix.ReconciliationPreview{Blockers: []ix.Blocker{}}
	in, err := normalizeExecutionReconciliation(input)
	if err != nil {
		return out, err
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = executionActor(ctx, tx, actor); err != nil {
		return out, err
	}
	v, err := readExecutionReconciliationView(ctx, tx, key, inbound, in.EpochID)
	if err != nil {
		return out, err
	}
	out.State = v.State
	if in.ExpectedRevision != v.State.Revision {
		return out, ErrConflict
	}
	out.Valid = true
	out.ReviewSHA = executionReconciliationReview(actor, inbound, in, v)
	return out, tx.Commit()
}

func readExecutionReconciliationReceipt(ctx context.Context, tx *sql.Tx, key []byte, event string) (executionReconciliationRecord, string, error) {
	var out executionReconciliationRecord
	var revision uint64
	var enc []byte
	var fingerprint string
	err := tx.QueryRowContext(ctx, `SELECT revision,fingerprint,payload_enc FROM inbound_execution_reconciliations WHERE event_id=?`, event).Scan(&revision, &fingerprint, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return out, "", ErrNotFound
	}
	if err != nil {
		return out, "", err
	}
	if openPlanValue(key, executionAAD("owner-loss", event, revision), enc, &out) != nil || out.Receipt.EventID != event || out.Receipt.Revision != revision || out.Input.EventID != event || out.Input.ExpectedRevision != revision {
		return out, "", ErrUnavailable
	}
	return out, fingerprint, nil
}

func (s *Store) ReconcileInboundExecution(ctx context.Context, key []byte, actor string, inbound uint64, input ix.ReconciliationInput) (ix.ReconciliationReceipt, error) {
	var none ix.ReconciliationReceipt
	in, err := normalizeExecutionReconciliation(input)
	if err != nil {
		return none, err
	}
	if !routeSHA.MatchString(in.ReviewSHA) {
		return none, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()
	if err = executionActor(ctx, tx, actor); err != nil {
		return none, err
	}
	fp := fingerprint("managed-inbound-owner-loss-command-v1", struct {
		Actor   string
		Inbound uint64
		Input   ix.ReconciliationInput
	}{actor, inbound, in})
	previous, oldFP, err := readExecutionReconciliationReceipt(ctx, tx, key, in.EventID)
	if err == nil {
		if oldFP != fp || previous.Receipt.ActorID != actor || previous.Receipt.InboundID != inbound {
			return none, ErrConflict
		}
		previous.Receipt.Replayed = true
		return previous.Receipt, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return none, err
	}
	v, err := readExecutionReconciliationView(ctx, tx, key, inbound, in.EpochID)
	if err != nil {
		return none, err
	}
	if in.ExpectedRevision != v.State.Revision || executionReconciliationReview(actor, inbound, in, v) != in.ReviewSHA {
		return none, ErrConflict
	}
	now := time.Now().UnixNano()
	receipt := ix.ReconciliationReceipt{EventID: in.EventID, EpochID: in.EpochID, InboundID: inbound, NodeID: v.State.NodeID, InstallationID: v.State.InstallationID, Generation: v.State.Generation, ActorID: actor, Reason: in.Reason, Revision: in.ExpectedRevision, State: "accepted_owner_loss", UnknownUsagePolicy: "owner_loss", KnownUsage: v.State.KnownUsage, AffectedClientIDs: v.State.AffectedClientIDs, AffectedInboundIDs: v.State.AffectedInboundIDs, HasMeasuredUsage: v.State.HasMeasuredUsage, CreatedUnixNano: now}
	clearance, err := ix.SealClearance(key, ix.EpochClearance{Version: ix.Version, EventID: in.EventID, EpochID: in.EpochID, NodeID: v.State.NodeID, InstallationID: v.State.InstallationID, Generation: v.State.Generation, EpochSHA: ix.Digest(v.Epoch), KnownSequence: v.State.Revision, KnownCountersSHA: ix.Digest(v.Counters), Policy: "owner_loss", CreatedUnixNano: now})
	if err != nil {
		return none, err
	}
	enc, err := sealPlanValue(key, executionAAD("owner-loss", in.EventID, in.ExpectedRevision), executionReconciliationRecord{Input: in, Receipt: receipt})
	if err != nil {
		return none, err
	}
	clearanceEnc, err := sealPlanValue(key, executionAAD("owner-loss-clearance", in.EpochID, in.ExpectedRevision), clearance)
	if err != nil {
		return none, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_reconciliations(event_id,epoch_id,inbound_id,actor_id,revision,fingerprint,review_sha,payload_enc,clearance_enc,created) VALUES(?,?,?,?,?,?,?,?,?,?)`, in.EventID, in.EpochID, inbound, actor, in.ExpectedRevision, fp, in.ReviewSHA, enc, clearanceEnc, now); err != nil {
		return none, err
	}
	if err = blockExecutionResume(ctx, tx, key, v.Epoch, now); err != nil {
		return none, err
	}
	for _, client := range v.State.AffectedClientIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_usage_hold_resolutions(epoch_id,client_id,event_id,created) VALUES(?,?,?,?)`, in.EpochID, client, in.EventID, now); err != nil {
			return none, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE pro_usage_accounts SET holds=holds-1 WHERE client_id=? AND holds>0`, client)
		if err != nil {
			return none, err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return none, ErrUnavailable
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE inbound_execution_epochs SET state='sealed' WHERE epoch_id=? AND state='uncertain' AND sequence=?`, in.EpochID, in.ExpectedRevision)
	if err != nil {
		return none, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return none, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE native_config_generation SET value=value+1 WHERE id=1 AND value<9007199254740991`); err != nil {
		return none, err
	}
	return receipt, tx.Commit()
}

func (s *Store) InboundExecutionReconciliationReceipt(ctx context.Context, key []byte, actor string, inbound uint64, event string) (ix.ReconciliationReceipt, error) {
	if !eventRE.MatchString(event) {
		return ix.ReconciliationReceipt{}, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.ReconciliationReceipt{}, err
	}
	defer tx.Rollback()
	if err = executionActor(ctx, tx, actor); err != nil {
		return ix.ReconciliationReceipt{}, err
	}
	record, _, err := readExecutionReconciliationReceipt(ctx, tx, key, event)
	if err != nil {
		return record.Receipt, err
	}
	if record.Receipt.InboundID != inbound {
		return ix.ReconciliationReceipt{}, ErrNotFound
	}
	return record.Receipt, tx.Commit()
}

func readExecutionClearance(ctx context.Context, tx *sql.Tx, key []byte, epoch string) (ix.EpochClearance, bool, error) {
	var out ix.EpochClearance
	var enc []byte
	var revision uint64
	err := tx.QueryRowContext(ctx, `SELECT revision,clearance_enc FROM inbound_execution_reconciliations WHERE epoch_id=?`, epoch).Scan(&revision, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	if openPlanValue(key, executionAAD("owner-loss-clearance", epoch, revision), enc, &out) != nil || ix.VerifyClearance(key, out) != nil || out.EpochID != epoch || out.KnownSequence != revision {
		return out, false, ErrUnavailable
	}
	return out, true, nil
}

func (s *Store) InboundExecutionClearance(ctx context.Context, key []byte, node, install, epoch string) (ix.EpochClearance, bool, error) {
	if !idRE.MatchString(node) || !routeID.MatchString(install) || !idRE.MatchString(epoch) {
		return ix.EpochClearance{}, false, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.EpochClearance{}, false, err
	}
	defer tx.Rollback()
	out, found, err := readExecutionClearance(ctx, tx, key, epoch)
	if err != nil || !found {
		return out, found, err
	}
	if out.NodeID != node || out.InstallationID != install {
		return ix.EpochClearance{}, false, ErrNotFound
	}
	return out, true, tx.Commit()
}

func listExecutionReconciliations(ctx context.Context, tx *sql.Tx, key []byte, inbound uint64) ([]ix.ReconciliationState, error) {
	out := []ix.ReconciliationState{}
	rows, err := tx.QueryContext(ctx, `SELECT epoch_id FROM inbound_execution_epochs WHERE state='uncertain' ORDER BY created,epoch_id`)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		v, err := readExecutionReconciliationView(ctx, tx, key, inbound, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v.State)
	}
	return out, nil
}
