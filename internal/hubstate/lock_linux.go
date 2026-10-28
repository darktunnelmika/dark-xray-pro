//go:build linux

package hubstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type Lock struct {
	root string
	file *os.File
}

// CheckQuarantine checks each supplied location and all its ancestors without
// following symlinks. It is safe before any writer constructor is called.
func CheckQuarantine(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		p, e := filepath.Abs(path)
		if e != nil {
			return ErrUnsafe
		}
		if canonicalPath(p) != nil {
			return ErrUnsafe
		}
		for {
			if _, e := os.Lstat(filepath.Join(p, QuarantineName)); e == nil {
				return ErrQuarantined
			} else if !errors.Is(e, os.ErrNotExist) && !errors.Is(e, syscall.ENOTDIR) {
				return ErrUnsafe
			}
			if p == filepath.Dir(p) {
				break
			}
			p = filepath.Dir(p)
		}
	}
	return nil
}

func canonicalPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnsafe
	}
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafe
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return ErrUnsafe
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

// Acquire is a lifetime exclusive fence. The production CLI must use
// ProductionRoot, not a caller-selected alternative lock for the same files.
func Acquire(root string) (*Lock, error) {
	if root == "/" || canonicalPath(root) != nil {
		return nil, ErrUnsafe
	}
	if e := CheckQuarantine(root); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, ErrUnsafe
	}
	st, e := os.Lstat(root)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !owned(st) {
		return nil, ErrUnsafe
	}
	f, e := openPrivate(filepath.Join(root, LockName), syscall.O_RDWR|syscall.O_CREAT, 0600, true)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, ErrBusy
	}
	l := &Lock{root: root, file: f}
	if e = CheckQuarantine(root); e != nil {
		l.Close()
		return nil, e
	}
	return l, nil
}
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
func (l *Lock) valid() error {
	if l == nil || l.file == nil || canonicalPath(l.root) != nil {
		return ErrUnsafe
	}
	a, e := l.file.Stat()
	if e != nil {
		return ErrUnsafe
	}
	b, e := os.Lstat(filepath.Join(l.root, LockName))
	if e != nil || !os.SameFile(a, b) {
		return ErrUnsafe
	}
	return CheckQuarantine(l.root)
}
func owned(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid()) && (st.IsDir() || s.Nlink == 1)
}
func openPrivate(path string, flags int, mode uint32, private bool) (*os.File, error) {
	if canonicalPath(path) != nil {
		return nil, ErrUnsafe
	}
	fd, e := syscall.Open(path, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, mode)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "private-hub-state")
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || !owned(st) || st.Mode().Perm()&0022 != 0 || private && st.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}
func readPrivate(path string, limit int64, private bool) ([]byte, error) {
	f, e := openPrivate(path, syscall.O_RDONLY, 0, private)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, ErrUnsafe
	}
	return b, nil
}

func ValidateInventory(l *Lock, inv Inventory) error {
	if e := l.valid(); e != nil {
		return e
	}
	if inv.Root != l.root || ValidateArchiveInventory(inv) != nil {
		return ErrUnsafe
	}
	for _, r := range inv.Resources {
		if canonicalPath(r.Path) != nil {
			return ErrUnsafe
		}
		if e := CheckQuarantine(r.Path); e != nil {
			return e
		}
	}
	return nil
}
func LoadInventory(l *Lock) (Inventory, error) {
	var inv Inventory
	if e := l.valid(); e != nil {
		return inv, e
	}
	raw, e := readPrivate(filepath.Join(l.root, InventoryName), 1<<20, true)
	if e != nil {
		return inv, ErrUnsafe
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&inv) != nil || d.Decode(&extra) != io.EOF {
		return inv, ErrUnsafe
	}
	return inv, ValidateInventory(l, inv)
}

func WriteInventory(l *Lock, inv Inventory) error {
	if e := ValidateInventory(l, inv); e != nil {
		return e
	}
	if inv.SnapshotFiles != nil {
		return ErrUnsafe
	}
	for i := range inv.Resources {
		r := &inv.Resources[i]
		f, e := openPrivate(r.Path, syscall.O_RDONLY, 0, r.Role != "agent_tls_cert")
		if errors.Is(e, os.ErrNotExist) && !r.Required {
			r.Present = false
			continue
		}
		if e != nil {
			return ErrUnsafe
		}
		r.Present = true
		f.Close()
	}
	if e := ValidateArchiveInventory(inv); e != nil {
		return e
	}
	return writeJSON(filepath.Join(l.root, InventoryName), inv)
}

// InvalidateInventory must run before the first write on every participating
// startup, including startups with backup disabled or later initialization
// failures. An old descriptor must not certify a changed storage layout.
func InvalidateInventory(l *Lock) error {
	if e := l.valid(); e != nil {
		return e
	}
	path := filepath.Join(l.root, InventoryName)
	f, e := openPrivate(path, syscall.O_RDONLY, 0, true)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return ErrUnsafe
	}
	f.Close()
	if e = os.Remove(path); e != nil {
		return e
	}
	d, e := os.Open(l.root)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

func writeJSON(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil || len(b) > 1<<20 {
		return ErrUnsafe
	}
	f, e := os.CreateTemp(filepath.Dir(path), "inventory-tmp-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(append(b, '\n'))
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer dir.Close()
	return dir.Sync()
}
