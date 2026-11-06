package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"net/http"
	"strconv"
)

func (s *Server) attachRepresentativeServiceGrants(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	path := "/api/v1/workspace/representatives/{id}/service-grants/{inbound}"
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id, e := strconv.ParseUint(r.PathValue("inbound"), 10, 53)
		if e != nil || id == 0 || r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.business.RepresentativeServiceGrant(r.Context(), r.PathValue("id"), id)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, out)
	})
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		id, e := strconv.ParseUint(r.PathValue("inbound"), 10, 53)
		if e != nil || id == 0 || r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		var raw struct {
			EventID          string `json:"event_id"`
			ExpectedRevision *int64 `json:"expected_revision"`
			Enabled          *bool  `json:"enabled"`
			Reason           string `json:"reason"`
		}
		if e := decode(w, r, 4096, &raw); e != nil || raw.ExpectedRevision == nil || raw.Enabled == nil {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.business.ChangeRepresentativeServiceGrant(r.Context(), session.User.ID, business.ServiceGrantChange{EventID: raw.EventID, RepresentativeID: r.PathValue("id"), InboundID: id, ExpectedRevision: *raw.ExpectedRevision, Enabled: *raw.Enabled, Reason: raw.Reason})
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, out)
	})
}
