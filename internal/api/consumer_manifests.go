package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// EnableConsumerEndpoints is startup-only. The browser can select exact
// preapproved tuples for their Inbound, never introduce network authority.
func (s *Server) EnableConsumerEndpoints(approvals []mx.ConsumerEndpointApproval) error {
	if s.business == nil || len(s.inboundKey) != 32 || !s.labSubscriptionsEnabled || !s.healthRoundsEnabled || s.healthEndpoint == nil || s.consumerManifestsEnabled {
		return errors.New("consumer endpoints require explicit N18 subscriptions and health rounds")
	}
	if err := mx.ValidateConsumerEndpointApprovals(approvals); err != nil {
		return err
	}
	for _, approval := range approvals {
		if !s.meteredRouteScope(approval.NodeID, approval.InstallationID) {
			return errors.New("consumer endpoint installation is outside the approved LAB Nodes")
		}
	}
	if err := s.business.EnableConsumerManifests(context.Background(), approvals); err != nil {
		return err
	}
	s.consumerManifestsEnabled = true
	return nil
}

func (s *Server) attachConsumerManifests(mux *http.ServeMux) {
	const path = "/api/v1/workspace/metered-handovers/{id}/consumer-manifest"
	mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, true)
		if !ok {
			return
		}
		if s.business == nil || r.URL.RawQuery != "" || !handoverID.MatchString(r.PathValue("id")) {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.consumerManifestsEnabled {
			respond(w, http.StatusOK, map[string]any{"enabled": false, "scope": "consumer_endpoint_lab", "handover_id": r.PathValue("id")})
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if _, ok = s.healthOperation(w, r, r.PathValue("id")); !ok {
			return
		}
		v, err := s.business.MeteredConsumerManifest(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"))
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, struct {
			business.ConsumerManifestView
			Enabled    bool   `json:"enabled"`
			Scope      string `json:"scope"`
			ServerTime int64  `json:"server_time_unix_milli"`
		}{v, true, "consumer_endpoint_lab", time.Now().UnixMilli()})
	})
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		var in struct {
			business.ConsumerManifestMutation
			Confirmed bool `json:"confirmed"`
		}
		if r.URL.RawQuery != "" || !handoverID.MatchString(r.PathValue("id")) || decode(w, r, 64<<10, &in) != nil || !handoverID.MatchString(in.EventID) || !in.Confirmed {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.consumerManifestsEnabled {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok = s.healthOperation(w, r, r.PathValue("id")); !ok {
			return
		}
		v, err := s.business.MutateMeteredConsumerManifest(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), in.ConsumerManifestMutation)
		if err != nil {
			businessError(w, err)
			return
		}
		status := http.StatusCreated
		if v.Replayed {
			status = http.StatusOK
		}
		respond(w, status, v)
	})
	mux.HandleFunc("POST /api/v1/workspace/metered-handovers/{id}/consumer-health-rounds", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		var in struct {
			ID                       string `json:"id"`
			ConsumerManifestID       string `json:"consumer_manifest_id"`
			ConsumerManifestRevision uint64 `json:"consumer_manifest_revision"`
			Confirmed                bool   `json:"confirmed"`
		}
		if r.URL.RawQuery != "" || !handoverID.MatchString(r.PathValue("id")) || decode(w, r, 1024, &in) != nil || !handoverID.MatchString(in.ID) || !handoverID.MatchString(in.ConsumerManifestID) || in.ConsumerManifestRevision == 0 || !in.Confirmed {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.consumerManifestsEnabled {
			businessError(w, business.ErrConflict)
			return
		}
		op, ok := s.healthOperation(w, r, r.PathValue("id"))
		if !ok {
			return
		}
		v, err := s.business.QueueMeteredConsumerHealthRound(r.Context(), s.inboundKey, in.ID, op.ID, in.ConsumerManifestID, in.ConsumerManifestRevision, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusAccepted, v)
	})
}
