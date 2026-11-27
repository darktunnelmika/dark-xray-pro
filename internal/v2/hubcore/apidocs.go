package hubcore

import (
	"net/http"
	"sort"
	"strings"
)

// docMux records every API route it registers, so the panel can list the
// API (GET /api/v2/admin/docs) without a hand-kept document going stale.
type docMux struct {
	*http.ServeMux
	patterns []string
}

func (m *docMux) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	m.patterns = append(m.patterns, pattern)
	m.ServeMux.HandleFunc(pattern, h)
}

type APIRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Access string `json:"access"` // public | viewer | admin | owner | rep
}

func (m *docMux) apiRoutes() []APIRoute {
	out := []APIRoute{}
	for _, p := range m.patterns {
		method, path, ok := strings.Cut(p, " ")
		if !ok || !strings.HasPrefix(path, "/api/") {
			continue
		}
		acc := "viewer"
		switch {
		case path == "/api/v2/auth/login" || path == "/api/v2/auth/logout":
			acc = "public"
		case strings.HasPrefix(path, "/api/tenant/"):
			acc = "tenant" // a connected shop's key (tenantapi.go)
		case strings.HasPrefix(path, "/api/v2/rep/"):
			acc = "rep"
		case isOwnerOnly(path):
			acc = "owner"
		case strings.HasPrefix(path, "/api/v2/auth/"):
			acc = "viewer"
		case method != http.MethodGet:
			acc = "admin"
		}
		out = append(out, APIRoute{Method: method, Path: path, Access: acc})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
