package business

import (
	"context"
	"database/sql"
)

// NodeDependencies is an observation, NOT a retirement/replacement permission.
// Counts include historical and foreign-installation references to the node ID.
// Unknown counts are null, never an invented zero.
type NodeDependencies struct {
	Counts         map[string]*int64 `json:"counts"`
	UnknownReasons map[string]string `json:"unknown_reasons"`
}

func (s *Store) InspectNodeDependencies(ctx context.Context, key []byte, node, installation string) (NodeDependencies, error) {
	if !routeNodeID.MatchString(node) || !routeID.MatchString(installation) {
		return NodeDependencies{}, ErrInvalid
	}
	out := NodeDependencies{Counts: make(map[string]*int64), UnknownReasons: make(map[string]string)}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return NodeDependencies{}, err
	}
	defer tx.Rollback()
	queries := []struct{ name, query string }{
		{"inbound_targets", `SELECT count(*) FROM native_inbound_targets WHERE node_id=?`},
		{"active_inbound_targets", `SELECT count(*) FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id WHERE t.node_id=? AND i.enabled=1 AND i.archived=0`},
		{"assigned_clients", `SELECT count(DISTINCT c.client_id) FROM native_inbound_clients c JOIN native_inbound_targets t ON t.inbound_id=c.inbound_id WHERE t.node_id=?`},
		{"route_drafts", `SELECT count(*) FROM native_route_drafts WHERE node_id=?`},
		{"draft_events", `SELECT count(*) FROM native_route_draft_events WHERE node_id=?`},
		{"config_revisions", `SELECT count(*) FROM native_node_revisions WHERE node_id=?`},
		{"runtime_deliveries", `SELECT count(*) FROM native_runtime_delivery WHERE node_id=?`},
		{"runtime_reports", `SELECT count(*) FROM native_runtime_reports WHERE node_id=?`},
		{"route_channels", `SELECT count(*) FROM native_route_channels WHERE node_id=?`},
		{"route_commands", `SELECT count(*) FROM native_route_commands WHERE node_id=?`},
		{"pending_route_commands", `SELECT count(*) FROM native_route_commands WHERE node_id=? AND state IN ('queued','delivered')`},
		{"unknown_route_commands", `SELECT count(*) FROM native_route_commands WHERE node_id=? AND state='abandoned_unknown'`},
		{"metered_channels", `SELECT count(*) FROM native_metered_route_channels WHERE node_id=?`},
		{"metered_commands", `SELECT count(*) FROM native_metered_route_commands WHERE node_id=?`},
		{"pending_metered_commands", `SELECT count(*) FROM native_metered_route_commands WHERE node_id=? AND state IN ('queued','delivered')`},
		{"unknown_metered_commands", `SELECT count(*) FROM native_metered_route_commands WHERE node_id=? AND state='abandoned_unknown'`},
		{"metered_births", `SELECT count(*) FROM native_metered_route_births b JOIN native_metered_route_commands c ON c.id=b.command_id WHERE c.node_id=?`},
	}
	count := func(name, query string, args ...any) error {
		var n int64
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			return err
		}
		out.Counts[name] = &n
		return nil
	}
	for _, q := range queries {
		if err := count(q.name, q.query, node); err != nil {
			return NodeDependencies{}, err
		}
	}
	// This is a row count, not a count of distinct installations. Include old
	// installation rows rather than hiding them behind the current pairing.
	if err := count("foreign_installation_rows", `SELECT count(*) FROM (
 SELECT node_id,installation_id FROM native_inbound_targets UNION ALL
 SELECT node_id,installation_id FROM native_route_drafts UNION ALL
 SELECT node_id,installation_id FROM native_node_revisions UNION ALL
 SELECT node_id,installation_id FROM native_runtime_delivery UNION ALL
 SELECT node_id,installation_id FROM native_runtime_reports UNION ALL
 SELECT node_id,installation_id FROM native_route_channels UNION ALL
 SELECT node_id,installation_id FROM native_route_commands UNION ALL
 SELECT node_id,installation_id FROM native_metered_route_channels UNION ALL
 SELECT node_id,installation_id FROM native_metered_route_commands
 ) WHERE node_id=? AND installation_id<>?`, node, installation); err != nil {
		return NodeDependencies{}, err
	}
	usageQueries := []struct{ name, query string }{
		{"usage_epochs", `SELECT count(*) FROM pro_usage_epochs WHERE node_id=?`},
		{"open_usage_epochs", `SELECT count(*) FROM pro_usage_epochs WHERE node_id=? AND state='open'`},
		{"uncertain_usage_epochs", `SELECT count(*) FROM pro_usage_epochs WHERE node_id=? AND state='uncertain'`},
		{"sealed_usage_epochs", `SELECT count(*) FROM pro_usage_epochs WHERE node_id=? AND state='sealed'`},
		{"usage_holds", `SELECT count(*) FROM pro_usage_holds h JOIN pro_usage_epochs e ON e.epoch_id=h.epoch_id WHERE e.node_id=?`},
		{"usage_receipts", `SELECT count(*) FROM pro_usage_receipts r JOIN pro_usage_epochs e ON e.epoch_id=r.epoch_id WHERE e.node_id=?`},
		{"usage_deltas", `SELECT count(*) FROM pro_usage_deltas d JOIN pro_usage_epochs e ON e.epoch_id=d.epoch_id WHERE e.node_id=?`},
	}
	for _, q := range usageQueries {
		if s.usage == nil {
			out.Counts[q.name] = nil
			out.UnknownReasons[q.name] = "usage_ledger_disabled"
			continue
		}
		if err := count(q.name, q.query, node); err != nil {
			return NodeDependencies{}, err
		}
	}
	if s.usage == nil {
		out.Counts["foreign_usage_epochs"] = nil
		out.UnknownReasons["foreign_usage_epochs"] = "usage_ledger_disabled"
	} else if err := count("foreign_usage_epochs", `SELECT count(*) FROM pro_usage_epochs WHERE node_id=? AND installation_id<>?`, node, installation); err != nil {
		return NodeDependencies{}, err
	}
	// Upstream references live inside authenticated encrypted draft payloads.
	// Indexed ports alone cannot prove that no other node depends on this node.
	out.Counts["swap_upstream_references"] = nil
	if len(key) != 32 {
		out.UnknownReasons["swap_upstream_references"] = "inbound_key_unavailable"
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT inbound_id,node_id FROM native_route_drafts ORDER BY inbound_id,node_id LIMIT 4097`)
		if err != nil {
			return NodeDependencies{}, err
		}
		type ref struct {
			id   uint64
			node string
		}
		refs := make([]ref, 0)
		for rows.Next() {
			var x ref
			if err := rows.Scan(&x.id, &x.node); err != nil {
				rows.Close()
				return NodeDependencies{}, err
			}
			refs = append(refs, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return NodeDependencies{}, err
		}
		if len(refs) > 4096 {
			out.UnknownReasons["swap_upstream_references"] = "draft_scan_limit"
		} else {
			var n int64
			for _, x := range refs {
				draft, err := readNativeRouteDraft(ctx, tx, key, x.id, x.node)
				if err != nil {
					return NodeDependencies{}, ErrUnavailable
				}
				if draft.SwapEnabled && draft.SwapUpstreamNodeID == node {
					n++
				}
			}
			out.Counts["swap_upstream_references"] = &n
		}
	}
	inspectCommandDependencies(ctx, tx, key, node, &out)
	if err := tx.Commit(); err != nil {
		return NodeDependencies{}, err
	}
	return out, nil
}
