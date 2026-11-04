package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

type telegramBotStatusSource interface {
	Statuses() []telegrambot.BotStatus
}

type telegramBotManager interface {
	telegramBotStatusSource
	Management() telegrambot.ManagementStatus
	Verify(context.Context, string) (telegrambot.BotIdentity, error)
	ConfigureAuthorized(context.Context, int64, string, int64, func(context.Context) error) (telegrambot.ManagementStatus, error)
	Check(context.Context, int64) (telegrambot.BotIdentity, error)
}

const telegramConnectionTimeout = 8 * time.Second
const telegramSafeInteger int64 = 1<<53 - 1

var telegramWorkspaceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var telegramWorkspaceUsername = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

// Owner-only outbound checks are bounded both in parallel and over a minute.
// No token or token fingerprint is retained by this pressure gate.
type telegramConnectionGate struct {
	mu       sync.Mutex
	window   time.Time
	attempts int
	active   int
}

func (g *telegramConnectionGate) admit() (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Sub(g.window) >= time.Minute {
		g.window, g.attempts = now, 0
	}
	if g.active >= 2 {
		return false, 2
	}
	if g.attempts >= 60 {
		return false, max(1, int(time.Until(g.window.Add(time.Minute)).Seconds())+1)
	}
	g.attempts++
	g.active++
	return true, 0
}

func (g *telegramConnectionGate) release() {
	g.mu.Lock()
	g.active--
	g.mu.Unlock()
}

// EnableTelegramBotsV1 initializes only native, optional schema. The credential
// routes pass secrets directly to the private transport manager, never business
// records, metadata responses or logs.
func (s *Server) EnableTelegramBotsV1(ctx context.Context) error {
	if s.business == nil || s.telegramBotsV1Enabled {
		return errors.New("Telegram business store unavailable or already configured")
	}
	if err := s.business.EnableTelegramCommerce(ctx); err != nil {
		return err
	}
	if err := s.business.EnableTelegramSupportV1(ctx); err != nil {
		return err
	}
	if err := s.business.EnableTelegramCampaignsV1(ctx); err != nil {
		return err
	}
	if err := s.business.EnableTelegramRewardsV1(ctx); err != nil {
		return err
	}
	if err := s.business.EnableTelegramBonusV1(ctx); err != nil {
		return err
	}
	s.telegramBotsV1Enabled = true
	return nil
}

func (s *Server) SetTelegramBotStatusSource(source telegramBotStatusSource) {
	s.telegramBotsStatus = source // startup-only; source supplies its own locking
}

func (s *Server) SetTelegramBotManager(manager telegramBotManager) {
	s.telegramBotsManager = manager // startup-only; manager supplies its own locking
	s.telegramBotsStatus = manager
}

type telegramWorkspaceStore struct {
	business.TelegramStore
	ConnectionState      string `json:"connection_state"`
	CredentialConfigured bool   `json:"credential_configured"`
	BotUsername          string `json:"bot_username"`
	ConnectionProblem    string `json:"connection_problem"`
	LastPollUnix         int64  `json:"last_poll_unix"`
	QueueStatusAvailable bool   `json:"queue_status_available"`
	InboxPending         int64  `json:"inbox_pending"`
	FinancialRetry       int64  `json:"financial_retry"`
	ReviewPending        int64  `json:"review_pending"`
	OutboxPending        int64  `json:"outbox_pending"`
	OutboxUnknown        int64  `json:"outbox_unknown"`
	DeliveryReview       int64  `json:"delivery_review"`
}

