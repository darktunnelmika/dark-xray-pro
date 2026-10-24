package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// EnableInboundInitialHealth is a distinct startup capability. Installing its
// storage creates neither a probe credential nor a command. Preparation,
// Stage, Apply, a protocol proof and subscription materialization remain
// separate owner actions; token delivery needs its own startup opt-in.
func (s *Server) EnableInboundInitialHealth(endpoint mx.ProtocolHealthEndpoint) error {
	if s.business == nil || len(s.inboundKey) != 32 || !s.inboundPlanPromotionEnabled ||
		len(s.meteredRouteNodes) != 1 || s.healthEndpoint != nil || s.initialApplyHealthEnabled || !endpoint.Valid() {
		return errors.New("initial Apply health requires one approved promotion Node and a pinned verifier")
	}
	for node, installation := range s.meteredRouteNodes {
		if !s.meteredRouteScope(node, installation) {
			return errors.New("initial Apply health installation is no longer paired")
		}
	}
	if err := s.business.EnableInitialApplyHealth(context.Background()); err != nil {
		return err
	}
	endpoint.CABundle = append([]byte(nil), endpoint.CABundle...)
	s.healthEndpoint = &endpoint
	s.initialApplyHealthEnabled = true
	return nil
}

// The submitted Agent version is never used to select proof authority. This
// rechecks the immutable encrypted promotion and the current registry pin.
func (s *Server) initialHealthPromotionScope(ctx context.Context, id uint64, node, installation string) bool {
	if !s.initialApplyHealthEnabled || s.healthEndpoint == nil || !s.inboundPlanPromotionEnabled {
		return false
	}
	v, err := s.business.NativeInboundPlanPromotionState(ctx, s.inboundKey, id)
	if err != nil || !v.Promoted || v.Receipt == nil || v.Receipt.InboundID != id {
		return false
	}
	return (node == "" || node == v.Receipt.NodeID) &&
		(installation == "" || installation == v.Receipt.InstallationID) &&
		s.meteredRouteScope(v.Receipt.NodeID, v.Receipt.InstallationID)
}

// These confirmed actions reject duplicate keys, nulls and unknown fields,
// including ambiguity in the reviewed identity or concurrency revision.
func decodeInitialHealthRequest(w http.ResponseWriter, r *http.Request, dest any, fields ...string) bool {
	var raw json.RawMessage
	if decode(w, r, 4096, &raw) != nil {
		return false
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return false
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	if last, err := d.Token(); err != nil || last != json.Delim('}') {
		return false
	}
	for _, field := range fields {
		if !seen[field] {
			return false
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(dest) == nil
}

func (s *Server) attachInboundInitialHealth(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	base := "/api/v1/workspace/native-inbound-plans/{id}/initial-health"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		if !s.initialApplyHealthEnabled {
			respond(w, http.StatusOK, map[string]any{"enabled": false, "scope": business.InitialApplyHealthScope, "inbound_id": id,
				"server_time_unix_milli": time.Now().UnixMilli(), "blockers": []business.InitialApplyHealthBlocker{{Field: "initial_health", Code: "initial_health_disabled"}}})
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		state, err := s.business.InitialApplyHealth(r.Context(), s.inboundKey, session.User.ID, id, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		available := s.initialHealthPromotionScope(r.Context(), id, state.NodeID, state.InstallationID)
		if !available {
			state.Eligible, state.CanPrepare = false, false
			state.Blockers = append(state.Blockers, business.InitialApplyHealthBlocker{Field: "nodes", Code: "pairing_changed"})
			if state.LatestRound != nil {
				state.LatestRound.Current = false
				state.LatestRound.State = "state_changed"
			}
		}
		state.ServerTimeMillis = time.Now().UnixMilli()
		respond(w, http.StatusOK, struct {
			business.InitialApplyHealthState
			Enabled   bool `json:"enabled"`
			Available bool `json:"available"`
		}{state, true, available})
	})
	mux.HandleFunc("POST "+base+"/prepare", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		var in struct {
			business.InitialApplyHealthPreparationInput
			Confirmed bool `json:"confirmed"`
		}
		if !decodeInitialHealthRequest(w, r, &in, "event_id", "promotion_event_id", "confirmed") || !in.Confirmed || !handoverID.MatchString(in.EventID) {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.initialHealthPromotionScope(r.Context(), id, "", "") {
			businessError(w, business.ErrConflict)
			return
		}
		v, err := s.business.PrepareInitialApplyHealth(r.Context(), s.inboundKey, session.User.ID, id, in.InitialApplyHealthPreparationInput)
		if err != nil {
			businessError(w, err)
			return
		}
		status := http.StatusCreated
		if v.Replayed {
			status = http.StatusOK
		}
		respond(w, status, struct {
			business.InitialApplyHealthPreparation
			ServerTime int64 `json:"server_time_unix_milli"`
		}{v, time.Now().UnixMilli()})
	})
	mux.HandleFunc("POST "+base+"/rounds", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		var in struct {
			business.InitialApplyHealthRoundInput
			Confirmed bool `json:"confirmed"`
		}
		if !decodeInitialHealthRequest(w, r, &in, "id", "initial_apply_sha", "confirmed") || !in.Confirmed || !handoverID.MatchString(in.ID) {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.initialHealthPromotionScope(r.Context(), id, "", "") {
			businessError(w, business.ErrConflict)
			return
		}
		v, err := s.business.QueueInitialApplyHealthRound(r.Context(), s.inboundKey, session.User.ID, id, in.InitialApplyHealthRoundInput, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusAccepted, struct {
			business.InitialApplyHealthRound
			ServerTime int64 `json:"server_time_unix_milli"`
		}{v, time.Now().UnixMilli()})
	})
	mux.HandleFunc("GET "+base+"/rounds/{round}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		if !handoverID.MatchString(r.PathValue("round")) {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.initialApplyHealthEnabled || s.healthEndpoint == nil {
			businessError(w, business.ErrConflict)
			return
		}
		v, err := s.business.InitialApplyHealthRound(r.Context(), s.inboundKey, session.User.ID, id, r.PathValue("round"), *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		if !s.initialHealthPromotionScope(r.Context(), id, v.NodeID, v.InstallationID) {
			v.Current, v.State = false, "state_changed"
		}
		respond(w, http.StatusOK, struct {
			business.InitialApplyHealthRound
			ServerTime int64 `json:"server_time_unix_milli"`
		}{v, time.Now().UnixMilli()})
	})
	mux.HandleFunc("POST "+base+"/snapshot", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		var in struct {
			business.InitialApplySubscriptionMaterialization
			Confirmed bool `json:"confirmed"`
		}
		if !decodeInitialHealthRequest(w, r, &in, "event_id", "client_id", "round_id", "initial_apply_sha", "expected_revision", "expected_delivery_revision", "confirmed") || !in.Confirmed || !handoverID.MatchString(in.EventID) || !handoverID.MatchString(in.RoundID) {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.labSubscriptionsEnabled || !s.initialHealthPromotionScope(r.Context(), id, "", "") {
			businessError(w, business.ErrConflict)
			return
		}
		v, err := s.business.MaterializeInitialApplySubscription(r.Context(), s.inboundKey, session.User.ID, id, in.InitialApplySubscriptionMaterialization, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		status := http.StatusCreated
		if v.Replayed {
			status = http.StatusOK
		}
		respond(w, status, struct {
			business.LabSubscriptionSnapshot
			ServerTime int64 `json:"server_time_unix_milli"`
		}{v, time.Now().UnixMilli()})
	})
}
