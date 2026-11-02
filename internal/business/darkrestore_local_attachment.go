package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

type DarkRestoreLocalAttachmentReceiptV1 struct {
	ClientID        string                            `json:"client_id"`
	OriginalEventID string                            `json:"original_import_event_id"`
	ActorUserID     string                            `json:"actor_user_id"`
	Settlement      darkrestore.LocalSourceSettlement `json:"source_settlement"`
	CreatedUnix     int64                             `json:"created_unix"`
	Replayed        bool                              `json:"replayed"`
}

// A persisted local proof only carries the scope verified at insertion time.
// It cannot authorize activation and does not claim custody of other nodes.
func validateDarkRestoreLocalAttachments(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreLocalAttachmentSchemaState(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT client_id,source_sha256,source_id,terminal_epoch_id,receipt_json,created_unix FROM darkrestore_local_settlements_v1")
	if err != nil {
		return ErrUnavailable
	}
	type item struct {
		id, sha, source, epoch string
		raw                    []byte
		at                     int64
	}
	var all []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.id, &v.sha, &v.source, &v.epoch, &v.raw, &v.at); err != nil {
			break
		}
		all = append(all, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	for _, v := range all {
		var receipt DarkRestoreLocalAttachmentReceiptV1
		if decodeDarkRestoreLocalAttachment(v.raw, &receipt) != nil ||
			receipt.ClientID != v.id || receipt.Settlement.SourceSHA != v.sha ||
			receipt.Settlement.SourceID != v.source ||
			receipt.Settlement.Terminal.EpochID != v.epoch ||
			receipt.CreatedUnix != v.at ||
			len(receipt.Settlement.AuthoritySHA) != 64 ||
			len(receipt.Settlement.JournalSHA) != 64 {
			return ErrUnavailable
		}
		var originalEvent, history, uuid, status, plan, deployment string
		var original []byte
		var expiry, quota, up, down int64
		var archived, baseline, holds int
		err = tx.QueryRowContext(ctx, `SELECT e.event_id,e.historical_bytes,e.client_uuid,e.source_status,
            r.request_json,c.plan_kind,c.deployment_state,c.expires_unix,c.volume_bytes,c.archived,
            a.uplink,a.downlink,a.baseline_complete,a.holds
            FROM darkrestore_import_entries_v1 e
            JOIN darkrestore_import_receipts_v1 r ON r.event_id=e.event_id AND r.source_sha256=e.source_sha256
            JOIN clients c ON c.id=e.client_id AND c.uuid=e.client_uuid
            JOIN pro_usage_accounts a ON a.client_id=e.client_id
            WHERE e.client_id=? AND e.source_sha256=? AND e.source_id=?`,
			v.id, v.sha, v.source).Scan(&originalEvent, &history, &uuid, &status, &original, &plan, &deployment, &expiry, &quota, &archived, &up, &down, &baseline, &holds)
		if err != nil || receipt.OriginalEventID != originalEvent ||
			history != strconv.FormatInt(receipt.Settlement.PriorBytes, 10) ||
			status != "requires_manual_review" ||
			plan != "volume" || deployment != "disabled" ||
			baseline != 0 || up != 0 || down != 0 || holds != 0 ||
			receipt.Settlement.Historical < 0 || receipt.Settlement.Historical > math.MaxInt64 {
			return ErrUnavailable
		}
		var req DarkRestoreImportCommitV1
		if json.Unmarshal(original, &req) != nil || req.Review.SourceSHA256 != v.sha {
			return ErrUnavailable
		}
		found := 0
		for _, c := range req.Review.Clients {
			if c.SourceID == v.source {
				found++
				originalQuota, e := strconv.ParseInt(c.QuotaBytes, 10, 64)
				if e != nil || originalQuota <= 0 || c.UUID != uuid || c.QuotaKind != "volume" ||
					c.HistoricalBytes != history || receipt.Settlement.Remaining != max(0, originalQuota-receipt.Settlement.Historical) {
					return ErrUnavailable
				}
			}
		}
		if found != 1 {
			return ErrUnavailable
		}
	}
	return nil
}

func readDarkRestoreLocalAttachment(ctx context.Context, q sellerQueryV2, id string) (*DarkRestoreLocalAttachmentReceiptV1, error) {
	present, err := darkRestoreLocalAttachmentSchemaState(ctx, q)
	if err != nil || !present {
		return nil, err
	}
	var raw []byte
	err = q.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_local_settlements_v1 WHERE client_id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt DarkRestoreLocalAttachmentReceiptV1
	if decodeDarkRestoreLocalAttachment(raw, &receipt) != nil || receipt.ClientID != id {
		return nil, ErrUnavailable
	}
	return &receipt, nil
}

// The stored receipt is checked again during backup validation and on read.
func decodeDarkRestoreLocalAttachment(raw []byte, out *DarkRestoreLocalAttachmentReceiptV1) error {
	if len(raw) > 16<<10 || json.Unmarshal(raw, out) != nil || out.Replayed ||
		out.ClientID == "" || out.OriginalEventID == "" || out.ActorUserID == "" ||
		out.CreatedUnix <= 0 || out.Settlement.Scope != "single_local_source_epoch" ||
		!uc.ValidReceipt(out.Settlement.Terminal) || out.Settlement.PriorBytes < 0 ||
		out.Settlement.Up < 0 || out.Settlement.Down < 0 ||
		out.Settlement.PriorBytes > math.MaxInt64-out.Settlement.Up ||
		out.Settlement.PriorBytes+out.Settlement.Up > math.MaxInt64-out.Settlement.Down ||
		out.Settlement.Historical != out.Settlement.PriorBytes+out.Settlement.Up+out.Settlement.Down {
		return ErrUnavailable
	}
	canonical, _ := json.Marshal(out)
	if !bytes.Equal(canonical, raw) {
		return ErrUnavailable
	}
	return nil
}
