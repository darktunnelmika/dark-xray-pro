//go:build linux

package backupimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/sys/unix"
)

type fixture struct {
	s     *Store
	dir   string
	event int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	f := &fixture{dir: filepath.Join(parent, "inbox")}
	f.reopen(t)
	t.Cleanup(func() { _ = f.s.Close() })
	return f
}

func (f *fixture) reopen(t *testing.T) {
	t.Helper()
	var err error
	f.s, err = openStore(f.dir, uint32(os.Geteuid()), time.Now)
	if err != nil {
		p, pe := os.Stat(filepath.Dir(f.dir))
		d, de := os.Stat(f.dir)
		t.Fatalf("open: %v parent=%v/%v directory=%v/%v", err, p, pe, d, de)
	}
	f.s.available = func() (int64, error) { return 1 << 40, nil }
}

func ciphertext(t *testing.T, size int) []byte {
	t.Helper()
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w, err := age.Encrypt(&b, key.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately not a native container: successful intake must not imply
	// authenticated native structure, compatible SQLite, or restore readiness.
	if _, err = io.Copy(w, io.LimitReader(zeroReader{}, int64(size))); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (f *fixture) allocate(t *testing.T, data []byte) Upload {
	t.Helper()
	f.event++
	q := CreateRequest{EventID: fmt.Sprintf("upload-event-%016d", f.event), ActorID: "owner-1", SizeBytes: int64(len(data)), SHA256: digest(data)}
	r, err := f.s.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return r.Upload
}

func (f *fixture) receive(t *testing.T, data []byte) Upload {
	t.Helper()
	u := f.allocate(t, data)
	r, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("receive: %+v %v", r, err)
	}
	return r.Upload
}

type observingReader struct {
	r      *bytes.Reader
	before func()
	once   sync.Once
	max    int
	closed atomic.Bool
}

func (r *observingReader) Read(p []byte) (int, error) {
	r.once.Do(r.before)
	r.max = max(r.max, len(p))
	return r.r.Read(p)
}
func (r *observingReader) Close() error { r.closed.Store(true); return nil }

func TestUploadReceiptBoundedStreamingIndependentDownloadAndRestart(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 2<<20)
	u := f.allocate(t, data)
	r := &observingReader{r: bytes.NewReader(data), before: func() {
		// Reentrant status proves no journal mutex is held during the read.
		status, err := f.s.Status(context.Background())
		if err != nil || len(status.Uploads) != 1 || status.Uploads[0].State != "receiving" {
			t.Errorf("intent not durable before body: %+v %v", status, err)
		}
		raw, err := os.ReadFile(filepath.Join(f.dir, journalName))
		if err != nil || !bytes.Contains(raw, []byte(`"state":"receiving"`)) {
			t.Error("missing persisted receive intent")
		}
	}}
	result, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, r)
	if err != nil || result.Replayed || result.Upload.State != "received" || !result.Upload.Downloadable ||
		result.Upload.Verification != "ciphertext_checksum_verified" || result.Upload.ReceivedBytes != u.SizeBytes || result.Upload.Attempts != 1 {
		t.Fatalf("result: %+v %v", result, err)
	}
	if r.max > 64<<10 || r.closed.Load() {
		t.Fatalf("unbounded read or success closed caller body: max=%d closed=%t", r.max, r.closed.Load())
	}
	// Let scheduling run: the joined cancellation watcher must remain stopped.
	for range 100 {
		time.Sleep(time.Microsecond)
	}
	if r.closed.Load() {
		t.Fatal("successful EOF closed body after return")
	}
	file, got, err := f.s.OpenDownload(context.Background(), u.ID)
	if err != nil || got != result.Upload {
		t.Fatalf("download: %+v %v", got, err)
	}
	if _, err = f.s.Remove(context.Background(), u.ID, u.ActorID); !errors.Is(err, ErrBusy) {
		t.Fatalf("deleted pinned file: %v", err)
	}
	copy, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(copy, data) {
		t.Fatal("download changed bytes")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	replay, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, panicReader{})
	if err != nil || !replay.Replayed || replay.Upload != result.Upload {
		t.Fatalf("lost response receipt after restart: %+v %v", replay, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, u.ID, partialName)); !os.IsNotExist(err) {
		t.Fatal("published partial retained")
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("receipt replay read replacement body") }

