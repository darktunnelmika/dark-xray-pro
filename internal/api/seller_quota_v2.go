package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// A separate, explicit LAB gate. The entrypoint never calls this during
// ordinary startup: existing customer billing, bots and DB schema stay unchanged.
func (s *Server) EnableSellerQuotaV2Lab(ctx context.Context) error {
	if s.business == nil {
		return errors.New("business store is required")
	}
	if s.sellerQuotaV2Enabled {
		return errors.New("seller quota API already configured")
	}
	if err := s.business.EnableSellerQuotaV2(ctx); err != nil {
		return err
	}
	s.sellerQuotaV2Enabled = true
	return nil
}

// Decimal strings preserve the entire int64 accounting range through JS;
// no unsafe IEEE-754 number-rounding before reaching the existing ledger.
func quotaDecimalV2(raw string) (int64, error) {
	if len(raw) < 1 || len(raw) > 19 {
		return 0, business.ErrInvalid
	}
	if len(raw) > 1 && raw[0] == '0' {
		return 0, business.ErrInvalid
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, business.ErrInvalid
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, business.ErrInvalid
	}
	return n, nil
}

func (s *Server) attachSellerQuotaV2(mux *http.ServeMux) {
	if s.business == nil || !s.sellerQuotaV2Enabled {
		return
	}
	s.attachSellerManagementV2(mux)
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/sellers",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeSellerBrowserV2(w, r, false, false)
			if !ok {
				return
			}
			q, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				businessError(w, business.ErrInvalid)
				return
			}
			for key, values := range q {
				if (key != "limit" && key != "offset") || len(values) != 1 {
					businessError(w, business.ErrInvalid)
					return
				}
			}
			limit, offset := 25, 0
			if x := q.Get("limit"); x != "" {
				v, e := strconv.Atoi(x)
				if e != nil || v < 1 || v > 100 {
					businessError(w, business.ErrInvalid)
					return
				}
				limit = v
			}
			if x := q.Get("offset"); x != "" {
				v, e := strconv.Atoi(x)
				if e != nil || v < 0 || v > 1_000_000 {
					businessError(w, business.ErrInvalid)
					return
				}
				offset = v
			}
			id := r.PathValue("id")
			result, e := s.business.ListSellersV2As(r.Context(), session.Token, id, limit, offset)
			if e != nil {
				sellerQuotaErrorV2(w, e)
				return
			}
			respond(w, http.StatusOK, map[string]any{"representative_id": id, "sellers": result, "limit": limit, "offset": offset})
		})
	mux.HandleFunc("GET /api/v1/workspace/representatives/{id}/sellers/{seller}",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeSellerBrowserV2(w, r, false, false)
			if !ok {
				return
			}
			if r.URL.RawQuery != "" {
				businessError(w, business.ErrInvalid)
				return
			}
			seller, e := s.business.SellerV2As(r.Context(), session.Token, r.PathValue("id"), r.PathValue("seller"))
			if e != nil {
				sellerQuotaErrorV2(w, e)
				return
			}
			respond(w, http.StatusOK, seller)
		})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/sellers",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeSellerBrowserV2(w, r, true, false)
			if !ok {
				return
			}
			if r.URL.RawQuery != "" {
				businessError(w, business.ErrInvalid)
				return
			}
			var input business.CreateSellerV2
			if decode(w, r, maxBusinessRequestBytes, &input) != nil {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			if input.RepresentativeID != r.PathValue("id") {
				businessError(w, business.ErrInvalid)
				return
			}
			result, e := s.business.CreateSellerV2As(r.Context(), session.Token, input)
			if e != nil {
				sellerQuotaErrorV2(w, e)
				return
			}
			respond(w, http.StatusCreated, result)
		})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/sellers/{seller}/quota",
		func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeSellerBrowserV2(w, r, true, false)
			if !ok {
				return
			}
			if r.URL.RawQuery != "" {
				businessError(w, business.ErrInvalid)
				return
			}
			var input struct {
				EventID          string `json:"event_id"`
				RepresentativeID string `json:"representative_id"`
				SellerID         string `json:"seller_id"`
				Action           string `json:"action"`
				Bytes            string `json:"bytes"`
				Units            string `json:"units"`
				Reason           string `json:"reason"`
			}
			if decode(w, r, maxBusinessRequestBytes, &input) != nil {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
				return
			}
			if input.RepresentativeID != r.PathValue("id") || input.SellerID != r.PathValue("seller") {
				businessError(w, business.ErrInvalid)
				return
			}
			bytes, e := quotaDecimalV2(input.Bytes)
			if e != nil {
				businessError(w, e)
				return
			}
			units, e := quotaDecimalV2(input.Units)
			if e != nil {
				businessError(w, e)
				return
			}
			result, e := s.business.TransferSellerQuotaV2As(r.Context(), session.Token, business.SellerQuotaTransferV2{
				EventID: input.EventID, RepresentativeID: input.RepresentativeID, SellerID: input.SellerID,
				Action: input.Action, Bytes: bytes, Units: units, Reason: input.Reason,
			})
			if e != nil {
				sellerQuotaErrorV2(w, e)
				return
			}
			respond(w, http.StatusOK, result)
		})
}
