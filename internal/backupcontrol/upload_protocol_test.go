//go:build linux

package backupcontrol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupimport"
)

type memoryUploadStore struct {
	mu      sync.Mutex
	entry   backupimport.Upload
	data    []byte
	reader  io.Closer
	started chan struct{}
	delay   time.Duration
	largest atomic.Int64
}

func memoryUploads(data []byte) *memoryUploadStore {
	hash := sha256.Sum256(data)
	return &memoryUploadStore{data: data, entry: backupimport.Upload{ID: strings.Repeat("c", 32), EventID: "upload-protocol-event-01", ActorID: "owner-1", SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(hash[:]), State: "awaiting_upload", Verification: "unverified", CreatedUnix: time.Now().Unix(), UpdatedUnix: time.Now().Unix()}}
}
func (s *memoryUploadStore) Status(context.Context) (backupimport.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return backupimport.Status{Enabled: true, Uploads: []backupimport.Upload{s.entry}}, nil
}
func (s *memoryUploadStore) Create(context.Context, backupimport.CreateRequest) (backupimport.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return backupimport.Response{Upload: s.entry}, nil
}
func (s *memoryUploadStore) Get(context.Context, string) (backupimport.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entry, nil
}
func (s *memoryUploadStore) Receive(ctx context.Context, _ string, _ string, size int64, reader io.Reader) (backupimport.Response, error) {
	s.mu.Lock()
	if s.entry.State == "received" {
		u := s.entry
		s.mu.Unlock()
		return backupimport.Response{Upload: u, Replayed: true}, nil
	}
	s.entry.State = "receiving"
	s.reader, _ = reader.(io.Closer)
	started := s.started
	s.mu.Unlock()
	if started != nil {
		close(started)
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var total int64
	for {
		n, err := reader.Read(buf)
		s.largest.Store(max(s.largest.Load(), int64(len(buf))))
		total += int64(n)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return backupimport.Response{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if total != size || hex.EncodeToString(h.Sum(nil)) != s.entry.SHA256 {
		return backupimport.Response{}, backupimport.ErrInvalid
	}
	s.entry.State = "received"
	s.entry.Verification = "ciphertext_checksum_verified"
	s.entry.ReceivedBytes = size
	s.entry.Downloadable = true
	s.entry.Attempts++
	return backupimport.Response{Upload: s.entry}, nil
}
func (s *memoryUploadStore) Cancel(context.Context, string, string) (backupimport.Response, error) {
	s.mu.Lock()
	s.entry.State = "cancelled"
	body := s.reader
	u := s.entry
	s.mu.Unlock()
	if body != nil {
		_ = body.Close()
	}
	return backupimport.Response{Upload: u}, nil
}
func (s *memoryUploadStore) Remove(context.Context, string, string) (backupimport.Response, error) {
	return backupimport.Response{}, backupimport.ErrBusy
}
func (s *memoryUploadStore) OpenDownload(context.Context, string) (backupimport.ReadSeekCloser, backupimport.Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &uploadMemoryFile{Reader: bytes.NewReader(s.data)}, s.entry, nil
}

type uploadMemoryFile struct{ *bytes.Reader }

func (*uploadMemoryFile) Close() error { return nil }

func awaitTransfers(t *testing.T, store *Store, want int) {
	t.Helper()
	until := time.Now().Add(time.Second)
	for {
		n, err := store.ActiveDownloads(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if n == want {
			return
		}
		if time.Now().After(until) {
			t.Fatalf("transfer pins=%d want%d", n, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUploadSocketStreamsAndPinsUntilHubClosesReceipt(t *testing.T) {
	f := newFixture(t)
	data := bytes.Repeat([]byte("ciphertext-without-plaintext-claims"), 32768)
	inbox := memoryUploads(data)
	inbox.delay = 150 * time.Millisecond
	client, _, _ := startUnixForTest(t, NewHandler(f.s, HandlerOptions{Uploads: inbox}))
	status, err := client.Status(context.Background())
	if err != nil || !status.Capabilities.Upload || status.Capabilities.Restore {
		t.Fatal("capability split", status, err)
	}
	result, err := client.Upload(context.Background(), inbox.entry.ID, "owner-1", int64(len(data)), inbox.entry.SHA256, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if result.Response.Upload.State != "received" || result.Response.Upload.Verification != "ciphertext_checksum_verified" {
		t.Fatal(result.Response)
	}
	awaitTransfers(t, f.s, 1)
	// Reading the full upstream JSON must not release the availability pin.
	if inbox.largest.Load() > 64<<10 {
		t.Fatal("unbounded read", inbox.largest.Load())
	}
	_ = result.Lease.Close()
	awaitTransfers(t, f.s, 0)
	download, err := client.DownloadUpload(context.Background(), inbox.entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(download.Body)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("download differs", err)
	}
	awaitTransfers(t, f.s, 1)
	_ = download.Body.Close()
	awaitTransfers(t, f.s, 0)
}

type uploadReadCounter struct{ reads atomic.Int64 }

func (r *uploadReadCounter) Read([]byte) (int, error) {
	r.reads.Add(1)
	return 0, errors.New("replayed upload body must not be sent")
}

func TestUploadSocketCompletedReplayReturnsBeforeBody(t *testing.T) {
	f := newFixture(t)
	inbox := memoryUploads([]byte("previously-completed-ciphertext"))
	inbox.entry.State = "received"
	inbox.entry.Verification = "ciphertext_checksum_verified"
	inbox.entry.Downloadable = true
	inbox.entry.ReceivedBytes = inbox.entry.SizeBytes
	client, _, _ := startUnixForTest(t, NewHandler(f.s, HandlerOptions{Uploads: inbox}))
	body := &uploadReadCounter{}
	result, err := client.Upload(context.Background(), inbox.entry.ID, "owner-1", inbox.entry.SizeBytes, inbox.entry.SHA256, body)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Response.Replayed || body.reads.Load() != 0 {
		t.Fatal("completed replay consumed body", result.Response, body.reads.Load())
	}
	awaitTransfers(t, f.s, 1)
	_ = result.Lease.Close()
	awaitTransfers(t, f.s, 0)
}

func TestUploadSocketCancellationInterruptsStalledBodyWithoutHoldingStore(t *testing.T) {
	f := newFixture(t)
	inbox := memoryUploads(bytes.Repeat([]byte("x"), 1024))
	inbox.started = make(chan struct{})
	client, _, _ := startUnixForTest(t, NewHandler(f.s, HandlerOptions{Uploads: inbox}))
	// An actual Unix socket peer sends headers and no body. A metadata cancel
	// must interrupt the blocked server Read, rather than wait for its60s idle.
	conn, err := client.dialDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request := "PUT /v1/uploads/" + inbox.entry.ID + "/content HTTP/1.1\r\nHost: backup-control\r\nContent-Type: application/octet-stream\r\nContent-Length: 1024\r\n" + uploadActorHeader + ": owner-1\r\n" + UploadSHA256Header + ": " + inbox.entry.SHA256 + "\r\n\r\n"
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inbox.started:
	case <-time.After(time.Second):
		t.Fatal("upload didnotstart")
	}
	awaitTransfers(t, f.s, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.CancelUpload(ctx, inbox.entry.ID, "owner-1")
	if err != nil {
		t.Fatal("cancel stalled behind bodyread", err)
	}
	awaitTransfers(t, f.s, 0)
}

func TestUploadLeaseAndWorkerClaimCannotCross(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	release, err := f.s.AcquireTransfer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.s.CreateJob(ctx, CreateRequest{EventID: "job-with-upload-pin", ActorID: "owner-1", Action: "create"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.UpdateJob(ctx, job.Job.ID, func(j *Job) error {
		j.State = "running"
		j.Phase = "preflight"
		j.StartedUnix = time.Now().Unix()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if other, err := f.s.AcquireTransfer(ctx); !errors.Is(err, ErrBusy) {
		if other != nil {
			other()
		}
		t.Fatal("new transfer entered afterstopclaim", err)
	}
	awaitTransfers(t, f.s, 1)
	release()
	release()
	awaitTransfers(t, f.s, 0)
}

func TestUploadClientRefusesUnboundedMetadataAndConflictingIdentity(t *testing.T) {
	inbox := memoryUploads([]byte("fixture"))
	for _, headers := range []map[string]string{
		{"Content-Length": "5000"},
		{"Content-Length": "2"},
	} {
		client, _, _ := startUnixForTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			_, _ = io.WriteString(w, "{}")
		}))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		result, err := client.Upload(ctx, inbox.entry.ID, "owner-1", inbox.entry.SizeBytes, inbox.entry.SHA256, bytes.NewReader(inbox.data))
		cancel()
		if err == nil {
			result.Lease.Close()
			t.Fatal("untrusted receipt accepted")
		}
	}
}

type uploadDeadlineRecorder struct {
	*httptest.ResponseRecorder
	reads []time.Time
}

func (w *uploadDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	w.reads = append(w.reads, deadline)
	return nil
}

func TestUploadEOFDoesNotRestartIdleTimeoutWhileWorkerVerifies(t *testing.T) {
	w := &uploadDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	body := &uploadDeadlineBody{body: io.NopCloser(strings.NewReader("done")), controller: http.NewResponseController(w), ctx: context.Background(), deadline: time.Now().Add(time.Hour)}
	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatal(err)
	}
	before := len(w.reads)
	if before == 0 || !w.reads[before-1].IsZero() {
		t.Fatal("EOF left idle timeout on connection")
	}
	// Transport probes EOF again when enforcing outgoing Content-Length.
	if _, err := body.Read(make([]byte, 1)); err != io.EOF {
		t.Fatal(err)
	}
	if len(w.reads) != before {
		t.Fatal("surplus probe rearmed idle timeout")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("closed body read", err)
	}
}
