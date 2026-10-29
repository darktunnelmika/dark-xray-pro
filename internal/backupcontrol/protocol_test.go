//go:build linux

package backupcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProtocolRejectsAmbiguousMutationJSONAndQueries(t *testing.T) {
	f := newFixture(t)
	handler := NewHandler(f.s, HandlerOptions{OpenArchive: openMemory})
	for _, body := range []string{
		`{"event_id":"one","event_id":"two","actor_id":"owner-1","action":"create"}`,
		`{"event_id":"one","actor_id":"owner-1","action":"create","path":"/etc/shadow"}`,
		`{"event_id":"one","actor_id":null,"action":"create"}`,
		`{"event_id":"one","actor_id":"owner-1","action":"create"} {}`,
		`{"event_id":"` + strings.Repeat("x", 5000) + `","actor_id":"owner-1","action":"create"}`,
	} {
		r := httptest.NewRequest("POST", "/v1/jobs", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 422 {
			t.Fatalf("accepted malformed mutation: %d %s", w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/v1/status?path=elsewhere", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 422 {
		t.Fatalf("query accepted: %d", w.Code)
	}
	status, e := f.s.Status(context.Background())
	if e != nil || len(status.Jobs) != 0 {
		t.Fatalf("invalid mutation changed store: %+v %v", status, e)
	}
}

func startUnixForTest(t *testing.T, handler http.Handler) (*Client, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		done <- serveUnixAt(ctx, path, uint32(os.Geteuid()), uint32(os.Geteuid()), handler)
		close(stopped)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Lstat(path); e == nil {
			break
		}
		select {
		case e := <-done:
			cancel()
			t.Fatalf("Unix listener: %v", e)
		default:
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("Unix listener not ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Errorf("Unix shutdown: %v", e)
			}
		case <-time.After(6 * time.Second):
			t.Error("Unix shutdown blocked")
		}
	})
	return newClient(path, 50*time.Millisecond), cancel, stopped
}

func TestUnixClientRoundTripAndStreamingHasNoMetadataDeadline(t *testing.T) {
	f := newFixture(t)
	a := f.archive(t, "archive", false)
	underlying := NewHandler(f.s, HandlerOptions{OpenArchive: openMemory})
	var statusCalls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			statusCalls.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/download") {
			time.Sleep(100 * time.Millisecond)
		}
		underlying.ServeHTTP(w, r)
	})
	client, _, _ := startUnixForTest(t, handler)
	ctx := context.Background()
	status, e := client.Status(ctx)
	if e != nil || len(status.Archives) != 1 || status.Capabilities.Restore || status.Capabilities.Upload {
		t.Fatalf("status: %+v %v", status, e)
	}
	download, e := client.Download(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := io.ReadAll(download.Body)
	download.Body.Close()
	if e != nil || string(raw) != "data" || download.ContentLength != 4 || statusCalls.Load() != 1 {
		t.Fatalf("stream %q, len=%d, metadata calls=%d: %v", raw, download.ContentLength, statusCalls.Load(), e)
	}
	created, e := client.CreateJob(ctx, CreateRequest{EventID: "job-via-socket", ActorID: "owner-1", Action: "create"})
	if e != nil || !ValidID(created.Job.ID) {
		t.Fatalf("create: %+v %v", created, e)
	}
	again, e := client.CreateJob(ctx, CreateRequest{EventID: "job-via-socket", ActorID: "owner-1", Action: "create"})
	if e != nil || !again.Replayed || again.Job.ID != created.Job.ID {
		t.Fatalf("replay: %+v %v", again, e)
	}
	got, e := client.GetJob(ctx, created.Job.ID)
	if e != nil || got.ID != created.Job.ID {
		t.Fatalf("get: %+v %v", got, e)
	}
	cancelled, e := client.CancelJob(ctx, created.Job.ID, CancelRequest{EventID: "cancel-socket", ActorID: "owner-1"})
	if e != nil || cancelled.Job.State != "cancelled" {
		t.Fatalf("cancel: %+v %v", cancelled, e)
	}
	settings := DefaultSettings()
	saved, e := client.UpdateSettings(ctx, SettingsRequest{EventID: "settings-socket", ActorID: "owner-1", ExpectedRevision: 1, Schedule: settings.Schedule, Retention: settings.Retention})
	if e != nil || saved.Settings.Revision != 2 {
		t.Fatalf("settings: %+v %v", saved, e)
	}
}

func TestClientRejectsDownloadMetadataLengthMismatch(t *testing.T) {
	f := newFixture(t)
	a := f.archive(t, "archive", false)
	a.SizeBytes = 5
	raw, _ := json.Marshal(a)
	client, _, _ := startUnixForTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(archiveHeader, base64.RawURLEncoding.EncodeToString(raw))
		w.Header().Set("Content-Length", "4")
		_, _ = w.Write([]byte("data"))
	}))
	if d, e := client.Download(context.Background(), a.ID); !errors.Is(e, ErrUnsafe) {
		if d != nil {
			d.Body.Close()
		}
		t.Fatalf("length mismatch accepted: %v", e)
	}
}

