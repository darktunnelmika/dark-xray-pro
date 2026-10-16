package api

import (
	"context"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageingest"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func (s *Server) nativeUsageHandler() http.Handler {
	if s.business == nil || s.business.UsageLedger() == nil {
		return nil
	}
	handler, err := usageingest.New(s.business.UsageLedger(), func(ctx context.Context, node, installation, token string) (usageledger.Principal, bool) {
		if ctx.Err() != nil {
			return usageledger.Principal{}, false
		}
		if _, ok := s.registry.Authenticate(node, installation, token); !ok {
			return usageledger.Principal{}, false
		}
		return usageledger.Principal{NodeID: node, InstallationID: installation}, true
	})
	if err != nil {
		return nil
	}
	return handler
}
func (s *Server) attachUsageRead(mux *http.ServeMux) {
	if s.business == nil || s.business.UsageLedger() == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/clients/{id}/usage", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok {
			return
		}
		c, err := s.business.Client(r.Context(), r.PathValue("id"))
		if err != nil {
			businessError(w, err)
			return
		}
		if session.User.Role != "owner" && c.RepresentativeID != session.User.RepresentativeID {
			respond(w, http.StatusForbidden, map[string]string{"error": "representative_scope_denied"})
			return
		}
		account, err := s.business.ClientUsage(r.Context(), c.ID)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{"usage": account, "scope": "committed_user_counters", "live_collection_enabled": false, "distributed_hard_limit_verified": false})
	})
}
