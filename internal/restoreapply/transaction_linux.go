//go:build linux

// Package restoreapply installs an authenticated snapshot into a stopped
// installation. A journal outside financial data and a writer fence cover all
// partially installed states. This package never runs a service.
package restoreapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"golang.org/x/sys/unix"
)

var ErrUnsafe = errors.New("restore_transaction_unsafe")
var ErrStarted = errors.New("restore_start_barrier_crossed")
var idRE = regexp.MustCompile(`^[a-f0-9]{32}$`)
var hashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)

const maxBytes int64 = 64 << 30

type Replacement struct {
	// Destination comes from the trusted current installation, not the archive.
	Destination string
	Source      string
	Present     bool
	Size        int64
	SHA256      string
}
type image struct {
	Present bool   `json:"present"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}
type entry struct {
	Destination string `json:"destination"`
	Old         image  `json:"old"`
	New         image  `json:"new"`
}
type plan struct {
	Version int     `json:"version"`
	ID      string  `json:"id"`
	UID     int     `json:"uid"`
	GID     int     `json:"gid"`
	Root    string  `json:"root"`
	Entries []entry `json:"entries"`
}
type Transaction struct {
	root, dir, id string
	uid, gid      int
	lock          *os.File
	plan          *plan
	afterWrite    func(int) error // private failure injection used only by tests
}

func canonical(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if p == "/" {
			return true
		}
	}
}
func owned(st os.FileInfo, uid int) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == uid && (st.IsDir() || s.Nlink == 1)
}
func checked(path string, uid int) (*os.File, error) {
	if !canonical(path) {
		return nil, ErrUnsafe
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), path)
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || !owned(st, uid) || st.Mode().Perm()&0077 != 0 || st.Size() < 0 || st.Size() > maxBytes {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func readSmall(path string, uid int) ([]byte, error) {
	f, e := checked(path, uid)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if e != nil || len(b) > 256<<10 {
		return nil, ErrUnsafe
	}
	return b, nil
}
func publish(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	p := filepath.Dir(path)
	f, e := os.CreateTemp(p, ".restore-record-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Link(f.Name(), path); e != nil {
		return e
	}
	if e = os.Remove(f.Name()); e != nil {
		return e
	}
	return syncDir(p)
}
func validRel(p string) bool {
	return p != "" && p != "." && !filepath.IsAbs(p) && filepath.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../") &&
		!strings.ContainsAny(p, "\x00\r\n") && p != hubstate.LockName && p != hubstate.InventoryName && p != hubstate.QuarantineName && p != hubstate.RestoreGuardName
}
func validImage(v image) bool {
	if !v.Present {
		return v.Size == 0 && v.SHA256 == ""
	}
	return v.Size >= 0 && v.Size <= maxBytes && hashRE.MatchString(v.SHA256)
}
func capture(ctx context.Context, path string, uid int, out *os.File) (image, error) {
	if _, e := os.Lstat(path); errors.Is(e, os.ErrNotExist) {
		return image{}, nil
	} else if e != nil {
		return image{}, ErrUnsafe
	}
	f, e := checked(path, uid)
	if e != nil {
		return image{}, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil {
		return image{}, e
	}
	h := sha256.New()
	var w io.Writer = h
	if out != nil {
		w = io.MultiWriter(out, h)
	}
	buf := make([]byte, 128<<10)
	var n int64
	for {
		if ctx.Err() != nil {
			return image{}, ctx.Err()
		}
		count, re := f.Read(buf)
		if count > 0 {
			n += int64(count)
			if n > maxBytes {
				return image{}, ErrUnsafe
			}
			if _, e = w.Write(buf[:count]); e != nil {
				return image{}, e
			}
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return image{}, re
		}
	}
	after, e := f.Stat()
	current, ce := os.Lstat(path)
	if e != nil || ce != nil || !os.SameFile(before, current) || before.Size() != n || after.Size() != n || before.ModTime() != after.ModTime() {
		return image{}, ErrUnsafe
	}
	return image{Present: true, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// Open holds the original target lock without replacing its inode. Both paths
// must exist and be private; the operation directory is coordinator-owned.
func Open(root, dir, id string, uid, gid int) (*Transaction, error) {
	if !idRE.MatchString(id) || uid < 0 || gid < 0 || !canonical(root) || !canonical(dir) || root == "/" || dir == root {
		return nil, ErrUnsafe
	}
	rel, e := filepath.Rel(root, dir)
	if e != nil || rel != ".." && !strings.HasPrefix(rel, "../") {
		return nil, ErrUnsafe
	}
	st, e := os.Stat(root)
	if e != nil || !st.IsDir() || !owned(st, uid) || st.Mode().Perm()&0077 != 0 {
		return nil, ErrUnsafe
	}
	st, e = os.Stat(dir)
	if e != nil || !st.IsDir() || !owned(st, os.Geteuid()) || st.Mode().Perm()&0077 != 0 {
		return nil, ErrUnsafe
	}
	lock, e := checked(filepath.Join(root, hubstate.LockName), uid)
	if e != nil {
		return nil, e
	}
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		lock.Close()
		return nil, ErrUnsafe
	}
	t := &Transaction{root: root, dir: dir, id: id, uid: uid, gid: gid, lock: lock}
	fail := func() (*Transaction, error) { t.Close(); return nil, ErrUnsafe }
	if _, e = os.Lstat(filepath.Join(root, hubstate.QuarantineName)); !errors.Is(e, os.ErrNotExist) {
		return fail()
	}
	if e = t.checkLock(); e != nil {
		return fail()
	}
	if b, e := readSmall(filepath.Join(dir, "plan.json"), os.Geteuid()); e == nil {
		var p plan
		d := json.NewDecoder(strings.NewReader(string(b)))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || p.Version != 1 || p.Root != root || p.ID != id || p.UID != uid || p.GID != gid || len(p.Entries) == 0 || len(p.Entries) > 128 {
			return fail()
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return fail()
		}
		seen := map[string]bool{}
		for _, v := range p.Entries {
			if !validRel(v.Destination) || seen[v.Destination] || !validImage(v.Old) || !validImage(v.New) {
				return fail()
			}
			seen[v.Destination] = true
		}
		t.plan = &p
	} else if _, e = os.Lstat(filepath.Join(dir, "plan.json")); !errors.Is(e, os.ErrNotExist) {
		return fail()
	}
	return t, nil
}
func (t *Transaction) Close() error {
	if t.lock == nil {
		return nil
	}
	e := t.lock.Close()
	t.lock = nil
	return e
}
func (t *Transaction) checkLock() error {
	if t.lock == nil {
		return ErrUnsafe
	}
	a, e := t.lock.Stat()
	b, be := os.Lstat(filepath.Join(t.root, hubstate.LockName))
	if e != nil || be != nil || !os.SameFile(a, b) || !canonical(t.root) {
		return ErrUnsafe
	}
	return nil
}
func (t *Transaction) intent(name string) bool {
	_, e := os.Lstat(filepath.Join(t.dir, name))
	return !errors.Is(e, os.ErrNotExist)
}
func (t *Transaction) guard() error {
	if e := t.checkLock(); e != nil {
		return e
	}
	path := filepath.Join(t.root, hubstate.RestoreGuardName)
	if _, e := os.Lstat(path); errors.Is(e, os.ErrNotExist) {
		return publish(path, map[string]string{"restore_id": t.id})
	}
	b, e := readSmall(path, os.Geteuid())
	if e != nil {
		return e
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil || len(m) != 1 || m["restore_id"] != t.id {
		return ErrUnsafe
	}
	return nil
}
func (t *Transaction) Prepare(ctx context.Context, replacements []Replacement) error {
	if t.plan != nil || t.intent("start-restored.json") || t.intent("start-original.json") || len(replacements) == 0 || len(replacements) > 128 {
		return ErrUnsafe
	}
	if e := CheckSpace(t.root, t.dir, replacements, t.uid); e != nil {
		return e
	}
	if e := t.guard(); e != nil {
		return e
	}
	p := plan{Version: 1, ID: t.id, UID: t.uid, GID: t.gid, Root: t.root}
	seen := map[string]bool{}
	var budget int64
	for i, r := range replacements {
		wanted := image{Present: r.Present, Size: r.Size, SHA256: r.SHA256}
		if !validRel(r.Destination) || seen[r.Destination] || !validImage(wanted) {
			return ErrUnsafe
		}
		seen[r.Destination] = true
		dest := filepath.Join(t.root, r.Destination)
		if !r.Present {
			if _, err := os.Lstat(dest); errors.Is(err, os.ErrNotExist) {
				p.Entries = append(p.Entries, entry{Destination: r.Destination})
				continue
			}
		}
		if !canonical(filepath.Dir(dest)) {
			return ErrUnsafe
		}
		st, e := os.Stat(filepath.Dir(dest))
		if e != nil || !st.IsDir() || !owned(st, t.uid) || st.Mode().Perm()&0077 != 0 {
			return ErrUnsafe
		}
		oldFile, e := os.OpenFile(filepath.Join(t.dir, oldName(i)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		old, e := capture(ctx, dest, t.uid, oldFile)
		se := oldFile.Sync()
		ce := oldFile.Close()
		if e != nil || se != nil || ce != nil {
			return ErrUnsafe
		}
		budget += old.Size + wanted.Size
		if budget > 2*maxBytes {
			return ErrUnsafe
		}
		if wanted.Present {
			f, e := os.OpenFile(filepath.Join(t.dir, newName(i)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			got, e := capture(ctx, r.Source, os.Geteuid(), f)
			se := f.Sync()
			ce := f.Close()
			if e != nil || se != nil || ce != nil || got != wanted {
				return ErrUnsafe
			}
		}
		p.Entries = append(p.Entries, entry{Destination: r.Destination, Old: old, New: wanted})
	}
	if e := publish(filepath.Join(t.dir, "plan.json"), p); e != nil {
		return e
	}
	t.plan = &p
	return nil
}
func oldName(i int) string { return fileName("original", i) }
func newName(i int) string { return fileName("candidate", i) }
func fileName(prefix string, i int) string {
	const digits = "0123456789"
	return prefix + "-" + string([]byte{digits[(i/100)%10], digits[(i/10)%10], digits[i%10]})
}
func (t *Transaction) replace(ctx context.Context, i int, restoreOld bool) error {
	e := t.plan.Entries[i]
	want, other, source := e.New, e.Old, newName(i)
	if restoreOld {
		want, other, source = e.Old, e.New, oldName(i)
	}
	dest := filepath.Join(t.root, e.Destination)
	got, err := capture(ctx, dest, t.uid, nil)
	if err != nil {
		return err
	}
	if got == want {
		return nil
	}
	if got != other {
		return ErrUnsafe
	}
	parent := filepath.Dir(dest)
	if !canonical(parent) {
		return ErrUnsafe
	}
	fd, err := unix.Open(parent, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return ErrUnsafe
	}
	dir := os.NewFile(uintptr(fd), parent)
	defer dir.Close()
	ds, err := dir.Stat()
	current, ce := os.Lstat(parent)
	if err != nil || ce != nil || !os.SameFile(ds, current) || !owned(ds, t.uid) || ds.Mode().Perm()&0077 != 0 {
		return ErrUnsafe
	}
	if !want.Present {
		if err = unix.Unlinkat(fd, filepath.Base(dest), 0); err != nil {
			return ErrUnsafe
		}
		return dir.Sync()
	}
	f, err := os.CreateTemp(parent, ".panel-restore-")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		return err
	}
	img, err := capture(ctx, filepath.Join(t.dir, source), os.Geteuid(), f)
	if err != nil || img != want {
		return ErrUnsafe
	}
	if err = f.Chown(t.uid, t.gid); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if now, err := capture(ctx, dest, t.uid, nil); err != nil || now != got {
		return ErrUnsafe
	}
	cur, err := os.Lstat(parent)
	if err != nil || !os.SameFile(ds, cur) {
		return ErrUnsafe
	}
	flags := uint(0)
	if !got.Present {
		flags = unix.RENAME_NOREPLACE
	}
	if err = unix.Renameat2(fd, filepath.Base(f.Name()), fd, filepath.Base(dest), flags); err != nil {
		return err
	}
	return dir.Sync()
}
func (t *Transaction) Apply(ctx context.Context) error {
	if t.plan == nil || t.intent("start-restored.json") || t.intent("start-original.json") {
		return ErrUnsafe
	}
	if e := t.guard(); e != nil {
		return e
	}
	for i := range t.plan.Entries {
		if e := t.replace(ctx, i, false); e != nil {
			return e
		}
		if t.afterWrite != nil {
			if e := t.afterWrite(i); e != nil {
				return e
			}
		}
	}
	for _, e := range t.plan.Entries {
		got, err := capture(ctx, filepath.Join(t.root, e.Destination), t.uid, nil)
		if err != nil || got != e.New {
			return ErrUnsafe
		}
	}
	return nil
}
func (t *Transaction) Rollback(ctx context.Context) error {
	if t.intent("start-restored.json") || t.intent("start-original.json") {
		return ErrStarted
	}
	if e := t.guard(); e != nil {
		return e
	}
	if t.plan != nil {
		for i := len(t.plan.Entries) - 1; i >= 0; i-- {
			if e := t.replace(ctx, i, true); e != nil {
				return e
			}
		}
	}
	return nil
}

// ReleaseForStart persists an irreversible start intent before removing the
// writer fence. The coordinator must persist its matching phase first. After
// restored=true, automatic rollback must never rewind newly accepted traffic.
func (t *Transaction) ReleaseForStart(restored bool) error {
	if e := t.checkLock(); e != nil {
		return e
	}
	name, opposite := "start-original.json", "start-restored.json"
	if restored {
		name, opposite = opposite, name
	}
	if t.intent(opposite) {
		return ErrStarted
	}
	path := filepath.Join(t.dir, name)
	if !t.intent(name) {
		if e := publish(path, map[string]string{"restore_id": t.id}); e != nil {
			return e
		}
	} else {
		b, e := readSmall(path, os.Geteuid())
		if e != nil {
			return e
		}
		var v map[string]string
		if json.Unmarshal(b, &v) != nil || len(v) != 1 || v["restore_id"] != t.id {
			return ErrUnsafe
		}
	}
	guard := filepath.Join(t.root, hubstate.RestoreGuardName)
	if _, e := os.Lstat(guard); errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e := t.guard(); e != nil {
		return e
	}
	if e := os.Remove(guard); e != nil {
		return e
	}
	return syncDir(t.root)
}
