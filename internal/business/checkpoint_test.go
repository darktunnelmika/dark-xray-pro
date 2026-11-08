package business

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBusinessCheckpointDoesNotOccupyAccountingPool(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "checkpoint.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version string
	if err = s.db.QueryRow("select sqlite_version()").Scan(&version); err != nil || version != "3.51.3" {
		t.Fatalf("checkpoint concurrency requires patched SQLite: %s %v", version, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := s.checkpoint.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var syncMode int
	if err = conn.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&syncMode); err != nil || syncMode != 2 {
		t.Fatalf("checkpoint durability: %d %v", syncMode, err)
	}
	// Deliberately occupy the maintenance pool. Accounting must still commit.
	writeCtx, writeCancel := context.WithTimeout(ctx, time.Second)
	defer writeCancel()
	if _, err = s.db.ExecContext(writeCtx, "CREATE TABLE checkpoint_probe (n INTEGER NOT NULL, payload BLOB)"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(writeCtx, "INSERT INTO checkpoint_probe VALUES (7, zeroblob(2097152))"); err != nil {
		t.Fatal(err)
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	_, frames, done, err := s.checkpoint.checkpoint(ctx)
	if err != nil || frames < 1 || done < 1 {
		t.Fatalf("checkpoint did not progress: %d/%d %v", done, frames, err)
	}
	var n int
	if err = s.readOnly.QueryRowContext(ctx, "SELECT n FROM checkpoint_probe").Scan(&n); err != nil || n != 7 {
		t.Fatalf("committed value lost: %d %v", n, err)
	}
	var integrity string
	if err = s.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity: %s %v", integrity, err)
	}
}