func TestAllocationCommitmentReplayAndTombstones(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 64)
	u := f.receive(t, data)
	q := CreateRequest{EventID: u.EventID, ActorID: u.ActorID, SizeBytes: u.SizeBytes, SHA256: u.SHA256}
	for _, modify := range []func(*CreateRequest){func(v *CreateRequest) { v.ActorID = "owner-2" }, func(v *CreateRequest) { v.SizeBytes++ }, func(v *CreateRequest) { v.SHA256 = strings.Repeat("a", 64) }} {
		other := q
		modify(&other)
		if _, err := f.s.Create(context.Background(), other); !errors.Is(err, ErrConflict) {
			t.Fatalf("conflicting event accepted: %v", err)
		}
	}
	for _, op := range []func() error{
		func() error {
			_, e := f.s.Receive(context.Background(), u.ID, "owner-2", u.SizeBytes, bytes.NewReader(data))
			return e
		},
		func() error { _, e := f.s.Cancel(context.Background(), u.ID, "owner-2"); return e },
		func() error { _, e := f.s.Remove(context.Background(), u.ID, "owner-2"); return e },
	} {
		if err := op(); !errors.Is(err, ErrConflict) {
			t.Fatalf("cross-actor mutation accepted: %v", err)
		}
	}
	r, err := f.s.Remove(context.Background(), u.ID, u.ActorID)
	if err != nil || r.Upload.State != "deleted" || r.Upload.Downloadable {
		t.Fatalf("delete: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, u.ID)); !os.IsNotExist(err) {
		t.Fatal("known upload directory retained")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	again, err := f.s.Create(context.Background(), q)
	if err != nil || !again.Replayed || again.Upload.ID != u.ID || again.Upload.State != "deleted" {
		t.Fatalf("tombstone event reused: %+v %v", again, err)
	}
	if _, err = f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data)); !errors.Is(err, ErrConflict) {
		t.Fatalf("deleted allocation resurrected: %v", err)
	}
	status, err := f.s.Status(context.Background())
	if err != nil || status.ReservedBytes != 0 || len(status.Uploads) != 1 || status.MaxEntries != 4096 || status.MaxActiveEntries != 32 {
		t.Fatalf("capacity/status: %+v %v", status, err)
	}
}

func TestTransferRejectionAndExplicitFullRetry(t *testing.T) {
	good := ciphertext(t, 4096)
	for _, name := range []string{"hash", "short", "long", "envelope", "disconnect", "header_line", "missing_body"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			u := f.allocate(t, good)
			bad := bytes.Clone(good)
			var body io.Reader
			switch name {
			case "hash":
				bad[len(bad)-1] ^= 1
				body = bytes.NewReader(bad)
			case "short":
				body = bytes.NewReader(bad[:len(bad)-1])
			case "long":
				body = bytes.NewReader(append(bad, 0))
			case "envelope":
				bad[0] = 'X'
				body = bytes.NewReader(bad)
			case "disconnect":
				body = &disconnectReader{prefix: bytes.NewReader(bad[:300])}
			case "header_line":
				body = strings.NewReader(strings.Repeat("x", 1025))
			case "missing_body":
				end := bytes.Index(bad, []byte("--- "))
				end += bytes.IndexByte(bad[end:], '\n') + 1
				body = bytes.NewReader(bad[:end])
			}
			r, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, body)
			state := "rejected"
			if name == "disconnect" {
				state = "interrupted"
			}
			if err == nil || r.Upload.State != state || r.Upload.Downloadable || r.Upload.Verification != "unverified" {
				t.Fatalf("unsafe acceptance: %+v %v", r, err)
			}
			if _, _, err = f.s.OpenDownload(context.Background(), u.ID); !errors.Is(err, ErrConflict) {
				t.Fatalf("partial downloadable: %v", err)
			}
			r, err = f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(good))
			if err != nil || r.Upload.State != "received" || r.Upload.Attempts != 2 {
				t.Fatalf("from-zero retry: %+v %v", r, err)
			}
		})
	}
}

type disconnectReader struct{ prefix *bytes.Reader }

func (r *disconnectReader) Read(p []byte) (int, error) {
	if r.prefix.Len() > 0 {
		return r.prefix.Read(p)
	}
	return 0, io.ErrUnexpectedEOF
}

