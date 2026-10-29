//go:build linux

package backupcontrol

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"filippo.io/age"
)

const MaxJobs = 4096
const MaxReceipts = 8192
const MaxArchives = 4096
const maxJournalBytes = 32 << 20

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var eventPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var actorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,95}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type receipt struct {
	Kind     string    `json:"kind"`
	Digest   string    `json:"digest"`
	JobID    string    `json:"job_id"`
	Settings *Settings `json:"settings,omitempty"`
}
type journal struct {
	Version             int                `json:"version"`
	Settings            Settings           `json:"settings"`
	ScheduleChangedUnix int64              `json:"schedule_changed_unix"`
	Jobs                map[string]Job     `json:"jobs"`
	Archives            map[string]Archive `json:"archives"`
	Receipts            map[string]receipt `json:"receipts"`
	Deleting            map[string]bool    `json:"deleting"`
}
type Store struct {
	mu        sync.Mutex
	dir       string
	uid       uint32
	lock      *os.File
	data      journal
	recipient string
	clock     func() time.Time
	pins      map[string]int
	closed    bool
	broken    bool
	dirInfo   os.FileInfo
	syncFile  func(*os.File) error
	syncDir   func(string) error
}

func ValidID(id string) bool { return idPattern.MatchString(id) }

// OpenStore is intentionally root-owned in production. The unprivileged HUB
// uses the peer-authenticated Unix client and never opens this journal.
func OpenStore(directory, publicRecipient string) (*Store, error) {
	return openStore(directory, publicRecipient, 0, time.Now)
}
func openStore(directory, publicRecipient string, uid uint32, clock func() time.Time) (*Store, error) {
	if clock == nil || canonicalPath(directory) != nil || directory == "/" {
		return nil, ErrUnsafe
	}
	key, err := age.ParseX25519Recipient(publicRecipient)
	if err != nil || key.String() != publicRecipient {
		return nil, ErrInvalid
	}
	if err = os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, ErrUnsafe
	}
	st, err := os.Lstat(directory)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !owner(st, uid, false) {
		return nil, ErrUnsafe
	}
	fd, err := syscall.Open(filepath.Join(directory, "control.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, ErrUnsafe
	}
	f := os.NewFile(uintptr(fd), "backup-control-lock")
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || !owner(st, uid, true) {
		f.Close()
		return nil, ErrUnsafe
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, ErrBusy
	}
	dirInfo, err := os.Lstat(directory)
	if err != nil {
		f.Close()
		return nil, ErrUnsafe
	}
	s := &Store{dir: directory, uid: uid, lock: f, recipient: publicRecipient, clock: clock, pins: map[string]int{}, dirInfo: dirInfo, syncFile: (*os.File).Sync, syncDir: syncDirectory}
	path := filepath.Join(directory, "control.json")
	file, err := s.openPrivate(path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = journal{Version: 1, Settings: DefaultSettings(), ScheduleChangedUnix: clock().Unix(), Jobs: map[string]Job{}, Archives: map[string]Archive{}, Receipts: map[string]receipt{}, Deleting: map[string]bool{}}
		err = s.commit(s.data)
	} else if err == nil {
		raw, e := io.ReadAll(io.LimitReader(file, maxJournalBytes+1))
		file.Close()
		if e != nil || len(raw) > maxJournalBytes || strictDecode(raw, &s.data) != nil || validateJournal(s.data) != nil {
			err = ErrUnsafe
		}
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func canonicalPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
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
func owner(st os.FileInfo, uid uint32, single bool) bool {
	x, ok := st.Sys().(*syscall.Stat_t)
	return ok && x.Uid == uid && (!single || x.Nlink == 1)
}
func (s *Store) openPrivate(path string) (*os.File, error) {
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "backup-control-state")
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || !owner(st, s.uid, true) || st.Size() > maxJournalBytes {
		f.Close()
		return nil, ErrUnsafe
	}
	return f, nil
}
func (s *Store) healthy(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.broken || s.lock == nil {
		return ErrUnavailable
	}
	a, e := s.lock.Stat()
	b, e2 := os.Lstat(filepath.Join(s.dir, "control.lock"))
	d, e3 := os.Lstat(s.dir)
	if e != nil || e2 != nil || e3 != nil || !os.SameFile(a, b) || !a.Mode().IsRegular() || a.Mode().Perm()&0077 != 0 || !owner(a, s.uid, true) || !os.SameFile(d, s.dirInfo) || !d.IsDir() || d.Mode().Perm()&0077 != 0 || !owner(d, s.uid, false) || canonicalPath(s.dir) != nil {
		return ErrUnsafe
	}
	return nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lock != nil {
		return s.lock.Close()
	}
	return nil
}
func cloneJournal(d journal) journal {
	x := d
	x.Jobs = make(map[string]Job, len(d.Jobs))
	for k, v := range d.Jobs {
		x.Jobs[k] = v
	}
	x.Archives = make(map[string]Archive, len(d.Archives))
	for k, v := range d.Archives {
		x.Archives[k] = v
	}
	x.Receipts = make(map[string]receipt, len(d.Receipts))
	for k, v := range d.Receipts {
		x.Receipts[k] = v
	}
	x.Deleting = make(map[string]bool, len(d.Deleting))
	for k, v := range d.Deleting {
		x.Deleting[k] = v
	}
	return x
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func (s *Store) commit(next journal) error {
	if validateJournal(next) != nil {
		return ErrUnsafe
	}
	raw, e := json.Marshal(next)
	if e != nil || len(raw) > maxJournalBytes {
		return ErrCapacity
	}
	path := filepath.Join(s.dir, "control.json")
	if f, e := s.openPrivate(path); e == nil {
		f.Close()
	} else if !errors.Is(e, os.ErrNotExist) {
		return ErrUnsafe
	}
	f, e := os.CreateTemp(s.dir, ".control-next-")
	if e != nil {
		return ErrUnavailable
	}
	temp := f.Name()
	defer os.Remove(temp)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = s.syncFile(f)
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return ErrUnavailable
	}
	if e = os.Rename(temp, path); e != nil {
		return ErrUnavailable
	}
	e = s.syncDir(s.dir)
	if e != nil {
		s.broken = true
		return ErrUnavailable
	}
	s.data = next
	return nil
}
func validateJournal(d journal) error {
	if d.Version != 1 || ValidateSettings(d.Settings) != nil || d.ScheduleChangedUnix <= 0 || d.Jobs == nil || d.Archives == nil || d.Receipts == nil || d.Deleting == nil || len(d.Jobs) > MaxJobs || len(d.Archives) > MaxArchives || len(d.Receipts) > MaxReceipts {
		return ErrUnsafe
	}
	active := 0
	for id, j := range d.Jobs {
		if id != j.ID || validateJob(j) != nil {
			return ErrUnsafe
		}
		if !Terminal(j.State) {
			active++
		}
	}
	if active > 1 {
		return ErrUnsafe
	}
	for id, a := range d.Archives {
		if id != a.ID || validateArchive(a) != nil {
			return ErrUnsafe
		}
		if _, ok := d.Jobs[a.JobID]; !ok {
			return ErrUnsafe
		}
	}
	for id, r := range d.Receipts {
		if !eventPattern.MatchString(id) || !digestPattern.MatchString(r.Digest) {
			return ErrUnsafe
		}
		switch r.Kind {
		case "create", "cancel", "schedule":
			if _, ok := d.Jobs[r.JobID]; !ok || r.Settings != nil {
				return ErrUnsafe
			}
		case "settings":
			if r.JobID != "" || r.Settings == nil || ValidateSettings(*r.Settings) != nil {
				return ErrUnsafe
			}
		default:
			return ErrUnsafe
		}
	}
	for id, b := range d.Deleting {
		if !b {
			return ErrUnsafe
		}
		if _, ok := d.Archives[id]; !ok {
			return ErrUnsafe
		}
	}
	return nil
}
func validateJob(j Job) error {
	if !ValidID(j.ID) || !eventPattern.MatchString(j.EventID) || !actorPattern.MatchString(j.ActorID) || j.Action != "create" || j.Trigger != "manual" && j.Trigger != "schedule" || j.CreatedUnix <= 0 || j.StartedUnix < 0 || j.FinishedUnix < 0 || j.DowntimeMS < 0 || j.Revision < 1 || j.Revision > 9007199254740991 || j.ArchiveID != "" && !ValidID(j.ArchiveID) || j.HubStoppedUnix < 0 || j.HubResumedUnix < 0 || ValidateSettings(j.Policy) != nil {
		return ErrInvalid
	}
	if j.ErrorCode != "" && !reasonPattern.MatchString(j.ErrorCode) || j.WarningCode != "" && !reasonPattern.MatchString(j.WarningCode) {
		return ErrInvalid
	}
	if _, e := age.ParseX25519Recipient(j.Recipient); e != nil {
		return ErrInvalid
	}
	switch j.State {
	case "queued", "running", "cancelling", "succeeded", "failed", "cancelled", "recovery_required":
	default:
		return ErrInvalid
	}
	switch j.Phase {
	case "queued", "preflight", "stopping_hub", "creating_offline", "restarting_hub", "checking_archive", "retention", "complete":
	default:
		return ErrInvalid
	}
	if Terminal(j.State) && (j.ResumeRequired || j.FinishedUnix <= 0 || j.Phase != "complete") {
		return ErrInvalid
	}
	if j.State == "queued" && (j.Phase != "queued" || j.ResumeRequired || j.StartedUnix != 0) {
		return ErrInvalid
	}
	if j.ResumeRequired && (!j.PreServiceActive || j.Phase != "stopping_hub" && j.Phase != "creating_offline" && j.Phase != "restarting_hub") {
		return ErrInvalid
	}
	return nil
}
func validateArchive(a Archive) error {
	if !ValidID(a.ID) || !ValidID(a.JobID) || a.CreatedUnix <= 0 || a.SizeBytes < 1 || a.SizeBytes > MaxArchiveBytes || a.PayloadBytes < 0 || a.PayloadBytes > 64<<30 || !digestPattern.MatchString(a.SHA256) || !revisionPattern.MatchString(a.SourceRevision) || a.SchemaVersion != 8 && a.SchemaVersion != 16 || a.Verification != "checksum_verified" && a.Verification != "restore_verified" {
		return ErrInvalid
	}
	return nil
}
func requestDigest(kind string, v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(append([]byte(kind+"\x00"), raw...))
	return hex.EncodeToString(h[:])
}
func newID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(b[:]), nil
}
func validActorEvent(actor, event string) bool {
	return actorPattern.MatchString(actor) && eventPattern.MatchString(event) && !strings.HasPrefix(event, "schedule:")
}
func activeJobs(d journal) bool {
	for _, j := range d.Jobs {
		if !Terminal(j.State) {
			return true
		}
	}
	return false
}
func (s *Store) newJob(d *journal, event, actor, trigger string, now time.Time) (Job, error) {
	if len(d.Jobs) >= MaxJobs || len(d.Receipts) >= MaxReceipts {
		return Job{}, ErrCapacity
	}
	if activeJobs(*d) {
		return Job{}, ErrBusy
	}
	id, e := newID()
	if e != nil {
		return Job{}, e
	}
	if _, exists := d.Jobs[id]; exists {
		return Job{}, ErrUnavailable
	}
	j := Job{ID: id, EventID: event, ActorID: actor, Action: "create", Trigger: trigger, State: "queued", Phase: "queued", CreatedUnix: now.Unix(), Revision: 1, Recipient: s.recipient, Policy: d.Settings}
	d.Jobs[id] = j
	return j, nil
}
func (s *Store) CreateJob(ctx context.Context, r CreateRequest) (JobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return JobResponse{}, e
	}
	if !validActorEvent(r.ActorID, r.EventID) || r.Action != "create" {
		return JobResponse{}, ErrInvalid
	}
	digest := requestDigest("create", r)
	if old, ok := s.data.Receipts[r.EventID]; ok {
		if old.Kind != "create" || old.Digest != digest {
			return JobResponse{}, ErrConflict
		}
		return JobResponse{Job: s.data.Jobs[old.JobID], Replayed: true}, nil
	}
	d := cloneJournal(s.data)
	j, e := s.newJob(&d, r.EventID, r.ActorID, "manual", s.clock())
	if e != nil {
		return JobResponse{}, e
	}
	d.Receipts[r.EventID] = receipt{Kind: "create", Digest: digest, JobID: j.ID}
	if e = s.commit(d); e != nil {
		return JobResponse{}, e
	}
	return JobResponse{Job: j}, nil
}
func (s *Store) GetJob(ctx context.Context, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return Job{}, e
	}
	if !ValidID(id) {
		return Job{}, ErrInvalid
	}
	j, ok := s.data.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return j, nil
}
func (s *Store) CancelJob(ctx context.Context, id string, r CancelRequest) (JobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return JobResponse{}, e
	}
	if !ValidID(id) || !validActorEvent(r.ActorID, r.EventID) {
		return JobResponse{}, ErrInvalid
	}
	j, ok := s.data.Jobs[id]
	if !ok {
		return JobResponse{}, ErrNotFound
	}
	digest := requestDigest("cancel", struct {
		ID      string
		Request CancelRequest
	}{id, r})
	if old, ok := s.data.Receipts[r.EventID]; ok {
		if old.Kind != "cancel" || old.Digest != digest {
			return JobResponse{}, ErrConflict
		}
		return JobResponse{Job: j, Replayed: true}, nil
	}
	if len(s.data.Receipts) >= MaxReceipts {
		return JobResponse{}, ErrCapacity
	}
	d := cloneJournal(s.data)
	if !Terminal(j.State) {
		j.CancelRequested = true
		if j.State == "queued" {
			j.State = "cancelled"
			j.Phase = "complete"
			j.FinishedUnix = s.clock().Unix()
		} else if j.State != "recovery_required" {
			j.State = "cancelling"
		}
		j.Revision++
		d.Jobs[id] = j
	}
	d.Receipts[r.EventID] = receipt{Kind: "cancel", Digest: digest, JobID: id}
	if e := s.commit(d); e != nil {
		return JobResponse{}, e
	}
	return JobResponse{Job: j}, nil
}
func (s *Store) UpdateSettings(ctx context.Context, r SettingsRequest) (SettingsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return SettingsResponse{}, e
	}
	if !validActorEvent(r.ActorID, r.EventID) {
		return SettingsResponse{}, ErrInvalid
	}
	digest := requestDigest("settings", r)
	if old, ok := s.data.Receipts[r.EventID]; ok {
		if old.Kind != "settings" || old.Digest != digest {
			return SettingsResponse{}, ErrConflict
		}
		return SettingsResponse{Settings: *old.Settings, Replayed: true}, nil
	}
	if r.ExpectedRevision != s.data.Settings.Revision {
		return SettingsResponse{}, ErrConflict
	}
	next := Settings{Revision: r.ExpectedRevision + 1, Schedule: r.Schedule, Retention: r.Retention}
	if ValidateSettings(next) != nil {
		return SettingsResponse{}, ErrInvalid
	}
	if len(s.data.Receipts) >= MaxReceipts {
		return SettingsResponse{}, ErrCapacity
	}
	d := cloneJournal(s.data)
	if d.Settings.Schedule != next.Schedule {
		d.ScheduleChangedUnix = s.clock().Unix()
	}
	d.Settings = next
	d.Receipts[r.EventID] = receipt{Kind: "settings", Digest: digest, Settings: &next}
	if e := s.commit(d); e != nil {
		return SettingsResponse{}, e
	}
	return SettingsResponse{Settings: next}, nil
}
func (s *Store) UpdateJob(ctx context.Context, id string, change func(*Job) error) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return Job{}, e
	}
	if !ValidID(id) || change == nil {
		return Job{}, ErrInvalid
	}
	old, ok := s.data.Jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	j := old
	if e := change(&j); e != nil {
		return Job{}, e
	}
	if j.ID != old.ID || j.EventID != old.EventID || j.ActorID != old.ActorID || j.Action != old.Action || j.Trigger != old.Trigger || j.CreatedUnix != old.CreatedUnix || j.Policy != old.Policy || j.Recipient != old.Recipient {
		return Job{}, ErrInvalid
	}
	if Terminal(old.State) && j != old {
		return Job{}, ErrConflict
	}
	if j == old {
		return old, nil
	}
	// A stale worker copy cannot erase the durable obligation to resume HUB.
	// Only the explicit post-start transition records its completion.
	if old.ResumeRequired && !j.ResumeRequired && (!j.PreServiceActive || j.HubResumedUnix <= 0 || j.Phase != "checking_archive") {
		return Job{}, ErrInvalid
	}
	if old.State != "queued" && j.State == "queued" {
		return Job{}, ErrInvalid
	}
	j.CancelRequested = j.CancelRequested || old.CancelRequested
	if j.CancelRequested && j.State == "running" {
		j.State = "cancelling"
	}
	j.Revision = old.Revision + 1
	if validateJob(j) != nil {
		return Job{}, ErrInvalid
	}
	d := cloneJournal(s.data)
	d.Jobs[id] = j
	if e := s.commit(d); e != nil {
		return Job{}, e
	}
	return j, nil
}
func (s *Store) RecoverableJobs(ctx context.Context) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return nil, e
	}
	out := []Job{}
	for _, j := range s.data.Jobs {
		if !Terminal(j.State) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedUnix == out[j].CreatedUnix {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedUnix < out[j].CreatedUnix
	})
	return out, nil
}
func (s *Store) EnqueueScheduled(ctx context.Context, now time.Time) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return nil, e
	}
	slot, e := latestRun(s.data.Settings.Schedule, now)
	if e != nil {
		return nil, e
	}
	if slot.IsZero() || slot.Unix() < s.data.ScheduleChangedUnix {
		return nil, nil
	}
	event := "schedule:" + strconv.FormatInt(slot.Unix(), 10)
	if _, ok := s.data.Receipts[event]; ok {
		return nil, nil
	}
	if activeJobs(s.data) {
		return nil, nil
	}
	d := cloneJournal(s.data)
	j, e := s.newJob(&d, event, "scheduler", "schedule", now)
	if e != nil {
		return nil, e
	}
	d.Receipts[event] = receipt{Kind: "schedule", Digest: requestDigest("schedule", event), JobID: j.ID}
	if e = s.commit(d); e != nil {
		return nil, e
	}
	return &j, nil
}
func (s *Store) PutArchive(ctx context.Context, a Archive) (Archive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return Archive{}, e
	}
	if validateArchive(a) != nil {
		return Archive{}, ErrInvalid
	}
	if _, ok := s.data.Jobs[a.JobID]; !ok {
		return Archive{}, ErrNotFound
	}
	if old, ok := s.data.Archives[a.ID]; ok {
		compare := a
		compare.Verification = old.Verification
		compare.Protected = old.Protected
		compare.Downloadable = old.Downloadable
		if compare != old || s.data.Deleting[a.ID] || old.Verification == "restore_verified" && a.Verification != "restore_verified" {
			return Archive{}, ErrConflict
		}
		if a == old {
			return a, nil
		}
	} else if len(s.data.Archives) >= MaxArchives {
		return Archive{}, ErrCapacity
	}
	d := cloneJournal(s.data)
	d.Archives[a.ID] = a
	if e := s.commit(d); e != nil {
		return Archive{}, e
	}
	return a, nil
}
func archiveViews(d journal) []Archive {
	out := make([]Archive, 0, len(d.Archives))
	for _, a := range d.Archives {
		if d.Deleting[a.ID] || d.Jobs[a.JobID].State != "succeeded" {
			a.Downloadable = false
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedUnix == out[j].CreatedUnix {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedUnix > out[j].CreatedUnix
	})
	good, restored := false, false
	for i := range out {
		a := &out[i]
		if d.Deleting[a.ID] || !a.Downloadable {
			continue
		}
		if !good {
			a.Protected = true
			good = true
		}
		if a.Verification == "restore_verified" && !restored {
			a.Protected = true
			restored = true
		}
	}
	return out
}
func (s *Store) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return Status{}, e
	}
	hash := sha256.Sum256([]byte(s.recipient))
	out := Status{Enabled: true, RunnerStatus: "ready", RecipientFingerprint: hex.EncodeToString(hash[:]), Capabilities: Capabilities{Create: true, Download: true, Cancel: true, Schedules: true, Retention: true}, Settings: s.data.Settings, Jobs: []Job{}, Archives: archiveViews(s.data)}
	for _, j := range s.data.Jobs {
		out.Jobs = append(out.Jobs, j)
		if !Terminal(j.State) {
			out.RunnerStatus = "busy"
		}
		if j.State == "recovery_required" {
			out.RunnerStatus = "recovery_required"
		}
	}
	sort.Slice(out.Jobs, func(i, j int) bool {
		if out.Jobs[i].CreatedUnix == out.Jobs[j].CreatedUnix {
			return out.Jobs[i].ID > out.Jobs[j].ID
		}
		return out.Jobs[i].CreatedUnix > out.Jobs[j].CreatedUnix
	})
	next, e := NextRun(out.Settings.Schedule, s.clock())
	if e != nil {
		return Status{}, e
	}
	if !next.IsZero() {
		out.NextRunUnix = next.Unix()
	}
	return out, nil
}
