package api

import (
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// P0-E is deliberately a DRAFT endpoint: never creates a live Xray listener,
// publishes a subscription, restarts an Agent or adjusts client credit.
func (s *Server) attachNativeRouteDrafts(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	validateInput := func(w http.ResponseWriter, r *http.Request) (business.NativeRouteDraftInput, bool) {
		var input business.NativeRouteDraftInput
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 53)
		if err != nil || id == 0 || decode(w, r, 16384, &input) != nil || input.InboundID != id {
			businessError(w, business.ErrInvalid)
			return input, false
		}
		installation, exists := s.nativeNode(input.NodeID)
		if !exists || installation != input.InstallationID {
			businessError(w, business.ErrConflict)
			return input, false
		}
		if input.SwapEnabled {
			if _, found := s.nativeNode(input.SwapUpstreamNodeID); !found {
				businessError(w, business.ErrConflict)
				return input, false
			}
		}
		return input, true
	}
	mux.HandleFunc("POST /api/v1/workspace/native-inbounds/{id}/route-drafts/preview",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
				return
			}
			input, ok := validateInput(w, r)
			if !ok {
				return
			}
			preview, err := s.business.PreviewNativeRouteDraft(r.Context(), s.inboundKey, input)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, preview)
		})
	mux.HandleFunc("POST /api/v1/workspace/native-inbounds/{id}/route-drafts",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
				return
			}
			input, ok := validateInput(w, r)
			if !ok {
				return
			}
			receipt, err := s.business.SaveNativeRouteDraft(r.Context(), s.inboundKey, input)
			if err != nil {
				businessError(w, err)
				return
			}
			code := http.StatusCreated
			if receipt.Replayed {
				code = http.StatusOK
			}
			respond(w, code, receipt)
		})
	mux.HandleFunc("GET /api/v1/workspace/native-inbounds/{id}/route-drafts/{node}",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
				return
			}
			id, err := strconv.ParseUint(r.PathValue("id"), 10, 53)
			if err != nil || id == 0 {
				businessError(w, business.ErrInvalid)
				return
			}
			node := r.PathValue("node")
			installation, found := s.nativeNode(node)
			if !found {
				businessError(w, business.ErrNotFound)
				return
			}
			draft, err := s.business.NativeRouteDraft(r.Context(), s.inboundKey, id, node)
			if err != nil {
				businessError(w, err)
				return
			}
			if draft.InstallationID != installation {
				businessError(w, business.ErrConflict)
				return
			}
			respond(w, http.StatusOK, draft)
		})
}
