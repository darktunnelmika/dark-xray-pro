package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// N9 is the accounting barrier, not health approval or subscription publication.
// Both installations must already be operator-approved loopback LAB channels.
type MeteredHandoverRequest struct {
	ID            string              `json:"id"`
	SourceNode    string              `json:"source_node"`
	TargetNode    string              `json:"target_node"`
	SourceStageID string              `json:"source_stage_id"`
	TargetStageID string              `json:"target_stage_id"`
	SourceUsage   mx.MeteredCondition `json:"source_usage"`
}

type MeteredHandoverView struct {
	MeteredHandoverRequest
	SourceInstallation string `json:"source_installation"`
	TargetInstallation string `json:"target_installation"`
	Created            int64  `json:"created_unix_nano"`
	DrainCommandID     string `json:"drain_command_id"`
	ApplyCommandID     string `json:"apply_command_id"`
	Phase              string `json:"phase"`
	CanPublish         bool   `json:"can_publish"`
	Cancelled          bool   `json:"cancelled"`
}

func ensureMeteredHandovers(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS native_metered_handovers(id TEXT PRIMARY KEY,source_node TEXT NOT NULL,target_node TEXT NOT NULL,source_installation TEXT NOT NULL,target_installation TEXT NOT NULL,request TEXT NOT NULL,fingerprint TEXT NOT NULL,created INTEGER NOT NULL,drain_command TEXT NOT NULL DEFAULT '',apply_command TEXT NOT NULL DEFAULT '',cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN(0,1)),CHECK(source_node!=target_node))`,
		`CREATE TABLE IF NOT EXISTS native_metered_handover_nodes(node_id TEXT PRIMARY KEY REFERENCES native_metered_route_channels(node_id),handover_id TEXT NOT NULL REFERENCES native_metered_handovers(id))`,
		`CREATE INDEX IF NOT EXISTS native_metered_handover_history ON native_metered_handovers(created DESC,id DESC)`,
		`CREATE TRIGGER IF NOT EXISTS native_metered_handover_identity BEFORE UPDATE OF id,source_node,target_node,source_installation,target_installation,request,fingerprint,created ON native_metered_handovers BEGIN SELECT RAISE(ABORT,'handover identity immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_metered_handover_no_delete BEFORE DELETE ON native_metered_handovers BEGIN SELECT RAISE(ABORT,'handover audit immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_metered_handover_drain_immutable BEFORE UPDATE OF drain_command ON native_metered_handovers WHEN OLD.drain_command!='' AND OLD.drain_command!=NEW.drain_command BEGIN SELECT RAISE(ABORT,'handover drain immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_metered_handover_apply_immutable BEFORE UPDATE OF apply_command ON native_metered_handovers WHEN OLD.apply_command!='' AND OLD.apply_command!=NEW.apply_command BEGIN SELECT RAISE(ABORT,'handover apply immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_metered_handover_cancel_immutable BEFORE UPDATE OF cancelled ON native_metered_handovers WHEN OLD.cancelled=1 OR NEW.drain_command!='' BEGIN SELECT RAISE(ABORT,'handover cancellation unsafe'); END`,
		// Older readers must refuse this DB rather than ignore node reservations.
	} {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version < 8 {
		if _, err := tx.Exec(`PRAGMA user_version=8`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func readMeteredHandoverRecord(ctx context.Context, tx *sql.Tx, id string) (MeteredHandoverView, string, error) {
	var v MeteredHandoverView
	var raw, fp string
	err := tx.QueryRowContext(ctx, `SELECT request,fingerprint,source_installation,target_installation,created,drain_command,apply_command,cancelled FROM native_metered_handovers WHERE id=?`, id).Scan(&raw, &fp, &v.SourceInstallation, &v.TargetInstallation, &v.Created, &v.DrainCommandID, &v.ApplyCommandID, &v.Cancelled)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fp, ErrNotFound
	}
	if err != nil {
		return v, fp, err
	}
	if json.Unmarshal([]byte(raw), &v.MeteredHandoverRequest) != nil || v.ID != id {
		return v, fp, ErrUnavailable
	}
	return v, fp, nil
}

func readMeteredHandover(ctx context.Context, tx *sql.Tx, id string) (MeteredHandoverView, string, error) {
	v, fp, err := readMeteredHandoverRecord(ctx, tx, id)
	if err != nil {
		return v, fp, err
	}
	v.Phase = "awaiting_source_drain"
	if v.Cancelled {
		v.Phase = "cancelled"
		return v, fp, nil
	}
	for _, step := range []struct{ id, pending, done, outcome string }{
		{v.DrainCommandID, "source_drain_pending", "source_drained", "drained"},
		{v.ApplyCommandID, "target_apply_pending", "target_applied_health_required", "applied"},
	} {
		if step.id == "" {
			continue
		}
		var state, receipt string
		if err := tx.QueryRowContext(ctx, `SELECT state,receipt FROM native_metered_route_commands WHERE id=?`, step.id).Scan(&state, &receipt); err != nil {
			return v, fp, err
		}
		v.Phase = step.pending
		if state == "queued" || state == "delivered" {
			continue
		}
		var ack mx.TransactionReceipt
		if state != "acknowledged" || json.Unmarshal([]byte(receipt), &ack) != nil || ack.Outcome != step.outcome {
			v.Phase = "recovery_required"
			return v, fp, nil
		}
		v.Phase = step.done
	}
	return v, fp, nil
}

func handoverIdleChannel(ctx context.Context, tx *sql.Tx, node string) (MeteredRouteChannelView, error) {
	ch, serial, err := meteredRouteObservation(ctx, tx, node)
	if err != nil {
		return ch, err
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_metered_route_commands WHERE node_id=? AND state IN('queued','delivered')`, node).Scan(&pending); err != nil {
		return ch, err
	}
	if !ch.ObservationFresh || ch.Status.Serial != serial || pending != 0 || ch.Status.InterruptedCommandSHA != "" || ch.Status.Usage == nil {
		return ch, ErrConflict
	}
	return ch, meteredLatestCondition(ctx, tx, node, ch.InstallationID, *ch.Status.Usage)
}

