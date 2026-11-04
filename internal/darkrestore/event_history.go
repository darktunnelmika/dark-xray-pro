package darkrestore

import (
	"context"
	"database/sql"
	"math"
	"unicode/utf8"
)

// Source IDs and details are optional only in older offline snapshot layouts.
// Preserve missing fields, repeated events and fractional timestamps exactly.
type LegacyEvent struct {
	ID       *int64  `json:"id,omitempty"`
	SourceID string  `json:"restore_id"`
	Kind     string  `json:"event"`
	Detail   *string `json:"detail,omitempty"`
	At       float64 `json:"at"`
}

func ReadEventHistory(ctx context.Context, path string, review Review) ([]LegacyEvent, error) {
	safe, err := safeSource(path)
	if err != nil {
		return nil, err
	}
	if review.Summary.Events < 0 || review.Summary.Events > 100000 {
		return nil, ErrInspection
	}
	sum, _, err := hashSource(safe)
	if err != nil || sum != review.SourceSHA256 {
		return nil, ErrUnsafeSource
	}
	db, err := sql.Open("sqlite", sourceURI(safe))
	if err != nil {
		return nil, ErrInspection
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, ErrInspection
	}
	defer tx.Rollback()
	columns, err := tx.QueryContext(ctx, "SELECT name FROM pragma_table_info('restore_events')")
	if err != nil {
		return nil, ErrInspection
	}
	id, detail, order := "NULL", "NULL", "rowid"
	for columns.Next() {
		var name string
		if columns.Scan(&name) != nil {
			columns.Close()
			return nil, ErrInspection
		}
		switch name {
		case "id":
			id, order = "id", "id"
		case "detail":
			detail = "CASE WHEN typeof(detail)='text' AND length(CAST(detail AS BLOB))<=65536 THEN detail END"
		case "restore_id", "event", "at":
		default:
			columns.Close()
			return nil, ErrInspection
		}
	}
	err = columns.Err()
	columns.Close()
	if err != nil {
		return nil, ErrInspection
	}
	// Bound oversized values inside SQLite before bringing them into process memory.
	rows, err := tx.QueryContext(ctx, "SELECT "+id+",CASE WHEN typeof(restore_id)='text' AND length(CAST(restore_id AS BLOB))<=1024 THEN restore_id END, CASE WHEN typeof(event)='text' AND length(CAST(event AS BLOB))<=512 THEN event END,"+detail+",CASE WHEN typeof(at) IN ('real','integer') THEN at END FROM restore_events ORDER BY "+order+" LIMIT 100001")
	if err != nil {
		return nil, ErrInspection
	}
	defer rows.Close()
	known := make(map[string]bool, len(review.Clients))
	for _, c := range review.Clients {
		known[c.SourceID] = true
	}
	result := make([]LegacyEvent, 0, review.Summary.Events)
	size := 0
	var lastID int64
	for rows.Next() {
		var e LegacyEvent
		if rows.Scan(&e.ID, &e.SourceID, &e.Kind, &e.Detail, &e.At) != nil || !known[e.SourceID] || !safeLabel(e.Kind) || !utf8.ValidString(e.Kind) || math.IsNaN(e.At) || math.IsInf(e.At, 0) || e.At < 0 || e.At > 253402300799 {
			return nil, ErrInspection
		}
		if id != "NULL" {
			if e.ID == nil || *e.ID <= lastID {
				return nil, ErrInspection
			}
			lastID = *e.ID
		}
		size += len(e.SourceID) + len(e.Kind) + 80
		if detail != "NULL" {
			if e.Detail == nil || !utf8.ValidString(*e.Detail) {
				return nil, ErrInspection
			}
			size += len(*e.Detail)
		}
		if size > 16<<20 || int64(len(result)) >= review.Summary.Events {
			return nil, ErrInspection
		}
		result = append(result, e)
	}
	if rows.Err() != nil || int64(len(result)) != review.Summary.Events {
		return nil, ErrInspection
	}
	if _, err = safeSource(safe); err != nil {
		return nil, err
	}
	sum, _, err = hashSource(safe)
	if err != nil || sum != review.SourceSHA256 {
		return nil, ErrUnsafeSource
	}
	return result, nil
}
