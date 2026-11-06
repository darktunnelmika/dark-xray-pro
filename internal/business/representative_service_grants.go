package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type RepresentativeServiceGrant struct {
	RepresentativeID string `json:"representative_id"`
	InboundID        uint64 `json:"inbound_id"`
	Revision         int64  `json:"revision"`
	Enabled          bool   `json:"enabled"`
}
type ServiceGrantChange struct {
	EventID          string `json:"event_id"`
	RepresentativeID string `json:"representative_id"`
	InboundID        uint64 `json:"inbound_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
	Reason           string `json:"reason"`
}

var serviceGrantSchema = []string{
	`CREATE TABLE IF NOT EXISTS representative_service_grants(event_id TEXT PRIMARY KEY,representative_id TEXT NOT NULL REFERENCES representatives(id),inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),revision INTEGER NOT NULL CHECK(revision BETWEEN 1 AND 9007199254740991),enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),actor_id TEXT NOT NULL REFERENCES auth_users(id),request TEXT NOT NULL,created_unix INTEGER NOT NULL,UNIQUE(representative_id,inbound_id,revision))`,
	`CREATE TRIGGER IF NOT EXISTS service_grants_no_update BEFORE UPDATE ON representative_service_grants BEGIN SELECT RAISE(ABORT,'immutable service grant'); END`,
	`CREATE TRIGGER IF NOT EXISTS service_grants_no_delete BEFORE DELETE ON representative_service_grants BEGIN SELECT RAISE(ABORT,'immutable service grant'); END`,
}

func ensureRepresentativeServiceGrants(db *sql.DB) error {
	present, e := serviceGrantSchemaState(context.Background(), db)
	if e != nil {
		return e
	}
	if present {
		return validateServiceGrantData(context.Background(), db)
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range serviceGrantSchema {
		if _, e = tx.Exec(q); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// The same transaction is required when this grant is used for command admission.
func serviceGrantTx(ctx context.Context, tx *sql.Tx, rep string, inbound uint64) (RepresentativeServiceGrant, error) {
	g := RepresentativeServiceGrant{RepresentativeID: rep, InboundID: inbound}
	e := tx.QueryRowContext(ctx, `SELECT revision,enabled FROM representative_service_grants WHERE representative_id=? AND inbound_id=? ORDER BY revision DESC LIMIT 1`, rep, inbound).Scan(&g.Revision, &g.Enabled)
	if errors.Is(e, sql.ErrNoRows) {
		e = nil
	}
	return g, e
}
func (s *Store) RepresentativeServiceGrant(ctx context.Context, rep string, inbound uint64) (RepresentativeServiceGrant, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return RepresentativeServiceGrant{}, e
	}
	defer tx.Rollback()
	g, e := serviceGrantTx(ctx, tx, rep, inbound)
	if e != nil {
		return g, e
	}
	return g, tx.Commit()
}
func (s *Store) ChangeRepresentativeServiceGrant(ctx context.Context, actor string, in ServiceGrantChange) (RepresentativeServiceGrant, error) {
	var zero RepresentativeServiceGrant
	if len(in.EventID) < 16 || len(in.EventID) > 128 || in.RepresentativeID == "" || in.InboundID == 0 || in.InboundID > 9007199254740991 || in.ExpectedRevision < 0 || in.ExpectedRevision >= 9007199254740991 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 512 {
		return zero, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	var ok int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM auth_users WHERE id=? AND role='owner' AND disabled=0`, actor).Scan(&ok); e != nil {
		return zero, e
	}
	if ok != 1 {
		return zero, ErrUnauthenticated
	}
	request, _ := json.Marshal(in)
	var priorActor, priorRequest string
	e = tx.QueryRowContext(ctx, `SELECT actor_id,request FROM representative_service_grants WHERE event_id=?`, in.EventID).Scan(&priorActor, &priorRequest)
	if e == nil {
		if priorActor != actor || priorRequest != string(request) {
			return zero, ErrConflict
		}
		return RepresentativeServiceGrant{RepresentativeID: in.RepresentativeID, InboundID: in.InboundID, Revision: in.ExpectedRevision + 1, Enabled: in.Enabled}, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return zero, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM representatives WHERE id=? AND kind='representative'`, in.RepresentativeID).Scan(&ok); e != nil {
		return zero, e
	}
	if ok != 1 {
		return zero, ErrInvalid
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM native_inbounds WHERE id=?`, in.InboundID).Scan(&ok); e != nil {
		return zero, e
	}
	if ok != 1 {
		return zero, ErrInvalid
	}
	g, e := serviceGrantTx(ctx, tx, in.RepresentativeID, in.InboundID)
	if e != nil {
		return zero, e
	}
	if g.Revision != in.ExpectedRevision {
		return zero, ErrConflict
	}
	if g.Enabled == in.Enabled {
		return zero, ErrInvalid
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO representative_service_grants(event_id,representative_id,inbound_id,revision,enabled,actor_id,request,created_unix) VALUES(?,?,?,?,?,?,?,?)`, in.EventID, in.RepresentativeID, in.InboundID, g.Revision+1, in.Enabled, actor, string(request), time.Now().Unix())
	if e != nil {
		return zero, e
	}
	g.Revision++
	g.Enabled = in.Enabled
	return g, tx.Commit()
}
