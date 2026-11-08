package business

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointUsesPendingFramesNotRetainedAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backlog.sqlite")
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=wal_autocheckpoint(0)&_pragma=busy_timeout(0)"}
	writer, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	exec := func(q string) {
		t.Helper()
		if _, err := writer.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE probe (id INTEGER PRIMARY KEY, payload BLOB)")
	maintenance, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer maintenance.Close()
	maintenance.SetMaxOpenConns(1)
	c := &businessCheckpointer{db: maintenance}
	if err := maintenance.QueryRow("PRAGMA page_size").Scan(&c.pageBytes); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	check := func(at time.Time, want bool) (int, int) {
		t.Helper()
		attempted, busy, frames, done, err := c.checkpointIfNeeded(ctx, at)
		if err != nil || busy != 0 || attempted != want {
			t.Fatalf("attempted=%v want=%v busy=%d frames=%d done=%d err=%v", attempted, want, busy, frames, done, err)
		}
		return frames, done
	}
	exec("INSERT INTO probe VALUES (1,zeroblob(2097152))")
	frames, done := check(now, true)
	if frames < 1 || done != frames {
		t.Fatalf("large batch incomplete: %d/%d", done, frames)
	}
	info, err := os.Stat(path + "-wal")
	if err != nil || info.Size() < 1<<20 {
		t.Fatalf("retained allocation fixture: %v %v", info, err)
	}
	check(now.Add(time.Second), false)
	exec("INSERT INTO probe VALUES (2,zeroblob(1))")
	frames, done = check(now.Add(2*time.Second), false)
	if frames <= done {
		t.Fatal("NOOP unexpectedly checkpointed small batch")
	}
	check(now.Add(61*time.Second), false)
	frames, done = check(now.Add(62*time.Second), true)
	if frames != done {
		t.Fatalf("aged batch incomplete: %d/%d", done, frames)
	}
	check(now.Add(63*time.Second), false)

	// An older reader must remain valid during PASSIVE work. Its unfinished
	// backlog must remain eligible until the reader releases its snapshot.
	reader, err := writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	var count int
	if err := reader.QueryRow("SELECT count(*) FROM probe").Scan(&count); err != nil || count != 2 {
		t.Fatalf("reader: %d %v", count, err)
	}
	if _, err := maintenance.Exec("INSERT INTO probe VALUES (3,zeroblob(2097152))"); err != nil {
		t.Fatal(err)
	}
	frames, done = check(now.Add(64*time.Second), true)
	if done >= frames {
		t.Fatalf("pinned reader did not retain backlog: %d/%d", done, frames)
	}
	if err := reader.QueryRow("SELECT count(*) FROM probe").Scan(&count); err != nil || count != 2 {
		t.Fatalf("snapshot changed: %d %v", count, err)
	}
	if err := reader.Rollback(); err != nil {
		t.Fatal(err)
	}
	frames, done = check(now.Add(65*time.Second), true)
	if done != frames {
		t.Fatalf("released backlog incomplete: %d/%d", done, frames)
	}
	var integrity string
	if err := writer.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity %s %v", integrity, err)
	}
	var syncMode int
	if err := writer.QueryRow("PRAGMA synchronous").Scan(&syncMode); err != nil || syncMode != 2 {
		t.Fatalf("durability %d %v", syncMode, err)
	}
}
