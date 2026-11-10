package business

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"sort"
	"time"
)

// detachPlanNode removes destination and relay roles together. It does not
// invent a replacement relay, mutate a customer or infer an offline settlement.
func detachPlanNode(nodes []NativeInboundPlanNode, node, install string) ([]NativeInboundPlanNode, bool) {
	out := make([]NativeInboundPlanNode, 0, len(nodes))
	changed := false
	for _, n := range nodes {
		if n.NodeID == node && n.InstallationID == install {
			changed = true
			continue
		}
		if n.SwapEnabled && n.SwapSourceNodeID == node && n.SwapSourceInstallationID == install {
			n.SwapEnabled = false
			n.SwapSourceNodeID = ""
			n.SwapSourceInstallationID = ""
			n.SwapSourceListenIP = ""
			n.SwapSourcePort = 0
			n.SwapEntryAddress = ""
			n.SwapEntryPort = 0
			n.SwapDestinationPort = 0
			changed = true
		}
		out = append(out, n)
	}
	return out, changed
}

// DetachManagedNode is a local, atomic authoring/runtime intent change. The
// caller must durably fence deployment to this installation BEFORE calling it,
// and retain the fence across crashes/retries. Old credentials remain usable
// only for genuine accounting settlement; this method never seals an epoch.
// No response from the detached node is a condition of committing this intent.
func (s *Store) DetachManagedNode(ctx context.Context, key []byte, actor, event, node, install string) ([]uint64, error) {
	if !eventRE.MatchString(event) || !idRE.MatchString(node) || !validPlanInstallation(install) || node == "hub-local" {
		return nil, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = executionActor(ctx, tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT inbound_id FROM native_inbound_plans ORDER BY inbound_id`)
	if err != nil {
		return nil, err
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
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
	previousPlans := map[uint64]uint64{}
	changedIDs := map[uint64]bool{}
	now := time.Now().Unix()
	for _, id := range ids {
		p, e := readNativeInboundPlan(ctx, tx, key, id)
		if e != nil {
			return nil, e
		}
		nodes, changed := detachPlanNode(p.Nodes, node, install)
		if !changed {
			continue
		}
		previous := p.PlanRevision
		previousPlans[id] = previous
		p.PlanRevision++
		p.Nodes = nodes
		p.Status = nativePlanStatus(nodes)
		p.UpdatedUnix = now
		p.LastEventID = "node-detach-plan-" + ix.BytesSHA([]byte(fmt.Sprintf("%s/%d", event, id)))[:40]
		p.Reason = "Owner node lifecycle detach"
		enc, e := sealPlanValue(key, planAAD(id, p.PlanRevision), p)
		if e != nil {
			return nil, e
		}
		res, e := tx.ExecContext(ctx, `UPDATE native_inbound_plans SET revision=?,status=?,last_event_id=?,payload_enc=?,updated_unix=? WHERE inbound_id=? AND revision=?`, p.PlanRevision, p.Status, p.LastEventID, enc, now, id, previous)
		if e != nil {
			return nil, e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return nil, ErrConflict
		}
		receipt := NativeInboundPlanReceipt{Plan: p, EventID: p.LastEventID}
		sealed, e := sealPlanValue(key, planReceiptAAD(p.LastEventID, id, p.PlanRevision), receipt)
		if e != nil {
			return nil, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO native_inbound_plan_events(event_id,fingerprint,kind,inbound_id,revision,receipt_enc,created_unix) VALUES(?,?,'update',?,?,?,?)`, p.LastEventID, fingerprint("node-detach-plan", struct{ Event, Node, Install string }{event, node, install}), id, p.PlanRevision, sealed, now)
		if e != nil {
			return nil, e
		}
		changedIDs[id] = true
	}
	rows, err = tx.QueryContext(ctx, `SELECT inbound_id FROM inbound_execution_heads WHERE desired_revision>0 ORDER BY inbound_id`)
	if err != nil {
		return nil, err
	}
	headsIDs := []uint64{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		headsIDs = append(headsIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	nexts := map[uint64]executionSnapshot{}
	heads := map[uint64]executionHead{}
	for _, id := range headsIDs {
		h, e := readExecutionHead(ctx, tx, key, id)
		if e != nil {
			return nil, e
		}
		old, e := readExecutionSnapshot(ctx, tx, key, id, h.DesiredRevision)
		if e != nil {
			return nil, e
		}
		nodes, changed := detachPlanNode(old.Nodes, node, install)
		if !changed && h.Pending != "" {
			var count int
			e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_execution_operation_nodes WHERE operation_id=? AND node_id=? AND installation_id=?`, h.Pending, node, install).Scan(&count)
			if e != nil {
				return nil, e
			}
			changed = count > 0
		}
		if !changed {
			continue
		}
		old.Nodes = nodes
		old.Ref.Revision = h.Revision + 1
		if p, e := readNativeInboundPlan(ctx, tx, key, id); e == nil {
			if previous, ok := previousPlans[id]; ok && old.Ref.PlanRevision == previous {
				old.Ref.PlanRevision = p.PlanRevision
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if len(nodes) == 0 {
			old.Dormant = true
			old.EffectiveClientIDs = []string{}
			old.Principals = []ix.Principal{}
			old.Profile.Users = nil
		}
		nexts[id] = old
		heads[id] = h
		changedIDs[id] = true
	}
	runtimeIDs := []uint64{}
	for id := range nexts {
		runtimeIDs = append(runtimeIDs, id)
	}
	sort.Slice(runtimeIDs, func(i, j int) bool { return runtimeIDs[i] < runtimeIDs[j] })
	if len(runtimeIDs) > 0 {
		op := "node-detach-" + ix.BytesSHA([]byte(event + "/" + node + "/" + install))[:40]
		bundles, e := executionBundlesWithOverrides(ctx, tx, key, nexts[runtimeIDs[0]], op, nexts)
		if e != nil {
			return nil, e
		}
		retained := []ix.Bundle{}
		for _, b := range bundles {
			if b.NodeID == node && b.InstallationID == install {
				continue
			}
			// Preserve a healthy generation when the complete executable configuration,
			// accounting manifest and revision references are exactly unchanged. An
			// unrelated stopped inbound must not restart another inbound's process.
			old, e := readExecutionBundle(ctx, tx, key, b.NodeID, b.InstallationID, b.Generation-1)
			if e == nil {
				same := b
				same.Generation = old.Generation
				same.OperationID = old.OperationID
				same, e = ix.SealBundle(key, same)
				if e != nil {
					return nil, e
				}
				if ix.Digest(same) == ix.Digest(old) {
					b = old
				}
			} else if !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			retained = append(retained, b)
		}
		bundles = retained
		for _, id := range runtimeIDs {
			h := heads[id]
			next := nexts[id]
			action := "apply"
			if next.State == "stopped" {
				action = "stop"
			}
			if next.State == "archived" {
				action = "archive"
			}
			in := ix.CommandInput{EventID: op + fmt.Sprintf("-%d", id), Action: action, ExpectedRevision: h.Revision, ExpectedPlanRevision: next.Ref.PlanRevision, ExpectedInboundRevision: next.Ref.InboundRevision, ClientIDs: next.Bindings.ClientIDs, AccountingMode: next.Bindings.Mode, Reason: "Owner node lifecycle detach"}
			a := executionAssessment{head: h, snapshot: next, bundles: bundles, preview: ix.Preview{Valid: true, Targets: []ix.TargetState{}, Blockers: []ix.Blocker{}}}
			for _, b := range bundles {
				a.preview.Targets = append(a.preview.Targets, executionTarget(b))
			}
			in.ReviewSHA = fingerprint("node-detach-review", struct {
				Snapshot executionSnapshot
				Bundles  []ix.Bundle
			}{next, bundles})
			if _, e = commitExecutionAssessment(ctx, tx, key, actor, id, in, a, fingerprint("node-detach-command", in)); e != nil {
				return nil, e
			}
			// Saved client selection edits remain unapplied, just like profile drafts.
			if ix.Digest(h.Bindings) != ix.Digest(next.Bindings) {
				current, e := readExecutionHead(ctx, tx, key, id)
				if e != nil {
					return nil, e
				}
				current.Bindings = h.Bindings
				if e = writeExecutionHead(ctx, tx, key, current, current.Revision); e != nil {
					return nil, e
				}
			}

		}
		// Reused healthy observations and an empty target set can complete locally.
		// This updates administrative intent only; it does not settle any epoch.
		for _, id := range runtimeIDs {
			if e := completeExecutionOperation(ctx, tx, key, op+fmt.Sprintf("-%d", id)); e != nil {
				return nil, e
			}
		}
	}
	result := []uint64{}
	for id := range changedIDs {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, tx.Commit()
}