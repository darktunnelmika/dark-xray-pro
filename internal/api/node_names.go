package api

import (
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

func (s *Server) attachNodeNames(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/nodes/{id}/name", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		state, err := s.registry.NameState(r.PathValue("id"))
		if err != nil {
			respond(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
			return
		}
		respond(w, http.StatusOK, state)
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/{id}/rename", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		var input noderegistry.RenameRequest
		if err := decode(w, r, maxEnrollmentBytes, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		receipt, err := s.registry.Rename(r.PathValue("id"), session.User.ID, input)
		switch {
		case err == nil:
			// This may be the original receipt of an old request. Clients must
			// fetch current name state separately instead of treating it as live.
			respond(w, http.StatusOK, map[string]any{"receipt": receipt})
		case errors.Is(err, noderegistry.ErrNodeNotFound):
			respond(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_node_name"})
		case errors.Is(err, noderegistry.ErrNameConflict), errors.Is(err, noderegistry.ErrTooMany):
			respond(w, http.StatusConflict, map[string]string{"error": "node_name_conflict"})
		default:
			// Outcome may be committed but not durably acknowledged. Never say
			// "nothing changed"; retry exactly the same event and request.
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "node_name_outcome_unknown"})
		}
	})
}
