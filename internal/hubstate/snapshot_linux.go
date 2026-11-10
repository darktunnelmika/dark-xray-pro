//go:build linux

package hubstate

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

func CollectSources(l *Lock, inv Inventory) ([]Source, error) {
	if e := ValidateInventory(l, inv); e != nil {
		return nil, e
	}
	out := []Source{}
	var total int64
	for _, r := range inv.Resources {
		f, e := openPrivate(r.Path, syscall.O_RDONLY, 0, r.Role != "agent_tls_cert")
		if errors.Is(e, os.ErrNotExist) && !r.Required {
			continue
		}
		if e != nil {
			return nil, ErrUnsafe
		}
		st, e := f.Stat()
		f.Close()
		if e != nil || st.Size() < 0 || st.Size() > maxRoleSize(r.Role) || total > maxSnapshotPayload-st.Size() {
			return nil, ErrUnsafe
		}
		total += st.Size()
		out = append(out, Source{Name: r.Name, Role: r.Role, Path: r.Path, Size: st.Size()})
	}
	f, e := openPrivate(filepath.Join(l.root, InventoryName), syscall.O_RDONLY, 0, true)
	if e != nil {
		return nil, ErrUnsafe
	}
	st, e := f.Stat()
	f.Close()
	if e != nil {
		return nil, ErrUnsafe
	}
	out = append(out, Source{Name: "inventory.json", Role: "inventory", Path: filepath.Join(l.root, InventoryName), Size: st.Size()})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Snapshot requires the offline lifetime lock. It never opens an original
// SQLite database through SQLite: committed WAL/rollback state is first copied
// into private staging, then a real VACUUM INTO produces a standalone snapshot.
// No Open/migration or accounting mutation from business is called.
func Snapshot(ctx context.Context, l *Lock, inv Inventory, tempDir string) ([]Source, error) {
	return SnapshotWithBusinessTransform(ctx, l, inv, tempDir, nil)
}

// SnapshotWithBusinessTransform applies an optional projection only to the
// private standalone business copy, before inventory sizes are finalized.
func SnapshotWithBusinessTransform(ctx context.Context, l *Lock, inv Inventory, tempDir string, transform func(context.Context, string) error, snapshotReady ...func() error) (result []Source, err error) {
	if len(snapshotReady) > 1 {
		return nil, ErrUnsafe
	}
	if e := ValidateInventory(l, inv); e != nil {
		return nil, e
	}
	if canonicalPath(tempDir) != nil || tempDir == l.root || inside(tempDir, l.root) {
		return nil, ErrUnsafe
	}
	st, e := os.Lstat(tempDir)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !owned(st) {
		return nil, ErrUnsafe
	}
	entries, e := os.ReadDir(tempDir)
	if e != nil || len(entries) != 0 {
		return nil, ErrUnsafe
	}
	releaseRuntime, e := holdRuntimeStopped(ctx, inv)
	if e != nil {
		return nil, e
	}
	var runtimeOnce sync.Once
	release := func() { runtimeOnce.Do(releaseRuntime) }
	defer release()
	releaseBotRaw, e := holdBotStopped(inv)
	if e != nil {
		return nil, e
	}
	var botOnce sync.Once
	releaseBot := func() { botOnce.Do(releaseBotRaw) }
	defer releaseBot()
	sources, e := CollectSources(l, inv)
	if e != nil {
		return nil, e
	}
	snapshot := inv
	snapshot.Resources = append([]Resource(nil), inv.Resources...)
	snapshot.SnapshotFiles = []SnapshotFile{}
	for i := range snapshot.Resources {
		snapshot.Resources[i].Present = false
	}
	for _, s := range sources {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if s.Role == "inventory" {
			continue
		}
		dest := filepath.Join(tempDir, filepath.FromSlash(s.Name))
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return nil, e
		}
		if s.Role == "business" || s.Role == "node_history" || s.Role == "owner_report_db" || s.Role == "telegram_bot_db" {
			if e = snapshotSQLite(ctx, s.Path, dest, tempDir); e != nil {
				return nil, e
			}
		} else if e = copyRegular(ctx, s.Path, dest, s.Role != "agent_tls_cert"); e != nil {
			return nil, e
		}
		st, e = os.Lstat(dest)
		if e != nil {
			return nil, e
		}
		result = append(result, Source{Name: s.Name, Role: s.Role, Path: dest, Size: st.Size()})
		snapshot.SnapshotFiles = append(snapshot.SnapshotFiles, SnapshotFile{Name: s.Name, Role: s.Role, Size: st.Size()})
		for i := range snapshot.Resources {
			if snapshot.Resources[i].Role == s.Role {
				snapshot.Resources[i].Present = true
			}
		}
	}
	// All authoritative resources are now copied and fsynced. From here on,
	// only private staging paths and the captured inventory are read. The
	// coordinator may restart HUB before validation, projection and encryption;
	// failure of those later steps still refuses archive publication.
	if len(snapshotReady) == 1 && snapshotReady[0] != nil {
		releaseBot()
		release()
		if e = snapshotReady[0](); e != nil {
			return nil, e
		}
	}
	for i := range result {
		if result[i].Role == "business" && transform != nil {
			if e = transform(ctx, result[i].Path); e != nil {
				return nil, e
			}
			st, e = os.Lstat(result[i].Path)
			if e != nil {
				return nil, e
			}
			result[i].Size = st.Size()
			snapshot.SnapshotFiles[i].Size = st.Size()
		}
	}
	if e = ValidateSnapshot(ctx, tempDir, snapshot); e != nil {
		return nil, e
	}
	invPath := filepath.Join(tempDir, "inventory.json")
	if e = writeJSON(invPath, snapshot); e != nil {
		return nil, e
	}
	st, e = os.Lstat(invPath)
	if e != nil {
		return nil, e
	}
	result = append(result, Source{Name: "inventory.json", Role: "inventory", Path: invPath, Size: st.Size()})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func copyRegular(ctx context.Context, source, dest string, private bool) error {
	in, e := openPrivate(source, syscall.O_RDONLY, 0, private)
	if e != nil {
		return ErrUnsafe
	}
	defer in.Close()
	before, e := in.Stat()
	if e != nil || before.Size() < 0 || before.Size() > 64<<30 {
		return ErrUnsafe
	}
	out, e := openPrivate(dest, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600, true)
	if e != nil {
		return ErrUnsafe
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(dest)
		}
	}()
	buffer := make([]byte, 128<<10)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, re := in.Read(buffer)
		if n > 0 {
			if _, e = out.Write(buffer[:n]); e != nil {
				return e
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	after, e := in.Stat()
	if e != nil || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return ErrUnsafe
	}
	current, e := os.Lstat(source)
	if e != nil || !os.SameFile(before, current) {
		return ErrUnsafe
	}
	if e = out.Sync(); e != nil {
		return e
	}
	if e = out.Close(); e != nil {
		return e
	}
	ok = true
	return nil
}

func snapshotSQLite(ctx context.Context, source, dest, stagingRoot string) error {
	// Bound all temporary SQLite inputs, not just the main database. Stopped
	// WAL can be larger than the database. Reserve space for both the copied
	// inputs and the standalone output before creating either.
	var working int64
	for _, suffix := range []string{"", "-wal", "-journal"} {
		f, e := openPrivate(source+suffix, syscall.O_RDONLY, 0, true)
		if errors.Is(e, os.ErrNotExist) && suffix != "" {
			continue
		}
		if e != nil {
			return ErrUnsafe
		}
		st, e := f.Stat()
		f.Close()
		if e != nil || st.Size() < 0 || st.Size() > maxSnapshotPayload || working > maxSnapshotPayload-st.Size() {
			return ErrUnsafe
		}
		working += st.Size()
	}
	var fs syscall.Statfs_t
	if syscall.Statfs(stagingRoot, &fs) != nil || fs.Bsize <= 0 || fs.Bavail < uint64((2*working+(1<<20)+fs.Bsize-1)/fs.Bsize) {
		return ErrUnsafe
	}
	work, e := os.MkdirTemp(stagingRoot, "sqlite-work-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(work)
	copyPath := filepath.Join(work, "source.sqlite")
	if e = copyRegular(ctx, source, copyPath, true); e != nil {
		return e
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		if _, e = os.Lstat(source + suffix); errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return ErrUnsafe
		}
		if e = copyRegular(ctx, source+suffix, copyPath+suffix, true); e != nil {
			return e
		}
	}
	// SHM is a reconstructible lock/cache, not data. Validate its type when
	// present, but never transplant SQLite lock bytes to another database.
	if f, e := openPrivate(source+"-shm", syscall.O_RDONLY, 0, true); e == nil {
		f.Close()
	} else if !errors.Is(e, os.ErrNotExist) {
		return ErrUnsafe
	}
	u := url.URL{Scheme: "file", Path: copyPath, RawQuery: "mode=rw&_pragma=busy_timeout(1000)&_pragma=trusted_schema(0)"}
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return ErrUnsafe
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	out, e := openPrivate(dest, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600, true)
	if e != nil {
		return ErrUnsafe
	}
	out.Close()
	if _, e = db.ExecContext(ctx, "VACUUM main INTO ?", dest); e != nil {
		return ErrUnsafe
	}
	f, e := openPrivate(dest, syscall.O_RDWR, 0, true)
	if e != nil {
		return ErrUnsafe
	}
	defer f.Close()
	return f.Sync()
}

func holdBotStopped(inv Inventory) (func(), error) {
	var held []*os.File
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = syscall.Flock(int(held[i].Fd()), syscall.LOCK_UN)
			_ = held[i].Close()
		}
	}
	fail := func(err error) (func(), error) { release(); return nil, err }
	for _, r := range inv.Resources {
		if r.Role != "owner_report_db" && r.Role != "telegram_bot_db" {
			continue
		}
		if _, e := os.Lstat(r.Path); errors.Is(e, os.ErrNotExist) && !r.Required {
			entries, de := os.ReadDir(filepath.Dir(r.Path))
			if errors.Is(de, os.ErrNotExist) || de == nil && len(entries) == 0 {
				continue
			}
			return fail(ErrUnsafe)
		} else if e != nil {
			return fail(ErrUnsafe)
		}
		f, e := openPrivate(r.Path+".lock", syscall.O_RDONLY, 0, true)
		if e != nil {
			return fail(ErrUnsafe)
		}
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			f.Close()
			return fail(ErrBusy)
		}
		held = append(held, f)
	}
	return release, nil
}
