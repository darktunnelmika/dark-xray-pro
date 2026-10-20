package api

import (
	"context"
	"net/http"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
)

func (s *Server) attachNodeDependencies(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/nodes/{id}/dependencies", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		node, err := s.registry.NameState(r.PathValue("id"))
		if err != nil {
			respond(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
			return
		}
		if s.business == nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "node_dependencies_unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		snapshot, err := s.business.InspectNodeDependencies(ctx, s.inboundKey, node.Node.NodeID, node.Node.InstallationID)
		if err != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "node_dependencies_unavailable"})
			return
		}
		// Separate journal: this is explicitly NOT a cross-store atomic snapshot.
		var total, pending *int64
		if s.replacements != nil {
			var n, p int64
			for _, op := range s.replacements.List() {
				if op.OldNodeID == node.Node.NodeID || op.NewNodeID == node.Node.NodeID {
					n++
					if op.Phase != replacement.Cancelled {
						p++
					}
				}
			}
			total, pending = &n, &p
		} else {
			snapshot.UnknownReasons["replacement_operations"] = "replacement_journal_disabled"
			snapshot.UnknownReasons["noncancelled_replacements"] = "replacement_journal_disabled"
		}
		snapshot.Counts["replacement_operations"] = total
		snapshot.Counts["noncancelled_replacements"] = pending
		respond(w, http.StatusOK, map[string]any{
			"node_id": node.Node.NodeID, "installation_id": node.Node.InstallationID,
			"observed_at": time.Now().UTC(), "coverage": "native_sqlite_and_replacement_journal_only",
			"atomic_across_stores": false, "can_retire": false, "can_replace": false,
			"counts": snapshot.Counts, "unknown_reasons": snapshot.UnknownReasons,
			"lifecycle_blocker": "atomic_node_lifecycle_not_implemented",
		})
	})
}
