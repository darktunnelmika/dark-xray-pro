package hubcore

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Restoring from the panel happens in two steps. Stage checks the upload
// (decrypting it if needed), opens it as a store and reports what is in it;
// nothing changes yet. Apply asks the HUB to exit; on the next start
// (systemd restarts it) ApplyPendingRestore swaps the staged database in
// before anything opens the live one. The replaced database is kept next
// to it as v2.sqlite.before-restore-<time>.

type RestorePreview struct {
	Clients  int   `json:"clients"`
	Nodes    int   `json:"nodes"`
	Inbounds int   `json:"inbounds"`
	Logins   int   `json:"logins"`
	Bytes    int64 `json:"bytes"`
}

func (s *Store) pendingPath() string { return s.path + ".pending" }

var restoreMu sync.Mutex

// StageRestore validates a backup (gzip, or the encrypted .dkb form) and
// keeps it ready for ApplyRestore.
func (s *Store) StageRestore(ctx context.Context, data []byte, pass string) (RestorePreview, error) {
	restoreMu.Lock()
	defer restoreMu.Unlock()
	if IsEncryptedBackup(data) {
		if pass == "" {
			return RestorePreview{}, fmt.Errorf("%w: this backup is encrypted — enter its password", ErrInvalid)
		}
		plain, err := DecryptBackup(data, pass)
		if err != nil {
			return RestorePreview{}, fmt.Errorf("%w: wrong password or damaged file", ErrInvalid)
		}
		data = plain
	}
	gz, _, err := backupParts(bytes.NewReader(data))
	if err != nil {
		return RestorePreview{}, fmt.Errorf("%w: not a Dark backup (expected .sqlite.gz or .dkb)", ErrInvalid)
	}
	tmp := s.pendingPath() + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return RestorePreview{}, err
	}
	n, err := io.Copy(out, io.LimitReader(gz, 4<<30))
	if err == nil {
		err = out.Sync()
	}
	out.Close()
	if err != nil {
		os.Remove(tmp)
		return RestorePreview{}, fmt.Errorf("%w: damaged backup: %v", ErrInvalid, err)
	}
	st, err := Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return RestorePreview{}, fmt.Errorf("%w: backup does not open as a Dark v2 database: %v", ErrInvalid, err)
	}
	p := RestorePreview{Bytes: n}
	q := func(sql string, v *int) {
		if err == nil {
			err = st.ro.QueryRowContext(ctx, sql).Scan(v)
		}
	}
	q(`SELECT COUNT(*) FROM clients`, &p.Clients)
	q(`SELECT COUNT(*) FROM nodes`, &p.Nodes)
	q(`SELECT COUNT(*) FROM inbounds`, &p.Inbounds)
	q(`SELECT COUNT(*) FROM admins WHERE role IN ('owner','admin','viewer')`, &p.Logins)
	if err == nil && p.Logins == 0 {
		err = fmt.Errorf("it has no owner login, so nobody could sign in after restoring")
	}
	// Fold the WAL back into the file so the staged copy is self-contained.
	if err == nil {
		_, err = st.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	st.Close()
	os.Remove(tmp + "-wal")
	os.Remove(tmp + "-shm")
	if err != nil {
		os.Remove(tmp)
		return RestorePreview{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := os.Rename(tmp, s.pendingPath()); err != nil {
		return RestorePreview{}, err
	}
	return p, nil
}

// CancelRestore drops a staged backup.
func (s *Store) CancelRestore() error {
	err := os.Remove(s.pendingPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// HasPendingRestore reports whether a staged backup waits for Apply.
func (s *Store) HasPendingRestore() bool {
	_, err := os.Stat(s.pendingPath())
	return err == nil
}

// restartHook is how ApplyRestore stops the HUB (main sets a clean exit;
// systemd starts it again).
var restartHook = func() { os.Exit(0) }

// SetRestartHook replaces the exit used after ApplyRestore.
func SetRestartHook(f func()) { restartHook = f }

// ApplyRestore makes the HUB restart into the staged backup.
func (s *Store) ApplyRestore() error {
	if !s.HasPendingRestore() {
		return fmt.Errorf("%w: upload a backup first", ErrInvalid)
	}
	RestartHub()
	return nil
}

// RestartHub exits shortly after the current reply; systemd starts the HUB
// again (used after a restore and to apply panel listener changes).
func RestartHub() {
	go func() {
		time.Sleep(700 * time.Millisecond) // let the reply reach the browser
		restartHook()
	}()
}

// ApplyPendingRestore swaps a staged backup in. Call it before Open.
func ApplyPendingRestore(dbPath string) (ok bool, err error) {
	pending := dbPath + ".pending"
	if _, err := os.Stat(pending); err != nil {
		return false, nil
	}
	marks := rowIDMarks(dbPath)
	defer func() {
		if ok {
			keepRowIDMarks(dbPath, marks)
		}
	}()
	if _, err := os.Stat(dbPath); err == nil {
		old := dbPath + ".before-restore-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(dbPath, old); err != nil {
			return false, err
		}
		for _, x := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(dbPath + x); err == nil {
				_ = os.Rename(dbPath+x, old+x)
			}
		}
	}
	if err := os.Rename(pending, dbPath); err != nil {
		return true, err
	}
	return true, nil
}

// rowIDTables are the tables whose row ids sales bots keep (users,
// templates, groups); newRowID never gives one of their ids out twice.
var rowIDTables = []string{"clients", "products", "inbounds"}

// rowIDMarks reads, from the database about to be replaced, the highest id
// each table has given out.
func rowIDMarks(path string) map[string]int64 {
	marks := map[string]int64{}
	if _, err := os.Stat(path); err != nil {
		return marks
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return marks
	}
	defer db.Close()
	for _, t := range rowIDTables {
		var hw, top int64
		_ = db.QueryRow(`SELECT CAST(value AS INTEGER) FROM settings WHERE key=?`, "rowid_hw_"+t).Scan(&hw)
		_ = db.QueryRow(`SELECT COALESCE(MAX(rowid),0) FROM ` + t).Scan(&top)
		marks[t] = max(hw, top)
	}
	return marks
}

// keepRowIDMarks raises the restored database's marks to what the replaced
// one had given out, so ids of users made after the backup are not reused.
func keepRowIDMarks(path string, marks map[string]int64) {
	if len(marks) == 0 {
		return
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return
	}
	defer db.Close()
	for t, m := range marks {
		var hw int64
		_ = db.QueryRow(`SELECT CAST(value AS INTEGER) FROM settings WHERE key=?`, "rowid_hw_"+t).Scan(&hw)
		if m > hw {
			_, _ = db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "rowid_hw_"+t, fmt.Sprint(m))
		}
	}
}
