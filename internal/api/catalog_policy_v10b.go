package api

import (
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func (s *Server) catalogMutationSession(w http.ResponseWriter, r *http.Request) (business.LoginSession, bool) {
	session, ok := s.authorizeBrowser(w, r, true, false)
	if !ok {
		return business.LoginSession{}, false
	}
	if session.User.Role == "owner" {
		return session, true
	}
	// Preflight denial is an HTTP 403, but the policy is independently
	// re-read INSIDE the SQLite write transaction to block concurrent revoke.
	policy, err := s.business.CatalogPolicy(r.Context(), session.User.RepresentativeID)
	if err != nil || !policy.ManageProducts {
		respond(w, http.StatusForbidden, map[string]string{"error": "catalog_management_not_granted"})
		return business.LoginSession{}, false
	}
	return session, true
}

// Owner catalog controls are separate from V9 general role permissions:
// V10a rollback denies representative catalog POST rather than elevating it.
func (s *Server) attachCatalogPolicy(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/products/policy",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, false, false)
			if !ok {
				return
			}
			if session.User.Role == "owner" {
				businessError(w, business.ErrInvalid)
				return
			}
			policy, err := s.business.CatalogPolicy(r.Context(), session.User.RepresentativeID)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, policy)
		})
	mux.HandleFunc("GET /api/v1/workspace/products/manage",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, false, false)
			if !ok {
				return
			}
			if session.User.Role == "owner" {
				businessError(w, business.ErrInvalid)
				return
			}
			q := r.URL.Query()
			options := business.CatalogSearch{Limit: 50, Query: q.Get("q"), State: q.Get("state"), RepresentativeID: q.Get("representative_id")}
			for key, values := range q {
				if len(values) != 1 {
					businessError(w, business.ErrInvalid)
					return
				}
				switch key {
				case "limit", "offset", "q", "state", "representative_id":
				default:
					businessError(w, business.ErrInvalid)
					return
				}
			}
			if value := q.Get("limit"); value != "" {
				n, e := strconv.Atoi(value)
				if e != nil || n < 1 || n > 100 {
					businessError(w, business.ErrInvalid)
					return
				}
				options.Limit = n
			}
			if value := q.Get("offset"); value != "" {
				n, e := strconv.Atoi(value)
				if e != nil || n < 0 || n > 1_000_000 {
					businessError(w, business.ErrInvalid)
					return
				}
				options.Offset = n
			}
			page, err := s.business.SearchCatalogManaged(r.Context(), options, session.User.RepresentativeID)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, page)
		})
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/catalog-policy",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
				return
			}
			policy, err := s.business.CatalogPolicy(r.Context(), r.PathValue("id"))
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, policy)
		})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/catalog-policy",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
				return
			}
			// Every policy bound must be supplied explicitly, preventing accidental
			// zeroing of permissions due to omitted JSON fields.
			var raw struct {
				EventID          string `json:"event_id"`
				RepresentativeID string `json:"representative_id"`
				ExpectedRevision *int64 `json:"expected_revision"`
				ManageProducts   *bool  `json:"manage_products"`
				CanPublish       *bool  `json:"can_publish"`
				AllowVolume      *bool  `json:"allow_volume"`
				AllowUnlimited   *bool  `json:"allow_unlimited"`
				MinPriceToman    *int64 `json:"min_price_toman"`
				MaxPriceToman    *int64 `json:"max_price_toman"`
				MaxDurationDays  *int64 `json:"max_duration_days"`
				MaxIPCount       *int64 `json:"max_ip_count"`
				MaxVolumeBytes   *int64 `json:"max_volume_bytes"`
				Reason           string `json:"reason"`
			}
			if err := decode(w, r, maxBusinessRequestBytes, &raw); err != nil {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			if raw.ExpectedRevision == nil || raw.ManageProducts == nil || raw.CanPublish == nil ||
				raw.AllowVolume == nil || raw.AllowUnlimited == nil || raw.MinPriceToman == nil ||
				raw.MaxPriceToman == nil || raw.MaxDurationDays == nil || raw.MaxIPCount == nil ||
				raw.MaxVolumeBytes == nil {
				businessError(w, business.ErrInvalid)
				return
			}
			if raw.RepresentativeID != r.PathValue("id") {
				businessError(w, business.ErrInvalid)
				return
			}
			input := business.CatalogPolicyChange{
				CatalogPolicy: business.CatalogPolicy{
					RepresentativeID: raw.RepresentativeID, ManageProducts: *raw.ManageProducts,
					CanPublish: *raw.CanPublish, AllowVolume: *raw.AllowVolume,
					AllowUnlimited: *raw.AllowUnlimited, MinPriceToman: *raw.MinPriceToman,
					MaxPriceToman: *raw.MaxPriceToman, MaxDurationDays: *raw.MaxDurationDays,
					MaxIPCount: *raw.MaxIPCount, MaxVolumeBytes: *raw.MaxVolumeBytes,
				},
				EventID: raw.EventID, ExpectedRevision: *raw.ExpectedRevision, Reason: raw.Reason,
			}
			receipt, err := s.business.UpdateCatalogPolicy(r.Context(), input)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, receipt)
		})
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/catalog-policy/events",
		func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
				return
			}
			q := r.URL.Query()
			for key, values := range q {
				if len(values) != 1 {
					businessError(w, business.ErrInvalid)
					return
				}
				switch key {
				case "limit", "offset":
				default:
					businessError(w, business.ErrInvalid)
					return
				}
			}
			limit, offset, err := pageParams(r)
			if err != nil || limit > 50 {
				businessError(w, business.ErrInvalid)
				return
			}
			events, err := s.business.CatalogPolicyEvents(r.Context(), r.PathValue("id"), limit, offset)
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, map[string]any{
				"events": events, "limit": limit, "offset": offset, "audit": "append_only",
			})
		})
}
