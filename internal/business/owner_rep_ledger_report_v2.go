package business

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// OwnerRepLedgerReport is a consistent, read-only single-representative view
// for the future main HUB Telegram bot. It includes the representative's
// owner-scoped identity and display name, but no Telegram identity, credentials,
// customer details or raw node traffic.
type OwnerRepLedgerReport struct {
	RepresentativeID       string `json:"representative_id"`
	DisplayName            string `json:"display_name"`
	Enabled                bool   `json:"enabled"`
	FreeVolumeBytes        string `json:"free_volume_bytes"`
	FreeUnlimitedUnits     string `json:"free_unlimited_units"`
	SellerQuotaState       string `json:"seller_quota_state"` // not_enabled | enabled
	SellerCount            int64  `json:"seller_count"`
	ReservedVolumeBytes    string `json:"reserved_volume_bytes,omitempty"`
	ReservedUnlimitedUnits string `json:"reserved_unlimited_units,omitempty"`
	BalanceSemantics       string `json:"balance_semantics"`
}

// OwnerRepresentativeLedgerReport deliberately fails closed if the opt-in
// seller tables are incomplete. A missing seller schema is NOT "zero escrow".
func (s *Store) OwnerRepresentativeLedgerReport(ctx context.Context, id string) (OwnerRepLedgerReport, error) {
	var result OwnerRepLedgerReport
	if !idRE.MatchString(id) || id == "owner" {
		return result, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, ErrUnavailable
	}
	defer tx.Rollback()
	var volume, units int64
	var enabled int
	err = tx.QueryRowContext(ctx, `SELECT id,display_name,enabled,volume_credit_bytes,unlimited_credit_units
    FROM representatives WHERE id=? AND kind='representative'`, id).Scan(
		&result.RepresentativeID, &result.DisplayName, &enabled, &volume, &units)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return OwnerRepLedgerReport{}, ErrUnavailable
	}
	if volume < 0 || units < 0 {
		return OwnerRepLedgerReport{}, ErrUnavailable
	}
	result.Enabled = enabled == 1
	result.FreeVolumeBytes = strconv.FormatInt(volume, 10)
	result.FreeUnlimitedUnits = strconv.FormatInt(units, 10)
	result.BalanceSemantics = "authoritative_representative_free_credit_not_total_sales_or_live_traffic"
	result.SellerQuotaState = "not_enabled"
	// The default-on main branch does not create seller tables, even after PR #115.
	// We must distinguish a true empty funded allocation from an unapplied schema.
	var found int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
   WHERE type='table' AND name IN ('seller_quota_v2_meta','representative_sellers_v2','seller_quota_events_v2')`).Scan(&found); err != nil {
		return OwnerRepLedgerReport{}, ErrUnavailable
	}
	if found != 0 && found != 3 {
		return OwnerRepLedgerReport{}, ErrUnavailable
	}
	if found == 3 {
		var version int
		if err = tx.QueryRowContext(ctx, "SELECT version FROM seller_quota_v2_meta WHERE id=1").Scan(&version); err != nil || version != 1 {
			return OwnerRepLedgerReport{}, ErrUnavailable
		}
		// All amounts remain integer SQLite values, then become exact JSON strings.
		// SUM can overflow: SQLite will return an error, fail-closed without
		// inventing a balance. Nil aggregate values are explicitly zero.
		var reservedBytes, reservedUnits int64
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(reserved_bytes),0),
      COALESCE(SUM(reserved_units),0)
      FROM representative_sellers_v2 WHERE representative_id=?`, id).Scan(
			&result.SellerCount, &reservedBytes, &reservedUnits)
		if err != nil || reservedBytes < 0 || reservedUnits < 0 {
			return OwnerRepLedgerReport{}, ErrUnavailable
		}
		result.SellerQuotaState = "enabled"
		result.ReservedVolumeBytes = strconv.FormatInt(reservedBytes, 10)
		result.ReservedUnlimitedUnits = strconv.FormatInt(reservedUnits, 10)
	}
	if err = tx.Commit(); err != nil {
		return OwnerRepLedgerReport{}, ErrUnavailable
	}
	return result, nil
}
