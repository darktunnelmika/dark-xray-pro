package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
)

// EnableInboundSecurity installs startup-only policy and transport authority.
// Generation and probes require separate, authenticated owner HTTP actions.
func (s *Server) EnableInboundSecurity(config inboundsecurity.Config) error {
	if s.inboundSecurity != nil {
		return errors.New("inbound security tools already configured")
	}
	service, err := inboundsecurity.New(config)
	if err != nil {
		return err
	}
	s.inboundSecurity = service
	return nil
}

func (s *Server) attachInboundSecurity(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	const base = "/api/v1/workspace/inbound-security"
	mux.HandleFunc("GET "+base+"/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		if s.inboundSecurity == nil {
			respond(w, http.StatusOK, map[string]any{"enabled": false, "unavailable_reasons": map[string]string{"service": "inbound_security_not_configured"}})
			return
		}
		respond(w, http.StatusOK, s.inboundSecurity.Capabilities())
	})
	post := func(path string, handler http.HandlerFunc) {
		mux.HandleFunc("POST "+base+path, func(w http.ResponseWriter, r *http.Request) {
			if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
				return
			}
			if s.inboundSecurity == nil {
				respond(w, http.StatusServiceUnavailable, map[string]string{"error": "inbound_security_not_configured"})
				return
			}
			if path == "/reality/find" {
				// The finder has its own bounded context (at most 90 seconds).
				// Extend only this response beyond the Hub's normal write timeout.
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(95 * time.Second))
			}
			handler(w, r)
		})
	}
	post("/keys/x25519", func(w http.ResponseWriter, r *http.Request) {
		if !decodeInboundSecurity(w, r, 1024, &struct{}{}) {
			return
		}
		value, err := s.inboundSecurity.GenerateX25519()
		inboundSecurityResponse(w, value, err)
	})
	post("/keys/mldsa65", func(w http.ResponseWriter, r *http.Request) {
		if !decodeInboundSecurity(w, r, 1024, &struct{}{}) {
			return
		}
		value, err := s.inboundSecurity.GenerateMLDSA65(r.Context())
		inboundSecurityResponse(w, value, err)
	})
	post("/keys/ech", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ServerName string `json:"server_name"`
		}
		if !decodeInboundSecurity(w, r, 2048, &request, "server_name") {
			return
		}
		value, err := s.inboundSecurity.GenerateECH(r.Context(), request.ServerName)
		inboundSecurityResponse(w, value, err)
	})
	post("/certificates/inspect", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			PEM        string `json:"pem"`
			ServerName string `json:"server_name"`
		}
		if !decodeInboundSecurity(w, r, 256<<10, &request, "pem", "server_name") {
			return
		}
		value, err := s.inboundSecurity.InspectCertificate(request.PEM, request.ServerName)
		inboundSecurityResponse(w, value, err)
	})
	post("/certificates/generate", func(w http.ResponseWriter, r *http.Request) {
		var request inboundsecurity.GenerateCertificateRequest
		if !decodeInboundSecurity(w, r, 8192, &request, "server_names", "valid_days") {
			return
		}
		value, err := s.inboundSecurity.GenerateCertificate(request)
		inboundSecurityResponse(w, value, err)
	})
	probe := func(w http.ResponseWriter, r *http.Request) {
		var request inboundsecurity.ProbeRequest
		if !decodeInboundSecurity(w, r, 4096, &request, "scope", "node_id", "target", "server_name") {
			return
		}
		value, err := s.inboundSecurity.Probe(r.Context(), request)
		inboundSecurityResponse(w, value, err)
	}
	post("/reality/scan", probe)
	post("/certificates/remote", probe)
	post("/reality/find", func(w http.ResponseWriter, r *http.Request) {
		var request inboundsecurity.FindRequest
		if !decodeInboundSecurity(w, r, 32<<10, &request, "scope", "node_id") {
			return
		}
		value, err := s.inboundSecurity.Find(r.Context(), request)
		inboundSecurityResponse(w, value, err)
	})
}

// Reject duplicate keys at every nesting level, case aliases, nulls, missing
// scope fields and unknown fields before any network or executable action.
func decodeInboundSecurity(w http.ResponseWriter, r *http.Request, maximum int64, destination any, required ...string) bool {
	invalid := func() bool {
		respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_security_request"})
		return false
	}
	var raw json.RawMessage
	if decode(w, r, maximum, &raw) != nil {
		return invalid()
	}
	if len(raw) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if !walkInboundSecurityJSON(decoder, 0) {
		return invalid()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return invalid()
	}
	for _, field := range required {
		if _, ok := fields[field]; !ok {
			return invalid()
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		return invalid()
	}
	return true
}

func walkInboundSecurityJSON(decoder *json.Decoder, depth int) bool {
	if depth > 6 {
		return false
	}
	token, err := decoder.Token()
	if err != nil || token == nil {
		return false
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok || key == "" || key != strings.ToLower(key) || seen[key] || len(seen) >= 16 {
				return false
			}
			seen[key] = true
			if !walkInboundSecurityJSON(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		count := 0
		for decoder.More() {
			count++
			if count > inboundsecurity.MaxCIDRAddresses || !walkInboundSecurityJSON(decoder, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func inboundSecurityResponse(w http.ResponseWriter, value any, err error) {
	if err == nil {
		respond(w, http.StatusOK, value)
		return
	}
	code, status := "inbound_security_failed", http.StatusInternalServerError
	var public *inboundsecurity.Error
	if errors.As(err, &public) {
		code, status = public.Code, http.StatusUnprocessableEntity
		switch code {
		case "pinned_xray_core_required", "pinned_core_unavailable", "core_pin_mismatch", "selected_node_transport_unavailable", "hub_probe_unavailable_on_node":
			status = http.StatusServiceUnavailable
		case "key_generation_busy", "target_finder_busy":
			status = http.StatusTooManyRequests
		case "selected_node_probe_failed", "selected_node_probe_scope_mismatch", "invalid_core_key_output", "core_key_generation_failed":
			status = http.StatusBadGateway
		case "key_generation_cancelled", "probe_cancelled":
			status = http.StatusRequestTimeout
		case "key_generation_failed", "certificate_generation_failed":
			status = http.StatusInternalServerError
		}
	}
	respond(w, status, map[string]string{"error": code})
}
