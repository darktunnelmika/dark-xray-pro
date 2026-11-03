package business

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// ProvisionDarkRestoreQuarantineProbeV1 is an owner-only, pre-birth
// preparation step. It creates or reuses one immutable nonbillable probe for
// the exact quarantine node/inbound and returns the newly pinned plan. It does
// not register an epoch, open a listener, issue a challenge or activate a
// subscription.
func (s *Store) ProvisionDarkRestoreQuarantineProbeV1(ctx context.Context, key []byte, token, client string) (DarkRestoreQuarantinePlan, error) {
	var zero DarkRestoreQuarantinePlan
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	plan, err := quarantinePlanTx(ctx, tx, key, actor, client)
	if err != nil {
		return zero, err
	}
	if plan.SystemProbeID != "" {
		if !routeSHA.MatchString(plan.SystemProbeCredentialSHA) || !routeSHA.MatchString(plan.PreparedAdmissionSHA) {
			return zero, ErrDarkRestoreCutover
		}
		return plan, tx.Commit()
	}
	present, err := birthSchema(ctx, tx)
	if err != nil {
		return zero, err
	}
	if present {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM darkrestore_native_birth_v1 WHERE client_id=?", client).Scan(&n); err != nil {
			return zero, err
		}
		if n != 0 {
			return zero, ErrConflict
		}
	}
	probe, err := s.provisionDarkRestoreQuarantineProbeTx(ctx, tx, plan)
	if err != nil {
		return zero, fmt.Errorf("dark restore probe provision: %w", err)
	}
	updated, err := quarantinePlanTx(ctx, tx, key, actor, client)
	if err != nil {
		return zero, fmt.Errorf("dark restore probe rederive: %w", err)
	}
	if updated.SystemProbeID != probe.ID || !routeSHA.MatchString(updated.SystemProbeCredentialSHA) ||
		!routeSHA.MatchString(updated.PreparedAdmissionSHA) || updated.ConfigSHA == plan.ConfigSHA ||
		updated.ManifestSHA == "" {
		return zero, ErrDarkRestoreCutover
	}
	// Provisioning is idempotent only for the exact immutable probe identity.
	again, err := usageledger.ProbeIdentityTx(ctx, tx, updated.NodeID, updated.InstallationID, updated.Prepared.InboundID)
	if errors.Is(err, usageledger.ErrMissing) {
		return zero, ErrDarkRestoreCutover
	}
	if err != nil {
		return zero, err
	}
	if again != probe {
		return zero, ErrConflict
	}
	return updated, tx.Commit()
}

func (s *Store) provisionDarkRestoreQuarantineProbeTx(ctx context.Context, tx *sql.Tx, plan DarkRestoreQuarantinePlan) (usageledger.ProbeIdentity, error) {
	var zero usageledger.ProbeIdentity
	prior, err := usageledger.ProbeIdentityTx(ctx, tx, plan.NodeID, plan.InstallationID, plan.Prepared.InboundID)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, usageledger.ErrMissing) {
		return zero, err
	}
	var n int
	for _, query := range []string{
		`SELECT COUNT(*) FROM native_metered_route_commands WHERE node_id=? AND state IN('queued','delivered')`,
		`SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=? AND state!='sealed'`,
		`SELECT COUNT(*) FROM native_metered_handover_nodes WHERE node_id=?`,
	} {
		if err = tx.QueryRowContext(ctx, query, plan.NodeID).Scan(&n); err != nil {
			return zero, err
		}
		if n != 0 {
			return zero, ErrConflict
		}
	}
	uuid, err := randomUUID()
	if err != nil {
		return zero, err
	}
	compact := strings.ReplaceAll(uuid, "-", "")
	probe := usageledger.ProbeIdentity{
		ID: "probe-" + compact, UUID: uuid, NodeID: plan.NodeID,
		InstallationID: plan.InstallationID, InboundID: plan.Prepared.InboundID,
		EventID: "darkrestore-probe-" + compact,
	}
	if err = s.usage.CreateProbeIdentityTx(ctx, tx, probe); err != nil {
		return zero, err
	}
	return probe, nil
}
