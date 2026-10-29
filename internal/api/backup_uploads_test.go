package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupimport"
)

type fakeUploadControl struct {
	*fakeBackupControl
	calls    int
	input    backupimport.CreateRequest
	actor    string
	entry    backupimport.Upload
	receive  func(context.Context, io.Reader) (*backupcontrol.UploadResponse, error)
	download func() *backupcontrol.UploadDownload
}

func (f *fakeUploadControl) UploadStatus(context.Context) (backupimport.Status, error) {
	f.calls++
	return backupimport.Status{Enabled: true, Uploads: []backupimport.Upload{f.entry}}, nil
}
func (f *fakeUploadControl) CreateUpload(_ context.Context, input backupimport.CreateRequest) (backupimport.Response, error) {
	f.calls++
	f.input = input
	return backupimport.Response{Upload: f.entry}, nil
}
func (f *fakeUploadControl) GetUpload(context.Context, string) (backupimport.Upload, error) {
	f.calls++
	return f.entry, nil
}
func (f *fakeUploadControl) Upload(ctx context.Context, _ string, actor string, _ int64, _ string, body io.Reader) (*backupcontrol.UploadResponse, error) {
	f.calls++
	f.actor = actor
	return f.receive(ctx, body)
}
func (f *fakeUploadControl) CancelUpload(_ context.Context, _, actor string) (backupimport.Response, error) {
	f.calls++
	f.actor = actor
	return backupimport.Response{Upload: f.entry}, nil
}
func (f *fakeUploadControl) RemoveUpload(_ context.Context, _, actor string) (backupimport.Response, error) {
	f.calls++
	f.actor = actor
	return backupimport.Response{Upload: f.entry}, nil
}
func (f *fakeUploadControl) DownloadUpload(context.Context, string) (*backupcontrol.UploadDownload, error) {
	f.calls++
	return f.download(), nil
}

func uploadFixture(t *testing.T) (backupFixture, *fakeUploadControl) {
	t.Helper()
	f := newBackupFixture(t)
	u := &fakeUploadControl{fakeBackupControl: f.f, entry: backupimport.Upload{
		ID: backupTestID, EventID: "backup-upload-event-00001", ActorID: f.actor,
		SizeBytes: 123, SHA256: strings.Repeat("a", 64), State: "awaiting_upload", Verification: "unverified",
	}}
	f.s.backupControl = u
	f.h = f.s.BrowserHandler()
	return f, u
}

type unreadUploadBody struct{ reads int }

func (b *unreadUploadBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (*unreadUploadBody) Close() error               { return nil }

func TestBackupUploadsEveryRouteRequiresOwnerAndAgentListenerIsClosed(t *testing.T) {
	f, control := uploadFixture(t)
	paths := []struct{ method, path string }{
		{"GET", backupUploadsBase}, {"POST", backupUploadsBase},
		{"GET", backupUploadsBase + "/" + backupTestID},
		{"PUT", backupUploadsBase + "/" + backupTestID + "/content"},
		{"POST", backupUploadsBase + "/" + backupTestID + "/cancel"},
		{"POST", backupUploadsBase + "/" + backupTestID + "/remove"},
		{"GET", backupUploadsBase + "/" + backupTestID + "/download"},
	}
	for _, route := range paths {
		for _, auth := range []struct {
			cookie *http.Cookie
			want   int
		}{{nil, 401}, {f.rep, 403}} {
			body := &unreadUploadBody{}
			r := httptest.NewRequest(route.method, "https://"+browserHost+route.path, body)
			r.ContentLength = 123
			if auth.cookie != nil {
				r.AddCookie(auth.cookie)
			}
			r.Header.Set("Authorization", "Bearer "+f.s.ownerKey)
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, r)
			if w.Code != auth.want || body.reads != 0 {
				t.Fatalf("unauthorized body used %s: status=%d reads=%d", route.path, w.Code, body.reads)
			}
		}
		r := httptest.NewRequest(route.method, "https://"+browserHost+route.path, nil)
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.AddCookie(f.owner)
		w := httptest.NewRecorder()
		f.s.AgentHandler().ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("upload route exposed on Agent listener", route.path, w.Code)
		}
	}
	if control.calls != 0 {
		t.Fatal("unauthorized request contacted worker")
	}
}

func TestBackupUploadBinaryAuthIsNarrowAndBodyRemainsUnreadOnRefusal(t *testing.T) {
	f, control := uploadFixture(t)
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Origin") },
		func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		func(r *http.Request) { r.Header.Set("X-CSRF-Token", "wrong") },
		func(r *http.Request) { r.Header.Set("Content-Type", "application/json") },
	} {
		body := &unreadUploadBody{}
		r := uploadBrowserRequest(f, body)
		change(r)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 403 || body.reads != 0 {
			t.Fatal("binary auth bypass", w.Code, body.reads)
		}
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.ContentLength = -1; r.TransferEncoding = []string{"chunked"} },
		func(r *http.Request) { r.ContentLength = 0 },
		func(r *http.Request) { r.ContentLength = backupimport.MaxUploadBytes + 1 },
		func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
		func(r *http.Request) { r.Header.Del(backupcontrol.UploadSHA256Header) },
		func(r *http.Request) { r.URL.RawQuery = "path=/etc/shadow" },
	} {
		body := &unreadUploadBody{}
		r := uploadBrowserRequest(f, body)
		change(r)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 400 || body.reads != 0 {
			t.Fatal("bad framing read", w.Code, body.reads)
		}
	}
	if control.calls != 0 {
		t.Fatal("refusal contacted worker")
	}
	// General browser mutations must still require JSON after adding uploads.
	r := httptest.NewRequest("POST", "https://"+browserHost+backupBrowserBase+"/jobs", strings.NewReader(`{"event_id":"backup-upload-event-00001","action":"create"}`))
	r.AddCookie(f.owner)
	r.Header.Set("Origin", "https://"+browserHost)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	if w.Code != 403 || f.f.calls != 0 {
		t.Fatal("binary exception widened ordinary mutations")
	}
}

