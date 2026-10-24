package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// Browser review contains public routing metadata, never a runnable config.
type MeteredRouteControlView struct {
	MeteredRouteChannelView
	HandoverID       string             `json:"handover_id,omitempty"`
	ServerTimeMillis int64              `json:"server_time_unix_milli"`
	DraftsValid      bool               `json:"drafts_valid"`
	Review           []RouteDraftReview `json:"review"`
	Controls         RouteControls      `json:"controls"`
}

// One read transaction supplies the complete Node review, observation and
// historical receipts. These advisory controls do not authorize an operation:
// QueueRouteCommand repeats every guard in its own write transaction.
func (s *Store) MeteredRouteControlChannel(ctx context.Context, key []byte, node string) (MeteredRouteControlView, error) {
	out := MeteredRouteControlView{ServerTimeMillis: time.Now().UnixMilli(), Review: []RouteDraftReview{}}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	out.MeteredRouteChannelView, e = readMeteredRouteChannel(ctx, tx, key, node)
	if e != nil {
		return out, e
	}
	e = tx.QueryRowContext(ctx, `SELECT handover_id FROM native_metered_handover_nodes WHERE node_id=?`, node).Scan(&out.HandoverID)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT i.id,i.revision,i.name,t.listen_ip FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id WHERE t.node_id=? AND t.installation_id=? AND i.enabled=1 AND i.archived=0 ORDER BY i.id`, node, out.InstallationID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var r RouteDraftReview
		if e = rows.Scan(&r.InboundID, &r.InboundRevision, &r.Name, &r.ListenIP); e != nil {
			rows.Close()
			return out, e
		}
		out.Review = append(out.Review, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	refs := make([]RouteDraftRef, 0, len(out.Review))
	for i := range out.Review {
		r := &out.Review[i]
		d, err := readNativeRouteDraft(ctx, tx, key, r.InboundID, node)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err == nil {
			r.DraftRevision, r.DirectPort, r.TunnelPort = d.DraftRevision, d.DirectPort, d.TunnelPort
		}
		refs = append(refs, r.RouteDraftRef)
	}
	_, e = meteredRouteCandidate(ctx, tx, key, node, out.InstallationID, refs)
	if e != nil && !routeReviewBlocked(e) {
		return out, e
	}
	out.DraftsValid = e == nil
	var pending, count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN state IN('queued','delivered') THEN 1 ELSE 0 END),0) FROM native_metered_route_commands WHERE node_id=?`, node).Scan(&count, &pending); e != nil {
		return out, e
	}
	if out.Status.Usage == nil || !out.ObservationFresh || pending != 0 || count >= 10000 || !routeID.MatchString(out.Status.Instance) || out.Status.Serial >= routeMax || out.Status.Observed.Highwater >= routeMax {
		return out, tx.Commit()
	}
	if err := meteredLatestCondition(ctx, tx, node, out.InstallationID, *out.Status.Usage); err != nil {
		if routeReviewBlocked(err) {
			return out, tx.Commit()
		}
		return out, err
	}
	for _, c := range out.Commands {
		if c.Receipt != nil && c.Receipt.ObservedAt > out.ReportedAt {
			return out, tx.Commit() // Wait for an observation after the receipt.
		}
	}
	out.Controls.Stage = out.DraftsValid && out.HandoverID == ""
	if out.Controls.Stage && s.initialApplyHealthEnabled {
		if e = requireInitialPreparationForStage(ctx, tx, key, node); e != nil {
			if !routeReviewBlocked(e) {
				return out, e
			}
			out.Controls.Stage = false
		}
	}
	if out.Status.InterruptedCommandSHA != "" {
		return out, tx.Commit() // Recovery requires an explicit new Stage.
	}
	if e = meteredRouteEmptyNode(ctx, tx, node, out.InstallationID); e != nil {
		if routeReviewBlocked(e) {
			return out, tx.Commit()
		}
		return out, e
	}
	stage := func(id string, current bool) (meteredRouteRow, bool, error) {
		if id == "" {
			return meteredRouteRow{}, false, nil
		}
		r, err := readMeteredRouteCommand(ctx, tx, key, id)
		if err != nil {
			if routeReviewBlocked(err) {
				return r, false, nil
			}
			return r, false, err
		}
		v := r.view
		if v.NodeID != node || v.InstallationID != out.InstallationID || v.Action != "stage" || v.State != "acknowledged" || v.Receipt == nil || v.Receipt.Outcome != "staged" {
			return r, false, nil
		}
		if current {
			if err = meteredRouteStillCurrent(ctx, tx, key, r); err != nil {
				if routeReviewBlocked(err) {
					return r, false, nil
				}
				return r, false, err
			}
		}
		return r, true, nil
	}
	target := func(r meteredRouteRow) *RouteActionTarget {
		t := &RouteActionTarget{StageID: r.view.ID, Ports: []uint16{}}
		for _, l := range r.payload.Candidate.Listeners {
			t.Ports = append(t.Ports, l.NodePort)
		}
		return t
	}
	now := time.Now().UnixNano()
	observed := out.Status.Observed
	r, ok, e := stage(out.Status.StagedID, true)
	if e != nil {
		return out, e
	}
	if ok && r.payload.Command.Usage != nil && *r.payload.Command.Usage == *out.Status.Usage && r.payload.Command.Expected == mx.Condition(observed) && r.payload.Command.Expires > now {
		out.Controls.Apply = target(r)
		out.Controls.Apply.ExpiresMillis = r.payload.Command.Expires / int64(time.Millisecond)
	}
	r, ok, e = stage(out.Status.CurrentStageID, false)
	if e != nil {
		return out, e
	}
	if ok && out.Status.Usage.EpochID != "" && out.Status.Usage.Settlement.Sequence == 0 {
		out.Controls.Drain = target(r)
	}
	if out.HandoverID != "" {
		return out, tx.Commit()
	}
	if ok && observed.ProcessReady && observed.LeaseExpires > now && observed.ActiveSHA == r.payload.Candidate.SHA256 {
		current, err := meteredRouteCandidate(ctx, tx, key, node, out.InstallationID, refs)
		if err == nil && mx.TransactionPayloadSHA(current) == r.payload.Command.PayloadSHA {
			out.Controls.Renew = target(r)
		}
		if err != nil && !routeReviewBlocked(err) {
			return out, err
		}
	}
	// Previous stage identity is independent of the core's PreviousSHA, including
	// interrupted Apply recovery. Restore the saved current stage if no previous
	// stage is available and the Agent explicitly reports its process stopped.
	id := out.Status.PreviousStageID
	if id == "" && !observed.ProcessReady {
		id = out.Status.CurrentStageID
	}
	r, ok, e = stage(id, false)
	if e != nil {
		return out, e
	}
	if ok && (id == out.Status.PreviousStageID || (!observed.ProcessReady && observed.SavedSHA == r.payload.Candidate.SHA256)) {
		current, err := meteredRouteCandidate(ctx, tx, key, node, out.InstallationID, refs)
		if err == nil && mx.TransactionPayloadSHA(current) == r.payload.Command.PayloadSHA {
			out.Controls.Rollback = target(r)
		}
		if err != nil && !routeReviewBlocked(err) {
			return out, err
		}
	}
	return out, tx.Commit()
}
