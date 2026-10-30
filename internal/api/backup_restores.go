package api

import (
	"context"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

type backupRestoreClient interface {
	RestoreStatus(context.Context) (backupcontrol.RestoreStatus, error)
	CreateRestore(context.Context, backupcontrol.RestoreRequest) (backupcontrol.RestoreResponse, error)
}

func (s *Server) attachBackupRestores(mux *http.ServeMux) {
	mux.HandleFunc("GET "+backupBrowserBase+"/restores", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.backupRequest(w, r, false); !ok {
			return
		}
		client, ok := s.backupControl.(backupRestoreClient)
		if !ok {
			respond(w, 200, backupcontrol.RestoreStatus{Jobs: []backupcontrol.RestoreJob{}})
			return
		}
		out, e := client.RestoreStatus(r.Context())
		if e != nil {
			backupControlError(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("POST "+backupBrowserBase+"/restores", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.backupRequest(w, r, true)
		if !ok || !s.backupEnabled(w) {
			return
		}
		client, ok := s.backupControl.(backupRestoreClient)
		if !ok {
			backupControlError(w, backupcontrol.ErrUnavailable)
			return
		}
		var input struct {
			EventID      string `json:"event_id"`
			SourceKind   string `json:"source_kind"`
			SourceID     string `json:"source_id"`
			SHA256       string `json:"sha256"`
			Confirmation string `json:"confirmation"`
		}
		if !decodeBackupBrowser(w, r, &input, backupJSONSchema{"event_id": nil, "source_kind": nil, "source_id": nil, "sha256": nil, "confirmation": nil}) {
			return
		}
		if !backupEventID.MatchString(input.EventID) || !backupID.MatchString(input.SourceID) || !backupSHA256.MatchString(input.SHA256) ||
			(input.SourceKind != "archive" && input.SourceKind != "upload") || input.Confirmation != "RESTORE" {
			backupControlError(w, backupcontrol.ErrInvalid)
			return
		}
		out, e := client.CreateRestore(r.Context(), backupcontrol.RestoreRequest{EventID: input.EventID, ActorID: actor, SourceKind: input.SourceKind, SourceID: input.SourceID, SHA256: input.SHA256, Confirmation: input.Confirmation})
		if e != nil {
			backupControlError(w, e)
			return
		}
		respond(w, http.StatusAccepted, out)
	})
}
