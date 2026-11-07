package business

import (
	"context"
	"database/sql"
	"net/url"
	"time"
)

// Open only after the writer has validated/migrated the private database.
// Each replacement connection remains mode=ro and query_only; no shared cache,
// immutable mode, long-lived snapshot, session cache, or extra writer is used.
// Restore closes the Store and reopens both pools with the replaced database.
func openBusinessReadPool(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=trusted_schema(OFF)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var journal string
	var readOnly int
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err == nil && journal != "wal" {
		err = ErrUnavailable
	}
	if err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&readOnly)
	}
	if err == nil && readOnly != 1 {
		err = ErrUnavailable
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Display and session verification may read the last committed state while a
// durable writer is busy. Accounting, commands, lease renewal, and all mutations
// still use the single FULL-durability writer and retain fail-closed behavior.
func (s *Store) readSnapshot(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.readOnly == nil {
		return nil, ErrUnavailable
	}
	return s.readOnly.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
}
