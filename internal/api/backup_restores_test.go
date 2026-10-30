package api

import (
	"context"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"net/http"
	"strings"
	"testing"
)

type fakeRestoreControl struct {
	fakeBackupControl
	restore backupcontrol.RestoreRequest
}

func (f *fakeRestoreControl) RestoreStatus(context.Context) (backupcontrol.RestoreStatus, error) {
	f.calls++
	return backupcontrol.RestoreStatus{Enabled: true, Jobs: []backupcontrol.RestoreJob{}}, nil
}
func (f *fakeRestoreControl) CreateRestore(_ context.Context, r backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, error) {
	f.calls++
	f.restore = r
	return backupcontrol.RestoreResponse{Job: backupcontrol.RestoreJob{ID: backupTestID, ActorID: r.ActorID, EventID: r.EventID}}, nil
}
func TestPanelRestoreOwnerSessionCSRFAndStrictBody(t *testing.T) {
	f := newBackupFixture(t)
	client := &fakeRestoreControl{}
	f.s.backupControl = client
	f.h = f.s.BrowserHandler()
	path := backupBrowserBase + "/restores"
	body := map[string]string{"event_id": "restore-request-00000001", "source_kind": "upload", "source_id": backupTestID, "sha256": strings.Repeat("a", 64), "confirmation": "RESTORE"}
	for _, method := range []string{"GET", "POST"} {
		for _, auth := range []struct {
			cookie *http.Cookie
			csrf   string
			want   int
		}{{nil, "", 401}, {f.rep, f.repCSRF, 403}} {
			w := browserRequest(t, f.h, method, path, body, auth.cookie, auth.csrf, "https://"+browserHost)
			if w.Code != auth.want {
				t.Fatal(method, w.Code, w.Body.String())
			}
		}
	}
	if w := browserRequest(t, f.h, "POST", path, body, f.owner, "", "https://"+browserHost); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, key := range []string{"path", "command", "identity_file", "actor_id"} {
		body[key] = "forbidden"
		w := browserRequest(t, f.h, "POST", path, body, f.owner, f.csrf, "https://"+browserHost)
		delete(body, key)
		if w.Code != 400 {
			t.Fatal(key, w.Code, w.Body.String())
		}
	}
	if client.calls != 0 {
		t.Fatal("refused request reached worker")
	}
	w := browserRequest(t, f.h, "POST", path, body, f.owner, f.csrf, "https://"+browserHost)
	if w.Code != 202 || client.restore.ActorID != f.actor || client.restore.SourceID != backupTestID {
		t.Fatal(w.Code, w.Body.String(), client.restore)
	}
}
