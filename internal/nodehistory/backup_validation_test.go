//go:build linux

package nodehistory

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func offlineHistoryFixture(t *testing.T) string {
	t.Helper()
	path := historyPath(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	store, err := Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(context.Background(), metricNode(now), strings.Repeat("a", 32)); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func mutateOfflineHistory(t *testing.T, path string, statements ...string) {
	t.Helper()
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOfflineHistoryValidationPreservesSnapshot(t *testing.T) {
	path := offlineHistoryFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ValidateOfflineBackup(context.Background(), path); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("snapshot bytes changed")
	}
	current, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(current.ModTime()) || info.Mode() != current.Mode() {
		t.Fatal("snapshot metadata changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("validator created a sidecar or file")
	}
}

func TestOfflineHistoryRejectsForeignOrIncompleteSchema(t *testing.T) {
	for name, statements := range map[string][]string{
		"wrong-version":         {"PRAGMA user_version=2"},
		"wrong-app":             {"PRAGMA application_id=0"},
		"missing-index":         {"DROP INDEX samples_minute_idx"},
		"different-index":       {"DROP INDEX samples_minute_idx", "CREATE INDEX samples_minute_idx ON samples(cpu)"},
		"unknown-table":         {"CREATE TABLE foreign_secret(value TEXT)"},
		"similar-system-prefix": {"CREATE TABLE sqliteXextra(value TEXT)"},
		"unknown-trigger":       {"CREATE TRIGGER foreign_trigger AFTER INSERT ON samples BEGIN SELECT 1; END"},
	} {
		t.Run(name, func(t *testing.T) {
			path := offlineHistoryFixture(t)
			mutateOfflineHistory(t, path, statements...)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateOfflineBackup(context.Background(), path); err != ErrUnsafe {
				t.Fatal("unsupported schema accepted", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid snapshot was repaired")
			}
		})
	}
}

func TestOfflineHistoryRejectsUnsafePathsAndSidecars(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "fifo", "file-symlink", "parent-symlink", "hardlink", "public-file", "public-parent", "oversized", "corrupt", "-wal", "-shm", "-journal"} {
		t.Run(kind, func(t *testing.T) {
			path := offlineHistoryFixture(t)
			switch kind {
			case "missing":
				path += ".missing"
			case "empty":
				if err := os.Truncate(path, 0); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				path += ".fifo"
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				link := path + ".link"
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			case "parent-symlink":
				link := filepath.Join(filepath.Dir(historyPath(t)), "alias")
				if err := os.Symlink(filepath.Dir(path), link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, filepath.Base(path))
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "public-file":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "public-parent":
				if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(path, MaxHistoryDBSize+1); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("private invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path+kind, []byte("uncheckpointed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := ValidateOfflineBackup(context.Background(), path); err != ErrUnsafe {
				t.Fatal("unsafe snapshot accepted", err)
			}
			if kind == "missing" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing database was created")
				}
			}
		})
	}
}

func TestOfflineHistoryCancellationAndStaticErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateOfflineBackup(ctx, "/not-opened"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if err := ValidateOfflineBackup(nil, "/not-opened"); err != ErrUnsafe {
		t.Fatal("nil context accepted", err)
	}
	secret := "/private/DO_NOT_ECHO_ROW_OR_PATH.sqlite"
	if err := ValidateOfflineBackup(context.Background(), secret); err != ErrUnsafe || strings.Contains(err.Error(), secret) {
		t.Fatal("raw error exposed", err)
	}
}
