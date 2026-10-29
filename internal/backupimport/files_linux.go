//go:build linux

package backupimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

func openDirectory(path string, uid uint32) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), "backup-inbox")
	st, err := f.Stat()
	if err != nil || !privateDirectory(st, uid) {
		_ = f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}

func checkedInfo(f *os.File, uid uint32, limit int64) (os.FileInfo, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, ErrUnsafe
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 ||
		stat.Uid != uid || stat.Nlink != 1 || st.Size() < 0 || limit > 0 && st.Size() > limit {
		return nil, ErrUnsafe
	}
	return st, nil
}

func openFileAt(dir *os.File, name string, uid uint32, limit int64) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), name)
	if _, err := checkedInfo(f, uid, limit); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func sameStat(info os.FileInfo, st *unix.Stat_t) bool {
	x, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(x.Dev) == uint64(st.Dev) && x.Ino == st.Ino && x.Mode == st.Mode && x.Uid == st.Uid && x.Nlink == st.Nlink
}

func unchangedAt(dir *os.File, name string, before os.FileInfo) bool {
	var st unix.Stat_t
	return unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW) == nil && sameStat(before, &st) &&
		st.Size == before.Size() && st.Mtim.Sec == before.ModTime().Unix() && st.Mtim.Nsec == int64(before.ModTime().Nanosecond())
}

func (s *Store) uploadDirectory(id string, create bool) (*os.File, error) {
	if !idPattern.MatchString(id) {
		return nil, ErrInvalid
	}
	if create {
		err := unix.Mkdirat(int(s.root.Fd()), id, 0700)
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, ErrUnavailable
		}
		if err == nil && s.syncDir(s.root) != nil {
			return nil, ErrUnavailable
		}
	}
	fd, err := unix.Openat(int(s.root.Fd()), id, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, ErrUnsafe
	}
	d := os.NewFile(uintptr(fd), id)
	st, err := d.Stat()
	if err != nil || !privateDirectory(st, s.uid) {
		_ = d.Close()
		return nil, ErrUnsafe
	}
	return d, nil
}

// inspectDirectory refuses unknown entries and validates both known files.
// It never interprets ciphertext and never recursively removes anything.
func (s *Store) inspectDirectory(d *os.File, u Upload) (partial, final int64, hasPartial, hasFinal bool, err error) {
	if _, err = d.Seek(0, io.SeekStart); err != nil {
		return
	}
	entries, e := d.ReadDir(4)
	if e != nil && e != io.EOF {
		err = ErrUnsafe
		return
	}
	for _, entry := range entries {
		if entry.Name() != partialName && entry.Name() != contentName {
			err = ErrUnsafe
			return
		}
		f, e := openFileAt(d, entry.Name(), s.uid, u.SizeBytes)
		if e != nil {
			err = ErrUnsafe
			return
		}
		st, e := f.Stat()
		_ = f.Close()
		if e != nil {
			err = ErrUnsafe
			return
		}
		if entry.Name() == contentName {
			final, hasFinal = st.Size(), true
		} else {
			partial, hasPartial = st.Size(), true
		}
	}
	return
}

func (s *Store) storedBytes(u Upload) (int64, error) {
	d, err := s.uploadDirectory(u.ID, false)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer d.Close()
	p, f, hp, hf, err := s.inspectDirectory(d, u)
	if err != nil {
		return 0, err
	}
	if hp && hf {
		return 0, ErrUnsafe
	}
	return p + f, nil
}

