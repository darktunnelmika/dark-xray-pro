package business

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Pool replacement must preserve Store.Open's connection-local protections.
// ErrBadConn exercises database/sql's public discard path without driver hooks.
func TestStoreReplacementConnectionPreservesPragmas(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "replacement-pragmas.sqlite"))
	if err != nil {
		t.Fatal("open temporary business store:", err)
	}
	defer s.Close()
	if s.db.Stats().MaxOpenConnections != 1 {
		t.Fatal("test requires the store's single-connection pool")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := func(phase string, conn *sql.Conn) {
		t.Helper()
		for _, pragma := range []struct {
			name string
			want int
		}{
			{"busy_timeout", 5000},
			{"foreign_keys", 1},
			{"synchronous", 2}, // FULL
			{"trusted_schema", 0},
		} {
			var got int
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma.name).Scan(&got); err != nil {
				t.Fatalf("%s %s query: %v", phase, pragma.name, err)
			}
			if got != pragma.want {
				t.Errorf("%s %s=%d; want %d", phase, pragma.name, got, pragma.want)
			}
		}
		var mode string
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
			t.Errorf("%s journal_mode=%q; want wal; error=%v", phase, mode, err)
		}
	}
	first, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal("acquire initial connection:", err)
	}
	defer first.Close()
	check("initial", first)
	for _, statement := range []string{
		"CREATE TABLE synthetic_connection_value (value INTEGER NOT NULL)",
		"INSERT INTO synthetic_connection_value VALUES (100)",
	} {
		if _, err := first.ExecContext(ctx, statement); err != nil {
			t.Fatal("initialize synthetic value:", err)
		}
	}
	if err := first.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("discard initial connection: got %v; want driver.ErrBadConn", err)
	}
	// Raw has released and discarded the physical connection. No Store reopen or
	// test-side PRAGMA initialization may repair the replacement before checking.
	fresh, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal("acquire replacement connection:", err)
	}
	defer fresh.Close()
	check("replacement", fresh)
	var value int
	if err := fresh.QueryRowContext(ctx, "SELECT value FROM synthetic_connection_value").Scan(&value); err != nil || value != 100 {
		t.Fatalf("replacement read: value=%d; want 100; error=%v", value, err)
	}
	result, err := fresh.ExecContext(ctx, "UPDATE synthetic_connection_value SET value=value+1")
	if err != nil {
		t.Fatal("replacement write:", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("replacement write affected=%d; want 1; error=%v", affected, err)
	}
	if err := fresh.QueryRowContext(ctx, "SELECT value FROM synthetic_connection_value").Scan(&value); err != nil || value != 101 {
		t.Fatalf("replacement read after write: value=%d; want 101; error=%v", value, err)
	}
	if err := fresh.Close(); err != nil {
		t.Fatal("release replacement connection:", err)
	}
	if got := s.db.Stats().InUse; got != 0 {
		t.Fatalf("connections still in use=%d; want 0", got)
	}
}
