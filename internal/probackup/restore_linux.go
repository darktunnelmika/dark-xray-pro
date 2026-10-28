//go:build linux

package probackup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type restoreStage struct {
	parent                                  *os.Root
	root                                    *os.Root
	parentFile                              *os.File
	parentPath, name, fullPath, destination string
	published                               bool
}

// Restore writes only into a new private sibling stage. validate is mandatory,
// must perform application/schema checks without modifying the stage, and must
// close its handles before returning. The file inventory and hashes are checked
// again after it returns. No service is started or configured.
//
// destination must be an absolute, clean, absent path whose existing parent is
// not symlinked or writable by group/other. Existing empty directories are also
// refused. Publication uses Linux RENAME_NOREPLACE, with no unsafe fallback.
// If ErrPublicationUncertain is returned, publication happened but durability
// could not be confirmed: the quarantined destination is deliberately retained.
func Restore(ctx context.Context, src io.Reader, identity, destination string, limits Limits, validate func(string, Manifest) error) (Manifest, error) {
	var empty Manifest
	limits, err := limits.resolve()
	if err != nil || ctx == nil || src == nil || validate == nil {
		return empty, ErrOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	s, err := newRestoreStage(destination)
	if err != nil {
		return empty, err
	}
	defer s.close()
	m, err := readArchive(ctx, src, identity, limits, s.create)
	if err != nil {
		return empty, err
	}
	if !s.parentUnchanged() {
		return empty, ErrDestination
	}
	if err = validate(s.fullPath, cloneManifest(m)); err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, ErrValidation
	}
	if err = s.verifyFiles(ctx, m); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	marker, err := json.Marshal(struct {
		State    string   `json:"state"`
		Manifest Manifest `json:"manifest"`
	}{State: "quarantined", Manifest: m})
	if err != nil {
		return empty, ErrIO
	}
	w, err := s.create(FileEntry{Name: QuarantineName})
	if err != nil {
		return empty, ErrIO
	}
	n, writeErr := w.Write(marker)
	closeErr := w.Close()
	if writeErr != nil || n != len(marker) || closeErr != nil {
		return empty, ErrIO
	}
	if err = syncRootDir(s.root, "."); err != nil {
		return empty, ErrIO
	}
	if !s.parentUnchanged() {
		return empty, ErrDestination
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	err = unix.Renameat2(int(s.parentFile.Fd()), s.name, int(s.parentFile.Fd()), s.destination, unix.RENAME_NOREPLACE)
	if err != nil {
		if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
			return empty, ErrDestination
		}
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
			return empty, ErrUnsupported
		}
		return empty, ErrIO
	}
	// Set the publication state BEFORE fsync: a failure must never remove or
	// silently roll back a destination that was already published.
	s.published = true
	if err = s.parentFile.Sync(); err != nil || !s.parentUnchanged() {
		return m, ErrPublicationUncertain
	}
	return m, nil
}

func newRestoreStage(destination string) (*restoreStage, error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination || destination == "/" {
		return nil, ErrDestination
	}
	parentPath, base := filepath.Dir(destination), filepath.Base(destination)
	resolved, err := filepath.EvalSymlinks(parentPath)
	if err != nil || resolved != parentPath {
		return nil, ErrDestination
	}
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, ErrDestination
	}
	s := &restoreStage{parent: parent, parentPath: parentPath, destination: base}
	f, err := parent.Open(".")
	if err != nil {
		s.close()
		return nil, ErrDestination
	}
	s.parentFile = f
	info, err := f.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		s.close()
		return nil, ErrDestination
	}
	if _, err = parent.Lstat(base); !errors.Is(err, fs.ErrNotExist) {
		s.close()
		return nil, ErrDestination
	}
	for attempt := 0; attempt < 8; attempt++ {
		name := ".probackup-stage-" + rand.Text()
		if err = parent.Mkdir(name, 0700); errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			s.close()
			return nil, ErrIO
		}
		s.name = name
		s.fullPath = filepath.Join(parentPath, name)
		s.root, err = parent.OpenRoot(name)
		if err != nil {
			s.close()
			return nil, ErrIO
		}
		return s, nil
	}
	s.close()
	return nil, ErrIO
}

func (s *restoreStage) close() {
	if s.root != nil {
		_ = s.root.Close()
	}
	if s.parent != nil {
		if s.name != "" && !s.published {
			_ = s.parent.RemoveAll(s.name)
		}
		_ = s.parent.Close()
	}
	if s.parentFile != nil {
		_ = s.parentFile.Close()
	}
}

func (s *restoreStage) parentUnchanged() bool {
	open, err := s.parentFile.Stat()
	if err != nil {
		return false
	}
	current, err := os.Lstat(s.parentPath)
	return err == nil && current.IsDir() && current.Mode()&os.ModeSymlink == 0 && os.SameFile(open, current)
}

type syncFileWriter struct {
	file      *os.File
	root      *os.Root
	directory string
}

func (w *syncFileWriter) Write(p []byte) (int, error) { return w.file.Write(p) }
func (w *syncFileWriter) Close() error {
	syncErr := w.file.Sync()
	closeErr := w.file.Close()
	if syncErr != nil || closeErr != nil {
		return ErrIO
	}
	return syncRootDir(w.root, w.directory)
}

func syncRootDir(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return ErrIO
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil || closeErr != nil {
		return ErrIO
	}
	return nil
}

func (s *restoreStage) create(entry FileEntry) (io.WriteCloser, error) {
	dir := path.Dir(entry.Name)
	if dir != "." {
		if err := s.root.MkdirAll(dir, 0700); err != nil {
			return nil, ErrIO
		}
	}
	f, err := s.root.OpenFile(entry.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, ErrIO
	}
	return &syncFileWriter{file: f, root: s.root, directory: dir}, nil
}

func (s *restoreStage) verifyFiles(ctx context.Context, m Manifest) error {
	files := make(map[string]FileEntry, len(m.Files))
	dirs := map[string]bool{".": true}
	for _, f := range m.Files {
		files[f.Name] = f
		for dir := path.Dir(f.Name); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	buf := make([]byte, copyBufferSize)
	err := fs.WalkDir(s.root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return ErrValidation
		}
		info, err := d.Info()
		if err != nil {
			return ErrValidation
		}
		if info.IsDir() {
			if !dirs[name] || info.Mode().Perm()&0077 != 0 {
				return ErrValidation
			}
			return nil
		}
		entry, ok := files[name]
		if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0177 != 0 || info.Size() != entry.Size {
			return ErrValidation
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return ErrValidation
		}
		f, err := s.root.Open(name)
		if err != nil {
			return ErrValidation
		}
		h := sha256.New()
		copyErr := copyExact(ctx, h, f, entry.Size, buf)
		if copyErr == nil {
			copyErr = requireEOF(ctx, f)
		}
		closeErr := f.Close()
		if copyErr != nil {
			return safeError(copyErr, ErrValidation)
		}
		if closeErr != nil || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
			return ErrValidation
		}
		delete(files, name)
		return nil
	})
	if err != nil {
		return safeError(err, ErrValidation)
	}
	if len(files) != 0 {
		return ErrValidation
	}
	// Make all child directory entries durable before publishing their root.
	for dir := range dirs {
		if err := syncRootDir(s.root, dir); err != nil {
			return ErrIO
		}
	}
	return nil
}
