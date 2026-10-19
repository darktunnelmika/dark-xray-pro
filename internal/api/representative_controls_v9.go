package api

import (
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Each control mutation is owner-session + origin + CSRF protected. It
// preserves the Representative.enabled flag used by billable Xray policy;
// login suspension is not customer traffic suspension.
func (s *Server) attachRepresentativeControls(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/control",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
				return
			}
			out, err := s.business.RepresentativeControl(r.Context(), r.PathValue("id"))
			if err != nil {
				businessError(w, err)
				return
			}
			if out.Representative.Kind != "representative" {
				businessError(w, business.ErrInvalid)
				return
			}
			respond(w, http.StatusOK, out)
		})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/control",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
				return
			}
			// Pointer fields reject accidental partial role updates. A missing
			// permission key must never be interpreted as an intentional false.
			var raw struct {
				EventID          string          `json:"event_id"`
				RepresentativeID string          `json:"representative_id"`
				ExpectedRevision *int64          `json:"expected_revision"`
				DisplayName      string          `json:"display_name"`
				LoginSuspended   *bool           `json:"login_suspended"`
				Permissions      map[string]bool `json:"permissions"`
				Reason           string          `json:"reason"`
			}
			if err := decode(w, r, maxBusinessRequestBytes, &raw); err != nil {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			keys := []string{"view_clients", "create_clients", "renew_clients", "edit_clients",
				"manage_clients", "manage_groups", "view_finance"}
			if raw.ExpectedRevision == nil || raw.LoginSuspended == nil || len(raw.Permissions) != len(keys) {
				businessError(w, business.ErrInvalid)
				return
			}
			for _, key := range keys {
				if _, exists := raw.Permissions[key]; !exists {
					businessError(w, business.ErrInvalid)
					return
				}
			}
			input := business.RepresentativeControlChange{
				EventID: raw.EventID, RepresentativeID: raw.RepresentativeID,
				ExpectedRevision: *raw.ExpectedRevision, DisplayName: raw.DisplayName,
				LoginSuspended: *raw.LoginSuspended, Reason: raw.Reason,
				Permissions: business.RepresentativePermissions{
					ViewClients:   raw.Permissions["view_clients"],
					CreateClients: raw.Permissions["create_clients"],
					RenewClients:  raw.Permissions["renew_clients"],
					EditClients:   raw.Permissions["edit_clients"],
					ManageClients: raw.Permissions["manage_clients"],
					ManageGroups:  raw.Permissions["manage_groups"],
					ViewFinance:   raw.Permissions["view_finance"],
				},
			}
			if input.RepresentativeID != r.PathValue("id") {
				businessError(w, business.ErrInvalid)
				return
			}
			result, err := s.business.UpdateRepresentativeControl(r.Context(), input)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, result)
		})
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/control/events",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
				return
			}
			limit, offset, err := pageParams(r)
			if err != nil || limit > 50 {
				businessError(w, business.ErrInvalid)
				return
			}
			events, err := s.business.ListRepresentativeControlEvents(r.Context(), r.PathValue("id"), limit, offset)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, map[string]any{
				"events": events, "limit": limit, "offset": offset, "audit": "append_only",
			})
		})
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/activity",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, false, false)
			if !ok {
				return
			}
			id := r.PathValue("id")
			if session.User.Role != "owner" && session.User.RepresentativeID != id {
				respond(w, http.StatusForbidden, map[string]string{"error": "representative_scope_denied"})
				return
			}
			activity, err := s.business.RepresentativeActivity(r.Context(), id)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, activity)
		})
}