func (s *Server) attachTelegramWorkspace(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/telegram", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			businessError(w, business.ErrInvalid)
			return
		}
		if !s.telegramBotsV1Enabled {
			respond(w, http.StatusOK, map[string]any{"enabled": false, "stores": []telegramWorkspaceStore{}, "can_configure": false, "can_manage_connections": false, "runtime_revision": 0})
			return
		}
		stores, err := s.business.ListTelegramStoresAs(r.Context(), session.Token)
		if err != nil {
			businessError(w, err)
			return
		}
		statuses := map[int64]telegrambot.BotStatus{}
		if s.telegramBotsStatus != nil {
			for _, status := range s.telegramBotsStatus.Statuses() {
				statuses[status.BotID] = status
			}
		}
		connections := map[int64]telegrambot.ManagedConnection{}
		var runtimeRevision int64
		canManage := s.telegramBotsManager != nil && session.User.Role == "owner"
		if s.telegramBotsManager != nil {
			state := s.telegramBotsManager.Management()
			for _, connection := range state.Connections {
				connections[connection.BotID] = connection
			}
			if canManage && state.Revision >= 0 && state.Revision <= telegramSafeInteger {
				runtimeRevision = state.Revision
			} else if canManage {
				canManage = false
			}
		}
		out := make([]telegramWorkspaceStore, 0, len(stores))
		for _, store := range stores {
			item := telegramWorkspaceStore{TelegramStore: store, ConnectionState: "not_configured"}
			if status, ok := statuses[store.BotID]; ok {
				item.ConnectionState = telegramPublicState(status.State)
				if status.LastError != "" {
					item.ConnectionProblem = telegramPublicProblem(status.LastError)
				}
				item.LastPollUnix = status.LastPollUnix
				item.QueueStatusAvailable = status.QueueStatusAvailable
				item.InboxPending = status.InboxPending
				item.FinancialRetry = status.FinancialRetry
				item.ReviewPending = status.ReviewPending
				item.OutboxPending = status.OutboxPending
				item.OutboxUnknown = status.OutboxUnknown
				item.DeliveryReview = status.DeliveryReview
			}
			if connection, ok := connections[store.BotID]; ok {
				item.CredentialConfigured = connection.Configured
				item.BotUsername = telegramPublicUsername(connection.Username)
				// Management was observed after the queue/status snapshot. A
				// replaced or stopped loop must not inherit an older polling badge.
				item.ConnectionState = telegramPublicState(connection.State)
				item.ConnectionProblem = telegramPublicProblem(connection.Problem)
			}
			if !store.Enabled {
				item.ConnectionState = "store_disabled"
			}
			out = append(out, item)
		}
		respond(w, http.StatusOK, map[string]any{"enabled": true, "stores": out, "can_configure": session.User.Role == "owner", "can_manage_connections": canManage, "runtime_revision": runtimeRevision})
	})
	mux.HandleFunc("POST /api/v1/workspace/telegram/stores", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, true, true)
		if !ok {
			return
		}
		if !s.telegramBotsV1Enabled {
			respond(w, http.StatusConflict, map[string]string{"error": "telegram_not_enabled"})
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			businessError(w, business.ErrInvalid)
			return
		}
		var in business.TelegramStoreRegistration
		if !decodeSellerOrderV1(w, r, &in, "event_id", "id", "bot_id", "representative_id", "name", "manager_user_id", "expected_revision", "enabled") {
			return
		}
		out, err := s.business.ConfigureTelegramStore(r.Context(), session.Token, in)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, out)
	})
	s.attachTelegramConnections(mux)
}

func telegramPublicUsername(value string) string {
	if telegramWorkspaceUsername.MatchString(value) {
		return value
	}
	return ""
}

func telegramPublicState(value string) string {
	switch value {
	case "not_configured", "configured", "starting", "identity_verified", "polling", "error", "stopped":
		return value
	default:
		return "unknown"
	}
}

func telegramPublicProblem(value string) string {
	switch value {
	case "", "worker_stopped", "commerce_maintenance_unavailable", "campaign_queue_unavailable",
		"identity_mismatch", "webhook_exists", "credentials_invalid", "transport_unavailable", "journal_unavailable", "connection_unavailable":
		return value
	default:
		return "connection_unavailable"
	}
}

