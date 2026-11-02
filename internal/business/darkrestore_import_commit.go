package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// CommitDarkRestoreImportV1 is an offline operator entry point. sourcePath must
// be an independently checkpointed private legacy snapshot, never the live HUB.
// It intentionally has no HTTP route accepting a filesystem path.
func (s *Store) CommitDarkRestoreImportV1(ctx context.Context, token, sourcePath string, historyKey []byte, input DarkRestoreImportCommitV1) (DarkRestoreImportReceiptV1, error) {
	if err := darkrestore.VerifyImportReview(ctx, sourcePath, input.Review); err != nil {
		return DarkRestoreImportReceiptV1{}, ErrInvalid
	}
	events, err := darkrestore.ReadEventHistory(ctx, sourcePath, input.Review)
	if err != nil || len(historyKey) != 32 {
		return DarkRestoreImportReceiptV1{}, ErrInvalid
	}
	return s.commitDarkRestoreTx(ctx, token, input, historyKey, events)
}

func darkRestoreRequestFingerprint(actor string, req DarkRestoreImportCommitV1) string {
	return fingerprint("darkrestore-import-commit-v1", struct {
		Actor   string
		Request DarkRestoreImportCommitV1
	}{actor, req})
}
func (s *Store) commitDarkRestoreTx(ctx context.Context, token string, input DarkRestoreImportCommitV1, key []byte, events []darkrestore.LegacyEvent) (DarkRestoreImportReceiptV1, error) {
	var out DarkRestoreImportReceiptV1
	intent, reviewSHA, err := canonicalDarkRestoreIntent(input.DarkRestoreImportIntentV1)
	if err != nil || !eventRE.MatchString(input.EventID) || len(input.QuoteHash) != 64 {
		return out, ErrInvalid
	}
	if _, err = hex.DecodeString(input.QuoteHash); err != nil {
		return out, ErrInvalid
	}
	input.DarkRestoreImportIntentV1 = intent
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return out, err
	}
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present || s.usage == nil {
		return out, ErrUnavailable
	}
	requestRaw, err := json.Marshal(input)
	if err != nil {
		return out, ErrInvalid
	}
	if len(requestRaw) > 64<<20 {
		return out, ErrInvalid
	}
	fp := darkRestoreRequestFingerprint(actor, input)
	var savedFP string
	var savedRequest, raw []byte
	err = tx.QueryRowContext(ctx, "SELECT fingerprint,request_json,receipt_json FROM darkrestore_import_receipts_v1 WHERE event_id=?", input.EventID).Scan(&savedFP, &savedRequest, &raw)
	if err == nil {
		if savedFP != fp || !bytes.Equal(requestRaw, savedRequest) {
			return out, ErrConflict
		}
		if err = decodeDarkRestoreReceipt(raw, input.EventID, &out); err != nil || out.ActorUserID != actor {
			return DarkRestoreImportReceiptV1{}, ErrUnavailable
		}
		out.Replayed = true
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err = newOwnershipEvent(ctx, tx, input.EventID); err != nil {
		return out, err
	}
	if err = rejectOwnershipEvent(ctx, tx, input.EventID); err != nil {
		return out, err
	}
	now := time.Now().Unix()
	if input.QuoteUnix <= 0 || input.QuoteUnix < now-180 || input.QuoteUnix > now+5 {
		return out, ErrConflict
	}
	quote, plan, err := quotedDarkRestoreTx(ctx, tx, actor, intent, reviewSHA, input.QuoteUnix)
	if err != nil {
		return out, err
	}
	if quote.QuoteHash != input.QuoteHash || quote.Blocked > 0 && !intent.ImportSafeOnly {
		return out, ErrConflict
	}
	out = DarkRestoreImportReceiptV1{EventID: input.EventID, ActorUserID: actor, SourceSHA256: intent.Review.SourceSHA256, ReviewSHA256: reviewSHA,
		Count: quote.Importable, Archived: quote.Archived, Disabled: quote.Importable, SkippedPromoted: quote.SkippedPromoted,
		Blocked: quote.Blocked, BlockedReasons: quote.BlockedReasons, Groups: quote.Groups, CreatedUnix: now, UsageBaselineMade: true}
	if int64(len(events)) != intent.Review.Summary.Events {
		return DarkRestoreImportReceiptV1{}, ErrInvalid
	}
	var sealed []byte
	if len(events) > 0 {
		plain, e := json.Marshal(events)
		if e != nil || len(plain) > 32<<20 {
			return DarkRestoreImportReceiptV1{}, ErrInvalid
		}
		sealed, e = sealSubscription(key, "darkrestore-event-history", []string{out.EventID, out.SourceSHA256, out.ReviewSHA256}, plain)
		if e != nil {
			return DarkRestoreImportReceiptV1{}, e
		}
		h := sha256.Sum256(sealed)
		out.HistorySHA = hex.EncodeToString(h[:])
		out.LegacyEvents = int64(len(events))
	}
	raw, err = json.Marshal(out)
	if err != nil {
		return DarkRestoreImportReceiptV1{}, err
	}
	if len(raw) > 16<<20 {
		return DarkRestoreImportReceiptV1{}, ErrInvalid
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_import_receipts_v1(event_id,fingerprint,actor_user_id,source_sha256,review_sha256,request_json,receipt_json,created_unix) VALUES(?,?,?,?,?,?,?,?)", input.EventID, fp, actor, out.SourceSHA256, reviewSHA, requestRaw, raw, now); err != nil {
		return DarkRestoreImportReceiptV1{}, err
	}
	if len(sealed) > 0 {
		if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_event_archives_v1(event_id,source_sha256,record_count,sealed_history) VALUES(?,?,?,?)", out.EventID, out.SourceSHA256, out.LegacyEvents, sealed); err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
	}
	sourceGroups := make(map[string]darkrestore.ReviewGroup, len(intent.Review.Groups))
	for _, g := range intent.Review.Groups {
		sourceGroups[g.SourceGroupID] = g
	}
	for _, g := range quote.Groups {
		if _, err = tx.ExecContext(ctx, "INSERT INTO client_groups(id,representative_id,name,color,created_unix,updated_unix) VALUES(?,?,?,'',?,?) ON CONFLICT(id) DO NOTHING", g.TargetGroupID, g.RepresentativeID, g.TargetGroupName, now, now); err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
		source, found := sourceGroups[g.SourceGroupID]
		if !found {
			return DarkRestoreImportReceiptV1{}, ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_group_history_v1(source_sha256,source_group_id,source_name,target_group_id,representative_id,source_count,archived_count,promoted_count,historical_bytes,event_id,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?)", out.SourceSHA256, g.SourceGroupID, g.SourceName, g.TargetGroupID, g.RepresentativeID, source.Count, source.Archived, source.Promoted, source.HistoricalBytes, input.EventID, now); err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
	}
	for _, row := range plan {
		reasons := row.Blocked
		if row.Promoted {
			reasons = []string{"already_promoted"}
		}
		if len(reasons) > 0 {
			for _, reason := range reasons {
				if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_import_skips_v1(source_sha256,source_id,source_group_id,reason,event_id,created_unix) VALUES(?,?,?,?,?,?)", out.SourceSHA256, row.Client.SourceID, row.Client.SourceGroupID, reason, input.EventID, now); err != nil {
					return DarkRestoreImportReceiptV1{}, err
				}
			}
			continue
		}
		// Snapshot import does not attest that legacy traffic stopped. Preserve the
		// old consumption separately; block service until a verified cutover exists.
		_, err = tx.ExecContext(ctx, "INSERT INTO clients("+clientColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			row.ClientID, row.Mapping.RepresentativeID, row.Client.Label, row.Client.UUID, "volume", 0, row.Quota, row.Duration, 0, 0, now, row.Client.ExpiryUnix, "disabled", row.Archived)
		if err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO client_group_members(client_id,group_id,assigned_unix) VALUES(?,?,?)", row.ClientID, row.GroupID, now); err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_import_entries_v1(source_sha256,source_id,client_id,client_uuid,source_group_id,target_group_id,representative_id,historical_bytes,source_status,event_id,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
			out.SourceSHA256, row.Client.SourceID, row.ClientID, row.Client.UUID, row.Client.SourceGroupID, row.GroupID, row.Mapping.RepresentativeID, row.Client.HistoricalBytes, row.Client.Status, input.EventID, now); err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
		if err = s.usage.InitializeAccountTx(ctx, tx, usageledger.Seed{ClientID: row.ClientID, EventID: "darkrestore-seed-" + row.ClientID, Uplink: 0, Downlink: 0, Complete: false, Reason: "Legacy history retained separately; verified traffic cutover required"}); err != nil {
			return DarkRestoreImportReceiptV1{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return DarkRestoreImportReceiptV1{}, err
	}
	return out, nil
}
func decodeDarkRestoreReceipt(raw []byte, event string, out *DarkRestoreImportReceiptV1) error {
	if len(raw) > 16<<20 || json.Unmarshal(raw, out) != nil || out.EventID != event || out.Replayed || out.Count < 0 || out.CreditChanged || out.UsageReset || out.XrayChanged || !out.UsageBaselineMade {
		return ErrUnavailable
	}
	canonical, _ := json.Marshal(out)
	if !bytes.Equal(canonical, raw) {
		return ErrUnavailable
	}
	return nil
}
func (s *Store) DarkRestoreImportReceiptV1(ctx context.Context, token, event string) (DarkRestoreImportReceiptV1, error) {
	var out DarkRestoreImportReceiptV1
	if !eventRE.MatchString(event) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return out, err
	}
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return out, ErrUnavailable
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_import_receipts_v1 WHERE event_id=?", event).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	} else if err != nil {
		return out, err
	}
	err = decodeDarkRestoreReceipt(raw, event, &out)
	return out, err
}
