package business

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// RefreshInboundExecutionEligibility reconciles the complete connected node
// component atomically. It uses reviewed snapshots, never unsent authoring
// edits. Each changed inbound has its own immutable event and CAS revision;
// all share one complete node bundle and one publication barrier.
func (s *Store) RefreshInboundExecutionEligibility(ctx context.Context, key []byte, node, install string) (bool, error) {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var stored string
	if err = tx.QueryRowContext(ctx, `SELECT installation_id FROM inbound_execution_nodes WHERE node_id=?`, node).Scan(&stored); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if stored != install {
		return false, ErrConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT inbound_id,desired_revision FROM inbound_execution_heads WHERE desired_state='active' AND desired_revision>0 ORDER BY inbound_id`)
	if err != nil {
		return false, err
	}
	type ref struct{ id, rev uint64 }
	refs := []ref{}
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.id, &r.rev); err != nil {
			rows.Close()
			return false, err
		}
		refs = append(refs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	snapshots := map[uint64]executionSnapshot{}
	heads := map[uint64]executionHead{}
	for _, r := range refs {
		old, e := readExecutionSnapshot(ctx, tx, key, r.id, r.rev)
		if e != nil {
			return false, e
		}
		snapshots[r.id] = old
		h, e := readExecutionHead(ctx, tx, key, r.id)
		if e != nil {
			return false, e
		}
		heads[r.id] = h
	}
	connected := map[string]bool{node: true}
	members := map[uint64]bool{}
	for progress := true; progress; {
		progress = false
		for id, snapshot := range snapshots {
			if members[id] {
				continue
			}
			nodes := executionSnapshotNodes(snapshot)
			overlap := false
			for n := range nodes {
				overlap = overlap || connected[n]
			}
			if overlap {
				members[id] = true
				progress = true
				for n := range nodes {
					connected[n] = true
				}
			}
		}
	}
	nexts := map[uint64]executionSnapshot{}
	changed := false
	for id := range members {
		old := snapshots[id]
		next, _, e := populateExecutionUsers(ctx, tx, old, true)
		if e != nil {
			return false, e
		}
		different := ix.Digest(next.EffectiveClientIDs) != ix.Digest(old.EffectiveClientIDs) || next.ExpiresUnix != old.ExpiresUnix || next.Dormant != old.Dormant
		changed = changed || different
		if different || heads[id].Pending != "" {
			next.Ref.Revision = heads[id].Revision + 1
			nexts[id] = next
		}
	}
	if !changed {
		return false, tx.Commit()
	}
	ids := make([]uint64, 0, len(nexts))
	for id := range nexts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	inputs := map[uint64]ix.CommandInput{}
	for _, id := range ids {
		h := heads[id]
		snapshot := nexts[id]
		event := "eligibility-" + ix.BytesSHA([]byte(fmt.Sprintf("%d/%d/%s", id, h.Revision, ix.Digest(snapshot))))[:48]
		inputs[id] = ix.CommandInput{EventID: event, Action: "apply", ExpectedRevision: h.Revision, ExpectedPlanRevision: snapshot.Ref.PlanRevision, ExpectedInboundRevision: snapshot.Ref.InboundRevision, ClientIDs: snapshot.Bindings.ClientIDs, AccountingMode: snapshot.Bindings.Mode, Reason: "Current entitlement reconciliation"}
	}
	bundles, err := executionBundlesWithOverrides(ctx, tx, key, nexts[ids[0]], inputs[ids[0]].EventID, nexts)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		h := heads[id]
		next := nexts[id]
		in := inputs[id]
		a := executionAssessment{head: h, snapshot: next, bundles: bundles, preview: ix.Preview{Valid: true, Targets: []ix.TargetState{}, Blockers: []ix.Blocker{}}}
		for _, b := range bundles {
			a.preview.Targets = append(a.preview.Targets, executionTarget(b))
		}
		in.ReviewSHA = fingerprint("managed-inbound-entitlement-v1", struct {
			Snapshot executionSnapshot
			Bundles  []ix.Bundle
		}{next, bundles})
		fp := fingerprint("managed-inbound-entitlement-command-v1", in)
		if err = executionEventAvailable(ctx, tx, in.EventID); err != nil {
			return false, err
		}
		if _, err = commitExecutionAssessment(ctx, tx, key, "system", id, in, a, fp); err != nil {
			return false, err
		}
		// A separately saved binding edit is not automatically applied.
		if ix.Digest(h.Bindings) != ix.Digest(snapshots[id].Bindings) {
			updated, e := readExecutionHead(ctx, tx, key, id)
			if e != nil {
				return false, e
			}
			updated.Bindings = h.Bindings
			if e = writeExecutionHead(ctx, tx, key, updated, updated.Revision); e != nil {
				return false, e
			}
		}
	}
	return true, tx.Commit()
}
