package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

const backupBrowserBase = "/api/v1/workspace/owner/backups"

var backupID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var backupEventID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,96}$`)
var backupSHA256 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var backupRevision = regexp.MustCompile(`^[a-f0-9]{40}$`)

type backupControlClient interface {
	Status(context.Context) (backupcontrol.Status, error)
	CreateJob(context.Context, backupcontrol.CreateRequest) (backupcontrol.JobResponse, error)
	GetJob(context.Context, string) (backupcontrol.Job, error)
	CancelJob(context.Context, string, backupcontrol.CancelRequest) (backupcontrol.JobResponse, error)
	UpdateSettings(context.Context, backupcontrol.SettingsRequest) (backupcontrol.SettingsResponse, error)
	Download(context.Context, string) (*backupcontrol.ArchiveDownload, error)
}

// EnableBackupControlV1 is startup-only. The HUB only proxies owner requests to
// the fixed Unix control service; it never stops itself or runs a backup child.
// Availability is checked per request so worker downtime cannot stop the HUB.
func (s *Server) EnableBackupControlV1(client *backupcontrol.Client) error {
	if s.business == nil || client == nil || s.backupControl != nil {
		return errors.New("backup_control_prerequisites_missing")
	}
	s.backupControl = client
	return nil
}

func backupControlError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "backup_worker_unavailable"
	switch {
	case errors.Is(err, backupcontrol.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_backup_request"
	case errors.Is(err, backupcontrol.ErrConflict):
		status, code = http.StatusConflict, "backup_conflict"
	case errors.Is(err, backupcontrol.ErrBusy):
		status, code = http.StatusConflict, "backup_busy"
	case errors.Is(err, backupcontrol.ErrCapacity):
		status, code = http.StatusConflict, "backup_capacity"
	case errors.Is(err, backupcontrol.ErrNotFound):
		status, code = http.StatusNotFound, "backup_not_found"
	case errors.Is(err, backupcontrol.ErrUnsafe):
		code = "backup_state_unsafe"
	}
	respond(w, status, map[string]string{"error": code})
}

// Explicit schemas reject duplicate keys, case aliases, unknown/missing fields,
// nulls and unintended nested values before ordinary typed JSON decoding.
type backupJSONSchema map[string]backupJSONSchema

func backupStrictObject(raw []byte, schema backupJSONSchema, depth int) bool {
	if depth > 3 {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return false
	}
	seen := make(map[string]bool, len(schema))
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		child, allowed := schema[key]
		if err != nil || !ok || !allowed || seen[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
		if child != nil && !backupStrictObject(value, child, depth+1) {
			return false
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(seen) != len(schema) {
		return false
	}
	var trailing any
	return errors.Is(d.Decode(&trailing), io.EOF)
}

func decodeBackupBrowser(w http.ResponseWriter, r *http.Request, input any, schema backupJSONSchema) bool {
	var raw json.RawMessage
	if decode(w, r, 4096, &raw) != nil || !backupStrictObject(raw, schema, 0) || json.Unmarshal(raw, input) != nil {
		backupControlError(w, backupcontrol.ErrInvalid)
		return false
	}
	return true
}

func (s *Server) backupRequest(w http.ResponseWriter, r *http.Request, mutate bool) (string, bool) {
	session, ok := s.authorizeBrowser(w, r, mutate, true)
	if !ok {
		return "", false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		backupControlError(w, backupcontrol.ErrInvalid)
		return "", false
	}
	return session.User.ID, true
}

func (s *Server) backupEnabled(w http.ResponseWriter) bool {
	if s.backupControl == nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "backup_control_disabled"})
		return false
	}
	return true
}

func (s *Server) attachBackupControl(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	mux.HandleFunc("GET "+backupBrowserBase, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.backupRequest(w, r, false); !ok {
			return
		}
		if s.backupControl == nil {
			respond(w, http.StatusOK, backupcontrol.DisabledStatus())
			return
		}
		status, err := s.backupControl.Status(r.Context())
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST "+backupBrowserBase+"/jobs", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.backupRequest(w, r, true)
		if !ok || !s.backupEnabled(w) {
			return
		}
		var input struct {
			EventID string `json:"event_id"`
			Action  string `json:"action"`
		}
		if !decodeBackupBrowser(w, r, &input, backupJSONSchema{"event_id": nil, "action": nil}) {
			return
		}
		if !backupEventID.MatchString(input.EventID) || input.Action != "create" {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		result, err := s.backupControl.CreateJob(r.Context(), backupcontrol.CreateRequest{EventID: input.EventID, Action: input.Action, ActorID: actor})
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, http.StatusAccepted, result)
	})
	mux.HandleFunc("GET "+backupBrowserBase+"/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.backupRequest(w, r, false); !ok || !s.backupEnabled(w) {
			return
		}
		if !backupID.MatchString(r.PathValue("id")) {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		result, err := s.backupControl.GetJob(r.Context(), r.PathValue("id"))
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST "+backupBrowserBase+"/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.backupRequest(w, r, true)
		if !ok || !s.backupEnabled(w) {
			return
		}
		var input struct {
			EventID string `json:"event_id"`
		}
		if !backupID.MatchString(r.PathValue("id")) {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		if !decodeBackupBrowser(w, r, &input, backupJSONSchema{"event_id": nil}) {
			return
		}
		if !backupEventID.MatchString(input.EventID) {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		result, err := s.backupControl.CancelJob(r.Context(), r.PathValue("id"), backupcontrol.CancelRequest{EventID: input.EventID, ActorID: actor})
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, http.StatusAccepted, result)
	})
	mux.HandleFunc("PUT "+backupBrowserBase+"/settings", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.backupRequest(w, r, true)
		if !ok || !s.backupEnabled(w) {
			return
		}
		var input struct {
			EventID          string                  `json:"event_id"`
			ExpectedRevision uint64                  `json:"expected_revision"`
			Schedule         backupcontrol.Schedule  `json:"schedule"`
			Retention        backupcontrol.Retention `json:"retention"`
		}
		schema := backupJSONSchema{"event_id": nil, "expected_revision": nil,
			"schedule":  {"enabled": nil, "frequency": nil, "time": nil, "timezone": nil, "weekday": nil},
			"retention": {"keep_last": nil, "max_age_days": nil}}
		if !decodeBackupBrowser(w, r, &input, schema) {
			return
		}
		if !backupEventID.MatchString(input.EventID) || input.ExpectedRevision == 0 {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		result, err := s.backupControl.UpdateSettings(r.Context(), backupcontrol.SettingsRequest{
			EventID: input.EventID, ActorID: actor, ExpectedRevision: input.ExpectedRevision, Schedule: input.Schedule, Retention: input.Retention})
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET "+backupBrowserBase+"/archives/{id}/download", s.downloadBackupArchive)
	s.attachBackupUploads(mux)
}

type backupDownloadWriter struct {
	w          http.ResponseWriter
	controller *http.ResponseController
}

func (w backupDownloadWriter) Write(p []byte) (int, error) {
	// Sliding idle deadline permits multi-GiB streaming beyond the server's
	// normal 15-second API budget without accepting a permanently stalled peer.
	if err := w.controller.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}

func (s *Server) downloadBackupArchive(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.backupRequest(w, r, false); !ok || !s.backupEnabled(w) {
		return
	}
	id := r.PathValue("id")
	if !backupID.MatchString(id) {
		backupControlError(w, backupcontrol.ErrInvalid)
		return
	}
	download, err := s.backupControl.Download(r.Context(), id)
	if err != nil {
		backupControlError(w, err)
		return
	}
	if download == nil || download.Body == nil {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	defer download.Body.Close()
	a := download.Archive
	if a.ID != id || !a.Downloadable || a.SizeBytes < 1 || a.SizeBytes > backupcontrol.MaxArchiveBytes || download.ContentLength != a.SizeBytes ||
		!backupSHA256.MatchString(a.SHA256) || !backupRevision.MatchString(a.SourceRevision) ||
		(a.Verification != "checksum_verified" && a.Verification != "restore_verified") {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		backupControlError(w, backupcontrol.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="dark-xray-pro-`+id+`.backup"`)
	w.Header().Set("Content-Length", strconv.FormatInt(a.SizeBytes, 10))
	w.Header().Set("X-Content-SHA256", a.SHA256)
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		if err := controller.Flush(); err != nil {
			panic(http.ErrAbortHandler)
		}
		return
	}
	written, err := io.CopyBuffer(backupDownloadWriter{w, controller}, io.LimitReader(download.Body, a.SizeBytes), make([]byte, 64<<10))
	if err != nil || written != a.SizeBytes {
		panic(http.ErrAbortHandler)
	}
	// Release the dedicated worker connection only after net/http has also
	// flushed its final downstream buffer. The worker holds the archive pin
	// until Body.Close, preventing a new backup from stopping this HUB early.
	if err := controller.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
}
