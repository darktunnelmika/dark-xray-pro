package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"math/big"
	"strconv"
)

// Reopen and backup validation require the immutable receipt, original review,
// client identity and group history to agree. Missing history is never repaired.
func validateDarkRestoreImportData(ctx context.Context, db *sql.DB) error {
	present, err := darkRestoreSchemaState(ctx, db)
	if err != nil || !present {
		return err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT event_id FROM darkrestore_import_receipts_v1 ORDER BY event_id")
	if err != nil {
		return ErrUnavailable
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return ErrUnavailable
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrUnavailable
	}
	for _, event := range ids {
		var fp, actor, sourceSHA, reviewSHA string
		var request, raw []byte
		var at int64
		err = tx.QueryRowContext(ctx, "SELECT fingerprint,actor_user_id,source_sha256,review_sha256,request_json,receipt_json,created_unix FROM darkrestore_import_receipts_v1 WHERE event_id=?", event).Scan(&fp, &actor, &sourceSHA, &reviewSHA, &request, &raw, &at)
		if err != nil || len(request) > 64<<20 {
			return ErrUnavailable
		}
		var req DarkRestoreImportCommitV1
		var receipt DarkRestoreImportReceiptV1
		if json.Unmarshal(request, &req) != nil || decodeDarkRestoreReceipt(raw, event, &receipt) != nil {
			return ErrUnavailable
		}
		canonical, _ := json.Marshal(req)
		intent, hash, e := canonicalDarkRestoreIntent(req.DarkRestoreImportIntentV1)
		if e != nil || !bytes.Equal(canonical, request) || req.EventID != event || darkRestoreRequestFingerprint(actor, req) != fp || hash != reviewSHA || intent.Review.SourceSHA256 != sourceSHA || receipt.ActorUserID != actor || receipt.SourceSHA256 != sourceSHA || receipt.ReviewSHA256 != reviewSHA || receipt.CreatedUnix != at || at <= 0 || len(receipt.Groups) != len(intent.Review.Groups) {
			return ErrUnavailable
		}
		mappings := make(map[string]string, len(intent.Mappings))
		for _, m := range intent.Mappings {
			mappings[m.SourceGroupID] = m.RepresentativeID
		}
		var count, archived, promoted, blocked int64
		for _, c := range intent.Review.Clients {
			var id, uuid, group, rep, history, status string
			e = tx.QueryRowContext(ctx, "SELECT client_id,client_uuid,target_group_id,representative_id,historical_bytes,source_status FROM darkrestore_import_entries_v1 WHERE source_sha256=? AND source_id=? AND event_id=?", sourceSHA, c.SourceID, event).Scan(&id, &uuid, &group, &rep, &history, &status)
			if e == sql.ErrNoRows {
				var skips int
				if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM darkrestore_import_skips_v1 WHERE source_sha256=? AND source_id=? AND event_id=?", sourceSHA, c.SourceID, event).Scan(&skips); e != nil || skips == 0 {
					return ErrUnavailable
				}
				if c.Status == "skip_already_promoted" {
					promoted++
				} else {
					blocked++
				}
				continue
			}
			if e != nil || id != darkRestoreClientID(sourceSHA, c.SourceID) || uuid != c.UUID || history != c.HistoricalBytes || status != c.Status || c.Status == "skip_already_promoted" {
				return ErrUnavailable
			}
			expectedGroup, _ := darkRestoreTargetGroup(sourceSHA, c.SourceGroupID, "")
			if group != expectedGroup {
				return ErrUnavailable
			}
			if mappings[c.SourceGroupID] != rep {
				return ErrUnavailable
			}
			var currentUUID string
			if e = tx.QueryRowContext(ctx, "SELECT uuid FROM clients WHERE id=?", id).Scan(&currentUUID); e != nil || currentUUID != uuid {
				return ErrUnavailable
			}
			count++
			if c.Status == "archive_history_only" {
				archived++
			}
		}
		if count != receipt.Count || archived != receipt.Archived || promoted != receipt.SkippedPromoted || blocked != receipt.Blocked || receipt.Disabled != receipt.Count {
			return ErrUnavailable
		}
		var entries int64
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM darkrestore_import_entries_v1 WHERE event_id=?", event).Scan(&entries); e != nil || entries != count {
			return ErrUnavailable
		}
		for _, g := range intent.Review.Groups {
			var name, history, target, rep string
			var n, a, p int64
			e = tx.QueryRowContext(ctx, "SELECT source_name,target_group_id,representative_id,source_count,archived_count,promoted_count,historical_bytes FROM darkrestore_group_history_v1 WHERE source_sha256=? AND source_group_id=? AND event_id=?", sourceSHA, g.SourceGroupID, event).Scan(&name, &target, &rep, &n, &a, &p, &history)
			expected, _ := darkRestoreTargetGroup(sourceSHA, g.SourceGroupID, g.Name)
			if e != nil || name != g.Name || target != expected || n != g.Count || a != g.Archived || p != g.Promoted || history != g.HistoricalBytes {
				return ErrUnavailable
			}
			if mappings[g.SourceGroupID] != rep {
				return ErrUnavailable
			}
		}
	}
	// Foreign-key enforcement can be bypassed by external tools; reject orphans.
	for _, table := range []string{"darkrestore_import_entries_v1", "darkrestore_group_history_v1", "darkrestore_import_skips_v1"} {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" d LEFT JOIN darkrestore_import_receipts_v1 r ON r.event_id=d.event_id WHERE r.event_id IS NULL").Scan(&n); err != nil || n != 0 {
			return ErrUnavailable
		}
	}
	return nil
}

type DarkRestoreUsageV1 struct {
	ClientID           string `json:"client_id"`
	HistoricalBytes    string `json:"historical_bytes"`
	PostMigrationBytes string `json:"post_migration_bytes"`
	TotalBytes         string `json:"total_bytes"`
	CutoverVerified    bool   `json:"cutover_verified"`
}

// This projection never adds history to the native counters or writes a ledger.
// Unknown cutover remains explicit even when current observed bytes equal zero.
func (s *Store) DarkRestoreUsageV1(ctx context.Context, id string) (DarkRestoreUsageV1, error) {
	account, err := s.ClientUsage(ctx, id)
	if err != nil {
		return DarkRestoreUsageV1{}, err
	}
	projection, err := s.DarkRestoreUsageForAccount(ctx, account)
	if err != nil {
		return DarkRestoreUsageV1{}, err
	}
	if projection == nil {
		return DarkRestoreUsageV1{}, ErrNotFound
	}
	return *projection, nil
}

// Use the same native account snapshot already checked against the caller's
// representative scope. The legacy count is immutable across reassignment.
func (s *Store) DarkRestoreUsageForAccount(ctx context.Context, account usageledger.Account) (*DarkRestoreUsageV1, error) {
	present, err := darkRestoreSchemaState(ctx, s.db)
	if err != nil || !present {
		return nil, err
	}
	out := DarkRestoreUsageV1{ClientID: account.ClientID}
	err = s.db.QueryRowContext(ctx, "SELECT historical_bytes FROM darkrestore_import_entries_v1 WHERE client_id=?", account.ClientID).Scan(&out.HistoricalBytes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	historical, err := strconv.ParseInt(out.HistoricalBytes, 10, 64)
	if err != nil || historical < 0 {
		return nil, ErrUnavailable
	}
	out.PostMigrationBytes = strconv.FormatInt(account.Total, 10)
	out.TotalBytes = new(big.Int).Add(big.NewInt(historical), big.NewInt(account.Total)).String()
	out.CutoverVerified = account.Known
	return &out, nil
}
