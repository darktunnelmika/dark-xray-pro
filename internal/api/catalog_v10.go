package api

import (
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// V10a: metadata-only marketplace, WITHOUT checkout/payment or billable
// customer activation. Owner can create drafts, publish and archive.
// Representatives may only read their own published products.
func (s *Server) attachCatalog(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	catalogScope := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok {
			return "", false
		}
		if session.User.Role == "owner" {
			return "", true
		}
		return session.User.RepresentativeID, true
	}
	mux.HandleFunc("GET /api/v1/workspace/products", func(w http.ResponseWriter, r *http.Request) {
		scope, ok := catalogScope(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		opts := business.CatalogSearch{Limit: 50, Query: q.Get("q"), State: q.Get("state"), RepresentativeID: q.Get("representative_id")}
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
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 100 {
				businessError(w, business.ErrInvalid)
				return
			}
			opts.Limit = n
		}
		if v := q.Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 1_000_000 {
				businessError(w, business.ErrInvalid)
				return
			}
			opts.Offset = n
		}
		page, err := s.business.SearchCatalog(r.Context(), opts, scope)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, page)
	})
	mux.HandleFunc("POST /api/v1/workspace/products", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		var input business.CatalogCreate
		if err := decode(w, r, maxBusinessRequestBytes, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		product, err := s.business.CreateCatalogProduct(r.Context(), input)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusCreated, map[string]any{
			"product": product, "checkout_enabled": false, "applied_to_xray": false,
			"billing_changed": false,
		})
	})
	mux.HandleFunc("GET /api/v1/workspace/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		scope, ok := catalogScope(w, r)
		if !ok {
			return
		}
		product, err := s.business.CatalogProduct(r.Context(), r.PathValue("id"), scope)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, product)
	})
	mux.HandleFunc("POST /api/v1/workspace/products/{id}/update", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		var input business.CatalogUpdate
		if err := decode(w, r, maxBusinessRequestBytes, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if input.ID != r.PathValue("id") {
			businessError(w, business.ErrInvalid)
			return
		}
		product, err := s.business.UpdateCatalogProduct(r.Context(), input)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{
			"product": product, "checkout_enabled": false, "applied_to_xray": false,
			"billing_changed": false,
		})
	})
	mux.HandleFunc("GET /api/v1/workspace/products/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		q := r.URL.Query()
		for key, vals := range q {
			if len(vals) != 1 {
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
		if err != nil || limit > 100 {
			businessError(w, business.ErrInvalid)
			return
		}
		rows, err := s.business.CatalogAudit(r.Context(), r.PathValue("id"), limit, offset)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{
			"events": rows, "limit": limit, "offset": offset, "audit": "append_only",
		})
	})
}
