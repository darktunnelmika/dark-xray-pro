package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

const backupTestID = "0123456789abcdef0123456789abcdef"

type fakeBackupControl struct {
	calls    int
	create   backupcontrol.CreateRequest
	settings backupcontrol.SettingsRequest
	cancel   backupcontrol.CancelRequest
	err      error
	download func() *backupcontrol.ArchiveDownload
}

func (f *fakeBackupControl) Status(context.Context) (backupcontrol.Status, error) {
	f.calls++
	s := backupcontrol.DisabledStatus()
	s.Enabled = true
	s.RunnerStatus = "ready"
	return s, f.err
}
func (f *fakeBackupControl) CreateJob(_ context.Context, r backupcontrol.CreateRequest) (backupcontrol.JobResponse, error) {
	f.calls++
	f.create = r
	return backupcontrol.JobResponse{Job: backupcontrol.Job{ID: backupTestID, EventID: r.EventID, ActorID: r.ActorID, State: "queued", Action: r.Action}}, f.err
}
func (f *fakeBackupControl) GetJob(context.Context, string) (backupcontrol.Job, error) {
	f.calls++
	return backupcontrol.Job{ID: backupTestID, State: "queued"}, f.err
}
func (f *fakeBackupControl) CancelJob(_ context.Context, _ string, r backupcontrol.CancelRequest) (backupcontrol.JobResponse, error) {
	f.calls++
	f.cancel = r
	return backupcontrol.JobResponse{Job: backupcontrol.Job{ID: backupTestID, State: "cancelling"}}, f.err
}
func (f *fakeBackupControl) UpdateSettings(_ context.Context, r backupcontrol.SettingsRequest) (backupcontrol.SettingsResponse, error) {
	f.calls++
	f.settings = r
	return backupcontrol.SettingsResponse{Settings: backupcontrol.Settings{Revision: r.ExpectedRevision + 1, Schedule: r.Schedule, Retention: r.Retention}}, f.err
}
func (f *fakeBackupControl) Download(context.Context, string) (*backupcontrol.ArchiveDownload, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.download == nil {
		return nil, backupcontrol.ErrNotFound
	}
	return f.download(), nil
}

type backupFixture struct {
	s                    *Server
	f                    *fakeBackupControl
	h                    http.Handler
	owner, rep           *http.Cookie
	csrf, repCSRF, actor string
}

func newBackupFixture(t *testing.T) backupFixture {
	t.Helper()
	s := enabledOwner(t)
	ctx := context.Background()
	if _, err := s.business.CreateRepresentative(ctx, "backupRep", "Synthetic representative"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "backupRep", "backuprep", repPassword); err != nil {
		t.Fatal(err)
	}
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "backuprep", repPassword)
	session, err := s.business.VerifySession(ctx, owner.Value)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeBackupControl{}
	s.backupControl = f
	return backupFixture{s: s, f: f, h: s.BrowserHandler(), owner: owner, rep: rep, csrf: csrf, repCSRF: repCSRF, actor: session.User.ID}
}

func backupSettingsBody() map[string]any {
	s := backupcontrol.DefaultSettings()
	return map[string]any{"event_id": "backup-settings-event-0001", "expected_revision": s.Revision, "schedule": s.Schedule, "retention": s.Retention}
}

