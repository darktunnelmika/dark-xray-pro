package backupworker

import (
	"context"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"testing"
)

type snapshotFakeRunner struct {
	*fakeRunner
	snapshot func(context.Context, backupcontrol.Job, func() error) (Receipt, error)
}

func (r *snapshotFakeRunner) CreateWithResume(c context.Context, j backupcontrol.Job, f func() error) (Receipt, error) {
	return r.snapshot(c, j, f)
}
func TestSnapshotResumeBeforePackingAndFailureDoesNotRestartTwice(t *testing.T) {
	for _, mode := range []string{"success", "packing_failure", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			w, s, h, r := workerFixture()
			w.runner = &snapshotFakeRunner{fakeRunner: r, snapshot: func(ctx context.Context, j backupcontrol.Job, resume func() error) (Receipt, error) {
				if h.stops.Load() != 1 || h.starts.Load() != 0 {
					t.Fatal("snapshot did not start offline")
				}
				if err := resume(); err != nil {
					return Receipt{}, err
				}
				current, _ := s.GetJob(ctx, j.ID)
				if current.ResumeRequired || h.starts.Load() != 1 || current.HubResumedUnix == 0 {
					t.Fatal("packing started before confirmed resume")
				}
				switch mode {
				case "packing_failure":
					return Receipt{}, ErrCreate
				case "cancelled":
					return Receipt{}, context.Canceled
				}
				r.publication.Store(true)
				return Receipt{}, nil
			}}
			if err := w.execute(context.Background(), s.job.ID); err != nil {
				t.Fatal(err)
			}
			state, reason := "succeeded", ""
			if mode == "packing_failure" {
				state, reason = "failed", "backup_creation_failed"
			}
			if mode == "cancelled" {
				state, reason = "cancelled", "cancelled"
			}
			requireFinished(t, s, state, reason)
			if h.starts.Load() != 1 {
				t.Fatal("panel restarted twice")
			}
			if mode != "success" && len(s.archives) != 0 {
				t.Fatal("failed archive published")
			}
		})
	}
}
func TestSnapshotResumeFailureRetainsDurableRecoveryDuty(t *testing.T) {
	w, s, h, r := workerFixture()
	h.start = func(context.Context) error { return ErrResume }
	w.runner = &snapshotFakeRunner{fakeRunner: r, snapshot: func(ctx context.Context, j backupcontrol.Job, resume func() error) (Receipt, error) {
		return Receipt{}, resume()
	}}
	if err := w.execute(context.Background(), s.job.ID); !errors.Is(err, ErrResume) {
		t.Fatal(err)
	}
	j, _ := s.GetJob(context.Background(), s.job.ID)
	if !j.ResumeRequired || j.State != "recovery_required" || len(s.archives) != 0 {
		t.Fatal("lost resume obligation")
	}
}
