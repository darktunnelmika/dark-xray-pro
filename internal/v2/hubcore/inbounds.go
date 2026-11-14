package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// InboundRaw is an inbound as stored, for the advanced editor.
type InboundRaw struct {
	Tag      string          `json:"tag"`
	NodeID   string          `json:"node_id"`
	Template json.RawMessage `json:"template"`
	Meta     json.RawMessage `json:"meta"`
	Enabled  bool            `json:"enabled"`
}

func (s *Store) InboundRaw(ctx context.Context, tag string) (InboundRaw, error) {
	var r InboundRaw
	var tpl, meta string
	err := s.ro.QueryRowContext(ctx, `SELECT tag,node_id,template,meta,enabled FROM inbounds WHERE tag=?`, tag).Scan(&r.Tag, &r.NodeID, &tpl, &meta, &r.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.Template, r.Meta = json.RawMessage(tpl), json.RawMessage(meta)
	return r, err
}

// SetInboundEnabled turns an inbound on or off; nodes drop or add it on their
// next sync, and subscriptions stop or start listing it.
func (s *Store) SetInboundEnabled(ctx context.Context, tag string, on bool) error {
	r, err := s.db.ExecContext(ctx, `UPDATE inbounds SET enabled=? WHERE tag=?`, on, tag)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
