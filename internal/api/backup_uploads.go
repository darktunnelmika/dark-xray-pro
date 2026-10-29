package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupimport"
)

const backupUploadsBase = backupBrowserBase + "/uploads"

type backupUploadControlClient interface {
	UploadStatus(context.Context) (backupimport.Status, error)
	CreateUpload(context.Context, backupimport.CreateRequest) (backupimport.Response, error)
	GetUpload(context.Context, string) (backupimport.Upload, error)
	Upload(context.Context, string, string, int64, string, io.Reader) (*backupcontrol.UploadResponse, error)
	CancelUpload(context.Context, string, string) (backupimport.Response, error)
	RemoveUpload(context.Context, string, string) (backupimport.Response, error)
	DownloadUpload(context.Context, string) (*backupcontrol.UploadDownload, error)
}

func (s *Server) backupUploadClient(w http.ResponseWriter) (backupUploadControlClient, bool) {
	if !s.backupEnabled(w) {
		return nil, false
	}
	client, ok := s.backupControl.(backupUploadControlClient)
	if !ok {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "backup_uploads_unavailable"})
	}
	return client, ok
}

func (s *Server) attachBackupUploads(mux *http.ServeMux) {
	mux.HandleFunc("GET "+backupUploadsBase, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.backupRequest(w, r, false); !ok {
			return
		}
		client, ok := s.backupUploadClient(w)
		if !ok {
			return
		}
		status, err := client.UploadStatus(r.Context())
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, 200, status)
	})
	mux.HandleFunc("POST "+backupUploadsBase, func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.backupRequest(w, r, true)
		if !ok {
			return
		}
		client, ok := s.backupUploadClient(w)
		if !ok {
			return
		}
		var input struct {
			EventID   string `json:"event_id"`
			SizeBytes int64  `json:"size_bytes"`
			SHA256    string `json:"sha256"`
		}
		if !decodeBackupBrowser(w, r, &input, backupJSONSchema{"event_id": nil, "size_bytes": nil, "sha256": nil}) {
			return
		}
		if !backupEventID.MatchString(input.EventID) || input.SizeBytes < 1 || input.SizeBytes > backupimport.MaxUploadBytes || !backupSHA256.MatchString(input.SHA256) {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		result, err := client.CreateUpload(r.Context(), backupimport.CreateRequest{EventID: input.EventID, ActorID: actor, SizeBytes: input.SizeBytes, SHA256: input.SHA256})
		if err != nil {
			backupControlError(w, err)
			return
		}
		code := http.StatusCreated
		if result.Replayed {
			code = http.StatusOK
		}
		respond(w, code, result)
	})
	mux.HandleFunc("GET "+backupUploadsBase+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.backupRequest(w, r, false); !ok {
			return
		}
		client, ok := s.backupUploadClient(w)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !backupID.MatchString(id) {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		result, err := client.GetUpload(r.Context(), id)
		if err != nil {
			backupControlError(w, err)
			return
		}
		respond(w, 200, result)
	})
	for _, action := range []string{"cancel", "remove"} {
		mux.HandleFunc("POST "+backupUploadsBase+"/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			actor, ok := s.backupRequest(w, r, true)
			if !ok {
				return
			}
			client, ok := s.backupUploadClient(w)
			if !ok {
				return
			}
			var input struct {
				Action string `json:"action"`
			}
			if !decodeBackupBrowser(w, r, &input, backupJSONSchema{"action": nil}) {
				return
			}
			id := r.PathValue("id")
			if !backupID.MatchString(id) || input.Action != action {
				backupControlError(w, backupcontrol.ErrInvalid)
				return
			}
			var result backupimport.Response
			var err error
			if action == "cancel" {
				result, err = client.CancelUpload(r.Context(), id, actor)
			} else {
				result, err = client.RemoveUpload(r.Context(), id, actor)
			}
			if err != nil {
				backupControlError(w, err)
				return
			}
			respond(w, 200, result)
		})
	}
	mux.HandleFunc("PUT "+backupUploadsBase+"/{id}/content", s.receiveBackupUpload)
	mux.HandleFunc("GET "+backupUploadsBase+"/{id}/download", s.downloadBackupUpload)
}

