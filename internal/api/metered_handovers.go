package api

import (
	"net/http"
	"regexp"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
)

var handoverID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type meteredHandoverInput struct {
	business.MeteredHandoverRequest
	SourceInstallation string `json:"source_installation"`
	TargetInstallation string `json:"target_installation"`
}

// Called under replacementLifecycle for every mutation. A historical operation
// remains readable when flags/registry change, but cannot gain execution rights.
func (s *Server) handoverScope(source, target, sourceInstall, targetInstall string) bool {
	if source == target || !s.meteredRouteScope(source, sourceInstall) || !s.meteredRouteScope(target, targetInstall) {
		return false
	}
	if s.replacements != nil {
		for _, op := range s.replacements.List() {
			if op.Phase != replacement.Cancelled && (op.OldNodeID == source || op.NewNodeID == source || op.OldNodeID == target || op.NewNodeID == target) {
				return false
			}
		}
	}
	return true
}

func (s *Server) attachMeteredHandovers(mux *http.ServeMux) {
	const base = "/api/v1/workspace/metered-handovers"
	ready := func(w http.ResponseWriter, r *http.Request, mutating bool) bool {
		if _, ok := s.authorizeBrowser(w, r, mutating, true); !ok {
			return false
		}
		if s.business == nil || len(s.inboundKey) != 32 {
			businessError(w, business.ErrUnavailable)
			return false
		}
		return true
	}
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if !ready(w, r, false) {
			return
		}
		v, err := s.business.ListMeteredHandovers(r.Context(), r.URL.Query().Get("before"))
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET "+base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !ready(w, r, false) {
			return
		}
		if !handoverID.MatchString(r.PathValue("id")) {
			businessError(w, business.ErrInvalid)
			return
		}
		v, err := s.business.MeteredHandoverAudit(r.Context(), s.inboundKey, r.PathValue("id"))
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		if !ready(w, r, true) {
			return
		}
		var in meteredHandoverInput
		if decode(w, r, 8192, &in) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.handoverScope(in.SourceNode, in.TargetNode, in.SourceInstallation, in.TargetInstallation) {
			businessError(w, business.ErrConflict)
			return
		}
		v, err := s.business.CreateMeteredHandover(r.Context(), s.inboundKey, in.MeteredHandoverRequest)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 201, v)
	})
	for _, action := range []string{"commands", "cancel"} {
		mux.HandleFunc("POST "+base+"/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			if !ready(w, r, true) {
				return
			}
			id := r.PathValue("id")
			if !handoverID.MatchString(id) {
				businessError(w, business.ErrInvalid)
				return
			}
			s.replacementLifecycle.Lock()
			defer s.replacementLifecycle.Unlock()
			op, err := s.business.MeteredHandover(r.Context(), id)
			if err != nil {
				businessError(w, err)
				return
			}
			if !s.handoverScope(op.SourceNode, op.TargetNode, op.SourceInstallation, op.TargetInstallation) {
				businessError(w, business.ErrConflict)
				return
			}
			if action == "cancel" {
				var in struct {
					Confirm bool `json:"confirm_before_drain"`
				}
				if decode(w, r, 128, &in) != nil || !in.Confirm {
					businessError(w, business.ErrInvalid)
					return
				}
				v, err := s.business.CancelMeteredHandover(r.Context(), id)
				if err != nil {
					businessError(w, err)
					return
				}
				respond(w, 200, v)
				return
			}
			var in business.MeteredRouteCommandRequest
			if decode(w, r, 8192, &in) != nil {
				businessError(w, business.ErrInvalid)
				return
			}
			v, err := s.business.QueueMeteredHandoverCommand(r.Context(), s.inboundKey, id, in)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, 202, v)
		})
	}
}
