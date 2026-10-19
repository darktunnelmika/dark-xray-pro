package api

import (
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Profile changes remain cookie/CSRF-based and do not publish an Xray
// subscription. Every change is scoped twice: at the HTTP boundary and
// inside the same SQLite transaction that charges representative credit.
func (s *Server) attachClientProfileEdits(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	authorizeClient := func(w http.ResponseWriter, r *http.Request, mutate bool) (string, bool) {
		session, ok := s.authorizeBrowser(w, r, mutate, false)
		if !ok {
			return "", false
		}
		id := r.PathValue("id")
		c, err := s.business.Client(r.Context(), id)
		if err != nil {
			businessError(w, err)
			return "", false
		}
		if session.User.Role == "owner" {
			return "", true
		}
		if c.RepresentativeID != session.User.RepresentativeID {
			respond(w, http.StatusForbidden, map[string]string{"error": "representative_scope_denied"})
			return "", false
		}
		return session.User.RepresentativeID, true
	}
	mux.HandleFunc("POST /api/v1/workspace/clients/{id}/edit/quote",
		func(w http.ResponseWriter, r *http.Request) {
			scope, ok := authorizeClient(w, r, true)
			if !ok {
				return
			}
			var input business.ClientProfileEditIntent
			if err := decode(w, r, maxBusinessRequestBytes, &input); err != nil {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			if r.PathValue("id") != input.ClientID {
				businessError(w, business.ErrInvalid)
				return
			}
			quote, err := s.business.QuoteClientProfileEdit(r.Context(), input, scope)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, quote)
		})
	mux.HandleFunc("POST /api/v1/workspace/clients/{id}/edit/commit",
		func(w http.ResponseWriter, r *http.Request) {
			scope, ok := authorizeClient(w, r, true)
			if !ok {
				return
			}
			var input business.ClientProfileEditCommit
			if err := decode(w, r, maxBusinessRequestBytes, &input); err != nil {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			if r.PathValue("id") != input.ClientID {
				businessError(w, business.ErrInvalid)
				return
			}
			receipt, err := s.business.CommitClientProfileEdit(r.Context(), input, scope)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, receipt)
		})
	mux.HandleFunc("GET /api/v1/workspace/clients/{id}/edits",
		func(w http.ResponseWriter, r *http.Request) {
			scope, ok := authorizeClient(w, r, false)
			if !ok {
				return
			}
			limit, offset, err := pageParams(r)
			if err != nil || limit > 50 || r.URL.Query().Has("representative_id") {
				businessError(w, business.ErrInvalid)
				return
			}
			rows, err := s.business.ClientProfileEditHistory(r.Context(), r.PathValue("id"), scope, limit, offset)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, map[string]any{
				"edits": rows, "limit": limit, "offset": offset, "audit": "append_only",
				"applied_to_xray": false,
			})
		})
}