func (s *Server) receiveBackupUpload(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authorizeBrowserMedia(w, r, true, true, "application/octet-stream")
	if !ok {
		return
	}
	id, hash := r.PathValue("id"), r.Header.Get(backupcontrol.UploadSHA256Header)
	if r.URL.RawQuery != "" || r.URL.ForceQuery || !backupID.MatchString(id) || !backupSHA256.MatchString(hash) ||
		r.Header.Get("Content-Encoding") != "" || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > backupimport.MaxUploadBytes {
		backupControlError(w, backupcontrol.ErrInvalid)
		return
	}
	client, ok := s.backupUploadClient(w)
	if !ok {
		return
	}
	// Reject conflicting declarations before reading any uploaded data. The
	// worker repeats this check against its authoritative durable allocation.
	entry, err := client.GetUpload(r.Context(), id)
	if err != nil {
		backupControlError(w, err)
		return
	}
	if entry.ActorID != session.User.ID || entry.SizeBytes != r.ContentLength || entry.SHA256 != hash {
		backupControlError(w, backupcontrol.ErrConflict)
		return
	}
	ctx, body, cleanup, err := backupcontrol.PrepareUpload(w, r)
	if err != nil {
		backupControlError(w, err)
		return
	}
	defer cleanup()
	result, err := client.Upload(ctx, id, session.User.ID, r.ContentLength, hash, body)
	if err != nil {
		backupControlError(w, err)
		return
	}
	if result == nil || result.Lease == nil {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	defer result.Lease.Close()
	value := result.Response.Upload
	if value.ID != id || value.ActorID != session.User.ID || value.SizeBytes != entry.SizeBytes || value.SHA256 != entry.SHA256 {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	c := http.NewResponseController(w)
	if c.SetWriteDeadline(time.Now().Add(30*time.Second)) != nil {
		panic(http.ErrAbortHandler)
	}
	raw, err := json.Marshal(result.Response)
	if err != nil || len(raw) > 4096 {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	// A known length makes the flushed receipt complete on the wire. A
	// chunked response would still need its final delimiter after handler
	// return, which is too late to release the worker availability lease here.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	if _, err := w.Write(raw); err != nil {
		panic(http.ErrAbortHandler)
	}
	if c.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) downloadBackupUpload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.backupRequest(w, r, false); !ok {
		return
	}
	client, ok := s.backupUploadClient(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !backupID.MatchString(id) {
		backupControlError(w, backupcontrol.ErrInvalid)
		return
	}
	controller := http.NewResponseController(w)
	if controller.SetWriteDeadline(time.Now().Add(time.Hour)) != nil {
		backupControlError(w, backupcontrol.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), backupcontrol.UploadMaxDuration)
	defer cancel()
	download, err := client.DownloadUpload(ctx, id)
	if err != nil {
		backupControlError(w, err)
		return
	}
	if download == nil || download.Body == nil {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	defer download.Body.Close()
	entry := download.Upload
	if entry.ID != id || entry.State != "received" || entry.Verification != "ciphertext_checksum_verified" || !entry.Downloadable ||
		entry.SizeBytes < 1 || entry.SizeBytes > backupimport.MaxUploadBytes || download.ContentLength != entry.SizeBytes || !backupSHA256.MatchString(entry.SHA256) {
		backupControlError(w, backupcontrol.ErrUnsafe)
		return
	}
	if controller.SetWriteDeadline(time.Now().Add(30*time.Second)) != nil {
		backupControlError(w, backupcontrol.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="dark-xray-pro-upload-`+id+`.backup"`)
	w.Header().Set("Content-Length", strconv.FormatInt(entry.SizeBytes, 10))
	w.Header().Set("X-Content-SHA256", entry.SHA256)
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Accept-Ranges", "none")
	if r.Method != http.MethodHead {
		written, err := io.CopyBuffer(backupDownloadWriter{w, controller}, io.LimitReader(download.Body, entry.SizeBytes), make([]byte, 64<<10))
		if err != nil || written != entry.SizeBytes {
			panic(http.ErrAbortHandler)
		}
	}
	if controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
}
