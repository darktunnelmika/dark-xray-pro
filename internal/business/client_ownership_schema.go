package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Ownership is management authority, not a purchase or a replacement payer.
// This independently-versioned family is installed only by an explicit opt-in.
var ownershipSchemaV1 = []string{
	`CREATE TABLE client_ownership_meta_v1(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE client_ownership_origins_v1(client_id TEXT PRIMARY KEY REFERENCES clients(id),client_uuid TEXT NOT NULL UNIQUE,representative_id TEXT NOT NULL REFERENCES representatives(id))`,
	`CREATE TABLE client_ownership_receipts_v1(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),actor_user_id TEXT NOT NULL REFERENCES auth_users(id),request_json BLOB NOT NULL,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)`,
	`CREATE TABLE client_ownership_entries_v1(client_id TEXT NOT NULL REFERENCES client_ownership_origins_v1(client_id),revision INTEGER NOT NULL CHECK(revision BETWEEN 1 AND 9007199254740991),event_id TEXT NOT NULL REFERENCES client_ownership_receipts_v1(event_id),previous_representative_id TEXT NOT NULL REFERENCES representatives(id),representative_id TEXT NOT NULL REFERENCES representatives(id),previous_group_id TEXT NOT NULL,group_id TEXT NOT NULL,usage_present INTEGER NOT NULL CHECK(usage_present IN(0,1)),usage_complete INTEGER NOT NULL CHECK(usage_complete IN(0,1)),uplink INTEGER NOT NULL CHECK(typeof(uplink)='integer' AND uplink>=0),downlink INTEGER NOT NULL CHECK(typeof(downlink)='integer' AND downlink>=0),holds INTEGER NOT NULL CHECK(holds>=0),PRIMARY KEY(client_id,revision),UNIQUE(event_id,client_id),CHECK(previous_representative_id!=representative_id),CHECK(uplink<=9223372036854775807-downlink))`,
	`CREATE INDEX client_ownership_event_v1 ON client_ownership_entries_v1(event_id,client_id)`,
}

func init() {
	for _, table := range []string{"client_ownership_meta_v1", "client_ownership_origins_v1", "client_ownership_receipts_v1", "client_ownership_entries_v1"} {
		for _, action := range []string{"UPDATE", "DELETE"} {
			ownershipSchemaV1 = append(ownershipSchemaV1, "CREATE TRIGGER "+table+"_no_"+strings.ToLower(action)+" BEFORE "+action+" ON "+table+" BEGIN SELECT RAISE(ABORT,'client ownership history immutable'); END")
		}
	}
}

func ownershipSchemaState(ctx context.Context, q sellerQueryV2) (bool, error) {
	var count int
	if e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'client_ownership_*'`).Scan(&count); e != nil {
		return false, ErrUnavailable
	}
	if count == 0 {
		return false, nil
	}
	if count != len(ownershipSchemaV1) {
		return false, ErrUnavailable
	}
	var version int
	if e := q.QueryRowContext(ctx, `SELECT version FROM client_ownership_meta_v1 WHERE id=1`).Scan(&version); e != nil || version != 1 {
		return false, ErrUnavailable
	}
	for _, ddl := range ownershipSchemaV1 {
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		var actual string
		if e := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); e != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
func (s *Store) EnableClientOwnershipV1(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	present, e := ownershipSchemaState(ctx, tx)
	if e != nil {
		return e
	}
	if !present {
		for _, ddl := range ownershipSchemaV1 {
			if _, e = tx.ExecContext(ctx, ddl); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO client_ownership_meta_v1 VALUES(1,1)`); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Returns the original identity owner only through a complete, immutable chain.
// It is used for pre-transfer subscription proofs and historical funding, never
// to authorize management. Live access always checks clients.representative_id.
func ownershipOrigin(ctx context.Context, q sellerQueryV2, c Client) (string, int64, error) {
	present, e := ownershipSchemaState(ctx, q)
	if e != nil {
		return "", 0, e
	}
	if !present {
		return c.RepresentativeID, 0, nil
	}
	return ownershipOriginKnown(ctx, q, c)
}
func ownershipOriginKnown(ctx context.Context, q sellerQueryV2, c Client) (string, int64, error) {
	var uuid, origin, current string
	e := q.QueryRowContext(ctx, `SELECT o.client_uuid,o.representative_id,c.representative_id FROM client_ownership_origins_v1 o JOIN clients c ON c.id=o.client_id WHERE o.client_id=?`, c.ID).Scan(&uuid, &origin, &current)
	if errors.Is(e, sql.ErrNoRows) {
		return c.RepresentativeID, 0, nil
	}
	if e != nil || uuid != c.UUID {
		return "", 0, ErrUnavailable
	}
	var highest, count, broken int64
	if e = q.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0),COUNT(*) FROM client_ownership_entries_v1 WHERE client_id=?`, c.ID).Scan(&highest, &count); e != nil || highest < 1 || count != highest {
		return "", 0, ErrUnavailable
	}
	var first, last string
	if e = q.QueryRowContext(ctx, `SELECT previous_representative_id FROM client_ownership_entries_v1 WHERE client_id=? AND revision=1`, c.ID).Scan(&first); e != nil || first != origin {
		return "", 0, ErrUnavailable
	}
	if e = q.QueryRowContext(ctx, `SELECT representative_id FROM client_ownership_entries_v1 WHERE client_id=? AND revision=?`, c.ID, highest).Scan(&last); e != nil || last != current {
		return "", 0, ErrUnavailable
	}
	if e = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_ownership_entries_v1 n LEFT JOIN client_ownership_entries_v1 p ON p.client_id=n.client_id AND p.revision=n.revision-1 WHERE n.client_id=? AND n.revision>1 AND (p.revision IS NULL OR n.previous_representative_id!=p.representative_id)`, c.ID).Scan(&broken); e != nil || broken != 0 {
		return "", 0, ErrUnavailable
	}
	return origin, highest, nil
}

func rejectOwnershipEvent(ctx context.Context, tx *sql.Tx, event string) error {
	present, e := ownershipSchemaState(ctx, tx)
	if e != nil || !present {
		return e
	}
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_ownership_receipts_v1 WHERE event_id=?`, event).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return ErrConflict
	}
	return nil
}
