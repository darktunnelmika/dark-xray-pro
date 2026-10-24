package business

import (
	"context"
	"database/sql"
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type subscriptionDeliveryHead struct {
	Enabled                    bool
	ClientID                   string
	Revision                   uint64
	Scope, SnapshotID, EventID string
	SnapshotRevision           uint64
	Created                    int64
}

func readSubscriptionDelivery(ctx context.Context, tx *sql.Tx, client string) (subscriptionDeliveryHead, error) {
	out := subscriptionDeliveryHead{ClientID: client}
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return out, err
	}
	if !usageledger.HasInitialApplyHealthSchema(version) {
		return out, nil
	}
	out.Enabled = true
	err := tx.QueryRowContext(ctx, `SELECT h.delivery_revision,h.scope,h.snapshot_id,e.event_id,e.snapshot_revision,e.created FROM pro_delivery_heads h JOIN pro_delivery_events e ON e.client_id=h.client_id AND e.delivery_revision=h.delivery_revision AND e.scope=h.scope AND e.snapshot_id=h.snapshot_id WHERE h.client_id=?`, client).Scan(&out.Revision, &out.Scope, &out.SnapshotID, &out.EventID, &out.SnapshotRevision, &out.Created)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var latest, count uint64
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(delivery_revision),0),COUNT(*) FROM pro_delivery_events WHERE client_id=?`, client).Scan(&latest, &count); e != nil {
		return out, e
	}
	if latest != out.Revision || count != latest {
		return out, ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		var local int
		if e := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM pro_subscription_snapshot_heads WHERE client_id=?)+(SELECT COUNT(*) FROM pro_initial_snapshot_heads WHERE client_id=?)`, client, client).Scan(&local); e != nil {
			return out, e
		}
		if local != 0 {
			return out, ErrUnavailable
		}
		return out, nil
	}
	if out.Revision == 0 || out.Revision >= routeMax || out.SnapshotRevision == 0 || out.SnapshotRevision >= routeMax || !routeID.MatchString(out.SnapshotID) || !eventRE.MatchString(out.EventID) || out.Created <= 0 {
		return out, ErrUnavailable
	}
	table, heads := "pro_subscription_snapshots", "pro_subscription_snapshot_heads"
	if out.Scope == InitialApplyHealthScope {
		table, heads = "pro_initial_snapshots", "pro_initial_snapshot_heads"
	} else if out.Scope != HandoverSubscriptionScope {
		return out, ErrUnavailable
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` s JOIN `+heads+` h ON h.snapshot_id=s.id AND h.client_id=s.client_id AND h.revision=s.revision WHERE s.id=? AND s.client_id=? AND s.revision=? AND s.event_id=?`, out.SnapshotID, client, out.SnapshotRevision, out.EventID).Scan(&n); err != nil {
		return out, err
	}
	if n != 1 {
		return out, ErrUnavailable
	}
	return out, nil
}

func appendSubscriptionDelivery(ctx context.Context, tx *sql.Tx, prior subscriptionDeliveryHead, scope string, row LabSubscriptionSnapshot) (uint64, error) {
	if !prior.Enabled {
		return 0, nil
	}
	if prior.ClientID != row.ClientID || prior.Revision >= routeMax-1 || (scope != InitialApplyHealthScope && scope != HandoverSubscriptionScope) {
		return 0, ErrConflict
	}
	revision := prior.Revision + 1
	if _, err := tx.ExecContext(ctx, `INSERT INTO pro_delivery_events(client_id,delivery_revision,event_id,scope,snapshot_id,snapshot_revision,created) VALUES(?,?,?,?,?,?,?)`, row.ClientID, revision, row.EventID, scope, row.ID, row.Revision, row.CreatedUnixNano); err != nil {
		return 0, err
	}
	if prior.Revision == 0 {
		_, err := tx.ExecContext(ctx, `INSERT INTO pro_delivery_heads(client_id,delivery_revision,scope,snapshot_id) VALUES(?,?,?,?)`, row.ClientID, revision, scope, row.ID)
		return revision, err
	}
	r, err := tx.ExecContext(ctx, `UPDATE pro_delivery_heads SET delivery_revision=?,scope=?,snapshot_id=? WHERE client_id=? AND delivery_revision=?`, revision, scope, row.ID, row.ClientID, prior.Revision)
	if err != nil {
		return 0, err
	}
	if n, e := r.RowsAffected(); e != nil || n != 1 {
		return 0, ErrConflict
	}
	return revision, nil
}

func snapshotDeliveryMetadata(ctx context.Context, tx *sql.Tx, head subscriptionDeliveryHead, scope string, out *LabSubscriptionSnapshot) error {
	if !head.Enabled {
		return nil
	}
	out.Scope = scope
	var client string
	var local uint64
	err := tx.QueryRowContext(ctx, `SELECT client_id,delivery_revision,snapshot_revision FROM pro_delivery_events WHERE scope=? AND snapshot_id=?`, scope, out.ID).Scan(&client, &out.DeliveryRevision, &local)
	// A handover snapshot superseded before v15 has no global authority event.
	// Its historical receipt remains readable, always Current=false.
	if errors.Is(err, sql.ErrNoRows) && scope == HandoverSubscriptionScope {
		out.Current = false
		return nil
	}
	if err != nil {
		return err
	}
	if client != out.ClientID || local != out.Revision {
		return ErrUnavailable
	}
	if head.Scope != scope || head.SnapshotID != out.ID || head.Revision != out.DeliveryRevision {
		out.Current = false
	}
	return nil
}

func initialEventNotUsed(ctx context.Context, tx *sql.Tx, event string) error {
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if !usageledger.HasInitialApplyHealthSchema(version) {
		return nil
	}
	for _, table := range []string{"pro_initial_preparations", "pro_initial_snapshots"} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE event_id=?`, event).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return healthIDNotInitial(ctx, tx, event)
}
