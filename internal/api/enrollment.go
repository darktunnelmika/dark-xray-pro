package api

import (
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

const maxHeartbeatBytes = 4096
const maxEnrollmentBytes = 1024

// All endpoints remain bound to the single-host LAB listener. Enrollment
// codes and agent-generated bearer credentials must never cross plaintext WAN.
func (s *Server) attachEnrollment(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/nodes", func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwner(r) {
			respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		respond(w, http.StatusOK, map[string]any{"nodes": s.registry.List()})
	})

	mux.HandleFunc("POST /api/v1/admin/nodes/invites", func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwner(r) {
			respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
		invite, err := s.registry.Issue(input.NodeID, input.Name)
		switch {
		case err == nil:
			respond(w, http.StatusCreated, invite)
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_node"})
		case errors.Is(err, noderegistry.ErrExists), errors.Is(err, noderegistry.ErrPending), errors.Is(err, noderegistry.ErrTooMany):
			respond(w, http.StatusConflict, map[string]string{"error": "node_or_pairing_conflict"})
		default:
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "registry_unavailable"})
		}
	})

	mux.HandleFunc("POST /api/v1/agent/enroll", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Code           string `json:"pair_code"`
			NodeID         string `json:"node_id"`
			InstallationID string `json:"installation_id"`
			Token          string `json:"agent_token"`
		}
		if err := decode(w, r, maxEnrollmentBytes, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		info, err := s.registry.Claim(input.Code, input.NodeID, input.InstallationID, input.Token)
		switch {
		case err == nil:
			respond(w, http.StatusOK, map[string]any{
				"paired": true, "node_id": info.NodeID,
				"installation_id": info.InstallationID,
			})
		case errors.Is(err, noderegistry.ErrInvite), errors.Is(err, noderegistry.ErrExists):
			respond(w, http.StatusConflict, map[string]string{"error": "pairing_rejected"})
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_enrollment"})
		default:
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "registry_unavailable"})
		}
	})

	mux.HandleFunc("POST /api/v1/agent/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var sample telemetry.Sample
		if err := decode(w, r, maxHeartbeatBytes, &sample); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		info, ok := s.registry.Authenticate(sample.NodeID, sample.InstallationID, bearer(r))
		if !ok {
			respond(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		// Authoritative display name comes from owner-approved pairing.
		sample.Name = info.Name
		if err := s.fleet.Record(sample); err != nil {
			if errors.Is(err, telemetry.ErrOutOfOrder) {
				respond(w, http.StatusConflict, map[string]string{"error": "out_of_order"})
			} else {
				respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_telemetry"})
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
