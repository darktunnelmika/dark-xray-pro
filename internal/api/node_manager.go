package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

// Owner-only, session-authenticated Node enrollment. Only NEW PRO Node identities
// may be paired. This does not execute an installer, apply Xray or move clients.
type ownerNode struct {
	NodeID         string     `json:"node_id"`
	Name           string     `json:"name"`
	PairedAt       time.Time  `json:"paired_at"`
	Status         string     `json:"status"`
	MetricsCurrent bool       `json:"metrics_current"`
	LastSeen       *time.Time `json:"last_seen"`
}

func (s *Server) attachNodeManager(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/nodes", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		fleet := s.snapshot()
		current := make(map[string]ownerNode, len(fleet.Nodes))
		for _, node := range fleet.Nodes {
			current[node.NodeID] = ownerNode{NodeID: node.NodeID, Name: node.Name, Status: node.Status, MetricsCurrent: node.MetricsCurrent, LastSeen: node.LastSeen}
		}
		rows := make([]ownerNode, 0)
		for _, registration := range s.registry.List() {
			row := ownerNode{NodeID: registration.NodeID, Name: registration.Name, PairedAt: registration.PairedAt, Status: "offline"}
			if sample, found := current[registration.NodeID]; found {
				row.Status = sample.Status
				row.MetricsCurrent = sample.MetricsCurrent
				row.LastSeen = sample.LastSeen
			}
			rows = append(rows, row)
		}
		respond(w, http.StatusOK, map[string]any{"nodes": rows, "mode": "new_pro_lab_only"})
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/invites", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		var input struct {
			NodeID string `json:"node_id"`
			Name   string `json:"name"`
		}
		if err := decode(w, r, maxEnrollmentBytes, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		ticket, err := s.registry.Issue(input.NodeID, input.Name)
		switch {
		case err == nil:
			// Pair Code is returned ONLY by this POST and is never stored in plaintext.
			respond(w, http.StatusCreated, ticket)
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_node"})
		case errors.Is(err, noderegistry.ErrExists), errors.Is(err, noderegistry.ErrPending), errors.Is(err, noderegistry.ErrTooMany):
			respond(w, http.StatusConflict, map[string]string{"error": "node_or_pairing_conflict"})
		default:
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "registry_unavailable"})
		}
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/invites/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		// Explicit CSRF-protected cancellation touches only an unclaimed invitation,
		// never a Node, its Agent identity, clients, accounting or desired inbounds.
		if err := decode(w, r, 128, &struct{}{}); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		err := s.registry.CancelPending(r.PathValue("id"))
		switch {
		case err == nil:
			respond(w, http.StatusOK, map[string]bool{"cancelled": true})
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_node"})
		case errors.Is(err, noderegistry.ErrInvite):
			respond(w, http.StatusNotFound, map[string]string{"error": "pending_invite_not_found"})
		default:
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "registry_unavailable"})
		}
	})
}
