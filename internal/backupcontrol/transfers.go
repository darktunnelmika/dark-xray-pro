package backupcontrol

import (
	"context"
	"sync"
)

// AcquireTransfer pins HUB availability while an owner upload or inbox
// download is active. The same mutex protects the worker's queued->running
// claim, so no new transfer can enter after the worker begins draining pins.
// The caller must retain this lease until the final downstream response is
// flushed, or the downstream connection is closed.
func (s *Store) AcquireTransfer(ctx context.Context) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return nil, err
	}
	for _, job := range s.data.Jobs {
		if job.State == "running" || job.State == "cancelling" || job.State == "recovery_required" {
			return nil, ErrBusy
		}
	}
	active := s.transfers
	for _, count := range s.pins {
		active += count
	}
	if active >= MaxActiveDownloads {
		return nil, ErrBusy
	}
	s.transfers++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.transfers--
		})
	}, nil
}
