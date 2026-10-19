package business

import (
	"context"
	"database/sql"
	"errors"
)

// NativeInboundTarget is a strictly read-only lookup of one NEW native
// Inbound/Node attachment. It never creates Node authority or modifies a route.
func (s *Store) NativeInboundTarget(ctx context.Context, inboundID uint64, nodeID string) (InboundTarget, error) {
	var target InboundTarget
	if inboundID == 0 || inboundID > (1<<53-1) || !idRE.MatchString(nodeID) {
		return target, ErrInvalid
	}
	err := s.db.QueryRowContext(ctx, `SELECT inbound_id,node_id,installation_id,listen_ip,direct_port,swap_port
  FROM native_inbound_targets WHERE inbound_id=? AND node_id=?`, inboundID, nodeID).
		Scan(&target.InboundID, &target.NodeID, &target.InstallationID, &target.ListenIP, &target.DirectPort, &target.SwapPort)
	if errors.Is(err, sql.ErrNoRows) {
		return InboundTarget{}, ErrNotFound
	}
	if err != nil {
		return InboundTarget{}, err
	}
	return target, nil
}
