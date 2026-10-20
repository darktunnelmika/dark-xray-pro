package business

import (
	"context"
	"database/sql"
)

// Historical command inspection is deliberately bounded and keeps only compact
// metadata in memory. Legacy encrypted payloads have no logical-node map.
func inspectCommandDependencies(ctx context.Context, tx *sql.Tx, key []byte, node string, out *NodeDependencies) {
	fields := []string{"historical_stage_references", "historical_upstream_command_references", "unmapped_historical_commands"}
	unknown := func(reason string) {
		for _, f := range fields {
			out.Counts[f] = nil
			out.UnknownReasons[f] = reason
		}
	}
	if len(key) != 32 {
		unknown("inbound_key_unavailable")
		return
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,id,length(payload_enc) FROM (
 SELECT 'route' AS kind,id,payload_enc FROM native_route_commands UNION ALL
 SELECT 'metered' AS kind,id,payload_enc FROM native_metered_route_commands
 ) ORDER BY kind,id LIMIT 513`)
	if err != nil {
		unknown("command_history_unverifiable")
		return
	}
	type ref struct {
		kind, id string
		size     int64
	}
	refs := make([]ref, 0)
	for rows.Next() {
		var r ref
		if err = rows.Scan(&r.kind, &r.id, &r.size); err != nil {
			break
		}
		refs = append(refs, r)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		unknown("command_history_unverifiable")
		return
	}
	if len(refs) > 512 {
		unknown("command_history_limit")
		return
	}
	type entry struct {
		owner, install, action, stage string
		nodes                         []string
	}
	entries := make(map[string]entry, len(refs))
	var unmapped, upstream int64
	for _, r := range refs {
		if r.size > 8<<20 {
			unknown("command_history_limit")
			return
		}
		var e entry
		if r.kind == "route" {
			v, err := readRouteCommand(ctx, tx, key, r.id)
			if err != nil {
				unknown("command_history_unverifiable")
				return
			}
			e = entry{v.view.NodeID, v.view.InstallationID, v.view.Action, v.view.StageID, v.payload.DependencyNodes}
		} else {
			v, err := readMeteredRouteCommand(ctx, tx, key, r.id)
			if err != nil {
				unknown("command_history_unverifiable")
				return
			}
			e = entry{v.view.NodeID, v.view.InstallationID, v.view.Action, v.view.StageID, v.payload.DependencyNodes}
		}
		if !routeNodeID.MatchString(e.owner) || !routeID.MatchString(e.install) || !routeID.MatchString(e.stage) {
			unknown("command_history_unverifiable")
			return
		}
		if e.action != "stage" && e.action != "apply" && e.action != "rollback" && e.action != "renew" {
			unknown("command_history_unverifiable")
			return
		}
		if len(e.nodes) == 0 {
			unmapped++
		} else {
			seen := make(map[string]bool)
			if len(e.nodes) > 65 {
				unknown("command_history_unverifiable")
				return
			}
			for _, n := range e.nodes {
				if !routeNodeID.MatchString(n) || seen[n] {
					unknown("command_history_unverifiable")
					return
				}
				seen[n] = true
			}
			if !seen[e.owner] {
				unknown("command_history_unverifiable")
				return
			}
			if e.owner != node && seen[node] {
				upstream++
			}
		}
		entries[r.kind+":"+r.id] = e
	}
	stages := make(map[string]bool)
	for _, r := range refs {
		e := entries[r.kind+":"+r.id]
		s, ok := entries[r.kind+":"+e.stage]
		if !ok || s.action != "stage" || s.stage != e.stage || s.owner != e.owner || s.install != e.install {
			unknown("command_history_unverifiable")
			return
		}
		// Apply/rollback/renew must preserve the map of their source Stage.
		if len(e.nodes) != len(s.nodes) {
			unknown("command_history_unverifiable")
			return
		}
		for i, n := range e.nodes {
			if n != s.nodes[i] {
				unknown("command_history_unverifiable")
				return
			}
		}
		if e.owner == node {
			stages[r.kind+":"+e.stage] = true
		}
	}
	n := int64(len(stages))
	out.Counts[fields[0]] = &n
	out.Counts[fields[2]] = &unmapped
	if unmapped > 0 {
		out.Counts[fields[1]] = nil
		out.UnknownReasons[fields[1]] = "historical_node_map_missing"
	} else {
		out.Counts[fields[1]] = &upstream
	}
}
