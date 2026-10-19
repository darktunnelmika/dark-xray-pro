package business

import (
	"context"
	"database/sql"
	"errors"
)

// A proposed Dark Swap is reviewed against *all* previously saved routes for
// this Inbound. This code NEVER connects to a Node or starts Xray.
func verifySwapDraftGraph(ctx context.Context, tx *sql.Tx, key []byte, in NativeRouteDraftInput) error {
	if !in.SwapEnabled {
		return nil
	}
	if len(key) != 32 {
		return ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT node_id FROM native_route_drafts
  WHERE inbound_id=? AND node_id!=? ORDER BY node_id LIMIT 257`, in.InboundID, in.NodeID)
	if err != nil {
		return err
	}
	nodes := make([]string, 0, 16)
	for rows.Next() {
		var node string
		if err = rows.Scan(&node); err != nil {
			rows.Close()
			return err
		}
		nodes = append(nodes, node)
		if len(nodes) > 256 {
			rows.Close()
			return ErrInvalid
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	edges := map[string]string{in.NodeID: in.SwapUpstreamNodeID}
	for _, node := range nodes {
		// readNativeRouteDraft authenticates every indexed port, node identity,
		// Inbound ID, revision and ciphertext before using an existing route.
		saved, e := readNativeRouteDraft(ctx, tx, key, in.InboundID, node)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return ErrUnavailable
			}
			return e
		}
		var install, listen string
		var direct, swap uint16
		e = tx.QueryRowContext(ctx, `SELECT installation_id,listen_ip,direct_port,swap_port
   FROM native_inbound_targets WHERE inbound_id=? AND node_id=?`,
			in.InboundID, node).Scan(&install, &listen, &direct, &swap)
		if e != nil || install != saved.InstallationID || listen != saved.ListenIP ||
			direct != saved.NativeDirectPort || swap != saved.NativeSwapPort {
			return ErrConflict
		}
		if saved.SwapEnabled {
			if !idRE.MatchString(saved.SwapUpstreamNodeID) || saved.SwapUpstreamNodeID == node {
				return ErrConflict
			}
			edges[node] = saved.SwapUpstreamNodeID
		}
	}
	// A pre-existing inconsistent component must not be hidden just because
	// the operator is editing a different Node. Fail closed on any graph cycle.
	for start := range edges {
		visited := make(map[string]bool, len(edges))
		next := start
		for hops := 0; hops <= len(edges); hops++ {
			if visited[next] {
				return ErrConflict
			}
			visited[next] = true
			up, ok := edges[next]
			if !ok || up == "" {
				break
			}
			next = up
			if hops == len(edges) {
				return ErrConflict
			}
		}
	}
	return nil
}