func (s *Store) recover() error {
	// Root entries are fixed metadata or registered opaque IDs. An unexpected
	// entry is operator-visible failure, not silently ignored disk consumption.
	if _, err := s.root.Seek(0, io.SeekStart); err != nil {
		return ErrUnsafe
	}
	for {
		entries, err := s.root.ReadDir(64)
		for _, entry := range entries {
			switch entry.Name() {
			case journalName, lockName:
			case nextJournalName:
				f, e := openFileAt(s.root, nextJournalName, s.uid, maxJournalBytes)
				if e != nil {
					return ErrUnsafe
				}
				_ = f.Close()
				// The committed journal won. This known unfinished metadata
				// file cannot authorize filesystem publication by itself.
				if unix.Unlinkat(int(s.root.Fd()), nextJournalName, 0) != nil || s.syncDir(s.root) != nil {
					return ErrUnavailable
				}
			default:
				if _, ok := s.data.Uploads[entry.Name()]; !ok || !idPattern.MatchString(entry.Name()) {
					return ErrUnsafe
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrUnsafe
		}
	}
	next := clone(s.data)
	changed := false
	for id, u := range s.data.Uploads {
		d, err := s.uploadDirectory(id, false)
		var partial, final int64
		var hp, hf bool
		if err == nil {
			partial, final, hp, hf, err = s.inspectDirectory(d, u)
			_ = d.Close()
		} else if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if err != nil {
			return err
		}
		original := u
		switch {
		case u.State == "deleting":
			if err := s.removeKnownFiles(u); err != nil {
				return err
			}
			u.State = "deleted"
			u.Downloadable = false
			u.ErrorCode = ""
		case u.State == "deleted":
			if hp || hf {
				return ErrUnsafe
			}
		case u.State == "publication_unknown":
			// An operator must resolve this state. Restart is not evidence
			// of successful publication or archive authentication.
		case u.State == "received":
			if !hf || hp || final != u.SizeBytes {
				u.State = "publication_unknown"
				u.ErrorCode = "published_file_unavailable"
			}
		case hf:
			u.State = "publication_unknown"
			u.ErrorCode = "interrupted_publication"
			u.ReceivedBytes = final
		case u.State == "receiving" || u.State == "checking":
			u.State = "interrupted"
			u.ErrorCode = "upload_interrupted"
			u.ReceivedBytes = partial
		case u.State == "awaiting_upload" && hp:
			u.State = "publication_unknown"
			u.ErrorCode = "unexpected_partial_file"
			u.ReceivedBytes = partial
		}
		if u.State != "received" {
			u.Downloadable = false
		}
		if u.State == "publication_unknown" {
			u.Verification = "unverified"
		}
		if u != original {
			u.UpdatedUnix = max(s.clock().Unix(), u.CreatedUnix)
			next.Uploads[id] = u
			changed = true
		}
	}
	if changed {
		return s.commit(next)
	}
	return nil
}

func (s *Store) removeKnownFiles(u Upload) error {
	d, err := s.uploadDirectory(u.ID, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer d.Close()
	_, final, _, hf, err := s.inspectDirectory(d, u)
	if err != nil || hf && final != u.SizeBytes {
		return ErrUnsafe
	}
	before, err := d.Stat()
	if err != nil || !unchangedAt(s.root, u.ID, before) {
		return ErrUnsafe
	}
	for _, name := range []string{partialName, contentName} {
		f, e := openFileAt(d, name, s.uid, u.SizeBytes)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return ErrUnsafe
		}
		st, e := f.Stat()
		if e != nil || !unchangedAt(d, name, st) {
			_ = f.Close()
			return ErrUnsafe
		}
		e = unix.Unlinkat(int(d.Fd()), name, 0)
		_ = f.Close()
		if e != nil {
			return ErrUnavailable
		}
	}
	if s.syncDir(d) != nil {
		return ErrUnavailable
	}
	// Check only inode/mode after contents changed (directory mtime changed).
	var named unix.Stat_t
	if unix.Fstatat(int(s.root.Fd()), u.ID, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameStat(before, &named) {
		return ErrUnsafe
	}
	if unix.Unlinkat(int(s.root.Fd()), u.ID, unix.AT_REMOVEDIR) != nil {
		return ErrUnsafe
	}
	if s.syncDir(s.root) != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) verifiedFile(ctx context.Context, d *os.File, u Upload) (*os.File, error) {
	f, err := openFileAt(d, contentName, s.uid, u.SizeBytes)
	if err != nil {
		return nil, ErrUnsafe
	}
	keep := false
	defer func() {
		if !keep {
			_ = f.Close()
		}
	}()
	before, err := f.Stat()
	if err != nil || before.Size() != u.SizeBytes {
		return nil, ErrUnsafe
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := f.Read(buf)
		if n > 0 {
			total += int64(n)
			_, _ = h.Write(buf[:n])
			if total > u.SizeBytes {
				return nil, ErrUnsafe
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrUnsafe
		}
	}
	after, err := f.Stat()
	if err != nil || total != u.SizeBytes || hex.EncodeToString(h.Sum(nil)) != u.SHA256 || !os.SameFile(before, after) ||
		!unchangedAt(d, contentName, before) || before.ModTime() != after.ModTime() || before.Sys().(*syscall.Stat_t).Ctim != after.Sys().(*syscall.Stat_t).Ctim {
		return nil, ErrUnsafe
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, ErrUnsafe
	}
	keep = true
	return f, nil
}

type pinnedDownload struct {
	*os.File
	store *Store
	id    string
	ctx   context.Context
	once  sync.Once
	err   error
}

func (p *pinnedDownload) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	return p.File.Read(b)
}

func (p *pinnedDownload) Close() error {
	p.once.Do(func() {
		p.err = p.File.Close()
		p.store.mu.Lock()
		p.store.pins[p.id]--
		if p.store.pins[p.id] == 0 {
			delete(p.store.pins, p.id)
		}
		_ = p.store.closeResources()
		p.store.mu.Unlock()
	})
	return p.err
}

func (s *Store) unpin(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pins[id]--
	if s.pins[id] == 0 {
		delete(s.pins, id)
	}
	_ = s.closeResources()
}

func (s *Store) OpenDownload(ctx context.Context, id string) (ReadSeekCloser, Upload, error) {
	s.mu.Lock()
	if err := s.healthy(ctx); err != nil {
		s.mu.Unlock()
		return nil, Upload{}, err
	}
	if !idPattern.MatchString(id) {
		s.mu.Unlock()
		return nil, Upload{}, ErrInvalid
	}
	u, ok := s.data.Uploads[id]
	if !ok {
		s.mu.Unlock()
		return nil, Upload{}, ErrNotFound
	}
	if u.State != "received" || !u.Downloadable {
		s.mu.Unlock()
		return nil, Upload{}, ErrConflict
	}
	s.pins[id]++
	s.mu.Unlock()
	keep := false
	defer func() {
		if !keep {
			s.unpin(id)
		}
	}()
	d, err := s.uploadDirectory(id, false)
	if err != nil {
		return nil, Upload{}, ErrUnsafe
	}
	defer d.Close()
	if _, _, hp, hf, err := s.inspectDirectory(d, u); err != nil || hp || !hf {
		return nil, Upload{}, ErrUnsafe
	}
	f, err := s.verifiedFile(ctx, d, u)
	if err != nil {
		return nil, Upload{}, err
	}
	s.mu.Lock()
	err = s.healthy(ctx)
	if err == nil && s.data.Uploads[id] != u {
		err = ErrConflict
	}
	s.mu.Unlock()
	if err != nil {
		_ = f.Close()
		return nil, Upload{}, err
	}
	keep = true
	return &pinnedDownload{File: f, store: s, id: id, ctx: ctx}, u, nil
}
