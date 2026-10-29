package backupworker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

type memoryJournal struct {
	mu          sync.Mutex
	job         backupcontrol.Job
	archives    map[string]backupcontrol.Archive
	downloads   int
	fail        func(backupcontrol.Job) error
	pruned      bool
	scheduleErr error
	downloadErr error
}

func (s *memoryJournal) GetJob(ctx context.Context, id string) (backupcontrol.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return backupcontrol.Job{}, ctx.Err()
	}
	if s.job.ID != id {
		return backupcontrol.Job{}, backupcontrol.ErrNotFound
	}
	return s.job, nil
}
func (s *memoryJournal) UpdateJob(ctx context.Context, id string, change func(*backupcontrol.Job) error) (backupcontrol.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return backupcontrol.Job{}, ctx.Err()
	}
	if s.job.ID != id {
		return backupcontrol.Job{}, backupcontrol.ErrNotFound
	}
	j := s.job
	if e := change(&j); e != nil {
		return backupcontrol.Job{}, e
	}
	if s.fail != nil {
		if e := s.fail(j); e != nil {
			return backupcontrol.Job{}, e
		}
	}
	j.CancelRequested = j.CancelRequested || s.job.CancelRequested
	if j.CancelRequested && j.State == "running" {
		j.State = "cancelling"
	}
	j.Revision++
	s.job = j
	return j, nil
}
func (s *memoryJournal) RecoverableJobs(ctx context.Context) ([]backupcontrol.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if backupcontrol.Terminal(s.job.State) {
		return nil, nil
	}
	return []backupcontrol.Job{s.job}, nil
}
func (s *memoryJournal) PutArchive(ctx context.Context, a backupcontrol.Archive) (backupcontrol.Archive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return backupcontrol.Archive{}, ctx.Err()
	}
	s.archives[a.ID] = a
	return a, nil
}
func (s *memoryJournal) EnqueueScheduled(context.Context, time.Time) (*backupcontrol.Job, error) {
	return nil, s.scheduleErr
}
func (s *memoryJournal) PruneArchives(context.Context, time.Time, func(backupcontrol.Archive) error) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruned = true
	return nil, nil
}
func (s *memoryJournal) ActiveDownloads(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downloadErr != nil {
		return 0, s.downloadErr
	}
	return s.downloads, ctx.Err()
}
func (s *memoryJournal) cancel() { s.mu.Lock(); defer s.mu.Unlock(); s.job.CancelRequested = true }

type fakeController struct {
	preflight, stop, start    func(context.Context) error
	preflights, stops, starts atomic.Int32
}

func (h *fakeController) Preflight(ctx context.Context) error {
	h.preflights.Add(1)
	if h.preflight != nil {
		return h.preflight(ctx)
	}
	return nil
}
func (h *fakeController) Stop(ctx context.Context) error {
	h.stops.Add(1)
	if h.stop != nil {
		return h.stop(ctx)
	}
	return nil
}
func (h *fakeController) Start(ctx context.Context) error {
	h.starts.Add(1)
	if h.start != nil {
		return h.start(ctx)
	}
	return nil
}

type fakeRunner struct {
	create             func(context.Context, backupcontrol.Job) (Receipt, error)
	finalize           func(context.Context, backupcontrol.Job) (backupcontrol.Archive, error)
	publication        atomic.Bool
	creates, finalizes atomic.Int32
}

func (r *fakeRunner) Create(ctx context.Context, j backupcontrol.Job) (Receipt, error) {
	r.creates.Add(1)
	if r.create != nil {
		return r.create(ctx, j)
	}
	r.publication.Store(true)
	return Receipt{}, nil
}
func (r *fakeRunner) Finalize(ctx context.Context, j backupcontrol.Job) (backupcontrol.Archive, error) {
	r.finalizes.Add(1)
	if r.finalize != nil {
		return r.finalize(ctx, j)
	}
	return backupcontrol.Archive{ID: j.ID, JobID: j.ID, CreatedUnix: time.Now().Unix(), SizeBytes: 8, PayloadBytes: 4,
		SHA256: strings.Repeat("a", 64), SourceRevision: strings.Repeat("b", 40), SchemaVersion: 8, Verification: "checksum_verified", Downloadable: true}, nil
}
func (r *fakeRunner) HasPublication(string) bool                { return r.publication.Load() }
func (r *fakeRunner) SealJobDirectory(string) error             { return nil }
func (r *fakeRunner) RemoveArchive(backupcontrol.Archive) error { return nil }

