package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func nativeInboundPlanError(w http.ResponseWriter, err error) {
	var detail *business.NativeInboundPlanError
	if errors.As(err, &detail) {
		code := http.StatusUnprocessableEntity
		label := "invalid_business_input"
		if errors.Is(err, business.ErrConflict) {
			code = http.StatusConflict
			label = "business_event_or_identity_conflict"
		}
		respond(w, code, map[string]any{"error": label, "issue": detail})
		return
	}
	businessError(w, err)
}

func (s *Server) nativePlanReferences(nodes []business.NativeInboundPlanNode) error {
	paired := map[string]string{}
	for _, n := range s.registry.List() {
		paired[n.NodeID] = n.InstallationID
	}
	for _, n := range nodes {
		if paired[n.NodeID] == "" || paired[n.NodeID] != n.InstallationID {
			return business.ErrConflict
		}
		if n.SwapEnabled && (paired[n.SwapSourceNodeID] == "" || paired[n.SwapSourceNodeID] != n.SwapSourceInstallationID) {
			return business.ErrConflict
		}
	}
	return nil
}

func (s *Server) attachNativeInboundPlans(mux *http.ServeMux) {
	// A stable explicit capability lets a newer frontend explain an older or
	// disabled authoring feature without attempting partial creation requests.
	mux.HandleFunc("GET /api/v1/workspace/native-inbound-plans/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		available := s.business != nil && len(s.inboundKey) == 32
		if !available {
			respond(w, http.StatusOK, map[string]any{"available": false, "version": 1, "runtime_apply": false})
			return
		}
		plans, err := s.business.ListNativeInboundPlans(r.Context(), s.inboundKey)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{"available": true, "version": 2,
			"max_nodes": business.MaxNativeInboundPlanNodes, "max_hosts_per_node": 64,
			"protocols": []string{"vless"}, "securities": []string{"reality"}, "transports": []string{"tcp", "grpc", "xhttp"},
			"profile_xhttp": true, "profile_client_options": true, "profile_network_options_scope": "creation_plan",
			"endpoint_options": true, "max_endpoints_per_path": business.MaxNativeInboundEndpointsPerPath,
			"listener_port_min": 1024, "listener_port_max": 65535, "reserved_listener_ports": []uint16{2087, 8443, 24443},
			"profile_options": true, "profile_sniffing": true, "profile_fallbacks": true,
			"profile_traffic_options_scope": "creation_plan",
			"runtime_apply":                 false, "plans": plans})
	})
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	decodeInput := func(w http.ResponseWriter, r *http.Request) (business.NativeInboundPlanInput, bool) {
		var in business.NativeInboundPlanInput
		if decode(w, r, 3<<20, &in) != nil {
			nativeInboundPlanError(w, business.ErrInvalid)
			return in, false
		}
		return in, true
	}
	validateReferences := func(w http.ResponseWriter, in business.NativeInboundPlanInput) bool {
		if err := s.nativePlanReferences(in.Nodes); err != nil {
			respond(w, http.StatusConflict, map[string]any{"error": "business_event_or_identity_conflict",
				"issue": map[string]string{"field": "nodes", "code": "pairing_changed"}})
			return false
		}
		return true
	}
	replayReceipt := func(w http.ResponseWriter, r *http.Request, in business.NativeInboundPlanInput) bool {
		receipt, found, err := s.business.ReplayNativeInboundPlan(r.Context(), s.inboundKey, in)
		if err != nil {
			nativeInboundPlanError(w, err)
			return true
		}
		if found {
			respond(w, http.StatusOK, receipt)
			return true
		}
		return false
	}
	mux.HandleFunc("POST /api/v1/workspace/native-inbound-plans/preview", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		in, ok := decodeInput(w, r)
		if !ok {
			return
		}
		if !validateReferences(w, in) {
			return
		}
		preview, err := s.business.PreviewNativeInboundPlan(r.Context(), s.inboundKey, in)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		respond(w, http.StatusOK, preview)
	})
	mux.HandleFunc("POST /api/v1/workspace/native-inbound-plans", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		in, ok := decodeInput(w, r)
		if !ok {
			return
		}
		if in.ExpectedPlanRevision != 0 || in.ExpectedInboundRevision != 0 {
			nativeInboundPlanError(w, business.ErrInvalid)
			return
		}
		if replayReceipt(w, r, in) || !validateReferences(w, in) {
			return
		}
		receipt, err := s.business.SaveNativeInboundPlan(r.Context(), s.inboundKey, in)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		code := http.StatusCreated
		if receipt.Replayed {
			code = http.StatusOK
		}
		respond(w, code, receipt)
	})
	mux.HandleFunc("PUT /api/v1/workspace/native-inbound-plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 53)
		if err != nil || id == 0 {
			nativeInboundPlanError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		in, ok := decodeInput(w, r)
		if !ok {
			return
		}
		if in.Profile.ID != id || in.ExpectedPlanRevision == 0 || in.ExpectedInboundRevision == 0 {
			nativeInboundPlanError(w, business.ErrInvalid)
			return
		}
		if replayReceipt(w, r, in) || !validateReferences(w, in) {
			return
		}
		receipt, err := s.business.SaveNativeInboundPlan(r.Context(), s.inboundKey, in)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		respond(w, http.StatusOK, receipt)
	})
	mux.HandleFunc("GET /api/v1/workspace/native-inbound-plans/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 53)
		if err != nil || id == 0 {
			nativeInboundPlanError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		plan, err := s.business.NativeInboundPlan(r.Context(), s.inboundKey, id)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		// A replaced node does not destroy the author's saved draft. Return it
		// with an explicit stale-reference signal; preview/save recheck all IDs.
		referencesCurrent := s.nativePlanReferences(plan.Nodes) == nil
		respond(w, http.StatusOK, map[string]any{"plan": plan, "references_current": referencesCurrent})
	})
}
