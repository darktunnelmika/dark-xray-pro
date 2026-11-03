package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
)

// DarkRestoreCutoverSnapshotV1 is read-only evidence from one SQLite snapshot.
// Unknown native coverage remains unknown, even if the recorded counters are zero.
// Its hashes are concurrency tokens, not source-custody or activation authority.
type DarkRestoreCutoverSnapshotV1 struct {
	SourceSHA               string `json:"source_sha256"`
	SourceID                string `json:"source_id"`
	ClientID                string `json:"client_id"`
	ClientUUID              string `json:"client_uuid"`
	RepresentativeID        string `json:"representative_id"`
	ImportEventID           string `json:"import_event_id"`
	ImportReceiptSHA        string `json:"import_receipt_sha256"`
	ImportedHistoricalBytes string `json:"imported_historical_bytes"`
	PlanKind                string `json:"plan_kind"`
	CurrentEntitlement      int64  `json:"current_entitlement,string"`
	ExpiresUnix             int64  `json:"expires_unix,string"`
	Archived                bool   `json:"archived"`
	DeploymentState         string `json:"deployment_state"`
	NativeAccountPresent    bool   `json:"native_account_present"`
	NativeBaselineComplete  bool   `json:"native_baseline_complete"`
	NativeUplink            *int64 `json:"native_uplink,omitempty,string"`
	NativeDownlink          *int64 `json:"native_downlink,omitempty,string"`
	NativeHolds             *int64 `json:"native_holds,omitempty,string"`
	NativeBaselineSHA       string `json:"native_baseline_sha256"`
	StateSHA                string `json:"state_sha256"`
	AuthorizesMutation      bool   `json:"authorizes_mutation"`
}

func cutoverSnapshotDigest(domain string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", ErrUnavailable
	}
	sum := sha256.Sum256(append([]byte(domain+"\x00"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

// DarkRestoreCutoverSnapshotV1 reads destination state only. It neither invents
// destination coverage nor persists an approval. A future transition must re-read
// and compare this state in its write transaction before issuing a receipt.
func (s *Store) DarkRestoreCutoverSnapshotV1(ctx context.Context, sourceSHA, sourceID string) (DarkRestoreCutoverSnapshotV1, error) {
	out := DarkRestoreCutoverSnapshotV1{SourceSHA: sourceSHA, SourceID: sourceID}
	sha, err := hex.DecodeString(sourceSHA)
	if err != nil || len(sha) != 32 || hex.EncodeToString(sha) != sourceSHA || !validName(sourceID, 256) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return out, ErrUnavailable
	}
	var receipt []byte
	var up, down, baseline, holds sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT c.id,c.uuid,c.representative_id,e.event_id,r.receipt_json,
 e.historical_bytes,c.plan_kind,c.volume_bytes,c.expires_unix,c.archived,c.deployment_state,
 a.uplink,a.downlink,a.baseline_complete,a.holds
 FROM darkrestore_import_entries_v1 e
 JOIN darkrestore_import_receipts_v1 r ON r.event_id=e.event_id AND r.source_sha256=e.source_sha256
 JOIN clients c ON c.id=e.client_id AND c.uuid=e.client_uuid
 LEFT JOIN pro_usage_accounts a ON a.client_id=c.id
 WHERE e.source_sha256=? AND e.source_id=?`, sourceSHA, sourceID).Scan(
		&out.ClientID, &out.ClientUUID, &out.RepresentativeID, &out.ImportEventID, &receipt,
		&out.ImportedHistoricalBytes, &out.PlanKind, &out.CurrentEntitlement, &out.ExpiresUnix,
		&out.Archived, &out.DeploymentState, &up, &down, &baseline, &holds)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	history, err := strconv.ParseInt(out.ImportedHistoricalBytes, 10, 64)
	if err != nil || history < 0 || strconv.FormatInt(history, 10) != out.ImportedHistoricalBytes ||
		!json.Valid(receipt) || out.CurrentEntitlement < 0 {
		return out, ErrUnavailable
	}
	receiptSum := sha256.Sum256(receipt)
	out.ImportReceiptSHA = hex.EncodeToString(receiptSum[:])
	if up.Valid || down.Valid || baseline.Valid || holds.Valid {
		if !up.Valid || !down.Valid || !baseline.Valid || !holds.Valid ||
			up.Int64 < 0 || down.Int64 < 0 || holds.Int64 < 0 || (baseline.Int64 != 0 && baseline.Int64 != 1) {
			return out, ErrUnavailable
		}
		out.NativeAccountPresent = true
		out.NativeBaselineComplete = baseline.Int64 == 1
		out.NativeUplink = &up.Int64
		out.NativeDownlink = &down.Int64
		out.NativeHolds = &holds.Int64
	}
	native := struct {
		ClientID   string
		ClientUUID string
		Present    bool
		Complete   bool
		Uplink     *int64
		Downlink   *int64
		Holds      *int64
	}{out.ClientID, out.ClientUUID, out.NativeAccountPresent, out.NativeBaselineComplete, out.NativeUplink, out.NativeDownlink, out.NativeHolds}
	out.NativeBaselineSHA, err = cutoverSnapshotDigest("DARK-PRO-NATIVE-BASELINE-v1", native)
	if err != nil {
		return out, err
	}
	out.StateSHA, err = cutoverSnapshotDigest("DARK-PRO-CUTOVER-STATE-v1", out)
	return out, err
}