func (s *Store) CreateMeteredHandover(ctx context.Context, key []byte, in MeteredHandoverRequest) (MeteredHandoverView, error) {
	var empty MeteredHandoverView
	if s.usage == nil {
		return empty, ErrUnavailable
	}
	if !routeID.MatchString(in.ID) || !routeNodeID.MatchString(in.SourceNode) || !routeNodeID.MatchString(in.TargetNode) || in.SourceNode == in.TargetNode || !routeID.MatchString(in.SourceStageID) || !routeID.MatchString(in.TargetStageID) || in.SourceUsage.EpochID == "" || in.SourceUsage.Settlement != (uc.Receipt{}) {
		return empty, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	fp := fingerprint("metered-handover-v1", in)
	prior, old, err := readMeteredHandover(ctx, tx, in.ID)
	if err == nil {
		if old != fp {
			return empty, ErrConflict
		}
		return prior, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return empty, err
	}
	source, err := handoverIdleChannel(ctx, tx, in.SourceNode)
	if err != nil {
		return empty, err
	}
	target, err := handoverIdleChannel(ctx, tx, in.TargetNode)
	if err != nil {
		return empty, err
	}
	if source.Status.CurrentStageID != in.SourceStageID || *source.Status.Usage != in.SourceUsage || !source.Status.Observed.ProcessReady || source.Status.Observed.ActiveSequence == 0 || target.Status.Observed.ProcessReady || target.Status.Observed.ActiveSequence != 0 || target.Status.Observed.ActiveSHA != "" || target.Status.Observed.LeaseExpires != 0 || target.Status.StagedID != in.TargetStageID {
		return empty, ErrConflict
	}
	if target.Status.Usage.EpochID != "" && !uc.ValidReceipt(target.Status.Usage.Settlement) {
		return empty, ErrConflict
	}
	stages := make([]meteredRouteRow, 0, 2)
	for i, id := range []string{in.SourceStageID, in.TargetStageID} {
		stage, err := readMeteredRouteCommand(ctx, tx, key, id)
		node, install := in.SourceNode, source.InstallationID
		if i == 1 {
			node, install = in.TargetNode, target.InstallationID
		}
		if err != nil {
			return empty, err
		}
		if stage.view.NodeID != node || stage.view.InstallationID != install || stage.view.Action != "stage" || stage.view.State != "acknowledged" || stage.view.Receipt == nil || stage.view.Receipt.Outcome != "staged" {
			return empty, ErrConflict
		}
		if err = meteredRouteStillCurrent(ctx, tx, key, stage); err != nil {
			return empty, err
		}
		stages = append(stages, stage)
	}
	// Transfer the same inbounds and current customer identities; never use a
	// new Node's unrelated configuration as a replacement for the source.
	a, b := stages[0].payload.Request.Drafts, stages[1].payload.Request.Drafts
	if len(a) != len(b) {
		return empty, ErrConflict
	}
	refs := map[uint64]uint64{}
	for _, r := range a {
		refs[r.InboundID] = r.InboundRevision
	}
	for _, r := range b {
		if refs[r.InboundID] != r.InboundRevision {
			return empty, ErrConflict
		}
	}
	if stages[0].payload.Candidate.SHA256 != source.Status.Observed.ActiveSHA || stages[1].payload.Command.RuntimeInstance != target.Status.Instance || stages[1].payload.Command.Expected != mx.Condition(target.Status.Observed) || stages[1].payload.Command.Usage == nil || *stages[1].payload.Command.Usage != *target.Status.Usage || stages[1].payload.Command.Expires <= time.Now().UnixNano() {
		return empty, ErrConflict
	}
	epoch, err := meteredEpoch(ctx, tx, in.SourceNode, source.InstallationID, in.SourceUsage)
	if err != nil || epoch.ConfigSHA != source.Status.Observed.ActiveSHA {
		return empty, ErrConflict
	}
	raw, _ := json.Marshal(in)
	_, err = tx.ExecContext(ctx, `INSERT INTO native_metered_handovers(id,source_node,target_node,source_installation,target_installation,request,fingerprint,created) VALUES(?,?,?,?,?,?,?,?)`, in.ID, in.SourceNode, in.TargetNode, source.InstallationID, target.InstallationID, string(raw), fp, time.Now().UnixNano())
	if err != nil {
		return empty, err
	}
	for _, node := range []string{in.SourceNode, in.TargetNode} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO native_metered_handover_nodes(node_id,handover_id) VALUES(?,?)`, node, in.ID); err != nil {
			return empty, ErrConflict
		}
	}
	v, _, err := readMeteredHandover(ctx, tx, in.ID)
	if err != nil {
		return empty, err
	}
	return v, tx.Commit()
}

// QueueMeteredHandoverCommand is explicit owner intent for ONE reviewed step.
// It never schedules the next step automatically after an uncertain reply.
func (s *Store) QueueMeteredHandoverCommand(ctx context.Context, key []byte, id string, in MeteredRouteCommandRequest) (MeteredRouteCommandView, error) {
	var empty MeteredRouteCommandView
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	op, _, err := readMeteredHandoverRecord(ctx, tx, id)
	if err != nil {
		return empty, err
	}
	if op.Cancelled {
		return empty, ErrConflict
	}
	var node, column, previous string
	switch in.Action {
	case "drain":
		node, column, previous = op.SourceNode, "drain_command", op.DrainCommandID
		if in.StageID != op.SourceStageID || in.Usage == nil || *in.Usage != op.SourceUsage {
			return empty, ErrConflict
		}
	case "apply":
		node, column, previous = op.TargetNode, "apply_command", op.ApplyCommandID
		if in.StageID != op.TargetStageID {
			return empty, ErrConflict
		}
	default:
		return empty, ErrInvalid
	}
	if previous != "" && previous != in.ID {
		return empty, ErrConflict
	}
	if previous == "" {
		if !routeID.MatchString(in.ID) {
			return empty, ErrInvalid
		}
		// Existing command IDs cannot be adopted into another operation.
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_metered_route_commands WHERE id=?`, in.ID).Scan(&n); err != nil {
			return empty, err
		}
		if n != 0 {
			return empty, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `UPDATE native_metered_handovers SET `+column+`=? WHERE id=?`, in.ID, id); err != nil {
			return empty, err
		}
	}
	v, err := queueMeteredRouteCommand(ctx, tx, key, node, in, id)
	if err != nil {
		return empty, err
	}
	return v, tx.Commit()
}

