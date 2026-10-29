//go:build linux

package backupcontrol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

type fixture struct {
	s              *Store
	dir, recipient string
	now            time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{dir: filepath.Join(t.TempDir(), "journal"), recipient: key.Recipient().String(), now: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	f.reopen(t)
	t.Cleanup(func() { _ = f.s.Close() })
	return f
}
func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	var err error
	f.s, err = openStore(f.dir, f.recipient, uint32(os.Geteuid()), func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) create(t *testing.T, event string) Job {
	t.Helper()
	r, err := f.s.CreateJob(context.Background(), CreateRequest{EventID: event, ActorID: "owner-1", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	return r.Job
}
func (f *fixture) finish(t *testing.T, id string) {
	t.Helper()
	_, err := f.s.UpdateJob(context.Background(), id, func(j *Job) error {
		j.State = "succeeded"
		j.Phase = "complete"
		j.FinishedUnix = f.now.Unix()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) archive(t *testing.T, event string, restored bool) Archive {
	t.Helper()
	j := f.create(t, event)
	f.finish(t, j.ID)
	a := Archive{ID: j.ID, JobID: j.ID, CreatedUnix: f.now.Unix(), SizeBytes: 4, PayloadBytes: 2, SHA256: strings.Repeat("a", 64), SourceRevision: strings.Repeat("b", 40), SchemaVersion: 8, Verification: "checksum_verified", Downloadable: true}
	if restored {
		a.Verification = "restore_verified"
	}
	a, err := f.s.PutArchive(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDurableReceiptsExclusiveLockAndCAS(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if duplicate, err := openStore(f.dir, f.recipient, uint32(os.Geteuid()), time.Now); !errors.Is(err, ErrBusy) {
		if duplicate != nil {
			duplicate.Close()
		}
		t.Fatalf("second process lock: %v", err)
	}
	r := CreateRequest{EventID: "create-1", ActorID: "owner-1", Action: "create"}
	first, err := f.s.CreateJob(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateJob(ctx, CreateRequest{EventID: r.EventID, ActorID: "owner-2", Action: r.Action}); !errors.Is(err, ErrConflict) {
		t.Fatalf("event was reusable by a different actor: %v", err)
	}
	if _, err = f.s.CreateJob(ctx, CreateRequest{EventID: "create-2", ActorID: r.ActorID, Action: r.Action}); !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel job: %v", err)
	}
	settings := DefaultSettings()
	update := SettingsRequest{EventID: "settings-1", ActorID: r.ActorID, ExpectedRevision: 1, Schedule: settings.Schedule, Retention: Retention{KeepLast: 3, MaxAgeDays: 30}}
	committed, err := f.s.UpdateSettings(ctx, update)
	if err != nil || committed.Settings.Revision != 2 {
		t.Fatalf("settings commit: %+v %v", committed, err)
	}
	stale := update
	stale.EventID = "settings-stale"
	if _, err = f.s.UpdateSettings(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS: %v", err)
	}
	newer := update
	newer.EventID = "settings-2"
	newer.ExpectedRevision = 2
	newer.Retention.KeepLast = 4
	if _, err = f.s.UpdateSettings(ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	replayed, err := f.s.CreateJob(ctx, r)
	if err != nil || !replayed.Replayed || replayed.Job.ID != first.Job.ID {
		t.Fatalf("lost create receipt: %+v %v", replayed, err)
	}
	again, err := f.s.UpdateSettings(ctx, update)
	if err != nil || !again.Replayed || again.Settings != committed.Settings {
		t.Fatalf("settings replay must return original revision: %+v %v", again, err)
	}
	st, err := os.Stat(filepath.Join(f.dir, "control.json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("journal permissions: %v %v", st, err)
	}
}

func TestResumeIntentAndCancellationCannotBeLost(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	j := f.create(t, "create-1")
	_, err := f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.State = "running"
		v.Phase = "preflight"
		v.StartedUnix = f.now.Unix()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.ResumeRequired = true
		v.PreServiceActive = true
		v.Phase = "stopping_hub"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.State = "failed"
		v.Phase = "complete"
		v.FinishedUnix = f.now.Unix()
		return nil
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("terminal while restart owed: %v", err)
	}
	if _, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error { v.ResumeRequired = false; return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("erased resume intent: %v", err)
	}
	_, err = f.s.CancelJob(ctx, j.ID, CancelRequest{EventID: "cancel-1", ActorID: "owner-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.State = "running"
		v.CancelRequested = false
		v.Phase = "restarting_hub"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	jobs, err := f.s.RecoverableJobs(ctx)
	if err != nil || len(jobs) != 1 || !jobs[0].ResumeRequired || !jobs[0].CancelRequested || jobs[0].State != "cancelling" {
		t.Fatalf("lost recovery duty: %+v %v", jobs, err)
	}
	_, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.ResumeRequired = false
		v.HubResumedUnix = f.now.Unix()
		v.Phase = "checking_archive"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.State = "cancelled"
		v.Phase = "complete"
		v.FinishedUnix = f.now.Unix()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UpdateJob(ctx, j.ID, func(v *Job) error { v.State = "running"; return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal resurrected: %v", err)
	}
}

func TestJournalRejectsCorruptionSymlinksAndUnsafeModes(t *testing.T) {
	for _, name := range []string{"malformed", "duplicate", "mode", "symlink", "hardlink", "lock_replaced"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.create(t, "create-1")
			path := filepath.Join(f.dir, "control.json")
			if name == "lock_replaced" {
				if err := os.Rename(filepath.Join(f.dir, "control.lock"), filepath.Join(f.dir, "old.lock")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(f.dir, "control.lock"), nil, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.Status(context.Background()); !errors.Is(err, ErrUnsafe) {
					t.Fatalf("replaced lock accepted: %v", err)
				}
				return
			}
			if err := f.s.Close(); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "malformed":
				if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				raw = append([]byte(`{"version":1,`), raw[1:]...)
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".target", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".alias"); err != nil {
					t.Fatal(err)
				}
			}
			s, err := openStore(f.dir, f.recipient, uint32(os.Geteuid()), time.Now)
			if err == nil {
				s.Close()
				t.Fatal("unsafe journal accepted")
			}
		})
	}
}

func TestReceiptCapacityFailsClosedWithoutForgettingEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	j := f.create(t, "create-1")
	f.finish(t, j.ID)
	f.s.mu.Lock()
	d := cloneJournal(f.s.data)
	for i := len(d.Receipts); i < MaxReceipts; i++ {
		d.Receipts[fmt.Sprintf("capacity-%d", i)] = receipt{Kind: "create", Digest: strings.Repeat("a", 64), JobID: j.ID}
	}
	err := f.s.commit(d)
	f.s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateJob(ctx, CreateRequest{EventID: "beyond-cap", ActorID: "owner-1", Action: "create"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	r, err := f.s.CreateJob(ctx, CreateRequest{EventID: "create-1", ActorID: "owner-1", Action: "create"})
	if err != nil || !r.Replayed || r.Job.ID != j.ID {
		t.Fatalf("original receipt lost: %+v %v", r, err)
	}
}

func TestDurabilityFailureBeforeAndAfterAtomicPublication(t *testing.T) {
	for _, boundary := range []string{"file_sync", "directory_sync"} {
		t.Run(boundary, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			request := CreateRequest{EventID: "durability-test", ActorID: "owner-1", Action: "create"}
			failure := errors.New("injected fsync failure")
			if boundary == "file_sync" {
				f.s.syncFile = func(*os.File) error { return failure }
			} else {
				f.s.syncDir = func(string) error { return failure }
			}
			if _, err := f.s.CreateJob(ctx, request); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("uncommitted response: %v", err)
			}
			if boundary == "directory_sync" {
				// Rename happened, but its durability is uncertain. Continuing
				// from stale memory could repeat an already-published request.
				if _, err := f.s.Status(ctx); !errors.Is(err, ErrUnavailable) {
					t.Fatalf("post-rename uncertainty did not fence the store: %v", err)
				}
				if _, err := f.s.CreateJob(ctx, request); !errors.Is(err, ErrUnavailable) {
					t.Fatalf("accepted mutation after uncertain commit: %v", err)
				}
			} else {
				status, err := f.s.Status(ctx)
				if err != nil || len(status.Jobs) != 0 {
					t.Fatalf("unpublished journal changed: %+v %v", status, err)
				}
			}
			if err := f.s.Close(); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			recovered, err := f.s.CreateJob(ctx, request)
			if err != nil || recovered.Replayed != (boundary == "directory_sync") {
				t.Fatalf("reopened receipt: %+v %v", recovered, err)
			}
		})
	}
}