// Never expose arbitrary error strings: transport errors can originate from
// credential-bearing requests, and filesystem errors contain private paths.
func telegramConnectionError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "telegram_connection_unavailable"
	var apiErr *telegrambot.APIError
	switch {
	case errors.Is(err, telegrambot.ErrAccess):
		status, code = http.StatusForbidden, "owner_permission_required"
	case errors.Is(err, telegrambot.ErrConflict):
		status, code = http.StatusConflict, "telegram_revision_conflict"
	case errors.Is(err, telegrambot.ErrWebhook):
		status, code = http.StatusConflict, "telegram_webhook_exists"
	case errors.Is(err, telegrambot.ErrIdentity):
		status, code = http.StatusUnprocessableEntity, "telegram_identity_mismatch"
	case errors.Is(err, telegrambot.ErrConfig):
		status, code = http.StatusUnprocessableEntity, "telegram_credentials_invalid"
	case errors.Is(err, telegrambot.ErrCapacity):
		status, code = http.StatusConflict, "telegram_capacity_reached"
	case errors.As(err, &apiErr):
		if apiErr.Definitive && apiErr.Code == http.StatusUnauthorized {
			status, code = http.StatusUnprocessableEntity, "telegram_credentials_invalid"
		} else if apiErr.Definitive && apiErr.Code == http.StatusTooManyRequests {
			status, code = http.StatusTooManyRequests, "telegram_rate_limited"
			retry := max(1, min(3600, int(apiErr.RetryAfter.Seconds())+1))
			w.Header().Set("Retry-After", strconv.Itoa(retry))
		} else {
			code = "telegram_transport_unavailable"
		}
	case errors.Is(err, telegrambot.ErrTransport), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		code = "telegram_transport_unavailable"
	}
	respond(w, status, map[string]string{"error": code})
}

func (s *Server) authorizeTelegramConnection(w http.ResponseWriter, r *http.Request) (business.LoginSession, bool) {
	w.Header().Set("Cache-Control", "no-store, private")
	session, ok := s.authorizeBrowser(w, r, true, true)
	if !ok {
		return session, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return session, false
	}
	if !s.telegramBotsV1Enabled {
		respond(w, http.StatusConflict, map[string]string{"error": "telegram_not_enabled"})
		return session, false
	}
	if s.telegramBotsManager == nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "telegram_management_unavailable"})
		return session, false
	}
	return session, true
}

func decodeTelegramConnection(w http.ResponseWriter, r *http.Request, in any, fields ...string) bool {
	// Reuse the duplicate/null/unknown-field resistant flat decoder, with the
	// credential route's tighter bound underneath its ordinary business bound.
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	return decodeSellerOrderV1(w, r, in, fields...)
}

func (s *Server) beginTelegramConnection(w http.ResponseWriter, r *http.Request) (context.Context, func(), bool) {
	if allowed, retry := s.telegramConnectionGate.admit(); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		respond(w, http.StatusTooManyRequests, map[string]string{"error": "telegram_connection_busy"})
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), telegramConnectionTimeout)
	return ctx, func() { cancel(); s.telegramConnectionGate.release() }, true
}

func (s *Server) telegramRegisteredStore(ctx context.Context, sessionToken, id string) (business.TelegramStore, error) {
	stores, err := s.business.ListTelegramStoresAs(ctx, sessionToken)
	if err != nil {
		return business.TelegramStore{}, err
	}
	for _, store := range stores {
		if store.ID == id {
			return store, nil
		}
	}
	return business.TelegramStore{}, business.ErrNotFound
}

func (s *Server) telegramConnectionStore(w http.ResponseWriter, r *http.Request, session business.LoginSession) (business.TelegramStore, bool) {
	id := r.PathValue("id")
	if !telegramWorkspaceID.MatchString(id) {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return business.TelegramStore{}, false
	}
	store, err := s.telegramRegisteredStore(r.Context(), session.Token, id)
	if errors.Is(err, business.ErrNotFound) {
		respond(w, http.StatusNotFound, map[string]string{"error": "telegram_store_not_found"})
		return business.TelegramStore{}, false
	}
	if err != nil {
		businessError(w, err)
		return business.TelegramStore{}, false
	}
	return store, true
}