func workerFixture() (*Worker, *memoryJournal, *fakeController, *fakeRunner) {
	s := &memoryJournal{job: backupcontrol.Job{ID: strings.Repeat("a", 32), State: "queued", Phase: "queued", CreatedUnix: time.Now().Unix(), Revision: 1}, archives: map[string]backupcontrol.Archive{}}
	h, r := &fakeController{}, &fakeRunner{}
	w := New(s, h, r, Config{BackupTimeoutSeconds: 10, StopTimeoutSeconds: 5, ResumeTimeoutSeconds: 5})
	w.pollInterval = time.Millisecond
	w.downloadWait = 15 * time.Millisecond
	return w, s, h, r
}
func requireFinished(t *testing.T, s *memoryJournal, state, reason string) backupcontrol.Job {
	t.Helper()
	j, e := s.GetJob(context.Background(), s.job.ID)
	if e != nil || j.State != state || j.ErrorCode != reason || j.ResumeRequired || j.Phase != "complete" || j.FinishedUnix == 0 {
		t.Fatalf("completion: %+v, %v", j, e)
	}
	return j
}

func TestExecuteDurableStopIntentAndResumeBeforePublication(t *testing.T) {
	w, s, h, r := workerFixture()
	h.stop = func(ctx context.Context) error {
		j, _ := s.GetJob(ctx, s.job.ID)
		if !j.ResumeRequired || !j.PreServiceActive || j.Phase != "stopping_hub" {
			t.Fatalf("stop before durable resume intent: %+v", j)
		}
		return nil
	}
	r.create = func(ctx context.Context, j backupcontrol.Job) (Receipt, error) {
		if h.stops.Load() != 1 || h.starts.Load() != 0 || j.Phase != "creating_offline" {
			t.Fatal("creation outside confirmed offline interval")
		}
		r.publication.Store(true)
		return Receipt{}, nil
	}
	r.finalize = func(ctx context.Context, j backupcontrol.Job) (backupcontrol.Archive, error) {
		current, _ := s.GetJob(ctx, j.ID)
		if h.starts.Load() != 1 || current.ResumeRequired {
			t.Fatal("archive checking before confirmed resume")
		}
		return backupcontrol.Archive{ID: j.ID, JobID: j.ID}, nil
	}
	if e := w.execute(context.Background(), s.job.ID); e != nil {
		t.Fatal(e)
	}
	requireFinished(t, s, "succeeded", "")
	if len(s.archives) != 1 || !s.pruned || h.stops.Load() != 1 || h.starts.Load() != 1 {
		t.Fatal("missing completion work")
	}
}

func TestExecuteFailureBoundariesAlwaysResume(t *testing.T) {
	for _, tc := range []struct {
		name, reason           string
		pre, stop, create      error
		stops, creates, starts int32
	}{
		{"preflight", "preflight_refused", ErrPreflight, nil, nil, 0, 0, 0},
		{"ambiguous_stop", "hub_stop_unconfirmed", nil, ErrStop, nil, 1, 0, 1},
		{"cli_failure", "backup_creation_failed", nil, nil, ErrCreate, 1, 1, 1},
		{"cli_timeout", "backup_timed_out", nil, nil, context.DeadlineExceeded, 1, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, s, h, r := workerFixture()
			h.preflight = func(context.Context) error { return tc.pre }
			h.stop = func(context.Context) error { return tc.stop }
			r.create = func(context.Context, backupcontrol.Job) (Receipt, error) { return Receipt{}, tc.create }
			h.start = func(ctx context.Context) error {
				if ctx.Err() != nil {
					t.Fatal("cancelled resume context")
				}
				return nil
			}
			if e := w.execute(context.Background(), s.job.ID); e != nil {
				t.Fatal(e)
			}
			requireFinished(t, s, "failed", tc.reason)
			if h.stops.Load() != tc.stops || h.starts.Load() != tc.starts || r.creates.Load() != tc.creates {
				t.Fatal("unexpected privileged operation counts")
			}
		})
	}
}

func TestCancellationDuringCLIResumesWithIndependentContext(t *testing.T) {
	w, s, h, r := workerFixture()
	started := make(chan struct{})
	r.create = func(ctx context.Context, _ backupcontrol.Job) (Receipt, error) {
		close(started)
		<-ctx.Done()
		return Receipt{}, ctx.Err()
	}
	h.start = func(ctx context.Context) error {
		if ctx.Err() != nil {
			return fmt.Errorf("resume inherited cancellation")
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- w.execute(context.Background(), s.job.ID) }()
	<-started
	s.cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel was not polled")
	}
	requireFinished(t, s, "cancelled", "cancelled")
	if h.starts.Load() != 1 || r.finalizes.Load() != 0 {
		t.Fatal("cancel did not resume without publishing")
	}
}

