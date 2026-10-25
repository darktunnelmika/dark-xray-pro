package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func executionFormat(r *http.Request) (string, bool) {
	q := r.URL.Query()
	for k, v := range q {
		if k != "format" || len(v) != 1 {
			return "", false
		}
	}
	format := q.Get("format")
	if format == "" {
		format = "uri"
	}
	switch format {
	case "uri", "json", "xray", "sing-box", "wireguard":
		return format, true
	default:
		return "", false
	}
}
func writeExecutionSubscription(w http.ResponseWriter, r *http.Request, s inboundexec.Subscription) {
	w.Header().Set("Content-Type", s.ContentType)
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Subscription-SHA256", s.SHA256)
	w.Header().Set("X-Subscription-Valid-Until", strconv.FormatInt(s.ExpiresUnixNano/1e6, 10))
	w.WriteHeader(200)
	_, _ = w.Write(s.Content)
}
func (s *Server) attachInboundExecutionSubscriptions(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	base := "/api/v1/workspace/clients/{id}/inbound-subscription"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok || !s.subscriptionClientScope(w, r, session.User) {
			return
		}
		if r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		if s.inboundExecution == nil {
			respond(w, 200, map[string]any{"enabled": false, "client_id": r.PathValue("id")})
			return
		}
		v, e := s.business.InboundExecutionSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"))
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, struct {
			inboundexec.SubscriptionView
			Enabled    bool  `json:"enabled"`
			ServerTime int64 `json:"server_time_unix_milli"`
		}{v, true, time.Now().UnixMilli()})
	})
	for _, action := range []string{"issue", "rotate", "revoke", "reveal"} {
		action := action
		mux.HandleFunc("POST "+base+"/"+action, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, true, false)
			if !ok || !s.subscriptionClientScope(w, r, session.User) {
				return
			}
			if s.inboundExecution == nil || r.URL.RawQuery != "" {
				businessError(w, business.ErrUnavailable)
				return
			}
			if action == "reveal" {
				var in struct {
					ExpectedGeneration uint64 `json:"expected_generation"`
				}
				if decodeExecution(w, r, 512, &in) != nil || in.ExpectedGeneration == 0 {
					businessError(w, business.ErrInvalid)
					return
				}
				secret, e := s.business.RevealInboundExecutionSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), in.ExpectedGeneration)
				if e != nil {
					businessError(w, e)
					return
				}
				respond(w, 200, struct {
					business.SubscriptionSecret
					Path string `json:"path"`
				}{secret, "/api/v1/inbound-subscriptions/" + secret.Token})
				return
			}
			var in struct {
				EventID            string `json:"event_id"`
				ExpectedGeneration uint64 `json:"expected_generation"`
			}
			if decodeExecution(w, r, 512, &in) != nil {
				businessError(w, business.ErrInvalid)
				return
			}
			out, e := s.business.MutateInboundExecutionSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), business.SubscriptionTokenMutation{EventID: in.EventID, Action: action, ExpectedGeneration: in.ExpectedGeneration})
			if e != nil {
				businessError(w, e)
				return
			}
			respond(w, 200, out)
		})
	}
	mux.HandleFunc("GET "+base+"/export", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok || !s.subscriptionClientScope(w, r, session.User) {
			return
		}
		if s.inboundExecution == nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		format, ok := executionFormat(r)
		if !ok {
			businessError(w, business.ErrInvalid)
			return
		}
		out, e := s.business.ProjectInboundExecutionSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), format)
		if e != nil {
			businessError(w, e)
			return
		}
		writeExecutionSubscription(w, r, out)
	})
	mux.HandleFunc("GET /api/v1/inbound-subscriptions/{token}", func(w http.ResponseWriter, r *http.Request) {
		if s.inboundExecution == nil {
			http.NotFound(w, r)
			return
		}
		format, ok := executionFormat(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		out, e := s.business.ReadInboundExecutionSubscription(r.Context(), s.inboundKey, r.PathValue("token"), format)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		writeExecutionSubscription(w, r, out)
	})
}
