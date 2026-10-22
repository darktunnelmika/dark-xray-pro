package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"time"
)

// Public audit metadata only. Private candidate bytes remain in the encrypted queue.
type MeteredHandoverAudit struct {
	MeteredHandoverView
	DrainCommand             *MeteredRouteCommandView `json:"drain_command,omitempty"`
	ApplyCommand             *MeteredRouteCommandView `json:"apply_command,omitempty"`
	HistoricalHealthEvidence *MeteredHealthEvidence   `json:"historical_health_evidence,omitempty"`
	HealthIntents            []MeteredHealthIntent    `json:"health_intents,omitempty"`
	HealthRounds             []MeteredHealthRound     `json:"health_rounds"`
}
type MeteredHandoverPage struct {
	Operations []MeteredHandoverView `json:"operations"`
	NextCursor string                `json:"next_cursor"`
}

func (s *Store) MeteredHandoverAudit(ctx context.Context, key []byte, id string) (MeteredHandoverAudit, error) {
	out := MeteredHandoverAudit{HealthRounds: []MeteredHealthRound{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.MeteredHandoverView, _, err = readMeteredHandover(ctx, tx, id)
	if err != nil {
		return out, err
	}
	for _, step := range []struct {
		id   string
		dest **MeteredRouteCommandView
	}{{out.DrainCommandID, &out.DrainCommand}, {out.ApplyCommandID, &out.ApplyCommand}} {
		if step.id == "" {
			continue
		}
		row, err := readMeteredRouteCommand(ctx, tx, key, step.id)
		if err != nil {
			return out, err
		}
		*step.dest = &row.view
	}
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return out, err
	}
	if usageledger.HasHealthSchema(version) {
		var v MeteredHealthEvidence
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT r.challenge_id,r.handover_id,r.receipt,r.accepted FROM pro_health_receipts r WHERE r.handover_id=?`, id).Scan(&v.ChallengeID, &v.HandoverID, &raw, &v.Accepted)
		if err == nil {
			var p mx.ProtocolHealthReceipt
			if json.Unmarshal([]byte(raw), &p) != nil {
				return out, ErrUnavailable
			}
			v.RequestSHA, v.ReceiptSHA = p.RequestSHA, meteredHash(p)
			out.HistoricalHealthEvidence = &v
		} else if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	if usageledger.HasHealthQueueSchema(version) {
		rows, e := tx.QueryContext(ctx, `SELECT id,handover_id,node_id,installation_id,inbound_id,endpoint_sha,expires FROM pro_health_intents WHERE handover_id=? ORDER BY created DESC LIMIT 50`, id)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var v MeteredHealthIntent
			if e = rows.Scan(&v.ID, &v.HandoverID, &v.NodeID, &v.InstallationID, &v.InboundID, &v.EndpointSHA, &v.Expires); e != nil {
				rows.Close()
				return out, e
			}
			out.HealthIntents = append(out.HealthIntents, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	if usageledger.HasHealthRoundsSchema(version) {
		rows, e := tx.QueryContext(ctx, `SELECT id FROM pro_health_rounds WHERE handover_id=? ORDER BY created DESC,id DESC LIMIT 50`, id)
		if e != nil {
			return out, e
		}
		ids := []string{}
		for rows.Next() {
			var round string
			if e = rows.Scan(&round); e != nil {
				rows.Close()
				return out, e
			}
			ids = append(ids, round)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		for _, round := range ids {
			r, e := readHealthRound(ctx, tx, round)
			if e != nil {
				return out, e
			}
			out.HealthRounds = append(out.HealthRounds, assessHealthRound(ctx, tx, key, r, time.Now().UnixNano()))
		}
	}
	return out, tx.Commit()
}

// The opaque cursor is an immutable operation ID, never a browser-rounded nanosecond.
func (s *Store) ListMeteredHandovers(ctx context.Context, before string) (MeteredHandoverPage, error) {
	out := MeteredHandoverPage{Operations: []MeteredHandoverView{}}
	if before != "" && !routeID.MatchString(before) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	query := `SELECT id FROM native_metered_handovers`
	args := []any{}
	if before != "" {
		var created int64
		if err = tx.QueryRowContext(ctx, `SELECT created FROM native_metered_handovers WHERE id=?`, before).Scan(&created); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				err = ErrNotFound
			}
			return out, err
		}
		query += ` WHERE created<? OR (created=? AND id<?)`
		args = append(args, created, created, before)
	}
	rows, err := tx.QueryContext(ctx, query+` ORDER BY created DESC,id DESC LIMIT 51`, args...)
	if err != nil {
		return out, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(ids) > 50 {
		ids = ids[:50]
		out.NextCursor = ids[49]
	}
	for _, id := range ids {
		v, _, err := readMeteredHandover(ctx, tx, id)
		if err != nil {
			return out, err
		}
		out.Operations = append(out.Operations, v)
	}
	return out, tx.Commit()
}
