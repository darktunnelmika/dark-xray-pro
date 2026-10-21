package api

import (
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
)

// Owner browser visibility and cancellation of the nonactivating preflight only.
// No create, report, activation, subscription, or Agent route is exposed here.
func (s *Server) attachNodeReplacements(mux *http.ServeMux) {
	known := func(id string) bool {
		_, err := s.registry.NameState(id)
		return err == nil
	}
	mux.HandleFunc("GET /api/v1/workspace/nodes/{id}/replacements", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id := r.PathValue("id")
		if !known(id) {
			respond(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
			return
		}
		if s.replacements == nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "replacement_journal_unavailable"})
			return
		}
		rows := make([]replacement.Operation, 0)
		for _, op := range s.replacements.List() {
			if op.OldNodeID == id || op.NewNodeID == id {
				rows = append(rows, op)
			}
		}
		respond(w, http.StatusOK, map[string]any{"node_id": id, "operations": rows, "can_activate": false})
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/{id}/replacements/{operation}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		id := r.PathValue("id")
		if !known(id) {
			respond(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
			return
		}
		if s.replacements == nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "replacement_journal_unavailable"})
			return
		}
		var input struct {
			ExpectedRevision uint64 `json:"expected_revision"`
		}
		if err := decode(w, r, 256, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		op, err := s.replacements.Get(r.PathValue("operation"))
		if err != nil || op.OldNodeID != id && op.NewNodeID != id {
			respond(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		result, err := s.replacements.Cancel(op.ID, input.ExpectedRevision)
		switch {
		case err == nil:
			respond(w, http.StatusOK, result)
		case errors.Is(err, replacement.ErrNotFound):
			respond(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		default:
			respond(w, http.StatusConflict, map[string]string{"error": "revision_conflict"})
		}
	})
}