func TestUnixListenerRejectsUnsafeSocketAndLiveListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	if e := os.WriteFile(path, []byte("do not replace"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := serveUnixAt(context.Background(), path, uint32(os.Geteuid()), uint32(os.Geteuid()), http.NotFoundHandler()); !errors.Is(e, ErrUnsafe) {
		t.Fatalf("non-socket overwritten: %v", e)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	listener, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	if e = serveUnixAt(context.Background(), path, uint32(os.Geteuid()), uint32(os.Geteuid()), http.NotFoundHandler()); !errors.Is(e, ErrBusy) {
		t.Fatalf("live listener replaced: %v", e)
	}
}

func TestStrictDecodeRejectsNullDuplicateAndDepth(t *testing.T) {
	for _, raw := range []string{`null`, `{"value":1,"value":2}`, `{"value":null}`, strings.Repeat("[", 18) + "1" + strings.Repeat("]", 18)} {
		var dest any
		if strictDecode([]byte(raw), &dest) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	var v struct {
		Value string `json:"value"`
	}
	if e := strictDecode(bytes.TrimSpace([]byte(" {\"value\":\"ok\"} \n")), &v); e != nil || v.Value != "ok" {
		t.Fatalf("valid JSON rejected: %v", e)
	}
}

func TestDownloadLeaseSurvivesFullReadUntilDownstreamCloseOrAbort(t *testing.T) {
	for _, method := range []string{"close", "context_cancel"} {
		t.Run(method, func(t *testing.T) {
			f := newFixture(t)
			a := f.archive(t, "archive", false)
			client, _, _ := startUnixForTest(t, NewHandler(f.s, HandlerOptions{OpenArchive: openMemory}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			download, err := client.Download(ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer download.Body.Close()
			data, err := io.ReadAll(download.Body)
			if err != nil || string(data) != "data" {
				t.Fatalf("full response: %q %v", data, err)
			}
			// All UDS bytes are read, but the downstream HUB writer has not
			// acknowledged completion. A claimed job still has to wait.
			job := f.create(t, "queued-while-downloading")
			_, err = f.s.UpdateJob(context.Background(), job.ID, func(j *Job) error {
				j.State = "running"
				j.Phase = "preflight"
				j.StartedUnix = f.now.Unix()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if active, err := f.s.ActiveDownloads(context.Background()); err != nil || active != 1 {
				t.Fatalf("worker could stop before downstream completion: pins=%d %v", active, err)
			}
			if _, _, err := f.s.OpenDownload(context.Background(), a.ID, openMemory); !errors.Is(err, ErrBusy) {
				t.Fatalf("new download bypassed claimed job: %v", err)
			}
			if method == "close" {
				_ = download.Body.Close()
			} else {
				cancel()
			}
			deadline := time.Now().Add(time.Second)
			for {
				active, err := f.s.ActiveDownloads(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if active == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("downstream close did not release worker pin")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

func TestDownloadResponseHeadersAreBounded(t *testing.T) {
	request, _ := http.NewRequest("GET", "http://backup-control/v1/archive", nil)
	raw := "HTTP/1.1 200 OK\r\nContent-Length: 4\r\nX-Oversized: " + strings.Repeat("x", 17<<10) + "\r\n\r\ndata"
	if _, err := readDownloadResponse(bufio.NewReaderSize(strings.NewReader(raw), 16<<10), request); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("unbounded response header: %v", err)
	}
}

func TestDownloadCancellationBeforeHeadersReleasesPin(t *testing.T) {
	f := newFixture(t)
	a := f.archive(t, "archive", false)
	opening, aborted := make(chan struct{}), make(chan struct{})
	client, _, _ := startUnixForTest(t, NewHandler(f.s, HandlerOptions{OpenArchive: func(ctx context.Context, a Archive) (ReadSeekCloser, error) {
		close(opening)
		<-ctx.Done()
		close(aborted)
		return nil, ctx.Err()
	}}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		download, err := client.Download(ctx, a.ID)
		if download != nil {
			download.Body.Close()
		}
		result <- err
	}()
	<-opening
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled opener: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not close dedicated socket")
	}
	select {
	case <-aborted:
	case <-time.After(time.Second):
		t.Fatal("checksum opener did not receive cancellation")
	}
	deadline := time.Now().Add(time.Second)
	for {
		active, err := f.s.ActiveDownloads(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled opener leaked pin")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestUnixShutdownFinishesBeforeReturningWithDownloadLease(t *testing.T) {
	f := newFixture(t)
	a := f.archive(t, "archive", false)
	client, stop, stopped := startUnixForTest(t, NewHandler(f.s, HandlerOptions{OpenArchive: openMemory}))
	download, err := client.Download(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Body.Close()
	if _, err = io.ReadAll(download.Body); err != nil {
		t.Fatal(err)
	}
	stop()
	select {
	case <-stopped:
	case <-time.After(7 * time.Second):
		t.Fatal("Unix shutdown did not finish within its bounded grace")
	}
	deadline := time.Now().Add(time.Second)
	for {
		active, err := f.s.ActiveDownloads(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ServeUnix returned with an unfinished download shutdown")
		}
		time.Sleep(time.Millisecond)
	}
}
