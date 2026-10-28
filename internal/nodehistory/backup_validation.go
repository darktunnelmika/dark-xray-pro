//go:build linux

package nodehistory

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ValidateOfflineBackup accepts an existing standalone private snapshot. It
// never initializes a database, checkpoints a WAL, repairs a schema or records
// a sample. The caller must separately fence the original writers and copy
// their committed SQLite state before using this validator.
func ValidateOfflineBackup(ctx context.Context, path string) (result error) {
	if ctx == nil {
		return ErrUnsafe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			if ctx.Err() != nil {
				result = ctx.Err()
			} else {
				result = ErrUnsafe
			}
		}
	}()
	before, err := offlineHistoryPath(path)
	if err != nil {
		return ErrUnsafe
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=trusted_schema(0)&_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return ErrUnsafe
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer func() {
		if err := db.Close(); err != nil && result == nil {
			result = ErrUnsafe
		}
	}()
	var version, app int
	if db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version) != nil || version != 1 ||
		db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app) != nil || app != historyApplicationID {
		return ErrUnsafe
	}
	// Reuse the exact schema emitted by Open, without calling its mutating
	// connection setup or CREATE statements. No unknown tables/indexes/triggers
	// are accepted, including names that merely resemble SQLite's own prefix.
	canonical := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	want := map[string]string{
		"samples":            canonical(strings.Replace(samplesTableDDL, "CREATE TABLE IF NOT EXISTS", "CREATE TABLE", 1)),
		"samples_minute_idx": canonical("CREATE INDEX samples_minute_idx ON samples(minute)"),
	}
	rows, err := db.QueryContext(ctx, "SELECT name,sql FROM sqlite_schema")
	if err != nil {
		return ErrUnsafe
	}
	for rows.Next() {
		var name, ddl string
		if rows.Scan(&name, &ddl) != nil || want[name] == "" || canonical(ddl) != want[name] {
			rows.Close()
			return ErrUnsafe
		}
		delete(want, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(want) != 0 {
		return ErrUnsafe
	}
	var integrity string
	if db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity) != nil || integrity != "ok" {
		return ErrUnsafe
	}
	rows, err = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrUnsafe
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if bad || err != nil {
		return ErrUnsafe
	}
	after, err := offlineHistoryPath(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ErrUnsafe
	}
	return ctx.Err()
}

func offlineHistoryPath(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnsafe
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnsafe
		}
		if p == filepath.Dir(path) {
			id, ok := st.Sys().(*syscall.Stat_t)
			if !ok || id.Uid != uint32(os.Geteuid()) || st.Mode().Perm()&0077 != 0 {
				return nil, ErrUnsafe
			}
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrUnsafe
		}
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), "offline-history-snapshot")
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0177 != 0 || st.Size() < 1 || st.Size() > MaxHistoryDBSize {
		return nil, ErrUnsafe
	}
	id, ok := st.Sys().(*syscall.Stat_t)
	if !ok || id.Uid != uint32(os.Geteuid()) || id.Nlink != 1 {
		return nil, ErrUnsafe
	}
	return st, nil
}