type blockingReader struct {
	prefix    []byte
	entered   chan struct{}
	released  chan struct{}
	once      sync.Once
	closeOnce sync.Once
	onClose   func()
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if len(r.prefix) > 0 {
		n := copy(p, r.prefix)
		r.prefix = r.prefix[n:]
		return n, nil
	}
	r.once.Do(func() { close(r.entered) })
	<-r.released
	return 0, io.ErrClosedPipe
}
func (r *blockingReader) Close() error {
	r.closeOnce.Do(func() {
		if r.onClose != nil {
			r.onClose()
		}
		close(r.released)
	})
	return nil
}

func TestCancelPersistsBeforeInterruptingReaderAndSingleReceiver(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 4096)
	u := f.allocate(t, data)
	other := f.allocate(t, data)
	r := &blockingReader{prefix: bytes.Clone(data[:300]), entered: make(chan struct{}), released: make(chan struct{})}
	r.onClose = func() {
		got, err := f.s.Get(context.Background(), u.ID)
		if err != nil || got.State != "cancelled" {
			t.Errorf("cancel not durable before close: %+v %v", got, err)
		}
	}
	done := make(chan error, 1)
	go func() { _, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, r); done <- err }()
	select {
	case <-r.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reader did not block")
	}
	if _, err := f.s.Receive(context.Background(), other.ID, other.ActorID, other.SizeBytes, bytes.NewReader(data)); !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel transfer: %v", err)
	}
	if _, err := f.s.Remove(context.Background(), u.ID, u.ActorID); !errors.Is(err, ErrBusy) {
		t.Fatalf("removed active transfer: %v", err)
	}
	result, err := f.s.Cancel(context.Background(), u.ID, u.ActorID)
	if err != nil || result.Upload.State != "cancelled" {
		t.Fatalf("cancel: %+v %v", result, err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not interrupt reader")
	}
	if _, err = f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data)); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancelled retry: %v", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	got, err := f.s.Get(context.Background(), u.ID)
	if err != nil || got.State != "cancelled" {
		t.Fatalf("cancel lost: %+v %v", got, err)
	}
}

func rewriteJournal(t *testing.T, f *fixture, change func(*journal)) {
	t.Helper()
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.dir, journalName))
	if err != nil {
		t.Fatal(err)
	}
	var j journal
	if json.Unmarshal(raw, &j) != nil {
		t.Fatal("bad fixture journal")
	}
	change(&j)
	raw, err = json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, journalName), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRestartClassifiesInterruptedAndUnknownPublicationWithoutOverwrite(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			f := newFixture(t)
			data := ciphertext(t, 4096)
			u := f.allocate(t, data)
			rewriteJournal(t, f, func(j *journal) {
				v := j.Uploads[u.ID]
				v.State = "checking"
				v.Attempts = 1
				v.ReceivedBytes = v.SizeBytes
				j.Uploads[u.ID] = v
			})
			if err := os.Mkdir(filepath.Join(f.dir, u.ID), 0700); err != nil {
				t.Fatal(err)
			}
			name := partialName
			body := data[:300]
			if published {
				name = contentName
				body = data
			}
			if err := os.WriteFile(filepath.Join(f.dir, u.ID, name), body, 0600); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			got, err := f.s.Get(context.Background(), u.ID)
			want := "interrupted"
			if published {
				want = "publication_unknown"
			}
			if err != nil || got.State != want || got.Downloadable || got.Verification != "unverified" {
				t.Fatalf("restart: %+v %v", got, err)
			}
			if published {
				if _, err = f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data)); !errors.Is(err, ErrConflict) {
					t.Fatalf("unknown publication overwritten: %v", err)
				}
				if _, err = f.s.Remove(context.Background(), u.ID, u.ActorID); !errors.Is(err, ErrUnsafe) {
					t.Fatalf("unknown publication deleted: %v", err)
				}
				copy, err := os.ReadFile(filepath.Join(f.dir, u.ID, name))
				if err != nil || !bytes.Equal(copy, data) {
					t.Fatal("unknown publication changed")
				}
			} else {
				if _, err = f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPublicationAndJournalFailuresRemainUncertain(t *testing.T) {
	for _, point := range []string{"rename", "publication_sync", "final_journal_write"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			data := ciphertext(t, 4096)
			u := f.allocate(t, data)
			fired := false
			switch point {
			case "rename":
				f.s.rename = func(int, string, int, string, uint) error { fired = true; return unix.EIO }
			case "publication_sync":
				f.s.syncDir = func(d *os.File) error {
					if !fired && d.Name() == u.ID {
						if _, err := os.Stat(filepath.Join(f.dir, u.ID, contentName)); err == nil {
							fired = true
							return unix.EIO
						}
					}
					return d.Sync()
				}
			case "final_journal_write":
				f.s.syncFile = func(file *os.File) error {
					if !fired && file.Name() == nextJournalName {
						raw, _ := os.ReadFile(filepath.Join(f.dir, nextJournalName))
						if bytes.Contains(raw, []byte(`"state":"received"`)) {
							fired = true
							return unix.EIO
						}
					}
					return file.Sync()
				}
			}
			r, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data))
			if err == nil || !fired || r.Upload.Downloadable {
				t.Fatalf("failure hidden: %+v %v fired=%t", r, err, fired)
			}
			if point != "rename" && r.Upload.State != "publication_unknown" {
				t.Fatalf("published failure lost: %+v", r)
			}
			if err = f.s.Close(); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			got, err := f.s.Get(context.Background(), u.ID)
			if err != nil || got.Downloadable {
				t.Fatalf("recovered unsafe success: %+v %v", got, err)
			}
			if point != "rename" {
				copy, err := os.ReadFile(filepath.Join(f.dir, u.ID, contentName))
				if err != nil || !bytes.Equal(copy, data) {
					t.Fatal("failed publication erased")
				}
			}
		})
	}
}

