//go:build linux

package backupcontrol

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type memoryArchive struct{ *bytes.Reader }

func (*memoryArchive) Close() error { return nil }
func openMemory(context.Context, Archive) (ReadSeekCloser, error) {
	return &memoryArchive{bytes.NewReader([]byte("data"))}, nil
}

func TestRetentionProtectsNewestRestoredAndPinnedAndRecoversDeletion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	restored := f.archive(t, "restored", true)
	f.now = f.now.Add(24 * time.Hour)
	expired := f.archive(t, "expired", false)
	f.now = f.now.Add(24 * time.Hour)
	pinned := f.archive(t, "pinned", false)
	f.now = f.now.Add(24 * time.Hour)
	newest := f.archive(t, "newest", false)
	settings := DefaultSettings()
	_, e := f.s.UpdateSettings(ctx, SettingsRequest{EventID: "retention", ActorID: "owner-1", ExpectedRevision: 1, Schedule: settings.Schedule, Retention: Retention{KeepLast: 1, MaxAgeDays: 1}})
	if e != nil {
		t.Fatal(e)
	}
	download, _, e := f.s.OpenDownload(ctx, pinned.ID, openMemory)
	if e != nil {
		t.Fatal(e)
	}
	defer download.Close()
	f.now = f.now.Add(24 * time.Hour)
	candidates, e := f.s.RetentionCandidates(ctx, f.now)
	if e != nil || len(candidates) != 1 || candidates[0].ID != expired.ID {
		t.Fatalf("protected archives selected: %+v %v", candidates, e)
	}
	failure := errors.New("unlink interrupted")
	removed, e := f.s.PruneArchives(ctx, f.now, func(a Archive) error {
		if a.ID != expired.ID {
			t.Fatalf("unsafe delete %s", a.ID)
		}
		return failure
	})
	if !errors.Is(e, failure) || len(removed) != 0 {
		t.Fatalf("failed deletion wasn't retained: %v %v", removed, e)
	}
	if _, _, e = f.s.OpenDownload(ctx, expired.ID, openMemory); !errors.Is(e, ErrBusy) {
		t.Fatalf("pending removal became downloadable: %v", e)
	}
	if e = download.Close(); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	seen := map[string]bool{}
	removed, e = f.s.PruneArchives(ctx, f.now, func(a Archive) error {
		if !a.Downloadable {
			t.Fatal("retry callback lost original registered metadata")
		}
		seen[a.ID] = true
		return nil
	})
	if e != nil || len(removed) != 2 || !seen[expired.ID] || !seen[pinned.ID] {
		t.Fatalf("deletion not recovered: %v %v %v", removed, seen, e)
	}
	status, e := f.s.Status(ctx)
	if e != nil || len(status.Archives) != 2 {
		t.Fatalf("remaining archives: %+v %v", status.Archives, e)
	}
	for _, a := range status.Archives {
		if !a.Protected || a.ID != newest.ID && a.ID != restored.ID {
			t.Fatalf("protection lost: %+v", a)
		}
	}
	if len(status.Jobs) != 4 {
		t.Fatal("retention erased job receipts")
	}
}

