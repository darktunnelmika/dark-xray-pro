package backupworker

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

// These narrow interfaces allow failure-at-every-boundary tests without a
// privileged systemd invocation or access to a real financial database.
type Journal interface {
	GetJob(context.Context, string) (backupcontrol.Job, error)
	UpdateJob(context.Context, string, func(*backupcontrol.Job) error) (backupcontrol.Job, error)
	RecoverableJobs(context.Context) ([]backupcontrol.Job, error)
	PutArchive(context.Context, backupcontrol.Archive) (backupcontrol.Archive, error)
	EnqueueScheduled(context.Context, time.Time) (*backupcontrol.Job, error)
	PruneArchives(context.Context, time.Time, func(backupcontrol.Archive) error) ([]string, error)
	ActiveDownloads(context.Context) (int, error)
}
type Controller interface {
	Preflight(context.Context) error
	Stop(context.Context) error
	Start(context.Context) error
}
type Runner interface {
	Create(context.Context, backupcontrol.Job) (Receipt, error)
	Finalize(context.Context, backupcontrol.Job) (backupcontrol.Archive, error)
	HasPublication(string) bool
	SealJobDirectory(string) error
	RemoveArchive(backupcontrol.Archive) error
}

type Worker struct {
	store                      Journal
	host                       Controller
	runner                     Runner
	config                     Config
	status                     atomic.Value
	pollInterval, downloadWait time.Duration
}

func New(store Journal, host Controller, runner Runner, c Config) *Worker {
	w := &Worker{store: store, host: host, runner: runner, config: c, pollInterval: 250 * time.Millisecond, downloadWait: 30 * time.Second}
	w.status.Store("starting")
	return w
}
func (w *Worker) Status() string {
	switch s := w.status.Load().(string); s {
	case "idle":
		return "ready"
	case "starting", "recovering", "running":
		return "busy"
	case "recovery_required":
		return s
	default:
		return "unavailable"
	}
}
func (w *Worker) setStatus(s string) { w.status.Store(s) }

func (w *Worker) update(id string, fn func(*backupcontrol.Job) error) (backupcontrol.Job, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.store.UpdateJob(ctx, id, fn)
}

func (w *Worker) finish(id, state, code, warning string) error {
	if state != "succeeded" {
		if e := w.runner.SealJobDirectory(id); e != nil {
			return e
		}
	}
	_, e := w.update(id, func(j *backupcontrol.Job) error {
		if j.ResumeRequired {
			return ErrResume
		}
		j.State = state
		j.Phase = "complete"
		j.FinishedUnix = time.Now().Unix()
		j.ErrorCode = code
		j.WarningCode = warning
		return nil
	})
	return e
}

func (w *Worker) resume(j backupcontrol.Job) error {
	if !j.ResumeRequired {
		return nil
	}
	if !j.PreServiceActive {
		return ErrResume
	}
	// Even a journal error must not prevent the already-durable resume duty.
	_, recordErr := w.update(j.ID, func(v *backupcontrol.Job) error { v.Phase = "restarting_hub"; return nil })
	ctx, cancel := context.WithTimeout(context.Background(), seconds(w.config.ResumeTimeoutSeconds))
	defer cancel()
	if e := w.host.Start(ctx); e != nil {
		_, _ = w.update(j.ID, func(v *backupcontrol.Job) error {
			v.State = "recovery_required"
			v.Phase = "restarting_hub"
			v.ErrorCode = "hub_resume_unconfirmed"
			return nil
		})
		w.setStatus("recovery_required")
		return ErrResume
	}
	_, e := w.update(j.ID, func(v *backupcontrol.Job) error {
		v.ResumeRequired = false
		v.HubResumedUnix = time.Now().Unix()
		if v.HubStoppedUnix > 0 && v.HubResumedUnix >= v.HubStoppedUnix {
			v.DowntimeMS = (v.HubResumedUnix - v.HubStoppedUnix) * 1000
		}
		v.State = "running"
		v.Phase = "checking_archive"
		return nil
	})
	if e != nil {
		return e
	}
	return recordErr
}

