package api

import (
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"net/http"
)

type darkRestoreApplyReviewRequest struct {
	HandoffSHA string `json:"handoff_sha256"`
	EventID    string `json:"event_id"`
	InboundID  uint64 `json:"inbound_id"`
	RoundID    string `json:"round_id"`
	ReviewSHA  string `json:"review_sha256"`
	Confirmed  bool   `json:"confirmed"`
}

func (s *Server) reviewDarkRestoreApply(w http.ResponseWriter, r *http.Request, token, client, handoff string, preview bool) {
	var in darkRestoreApplyReviewRequest
	if !decodeInitialHealthRequest(w, r, &in, "handoff_sha256", "event_id", "inbound_id", "round_id", "review_sha256", "confirmed") || in.HandoffSHA != handoff || preview && (in.Confirmed || in.ReviewSHA != "") || !preview && !in.Confirmed {
		businessError(w, business.ErrInvalid)
		return
	}
	s.replacementLifecycle.Lock()
	defer s.replacementLifecycle.Unlock()
	if !s.initialHealthPromotionScope(r.Context(), in.InboundID, "", "") {
		businessError(w, business.ErrConflict)
		return
	}

	session, err := s.business.VerifySession(r.Context(), token)
	if err != nil {
		businessError(w, err)
		return
	}
	prior, priorErr := s.business.DarkRestoreNativeApplyAcceptanceReceipt(r.Context(), token, client)
	if priorErr != nil && !errors.Is(priorErr, business.ErrNotFound) {
		businessError(w, priorErr)
		return
	}
	if preview || prior.ClientID == "" {
		round, err := s.business.InitialApplyHealthRound(r.Context(), s.inboundKey, session.User.ID, in.InboundID, in.RoundID, *s.healthEndpoint)
		if err != nil || !round.Current || round.State != "complete" {
			businessError(w, business.ErrConflict)
			return
		}
	}
	input := business.DarkRestoreNativeApplyReviewInputV1{EventID: in.EventID, ClientID: client, InboundID: in.InboundID, RoundID: in.RoundID}
	if preview {
		out, err := s.business.PreviewDarkRestoreNativeApplyReviewV1(r.Context(), s.inboundKey, token, input)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, out)
		return
	}
	out, err := s.business.CommitDarkRestoreNativeApplyReviewV1(r.Context(), s.inboundKey, token, input, in.ReviewSHA)
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
