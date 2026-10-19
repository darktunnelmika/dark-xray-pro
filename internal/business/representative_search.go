package business

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RepresentativeSearch is an owner-only, read-only directory request.
// It deliberately cannot change credit balances or representative status.
type RepresentativeSearch struct {
	Query  string
	State  string
	Sort   string
	Limit  int
	Offset int
}

type RepresentativeDirectoryRow struct {
	Representative
	ClientCount   int64 `json:"client_count"`
	ArchivedCount int64 `json:"archived_count"`
}

type RepresentativeSearchPage struct {
	Representatives []RepresentativeDirectoryRow
	Total           int64
	Limit           int
	Offset          int
	HasMore         bool
}

func validRepresentativeSearch(q RepresentativeSearch) bool {
	if q.Limit < 1 || q.Limit > 200 || q.Offset < 0 || q.Offset > 1_000_000 ||
		!utf8.ValidString(q.Query) || utf8.RuneCountInString(q.Query) > 128 {
		return false
	}
	for _, r := range q.Query {
		if unicode.IsControl(r) {
			return false
		}
	}
	switch q.State {
	case "", "all", "enabled", "disabled":
	default:
		return false
	}
	switch q.Sort {
	case "", "id", "newest", "name", "volume", "unlimited":
	default:
		return false
	}
	return true
}

// SearchRepresentatives returns exact result counts and only real client
// records. A single SQLite read transaction ties page and total together.
// Search is literal substring; sorting never interpolates untrusted SQL.
func (s *Store) SearchRepresentatives(ctx context.Context, q RepresentativeSearch) (RepresentativeSearchPage, error) {
	var out RepresentativeSearchPage
	if !validRepresentativeSearch(q) {
		return out, ErrInvalid
	}
	q.Query = strings.TrimSpace(q.Query)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	var clauses []string
	var args []any
	if q.Query != "" {
		clauses = append(clauses, "(instr(lower(r.id),lower(?))>0 OR instr(lower(r.display_name),lower(?))>0)")
		args = append(args, q.Query, q.Query)
	}
	switch q.State {
	case "enabled":
		clauses = append(clauses, "r.enabled=1")
	case "disabled":
		clauses = append(clauses, "r.enabled=0")
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	order := "r.id ASC"
	switch q.Sort {
	case "newest":
		order = "r.created_unix DESC,r.id ASC"
	case "name":
		order = "r.display_name COLLATE NOCASE ASC,r.id ASC"
	case "volume":
		order = "r.volume_credit_bytes DESC,r.id ASC"
	case "unlimited":
		order = "r.unlimited_credit_units DESC,r.id ASC"
	}

	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM representatives r"+where, args...).Scan(&out.Total); err != nil {
		return RepresentativeSearchPage{}, err
	}
	pageArgs := append(append([]any(nil), args...), q.Limit, q.Offset)
	query := `SELECT r.id,r.display_name,r.kind,r.enabled,r.volume_credit_bytes,r.unlimited_credit_units,r.created_unix,
		(SELECT COUNT(*) FROM clients c WHERE c.representative_id=r.id) AS client_count,
		(SELECT COUNT(*) FROM clients c WHERE c.representative_id=r.id AND c.archived=1) AS archived_count
		FROM representatives r` + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	rows, err := tx.QueryContext(ctx, query, pageArgs...)
	if err != nil {
		return RepresentativeSearchPage{}, err
	}
	out.Representatives = make([]RepresentativeDirectoryRow, 0, q.Limit)
	for rows.Next() {
		var item RepresentativeDirectoryRow
		var enabled int
		err = rows.Scan(&item.ID, &item.DisplayName, &item.Kind, &enabled,
			&item.VolumeCreditBytes, &item.UnlimitedCreditUnits, &item.CreatedUnix,
			&item.ClientCount, &item.ArchivedCount)
		if err != nil {
			rows.Close()
			return RepresentativeSearchPage{}, err
		}
		item.Enabled = enabled == 1
		out.Representatives = append(out.Representatives, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return RepresentativeSearchPage{}, err
	}
	if err = rows.Close(); err != nil {
		return RepresentativeSearchPage{}, err
	}
	if err = tx.Commit(); err != nil {
		return RepresentativeSearchPage{}, err
	}
	out.Limit = q.Limit
	out.Offset = q.Offset
	out.HasMore = int64(q.Offset)+int64(len(out.Representatives)) < out.Total
	return out, nil
}