func TestShutdownDuringStopStillResumes(t *testing.T) {
	w, s, h, r := workerFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.stop = func(context.Context) error { cancel(); return nil }
	h.start = func(ctx context.Context) error { return ctx.Err() }
	if e := w.execute(ctx, s.job.ID); e != nil {
		t.Fatal(e)
	}
	requireFinished(t, s, "cancelled", "cancelled")
	if h.starts.Load() != 1 || r.creates.Load() != 0 {
		t.Fatal("shutdown left a resume duty or started new work")
	}
}

func TestCreateTimeoutCancelsChildBeforeResuming(t *testing.T) {
	w, s, h, r := workerFixture()
	w.config.BackupTimeoutSeconds = 0 // Immediate deadline, using the real worker context path.
	r.create = func(ctx context.Context, _ backupcontrol.Job) (Receipt, error) {
		<-ctx.Done()
		return Receipt{}, ctx.Err()
	}
	h.start = func(ctx context.Context) error { return ctx.Err() }
	if e := w.execute(context.Background(), s.job.ID); e != nil {
		t.Fatal(e)
	}
	requireFinished(t, s, "failed", "backup_timed_out")
	if h.starts.Load() != 1 {
		t.Fatal("timeout suppressed resume")
	}
}

func TestCreateErrorAfterPublicationNeverRegistersOrRetries(t *testing.T) {
	w, s, h, r := workerFixture()
	r.create = func(context.Context, backupcontrol.Job) (Receipt, error) {
		r.publication.Store(true)
		return Receipt{}, ErrUnknownPublication
	}
	if e := w.execute(context.Background(), s.job.ID); e != nil {
		t.Fatal(e)
	}
	requireFinished(t, s, "failed", "publication_unknown")
	if e := w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !r.publication.Load() || len(s.archives) != 0 || r.creates.Load() != 1 || r.finalizes.Load() != 0 || h.starts.Load() != 1 {
		t.Fatal("ambiguous publication was retried, accepted, or removed")
	}
}

func TestRestartFailureDurableRecoveryNeverRepeatsCreate(t *testing.T) {
	w, s, h, r := workerFixture()
	h.start = func(context.Context) error { return ErrResume }
	if e := w.execute(context.Background(), s.job.ID); !errors.Is(e, ErrResume) {
		t.Fatalf("restart error: %v", e)
	}
	j, _ := s.GetJob(context.Background(), s.job.ID)
	if !j.ResumeRequired || j.State != "recovery_required" || w.Status() != "recovery_required" {
		t.Fatalf("lost resume duty: %+v", j)
	}
	h.start = nil
	if e := w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	j = requireFinished(t, s, "succeeded", "")
	if r.creates.Load() != 1 || h.starts.Load() != 2 || j.WarningCode != "recovered_after_interruption" {
		t.Fatal("recovery repeated creation or lost warning")
	}
}

func TestCrashRecoveryAndLostReceipt(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		resume, published, invalidReceipt bool
		state, code                       string
	}{
		{"before_stop", true, false, false, "failed", "interrupted"},
		{"during_create", true, false, false, "failed", "interrupted"},
		{"published_receipt_lost", true, true, true, "failed", "publication_unknown"},
		{"published_after_resume", false, true, false, "succeeded", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, s, h, r := workerFixture()
			s.job.State, s.job.Phase = "running", "creating_offline"
			s.job.ResumeRequired, s.job.PreServiceActive = tc.resume, true
			r.publication.Store(tc.published)
			if tc.invalidReceipt {
				r.finalize = func(context.Context, backupcontrol.Job) (backupcontrol.Archive, error) {
					return backupcontrol.Archive{}, ErrReceipt
				}
			}
			if e := w.Recover(context.Background()); e != nil {
				t.Fatal(e)
			}
			requireFinished(t, s, tc.state, tc.code)
			wantStarts := int32(0)
			if tc.resume {
				wantStarts = 1
			}
			if h.starts.Load() != wantStarts || h.stops.Load() != 0 || r.creates.Load() != 0 {
				t.Fatal("recovery issued a new backup/stop")
			}
			if tc.invalidReceipt && (len(s.archives) != 0 || !r.HasPublication(s.job.ID)) {
				t.Fatal("unknown publication was accepted or erased")
			}
		})
	}
}