func (s *Store) MeteredHandover(ctx context.Context, id string) (MeteredHandoverView, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MeteredHandoverView{}, err
	}
	defer tx.Rollback()
	v, _, err := readMeteredHandover(ctx, tx, id)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

// Cancellation is safe only before any source-drain command was committed.
// Unknown, failed or applied operations keep BOTH reservations after restart.
func (s *Store) CancelMeteredHandover(ctx context.Context, id string) (MeteredHandoverView, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MeteredHandoverView{}, err
	}
	defer tx.Rollback()
	v, _, err := readMeteredHandover(ctx, tx, id)
	if err != nil {
		return v, err
	}
	if v.Cancelled {
		return v, tx.Commit()
	}
	if v.DrainCommandID != "" || v.ApplyCommandID != "" {
		return v, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE native_metered_handovers SET cancelled=1 WHERE id=?`, id); err != nil {
		return v, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM native_metered_handover_nodes WHERE handover_id=?`, id); err != nil {
		return v, err
	}
	v.Cancelled, v.Phase = true, "cancelled"
	return v, tx.Commit()
}

func meteredHandoverCommandAllowed(ctx context.Context, tx *sql.Tx, key []byte, row meteredRouteRow) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT handover_id FROM native_metered_handover_nodes WHERE node_id=?`, row.view.NodeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if row.payload.HandoverID != "" {
			return ErrConflict
		}
		return nil
	}
	if err != nil {
		return err
	}
	// Emergency stop never grants execution authority; reservations remain.
	if row.payload.HandoverID == "" && row.view.Action == "drain" {
		return nil
	}
	if row.payload.HandoverID != id {
		return ErrConflict
	}
	op, _, err := readMeteredHandoverRecord(ctx, tx, id)
	if err != nil {
		return err
	}
	if op.Cancelled {
		return ErrConflict
	}
	if row.view.NodeID == op.SourceNode && row.view.InstallationID == op.SourceInstallation && row.view.ID == op.DrainCommandID && row.view.Action == "drain" && row.view.StageID == op.SourceStageID {
		return nil
	}
	if row.view.NodeID != op.TargetNode || row.view.InstallationID != op.TargetInstallation || row.view.ID != op.ApplyCommandID || row.view.Action != "apply" || row.view.StageID != op.TargetStageID || op.DrainCommandID == "" {
		return ErrConflict
	}
	drain, err := readMeteredRouteCommand(ctx, tx, key, op.DrainCommandID)
	if err != nil {
		return err
	}
	ack := drain.view.Receipt
	if drain.payload.HandoverID != id || drain.view.NodeID != op.SourceNode || drain.view.InstallationID != op.SourceInstallation || drain.view.State != "acknowledged" || ack == nil || ack.Outcome != "drained" || ack.Metered == nil || ack.Metered.After.EpochID != op.SourceUsage.EpochID || ack.Metered.After.EpochSHA != op.SourceUsage.EpochSHA || ack.Metered.PriorSettlement != ack.Metered.After.Settlement {
		return ErrConflict
	}
	if err = meteredSeal(ctx, tx, op.SourceNode, op.SourceInstallation, ack.Metered.After.Settlement); err != nil {
		return err
	}
	ch, _, err := meteredRouteObservation(ctx, tx, op.SourceNode)
	if err != nil {
		return err
	}
	if !ch.ObservationFresh || ch.ReportedAt < ack.ObservedAt || ch.InstallationID != op.SourceInstallation || ch.Status.CurrentStageID != op.SourceStageID || ch.Status.Usage == nil || *ch.Status.Usage != ack.Metered.After || ch.Status.Observed.ProcessReady || ch.Status.Observed.ActiveSequence != 0 || ch.Status.Observed.ActiveSHA != "" || ch.Status.Observed.LeaseExpires != 0 || ch.Status.InterruptedCommandSHA != "" {
		return ErrConflict
	}
	return meteredLatestCondition(ctx, tx, op.SourceNode, op.SourceInstallation, *ch.Status.Usage)
}
