package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ClientSearch is server-side, private and limited to one representative or
// an owner-authorized wider scope. It is never a public user enumeration API.
type ClientSearch struct {
	Summary          bool
	InboundID        uint64
	RepresentativeID string
	GroupID          string
	Origin           string
	Query            string
	State            string
	Plan             string
	Sort             string
	Limit            int
	Offset           int
}
type ClientSearchPage struct {
	Clients []Client
	Total   int64
	Limit   int
	Offset  int
	HasMore bool
}

func validClientSearch(q ClientSearch) bool {
	if q.InboundID > 9007199254740991 {
		return false
	}
	if q.RepresentativeID != "" && !idRE.MatchString(q.RepresentativeID) {
		return false
	}
	if q.GroupID != "" && !idRE.MatchString(q.GroupID) {
		return false
	}
	switch q.Origin {
	case "", "all", "native", "dark_restore":
	default:
		return false
	}
	if q.Limit < 1 || q.Limit > 200 || q.Offset < 0 || q.Offset > 1_000_000 {
		return false
	}
	if !utf8.ValidString(q.Query) || utf8.RuneCountInString(q.Query) > 128 {
		return false
	}
	for _, r := range q.Query {
		if unicode.IsControl(r) {
			return false
		}
	}
	switch q.State {
	case "", "all", "pending", "disabled", "expired", "archived", "other":
	default:
		return false
	}
	switch q.Plan {
	case "", "all", "volume", "unlimited":
	default:
		return false
	}
	switch q.Sort {
	case "", "newest", "oldest", "name", "expiry_soon", "expiry_late":
	default:
		return false
	}
	return true
}

// SearchClients uses one SQLite read transaction so total and page reflect a
// consistent snapshot. Search is literal substring (instr), not user-supplied
// SQL LIKE wildcard or a dynamic ORDER BY clause.
func (s *Store) SearchClients(ctx context.Context, q ClientSearch) (ClientSearchPage, error) {
	return s.searchClients(ctx, nil, q)
}
func (s *Store) SearchClientsWithInbound(ctx context.Context, key []byte, q ClientSearch) (ClientSearchPage, error) {
	return s.searchClients(ctx, key, q)
}
func (s *Store) searchClients(ctx context.Context, key []byte, q ClientSearch) (ClientSearchPage, error) {
	var result ClientSearchPage
	if !validClientSearch(q) {
		return result, ErrInvalid
	}
	q.Query = strings.TrimSpace(q.Query)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 8)
	if q.RepresentativeID != "" {
		clauses = append(clauses, "representative_id=?")
		args = append(args, q.RepresentativeID)
	}
	if q.InboundID != 0 {
		ids, e := clientInboundIDs(ctx, tx, key, q.InboundID)
		if e != nil {
			return result, e
		}
		data, e := json.Marshal(ids)
		if e != nil {
			return result, e
		}
		clauses = append(clauses, "clients.id IN (SELECT value FROM json_each(?))")
		args = append(args, string(data))
	}
	if q.GroupID != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM client_group_members gm WHERE gm.client_id=clients.id AND gm.group_id=?)")
		args = append(args, q.GroupID)
	}
	if q.Origin == "native" || q.Origin == "dark_restore" {
		present, e := darkRestoreSchemaState(ctx, tx)
		if e != nil {
			return ClientSearchPage{}, e
		}
		if present {
			clause := "EXISTS (SELECT 1 FROM darkrestore_import_entries_v1 di WHERE di.client_id=clients.id)"
			if q.Origin == "native" {
				clause = "NOT " + clause
			}
			clauses = append(clauses, clause)
		} else if q.Origin == "dark_restore" {
			clauses = append(clauses, "0=1")
		}
	}
	if q.Query != "" {
		contactQuery := strings.TrimPrefix(q.Query, "@")
		if contactQuery == "" {
			contactQuery = q.Query
		}
		clauses = append(clauses, "(instr(lower(id),lower(?))>0 OR instr(lower(display_name),lower(?))>0 OR instr(lower(COALESCE(cc.telegram_username,'')),lower(?))>0 OR instr(COALESCE(cc.telegram_id,''),?)>0 OR instr(lower(COALESCE(cc.note,'')),lower(?))>0)")
		args = append(args, q.Query, q.Query, contactQuery, q.Query, q.Query)
	}
	if q.Plan != "" && q.Plan != "all" {
		clauses = append(clauses, "plan_kind=?")
		args = append(args, q.Plan)
	}
	now := time.Now().UTC().Unix()
	switch q.State {
	case "archived":
		clauses = append(clauses, "archived=1")
	case "disabled":
		clauses = append(clauses, "archived=0 AND deployment_state='disabled'")
	case "expired":
		clauses = append(clauses, "archived=0 AND deployment_state!='disabled' AND expires_unix<=?")
		args = append(args, now)
	case "pending":
		clauses = append(clauses, "archived=0 AND deployment_state='pending_node_apply' AND expires_unix>?")
		args = append(args, now)
	case "other":
		clauses = append(clauses, "archived=0 AND deployment_state!='disabled' AND deployment_state!='pending_node_apply' AND expires_unix>?")
		args = append(args, now)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	// Sorting is hard-coded, never copied from input.
	order := "created_unix DESC,id ASC"
	switch q.Sort {
	case "oldest":
		order = "created_unix ASC,id ASC"
	case "name":
		order = "display_name COLLATE NOCASE ASC,id ASC"
	case "expiry_soon":
		order = "expires_unix ASC,id ASC"
	case "expiry_late":
		order = "expires_unix DESC,id ASC"
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+clientReadSource+where, args...).Scan(&result.Total); err != nil {
		return ClientSearchPage{}, err
	}
	pageArgs := append(append([]any(nil), args...), q.Limit, q.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT "+clientReadColumns+" FROM "+clientReadSource+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		return ClientSearchPage{}, err
	}
	result.Clients = make([]Client, 0, q.Limit)
	for rows.Next() {
		client, e := clientScan(rows)
		if e != nil {
			rows.Close()
			return ClientSearchPage{}, e
		}
		result.Clients = append(result.Clients, client)
	}
	if e := rows.Err(); e != nil {
		rows.Close()
		return ClientSearchPage{}, e
	}
	if e := rows.Close(); e != nil {
		return ClientSearchPage{}, e
	}
	if q.Summary {
		if e := s.enrichClientDirectory(ctx, tx, key, result.Clients); e != nil {
			return ClientSearchPage{}, e
		}
	}
	if e := tx.Commit(); e != nil {
		return ClientSearchPage{}, e
	}
	result.Limit = q.Limit
	result.Offset = q.Offset
	result.HasMore = int64(q.Offset)+int64(len(result.Clients)) < result.Total
	return result, nil
}
