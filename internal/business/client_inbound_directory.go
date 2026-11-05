package business

import (
	"context"
	"database/sql"
	"encoding/json"
)

// These are saved assignments, not a claim of current runtime delivery.
type ClientInboundChoice struct {
	ID       uint64 `json:"id"`
	Name     string `json:"name"`
	Archived bool   `json:"archived"`
	Clients  int64  `json:"clients"`
}

func clientInboundIDs(ctx context.Context, tx *sql.Tx, key []byte, id uint64) ([]string, error) {
	var exists int
	if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='inbound_execution_heads'").Scan(&exists); e != nil {
		return nil, e
	}
	if exists > 0 {
		var managed int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM inbound_execution_heads WHERE inbound_id=?", id).Scan(&managed); e != nil {
			return nil, e
		}
		if managed > 0 {
			if len(key) != 32 {
				return nil, ErrUnavailable
			}
			h, e := readExecutionHead(ctx, tx, key, id)
			if e != nil {
				return nil, e
			}
			return executionClientIDs(h.Bindings.ClientIDs)
		}
	}
	ids := []string{}
	for _, table := range []string{"native_inbound_clients", "native_inbound_plan_promotion_clients"} {
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); e != nil {
			return nil, e
		}
		if exists == 0 {
			continue
		}
		rows, e := tx.QueryContext(ctx, "SELECT client_id FROM "+table+" WHERE inbound_id=?", id)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var value string
			if e = rows.Scan(&value); e != nil {
				rows.Close()
				return nil, e
			}
			ids = append(ids, value)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	unique := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if !unique[id] {
			out = append(out, id)
			unique[id] = true
		}
	}
	return executionClientIDs(out)
}
func (s *Store) ClientInboundChoices(ctx context.Context, key []byte, scope string) ([]ClientInboundChoice, error) {
	if scope != "" && !idRE.MatchString(scope) {
		return nil, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, "SELECT id,name,archived FROM native_inbounds ORDER BY id LIMIT 257")
	if e != nil {
		return nil, e
	}
	items := []ClientInboundChoice{}
	for rows.Next() {
		var c ClientInboundChoice
		if e = rows.Scan(&c.ID, &c.Name, &c.Archived); e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(items) > 256 {
		return nil, ErrUnavailable
	}
	out := []ClientInboundChoice{}
	for _, c := range items {
		ids, e := clientInboundIDs(ctx, tx, key, c.ID)
		if e != nil {
			return nil, e
		}
		encoded, e := json.Marshal(ids)
		if e != nil {
			return nil, e
		}
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients WHERE id IN (SELECT value FROM json_each(?)) AND (?='' OR representative_id=?)", string(encoded), scope, scope).Scan(&c.Clients); e != nil {
			return nil, e
		}
		if scope == "" || c.Clients > 0 {
			out = append(out, c)
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}
