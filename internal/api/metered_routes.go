package api

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
)

// This separate operator-only startup switch claims a never-delivered metered loopback
// Node persistently. Browser requests cannot enable it or select another Node.
func (s *Server) EnableMeteredRouteTransactions(node, installation, binarySHA string) error {
	return s.EnableMeteredRouteNodes([]business.MeteredRouteNode{{NodeID: node, InstallationID: installation, BinarySHA: binarySHA}})
}

// EnableMeteredRouteNodes is a startup-only operator allowlist. Persist all
// claims in one transaction; neither browser nor Agent can add a member.
func (s *Server) EnableMeteredRouteNodes(nodes []business.MeteredRouteNode) error {
	return s.enableMeteredRouteNodes(nodes, false)
}

// EnableMeteredRouteNodesForPlanPromotion reserves one explicitly approved,
// never-delivered LAB channel before a saved plan has any targets. Candidate
// generation and Stage/Apply still require exact promoted targets and bindings.
func (s *Server) EnableMeteredRouteNodesForPlanPromotion(nodes []business.MeteredRouteNode) error {
	if len(nodes) != 1 {
		return errors.New("plan promotion requires exactly one isolated metered Node")
	}
	return s.enableMeteredRouteNodes(nodes, true)
}

func (s *Server) enableMeteredRouteNodes(nodes []business.MeteredRouteNode, promotion bool) error {
	if s.business == nil || s.business.UsageLedger() == nil || len(s.inboundKey) != 32 || len(nodes) == 0 || len(nodes) > 64 || s.meteredRouteNodes != nil {
		return errors.New("isolated route transaction prerequisites missing")
	}
	allowlist := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if s.replacements != nil {
			for _, op := range s.replacements.List() {
				if op.Phase != replacement.Cancelled && (op.OldNodeID == node.NodeID || op.NewNodeID == node.NodeID) {
					return errors.New("metered Node is reserved by legacy preflight")
				}
			}
		}
		current, ok := s.nativeNode(node.NodeID)
		if node.NodeID == s.meteredNode || !ok || current != node.InstallationID || allowlist[node.NodeID] != "" {
			return errors.New("route transaction installation is not paired or scope is duplicated")
		}
		allowlist[node.NodeID] = node.InstallationID
	}
	var e error
	if promotion {
		e = s.business.ReserveMeteredRouteChannelsForPlanPromotion(context.Background(), nodes)
	} else {
		e = s.business.ClaimMeteredRouteChannels(context.Background(), nodes)
	}
	if e != nil {
		return e
	}
	s.meteredRouteNodes = allowlist
	s.inboundPlanPromotionEnabled = promotion
	// Keep the former discovery fields for single-node clients.
	s.meteredRouteNode = nodes[0].NodeID
	s.meteredRouteInstall = nodes[0].InstallationID
	return nil
}
func (s *Server) meteredRouteScope(node, installation string) bool {
	current, ok := s.nativeNode(node)
	return installation != "" && s.meteredRouteNodes[node] == installation && ok && current == installation
}
func (s *Server) attachMeteredRouteTransactions(mux *http.ServeMux) {
	s.attachMeteredHandovers(mux)
	s.attachMeteredHealth(mux)
	mux.HandleFunc("GET /api/v1/workspace/metered-route-transactions", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		// Discovery is read-only; it cannot claim a Node or enable an Agent.
		out := map[string]any{"enabled": false, "scope": "metered_tcp_loopback_lab"}
		out["health_enabled"] = s.healthEndpoint != nil
		out["health_rounds_enabled"] = s.healthRoundsEnabled
		out["initial_apply_health_enabled"] = s.initialApplyHealthEnabled
		out["lab_subscriptions_enabled"] = s.labSubscriptionsEnabled
		out["consumer_manifests_enabled"] = s.consumerManifestsEnabled
		nodes := make([]map[string]string, 0, len(s.meteredRouteNodes))
		ids := make([]string, 0, len(s.meteredRouteNodes))
		for id := range s.meteredRouteNodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if s.meteredRouteScope(id, s.meteredRouteNodes[id]) {
				nodes = append(nodes, map[string]string{"node_id": id, "installation_id": s.meteredRouteNodes[id]})
			}
		}
		out["nodes"] = nodes
		if len(nodes) > 0 {
			out["enabled"], out["node_id"], out["installation_id"] = true, nodes[0]["node_id"], nodes[0]["installation_id"]
		}
		respond(w, http.StatusOK, out)
	})
	if len(s.meteredRouteNodes) == 0 {
		return
	}
	base := "/api/v1/workspace/native-nodes/{node}/metered-route-transactions"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		node := r.PathValue("node")
		if !s.meteredRouteScope(node, s.meteredRouteNodes[node]) {
			businessError(w, business.ErrConflict)
			return
		}
		state, e := s.business.MeteredRouteControlChannel(r.Context(), s.inboundKey, node)
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
		node := r.PathValue("node")
		if !s.meteredRouteScope(node, s.meteredRouteNodes[node]) {
			businessError(w, business.ErrConflict)
			return
		}
		var in business.MeteredRouteCommandRequest
		if decode(w, r, 65536, &in) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		v, e := s.business.QueueMeteredRouteCommand(r.Context(), s.inboundKey, node, in)
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
		node := r.PathValue("node")
		if !s.meteredRouteScope(node, s.meteredRouteNodes[node]) {
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
		if e := s.business.AbandonMeteredRouteCommand(r.Context(), node, r.PathValue("id")); e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, map[string]any{"state": "abandoned_unknown", "process_stopped": false, "requires_fresh_observation": true})
	})
}

// Called exclusively from the dedicated TLS 1.3 Agent allowlist.
func (s *Server) handleMeteredRouteTransactionAgent(w http.ResponseWriter, r *http.Request) {
	if len(s.meteredRouteNodes) == 0 {
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
