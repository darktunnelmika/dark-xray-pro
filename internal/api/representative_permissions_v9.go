package api

import (
	"net/http"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// The API is authoritative. These permissions affect native PRO business
// actions only, never Node credentials, backup data or Xray accounting.
func requiredRepresentativePermission(r *http.Request) (string, bool) {
	path := r.URL.Path
	method := r.Method
	switch {
	case path == "/api/v1/workspace/products" && (method == "GET" || method == "POST"):
		return "view_clients", true
	case path == "/api/v1/workspace/products/manage" && method == "GET":
		return "view_clients", true
	case path == "/api/v1/workspace/products/policy" && method == "GET":
		return "view_clients", true
	case strings.HasPrefix(path, "/api/v1/workspace/products/") &&
		strings.HasSuffix(path, "/update") && method == "POST":
		return "view_clients", true
	case path == "/api/v1/workspace/products" && method == "POST":
		return "view_clients", true
	case path == "/api/v1/workspace/products/manage" && method == "GET":
		return "view_clients", true
	case path == "/api/v1/workspace/products/policy" && method == "GET":
		return "view_clients", true
	case strings.HasPrefix(path, "/api/v1/workspace/products/") &&
		strings.HasSuffix(path, "/update") && method == "POST":
		return "view_clients", true
	case path == "/api/v1/workspace/products" && method == "GET":
		return "view_clients", true
	case strings.HasPrefix(path, "/api/v1/workspace/products/") && method == "GET":
		return "view_clients", true
	case path == "/api/v1/workspace/me" && method == "GET":
		return "", true
	case path == "/api/v1/workspace/clients":
		if method == "GET" {
			return "view_clients", true
		}
		if method == "POST" {
			return "create_clients", true
		}
	case path == "/api/v1/workspace/clients/bulk/state" && method == "POST":
		return "manage_clients", true
	case strings.HasPrefix(path, "/api/v1/workspace/clients/bulk/extend/") && method == "POST":
		if strings.HasSuffix(path, "/quote") || strings.HasSuffix(path, "/commit") {
			return "renew_clients", true
		}
	case strings.HasPrefix(path, "/api/v1/workspace/clients/"):
		if method == "POST" {
			if strings.Contains(path, "/subscription/") {
				return "manage_clients", true
			}
			if strings.HasSuffix(path, "/edit/quote") || strings.HasSuffix(path, "/edit/commit") {
				return "edit_clients", true
			}
			if strings.HasSuffix(path, "/extend") {
				return "renew_clients", true
			}
			if strings.HasSuffix(path, "/state") {
				return "manage_clients", true
			}
		}
		if method == "GET" {
			if !strings.Contains(path, "/edit/") {
				return "view_clients", true
			}
			if strings.HasSuffix(path, "/edits") {
				return "view_clients", true
			}
		}
	case path == "/api/v1/workspace/client-operations" && method == "GET":
		return "view_finance", true
	case path == "/api/v1/workspace/events" && method == "GET":
		return "view_finance", true
	case strings.HasPrefix(path, "/api/v1/workspace/groups"):
		if method == "GET" {
			return "view_clients", true
		}
		if method == "POST" || method == "PATCH" || method == "DELETE" {
			return "manage_groups", true
		}
	case strings.HasPrefix(path, "/api/v1/workspace/representatives/") &&
		strings.HasSuffix(path, "/activity") && method == "GET":
		return "view_finance", true
	}
	// Unknown workspace routes are owner-only until independently reviewed.
	// Session password rotation and logout are not workspace routes.
	if strings.HasPrefix(path, "/api/v1/workspace/") {
		return "", false
	}
	return "", true
}
func repPermissionAllowed(p business.RepresentativePermissions, key string) bool {
	switch key {
	case "":
		return true
	case "view_clients":
		return p.ViewClients
	case "create_clients":
		return p.CreateClients
	case "renew_clients":
		return p.RenewClients
	case "edit_clients":
		return p.EditClients
	case "manage_clients":
		return p.ManageClients
	case "manage_groups":
		return p.ManageGroups
	case "view_finance":
		return p.ViewFinance
	default:
		return false
	}
}