func TestCapacityReservationsDiskHeadroomAndLifetimeEntries(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 16)
	size := int64(len(data))
	f.s.limits.maxUpload = size
	f.s.limits.maxInbox = 2 * size
	f.s.limits.maxActive = 2
	f.s.limits.maxEntries = 3
	f.s.limits.minFree = 100
	f.s.available = func() (int64, error) { return 100 + size - 1, nil }
	if _, err := f.s.Create(context.Background(), CreateRequest{EventID: "allocation-event-0001", ActorID: "owner-1", SizeBytes: size, SHA256: digest(data)}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("disk reservation omitted: %v", err)
	}
	f.s.available = func() (int64, error) { return 1 << 40, nil }
	u := f.allocate(t, data)
	f.allocate(t, data)
	if _, err := f.s.Create(context.Background(), CreateRequest{EventID: "allocation-event-0002", ActorID: "owner-1", SizeBytes: size, SHA256: digest(data)}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("inbox capacity omitted: %v", err)
	}
	if _, err := f.s.Cancel(context.Background(), u.ID, u.ActorID); err != nil {
		t.Fatal(err)
	}
	status, _ := f.s.Status(context.Background())
	if status.ReservedBytes != 2*size {
		t.Fatal("cancel silently freed reserved bytes")
	}
	if _, err := f.s.Remove(context.Background(), u.ID, u.ActorID); err != nil {
		t.Fatal(err)
	}
	third := f.allocate(t, data)
	if _, err := f.s.Remove(context.Background(), third.ID, third.ActorID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Create(context.Background(), CreateRequest{EventID: "allocation-event-0003", ActorID: "owner-1", SizeBytes: size, SHA256: digest(data)}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("lifetime receipt bound omitted: %v", err)
	}
}

func TestUnsafeFilesystemNeverFollowedOrRecursivelyRemoved(t *testing.T) {
	for _, name := range []string{"unknown", "symlink", "hardlink", "mode", "tamper"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			data := ciphertext(t, 64)
			u := f.receive(t, data)
			path := filepath.Join(f.dir, u.ID, contentName)
			switch name {
			case "unknown":
				if err := os.WriteFile(filepath.Join(f.dir, u.ID, "operator-evidence"), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, filepath.Join(t.TempDir(), "outside")); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "tamper":
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				bad := bytes.Clone(data)
				bad[len(bad)-1] ^= 1
				if err := os.WriteFile(path, bad, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := f.s.OpenDownload(context.Background(), u.ID); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("unsafe download: %v", err)
			}
			if name != "tamper" {
				if _, err := f.s.Remove(context.Background(), u.ID, u.ActorID); !errors.Is(err, ErrUnsafe) {
					t.Fatalf("unsafe delete: %v", err)
				}
				if _, err := os.Lstat(path); err != nil {
					t.Fatal("unsafe file removed")
				}
			}
		})
	}
}

