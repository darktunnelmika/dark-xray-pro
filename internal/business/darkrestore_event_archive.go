package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

func readDarkRestoreArchive(ctx context.Context, tx *sql.Tx, r DarkRestoreImportReceiptV1) ([]byte, error) {
	var source string
	var count int64
	var sealed []byte
	err := tx.QueryRowContext(ctx, "SELECT source_sha256,record_count,CASE WHEN length(sealed_history) BETWEEN 28 AND 33554460 THEN sealed_history END FROM darkrestore_event_archives_v1 WHERE event_id=?", r.EventID).Scan(&source, &count, &sealed)
	if errors.Is(err, sql.ErrNoRows) && r.LegacyEvents == 0 && r.HistorySHA == "" {
		return nil, nil
	}
	if err != nil || count != r.LegacyEvents || count <= 0 || source != r.SourceSHA256 || len(sealed) < 28 {
		return nil, ErrUnavailable
	}
	sum := sha256.Sum256(sealed)
	if hex.EncodeToString(sum[:]) != r.HistorySHA {
		return nil, ErrUnavailable
	}
	return sealed, nil
}

type DarkRestoreEventPage struct {
	Events []darkrestore.LegacyEvent `json:"events"`
	Total  int64                     `json:"total"`
	Offset int                       `json:"offset"`
}

func (s *Store) DarkRestoreEventHistory(ctx context.Context, token, event string, key []byte, offset, limit int) (DarkRestoreEventPage, error) {
	var page DarkRestoreEventPage
	if !eventRE.MatchString(event) || limit < 1 || limit > 50 || offset < 0 || offset > 100000 {
		return page, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return page, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_import_receipts_v1 WHERE event_id=?", event).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return page, ErrNotFound
	} else if err != nil {
		return page, err
	}
	var r DarkRestoreImportReceiptV1
	if err = decodeDarkRestoreReceipt(raw, event, &r); err != nil {
		return page, err
	}
	sealed, err := readDarkRestoreArchive(ctx, tx, r)
	if err != nil {
		return page, err
	}
	page = DarkRestoreEventPage{Events: []darkrestore.LegacyEvent{}, Total: r.LegacyEvents, Offset: offset}
	if len(sealed) == 0 {
		return page, nil
	}
	plain, err := openSubscription(key, "darkrestore-event-history", []string{r.EventID, r.SourceSHA256, r.ReviewSHA256}, sealed)
	if err != nil {
		return DarkRestoreEventPage{}, ErrUnavailable
	}
	var events []darkrestore.LegacyEvent
	if json.Unmarshal(plain, &events) != nil || int64(len(events)) != r.LegacyEvents {
		return DarkRestoreEventPage{}, ErrUnavailable
	}
	if offset < len(events) {
		page.Events = events[offset:min(offset+limit, len(events))]
	}
	return page, nil
}

func (s *Store) DarkRestoreHistoryAvailable(ctx context.Context) (bool, error) {
	return darkRestoreSchemaState(ctx, s.db)
}

type RestoreImportSummary struct {
	EventID     string `json:"event_id"`
	CreatedUnix int64  `json:"created_unix"`
	SourceSHA   string `json:"source_sha256"`
}

func (s *Store) RecentDarkRestoreImports(ctx context.Context, token string) ([]RestoreImportSummary, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT event_id,created_unix,source_sha256 FROM darkrestore_import_receipts_v1 ORDER BY created_unix DESC,event_id DESC LIMIT 20")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RestoreImportSummary{}
	for rows.Next() {
		var v RestoreImportSummary
		if err = rows.Scan(&v.EventID, &v.CreatedUnix, &v.SourceSHA); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Reject backup publication or restore if the inventory key cannot recover all
// private history, even when the schema and ciphertext checksums are intact.
func validateDarkRestoreArchiveKeys(ctx context.Context, db *sql.DB, key []byte) error {
	present, err := darkRestoreSchemaState(ctx, db)
	if err != nil || !present {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT a.event_id,a.source_sha256,r.review_sha256,a.record_count,CASE WHEN length(a.sealed_history) BETWEEN 28 AND 33554460 THEN a.sealed_history END FROM darkrestore_event_archives_v1 a LEFT JOIN darkrestore_import_receipts_v1 r ON r.event_id=a.event_id ORDER BY a.event_id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var event, source, review string
		var n int64
		var sealed []byte
		if rows.Scan(&event, &source, &review, &n, &sealed) != nil || n <= 0 || n > 100000 {
			return ErrUnavailable
		}
		plain, err := openSubscription(key, "darkrestore-event-history", []string{event, source, review}, sealed)
		var events []darkrestore.LegacyEvent
		if err != nil || json.Unmarshal(plain, &events) != nil || int64(len(events)) != n {
			return ErrUnavailable
		}
	}
	return rows.Err()
}
