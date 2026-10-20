package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// This separate operator-only startup switch claims a never-delivered metered loopback
// Node persistently. Browser requests cannot enable it or select another Node.
func (s *Server) EnableMeteredRouteTransactions(node, installation, binarySHA string) error {
	if s.business == nil || s.business.UsageLedger() == nil || len(s.inboundKey) != 32 || node == "" || s.meteredRouteNode != "" || node == s.meteredNode {
		return errors.New("isolated route transaction prerequisites missing")
	}
	current, ok := s.nativeNode(node)
	if !ok || current != installation {
		return errors.New("route transaction installation is not paired")
	}
	if e := s.business.ClaimMeteredRouteChannel(context.Background(), node, installation, binarySHA); e != nil {
		return e
	}
	s.meteredRouteNode = node
	s.meteredRouteInstall = installation
	return nil
}
func (s *Server) meteredRouteScope(node, installation string) bool {
	current, ok := s.nativeNode(node)
	return s.meteredRouteNode != "" && node == s.meteredRouteNode && installation == s.meteredRouteInstall && ok && current == installation
}
func (s *Server) attachMeteredRouteTransactions(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/metered-route-transactions", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		// Discovery is read-only; it cannot claim a Node or enable an Agent.
		out := map[string]any{"enabled": false, "scope": "metered_tcp_loopback_lab"}
		if s.meteredRouteScope(s.meteredRouteNode, s.meteredRouteInstall) {
			out["enabled"], out["node_id"], out["installation_id"] = true, s.meteredRouteNode, s.meteredRouteInstall
		}
		respond(w, http.StatusOK, out)
	})
	if s.meteredRouteNode == "" {
		return
	}
	base := "/api/v1/workspace/native-nodes/{node}/metered-route-transactions"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		if !s.meteredRouteScope(r.PathValue("node"), s.meteredRouteInstall) {
			businessError(w, business.ErrConflict)
			return
		}
		state, e := s.business.MeteredRouteControlChannel(r.Context(), s.inboundKey, s.meteredRouteNode)
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
		if !s.meteredRouteScope(r.PathValue("node"), s.meteredRouteInstall) {
			businessError(w, business.ErrConflict)
			return
		}
		var in business.MeteredRouteCommandRequest
		if decode(w, r, 65536, &in) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		v, e := s.business.QueueMeteredRouteCommand(r.Context(), s.inboundKey, s.meteredRouteNode, in)
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
		if !s.meteredRouteScope(r.PathValue("node"), s.meteredRouteInstall) {
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
		if e := s.business.AbandonMeteredRouteCommand(r.Context(), s.meteredRouteNode, r.PathValue("id")); e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, map[string]any{"state": "abandoned_unknown", "process_stopped": false, "requires_fresh_observation": true})
	})
}

// Called exclusively from the dedicated TLS 1.3 Agent allowlist.
func (s *Server) handleMeteredRouteTransactionAgent(w http.ResponseWriter, r *http.Request) {
	if s.meteredRouteNode == "" {
		http.NotFound(w, r)
		return
	}
	token := bearer(r)
	switch r.URL.Path {
	case "/api/v1/agent/metered-route-transactions/poll":
		var p mx.TransactionPoll
		if decode(w, r, 16384, &p) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.meteredRouteScope(p.NodeID, p.InstallationID) {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok := s.registry.Authenticate(p.NodeID, p.InstallationID, token); !ok || !mx.VerifyMeteredTransactionPoll([]byte(token), p) {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		d, found, e := s.business.PollMeteredRouteCommand(r.Context(), s.inboundKey, []byte(token), p)
		if e != nil {
			businessError(w, e)
			return
		}
		if !found {
			w.WriteHeader(204)
			return
		}
		respond(w, 200, d)
	case "/api/v1/agent/metered-route-transactions/receipt":
		var ack mx.TransactionReceipt
		if decode(w, r, 16384, &ack) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.meteredRouteScope(ack.NodeID, ack.InstallationID) {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok := s.registry.Authenticate(ack.NodeID, ack.InstallationID, token); !ok {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if e := s.business.AcceptMeteredRouteReceipt(r.Context(), s.inboundKey, []byte(token), ack); e != nil {
			businessError(w, e)
			return
		}
		w.WriteHeader(204)
	case "/api/v1/agent/metered-route-transactions/check", "/api/v1/agent/metered-route-transactions/register":
		var in mx.MeteredAuthorityRequest
		if decode(w, r, 16384, &in) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		c := in.Command
		if !s.meteredRouteScope(c.NodeID, c.InstallationID) {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok := s.registry.Authenticate(c.NodeID, c.InstallationID, token); !ok {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		registration := r.URL.Path == "/api/v1/agent/metered-route-transactions/register"
		if registration != (in.Birth != nil) {
			businessError(w, business.ErrInvalid)
			return
		}
		epoch, e := s.business.MeteredRouteAuthority(r.Context(), s.inboundKey, []byte(token), in)
		if e != nil {
			businessError(w, e)
			return
		}
		if registration {
			respond(w, http.StatusCreated, epoch)
		} else {
			w.WriteHeader(204)
		}
	default:
		http.NotFound(w, r)
	}
}
