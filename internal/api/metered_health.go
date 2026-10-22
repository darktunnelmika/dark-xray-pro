package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// Startup-only operator configuration. No browser may set a verifier URL or
// enable a Node. No system identity, runtime or lease is created here.
func (s *Server) EnableMeteredHealth(endpoint mx.ProtocolHealthEndpoint) error {
	if s.business == nil || len(s.inboundKey) != 32 || s.healthEndpoint != nil || len(s.meteredRouteNodes) < 2 || !endpoint.Valid() {
		return errors.New("health transport requires approved metered Nodes and pinned verifier")
	}
	ctx := context.Background()
	if err := s.business.EnableSystemProbeAccounting(ctx); err != nil {
		return err
	}
	if err := s.business.EnableMeteredHealthChallenges(ctx); err != nil {
		return err
	}
	if err := s.business.EnableMeteredHealthQueue(ctx); err != nil {
		return err
	}
	endpoint.CABundle = append([]byte(nil), endpoint.CABundle...)
	s.healthEndpoint = &endpoint
	return nil
}

// EnableMeteredHealthRounds is an additional startup-only opt-in. The existing
// pinned transport must be configured first; a browser cannot migrate storage
// or choose the destination members, verifier, or validity window.
func (s *Server) EnableMeteredHealthRounds() error {
	if s.business == nil || s.healthEndpoint == nil || s.healthRoundsEnabled {
		return errors.New("health rounds require the approved pinned health transport")
	}
	if err := s.business.EnableMeteredHealthRounds(context.Background()); err != nil {
		return err
	}
	s.healthRoundsEnabled = true
	return nil
}

func (s *Server) healthOperation(w http.ResponseWriter, r *http.Request, id string) (business.MeteredHandoverView, bool) {
	op, err := s.business.MeteredHandover(r.Context(), id)
	if err != nil {
		businessError(w, err)
		return op, false
	}
	if s.healthEndpoint == nil || !s.handoverScope(op.SourceNode, op.TargetNode, op.SourceInstallation, op.TargetInstallation) {
		businessError(w, business.ErrConflict)
		return op, false
	}
	return op, true
}

func (s *Server) attachMeteredHealth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/workspace/metered-handovers/{id}/health-rounds", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		if s.business == nil || !handoverID.MatchString(r.PathValue("id")) {
			businessError(w, business.ErrInvalid)
			return
		}
		var in struct {
			ID        string `json:"id"`
			Confirmed bool   `json:"confirmed"`
		}
		if decode(w, r, 512, &in) != nil || !handoverID.MatchString(in.ID) || !in.Confirmed {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.healthRoundsEnabled {
			businessError(w, business.ErrConflict)
			return
		}
		op, ok := s.healthOperation(w, r, r.PathValue("id"))
		if !ok {
			return
		}
		v, err := s.business.QueueMeteredHealthRound(r.Context(), s.inboundKey, in.ID, op.ID, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusAccepted, v)
	})
	mux.HandleFunc("POST /api/v1/workspace/metered-handovers/{id}/health", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		if s.business == nil || !handoverID.MatchString(r.PathValue("id")) {
			businessError(w, business.ErrInvalid)
			return
		}
		var in struct {
			ID        string `json:"id"`
			InboundID uint64 `json:"inbound_id"`
			Confirm   bool   `json:"confirm_probe"`
		}
		if decode(w, r, 512, &in) != nil || !in.Confirm {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		op, ok := s.healthOperation(w, r, r.PathValue("id"))
		if !ok {
			return
		}
		v, err := s.business.QueueMeteredHealth(r.Context(), s.inboundKey, in.ID, op.ID, in.InboundID, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 202, v)
	})
}

func (s *Server) handleMeteredHealthAgent(w http.ResponseWriter, r *http.Request) {
	if s.healthEndpoint == nil {
		http.NotFound(w, r)
		return
	}
	// Keep lifecycle ownership checks short; no verifier I/O runs in the HUB.
	s.replacementLifecycle.Lock()
	defer s.replacementLifecycle.Unlock()
	token := []byte(bearer(r))
	if r.URL.Path == "/api/v1/agent/metered-health/poll" {
		var p mx.TransactionPoll
		if decode(w, r, 16384, &p) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if _, ok := s.registry.Authenticate(p.NodeID, p.InstallationID, string(token)); !ok || !mx.VerifyMeteredTransactionPoll(token, p) {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if !s.meteredRouteScope(p.NodeID, p.InstallationID) {
			businessError(w, business.ErrConflict)
			return
		}
		// Commit the signed observation through the existing channel, refusing
		// a pending execution command rather than running health alongside it.
		_, found, err := s.business.PollMeteredRouteCommand(r.Context(), s.inboundKey, token, p)
		if err != nil {
			businessError(w, err)
			return
		}
		if found {
			businessError(w, business.ErrConflict)
			return
		}
		q, found, err := s.business.PollMeteredHealth(r.Context(), s.inboundKey, token, p.NodeID, p.InstallationID, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		if !found {
			w.WriteHeader(204)
			return
		}
		if _, ok := s.healthOperation(w, r, q.HandoverID); !ok {
			return
		}
		respond(w, 200, mx.ProtocolHealthDelivery{Request: q, Endpoint: *s.healthEndpoint})
		return
	}
	var in mx.ProtocolHealthSubmission
	if decode(w, r, 2048, &in) != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	node, install, op, err := s.business.MeteredHealthChallengeScope(r.Context(), in.ChallengeID)
	if err != nil {
		businessError(w, err)
		return
	}
	if _, ok := s.registry.Authenticate(node, install, string(token)); !ok {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if _, ok := s.healthOperation(w, r, op); !ok {
		return
	}
	if _, err = s.business.ConsumeMeteredHealthProof(r.Context(), s.inboundKey, token, in.ChallengeID, *s.healthEndpoint, in.Receipt); err != nil {
		businessError(w, err)
		return
	}
	w.WriteHeader(204)
}