func TestNativeBackupEveryBrowserRouteRequiresOwnerSession(t *testing.T) {
	f := newBackupFixture(t)
	routes := []struct {
		method, path string
		body         any
	}{
		{"GET", backupBrowserBase, nil},
		{"POST", backupBrowserBase + "/jobs", map[string]string{"event_id": "backup-create-event-0001", "action": "create"}},
		{"GET", backupBrowserBase + "/jobs/" + backupTestID, nil},
		{"POST", backupBrowserBase + "/jobs/" + backupTestID + "/cancel", map[string]string{"event_id": "backup-cancel-event-0001"}},
		{"PUT", backupBrowserBase + "/settings", backupSettingsBody()},
		{"GET", backupBrowserBase + "/archives/" + backupTestID + "/download", nil},
	}
	for _, route := range routes {
		for _, auth := range []struct {
			cookie *http.Cookie
			csrf   string
			want   int
		}{{nil, "", 401}, {f.rep, f.repCSRF, 403}} {
			w := browserRequest(t, f.h, route.method, route.path, route.body, auth.cookie, auth.csrf, "https://"+browserHost)
			if w.Code != auth.want {
				t.Fatalf("%s %s: %d %s", route.method, route.path, w.Code, w.Body.String())
			}
		}
		r := httptest.NewRequest(route.method, "https://"+browserHost+route.path, nil)
		r.Header.Set("Authorization", "Bearer "+f.s.ownerKey)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("owner bearer reached browser backups", w.Code)
		}
		r = httptest.NewRequest(route.method, "https://"+browserHost+route.path, nil)
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.AddCookie(f.owner)
		w = httptest.NewRecorder()
		f.s.AgentHandler().ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatal("Agent listener exposed backups", w.Code)
		}
	}
	if f.f.calls != 0 {
		t.Fatal("unauthorized request reached independent controller")
	}
}

func TestNativeBackupDisabledStateIsExplicitAndOwnerOnly(t *testing.T) {
	f := newBackupFixture(t)
	f.s.backupControl = nil
	h := f.s.BrowserHandler()
	w := browserRequest(t, h, "GET", backupBrowserBase, nil, f.owner, "", "")
	var status backupcontrol.Status
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.Enabled || status.Capabilities.Create || status.RunnerStatus != "disabled" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = browserRequest(t, h, "POST", backupBrowserBase+"/jobs", map[string]string{"event_id": "backup-disabled-event-01", "action": "create"}, f.owner, f.csrf, "https://"+browserHost)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "backup_control_disabled") {
		t.Fatal(w.Code, w.Body.String())
	}
	if f.f.calls != 0 {
		t.Fatal("disabled backups performed control I/O")
	}
}

