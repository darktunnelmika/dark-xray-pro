package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestDisplaySessionSnapshotCannotAuthorizeMutationsOrOtherRoutes(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodGet, "/api/v1/session/status", true},
		{http.MethodGet, "/api/v1/workspace/nodes", true},
		{http.MethodPost, "/api/v1/session/status", false},
		{http.MethodPost, "/api/v1/workspace/nodes", false},
		{http.MethodDelete, "/api/v1/workspace/nodes", false},
		{http.MethodHead, "/api/v1/workspace/nodes", false},
		{http.MethodGet, "/api/v1/workspace/nodes/one", false},
		{http.MethodPost, "/api/v1/workspace/nodes/invites", false},
		{http.MethodGet, "/api/v1/workspace/clients", false},
		{http.MethodGet, "/api/v1/workspace/nodes/", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			r := &http.Request{Method: tc.method, URL: &url.URL{Path: tc.path}}
			if got := displaySessionSnapshot(r); got != tc.allowed {
				t.Fatal("unexpected display authority route", got)
			}
		})
	}
}
