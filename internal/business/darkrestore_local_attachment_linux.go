//go:build linux

package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// AttachVerifiedLocalSettlementV1 is a local operator operation, with no HTTP
// endpoint. The independent source journal key is used only by the verifier on
// the source host; it is never imported into the destination HUB.
func (s *Store) AttachVerifiedLocalSettlementV1(ctx context.Context, token string, in darkrestore.LocalInputs, seal managedxray.LocalTCPSeal) (DarkRestoreLocalAttachmentReceiptV1, error) {
	proof, err := darkrestore.InspectLocalSettlement(ctx, in, seal)
	if err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrUnavailable
	}
	present, err = darkRestoreLocalAttachmentSchemaState(ctx, tx)
	if err != nil || !present {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrUnavailable
	}

	var id, originalEvent, originalUUID, history, status, uuid, plan, deployment string
	var originalRequest, saved []byte
	var expiry, quota, up, down int64
	var archived, baseline, holds int
	err = tx.QueryRowContext(ctx, `SELECT e.client_id,e.event_id,e.client_uuid,e.historical_bytes,e.source_status,
        r.request_json,c.uuid,c.plan_kind,c.deployment_state,c.expires_unix,c.volume_bytes,c.archived,
        a.uplink,a.downlink,a.baseline_complete,a.holds
        FROM darkrestore_import_entries_v1 e
        JOIN darkrestore_import_receipts_v1 r ON r.event_id=e.event_id AND r.source_sha256=e.source_sha256
        JOIN clients c ON c.id=e.client_id JOIN pro_usage_accounts a ON a.client_id=c.id
        WHERE e.source_sha256=? AND e.source_id=?`, proof.SourceSHA, proof.SourceID).
		Scan(&id, &originalEvent, &originalUUID, &history, &status, &originalRequest, &uuid, &plan, &deployment, &expiry, &quota, &archived, &up, &down, &baseline, &holds)
	if errors.Is(err, sql.ErrNoRows) {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrNotFound
	}
	if err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	if err = validateRestoreReceipt(ctx, tx, originalEvent); err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	var req DarkRestoreImportCommitV1
	if json.Unmarshal(originalRequest, &req) != nil || req.Review.SourceSHA256 != proof.SourceSHA ||
		id != darkRestoreClientID(proof.SourceSHA, proof.SourceID) || status != "requires_manual_review" ||
		history != strconv.FormatInt(proof.PriorBytes, 10) || originalUUID == "" || uuid != originalUUID {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
	}
	var original *darkrestore.ReviewClient
	for i := range req.Review.Clients {
		if req.Review.Clients[i].SourceID == proof.SourceID {
			if original != nil {
				return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
			}
			original = &req.Review.Clients[i]
		}
	}
	if original == nil || original.UUID != uuid || original.HistoricalBytes != history ||
		original.QuotaKind != "volume" || plan != "volume" ||
		deployment != "disabled" || !uc.ValidReceipt(proof.Terminal) ||
		proof.Historical != proof.PriorBytes+proof.Up+proof.Down {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
	}
	originalQuota, err := strconv.ParseInt(original.QuotaBytes, 10, 64)
	if err != nil || originalQuota <= 0 || proof.Remaining != max(0, originalQuota-proof.Historical) {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
	}

	err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_local_settlements_v1 WHERE client_id=?", id).Scan(&saved)
	if err == nil {
		var prior DarkRestoreLocalAttachmentReceiptV1
		if decodeDarkRestoreLocalAttachment(saved, &prior) != nil ||
			prior.ClientID != id || prior.OriginalEventID != originalEvent ||
			prior.ActorUserID != actor || prior.Settlement != proof {
			return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
		}
		prior.Replayed = true
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	if original.ExpiryUnix != expiry || quota != originalQuota || archived != 0 ||
		baseline != 0 || up != 0 || down != 0 || holds != 0 || proof.PriorBytes < 0 ||
		proof.Up < 0 || proof.Down < 0 || proof.Historical < proof.PriorBytes {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
	}
	var activity int
	if err = tx.QueryRowContext(ctx, `SELECT
        (SELECT count(*) FROM pro_usage_deltas WHERE client_id=?)+
        (SELECT count(*) FROM pro_usage_holds WHERE client_id=?)+
        (SELECT count(*) FROM pro_usage_epochs WHERE epoch_json LIKE ?)`, id, id, "%"+id+"%").Scan(&activity); err != nil || activity != 0 {
		return DarkRestoreLocalAttachmentReceiptV1{}, ErrConflict
	}
	out := DarkRestoreLocalAttachmentReceiptV1{ClientID: id, OriginalEventID: originalEvent, ActorUserID: actor, Settlement: proof, CreatedUnix: time.Now().Unix()}
	raw, err := json.Marshal(out)
	if err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_local_settlements_v1(client_id,source_sha256,source_id,terminal_epoch_id,receipt_json,created_unix) VALUES(?,?,?,?,?,?)`,
		id, proof.SourceSHA, proof.SourceID, proof.Terminal.EpochID, raw, out.CreatedUnix)
	if err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return DarkRestoreLocalAttachmentReceiptV1{}, err
	}
	return out, nil
}
