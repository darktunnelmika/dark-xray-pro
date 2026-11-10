package api

import (
	"context"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"net/http"
	"time"
)

func (s *Server) attachNodeLifecycle(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/nodes/{id}/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		n, err := s.registry.NameState(r.PathValue("id"))
		if err != nil {
			respond(w, 404, map[string]string{"error": "node_not_found"})
			return
		}
		state, pending := s.registry.LifecycleDetails(n.Node.NodeID)
		respond(w, 200, map[string]any{"node": n.Node, "state": state, "pending": pending})
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/{id}/lifecycle", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if id == localHubNodeID {
			respond(w, 422, map[string]string{"error": "reserved_hub_identity"})
			return
		}
		if s.business == nil || s.inboundExecution == nil {
			respond(w, 503, map[string]string{"error": "managed_node_lifecycle_unavailable"})
			return
		}
		var input noderegistry.LifecycleInput
		if decode(w, r, 2048, &input) != nil {
			respond(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		receipt, err := s.registry.BeginLifecycle(id, session.User.ID, input)
		if err != nil {
			switch {
			case errors.Is(err, noderegistry.ErrInvalid):
				respond(w, 422, map[string]string{"error": "invalid_node_lifecycle"})
			case errors.Is(err, noderegistry.ErrNodeNotFound):
				respond(w, 404, map[string]string{"error": "node_not_found"})
			case errors.Is(err, noderegistry.ErrNameConflict), errors.Is(err, noderegistry.ErrTooMany):
				respond(w, 409, map[string]string{"error": "node_lifecycle_conflict"})
			default:
				respond(w, 503, map[string]string{"error": "node_lifecycle_outcome_unknown"})
			}
			return
		}
		if receipt.Completed {
			respond(w, 200, receipt)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		ids, err := s.business.DetachManagedNode(ctx, s.inboundKey, session.User.ID, input.EventID, id, input.InstallationID)
		if err != nil {
			respond(w, 503, map[string]any{"error": "node_detachment_pending", "receipt": receipt, "deployment_fenced": true})
			return
		}
		receipt, err = s.registry.FinishLifecycle(input.EventID, ids)
		if err != nil {
			respond(w, 503, map[string]any{"error": "node_detachment_pending", "deployment_fenced": true})
			return
		}
		respond(w, 200, receipt)
	})
}