func TestJournalRejectsCorruptionExclusiveLockAndUnexpectedRootEntry(t *testing.T) {
	f := newFixture(t)
	if other, err := openStore(f.dir, uint32(os.Geteuid()), time.Now); !errors.Is(err, ErrBusy) {
		if other != nil {
			_ = other.Close()
		}
		t.Fatalf("second writer: %v", err)
	}
	u := f.allocate(t, ciphertext(t, 64))
	_ = u
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(f.dir, journalName))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, journalName), append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := openStore(f.dir, uint32(os.Geteuid()), time.Now); !errors.Is(err, ErrUnsafe) {
		if other != nil {
			_ = other.Close()
		}
		t.Fatalf("ambiguous journal: %v", err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, journalName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.dir, "unexpected"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := openStore(f.dir, uint32(os.Geteuid()), time.Now); !errors.Is(err, ErrUnsafe) {
		if other != nil {
			_ = other.Close()
		}
		t.Fatalf("unknown entry hidden: %v", err)
	}
	if _, err = os.Stat(filepath.Join(f.dir, "unexpected")); err != nil {
		t.Fatal("unknown entry removed")
	}
}

func TestDeleteIntentReconcilesToTombstoneAndKeepsReceipt(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 64)
	u := f.receive(t, data)
	rewriteJournal(t, f, func(j *journal) {
		v := j.Uploads[u.ID]
		v.State = "deleting"
		v.Downloadable = false
		j.Uploads[u.ID] = v
	})
	f.reopen(t)
	got, err := f.s.Get(context.Background(), u.ID)
	if err != nil || got.State != "deleted" || got.SHA256 != u.SHA256 {
		t.Fatalf("delete intent lost: %+v %v", got, err)
	}
	if _, err = os.Stat(filepath.Join(f.dir, u.ID)); !os.IsNotExist(err) {
		t.Fatal("delete intent not fulfilled")
	}
}

func TestFinalJournalDirectorySyncAmbiguityRecoversExactReceipt(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 4096)
	u := f.allocate(t, data)
	fired := false
	f.s.syncDir = func(d *os.File) error {
		if !fired && d == f.s.root {
			raw, _ := os.ReadFile(filepath.Join(f.dir, journalName))
			if bytes.Contains(raw, []byte(`"state":"received"`)) {
				fired = true
				return unix.EIO
			}
		}
		return d.Sync()
	}
	if _, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data)); err == nil || !fired {
		t.Fatalf("uncertain commit reported success: %v", err)
	}
	if _, err := f.s.Status(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("uncertain live journal remained writable: %v", err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	// The received record was written only after final-file fsync, directory
	// fsync and independent content hash. OpenStore confirms the observed
	// journal name with another directory fsync before serving its receipt.
	replayed, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, panicReader{})
	if err != nil || !replayed.Replayed || replayed.Upload.SHA256 != u.SHA256 {
		t.Fatalf("receipt lost after directory sync recovery: %+v %v", replayed, err)
	}
	file, _, err := f.s.OpenDownload(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("ambiguous publication bytes changed")
	}
}

func TestNoReplacePublicationNeverClobbersUnexpectedFinal(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 64)
	u := f.allocate(t, data)
	prior := bytes.Repeat([]byte{77}, len(data))
	f.s.rename = func(old int, oldName string, next int, newName string, flags uint) error {
		fd, err := unix.Openat(next, newName, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), newName)
		_, err = file.Write(prior)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		return unix.Renameat2(old, oldName, next, newName, flags)
	}
	result, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data))
	if err == nil || result.Upload.State != "publication_unknown" || result.Upload.Downloadable {
		t.Fatalf("unexpected target accepted: %+v %v", result, err)
	}
	got, err := os.ReadFile(filepath.Join(f.dir, u.ID, contentName))
	if err != nil || !bytes.Equal(got, prior) {
		t.Fatal("unexpected final target replaced")
	}
}

func TestDiskPressureInterruptsWithoutPublishingAndAllowsExplicitRetry(t *testing.T) {
	f := newFixture(t)
	data := ciphertext(t, 2<<20)
	u := f.allocate(t, data)
	checks := 0
	f.s.available = func() (int64, error) {
		checks++
		if checks >= 3 {
			return MinFreeBytes, nil
		}
		return 1 << 40, nil
	}
	result, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data))
	if !errors.Is(err, ErrCapacity) || result.Upload.State != "interrupted" || result.Upload.ErrorCode != "disk_capacity" || result.Upload.Downloadable {
		t.Fatalf("disk exhaustion: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, u.ID, contentName)); !os.IsNotExist(err) {
		t.Fatal("disk-limited partial published")
	}
	f.s.available = func() (int64, error) { return 1 << 40, nil }
	if _, err := f.s.Receive(context.Background(), u.ID, u.ActorID, u.SizeBytes, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}
