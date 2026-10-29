package backupcontrol

import (
	"context"
	"sync"
	"time"
)

// Each lease retains an open file, connection and independent checksum read.
// The limit also bounds a trusted HUB's accidentally abandoned downloads.
const MaxActiveDownloads = 8

func (s *Store) retentionCandidates(now time.Time) []Archive {
	out := []Archive{}
	rank := 0
	for _, a := range archiveViews(s.data) {
		if s.data.Deleting[a.ID] {
			// The public view hides a pending deletion from downloads. The
			// worker needs its original registered metadata to safely retry.
			out = append(out, s.data.Archives[a.ID])
			continue
		}
		job := s.data.Jobs[a.JobID]
		if !a.Downloadable || !Terminal(job.State) {
			continue
		}
		rank++
		old := a.CreatedUnix < now.Add(-time.Duration(s.data.Settings.Retention.MaxAgeDays)*24*time.Hour).Unix()
		if !a.Protected && s.pins[a.ID] == 0 && (rank > s.data.Settings.Retention.KeepLast || old) {
			out = append(out, a)
		}
	}
	return out
}
func (s *Store) RetentionCandidates(ctx context.Context, now time.Time) ([]Archive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return nil, e
	}
	return s.retentionCandidates(now), nil
}

// PruneArchives journals removal intent before unlink, and retains that intent
// across a failed unlink/crash. remove must be idempotent for an already absent
// fixed archive. No untracked file is ever supplied to the callback.
func (s *Store) PruneArchives(ctx context.Context, now time.Time, remove func(Archive) error) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return nil, e
	}
	if remove == nil {
		return nil, ErrInvalid
	}
	removed := []string{}
	for _, a := range s.retentionCandidates(now) {
		if e := ctx.Err(); e != nil {
			return removed, e
		}
		if s.pins[a.ID] != 0 {
			continue
		}
		if !s.data.Deleting[a.ID] {
			d := cloneJournal(s.data)
			d.Deleting[a.ID] = true
			if e := s.commit(d); e != nil {
				return removed, e
			}
		}
		if e := remove(a); e != nil {
			return removed, e
		}
		d := cloneJournal(s.data)
		delete(d.Archives, a.ID)
		delete(d.Deleting, a.ID)
		if e := s.commit(d); e != nil {
			return removed, e
		}
		removed = append(removed, a.ID)
	}
	return removed, nil
}
func (s *Store) ActiveDownloads(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.healthy(ctx); e != nil {
		return 0, e
	}
	n := 0
	for _, v := range s.pins {
		n += v
	}
	return n, nil
}

type pinnedFile struct {
	ReadSeekCloser
	once    sync.Once
	release func()
}

func (p *pinnedFile) Close() error {
	var err error
	p.once.Do(func() { err = p.ReadSeekCloser.Close(); p.release() })
	return err
}

// OpenDownload acquires its pin before opening/hashing outside the mutex. A
// worker claims running/preflight before waiting for existing pins to drain.
func (s *Store) OpenDownload(ctx context.Context, id string, open func(context.Context, Archive) (ReadSeekCloser, error)) (ReadSeekCloser, Archive, error) {
	s.mu.Lock()
	if e := s.healthy(ctx); e != nil {
		s.mu.Unlock()
		return nil, Archive{}, e
	}
	if !ValidID(id) || open == nil {
		s.mu.Unlock()
		return nil, Archive{}, ErrInvalid
	}
	for _, j := range s.data.Jobs {
		if j.State == "running" || j.State == "cancelling" || j.State == "recovery_required" {
			s.mu.Unlock()
			return nil, Archive{}, ErrBusy
		}
	}
	a, ok := s.data.Archives[id]
	if !ok {
		s.mu.Unlock()
		return nil, Archive{}, ErrNotFound
	}
	if !a.Downloadable || s.data.Deleting[id] || s.data.Jobs[a.JobID].State != "succeeded" {
		s.mu.Unlock()
		return nil, Archive{}, ErrBusy
	}
	active := 0
	for _, count := range s.pins {
		active += count
	}
	if active >= MaxActiveDownloads {
		s.mu.Unlock()
		return nil, Archive{}, ErrBusy
	}
	s.pins[id]++
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.pins[id]--
		if s.pins[id] <= 0 {
			delete(s.pins, id)
		}
	}
	f, e := open(ctx, a)
	if e != nil {
		release()
		return nil, Archive{}, e
	}
	if f == nil {
		release()
		return nil, Archive{}, ErrUnavailable
	}
	return &pinnedFile{ReadSeekCloser: f, release: release}, a, nil
}
