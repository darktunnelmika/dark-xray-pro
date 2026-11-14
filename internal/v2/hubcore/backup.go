package hubcore

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backups are a consistent SQLite snapshot (VACUUM INTO) of the whole v2
// store: nodes, inbounds with keys, clients, resellers, credit ledger and
// usage. A snapshot is small because usage is kept as totals, not events.

// SnapshotTo writes a consistent copy of the store to path.
func (s *Store) SnapshotTo(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %s exists", ErrInvalid, path)
	}
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}

// WriteBackup streams a gzip-compressed snapshot to w.
func (s *Store) WriteBackup(ctx context.Context, tmpDir string, w io.Writer) error {
	f, err := os.CreateTemp(tmpDir, "snapshot-*.sqlite")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	os.Remove(name) // VACUUM INTO needs a path that does not exist yet
	defer os.Remove(name)
	if err := s.SnapshotTo(ctx, name); err != nil {
		return err
	}
	src, err := os.Open(name)
	if err != nil {
		return err
	}
	defer src.Close()
	gz := gzip.NewWriter(w)
	if _, err := io.Copy(gz, src); err != nil {
		return err
	}
	return gz.Close()
}

// AutoBackup writes dir/dark-v2-<time>.sqlite.gz and keeps the newest keep.
func (s *Store) AutoBackup(ctx context.Context, dir string, keep int) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "dark-v2-"+s.now().UTC().Format("20060102-150405")+".sqlite.gz")
	tmp := path + ".partial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	err = s.WriteBackup(ctx, dir, f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "dark-v2-") && strings.HasSuffix(e.Name(), ".sqlite.gz") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep && keep > 0 {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
	return path, nil
}

// RestoreBackup replaces dbPath with a gzip snapshot. The HUB must be
// stopped; the previous database is kept next to it with a timestamp.
func RestoreBackup(backupPath, dbPath string) error {
	in, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("not a dark-v2 backup: %w", err)
	}
	tmp := dbPath + ".restore"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, gz); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	out.Close()
	// Validate it opens as a v2 store before swapping it in.
	st, err := Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("backup does not open: %w", err)
	}
	if _, err := st.Nodes(context.Background()); err != nil {
		st.Close()
		os.Remove(tmp)
		return fmt.Errorf("backup is not a dark-v2 store: %w", err)
	}
	st.Close()
	if _, err := os.Stat(dbPath); err == nil {
		old := dbPath + ".before-restore-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(dbPath, old); err != nil {
			return err
		}
		os.Remove(dbPath + "-wal")
		os.Remove(dbPath + "-shm")
	}
	os.Remove(tmp + "-wal")
	os.Remove(tmp + "-shm")
	return os.Rename(tmp, dbPath)
}
