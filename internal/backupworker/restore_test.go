package backupworker

import (
	"context"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"testing"
	"time"
)

type restoreFixture struct {
	job                               backupcontrol.RestoreJob
	calls                             []string
	validationErr, applyErr, startErr error
	failStartJournal                  bool
}

func newRestoreFixture() *restoreFixture {
	return &restoreFixture{job: backupcontrol.RestoreJob{ID: "11111111111111111111111111111111", State: "queued", Phase: "queued", Revision: 1}}
}
func (f *restoreFixture) CreateRestore(context.Context, backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, error) {
	return backupcontrol.RestoreResponse{Job: f.job}, nil
}
func (f *restoreFixture) FindRestoreReceipt(context.Context, backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, bool, error) {
	return backupcontrol.RestoreResponse{}, false, nil
}
func (f *restoreFixture) RestoreJobs(context.Context) ([]backupcontrol.RestoreJob, error) {
	return []backupcontrol.RestoreJob{f.job}, nil
}
func (f *restoreFixture) UpdateRestore(_ context.Context, _ string, fn func(*backupcontrol.RestoreJob) error) (backupcontrol.RestoreJob, error) {
	if e := fn(&f.job); e != nil {
		return backupcontrol.RestoreJob{}, e
	}
	f.job.Revision++
	if f.failStartJournal && f.job.Phase == "starting_restored" {
		f.failStartJournal = false
		return backupcontrol.RestoreJob{}, backupcontrol.ErrUnavailable
	}
	return f.job, nil
}
func (f *restoreFixture) Available() bool                                                 { return true }
func (f *restoreFixture) CheckSource(context.Context, backupcontrol.RestoreRequest) error { return nil }
func (f *restoreFixture) Preflight(context.Context) error {
	f.calls = append(f.calls, "preflight")
	return nil
}
func (f *restoreFixture) Stop(context.Context) error { f.calls = append(f.calls, "stop"); return nil }
func (f *restoreFixture) Start(context.Context) error {
	f.calls = append(f.calls, "start")
	return f.startErr
}
func (f *restoreFixture) PrepareRestore(context.Context, backupcontrol.RestoreJob) (int, error) {
	f.calls = append(f.calls, "validate")
	return 60, f.validationErr
}
func (f *restoreFixture) ApplyRestore(context.Context, backupcontrol.RestoreJob) error {
	f.calls = append(f.calls, "apply")
	return f.applyErr
}
func (f *restoreFixture) RollbackRestore(context.Context, backupcontrol.RestoreJob) error {
	f.calls = append(f.calls, "rollback")
	return nil
}
func (f *restoreFixture) ReleaseRestore(_ context.Context, _ backupcontrol.RestoreJob, restored bool) error {
	if restored {
		f.calls = append(f.calls, "release-restored")
	} else {
		f.calls = append(f.calls, "release-original")
	}
	return nil
}
func countCall(f *restoreFixture, s string) int {
	n := 0
	for _, v := range f.calls {
		if v == s {
			n++
		}
	}
	return n
}
func TestPanelRestoreValidatesBeforeStopAndActuallyApplies(t *testing.T) {
	f := newRestoreFixture()
	c := NewRestoreCoordinator(f, f, f, time.Second)
	worked, e := c.RunOne(context.Background())
	if e != nil || !worked || f.job.State != "succeeded" || !f.job.DataRestored || countCall(f, "apply") != 1 || countCall(f, "rollback") != 0 {
		t.Fatal(f.job, f.calls, e)
	}
	if _, e = c.RunOne(context.Background()); e != nil || countCall(f, "apply") != 1 {
		t.Fatal("completed job repeated", e)
	}
}
func TestPanelRestoreCorruptArchiveNeverStopsHub(t *testing.T) {
	f := newRestoreFixture()
	f.validationErr = errors.New("corrupt archive")
	c := NewRestoreCoordinator(f, f, f, time.Second)
	_, e := c.RunOne(context.Background())
	if e != nil || f.job.State != "failed" || countCall(f, "stop") != 0 || countCall(f, "apply") != 0 {
		t.Fatal(f.job, f.calls, e)
	}
}
func TestPanelRestoreApplyFailureRollsBackBeforeOriginalStart(t *testing.T) {
	f := newRestoreFixture()
	f.applyErr = errors.New("disk failure")
	c := NewRestoreCoordinator(f, f, f, time.Second)
	_, e := c.RunOne(context.Background())
	if e != nil || f.job.State != "failed" || f.job.DataRestored || countCall(f, "rollback") != 1 || countCall(f, "release-original") != 1 {
		t.Fatal(f.job, f.calls, e)
	}
}
func TestPanelRestoreStartFailureAndAmbiguousJournalNeverRollback(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		f := newRestoreFixture()
		f.failStartJournal = ambiguous
		if !ambiguous {
			f.startErr = ErrResume
		}
		c := NewRestoreCoordinator(f, f, f, time.Second)
		_, e := c.RunOne(context.Background())
		if e == nil || countCall(f, "rollback") != 0 {
			t.Fatal("failure rewound financial state", f.calls, e)
		}
		f.startErr = nil
		c = NewRestoreCoordinator(f, f, f, time.Second)
		if e = c.Recover(context.Background()); e != nil {
			t.Fatal(e)
		}
		if f.job.State != "succeeded" || countCall(f, "apply") != 1 || countCall(f, "rollback") != 0 {
			t.Fatal(f.job, f.calls)
		}
	}
}
func TestPanelRestoreCrashDuringApplyRecoversBeforeStarting(t *testing.T) {
	f := newRestoreFixture()
	f.job.State = "running"
	f.job.Phase = "applying"
	f.job.TableCount = 60
	c := NewRestoreCoordinator(f, f, f, time.Second)
	if e := c.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.job.State != "failed" || countCall(f, "rollback") != 1 || countCall(f, "apply") != 0 {
		t.Fatal(f.job, f.calls)
	}
}
