package api

import (
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodehistory"
	"net/http"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

// Owner-only, session-authenticated Node enrollment. Only NEW PRO Node identities
// may be paired. This does not execute an installer, apply Xray or move clients.
type ownerNode struct {
	NodeID                string     `json:"node_id"`
	Name                  string     `json:"name"`
	PairedAt              time.Time  `json:"paired_at"`
	Status                string     `json:"status"`
	MetricsCurrent        bool       `json:"metrics_current"`
	LastSeen              *time.Time `json:"last_seen"`
	TransportState        string     `json:"transport_state"`
	MetricsState          string     `json:"metrics_state"`
	CoreState             string     `json:"core_state"`
	AccountingState       string     `json:"accounting_state"`
	ExecutionCurrent      bool       `json:"execution_current"`
	ExecutionFreshForMS   int64      `json:"execution_fresh_for_ms"`
	ExecutionReportedAt   *time.Time `json:"execution_reported_at"`
	ExecutionInstallation string     `json:"-"`
}

// EnableNodeHistory is an explicit isolated-PRO-only opt-in. Nothing is
// written into the business database, and no Node can be enrolled implicitly.
func (s *Server) EnableNodeHistory(h *nodehistory.Store) error {
	if h == nil {
		return errors.New("independent history store required")
	}
	s.nodeHistory = h
	return nil
}

func (s *Server) attachNodeManager(mux *http.ServeMux) {
	s.attachNodeNames(mux)
	s.attachNodeDependencies(mux)
	s.attachNodeReplacements(mux)
	mux.HandleFunc("GET /api/v1/workspace/nodes", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		fleet := s.snapshot()
		current := make(map[string]ownerNode, len(fleet.Nodes))
		for _, node := range fleet.Nodes {
			transport, metrics := "offline", "unavailable"
			if node.Status == "online" {
				transport, metrics = "connected", "stale"
				if node.MetricsCurrent {
					metrics = "current"
				}
			}
			current[node.NodeID] = ownerNode{NodeID: node.NodeID, Name: node.Name, Status: node.Status, MetricsCurrent: node.MetricsCurrent, LastSeen: node.LastSeen, TransportState: transport, MetricsState: metrics}
		}
		execution, executionAvailable := map[string]ownerNode{}, false
		if s.business != nil && len(s.inboundKey) == 32 {
			if channels, err := s.business.ListInboundExecutionNodeChannels(r.Context(), s.inboundKey); err == nil {
				executionAvailable = true
				now := time.Now()
				for _, channel := range channels {
					status := observedNodeChannels(channel, now)
					execution[channel.NodeID] = status
				}
			}
		}
		rows := make([]ownerNode, 0)
		for _, registration := range s.registry.List() {
			row := ownerNode{
				NodeID: registration.NodeID, Name: registration.Name, PairedAt: registration.PairedAt,
				Status: "offline", TransportState: "offline", MetricsState: "unavailable",
				CoreState: "unknown", AccountingState: "unknown",
			}
			if sample, found := current[registration.NodeID]; found {
				row.Status = sample.Status
				row.MetricsCurrent = sample.MetricsCurrent
				row.LastSeen = sample.LastSeen
				row.TransportState = sample.TransportState
				row.MetricsState = sample.MetricsState
			}
			if channel, found := execution[registration.NodeID]; found && channel.ExecutionInstallation == registration.InstallationID {
				row.CoreState = channel.CoreState
				row.AccountingState = channel.AccountingState
				row.ExecutionCurrent = channel.ExecutionCurrent
				row.ExecutionFreshForMS = channel.ExecutionFreshForMS
				row.ExecutionReportedAt = channel.ExecutionReportedAt
			} else if executionAvailable {
				row.CoreState, row.AccountingState = "not_configured", "not_configured"
			}
			rows = append(rows, row)
		}
		respond(w, http.StatusOK, map[string]any{
			"nodes": rows, "mode": "new_pro_lab_only",
			"installer_available":        s.nodeBundle != nil,
			"agent_transport_scope":      s.nodeTransportScope,
			"execution_status_available": executionAvailable,
		})
	})
	// Explicit owner-session-only download. Never serves old Nodes, Xray,
	// customer credentials or a generated Pair Code in the ZIP package.
	mux.HandleFunc("GET /api/v1/workspace/nodes/installer-bundle", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		if s.nodeBundle == nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "installer_bundle_unavailable"})
			return
		}
		if err := s.nodeBundle.Download(w); err != nil {
			// No secret is ever returned, and we must never substitute a
			// silently different Agent binary.
			if w.Header().Get("Content-Type") == "" {
				respond(w, http.StatusServiceUnavailable, map[string]string{"error": "installer_bundle_unavailable"})
			}
		}
	})
	mux.HandleFunc("GET /api/v1/workspace/nodes/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id := r.PathValue("id")
		var installation string
		for _, registration := range s.registry.List() {
			if registration.NodeID == id {
				installation = registration.InstallationID
				break
			}
		}
		if installation == "" {
			respond(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
			return
		}
		if s.nodeHistory == nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "node_history_unavailable"})
			return
		}
		window := r.URL.Query().Get("window")
		points, err := s.nodeHistory.Query(r.Context(), id, installation, window)
		if errors.Is(err, nodehistory.ErrWindow) || errors.Is(err, nodehistory.ErrInvalid) {
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_history_window"})
			return
		}
		if err != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "node_history_unavailable"})
			return
		}
		bucket := 60
		if window == "24h" {
			bucket = 300
		}
		respond(w, http.StatusOK, map[string]any{"node_id": id, "window": window, "bucket_seconds": bucket, "scope": "host_interface_not_billable", "points": points})
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/invites", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		var input struct {
			NodeID string `json:"node_id"`
			Name   string `json:"name"`
		}
		if err := decode(w, r, maxEnrollmentBytes, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if input.NodeID == localHubNodeID {
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "reserved_hub_identity"})
			return
		}
		ticket, err := s.registry.Issue(input.NodeID, input.Name)
		switch {
		case err == nil:
			// Pair Code is returned ONLY by this POST and is never stored in plaintext.
			respond(w, http.StatusCreated, ticket)
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_node"})
		case errors.Is(err, noderegistry.ErrExists), errors.Is(err, noderegistry.ErrPending), errors.Is(err, noderegistry.ErrTooMany):
			respond(w, http.StatusConflict, map[string]string{"error": "node_or_pairing_conflict"})
		default:
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "registry_unavailable"})
		}
	})
	mux.HandleFunc("POST /api/v1/workspace/nodes/invites/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		// Explicit CSRF-protected cancellation touches only an unclaimed invitation,
		// never a Node, its Agent identity, clients, accounting or desired inbounds.
		if err := decode(w, r, 128, &struct{}{}); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		err := s.registry.CancelPending(r.PathValue("id"))
		switch {
		case err == nil:
			respond(w, http.StatusOK, map[string]bool{"cancelled": true})
		case errors.Is(err, noderegistry.ErrInvalid):
			respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_node"})
		case errors.Is(err, noderegistry.ErrInvite):
			respond(w, http.StatusNotFound, map[string]string{"error": "pending_invite_not_found"})
		default:
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "registry_unavailable"})
		}
	})
}
