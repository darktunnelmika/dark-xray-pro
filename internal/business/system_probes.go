package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Explicit startup-only opt-in. No HTTP endpoint or command-line flag invokes
// this migration yet, and creating an identity grants no execution authority.
func (s *Store) EnableSystemProbeAccounting(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	return s.usage.EnableProbeAccounting(ctx)
}

// ProvisionMeteredProbe is private topology provisioning before a fresh stage.
// Existing identities are retained; pending commands, handovers or unsettled
// epochs block creation of a new identity. Only claimed loopback Direct TCP
// channels qualify; this never changes ordinary/native or legacy configs.
func (s *Store) ProvisionMeteredProbe(ctx context.Context, key []byte, node, installation string, inbound uint64) (usageledger.ProbeIdentity, error) {
	var empty usageledger.ProbeIdentity
	if s.usage == nil || len(key) != 32 {
		return empty, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	p, err := s.provisionMeteredProbeTx(ctx, tx, key, node, installation, inbound)
	if err != nil {
		return empty, err
	}
	return p, tx.Commit()
}

// The caller owns the writer transaction. Preparation and probe identity must
// commit together, so a failed reviewed preparation cannot leave a credential.
func (s *Store) provisionMeteredProbeTx(ctx context.Context, tx *sql.Tx, key []byte, node, installation string, inbound uint64) (usageledger.ProbeIdentity, error) {
	var empty usageledger.ProbeIdentity
	prior, err := usageledger.ProbeIdentityTx(ctx, tx, node, installation, inbound)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, usageledger.ErrMissing) {
		return empty, err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_metered_route_channels c JOIN native_inbound_targets t ON t.node_id=c.node_id AND t.installation_id=c.installation_id JOIN native_inbounds i ON i.id=t.inbound_id WHERE c.node_id=? AND c.installation_id=? AND t.inbound_id=? AND t.listen_ip='127.0.0.1' AND i.enabled=1 AND i.archived=0`, node, installation, inbound).Scan(&n); err != nil {
		return empty, err
	}
	if n != 1 {
		return empty, ErrConflict
	}
	var encrypted []byte
	if err = tx.QueryRowContext(ctx, `SELECT profile_enc FROM native_inbounds WHERE id=?`, inbound).Scan(&encrypted); err != nil {
		return empty, err
	}
	profile, err := openProfile(key, encrypted, inbound)
	if err != nil {
		return empty, err
	}
	if profile.Transport != "tcp" {
		return empty, ErrConflict
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM native_metered_route_commands WHERE node_id=? AND state IN('queued','delivered')`,
		`SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=? AND state!='sealed'`,
		`SELECT COUNT(*) FROM native_metered_handover_nodes WHERE node_id=?`,
	} {
		if err = tx.QueryRowContext(ctx, query, node).Scan(&n); err != nil {
			return empty, err
		}
		if n != 0 {
			return empty, ErrConflict
		}
	}
	uuid, err := randomUUID()
	if err != nil {
		return empty, err
	}
	p := usageledger.ProbeIdentity{ID: "probe-" + strings.ReplaceAll(uuid, "-", ""), UUID: uuid, NodeID: node, InstallationID: installation, InboundID: inbound, EventID: "system-probe-" + strings.ReplaceAll(uuid, "-", "")}
	if err = s.usage.CreateProbeIdentityTx(ctx, tx, p); err != nil {
		return empty, err
	}
	return p, nil
}