func TestDownloadPinPrecedesSlowOpenAndWorkerGate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.archive(t, "archive", false)
	entered, release := make(chan struct{}), make(chan struct{})
	type answer struct {
		f ReadSeekCloser
		e error
	}
	result := make(chan answer, 1)
	go func() {
		v, _, e := f.s.OpenDownload(ctx, a.ID, func(context.Context, Archive) (ReadSeekCloser, error) {
			close(entered)
			<-release
			return &memoryArchive{bytes.NewReader([]byte("data"))}, nil
		})
		result <- answer{v, e}
	}()
	<-entered
	count, e := f.s.ActiveDownloads(ctx)
	if e != nil || count != 1 {
		t.Fatalf("slow opener wasn't pinned: %d %v", count, e)
	}
	j := f.create(t, "next-job")
	_, e = f.s.UpdateJob(ctx, j.ID, func(v *Job) error {
		v.State = "running"
		v.Phase = "preflight"
		v.StartedUnix = f.now.Unix()
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.OpenDownload(ctx, a.ID, openMemory); !errors.Is(e, ErrBusy) {
		t.Fatalf("new download bypassed running gate: %v", e)
	}
	close(release)
	r := <-result
	if r.e != nil {
		t.Fatal(r.e)
	}
	raw, e := io.ReadAll(r.f)
	if e != nil || string(raw) != "data" {
		t.Fatalf("in-flight download cut: %q %v", raw, e)
	}
	if e = r.f.Close(); e != nil {
		t.Fatal(e)
	}
	if e = r.f.Close(); e != nil {
		t.Fatal(e)
	}
	count, e = f.s.ActiveDownloads(ctx)
	if e != nil || count != 0 {
		t.Fatalf("pin leak: %d %v", count, e)
	}
}

func TestOpenFailureUnpinsAndArchiveMetadataIsImmutable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.archive(t, "archive", true)
	if _, _, e := f.s.OpenDownload(ctx, a.ID, func(context.Context, Archive) (ReadSeekCloser, error) { return nil, errors.New("missing") }); e == nil {
		t.Fatal("open failure lost")
	}
	if n, e := f.s.ActiveDownloads(ctx); e != nil || n != 0 {
		t.Fatalf("failed open leaked pin: %d %v", n, e)
	}
	changed := a
	changed.SizeBytes++
	if _, e := f.s.PutArchive(ctx, changed); !errors.Is(e, ErrConflict) {
		t.Fatalf("archive rewritten: %v", e)
	}
	changed = a
	changed.Verification = "checksum_verified"
	if _, e := f.s.PutArchive(ctx, changed); !errors.Is(e, ErrConflict) {
		t.Fatalf("restore verification downgraded: %v", e)
	}
}

func TestActiveDownloadLeasesAreBounded(t *testing.T) {
	f := newFixture(t)
	a := f.archive(t, "archive", false)
	ctx := context.Background()
	for i := 0; i < MaxActiveDownloads; i++ {
		file, _, err := f.s.OpenDownload(ctx, a.ID, openMemory)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
	}
	if _, _, err := f.s.OpenDownload(ctx, a.ID, openMemory); !errors.Is(err, ErrBusy) {
		t.Fatalf("unbounded download leases: %v", err)
	}
}

func TestUnfinishedOrFailedArchiveCannotReplaceLastGood(t *testing.T) {
	for _, outcome := range []string{"succeeded", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			good := f.archive(t, "last-successful", false)
			f.now = f.now.Add(40 * 24 * time.Hour)
			job := f.create(t, "new-publication")
			_, err := f.s.UpdateJob(ctx, job.ID, func(j *Job) error {
				j.State = "running"
				j.Phase = "retention"
				j.StartedUnix = f.now.Unix()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			pending := good
			pending.ID, pending.JobID, pending.CreatedUnix = job.ID, job.ID, f.now.Unix()
			if _, err = f.s.PutArchive(ctx, pending); err != nil {
				t.Fatal(err)
			}
			if err = f.s.Close(); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			check := func(published bool) {
				t.Helper()
				status, err := f.s.Status(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, archive := range status.Archives {
					if archive.ID == pending.ID && (archive.Downloadable != published || archive.Protected != published) {
						t.Fatalf("unfinished archive advertised as good: %+v", archive)
					}
					if archive.ID == good.ID && archive.Protected == published {
						t.Fatalf("last successful archive protection: %+v", archive)
					}
				}
			}
			check(false)
			if candidates, err := f.s.RetentionCandidates(ctx, f.now); err != nil || len(candidates) != 0 {
				t.Fatalf("retention replaced last good before success: %+v %v", candidates, err)
			}
			if _, _, err = f.s.OpenDownload(ctx, pending.ID, openMemory); !errors.Is(err, ErrBusy) {
				t.Fatalf("unfinished publication downloadable: %v", err)
			}
			_, err = f.s.UpdateJob(ctx, job.ID, func(j *Job) error {
				j.State = outcome
				j.Phase = "complete"
				j.FinishedUnix = f.now.Unix()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			check(outcome == "succeeded")
			file, _, err := f.s.OpenDownload(ctx, pending.ID, openMemory)
			if outcome == "failed" {
				if !errors.Is(err, ErrBusy) {
					if file != nil {
						file.Close()
					}
					t.Fatalf("failed publication downloadable: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				file.Close()
			}
		})
	}
}
