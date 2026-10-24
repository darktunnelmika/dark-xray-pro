package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

type nativeInboundPlanPromotionWire struct {
	business.NativeInboundPlanPromotionInput
	Confirmed bool `json:"confirmed"`
}

// Promotion reviews one exact saved plan and client set. Reject duplicate,
// unknown and null fields before interpreting the confirmation or review hash.
func decodeNativeInboundPlanPromotion(w http.ResponseWriter, r *http.Request) (nativeInboundPlanPromotionWire, bool) {
	var in nativeInboundPlanPromotionWire
	var raw json.RawMessage
	if decode(w, r, 65536, &raw) != nil {
		return in, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return in, false
	}
	allowed := map[string]bool{"event_id": true, "expected_plan_revision": true,
		"expected_inbound_revision": true, "client_ids": true, "reason": true,
		"review_sha256": true, "confirmed": true}
	seen := map[string]bool{}
	for d.More() {
		token, e := d.Token()
		name, ok := token.(string)
		if e != nil || !ok || !allowed[name] || seen[name] {
			return in, false
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return in, false
		}
	}
	if last, e := d.Token(); e != nil || last != json.Delim('}') {
		return in, false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return in, false
	}
	return in, true
}

func nativePlanPromotionID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseUint(raw, 10, 53)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw || r.URL.RawQuery != "" {
		nativeInboundPlanError(w, business.ErrInvalid)
		return 0, false
	}
	return id, true
}

// The HTTP caller cannot claim a channel, change its pin or enable execution.
// These checks are repeated under the replacement lock for every new request.
func (s *Server) nativePlanPromotionBlockers(plan business.NativeInboundPlan) []business.NativeInboundPlanPromotionBlocker {
	blocked := make([]business.NativeInboundPlanPromotionBlocker, 0)
	if !s.inboundPlanPromotionEnabled {
		return append(blocked, business.NativeInboundPlanPromotionBlocker{Field: "promotion", Code: "promotion_disabled"})
	}
	if err := s.nativePlanReferences(plan.Nodes); err != nil {
		return append(blocked, business.NativeInboundPlanPromotionBlocker{Field: "nodes", Code: "pairing_changed"})
	}
	for _, node := range plan.Nodes {
		if !s.meteredRouteScope(node.NodeID, node.InstallationID) {
			blocked = append(blocked, business.NativeInboundPlanPromotionBlocker{NodeID: node.NodeID, Field: "nodes", Code: "node_not_approved"})
		}
	}
	return blocked
}

func nativePlanPromotionBlocked(w http.ResponseWriter, issue business.NativeInboundPlanPromotionBlocker) {
	respond(w, http.StatusConflict, map[string]any{"error": "business_event_or_identity_conflict", "issue": issue})
}

func (s *Server) attachNativeInboundPlanPromotions(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	base := "/api/v1/workspace/native-inbound-plans/{id}/promotion"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id, ok := nativePlanPromotionID(w, r)
		if !ok {
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		plan, err := s.business.NativeInboundPlan(r.Context(), s.inboundKey, id)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		state, err := s.business.NativeInboundPlanPromotionState(r.Context(), s.inboundKey, id)
		if err != nil {
			nativeInboundPlanError(w, err)
			return
		}
		blocked := s.nativePlanPromotionBlockers(plan)
		available := len(blocked) == 0
		state.Blockers = append(blocked, state.Blockers...)
		if state.Blockers == nil {
			state.Blockers = []business.NativeInboundPlanPromotionBlocker{}
		}
		respond(w, http.StatusOK, struct {
			Available bool `json:"available"`
			business.NativeInboundPlanPromotionState
		}{available, state})
	})
	mutate := func(preview bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
				return
			}
			id, ok := nativePlanPromotionID(w, r)
			if !ok {
				return
			}
			in, ok := decodeNativeInboundPlanPromotion(w, r)
			if !ok || preview && (in.Confirmed || in.ReviewSHA256 != "") || !preview && !in.Confirmed {
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
			if blocked := s.nativePlanPromotionBlockers(plan); len(blocked) != 0 {
				nativePlanPromotionBlocked(w, blocked[0])
				return
			}
			if preview {
				result, err := s.business.PreviewNativeInboundPlanPromotion(r.Context(), s.inboundKey, id, in.NativeInboundPlanPromotionInput)
				if err != nil {
					nativeInboundPlanError(w, err)
					return
				}
				respond(w, http.StatusOK, result)
				return
			}
			receipt, err := s.business.PromoteNativeInboundPlan(r.Context(), s.inboundKey, id, in.NativeInboundPlanPromotionInput)
			if err != nil {
				nativeInboundPlanError(w, err)
				return
			}
			code := http.StatusCreated
			if receipt.Replayed {
				code = http.StatusOK
			}
			respond(w, code, receipt)
		}
	}
	mux.HandleFunc("POST "+base+"/preview", mutate(true))
	mux.HandleFunc("POST "+base, mutate(false))
}
