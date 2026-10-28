package business

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Public financial history deliberately omits runtime proofs, installation
// identities and encrypted receipts. Amounts remain exact decimal strings.
type VolumeRefundHistoryRow struct {
	Generation       string `json:"generation"`
	ClientID         string `json:"client_id"`
	RepresentativeID string `json:"representative_id"`
	Trigger          string `json:"trigger"`
	RefundedBytes    int64  `json:"refunded_bytes,string"`
	UsedBytes        int64  `json:"used_bytes,string"`
	VolumeBefore     int64  `json:"volume_before_bytes,string"`
	CreatedUnix      int64  `json:"created_unix"`
}

// Empty representative scope is reserved for the authenticated owner by the
// browser API. A representative scope is always resolved from the session.
func (s *Store) VolumeRefundHistory(ctx context.Context, representativeID string, limit, offset int) ([]VolumeRefundHistoryRow, error) {
	if representativeID != "" && !idRE.MatchString(representativeID) || limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = volumeRefundConfigured(ctx, tx); err != nil {
		return nil, err
	}
	query := `SELECT generation,client_id,representative_id,trigger_kind,refunded_bytes,created_unix,receipt_json FROM volume_refund_receipts`
	args := []any{}
	if representativeID != "" {
		query += ` WHERE representative_id=?`
		args = append(args, representativeID)
	}
	query += ` ORDER BY created_unix DESC,generation ASC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VolumeRefundHistoryRow, 0)
	for rows.Next() {
		var row VolumeRefundHistoryRow
		var raw []byte
		if err = rows.Scan(&row.Generation, &row.ClientID, &row.RepresentativeID, &row.Trigger, &row.RefundedBytes, &row.CreatedUnix, &raw); err != nil {
			return nil, err
		}
		var receipt VolumeExpiryRefundReceipt
		if json.Unmarshal(raw, &receipt) != nil || receipt.Generation != row.Generation || receipt.ClientID != row.ClientID ||
			receipt.RepresentativeID != row.RepresentativeID || receipt.Trigger != row.Trigger || receipt.RefundedBytes != row.RefundedBytes ||
			receipt.CreatedUnix != row.CreatedUnix || receipt.UsedBytes < 0 || receipt.VolumeBefore < 0 {
			return nil, ErrUnavailable
		}
		row.UsedBytes, row.VolumeBefore = receipt.UsedBytes, receipt.VolumeBefore
		out = append(out, row)
	}
	return out, rows.Err()
}