func TestNativeBackupMutationsPreserveNonceAndDeriveActor(t *testing.T) {
	f := newBackupFixture(t)
	body := map[string]string{"event_id": "backup-create-event-0001", "action": "create"}
	for _, auth := range []struct{ csrf, origin string }{{"", "https://" + browserHost}, {f.csrf, ""}, {f.csrf, "https://evil.example"}} {
		w := browserRequest(t, f.h, "POST", backupBrowserBase+"/jobs", body, f.owner, auth.csrf, auth.origin)
		if w.Code != 403 {
			t.Fatal("mutation authority", w.Code)
		}
	}
	if f.f.calls != 0 {
		t.Fatal("CSRF denial reached worker")
	}
	w := browserRequest(t, f.h, "POST", backupBrowserBase+"/jobs", body, f.owner, f.csrf, "https://"+browserHost)
	if w.Code != 202 || f.f.create.ActorID != f.actor || f.f.create.EventID != body["event_id"] {
		t.Fatal(w.Code, w.Body.String(), f.f.create)
	}
	w = browserRequest(t, f.h, "PUT", backupBrowserBase+"/settings", backupSettingsBody(), f.owner, f.csrf, "https://"+browserHost)
	if w.Code != 200 || f.f.settings.ActorID != f.actor || f.f.settings.ExpectedRevision != 1 {
		t.Fatal(w.Code, w.Body.String(), f.f.settings)
	}
	w = browserRequest(t, f.h, "POST", backupBrowserBase+"/jobs/"+backupTestID+"/cancel", map[string]string{"event_id": "backup-cancel-event-0001"}, f.owner, f.csrf, "https://"+browserHost)
	if w.Code != 202 || f.f.cancel.ActorID != f.actor {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestNativeBackupStrictJSONRejectsAmbiguousOrInjectedRequests(t *testing.T) {
	f := newBackupFixture(t)
	settings, _ := json.Marshal(backupSettingsBody())
	cases := []struct{ method, path, body string }{
		{"POST", "/jobs", `{"event_id":"backup-create-event-0001","event_id":"backup-create-event-0002","action":"create"}`},
		{"POST", "/jobs", `{"Event_ID":"backup-create-event-0001","action":"create"}`},
		{"POST", "/jobs", `{"event_id":"backup-create-event-0001","action":"create","actor_id":"another-owner"}`},
		{"POST", "/jobs", `{"event_id":"backup-create-event-0001","action":"restore"}`},
		{"POST", "/jobs", `{"event_id":null,"action":"create"}`},
		{"POST", "/jobs", `{"event_id":"backup-create-event-0001"}`},
		{"POST", "/jobs?destination=/etc", `{"event_id":"backup-create-event-0001","action":"create"}`},
		{"PUT", "/settings", strings.Replace(string(settings), `"keep_last":7`, `"keep_last":7,"keep_last":1`, 1)},
		{"PUT", "/settings", strings.Replace(string(settings), `"timezone":"Asia/Tehran"`, `"Timezone":"Asia/Tehran"`, 1)},
		{"PUT", "/settings", string(settings) + `{}`},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "https://"+browserHost+backupBrowserBase+c.path, strings.NewReader(c.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://"+browserHost)
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(f.owner)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted ambiguous input %s: %d %s", c.body, w.Code, w.Body.String())
		}
	}
	if f.f.calls != 0 {
		t.Fatal("invalid JSON reached durable queue")
	}
}

func TestNativeBackupErrorsNeverExposeControllerPaths(t *testing.T) {
	f := newBackupFixture(t)
	for _, item := range []struct {
		err  error
		code int
		body string
	}{
		{backupcontrol.ErrConflict, 409, "backup_conflict"}, {backupcontrol.ErrBusy, 409, "backup_busy"},
		{backupcontrol.ErrNotFound, 404, "backup_not_found"}, {backupcontrol.ErrUnsafe, 503, "backup_state_unsafe"},
		{errors.New("private /var/backups/path credential must not escape"), 503, "backup_worker_unavailable"},
	} {
		f.f.err = item.err
		w := browserRequest(t, f.h, "GET", backupBrowserBase, nil, f.owner, "", "")
		if w.Code != item.code || !strings.Contains(w.Body.String(), item.body) || strings.Contains(w.Body.String(), "/var/") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

type measuredBackupReader struct {
	data    *bytes.Reader
	delay   time.Duration
	largest atomic.Int64
	closed  chan struct{}
}

func (r *measuredBackupReader) Read(p []byte) (int, error) {
	if int64(len(p)) > r.largest.Load() {
		r.largest.Store(int64(len(p)))
	}
	if r.delay > 0 {
		time.Sleep(r.delay)
		r.delay = 0
	}
	return r.data.Read(p)
}
func (r *measuredBackupReader) Close() error { close(r.closed); return nil }

func TestNativeBackupDownloadStreamsPastAPIDeadlineWithBoundedBuffer(t *testing.T) {
	f := newBackupFixture(t)
	data := bytes.Repeat([]byte("encrypted-fixture-data"), 65536)
	sum := sha256.Sum256(data)
	reader := &measuredBackupReader{data: bytes.NewReader(data), delay: 250 * time.Millisecond, closed: make(chan struct{})}
	f.f.download = func() *backupcontrol.ArchiveDownload {
		return &backupcontrol.ArchiveDownload{
			Archive: backupcontrol.Archive{ID: backupTestID, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), SourceRevision: strings.Repeat("a", 40), Verification: "checksum_verified", Downloadable: true},
			Body:    reader, ContentLength: int64(len(data))}
	}
	server := httptest.NewUnstartedServer(f.h)
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()
	r, _ := http.NewRequest("GET", server.URL+backupBrowserBase+"/archives/"+backupTestID+"/download", nil)
	r.Host = browserHost
	r.AddCookie(f.owner)
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(got, data) {
		t.Fatal("stream failed", response.StatusCode, len(got), err)
	}
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("controller download was not closed")
	}
	if reader.largest.Load() > 64<<10 {
		t.Fatal("unbounded controller download", reader.largest.Load())
	}
	if response.Header.Get("Content-Disposition") != `attachment; filename="dark-xray-pro-`+backupTestID+`.backup"` || response.Header.Get("X-Content-SHA256") != hex.EncodeToString(sum[:]) {
		t.Fatal("missing download identity")
	}
}
