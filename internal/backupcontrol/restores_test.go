package backupcontrol

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func restoreRequest(id string) RestoreRequest {
	return RestoreRequest{EventID: "restore-owner-request-0001", ActorID: "owner-1", SourceKind: "archive", SourceID: id, SHA256: strings.Repeat("a", 64), Confirmation: "RESTORE"}
}
func TestRestoreDurableReceiptExclusionAndStartBarrier(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.archive(t, "source-archive", false)
	q := restoreRequest(a.ID)
	bad := q
	bad.Confirmation = ""
	if _, e := f.s.CreateRestore(ctx, bad); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	response, e := f.s.CreateRestore(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	id := response.Job.ID
	if _, e = f.s.AcquireTransfer(ctx); !errors.Is(e, ErrBusy) {
		t.Fatal("transfer admitted", e)
	}
	if _, e = f.s.CreateJob(ctx, CreateRequest{ActorID: "owner-1", EventID: "another-backup", Action: "create"}); !errors.Is(e, ErrBusy) {
		t.Fatal("backup admitted", e)
	}
	if _, e = f.s.PruneArchives(ctx, time.Now(), func(Archive) error { t.Fatal("pruning ran during restore"); return nil }); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	bad = q
	bad.SHA256 = strings.Repeat("b", 64)
	if _, e = f.s.CreateRestore(ctx, bad); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	for _, phase := range []string{"validating", "stopping_hub", "applying", "starting_restored"} {
		_, e = f.s.UpdateRestore(ctx, id, func(j *RestoreJob) error {
			j.State = "running"
			j.Phase = phase
			j.TableCount = 60
			j.DataRestored = phase == "starting_restored"
			return nil
		})
		if e != nil {
			t.Fatal(phase, e)
		}
	}
	if _, e = f.s.UpdateRestore(ctx, id, func(j *RestoreJob) error { j.Phase = "rolling_back"; j.DataRestored = false; return nil }); !errors.Is(e, ErrConflict) {
		t.Fatal("start barrier allowed rewind", e)
	}
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	replay, found, e := f.s.FindRestoreReceipt(ctx, q)
	if e != nil || !found || !replay.Replayed || replay.Job.Phase != "starting_restored" {
		t.Fatal("receipt lost on restart", e)
	}
	_, e = f.s.UpdateRestore(ctx, id, func(j *RestoreJob) error {
		j.State = "succeeded"
		j.Phase = "complete"
		j.FinishedUnix = f.now.Unix()
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	after, e := f.s.CreateRestore(ctx, q)
	if e != nil || !after.Replayed || after.Job.State != "succeeded" {
		t.Fatal("restore replay repeated work", e)
	}
	jobs, e := f.s.RestoreJobs(ctx)
	if e != nil || len(jobs) != 1 {
		t.Fatal(e, len(jobs))
	}
}
func TestRestoreRefusesExistingTransfer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	release, e := f.s.AcquireTransfer(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.CreateRestore(ctx, restoreRequest(strings.Repeat("1", 32))); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	release()
	if _, e = f.s.CreateRestore(ctx, restoreRequest(strings.Repeat("1", 32))); e != nil {
		t.Fatal(e)
	}
}
func TestRestoreAndBackupHaveOneAtomicAdmission(t *testing.T) {
	for i := 0; i < 12; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, e := f.s.CreateRestore(ctx, restoreRequest(strings.Repeat("1", 32)))
				errs <- e
			}()
			go func() {
				defer wg.Done()
				_, e := f.s.CreateJob(ctx, CreateRequest{EventID: "create-competing", ActorID: "owner-1", Action: "create"})
				errs <- e
			}()
			wg.Wait()
			close(errs)
			success, busy := 0, 0
			for e := range errs {
				if e == nil {
					success++
				} else if errors.Is(e, ErrBusy) {
					busy++
				} else {
					t.Fatal(e)
				}
			}
			if success != 1 || busy != 1 {
				t.Fatal(success, busy)
			}
		})
	}
}