func (w *Worker) finalize(j backupcontrol.Job, warning string) error {
	ctx, cancel := context.WithTimeout(context.Background(), seconds(w.config.BackupTimeoutSeconds))
	defer cancel()
	a, e := w.runner.Finalize(ctx, j)
	if e != nil {
		return w.finish(j.ID, "failed", "publication_unknown", warning)
	}
	if _, e = w.store.PutArchive(ctx, a); e != nil {
		return e
	}
	_, e = w.update(j.ID, func(v *backupcontrol.Job) error { v.ArchiveID = a.ID; v.Phase = "retention"; return nil })
	if e != nil {
		return e
	}
	if _, e = w.store.PruneArchives(ctx, time.Now(), w.runner.RemoveArchive); e != nil {
		warning = "retention_incomplete"
	}
	return w.finish(j.ID, "succeeded", "", warning)
}

// Recover never reruns create. It first fulfils an interrupted resume duty and
// then accepts only a complete receipt and independently checked published file.
func (w *Worker) Recover(ctx context.Context) error {
	w.setStatus("recovering")
	jobs, e := w.store.RecoverableJobs(ctx)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		if j.State == "queued" && !j.ResumeRequired {
			continue
		}
		if e = w.resume(j); e != nil {
			return e
		}
		if w.runner.HasPublication(j.ID) {
			if e = w.finalize(j, "recovered_after_interruption"); e != nil {
				return e
			}
		} else {
			state := "failed"
			code := "interrupted"
			if j.CancelRequested {
				state = "cancelled"
				code = "cancelled"
			}
			if e = w.finish(j.ID, state, code, ""); e != nil {
				return e
			}
		}
	}
	w.setStatus("idle")
	return nil
}

// RecoverService is the bounded ExecStopPost path: restore service availability
// first, leaving archive hashing and reconciliation for the next normal start.
func (w *Worker) RecoverService(ctx context.Context) error {
	jobs, e := w.store.RecoverableJobs(ctx)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		if j.ResumeRequired {
			if e = w.resume(j); e != nil {
				return e
			}
		}
	}
	return nil
}

func (w *Worker) waitDownloads(ctx context.Context, id string) error {
	deadline := time.NewTimer(w.downloadWait)
	defer deadline.Stop()
	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		j, e := w.store.GetJob(ctx, id)
		if e != nil {
			return e
		}
		if j.CancelRequested {
			return context.Canceled
		}
		n, e := w.store.ActiveDownloads(ctx)
		if e != nil {
			return e
		}
		if n == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return backupcontrol.ErrBusy
		case <-tick.C:
		}
	}
}

