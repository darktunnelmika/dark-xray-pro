package business

import (
	"context"
	"database/sql"
	"errors"
)

// These additive tables do not change the ordinary usage schema. A deployment
// rollback must refuse an older writer once a reservation or promotion exists.
// The original plan and all creation receipts remain immutable authoring history.
func ensureNativeInboundPromotions(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`CREATE TABLE IF NOT EXISTS native_inbound_plan_promotion_nodes(node_id TEXT PRIMARY KEY REFERENCES native_metered_route_channels(node_id),installation_id TEXT NOT NULL,binary_sha TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS native_inbound_plan_promotions(
 inbound_id INTEGER PRIMARY KEY REFERENCES native_inbound_plans(inbound_id),event_id TEXT NOT NULL UNIQUE,fingerprint TEXT NOT NULL,
 plan_revision INTEGER NOT NULL CHECK(plan_revision>=1),source_inbound_revision INTEGER NOT NULL CHECK(source_inbound_revision>=1),
 inbound_revision INTEGER NOT NULL CHECK(inbound_revision=source_inbound_revision+1 AND inbound_revision<=9007199254740991),
 node_id TEXT NOT NULL UNIQUE REFERENCES native_inbound_plan_promotion_nodes(node_id),installation_id TEXT NOT NULL,
 direct_port INTEGER NOT NULL CHECK(direct_port>=1024 AND direct_port<=65535 AND direct_port NOT IN(2087,8443,24443)),
 draft_event_id TEXT NOT NULL UNIQUE,created_unix INTEGER NOT NULL,input_enc BLOB NOT NULL,receipt_enc BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS native_inbound_plan_promotion_clients(inbound_id INTEGER NOT NULL REFERENCES native_inbound_plan_promotions(inbound_id),client_id TEXT NOT NULL REFERENCES clients(id),PRIMARY KEY(inbound_id,client_id))`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_reservation_no_update BEFORE UPDATE ON native_inbound_plan_promotion_nodes BEGIN SELECT RAISE(ABORT,'promotion reservation immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_reservation_no_delete BEFORE DELETE ON native_inbound_plan_promotion_nodes BEGIN SELECT RAISE(ABORT,'promotion reservation immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_no_update BEFORE UPDATE ON native_inbound_plan_promotions BEGIN SELECT RAISE(ABORT,'promotion receipt immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_no_delete BEFORE DELETE ON native_inbound_plan_promotions BEGIN SELECT RAISE(ABORT,'promotion receipt immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_clients_no_update BEFORE UPDATE ON native_inbound_plan_promotion_clients BEGIN SELECT RAISE(ABORT,'promoted binding intent immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_clients_no_delete BEFORE DELETE ON native_inbound_plan_promotion_clients BEGIN SELECT RAISE(ABORT,'promoted binding intent immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_clients_once BEFORE INSERT ON native_inbound_plan_promotion_clients
 WHEN EXISTS(SELECT 1 FROM native_inbounds WHERE id=NEW.inbound_id AND enabled<>0)
 BEGIN SELECT RAISE(ABORT,'promoted binding intent closed'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_plan_no_update BEFORE UPDATE ON native_inbound_plans
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id OR inbound_id=NEW.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted authoring history immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_plan_no_delete BEFORE DELETE ON native_inbound_plans
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted authoring history immutable'); END`,
		`DROP TRIGGER IF EXISTS native_plan_block_enable`,
		`CREATE TRIGGER native_plan_block_enable BEFORE UPDATE OF enabled ON native_inbounds
 WHEN NEW.enabled<>0 AND EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_plan_promotions p WHERE p.inbound_id=OLD.id AND p.source_inbound_revision=OLD.revision AND p.inbound_revision=NEW.revision AND OLD.enabled=0 AND NEW.enabled=1 AND OLD.archived=0 AND NEW.archived=0)
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_profile_immutable BEFORE UPDATE ON native_inbounds
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.id OR inbound_id=NEW.id)
 AND NOT(NEW.id=OLD.id AND NEW.name=OLD.name AND NEW.profile_enc=OLD.profile_enc AND NEW.public_key=OLD.public_key AND NEW.created_unix=OLD.created_unix AND OLD.enabled=0 AND NEW.enabled=1 AND OLD.archived=0 AND NEW.archived=0
 AND EXISTS(SELECT 1 FROM native_inbound_plan_promotions p WHERE p.inbound_id=OLD.id AND p.source_inbound_revision=OLD.revision AND p.inbound_revision=NEW.revision))
 BEGIN SELECT RAISE(ABORT,'promoted profile immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_profile_no_delete BEFORE DELETE ON native_inbounds
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.id)
 BEGIN SELECT RAISE(ABORT,'promoted profile immutable'); END`,
		`DROP TRIGGER IF EXISTS native_plan_block_target_insert`,
		`CREATE TRIGGER native_plan_block_target_insert BEFORE INSERT ON native_inbound_targets
 WHEN EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.inbound_id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_plan_promotions p JOIN native_inbounds i ON i.id=p.inbound_id
 WHERE p.inbound_id=NEW.inbound_id AND p.node_id=NEW.node_id AND p.installation_id=NEW.installation_id AND NEW.listen_ip='127.0.0.1' AND p.direct_port=NEW.direct_port AND NEW.swap_port=0 AND i.revision=p.inbound_revision AND i.enabled=1 AND i.archived=0
 AND NOT EXISTS(SELECT 1 FROM native_inbound_targets t WHERE t.inbound_id=p.inbound_id))
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_target_no_delete BEFORE DELETE ON native_inbound_targets
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted target immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_reserved_target_insert BEFORE INSERT ON native_inbound_targets
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotion_nodes WHERE node_id=NEW.node_id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_plan_promotions p WHERE p.inbound_id=NEW.inbound_id AND p.node_id=NEW.node_id AND p.installation_id=NEW.installation_id AND NEW.listen_ip='127.0.0.1' AND p.direct_port=NEW.direct_port AND NEW.swap_port=0
 AND NOT EXISTS(SELECT 1 FROM native_inbound_targets WHERE node_id=NEW.node_id))
 BEGIN SELECT RAISE(ABORT,'reserved Node requires complete plan promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_reserved_target_update BEFORE UPDATE ON native_inbound_targets
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotion_nodes WHERE node_id=NEW.node_id OR node_id=OLD.node_id)
 BEGIN SELECT RAISE(ABORT,'promotion Node assignments immutable'); END`,
		`DROP TRIGGER IF EXISTS native_plan_block_client_bind`,
		`CREATE TRIGGER native_plan_block_client_bind BEFORE INSERT ON native_inbound_clients
 WHEN EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.inbound_id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_plan_promotions p JOIN native_inbound_plan_promotion_clients c ON c.inbound_id=p.inbound_id JOIN native_inbounds i ON i.id=p.inbound_id
 WHERE p.inbound_id=NEW.inbound_id AND c.client_id=NEW.client_id AND i.revision=p.inbound_revision AND i.enabled=1 AND i.archived=0
 AND NOT EXISTS(SELECT 1 FROM native_inbound_clients b WHERE b.inbound_id=NEW.inbound_id AND b.client_id=NEW.client_id))
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_binding_no_update BEFORE UPDATE ON native_inbound_clients
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id OR inbound_id=NEW.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted binding immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_binding_no_delete BEFORE DELETE ON native_inbound_clients
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted binding immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_draft_once BEFORE INSERT ON native_route_drafts
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=NEW.inbound_id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_plan_promotions p WHERE p.inbound_id=NEW.inbound_id AND p.node_id=NEW.node_id AND p.installation_id=NEW.installation_id AND NEW.revision=1 AND NEW.inbound_revision=p.inbound_revision AND NEW.listen_ip='127.0.0.1' AND NEW.original_direct_port=p.direct_port AND NEW.original_swap_port=0 AND NEW.direct_port=p.direct_port AND NEW.tunnel_port=0 AND NEW.swap_node_port=0 AND NEW.last_event_id=p.draft_event_id
 AND NOT EXISTS(SELECT 1 FROM native_route_drafts d WHERE d.inbound_id=NEW.inbound_id))
 BEGIN SELECT RAISE(ABORT,'promoted draft immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_draft_no_update BEFORE UPDATE ON native_route_drafts
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id OR inbound_id=NEW.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted draft immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_promotion_draft_no_delete BEFORE DELETE ON native_route_drafts
 WHEN EXISTS(SELECT 1 FROM native_inbound_plan_promotions WHERE inbound_id=OLD.inbound_id)
 BEGIN SELECT RAISE(ABORT,'promoted draft immutable'); END`,
	} {
		if _, err = tx.Exec(query); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// This is called only by the explicit startup opt-in, never by a browser
// mutation. It reserves the existing immutable metered executor identity while
// permitting an entirely empty Node. Candidate/Stage/Apply still require the
// original nonempty meteredRouteEmptyNode guard after atomic promotion.
func (s *Store) ReserveMeteredRouteChannelsForPlanPromotion(ctx context.Context, nodes []MeteredRouteNode) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	if len(nodes) != 1 {
		return ErrInvalid
	}
	node := nodes[0]
	if !routeNodeID.MatchString(node.NodeID) || !routeID.MatchString(node.InstallationID) || !routeSHA.MatchString(node.BinarySHA) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var installation, binary string
	err = tx.QueryRowContext(ctx, `SELECT installation_id,binary_sha FROM native_inbound_plan_promotion_nodes WHERE node_id=?`, node.NodeID).Scan(&installation, &binary)
	if err == nil {
		if installation != node.InstallationID || binary != node.BinarySHA {
			return ErrConflict
		}
		if err = tx.QueryRowContext(ctx, `SELECT installation_id,binary_sha FROM native_metered_route_channels WHERE node_id=?`, node.NodeID).Scan(&installation, &binary); err != nil {
			return err
		}
		if installation != node.InstallationID || binary != node.BinarySHA {
			return ErrUnavailable
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM native_metered_route_channels WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_route_channels WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_inbound_targets WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_route_drafts WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_runtime_delivery WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_node_revisions WHERE node_id=?`,
		`SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=?`,
	} {
		var count int
		if err = tx.QueryRowContext(ctx, query, node.NodeID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_metered_route_channels(node_id,installation_id,binary_sha) VALUES(?,?,?)`, node.NodeID, node.InstallationID, node.BinarySHA); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_plan_promotion_nodes(node_id,installation_id,binary_sha) VALUES(?,?,?)`, node.NodeID, node.InstallationID, node.BinarySHA); err != nil {
		return err
	}
	return tx.Commit()
}
