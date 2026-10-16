package api

import "net/http"

// BrowserHandler is the public reverse-proxy application surface. A shared
// browser session, not just a hidden menu, protects infrastructure telemetry.
// Local owner-token and the separate TLS Agent handlers keep their contracts;
// the public reverse proxy continues to deny those paths independently.
func (s *Server) BrowserHandler() http.Handler {
	next := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/api/v1/fleet" {
			if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