func TestRecoverServiceDoesNotTouchArchiveWriter(t *testing.T) {
	w, s, h, r := workerFixture()
	s.job.State, s.job.Phase = "recovery_required", "restarting_hub"
	s.job.ResumeRequired, s.job.PreServiceActive = true, true
	r.publication.Store(true)
	r.finalize = func(context.Context, backupcontrol.Job) (backupcontrol.Archive, error) {
		t.Fatal("service recovery called unavailable archive writer")
		return backupcontrol.Archive{}, nil
	}
	if e := w.RecoverService(context.Background()); e != nil {
		t.Fatal(e)
	}
	if h.starts.Load() != 1 || r.creates.Load() != 0 || r.finalizes.Load() != 0 || s.job.ResumeRequired {
		t.Fatal("service-only recovery incomplete")
	}
}

func TestDownloadDrainBlocksStopAndSupportsCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			w, s, h, r := workerFixture()
			s.downloads = 1
			if cancel {
				s.cancel()
			}
			if e := w.execute(context.Background(), s.job.ID); e != nil {
				t.Fatal(e)
			}
			state, reason := "failed", "download_busy"
			if cancel {
				state, reason = "cancelled", "cancelled"
			}
			requireFinished(t, s, state, reason)
			if h.stops.Load() != 0 || h.preflights.Load() != 0 || r.creates.Load() != 0 {
				t.Fatal("active download interrupted")
			}
		})
	}
}

func TestDownloadDrainThenRun(t *testing.T) {
	w, s, h, _ := workerFixture()
	id := s.job.ID
	s.downloads = 1
	w.downloadWait = time.Second
	done := make(chan error, 1)
	go func() { done <- w.execute(context.Background(), id) }()
	deadline := time.After(time.Second)
	for {
		j, _ := s.GetJob(context.Background(), id)
		if j.State == "running" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("job not claimed before drain")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if h.stops.Load() != 0 {
		t.Fatal("stopped before download drained")
	}
	s.mu.Lock()
	s.downloads = 0
	s.mu.Unlock()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	requireFinished(t, s, "succeeded", "")
}

func TestJournalFailureBoundaries(t *testing.T) {
	for _, phase := range []string{"stopping_hub", "creating_offline", "restarting_hub"} {
		t.Run(phase, func(t *testing.T) {
			w, s, h, _ := workerFixture()
			failed := false
			s.fail = func(j backupcontrol.Job) error {
				if !failed && j.Phase == phase {
					failed = true
					return backupcontrol.ErrUnavailable
				}
				return nil
			}
			e := w.execute(context.Background(), s.job.ID)
			if phase == "stopping_hub" {
				if e == nil || h.stops.Load() != 0 || h.starts.Load() != 0 {
					t.Fatal("stop issued without durable intent")
				}
			} else if h.starts.Load() != 1 || s.job.ResumeRequired {
				t.Fatal("journal failure suppressed resume")
			}
		})
	}
}

func TestScheduleCapacityKeepsQueuedWorkAndControlLoopAvailable(t *testing.T) {
	w, s, h, _ := workerFixture()
	s.scheduleErr = backupcontrol.ErrCapacity
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if e := w.Run(ctx); e != nil {
		t.Fatalf("capacity killed worker: %v", e)
	}
	requireFinished(t, s, "succeeded", "")
	if h.stops.Load() != 1 || w.Status() != "ready" {
		t.Fatal("capacity prevented existing queued work")
	}
	// Actual journal unavailability is still fenced instead of ignored.
	w, s, h, _ = workerFixture()
	s.scheduleErr = backupcontrol.ErrUnavailable
	if e := w.Run(context.Background()); !errors.Is(e, backupcontrol.ErrUnavailable) || h.stops.Load() != 0 {
		t.Fatalf("unexpected journal failure bypassed: %v", e)
	}
}

func TestDownloadJournalErrorIsNotReportedAsBusy(t *testing.T) {
	w, s, h, _ := workerFixture()
	s.downloadErr = backupcontrol.ErrUnavailable
	if e := w.execute(context.Background(), s.job.ID); !errors.Is(e, backupcontrol.ErrUnavailable) {
		t.Fatalf("journal failure swallowed: %v", e)
	}
	if h.stops.Load() != 0 || s.job.ErrorCode == "download_busy" || backupcontrol.Terminal(s.job.State) {
		t.Fatal("journal failure was mislabeled as download contention")
	}
}

func TestQueuedCancellationBetweenReadAndClaimIsBenign(t *testing.T) {
	w, s, h, _ := workerFixture()
	s.job.State, s.job.Phase = "cancelled", "complete"
	s.job.FinishedUnix = time.Now().Unix()
	if e := w.execute(context.Background(), s.job.ID); e != nil {
		t.Fatalf("queued cancellation restarted worker: %v", e)
	}
	if h.stops.Load() != 0 {
		t.Fatal("cancelled queued job stopped HUB")
	}
}
