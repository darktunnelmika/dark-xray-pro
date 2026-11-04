package api

import (
	"encoding/json"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

type darkRestorePromotionRequest struct {
	HandoffSHA         string `json:"handoff_sha256"`
	EventID            string `json:"event_id"`
	PromotionEventID   string `json:"promotion_event_id"`
	InboundID          uint64 `json:"inbound_id"`
	PlanRevision       uint64 `json:"plan_revision"`
	InboundRevision    uint64 `json:"inbound_revision"`
	Reason             string `json:"reason"`
	ReviewSHA          string `json:"review_sha256"`
	PromotionReviewSHA string `json:"promotion_review_sha256"`
	Confirmed          bool   `json:"confirmed"`
}

// The client comes from the independently staged handoff, never browser input.
// Preview rolls back its eligibility transition; commit only prepares native
// execution. Stage, Apply, health, delivery and activation remain separate.
func (s *Server) prepareDarkRestorePromotion(w http.ResponseWriter, r *http.Request, token, client, handoffSHA string, preview bool) {
	var raw json.RawMessage
	var req darkRestorePromotionRequest
	if decode(w, r, 4096, &raw) != nil || usagewire.Decode(raw, &req) != nil || req.HandoffSHA != handoffSHA ||
		preview && (req.Confirmed || req.ReviewSHA != "" || req.PromotionReviewSHA != "") || !preview && !req.Confirmed {
		businessError(w, business.ErrInvalid)
		return
	}
	s.replacementLifecycle.Lock()
	defer s.replacementLifecycle.Unlock()
	plan, err := s.business.NativeInboundPlan(r.Context(), s.inboundKey, req.InboundID)
	if err != nil {
		businessError(w, err)
		return
	}
	if blocked := s.nativePlanPromotionBlockers(plan); len(blocked) != 0 {
		nativePlanPromotionBlocked(w, blocked[0])
		return
	}
	in := business.DarkRestorePreparedPromotionInputV1{EventID: req.EventID, ClientID: client, InboundID: req.InboundID,
		Promotion: business.NativeInboundPlanPromotionInput{EventID: req.PromotionEventID, ExpectedPlanRevision: req.PlanRevision, ExpectedInboundRevision: req.InboundRevision,
			ClientIDs: []string{client}, Reason: req.Reason, ReviewSHA256: req.PromotionReviewSHA}}
	if preview {
		out, err := s.business.PreviewDarkRestorePreparedPromotionV1(r.Context(), s.inboundKey, token, in)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, out)
		return
	}
	out, err := s.business.CommitDarkRestorePreparedPromotionV1(r.Context(), s.inboundKey, token, req.ReviewSHA, in)
	if err != nil {
		businessError(w, err)
		return
	}
	status := http.StatusCreated
	if out.Replayed {
		status = http.StatusOK
	}
	respond(w, status, out)
}
