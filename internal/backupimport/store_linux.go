//go:build linux

package backupimport

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const maxJournalBytes = 8 << 20
const journalName = "inbox.json"
const nextJournalName = ".inbox.next"
const lockName = "inbox.lock"
const partialName = "pending.backup"
const contentName = "content.backup"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var eventPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,96}$`)
var actorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errorPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)

type journal struct {
	Version int               `json:"version"`
	Uploads map[string]Upload `json:"uploads"`
}

type activeReceive struct {
	id        string
	cancel    context.CancelFunc
	closer    io.Closer
	closeOnce sync.Once
	bytes     atomic.Int64
	published bool // guarded by Store.mu
}

func (a *activeReceive) stop() {
	a.cancel()
	if a.closer != nil {
		a.closeOnce.Do(func() { _ = a.closer.Close() })
	}
}

type limits struct {
	maxUpload, maxInbox, minFree int64
	maxEntries, maxActive        int
}

// Store owns one locked, root-owned inbox. A live transfer and download pins
// keep the process lock alive after Close until their last handle is released.
type Store struct {
	mu             sync.Mutex
	dir            string
	uid            uint32
	root, lock     *os.File
	rootInfo       os.FileInfo
	data           journal
	closed, broken bool
	active         *activeReceive
	pins           map[string]int
	clock          func() time.Time
	limits         limits
	available      func() (int64, error)
	syncFile       func(*os.File) error
	syncDir        func(*os.File) error
	rename         func(int, string, int, string, uint) error
}

func OpenStore(directory string) (*Store, error) {
	return openStore(directory, 0, time.Now)
}

func openStore(directory string, uid uint32, clock func() time.Time) (*Store, error) {
	if clock == nil || canonical(directory) != nil || directory == "/" {
		return nil, ErrUnsafe
	}
	// Keep the inbox entry under a pinned, private owner-controlled parent.
	// Syncing only the inbox itself cannot persist its newly created name.
	parentPath := filepath.Dir(directory)
	parent, err := openDirectory(parentPath, uid)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	parentInfo, err := parent.Stat()
	if err != nil {
		return nil, ErrUnsafe
	}
	if err := unix.Mkdirat(int(parent.Fd()), filepath.Base(directory), 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, ErrUnsafe
	}
	root, err := openDirectory(directory, uid)
	if err != nil {
		return nil, err
	}
	s := &Store{dir: directory, uid: uid, root: root, clock: clock, pins: map[string]int{},
		limits:   limits{MaxUploadBytes, MaxInboxBytes, MinFreeBytes, MaxEntries, MaxActiveEntries},
		syncFile: (*os.File).Sync, syncDir: (*os.File).Sync, rename: unix.Renameat2}
	s.rootInfo, err = root.Stat()
	if err != nil {
		_ = root.Close()
		return nil, ErrUnsafe
	}
	currentParent, err := os.Lstat(parentPath)
	if err != nil || !os.SameFile(parentInfo, currentParent) || !privateDirectory(currentParent, uid) || !unchangedAt(parent, filepath.Base(directory), s.rootInfo) {
		_ = root.Close()
		return nil, ErrUnsafe
	}
	if parent.Sync() != nil {
		_ = root.Close()
		return nil, ErrUnavailable
	}
	s.available = func() (int64, error) { return availableAt(s.root) }
	fd, err := unix.Openat(int(root.Fd()), lockName, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		_ = root.Close()
		return nil, ErrUnsafe
	}
	s.lock = os.NewFile(uintptr(fd), lockName)
	if _, err = checkedInfo(s.lock, uid, 0); err != nil {
		_ = s.Close()
		return nil, err
	}
	if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		_ = s.Close()
		return nil, ErrBusy
	}
	f, err := openFileAt(root, journalName, uid, maxJournalBytes)
	if errors.Is(err, os.ErrNotExist) {
		// No other entry can predate an initial journal. Never adopt an orphan.
		entries, e := root.ReadDir(3)
		if e != nil && e != io.EOF {
			_ = s.Close()
			return nil, ErrUnsafe
		}
		for _, entry := range entries {
			if entry.Name() != lockName {
				_ = s.Close()
				return nil, ErrUnsafe
			}
		}
		s.data = journal{Version: 1, Uploads: map[string]Upload{}}
		err = s.commit(s.data)
	} else if err == nil {
		raw, e := io.ReadAll(io.LimitReader(f, maxJournalBytes+1))
		_ = f.Close()
		if e != nil || len(raw) > maxJournalBytes || json.Unmarshal(raw, &s.data) != nil {
			err = ErrUnsafe
		} else {
			canonical, e := json.Marshal(s.data)
			if e != nil || !bytes.Equal(raw, canonical) || validateJournal(s.data) != nil {
				err = ErrUnsafe
			}
		}
	}
	if err == nil {
		err = s.recover()
	}
	if err == nil {
		// A previous process may have completed a journal rename but lost its
		// final directory fsync/response. Confirm the currently observed name
		// before serving its durable receipt after restart.
		err = s.syncDir(root)
	}
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func validateUpload(u Upload) error {
	if !idPattern.MatchString(u.ID) || !eventPattern.MatchString(u.EventID) || !actorPattern.MatchString(u.ActorID) ||
		u.SizeBytes < 1 || u.SizeBytes > MaxUploadBytes || !hashPattern.MatchString(u.SHA256) || u.CreatedUnix < 1 || u.UpdatedUnix < u.CreatedUnix ||
		u.ReceivedBytes < 0 || u.ReceivedBytes > u.SizeBytes || u.Attempts < 0 || u.Attempts > 1000000 || u.ErrorCode != "" && !errorPattern.MatchString(u.ErrorCode) {
		return ErrUnsafe
	}
	switch u.State {
	case "awaiting_upload", "receiving", "checking", "received", "interrupted", "rejected", "cancelled", "deleting", "deleted", "publication_unknown":
	default:
		return ErrUnsafe
	}
	if u.State == "received" {
		if !u.Downloadable || u.Verification != "ciphertext_checksum_verified" || u.ReceivedBytes != u.SizeBytes || u.Attempts == 0 || u.ErrorCode != "" {
			return ErrUnsafe
		}
	} else if u.Downloadable {
		return ErrUnsafe
	}
	if u.Verification != "unverified" && (u.Verification != "ciphertext_checksum_verified" || u.State != "received" && u.State != "deleting" && u.State != "deleted") {
		return ErrUnsafe
	}
	if u.State == "awaiting_upload" && (u.Attempts != 0 || u.ReceivedBytes != 0) {
		return ErrUnsafe
	}
	return nil
}

func validateJournal(d journal) error {
	if d.Version != 1 || d.Uploads == nil || len(d.Uploads) > MaxEntries {
		return ErrUnsafe
	}
	events := map[string]bool{}
	var reserved int64
	active, receiving := 0, 0
	for id, u := range d.Uploads {
		if id != u.ID || validateUpload(u) != nil || events[u.EventID] {
			return ErrUnsafe
		}
		events[u.EventID] = true
		if u.State != "deleted" {
			active++
			reserved += u.SizeBytes
		}
		if u.State == "receiving" || u.State == "checking" {
			receiving++
		}
	}
	if active > MaxActiveEntries || reserved > MaxInboxBytes || receiving > 1 {
		return ErrUnsafe
	}
	return nil
}

func (s *Store) healthy(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.broken || s.root == nil || s.lock == nil {
		return ErrUnavailable
	}
	current, e := os.Lstat(s.dir)
	lock, e2 := s.lock.Stat()
	var named unix.Stat_t
	e3 := unix.Fstatat(int(s.root.Fd()), lockName, &named, unix.AT_SYMLINK_NOFOLLOW)
	if e != nil || e2 != nil || e3 != nil || !os.SameFile(s.rootInfo, current) || !privateDirectory(current, s.uid) || canonical(s.dir) != nil || !sameStat(lock, &named) {
		return ErrUnsafe
	}
	if _, err := checkedInfo(s.lock, s.uid, 0); err != nil {
		return ErrUnsafe
	}
	return nil
}

func clone(d journal) journal {
	n := journal{Version: d.Version, Uploads: make(map[string]Upload, len(d.Uploads))}
	for k, v := range d.Uploads {
		n.Uploads[k] = v
	}
	return n
}

// commit runs with mu held (or during exclusive startup). The only journal
// replacement is a complete fsynced regular file in the pinned directory.
func (s *Store) commit(next journal) error {
	if err := s.healthy(context.Background()); err != nil {
		return err
	}
	if validateJournal(next) != nil {
		return ErrUnsafe
	}
	raw, err := json.Marshal(next)
	if err != nil || len(raw) > maxJournalBytes {
		return ErrCapacity
	}
	if f, err := openFileAt(s.root, journalName, s.uid, maxJournalBytes); err == nil {
		_ = f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrUnsafe
	}
	fd, err := unix.Openat(int(s.root.Fd()), nextJournalName, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), nextJournalName)
	defer func() { _ = f.Close(); _ = unix.Unlinkat(int(s.root.Fd()), nextJournalName, 0) }()
	if n, e := f.Write(raw); e != nil || n != len(raw) {
		return ErrUnavailable
	}
	if s.syncFile(f) != nil || f.Close() != nil {
		return ErrUnavailable
	}
	if unix.Renameat(int(s.root.Fd()), nextJournalName, int(s.root.Fd()), journalName) != nil {
		return ErrUnavailable
	}
	if s.syncDir(s.root) != nil {
		s.broken = true
		return ErrUnavailable
	}
	s.data = next
	return nil
}

func (s *Store) put(u Upload) error {
	u.UpdatedUnix = max(s.clock().Unix(), u.CreatedUnix)
	next := clone(s.data)
	next.Uploads[u.ID] = u
	return s.commit(next)
}

func (s *Store) view(u Upload) Upload {
	if s.active != nil && s.active.id == u.ID && (u.State == "receiving" || u.State == "checking") {
		u.ReceivedBytes = min(s.active.bytes.Load(), u.SizeBytes)
	}
	return u
}

func (s *Store) totals() (reserved int64, count int) {
	for _, u := range s.data.Uploads {
		if u.State != "deleted" {
			reserved += u.SizeBytes
			count++
		}
	}
	return
}

func (s *Store) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Status{}, err
	}
	reserved, _ := s.totals()
	v := Status{Enabled: true, MaxUploadBytes: s.limits.maxUpload, MaxInboxBytes: s.limits.maxInbox,
		ReservedBytes: reserved, MaxEntries: s.limits.maxEntries, MaxActiveEntries: s.limits.maxActive, Uploads: []Upload{}}
	for _, u := range s.data.Uploads {
		v.Uploads = append(v.Uploads, s.view(u))
	}
	sort.Slice(v.Uploads, func(i, j int) bool {
		if v.Uploads[i].CreatedUnix != v.Uploads[j].CreatedUnix {
			return v.Uploads[i].CreatedUnix > v.Uploads[j].CreatedUnix
		}
		return v.Uploads[i].ID < v.Uploads[j].ID
	})
	return v, nil
}

func (s *Store) Get(ctx context.Context, id string) (Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Upload{}, err
	}
	if !idPattern.MatchString(id) {
		return Upload{}, ErrInvalid
	}
	u, ok := s.data.Uploads[id]
	if !ok {
		return Upload{}, ErrNotFound
	}
	return s.view(u), nil
}

// space reserves unwritten bytes as well as on-disk capacity. Existing partial
// bytes are measured from known files, rather than trusting a progress counter.
func (s *Store) space(add int64) error {
	needed := add
	for _, u := range s.data.Uploads {
		if u.State == "deleted" {
			continue
		}
		present, err := s.storedBytes(u)
		if err != nil {
			return err
		}
		needed += max(u.SizeBytes-present, 0)
	}
	free, err := s.available()
	if err != nil {
		return ErrUnavailable
	}
	if free < s.limits.minFree || needed > free-s.limits.minFree {
		return ErrCapacity
	}
	return nil
}

func (s *Store) Create(ctx context.Context, q CreateRequest) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Response{}, err
	}
	if !eventPattern.MatchString(q.EventID) || !actorPattern.MatchString(q.ActorID) || q.SizeBytes < 1 || q.SizeBytes > s.limits.maxUpload || !hashPattern.MatchString(q.SHA256) {
		return Response{}, ErrInvalid
	}
	for _, u := range s.data.Uploads {
		if u.EventID == q.EventID {
			if u.ActorID != q.ActorID || u.SizeBytes != q.SizeBytes || u.SHA256 != q.SHA256 {
				return Response{}, ErrConflict
			}
			return Response{Upload: s.view(u), Replayed: true}, nil
		}
	}
	reserved, active := s.totals()
	if len(s.data.Uploads) >= s.limits.maxEntries || active >= s.limits.maxActive || q.SizeBytes > s.limits.maxInbox-reserved {
		return Response{}, ErrCapacity
	}
	if err := s.space(q.SizeBytes); err != nil {
		return Response{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Response{}, ErrUnavailable
	}
	id := hex.EncodeToString(random[:])
	if _, ok := s.data.Uploads[id]; ok {
		return Response{}, ErrUnavailable
	}
	u := Upload{ID: id, EventID: q.EventID, ActorID: q.ActorID, SizeBytes: q.SizeBytes, SHA256: q.SHA256,
		State: "awaiting_upload", CreatedUnix: s.clock().Unix(), UpdatedUnix: s.clock().Unix(), Verification: "unverified"}
	if err := s.put(u); err != nil {
		return Response{}, err
	}
	return Response{Upload: s.data.Uploads[id]}, nil
}

func (s *Store) actorUpload(ctx context.Context, id, actor string) (Upload, error) {
	if err := s.healthy(ctx); err != nil {
		return Upload{}, err
	}
	if !idPattern.MatchString(id) || !actorPattern.MatchString(actor) {
		return Upload{}, ErrInvalid
	}
	u, ok := s.data.Uploads[id]
	if !ok {
		return Upload{}, ErrNotFound
	}
	if u.ActorID != actor {
		return Upload{}, ErrConflict
	}
	return u, nil
}

func (s *Store) Cancel(ctx context.Context, id, actor string) (Response, error) {
	s.mu.Lock()
	u, err := s.actorUpload(ctx, id, actor)
	if err != nil {
		s.mu.Unlock()
		return Response{}, err
	}
	if u.State == "cancelled" {
		s.mu.Unlock()
		return Response{Upload: u, Replayed: true}, nil
	}
	if u.State == "received" || u.State == "deleting" || u.State == "deleted" || u.State == "publication_unknown" || s.active != nil && s.active.id == id && s.active.published {
		s.mu.Unlock()
		return Response{}, ErrConflict
	}
	a := s.active
	u = s.view(u)
	u.State = "cancelled"
	u.Downloadable = false
	u.ErrorCode = "cancelled"
	err = s.put(u)
	u = s.data.Uploads[id]
	s.mu.Unlock()
	if err == nil && a != nil && a.id == id {
		a.stop()
	}
	return Response{Upload: u}, err
}

func (s *Store) Remove(ctx context.Context, id, actor string) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.actorUpload(ctx, id, actor)
	if err != nil {
		return Response{}, err
	}
	if u.State == "deleted" {
		return Response{Upload: u, Replayed: true}, nil
	}
	if s.pins[id] > 0 || s.active != nil && s.active.id == id || u.State == "receiving" || u.State == "checking" {
		return Response{}, ErrBusy
	}
	if u.State == "publication_unknown" {
		return Response{}, ErrUnsafe
	}
	u.State = "deleting"
	u.Downloadable = false
	u.ErrorCode = ""
	if err := s.put(u); err != nil {
		return Response{}, err
	}
	if err := s.removeKnownFiles(u); err != nil {
		u.ErrorCode = "delete_incomplete"
		_ = s.put(u)
		return Response{Upload: s.data.Uploads[id]}, err
	}
	u.State = "deleted"
	u.ErrorCode = ""
	if err := s.put(u); err != nil {
		return Response{}, err
	}
	return Response{Upload: s.data.Uploads[id]}, nil
}

func (s *Store) closeResources() error {
	if !s.closed || s.active != nil || len(s.pins) != 0 {
		return nil
	}
	var err error
	if s.lock != nil {
		err = s.lock.Close()
		s.lock = nil
	}
	if s.root != nil {
		if e := s.root.Close(); err == nil {
			err = e
		}
		s.root = nil
	}
	return err
}

func (s *Store) Close() error {
	s.mu.Lock()
	s.closed = true
	a := s.active
	err := s.closeResources()
	s.mu.Unlock()
	if a != nil {
		a.stop()
	}
	return err
}

func canonical(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrUnsafe
	}
	for p := path; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err == nil && st.Mode()&os.ModeSymlink != 0 || err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrUnsafe
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}

func privateDirectory(st os.FileInfo, uid uint32) bool {
	stat, ok := st.Sys().(*syscall.Stat_t)
	return ok && st.IsDir() && st.Mode().Perm()&0077 == 0 && st.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 && stat.Uid == uid
}

func availableAt(root *os.File) (int64, error) {
	var st unix.Statfs_t
	if unix.Fstatfs(int(root.Fd()), &st) != nil || st.Bsize <= 0 {
		return 0, ErrUnavailable
	}
	if st.Bavail > uint64((1<<63-1)/st.Bsize) {
		return 1<<63 - 1, nil
	}
	return int64(st.Bavail) * st.Bsize, nil
}
