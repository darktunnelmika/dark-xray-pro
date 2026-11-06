package business

import (
	"context"
	"database/sql"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// executionPublicationAuthority authorizes a previously applied inbound in the
// current authorized bundle. It is read-only and does not grant an execution
// lease. Runtime admission continues to use executionBundleAuthority, including
// its independent stale-accounting guard. Publication retains all current
// identity, intent, explicit-resume and entitlement checks below.
func executionPublicationAuthority(ctx context.Context, tx *sql.Tx, key []byte, b ix.Bundle, inbound, revision uint64) error {
	var generation uint64
	var install string
	if err := tx.QueryRowContext(ctx, `SELECT installation_id,desired_generation FROM inbound_execution_nodes WHERE node_id=?`, b.NodeID).Scan(&install, &generation); err != nil {
		return err
	}
	if install != b.InstallationID || generation != b.Generation {
		return ErrConflict
	}

	// Anchor the credential to this inbound's own committed application, not
	// the origin operation of a later shared-node bundle. Another inbound may
	// still be waiting on an unrelated offline target while this route remains
	// unchanged. A new revision of this inbound must itself finish acceptance.
	var committed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_execution_heads h
        JOIN inbound_execution_operations o ON o.id=h.active_operation AND o.inbound_id=h.inbound_id
        JOIN inbound_execution_operation_nodes n ON n.operation_id=o.id
        WHERE h.inbound_id=? AND h.active_revision=? AND h.active_revision=h.desired_revision
          AND h.active_revision=o.revision AND h.desired_state='active' AND h.observed_state='active'
          AND h.pending_operation='' AND n.node_id=? AND n.installation_id=?
          AND n.generation>0 AND n.generation<=?`,
		inbound, revision, b.NodeID, b.InstallationID, b.Generation).Scan(&committed); err != nil {
		return err
	}
	if committed != 1 {
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
	}
	present := false
	for _, r := range b.Revisions {
		if r.InboundID == inbound && r.Revision == revision {
			present = true
		}
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
		}
	}
	if !present {
		return ErrConflict
	}
	return nil
}
