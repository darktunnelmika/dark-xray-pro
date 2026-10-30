package backupworker

import (
	"context"
	"errors"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

type RestoreJournal interface {
	CreateRestore(context.Context, backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, error)
	FindRestoreReceipt(context.Context, backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, bool, error)
	RestoreJobs(context.Context) ([]backupcontrol.RestoreJob, error)
	UpdateRestore(context.Context, string, func(*backupcontrol.RestoreJob) error) (backupcontrol.RestoreJob, error)
}
type RestoreRunner interface {
	Available() bool
	CheckSource(context.Context, backupcontrol.RestoreRequest) error
	PrepareRestore(context.Context, backupcontrol.RestoreJob) (int, error)
	ApplyRestore(context.Context, backupcontrol.RestoreJob) error
	RollbackRestore(context.Context, backupcontrol.RestoreJob) error
	ReleaseRestore(context.Context, backupcontrol.RestoreJob, bool) error
}
type RestoreCoordinator struct {
	store   RestoreJournal
	host    Controller
	runner  RestoreRunner
	timeout time.Duration
}

func NewRestoreCoordinator(store RestoreJournal, host Controller, runner RestoreRunner, timeout time.Duration) *RestoreCoordinator {
	return &RestoreCoordinator{store: store, host: host, runner: runner, timeout: timeout}
}
func (c *RestoreCoordinator) Available() bool {
	return c != nil && c.runner != nil && c.runner.Available()
}
func (c *RestoreCoordinator) Create(ctx context.Context, q backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, error) {
	if out, found, e := c.store.FindRestoreReceipt(ctx, q); found || e != nil {
		return out, e
	}
	if !c.Available() {
		return backupcontrol.RestoreResponse{}, backupcontrol.ErrUnavailable
	}
	if e := c.runner.CheckSource(ctx, q); e != nil {
		return backupcontrol.RestoreResponse{}, e
	}
	return c.store.CreateRestore(ctx, q)
}
func (c *RestoreCoordinator) update(id, phase, state, code string, count int, restored bool) (backupcontrol.RestoreJob, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return c.store.UpdateRestore(ctx, id, func(j *backupcontrol.RestoreJob) error {
		j.Phase = phase
		j.State = state
		j.ErrorCode = code
		if count > 0 {
			j.TableCount = count
		}
		j.DataRestored = restored
		if backupcontrol.Terminal(state) {
			j.FinishedUnix = time.Now().Unix()
		}
		return nil
	})
}
func (c *RestoreCoordinator) attention(j backupcontrol.RestoreJob) error {
	_, _ = c.update(j.ID, j.Phase, "recovery_required", "restore_recovery_required", j.TableCount, j.DataRestored)
	return ErrResume
}
func (c *RestoreCoordinator) start(j backupcontrol.RestoreJob, restored bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	if e := c.runner.ReleaseRestore(ctx, j, restored); e != nil {
		return c.attention(j)
	}
	if e := c.host.Start(ctx); e != nil {
		return c.attention(j)
	}
	state, code := "failed", "restore_not_applied"
	if restored {
		state, code = "succeeded", ""
	}
	_, e := c.update(j.ID, "complete", state, code, j.TableCount, restored)
	return e
}
func (c *RestoreCoordinator) rollback(j backupcontrol.RestoreJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	// Stop must be independently confirmed even after a failed/ambiguous stop.
	// It is legal only before the persisted first-restored-start barrier.
	if e := c.host.Stop(ctx); e != nil {
		return c.attention(j)
	}
	updated, e := c.update(j.ID, "rolling_back", "running", "restore_not_applied", j.TableCount, false)
	if e != nil {
		return e
	}
	if e = c.runner.RollbackRestore(ctx, updated); e != nil {
		return c.attention(updated)
	}
	updated, e = c.update(j.ID, "starting_original", "running", "restore_not_applied", j.TableCount, false)
	if e != nil {
		return e
	}
	return c.start(updated, false)
}
func (c *RestoreCoordinator) recoverJob(j backupcontrol.RestoreJob) error {
	switch j.Phase {
	case "queued":
		return nil
	case "validating":
		_, e := c.update(j.ID, "complete", "failed", "restore_validation_interrupted", 0, false)
		return e
	case "stopping_hub", "applying", "rolling_back":
		return c.rollback(j)
	case "starting_original":
		return c.start(j, false)
	case "starting_restored":
		return c.start(j, true)
	default:
		return backupcontrol.ErrUnsafe
	}
}
func (c *RestoreCoordinator) Recover(ctx context.Context) error {
	jobs, e := c.store.RestoreJobs(ctx)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		if !backupcontrol.Terminal(j.State) {
			if e = c.recoverJob(j); e != nil {
				return e
			}
		}
	}
	return nil
}

// RunOne is called by the single backup execution loop, not by an HTTP handler.
func (c *RestoreCoordinator) RunOne(ctx context.Context) (bool, error) {
	jobs, e := c.store.RestoreJobs(ctx)
	if e != nil {
		return false, e
	}
	for _, j := range jobs {
		if backupcontrol.Terminal(j.State) {
			continue
		}
		if j.State != "queued" {
			return true, c.recoverJob(j)
		}
		j, e = c.update(j.ID, "validating", "running", "", 0, false)
		if e != nil {
			return true, e
		}
		work, cancel := context.WithTimeout(ctx, c.timeout)
		if e = c.host.Preflight(work); e == nil && c.Available() {
			var count int
			count, e = c.runner.PrepareRestore(work, j)
			if e == nil && count <= 0 {
				e = ErrReceipt
			}
			if e == nil {
				j, e = c.update(j.ID, "validating", "running", "", count, false)
			}
		} else if e == nil {
			e = ErrPreflight
		}
		if e == nil {
			e = c.host.Preflight(work)
		}
		if e != nil {
			cancel()
			// No stop has happened. A broken journal is not made healthy by a retry.
			if errors.Is(e, backupcontrol.ErrUnavailable) || errors.Is(e, backupcontrol.ErrUnsafe) {
				return true, e
			}
			_, e = c.update(j.ID, "complete", "failed", "restore_validation_failed", 0, false)
			return true, e
		}
		j, e = c.update(j.ID, "stopping_hub", "running", "", j.TableCount, false)
		if e != nil {
			cancel()
			return true, e
		}
		if e = c.host.Stop(work); e != nil {
			cancel()
			return true, c.rollback(j)
		}
		j, e = c.update(j.ID, "applying", "running", "", j.TableCount, false)
		if e != nil {
			cancel()
			return true, e
		}
		if e = c.runner.ApplyRestore(work, j); e != nil {
			cancel()
			return true, c.rollback(j)
		}
		cancel()
		// This durable intent is BEFORE opening any restored service to writes.
		// Failure or a lost health response after here never restores old balances.
		j, e = c.update(j.ID, "starting_restored", "running", "", j.TableCount, true)
		if e != nil {
			return true, e
		}
		return true, c.start(j, true)
	}
	return false, nil
}
