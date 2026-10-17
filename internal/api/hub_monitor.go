package api

import (
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/hubmonitor"
)

// An explicitly wired NEW PRO local monitor, never remote Agent or legacy HUB.
func (s *Server) EnableHubMonitor(m *hubmonitor.Monitor) error {
	if m == nil {
		return errors.New("independent local PRO HUB monitor required")
	}
	s.hubMonitor = m
	return nil
}

// A private, owner-session-only, read-only endpoint on the unified UI.
// No owner Bearer fallback; representatives must never learn host internals.
func (s *Server) attachHubMonitor(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/hub/metrics", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		if s.hubMonitor == nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "hub_monitor_unavailable"})
			return
		}
		respond(w, http.StatusOK, s.hubMonitor.Snapshot())
	})
}
