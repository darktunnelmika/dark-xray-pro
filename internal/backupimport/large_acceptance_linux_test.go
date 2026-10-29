//go:build linux

package backupimport

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// syntheticCipherStream emits a plausible public age envelope and opaque
// deterministic bytes. It is deliberately NOT authenticated age ciphertext or
// a native archive/SQLite fixture. Tests cover intake memory and I/O only.
type syntheticCipherStream struct {
	header            []byte
	pattern           [64 << 10]byte
	remaining, offset int64
	maxRead           int
}

func newSyntheticCipherStream(size int64) *syntheticCipherStream {
	text := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	r := &syntheticCipherStream{header: []byte("age-encryption.org/v1\n-> X25519 " + text + "\n" + text + "\n--- " + text + "\n"), remaining: size}
	var state uint64 = 0x85ebca6b9e3779b9
	for i := range r.pattern {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		r.pattern[i] = byte(state)
	}
	return r
}

func (r *syntheticCipherStream) Read(p []byte) (int, error) {
	r.maxRead = max(r.maxRead, len(p))
	if r.remaining == 0 {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), r.remaining)]
	n := len(p)
	if len(r.header) > 0 {
		copied := copy(p, r.header)
		r.header = r.header[copied:]
		p = p[copied:]
	}
	for len(p) > 0 {
		at := int(r.offset % int64(len(r.pattern)))
		copied := copy(p, r.pattern[at:])
		r.offset += int64(copied)
		p = p[copied:]
	}
	r.remaining -= int64(n)
	return n, nil
}

func syntheticDigest(t *testing.T, size int64) string {
	t.Helper()
	h := sha256.New()
	r := newSyntheticCipherStream(size)
	if n, err := io.CopyBuffer(h, r, make([]byte, 64<<10)); err != nil || n != size {
		t.Fatalf("stream digest: %d %v", n, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestLargeUploadInboxStreamingRoundTrip(t *testing.T) {
	if os.Getenv("DARK_PRO_RUN_LARGE_UPLOAD") != "1" {
		t.Skip("opt-in >3GiB ciphertext inbox acceptance")
	}
	const size = int64(3<<30) + 12345
	f := newFixture(t)
	// Use actual filesystem space checks for the expensive acceptance.
	f.s.available = func() (int64, error) { return availableAt(f.s.root) }
	want := syntheticDigest(t, size)
	q := CreateRequest{EventID: "large-upload-acceptance-0001", ActorID: "owner-1", SizeBytes: size, SHA256: want}
	created, err := f.s.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	stream := newSyntheticCipherStream(size)
	start := time.Now()
	result, err := f.s.Receive(context.Background(), created.Upload.ID, q.ActorID, size, stream)
	if err != nil || result.Upload.State != "received" || result.Upload.Verification != "ciphertext_checksum_verified" || stream.maxRead > 64<<10 {
		t.Fatalf("large receive: %+v %v maxread=%d", result, err, stream.maxRead)
	}
	file, u, err := f.s.OpenDownload(context.Background(), created.Upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, file, make([]byte, 64<<10))
	closeErr := file.Close()
	if err != nil || closeErr != nil || n != size || hex.EncodeToString(h.Sum(nil)) != want || u.SHA256 != want {
		t.Fatalf("large download: bytes=%d read=%v close=%v", n, err, closeErr)
	}
	info, err := os.Stat(filepath.Join(f.dir, u.ID, contentName))
	if err != nil || info.Size() != size {
		t.Fatalf("actual file size: %v %v", info, err)
	}
	t.Logf("ciphertext_bytes=%d sha256=%s max_reader_buffer=%d elapsed=%s; opaque intake only, no age authentication, native compatibility, SQLite or restore claim", size, want, stream.maxRead, time.Since(start))
}

type crashReader struct {
	stream    *syntheticCipherStream
	read      int64
	ready     string
	signalled bool
}

func (r *crashReader) Read(p []byte) (int, error) {
	if r.read >= 1<<20 {
		if !r.signalled {
			r.signalled = true
			if err := os.WriteFile(r.ready, []byte("partial-written"), 0600); err != nil {
				return 0, err
			}
		}
		<-time.After(time.Hour)
		return 0, io.ErrClosedPipe
	}
	n, err := r.stream.Read(p)
	r.read += int64(n)
	return n, err
}

func TestInboxInterruptedChildProcess(t *testing.T) {
	const size = int64(8<<20) + 123
	if dir := os.Getenv("DARK_PRO_UPLOAD_CRASH_CHILD"); dir != "" {
		s, err := openStore(dir, uint32(os.Geteuid()), time.Now)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		q := CreateRequest{EventID: "crash-upload-acceptance-0001", ActorID: "owner-1", SizeBytes: size, SHA256: syntheticDigest(t, size)}
		u, err := s.Create(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Receive(context.Background(), u.Upload.ID, q.ActorID, size, &crashReader{stream: newSyntheticCipherStream(size), ready: dir + ".ready"})
		t.Fatalf("crash child unexpectedly returned: %v", err)
		return
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "inbox")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestInboxInterruptedChildProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "DARK_PRO_UPLOAD_CRASH_CHILD="+dir)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir + ".ready"); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("child did not write its live partial")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("child completed instead of SIGKILL")
	}
	s, err := openStore(dir, uint32(os.Geteuid()), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	status, err := s.Status(context.Background())
	if err != nil || len(status.Uploads) != 1 {
		t.Fatalf("child receipt: %+v %v", status, err)
	}
	u := status.Uploads[0]
	if u.State != "interrupted" || u.ReceivedBytes < 1<<20 || u.Downloadable || u.Attempts != 1 {
		t.Fatalf("partial promoted or lost: %+v", u)
	}
	if _, _, err = s.OpenDownload(context.Background(), u.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("child partial downloadable: %v", err)
	}
	result, err := s.Receive(context.Background(), u.ID, u.ActorID, size, newSyntheticCipherStream(size))
	if err != nil || result.Upload.State != "received" || result.Upload.Attempts != 2 || result.Upload.SHA256 != u.SHA256 {
		t.Fatalf("full retry after SIGKILL: %+v %v", result, err)
	}
}
