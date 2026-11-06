package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"net/http"
	"strconv"
)

func (s *Server) attachRepresentativeServiceExecution(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/clients/{id}/services", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok {
			return
		}
		if session.User.Role != "representative" {
			respond(w, 403, map[string]string{"error": "representative_scope_required"})
			return
		}
		if s.inboundExecution == nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		if r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.business.RepresentativeClientServices(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"))
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, out)
	})
	for _, action := range []string{"preview", "commands"} {
		action := action
		mux.HandleFunc("POST /api/v1/workspace/clients/{id}/services/{inbound}/"+action, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, true, false)
			if !ok {
				return
			}
			if session.User.Role != "representative" {
				respond(w, 403, map[string]string{"error": "representative_scope_required"})
				return
			}
			if s.inboundExecution == nil {
				businessError(w, business.ErrUnavailable)
				return
			}
			id, e := strconv.ParseUint(r.PathValue("inbound"), 10, 53)
			if e != nil || id == 0 || r.URL.RawQuery != "" {
				businessError(w, business.ErrInvalid)
				return
			}
			var raw struct {
				EventID           string  `json:"event_id"`
				GrantRevision     *uint64 `json:"grant_revision"`
				ExecutionRevision *uint64 `json:"execution_revision"`
				PlanRevision      *uint64 `json:"plan_revision"`
				InboundRevision   *uint64 `json:"inbound_revision"`
				Add               *bool   `json:"add"`
				Reason            string  `json:"reason"`
				ReviewSHA         string  `json:"review_sha,omitempty"`
			}
			if e := decode(w, r, 4096, &raw); e != nil || raw.GrantRevision == nil || raw.ExecutionRevision == nil || raw.PlanRevision == nil || raw.InboundRevision == nil || raw.Add == nil {
				businessError(w, business.ErrInvalid)
				return
			}
			if action == "preview" && raw.ReviewSHA != "" {
				businessError(w, business.ErrInvalid)
				return
			}
			out, e := s.business.ChangeRepresentativeService(r.Context(), s.inboundKey, session.User.ID, business.RepresentativeServiceChange{EventID: raw.EventID, ClientID: r.PathValue("id"), InboundID: id, GrantRevision: *raw.GrantRevision, ExecutionRevision: *raw.ExecutionRevision, PlanRevision: *raw.PlanRevision, InboundRevision: *raw.InboundRevision, Add: *raw.Add, Reason: raw.Reason, ReviewSHA: raw.ReviewSHA}, action == "commands", func(p ix.Preview) bool { return s.executionHostPreview(p).Valid })
			if e != nil {
				businessError(w, e)
				return
			}
			respond(w, 200, out)
		})
	}
}
