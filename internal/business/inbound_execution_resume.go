package business

import (
	"context"
	"database/sql"
	"errors"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func executionResumeRequired(ctx context.Context, tx *sql.Tx, node, install string) (bool, error) {
	var required bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbound_execution_resume_scopes s LEFT JOIN inbound_execution_resume_events r ON r.epoch_id=s.epoch_id AND r.node_id=s.node_id AND r.installation_id=s.installation_id WHERE s.node_id=? AND s.installation_id=? AND r.epoch_id IS NULL)`, node, install).Scan(&required)
	return required, err
}

func blockExecutionResume(ctx context.Context, tx *sql.Tx, key []byte, epoch ix.EpochInput, now int64) error {
	b, err := readExecutionBundle(ctx, tx, key, epoch.NodeID, epoch.InstallationID, epoch.Generation)
	if err != nil {
		return err
	}
	nodes := map[string]string{epoch.NodeID: epoch.InstallationID}
	for _, r := range b.Revisions {
		snapshot, err := readExecutionSnapshot(ctx, tx, key, r.InboundID, r.Revision)
		if err != nil {
			return err
		}
		for node, install := range executionSnapshotNodes(snapshot) {
			if old := nodes[node]; old != "" && old != install {
				return ErrConflict
			}
			nodes[node] = install
		}
	}
	for node, install := range nodes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_resume_scopes(epoch_id,node_id,installation_id,created) VALUES(?,?,?,?)`, epoch.EpochID, node, install, now); err != nil {
			return err
		}
	}
	return nil
}

// Only the reviewed owner command entry point calls this. Entitlement refresh,
// reconciliation, stop, and archive never authorize another process admission.
func resolveExecutionResume(ctx context.Context, tx *sql.Tx, operation string, bundles []ix.Bundle) error {
	for _, b := range bundles {
		if len(b.Listeners) == 0 && len(b.Components) == 0 {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO inbound_execution_resume_events(epoch_id,node_id,installation_id,operation_id,generation,created) SELECT s.epoch_id,s.node_id,s.installation_id,?,?,? FROM inbound_execution_resume_scopes s LEFT JOIN inbound_execution_resume_events r ON r.epoch_id=s.epoch_id AND r.node_id=s.node_id AND r.installation_id=s.installation_id WHERE s.node_id=? AND s.installation_id=? AND r.epoch_id IS NULL`, operation, b.Generation, time.Now().UnixNano(), b.NodeID, b.InstallationID)
		if err != nil {
			return err
		}
	}
	return nil
}

// InboundExecutionResume identifies the explicit reviewed owner operation
// required for a worker to leave a reconciled journal. It is delivered only in
// a fresh, exact-bundle signed lease after current authority is checked.
func (s *Store) InboundExecutionResume(ctx context.Context, key []byte, node, install, epoch string, generation uint64) (string, bool, error) {
	if !idRE.MatchString(node) || !routeID.MatchString(install) || !idRE.MatchString(epoch) || generation == 0 {
		return "", false, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return "", false, err
	}
	if err = executionBundleAuthority(ctx, tx, key, b); err != nil {
		return "", false, err
	}
	var event, actor, action string
	var reviewedGeneration uint64
	err = tx.QueryRowContext(ctx, `SELECT r.operation_id,r.generation,e.actor_id,o.action FROM inbound_execution_resume_events r JOIN inbound_execution_events e ON e.event_id=r.operation_id JOIN inbound_execution_operations o ON o.id=r.operation_id JOIN inbound_execution_reconciliations c ON c.epoch_id=r.epoch_id WHERE r.epoch_id=? AND r.node_id=? AND r.installation_id=?`, epoch, node, install).Scan(&event, &reviewedGeneration, &actor, &action)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, tx.Commit()
	}
	if err != nil {
		return "", false, err
	}
	if actor == "system" || reviewedGeneration > generation || action != "activate" && action != "apply" && action != "restart" {
		return "", false, ErrUnavailable
	}
	return event, true, tx.Commit()
}
