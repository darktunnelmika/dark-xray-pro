package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Backup drill: a backup is only trusted once it has been restored. The
// drill makes a real encrypted full backup, decrypts it, restores it into
// a throwaway directory (never the live database or /etc/dark-hub2),
// opens the copy and compares it with the live HUB. The result is kept in
// the setting backup_drill and reported to the owner.

// DrillResult is the outcome of one drill.
type DrillResult struct {
	Unix   int64               `json:"unix"`
	OK     bool                `json:"ok"`
	Bytes  int                 `json:"bytes"`
	Millis int64               `json:"millis"`
	Error  string              `json:"error,omitempty"`
	Counts map[string][2]int64 `json:"counts"` // table -> [live, restored]
	Files  []string            `json:"files"`
}

var drillTables = []string{"clients", "client_inbounds", "nodes", "inbounds", "reps", "products", "orders", "credit_ledger", "admins", "tg_users", "toman_wallet", "settings"}

// BackupDrill runs one drill with the backup password and records it.
func (s *Store) BackupDrill(ctx context.Context, pass string) DrillResult {
	start := time.Now()
	r := DrillResult{Unix: s.now().Unix(), Counts: map[string][2]int64{}}
	fail := func(format string, a ...any) DrillResult {
		r.Error = fmt.Sprintf(format, a...)
		r.Millis = time.Since(start).Milliseconds()
		s.saveDrill(ctx, r)
		return r
	}
	live := map[string]int64{}
	for _, t := range drillTables {
		live[t] = s.tableCount(ctx, s, t)
	}
	data, err := s.FullBackup(ctx, pass)
	if err != nil {
		return fail("making the backup: %v", err)
	}
	r.Bytes = len(data)
	dir, err := os.MkdirTemp(s.tmpDir(), "drill-")
	if err != nil {
		return fail("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	enc := filepath.Join(dir, "backup.dkb")
	if err := os.WriteFile(enc, data, 0o600); err != nil {
		return fail("writing the backup: %v", err)
	}
	plain, err := DecryptFile(enc, pass)
	if err != nil {
		return fail("decrypting: %v", err)
	}
	defer os.Remove(plain)
	files, err := RestoreFull(plain, filepath.Join(dir, "v2.sqlite"), filepath.Join(dir, "etc"), "")
	if err != nil {
		return fail("restoring: %v", err)
	}
	r.Files = files
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	for _, f := range []string{"ca.pem", "server.pem", "server.key", "admin-token"} {
		if !have[f] {
			return fail("the backup lacks %s", f)
		}
	}
	copyStore, err := Open(filepath.Join(dir, "v2.sqlite"))
	if err != nil {
		return fail("opening the restored copy: %v", err)
	}
	defer copyStore.Close()
	for _, t := range drillTables {
		got := s.tableCount(ctx, copyStore, t)
		r.Counts[t] = [2]int64{live[t], got}
		// Rows added while the drill ran are fine; rows missing are not.
		if got < 0 || got < live[t]-drillSlack(live[t]) || (live[t] > 0 && got == 0) {
			return fail("table %s: live %d, restored %d", t, live[t], got)
		}
	}
	r.OK = true
	r.Millis = time.Since(start).Milliseconds()
	s.saveDrill(ctx, r)
	return r
}

// drillSlack tolerates a few rows removed during the drill (expired orders,
// a deleted client) on busy tables.
func drillSlack(n int64) int64 {
	if n/200 > 3 {
		return n / 200
	}
	return 3
}

func (s *Store) tableCount(ctx context.Context, st *Store, table string) int64 {
	var n int64
	if err := st.ro.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		return -1
	}
	return n
}

func (s *Store) saveDrill(ctx context.Context, r DrillResult) {
	raw, _ := json.Marshal(r)
	_ = s.SetSetting(ctx, "backup_drill", string(raw))
}

// LastDrill is the last recorded drill (Unix 0 = never).
func (s *Store) LastDrill(ctx context.Context) DrillResult {
	var r DrillResult
	_ = json.Unmarshal([]byte(s.Setting(ctx, "backup_drill")), &r)
	return r
}
