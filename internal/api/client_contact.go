package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"net/http"
)

func (s *Server) attachClientContacts(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/workspace/clients/{id}/contact", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, false)
		if !ok {
			return
		}
		var input business.ClientContactChange
		if decode(w, r, maxBusinessRequestBytes, &input) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		scope := ""
		if session.User.Role != "owner" {
			scope = session.User.RepresentativeID
		}
		result, e := s.business.SetClientContact(r.Context(), r.PathValue("id"), scope, input)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, http.StatusOK, result)
	})
}