func uploadBrowserRequest(f backupFixture, body io.Reader) *http.Request {
	r := httptest.NewRequest("PUT", "https://"+browserHost+backupUploadsBase+"/"+backupTestID+"/content", body)
	r.ContentLength = 123
	r.AddCookie(f.owner)
	r.Header.Set("Origin", "https://"+browserHost)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set(backupcontrol.UploadSHA256Header, strings.Repeat("a", 64))
	return r
}

func TestBackupUploadStrictDeclarationAndActorBinding(t *testing.T) {
	f, control := uploadFixture(t)
	good := `{"event_id":"backup-upload-event-00001","size_bytes":123,"sha256":"` + strings.Repeat("a", 64) + `"}`
	for _, body := range []string{
		strings.Replace(good, `"size_bytes":123`, `"size_bytes":123,"size_bytes":123`, 1),
		strings.Replace(good, `"size_bytes":123`, `"Size_Bytes":123`, 1),
		strings.Replace(good, `"size_bytes":123`, `"size_bytes":null`, 1),
		strings.TrimSuffix(good, "}") + `,"actor_id":"another-owner"}`,
		strings.TrimSuffix(good, "}") + `,"identity_file":"/private/key"}`,
		good + "{}",
	} {
		r := httptest.NewRequest("POST", "https://"+browserHost+backupUploadsBase, strings.NewReader(body))
		r.AddCookie(f.owner)
		r.Header.Set("Origin", "https://"+browserHost)
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("ambiguous allocation accepted", body, w.Code)
		}
	}
	if control.calls != 0 {
		t.Fatal("invalid allocation contacted worker")
	}
	w := browserRequest(t, f.h, "POST", backupUploadsBase, map[string]any{"event_id": "backup-upload-event-00001", "size_bytes": 123, "sha256": strings.Repeat("a", 64)}, f.owner, f.csrf, "https://"+browserHost)
	if w.Code != 201 || control.input.ActorID != f.actor {
		t.Fatal("actor not derived", w.Code, control.input)
	}
	for _, action := range []string{"cancel", "remove"} {
		w = browserRequest(t, f.h, "POST", backupUploadsBase+"/"+backupTestID+"/"+action, map[string]string{"action": action}, f.owner, f.csrf, "https://"+browserHost)
		if w.Code != 200 || control.actor != f.actor {
			t.Fatal("actor missing on mutation", w.Code, action)
		}
	}
	control.entry.SHA256 = strings.Repeat("b", 64)
	body := &unreadUploadBody{}
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, uploadBrowserRequest(f, body))
	if w.Code != 409 || body.reads != 0 {
		t.Fatal("different content passed declaration", w.Code, body.reads)
	}
}

type uploadCloseFunc func() error

func (f uploadCloseFunc) Close() error { return f() }

func TestBackupUploadStreamsBeyondOrdinaryServerDeadlinesAndReleasesLease(t *testing.T) {
	f, control := uploadFixture(t)
	data := bytes.Repeat([]byte("opaque-age-ciphertext"), 65536)
	sum := sha256.Sum256(data)
	control.entry.SizeBytes = int64(len(data))
	control.entry.SHA256 = hex.EncodeToString(sum[:])
	var largest atomic.Int64
	closed := make(chan struct{})
	control.receive = func(ctx context.Context, body io.Reader) (*backupcontrol.UploadResponse, error) {
		h := sha256.New()
		buf := make([]byte, 64<<10)
		for {
			n, err := body.Read(buf)
			if int64(len(buf)) > largest.Load() {
				largest.Store(int64(len(buf)))
			}
			h.Write(buf[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
		}
		if hex.EncodeToString(h.Sum(nil)) != control.entry.SHA256 {
			return nil, backupcontrol.ErrUnsafe
		}
		entry := control.entry
		entry.State = "received"
		entry.Verification = "ciphertext_checksum_verified"
		entry.Downloadable = true
		return &backupcontrol.UploadResponse{Response: backupimport.Response{Upload: entry}, Lease: uploadCloseFunc(func() error { close(closed); return nil })}, nil
	}
	server := httptest.NewUnstartedServer(f.h)
	server.Config.ReadTimeout = 50 * time.Millisecond
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()
	body := &measuredBackupReader{data: bytes.NewReader(data), delay: 250 * time.Millisecond, closed: make(chan struct{})}
	r, _ := http.NewRequest("PUT", server.URL+backupUploadsBase+"/"+backupTestID+"/content", body)
	r.ContentLength = int64(len(data))
	r.Host = browserHost
	r.AddCookie(f.owner)
	r.Header.Set("Origin", "https://"+browserHost)
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set(backupcontrol.UploadSHA256Header, control.entry.SHA256)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out backupimport.Response
	if json.NewDecoder(response.Body).Decode(&out) != nil || response.StatusCode != 200 || response.ContentLength < 1 || out.Upload.State != "received" {
		t.Fatal("long upload failed", response.StatusCode, out)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("receipt lease not released")
	}
	if largest.Load() > 64<<10 || control.actor != f.actor {
		t.Fatal("unbounded upload or wrong actor", largest.Load(), control.actor)
	}
}