func (s *Server) telegramConnectionAuthority(ctx context.Context, session business.LoginSession, store business.TelegramStore) error {
	current, err := s.business.VerifySession(ctx, session.Token)
	if err != nil || current.User.ID != session.User.ID || current.User.Role != "owner" {
		return telegrambot.ErrAccess
	}
	bound, err := s.telegramRegisteredStore(ctx, session.Token, store.ID)
	if err != nil || bound.BotID != store.BotID || bound.RepresentativeID != store.RepresentativeID {
		return telegrambot.ErrAccess
	}
	return nil
}

func telegramIdentityResult(w http.ResponseWriter, identity telegrambot.BotIdentity, expectedBot int64) bool {
	if identity.BotID < 1 || identity.BotID > telegramSafeInteger || expectedBot > 0 && identity.BotID != expectedBot {
		telegramConnectionError(w, telegrambot.ErrIdentity)
		return false
	}
	respond(w, http.StatusOK, map[string]any{"bot_id": identity.BotID, "username": telegramPublicUsername(identity.Username)})
	return true
}

func (s *Server) attachTelegramConnections(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/workspace/telegram/verify", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeTelegramConnection(w, r)
		if !ok {
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		if !decodeTelegramConnection(w, r, &in, "token") {
			return
		}
		if len(in.Token) < 1 || len(in.Token) > 256 {
			telegramConnectionError(w, telegrambot.ErrConfig)
			return
		}
		ctx, finish, ok := s.beginTelegramConnection(w, r)
		if !ok {
			return
		}
		defer finish()
		identity, err := s.telegramBotsManager.Verify(ctx, in.Token)
		in.Token = ""
		if err != nil {
			telegramConnectionError(w, err)
			return
		}
		current, err := s.business.VerifySession(ctx, session.Token)
		if err != nil || current.User.ID != session.User.ID || current.User.Role != "owner" {
			telegramConnectionError(w, telegrambot.ErrAccess)
			return
		}
		telegramIdentityResult(w, identity, 0)
	})
	mux.HandleFunc("POST /api/v1/workspace/telegram/stores/{id}/connection", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeTelegramConnection(w, r)
		if !ok {
			return
		}
		var in struct {
			Token            string `json:"token"`
			ExpectedRevision int64  `json:"expected_revision"`
		}
		if !decodeTelegramConnection(w, r, &in, "token", "expected_revision") {
			return
		}
		if len(in.Token) > 256 || in.ExpectedRevision < 0 || in.ExpectedRevision > telegramSafeInteger {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		store, ok := s.telegramConnectionStore(w, r, session)
		if !ok {
			return
		}
		ctx, finish, ok := s.beginTelegramConnection(w, r)
		if !ok {
			return
		}
		defer finish()
		state, err := s.telegramBotsManager.ConfigureAuthorized(ctx, store.BotID, in.Token, in.ExpectedRevision, func(ctx context.Context) error {
			return s.telegramConnectionAuthority(ctx, session, store)
		})
		in.Token = ""
		if err != nil {
			telegramConnectionError(w, err)
			return
		}
		for _, connection := range state.Connections {
			if connection.BotID == store.BotID && connection.Configured && state.Revision >= 0 && state.Revision <= telegramSafeInteger {
				respond(w, http.StatusOK, map[string]any{"revision": state.Revision, "bot_id": store.BotID, "username": telegramPublicUsername(connection.Username), "state": telegramPublicState(connection.State)})
				return
			}
		}
		telegramConnectionError(w, telegrambot.ErrState)
	})
	mux.HandleFunc("POST /api/v1/workspace/telegram/stores/{id}/check", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeTelegramConnection(w, r)
		if !ok {
			return
		}
		var in struct{}
		if !decodeTelegramConnection(w, r, &in) {
			return
		}
		store, ok := s.telegramConnectionStore(w, r, session)
		if !ok {
			return
		}
		ctx, finish, ok := s.beginTelegramConnection(w, r)
		if !ok {
			return
		}
		defer finish()
		identity, err := s.telegramBotsManager.Check(ctx, store.BotID)
		if err != nil {
			telegramConnectionError(w, err)
			return
		}
		if err := s.telegramConnectionAuthority(ctx, session, store); err != nil {
			telegramConnectionError(w, err)
			return
		}
		telegramIdentityResult(w, identity, store.BotID)
	})
}
