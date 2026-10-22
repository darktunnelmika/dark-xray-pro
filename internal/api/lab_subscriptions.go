package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// EnableLabSubscriptions installs the reviewed schema only at explicit startup.
// It does not queue health work, start a runtime, renew a lease or publish a
// public endpoint. The normal HUB remains default-off.
func (s *Server) EnableLabSubscriptions() error {
	if s.business == nil || len(s.inboundKey) != 32 || !s.healthRoundsEnabled || s.healthEndpoint == nil || s.labSubscriptionsEnabled {
		return errors.New("LAB subscriptions require explicitly enabled N17 health rounds")
	}
	if err := s.business.EnableLabSubscriptions(context.Background()); err != nil {
		return err
	}
	s.labSubscriptionsEnabled = true
	return nil
}

func (s *Server) attachLabSubscriptions(mux *http.ServeMux) {
	// Metadata contains no bearer capability or configuration. Even feature
	// discovery is client-scoped and authenticated.
	mux.HandleFunc("GET /api/v1/workspace/clients/{id}/subscription", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok || !s.subscriptionClientScope(w, r, session.User) {
			return
		}
		if r.URL.RawQuery != "" {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.labSubscriptionsEnabled {
			respond(w, http.StatusOK, map[string]any{"enabled": false, "scope": "lab_single_handover", "client_id": r.PathValue("id")})
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		v, err := s.business.LabSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		if v.Snapshot != nil && v.Snapshot.Current {
			op, e := s.business.MeteredHandover(r.Context(), v.Snapshot.HandoverID)
			if e != nil || !s.handoverScope(op.SourceNode, op.TargetNode, op.SourceInstallation, op.TargetInstallation) {
				v.Snapshot.Current = false
			}
		}
		respond(w, http.StatusOK, struct {
			business.LabSubscriptionView
			Enabled    bool   `json:"enabled"`
			Scope      string `json:"scope"`
			ServerTime int64  `json:"server_time_unix_milli"`
		}{v, true, "lab_single_handover", time.Now().UnixMilli()})
	})
	for _, action := range []string{"issue", "rotate", "revoke", "reveal"} {
		action := action
		mux.HandleFunc("POST /api/v1/workspace/clients/{id}/subscription/"+action, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, true, false)
			if !ok || !s.subscriptionClientScope(w, r, session.User) {
				return
			}
			if !s.labSubscriptionsEnabled {
				businessError(w, business.ErrConflict)
				return
			}
			if r.URL.RawQuery != "" {
				businessError(w, business.ErrInvalid)
				return
			}
			if action == "reveal" {
				var in struct {
					ExpectedGeneration uint64 `json:"expected_generation"`
				}
				if decode(w, r, 512, &in) != nil || in.ExpectedGeneration == 0 {
					businessError(w, business.ErrInvalid)
					return
				}
				v, err := s.business.RevealLabSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), in.ExpectedGeneration)
				if err != nil {
					businessError(w, err)
					return
				}
				respond(w, http.StatusOK, struct {
					business.SubscriptionSecret
					Path string `json:"path"`
				}{v, "/api/v1/subscriptions/" + v.Token})
				return
			}
			var in struct {
				EventID            string `json:"event_id"`
				ExpectedGeneration uint64 `json:"expected_generation"`
			}
			if decode(w, r, 512, &in) != nil || !handoverID.MatchString(in.EventID) {
				businessError(w, business.ErrInvalid)
				return
			}
			v, err := s.business.MutateLabSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), business.SubscriptionTokenMutation{EventID: in.EventID, Action: action, ExpectedGeneration: in.ExpectedGeneration})
			if err != nil {
				businessError(w, err)
				return
			}
			respond(w, http.StatusOK, v)
		})
	}
	mux.HandleFunc("POST /api/v1/workspace/metered-handovers/{id}/lab-subscription-snapshot", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		var in business.LabSubscriptionMaterialization
		if r.URL.RawQuery != "" || !handoverID.MatchString(r.PathValue("id")) || decode(w, r, 1024, &in) != nil || !handoverID.MatchString(in.EventID) || !handoverID.MatchString(in.RoundID) {
			businessError(w, business.ErrInvalid)
			return
		}
		s.replacementLifecycle.Lock()
		defer s.replacementLifecycle.Unlock()
		if !s.labSubscriptionsEnabled {
			businessError(w, business.ErrConflict)
			return
		}
		if _, ok = s.healthOperation(w, r, r.PathValue("id")); !ok {
			return
		}
		v, err := s.business.MaterializeLabSubscription(r.Context(), s.inboundKey, session.User.ID, r.PathValue("id"), in, *s.healthEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		status := http.StatusCreated
		if v.Replayed {
			status = http.StatusOK
		}
		respond(w, status, v)
	})
	mux.HandleFunc("GET /api/v1/subscriptions/{token}", s.serveLabSubscription)
}

