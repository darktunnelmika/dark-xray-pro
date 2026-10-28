package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// EnableSellerOrdersV1Lab is startup-only. It installs only the reviewed order
// schema, after explicit seller-management, usage and refund configuration.
// Calling it does not launch workers, mount an Agent route or activate a client.
func (s *Server) EnableSellerOrdersV1Lab(ctx context.Context) error {
	if s.sellerOrdersV1Enabled {
		return errors.New("seller_orders_api_already_configured")
	}
	if s.business == nil || s.business.UsageLedger() == nil || !s.sellerQuotaV2Enabled ||
		s.volumeRefunds == nil || len(s.inboundKey) != 32 {
		return errors.New("seller_orders_prerequisites_missing")
	}
	if err := s.business.EnableSellerOrdersV1(ctx); err != nil {
		return err
	}
	s.sellerOrdersV1Enabled = true
	return nil
}

// Orders have flat, explicit wire contracts. Before typed decoding, reject
// duplicate keys, case aliases, nulls and missing fields, which encoding/json's
// ordinary struct decoder otherwise accepts. The shared decoder bounds the
// entire body and rejects trailing JSON values.
func decodeSellerOrderV1(w http.ResponseWriter, r *http.Request, destination any, fields ...string) bool {
	invalid := func() bool {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	var raw json.RawMessage
	if decode(w, r, maxBusinessRequestBytes, &raw) != nil {
		return invalid()
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return invalid()
	}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] {
			return invalid()
		}
		delete(allowed, key)
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid()
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(allowed) != 0 {
		return invalid()
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(destination) != nil {
		return invalid()
	}
	return true
}

func (s *Server) attachSellerOrdersV1(mux *http.ServeMux) {
	if s.business == nil || !s.sellerOrdersV1Enabled {
		return
	}
	// Keep the internal representative placeholder "id": seller browser
	// authorization uses it to compare the trusted session's tenant.
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/sellers/{seller}/clients", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeSellerBrowserV2(w, r, true, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			businessError(w, business.ErrInvalid)
			return
		}
		var input struct {
			EventID          string `json:"event_id"`
			RepresentativeID string `json:"representative_id"`
			SellerID         string `json:"seller_id"`
			ClientID         string `json:"client_id"`
			Name             string `json:"name"`
			DurationDays     int64  `json:"duration_days"`
			IPCount          int64  `json:"ip_count"`
			VolumeBytes      string `json:"volume_bytes"`
			Unlimited        bool   `json:"unlimited"`
			Reason           string `json:"reason"`
		}
		if !decodeSellerOrderV1(w, r, &input, "event_id", "representative_id", "seller_id", "client_id",
			"name", "duration_days", "ip_count", "volume_bytes", "unlimited", "reason") {
			return
		}
		if input.RepresentativeID != r.PathValue("id") || input.SellerID != r.PathValue("seller") {
			businessError(w, business.ErrInvalid)
			return
		}
		volume, err := quotaDecimalV2(input.VolumeBytes)
		if err != nil {
			businessError(w, err)
			return
		}
		receipt, err := s.business.CreateSellerClientV1(r.Context(), session.Token, business.SellerClientCreateV1{
			EventID: input.EventID, RepresentativeID: input.RepresentativeID, SellerID: input.SellerID,
			ClientID: input.ClientID, Name: input.Name, DurationDays: input.DurationDays, IPCount: input.IPCount,
			VolumeBytes: volume, Unlimited: input.Unlimited, Reason: input.Reason,
		})
		if err != nil {
			sellerQuotaErrorV2(w, err)
			return
		}
		respond(w, http.StatusCreated, receipt)
	})
	mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/sellers/{seller}/clients/{client}/extend", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeSellerBrowserV2(w, r, true, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			businessError(w, business.ErrInvalid)
			return
		}
		var input struct {
			EventID               string `json:"event_id"`
			RepresentativeID      string `json:"representative_id"`
			SellerID              string `json:"seller_id"`
			ClientID              string `json:"client_id"`
			AdditionalDays        int64  `json:"additional_days"`
			AdditionalVolumeBytes string `json:"additional_volume_bytes"`
			Reason                string `json:"reason"`
		}
		if !decodeSellerOrderV1(w, r, &input, "event_id", "representative_id", "seller_id", "client_id",
			"additional_days", "additional_volume_bytes", "reason") {
			return
		}
		if input.RepresentativeID != r.PathValue("id") || input.SellerID != r.PathValue("seller") || input.ClientID != r.PathValue("client") {
			businessError(w, business.ErrInvalid)
			return
		}
		volume, err := quotaDecimalV2(input.AdditionalVolumeBytes)
		if err != nil {
			businessError(w, err)
			return
		}
		receipt, err := s.business.ExtendSellerClientV1(r.Context(), session.Token, business.SellerClientExtendV1{
			EventID: input.EventID, RepresentativeID: input.RepresentativeID, SellerID: input.SellerID,
			ClientID: input.ClientID, AdditionalDays: input.AdditionalDays, AdditionalVolumeBytes: volume, Reason: input.Reason,
		})
		if err != nil {
			sellerQuotaErrorV2(w, err)
			return
		}
		respond(w, http.StatusOK, receipt)
	})
}