func (w *Worker) execute(ctx context.Context, id string) error {
	j, e := w.update(id, func(v *backupcontrol.Job) error {
		if v.State != "queued" {
			return backupcontrol.ErrConflict
		}
		v.State = "running"
		v.Phase = "preflight"
		v.StartedUnix = time.Now().Unix()
		return nil
	})
	if e != nil {
		// Cancellation may finish a queued job between the loop's read and
		// this atomic claim. It needs no stop and is not a worker failure.
		if errors.Is(e, backupcontrol.ErrConflict) {
			current, readErr := w.store.GetJob(ctx, id)
			if readErr == nil && backupcontrol.Terminal(current.State) {
				return nil
			}
		}
		return e
	}
	w.setStatus("running")
	if e = w.waitDownloads(ctx, id); e != nil {
		if errors.Is(e, context.Canceled) {
			return w.finish(id, "cancelled", "cancelled", "")
		}
		if errors.Is(e, backupcontrol.ErrBusy) {
			return w.finish(id, "failed", "download_busy", "")
		}
		return e
	}
	pre, preCancel := context.WithTimeout(ctx, 15*time.Second)
	e = w.host.Preflight(pre)
	preCancel()
	if e != nil {
		return w.finish(id, "failed", "preflight_refused", "")
	}
	j, e = w.update(id, func(v *backupcontrol.Job) error {
		if v.CancelRequested {
			return context.Canceled
		}
		v.ResumeRequired = true
		v.PreServiceActive = true
		v.Phase = "stopping_hub"
		return nil
	})
	if e != nil {
		if errors.Is(e, context.Canceled) {
			return w.finish(id, "cancelled", "cancelled", "")
		}
		return e
	}
	// This intent is durable before stop. All paths below call resume with a
	// fresh context, including failed/ambiguous stop and process cancellation.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), seconds(w.config.StopTimeoutSeconds))
	createErr := w.host.Stop(stopCtx)
	stopCancel()
	if createErr == nil {
		j, e = w.update(id, func(v *backupcontrol.Job) error {
			v.HubStoppedUnix = time.Now().Unix()
			v.Phase = "creating_offline"
			return nil
		})
		if e != nil {
			createErr = e
		} else if j.CancelRequested || ctx.Err() != nil {
			createErr = context.Canceled
		} else {
			backupCtx, backupCancel := context.WithTimeout(ctx, seconds(w.config.BackupTimeoutSeconds))
			pollDone := make(chan struct{})
			pollStopped := make(chan struct{})
			go func() {
				defer close(pollStopped)
				ticker := time.NewTicker(w.pollInterval)
				defer ticker.Stop()
				for {
					select {
					case <-pollDone:
						return
					case <-backupCtx.Done():
						return
					case <-ticker.C:
						current, e := w.store.GetJob(backupCtx, id)
						if e != nil || current.CancelRequested {
							backupCancel()
							return
						}
					}
				}
			}()
			_, createErr = w.runner.Create(backupCtx, j)
			close(pollDone)
			backupCancel()
			<-pollStopped
		}
	}
	// Use the original intent if a failed journal update zeroed the local job.
	intent := backupcontrol.Job{ID: id, ResumeRequired: true, PreServiceActive: true}
	if e = w.resume(intent); e != nil {
		return e
	}
	current, e := w.store.GetJob(context.Background(), id)
	if e != nil {
		return e
	}
	if createErr != nil {
		code := "backup_creation_failed"
		state := "failed"
		if errors.Is(createErr, ErrStop) {
			code = "hub_stop_unconfirmed"
		}
		if errors.Is(createErr, context.DeadlineExceeded) {
			code = "backup_timed_out"
		}
		if errors.Is(createErr, context.Canceled) || current.CancelRequested {
			state = "cancelled"
			code = "cancelled"
		}
		if w.runner.HasPublication(id) {
			code = "publication_unknown"
		}
		return w.finish(id, state, code, "")
	}
	warning := ""
	if current.CancelRequested {
		warning = "cancel_requested_after_publication"
	}
	return w.finalize(current, warning)
}

// Run owns the execution loop. HTTP requests only append durable intents.
func (w *Worker) Run(ctx context.Context) error {
	if e := w.Recover(ctx); e != nil {
		return e
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		// Capacity is an expected, visible control-plane limit. Keep status,
		// existing downloads/settings and already queued work available.
		if _, e := w.store.EnqueueScheduled(ctx, time.Now()); e != nil && !errors.Is(e, backupcontrol.ErrCapacity) {
			return e
		}
		jobs, e := w.store.RecoverableJobs(ctx)
		if e != nil {
			return e
		}
		hasRecovery := false
		for _, j := range jobs {
			if j.State != "queued" {
				hasRecovery = true
				break
			}
		}
		if hasRecovery {
			if e = w.Recover(ctx); e != nil {
				return e
			}
		}
		for _, j := range jobs {
			if j.State == "queued" {
				e = w.execute(ctx, j.ID)
				if e != nil {
					return e
				}
				break
			}
		}
		w.setStatus("idle")
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