func (s *Server) subscriptionClientScope(w http.ResponseWriter, r *http.Request, user business.AuthUser) bool {
	if s.business == nil {
		http.NotFound(w, r)
		return false
	}
	c, err := s.business.Client(r.Context(), r.PathValue("id"))
	if err != nil {
		businessError(w, err)
		return false
	}
	if user.Role != "owner" && c.RepresentativeID != user.RepresentativeID {
		respond(w, http.StatusForbidden, map[string]string{"error": "representative_scope_denied"})
		return false
	}
	return true
}

func (s *Server) serveLabSubscription(w http.ResponseWriter, r *http.Request) {
	// GET patterns also match HEAD in Go's mux. Do not reveal metadata or run
	// the delivery transaction for any method other than an actual fetch.
	w.Header().Set("Cache-Control", "no-store, private, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	unavailable := func() { respond(w, http.StatusNotFound, map[string]string{"error": "subscription_unavailable"}) }
	if !s.labSubscriptionsEnabled || s.healthEndpoint == nil {
		unavailable()
		return
	}
	token := r.PathValue("token")
	if len(token) != 43 {
		unavailable()
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != token {
		unavailable()
		return
	}
	// ParseQuery rejects malformed escaping; URL.Query alone would discard it.
	format, err := subscriptionQuery(r.URL.RawQuery)
	if err != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	// Public bearer requests must never queue unbounded waiters ahead of Node
	// lifecycle work. A busy reader retries; no background work is created.
	if !s.replacementLifecycle.TryLock() {
		w.Header().Set("Retry-After", "1")
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "subscription_unavailable"})
		return
	}
	var body []byte
	var expires int64
	err = func() error {
		defer s.replacementLifecycle.Unlock()
		v, readErr := s.business.ReadLabSubscription(ctx, s.inboundKey, token, *s.healthEndpoint)
		if readErr != nil || !s.handoverScope(v.SourceNodeID, v.Snapshot.NodeID, v.SourceInstallationID, v.Snapshot.InstallationID) || !v.Snapshot.Current || time.Now().UnixNano() >= v.Snapshot.ExpiresUnixNano || len(v.Raw) == 0 || len(v.Raw) > 2<<20 {
			return business.ErrUnavailable
		}
		body, expires = v.Raw, v.Snapshot.ExpiresUnixNano
		if format == "base64" {
			body = []byte(base64.StdEncoding.EncodeToString(body))
		}
		return nil
	}()
	// Authorization linearizes above. An arbitrary slow HTTP consumer must not
	// retain the Node lifecycle lock during socket writes or error responses.
	if err != nil || ctx.Err() != nil || time.Now().UnixNano() >= expires {
		unavailable()
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Dark-Pro-Scope", "lab_single_handover")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func subscriptionQuery(raw string) (string, error) {
	q, err := url.ParseQuery(raw)
	if err != nil || len(q) > 1 {
		return "", business.ErrInvalid
	}
	if len(q) == 0 {
		return "base64", nil
	}
	v, ok := q["format"]
	if !ok || len(v) != 1 || (v[0] != "raw" && v[0] != "base64") {
		return "", business.ErrInvalid
	}
	return v[0], nil
}
