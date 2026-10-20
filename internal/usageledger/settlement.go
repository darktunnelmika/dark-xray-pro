package usageledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// ConfirmSettlement is read-only. It cannot seal an epoch, forgive an unknown
// tail or alter credit. One SQL snapshot binds principal, cursor and receipt.
func (l *Ledger) ConfirmSettlement(ctx context.Context, p Principal, r uc.Receipt) (uc.SettlementReceipt, error) {
	if !uc.ValidReceipt(r) {
		return uc.SettlementReceipt{}, uc.ErrInvalid
	}
	var node, installation, state string
	var cursorJSON []byte
	var sha sql.NullString
	var quiesced sql.NullBool
	err := l.read(ctx, func(c *ledgerConn) error {
		return c.QueryRowContext(ctx, `SELECT e.node_id,e.installation_id,e.state,e.cursor_json,r.sha256,r.quiesced
		FROM pro_usage_epochs e LEFT JOIN pro_usage_receipts r ON r.epoch_id=e.epoch_id AND r.sequence=?
		WHERE e.epoch_id=?`, strconv.FormatUint(r.Sequence, 10), r.EpochID).Scan(&node, &installation, &state, &cursorJSON, &sha, &quiesced)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return uc.SettlementReceipt{}, ErrMissing
	}
	if err != nil {
		return uc.SettlementReceipt{}, err
	}
	if node != p.NodeID || installation != p.InstallationID {
		return uc.SettlementReceipt{}, ErrAuthority
	}
	var cursor uc.Cursor
	if json.Unmarshal(cursorJSON, &cursor) != nil {
		return uc.SettlementReceipt{}, ErrStorage
	}
	if state != "sealed" || !sha.Valid || sha.String != r.SHA || !quiesced.Valid || !quiesced.Bool ||
		!cursor.Quiesced || cursor.EpochID != r.EpochID || cursor.Sequence != r.Sequence || cursor.ReportSHA != r.SHA {
		return uc.SettlementReceipt{}, ErrConflict
	}
	return uc.SettlementReceipt{Receipt: r, Sealed: true}, nil
}
