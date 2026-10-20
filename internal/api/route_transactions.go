package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// This operator-only startup switch claims a never-delivered, empty loopback
// Node persistently. Browser requests cannot enable it or select another Node.
func (s *Server) EnableRouteTransactions(node, installation, binarySHA string) error {
	if s.business == nil || len(s.inboundKey) != 32 || node == "" || s.routeNode != "" || node == s.meteredNode {
		return errors.New("isolated route transaction prerequisites missing")
	}
	current, ok := s.nativeNode(node)
	if !ok || current != installation {
		return errors.New("route transaction installation is not paired")
	}
	if e := s.business.ClaimRouteChannel(context.Background(), node, installation, binarySHA); e != nil {
		return e
	}
	s.routeNode = node
	s.routeInstall = installation
	return nil
}
func (s *Server) routeScope(node, installation string) bool {
	current, ok := s.nativeNode(node)
	return s.routeNode != "" && node == s.routeNode && installation == s.routeInstall && ok && current == installation
}
func (s *Server) attachRouteTransactions(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/route-transactions", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		// Discovery is read-only; it cannot claim a Node or enable an Agent.
		out := map[string]any{"enabled": false, "scope": "empty_loopback_lab"}
		if s.routeScope(s.routeNode, s.routeInstall) {
			out["enabled"], out["node_id"], out["installation_id"] = true, s.routeNode, s.routeInstall
		}
		respond(w, http.StatusOK, out)
	})
	if s.routeNode == "" {
		return
	}
	base := "/api/v1/workspace/native-nodes/{node}/route-transactions"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		if !s.routeScope(r.PathValue("node"), s.routeInstall) {
			businessError(w, business.ErrConflict)
			return
		}
		state, e := s.business.RouteControlChannel(r.Context(), s.inboundKey, s.routeNode)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, state)
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		if !s.routeScope(r.PathValue("node"), s.routeInstall) {
			businessError(w, business.ErrConflict)
			return
		}
		var in business.RouteCommandRequest
		if decode(w, r, 65536, &in) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		v, e := s.business.QueueRouteCommand(r.Context(), s.inboundKey, s.routeNode, in)
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, http.StatusAccepted, v)
	})
	mux.HandleFunc("POST "+base+"/{id}/abandon", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		if !s.routeScope(r.PathValue("node"), s.routeInstall) {
			businessError(w, business.ErrConflict)
			return
		}
		var in struct {
			ConfirmUnknown bool `json:"confirm_execution_may_have_occurred"`
		}
		if decode(w, r, 256, &in) != nil || !in.ConfirmUnknown {
			businessError(w, business.ErrInvalid)
			return
		}
		if e := s.business.AbandonRouteCommand(r.Context(), s.routeNode, r.PathValue("id")); e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, map[string]any{"state": "abandoned_unknown", "process_stopped": false, "requires_fresh_observation": true})
	})
}

// Called exclusively from the dedicated TLS 1.3 Agent allowlist.
func (s *Server) handleRouteTransactionAgent(w http.ResponseWriter, r *http.Request) {
	if s.routeNode == "" {
		http.NotFound(w, r)
		return
	}
	token := bearer(r)
	switch r.URL.Path {
	case "/api/v1/agent/route-transactions/poll":
		var p mx.TransactionPoll
		if decode(w, r, 16384, &p) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.routeScope(p.NodeID, p.InstallationID) {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok := s.registry.Authenticate(p.NodeID, p.InstallationID, token); !ok || !mx.VerifyTransactionPoll([]byte(token), p) {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		d, found, e := s.business.PollRouteCommand(r.Context(), s.inboundKey, []byte(token), p)
		if e != nil {
			businessError(w, e)
			return
		}
		if !found {
			w.WriteHeader(204)
			return
		}
		respond(w, 200, d)
	case "/api/v1/agent/route-transactions/receipt":
		var ack mx.TransactionReceipt
		if decode(w, r, 16384, &ack) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.routeScope(ack.NodeID, ack.InstallationID) {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok := s.registry.Authenticate(ack.NodeID, ack.InstallationID, token); !ok {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if e := s.business.AcceptRouteReceipt(r.Context(), s.inboundKey, []byte(token), ack); e != nil {
			businessError(w, e)
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
