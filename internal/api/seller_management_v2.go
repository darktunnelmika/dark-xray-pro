package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Only these reviewed route shapes participate in the new permission. Tenant
// ownership is checked separately by the handler and authoritative Store.
func sellerRepresentativePermissionV2(r *http.Request) (string, bool) {
	const prefix = "/api/v1/workspace/representatives/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if len(parts) == 2 && parts[1] == "seller-permission" && r.Method == http.MethodGet {
		return "", true
	}
	if len(parts) < 2 || parts[1] != "sellers" {
		return "", false
	}
	// The handler additionally requires manage_sellers and the same tenant;
	// the Store rechecks both grants within the order's write transaction.
	if r.Method == http.MethodPost && len(parts) == 4 && parts[0] != "" && parts[2] != "" && parts[3] == "clients" {
		return "create_clients", true
	}
	if r.Method == http.MethodPost && len(parts) == 6 && parts[0] != "" && parts[2] != "" && parts[3] == "clients" && parts[4] != "" && parts[5] == "extend" {
		return "renew_clients", true
	}
	if len(parts) == 2 && (r.Method == http.MethodGet || r.Method == http.MethodPost) ||
		len(parts) == 3 && r.Method == http.MethodGet ||
		len(parts) == 4 && r.Method == http.MethodPost && (parts[3] == "quota" || parts[3] == "state") {
		return "manage_sellers", true
	}
	return "", false
}

func (s *Server) authorizeSellerBrowserV2(w http.ResponseWriter, r *http.Request, mutate, permissionRead bool) (business.LoginSession, bool) {
	session, ok := s.authorizeBrowser(w, r, mutate, false)
	if !ok {
		return session, false
	}
	if session.User.Role == "owner" {
		return session, true
	}
	if session.User.Role != "representative" || session.User.RepresentativeID != r.PathValue("id") ||
		!permissionRead && !session.User.Permissions.ManageSellers {
		respond(w, http.StatusForbidden, map[string]string{"error": "seller_management_permission_denied"})
		return business.LoginSession{}, false
	}
	return session, true
}

func sellerQuotaErrorV2(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, business.ErrUnauthenticated):
		respond(w, http.StatusUnauthorized, map[string]string{"error": "session_required"})
	case errors.Is(err, business.ErrSellerPermissionDenied):
		respond(w, http.StatusForbidden, map[string]string{"error": "seller_management_permission_denied"})
	default:
		businessError(w, err)
	}
}

// Called only by attachSellerQuotaV2 after the explicit feature gate.
func (s *Server) attachSellerManagementV2(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/seller-permission", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeSellerBrowserV2(w, r, false, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		permission, err := s.business.SellerPermissionV2(r.Context(), session.Token, r.PathValue("id"))
		if err != nil {
			sellerQuotaErrorV2(w, err)
			return
		}
		respond(w, http.StatusOK, permission)
	})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/seller-permission", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		var raw struct {
			EventID          string `json:"event_id"`
			RepresentativeID string `json:"representative_id"`
			ExpectedRevision *int64 `json:"expected_revision"`
			ManageSellers    *bool  `json:"manage_sellers"`
			Reason           string `json:"reason"`
		}
		if decode(w, r, maxBusinessRequestBytes, &raw) != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if raw.RepresentativeID != r.PathValue("id") || raw.ExpectedRevision == nil || raw.ManageSellers == nil {
			businessError(w, business.ErrInvalid)
			return
		}
		result, err := s.business.UpdateSellerPermissionV2(r.Context(), session.Token, business.SellerPermissionChangeV2{
			EventID: raw.EventID, RepresentativeID: raw.RepresentativeID, ExpectedRevision: *raw.ExpectedRevision,
			ManageSellers: *raw.ManageSellers, Reason: raw.Reason,
		})
		if err != nil {
			sellerQuotaErrorV2(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/sellers/{seller}/state", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeSellerBrowserV2(w, r, true, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		var raw struct {
			EventID          string `json:"event_id"`
			RepresentativeID string `json:"representative_id"`
			SellerID         string `json:"seller_id"`
			ExpectedRevision *int64 `json:"expected_revision"`
			Enabled          *bool  `json:"enabled"`
			Reason           string `json:"reason"`
		}
		if decode(w, r, maxBusinessRequestBytes, &raw) != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if raw.RepresentativeID != r.PathValue("id") || raw.SellerID != r.PathValue("seller") || raw.ExpectedRevision == nil || raw.Enabled == nil {
			businessError(w, business.ErrInvalid)
			return
		}
		result, err := s.business.UpdateSellerStateV2(r.Context(), session.Token, business.SellerStateChangeV2{
			EventID: raw.EventID, RepresentativeID: raw.RepresentativeID, SellerID: raw.SellerID,
			ExpectedRevision: *raw.ExpectedRevision, Enabled: *raw.Enabled, Reason: raw.Reason,
		})
		if err != nil {
			sellerQuotaErrorV2(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
}
