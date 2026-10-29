//go:build linux

package backupworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"golang.org/x/sys/unix"
)

func canonical(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnsafe
	}
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafe
		}
		if p == "/" {
			break
		}
	}
	return nil
}

// Executable/configuration ancestors must not be replaceable by the HUB UID.
func trustedAncestors(path string) error {
	if canonical(filepath.Dir(path)) != nil {
		return ErrUnsafe
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || !owner(st, 0) || st.Mode().Perm()&0022 != 0 {
			return ErrUnsafe
		}
		if p == "/" {
			return nil
		}
	}
}

func owner(st os.FileInfo, uid uint32) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uid && (st.IsDir() || s.Nlink == 1)
}

func openChecked(path string, uid uint32, private bool, max int64) (*os.File, error) {
	if canonical(path) != nil {
		return nil, ErrUnsafe
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), path)
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || !owner(st, uid) || st.Mode().Perm()&0022 != 0 ||
		(private && st.Mode().Perm()&0077 != 0) || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Size() < 0 || (max > 0 && st.Size() > max) {
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

func hashFile(ctx context.Context, f *os.File) (string, int64, error) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return "", 0, ErrUnsafe
	}
	h := sha256.New()
	buf := make([]byte, 128<<10)
	var size int64
	for {
		if e := ctx.Err(); e != nil {
			return "", 0, e
		}
		n, e := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			size += int64(n)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", 0, ErrUnsafe
		}
	}
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return "", 0, ErrUnsafe
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func (r *LinuxRuntime) archivePath(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrUnsafe
	}
	return filepath.Join(r.archives, id, "native.backup"), nil
}

// Failed or interrupted CLI staging can contain unencrypted source snapshots.
// Preserve it for explicit operator review, behind the root-owned directory;
// never recursively delete unknown files or publish them as a valid backup.
func (r *LinuxRuntime) SealJobDirectory(id string) error {
	p, e := r.archivePath(id)
	if e != nil {
		return e
	}
	dir := filepath.Dir(p)
	st, e := os.Lstat(dir)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || canonical(dir) != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || (!owner(st, r.uid) && !owner(st, r.rootUID)) {
		return ErrUnsafe
	}
	if os.Chown(dir, int(r.rootUID), int(r.rootGID)) != nil || os.Chmod(dir, 0700) != nil || syncDir(dir) != nil || syncDir(r.archives) != nil {
		return ErrUnsafe
	}
	return nil
}

func (r *LinuxRuntime) OpenArchive(ctx context.Context, a backupcontrol.Archive) (backupcontrol.ReadSeekCloser, error) {
	p, e := r.archivePath(a.JobID)
	if e != nil || a.ID != a.JobID || !a.Downloadable || (a.Verification != "checksum_verified" && a.Verification != "restore_verified") {
		return nil, ErrUnsafe
	}
	d, e := os.Lstat(filepath.Dir(p))
	if e != nil || !d.IsDir() || !owner(d, r.rootUID) || d.Mode().Perm()&0077 != 0 {
		return nil, ErrUnsafe
	}
	f, e := openChecked(p, r.rootUID, true, r.config.ciphertextLimit())
	if e != nil {
		return nil, e
	}
	got, size, e := hashFile(ctx, f)
	if e != nil || got != a.SHA256 || size != a.SizeBytes {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}

// Caller holds the Store's retention/download-pin mutex. Only the exact
// registered file is unlinked; unknown files and directories are preserved.
func (r *LinuxRuntime) RemoveArchive(a backupcontrol.Archive) error {
	p, e := r.archivePath(a.JobID)
	if e != nil || a.ID != a.JobID || !a.Downloadable || (a.Verification != "checksum_verified" && a.Verification != "restore_verified") {
		return ErrUnsafe
	}
	if canonical(filepath.Dir(p)) != nil {
		return ErrUnsafe
	}
	parent, e := os.Lstat(filepath.Dir(p))
	if e != nil || !parent.IsDir() || !owner(parent, r.rootUID) || parent.Mode().Perm()&0077 != 0 {
		return ErrUnsafe
	}
	if _, e = os.Lstat(p); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return ErrUnsafe
	}
	// Root owns the finalized file. Retention's callback must not hash GiB of
	// data while holding the journal lock; check immutable identity and size.
	f, e := openChecked(p, r.rootUID, true, r.config.ciphertextLimit())
	if e != nil {
		return e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || before.Size() != a.SizeBytes {
		return ErrUnsafe
	}
	d, e := os.Open(filepath.Dir(p))
	if e != nil {
		return ErrUnsafe
	}
	defer d.Close()
	current, e := os.Lstat(p)
	if e != nil || !os.SameFile(before, current) {
		return ErrUnsafe
	}
	if e = unix.Unlinkat(int(d.Fd()), "native.backup", 0); e != nil {
		return ErrUnsafe
	}
	return d.Sync()
}
