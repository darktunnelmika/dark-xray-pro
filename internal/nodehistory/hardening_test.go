package nodehistory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryApplicationIdentityAndTrueByteBudget(t *testing.T) {
	path := historyPath(t)
	h, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	var appID int
	var pageSize, pageCap int64
	if err := h.db.QueryRow("PRAGMA application_id").Scan(&appID); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow("PRAGMA max_page_count").Scan(&pageCap); err != nil {
		t.Fatal(err)
	}
	if appID != historyApplicationID || pageSize <= 0 || pageCap <= 0 || pageCap*pageSize > MaxHistoryDBSize {
		t.Fatalf("database identity/size guard: app=%x page=%d cap=%d", appID, pageSize, pageCap)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	// Upgrade an authentic first-release PRO v1 schema that has no marker.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec("PRAGMA application_id=0"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(path, nil)
	if err != nil {
		t.Fatalf("legacy PRO v1 schema must remain restart-compatible: %v", err)
	}
	if err := upgraded.db.QueryRow("PRAGMA application_id").Scan(&appID); err != nil || appID != historyApplicationID {
		t.Fatalf("missing successful marker upgrade: %x %v", appID, err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}

	// A different SQLite application marker may not be adopted.
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec("PRAGMA application_id=99"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path, nil); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("foreign application ID accepted: %v", err)
	}
}

func TestRejectForeignVersionOneNamedSamplesTable(t *testing.T) {
	path := historyPath(t)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// A foreign database could have its own "samples" table with a "minute"
	// column; checking the name alone cannot establish PRO ownership.
	if _, err = raw.Exec("CREATE TABLE samples(node_id TEXT, minute INTEGER, value TEXT); PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path, nil); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("foreign samples schema was adopted: %v", err)
	}
}

func TestHourlyPurgeBoundsRowsAndInstallationCache(t *testing.T) {
	now := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	h, err := Open(historyPath(t), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	install := strings.Repeat("a", 32)
	ancient := metricNode(now)
	ancient.NodeID = "old-pro-installation"
	if err := h.Record(context.Background(), ancient, install); err != nil {
		t.Fatal(err)
	}
	oldKey := ancient.NodeID + ":" + install
	if _, ok := h.latest[oldKey]; !ok {
		t.Fatal("first minute not cached")
	}
	now = now.Add(25 * time.Hour)
	newNode := metricNode(now)
	newNode.NodeID = "new-pro-installation"
	if err := h.Record(context.Background(), newNode, install); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.latest[oldKey]; ok {
		t.Fatal("removed installation retained forever in memory")
	}
	var count int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM samples").Scan(&count); err != nil || count != 1 {
		t.Fatalf("retention did not purge older rows: %d %v", count, err)
	}
	day, err := h.Query(context.Background(), ancient.NodeID, install, "24h")
	if err != nil || len(day) != 0 {
		t.Fatalf("out-of-window installation still queryable: %v %v", day, err)
	}
}

func TestSQLiteLockWaitIsBoundedAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	h, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")

	start := time.Now()
	n := metricNode(time.Now())
	err = h.Record(context.Background(), n, strings.Repeat("a", 32))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("write lock incorrectly accepted a concurrent write")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("a busy SQLite file stalled Agent telemetry for %v: %v", elapsed, err)
	}
	if _, err = conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err = h.Record(context.Background(), n, strings.Repeat("a", 32)); err != nil {
		t.Fatalf("history must recover after disk lock clears: %v", err)
	}
}
