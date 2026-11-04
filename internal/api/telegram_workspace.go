package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

type telegramBotStatusSource interface {
	Statuses() []telegrambot.BotStatus
}

// EnableTelegramBotsV1 initializes only native, optional schema. Credentials
// are accepted exclusively by the separate private transport configuration.
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
	s.telegramBotsV1Enabled = true
	return nil
}

func (s *Server) SetTelegramBotStatusSource(source telegramBotStatusSource) {
	s.telegramBotsStatus = source // startup-only; source supplies its own locking
}

type telegramWorkspaceStore struct {
	business.TelegramStore
	ConnectionState      string `json:"connection_state"`
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
			respond(w, http.StatusOK, map[string]any{"enabled": false, "stores": []telegramWorkspaceStore{}, "can_configure": false})
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
		out := make([]telegramWorkspaceStore, 0, len(stores))
		for _, store := range stores {
			item := telegramWorkspaceStore{TelegramStore: store, ConnectionState: "not_configured"}
			if status, ok := statuses[store.BotID]; ok {
				item.ConnectionState = status.State
				item.LastPollUnix = status.LastPollUnix
				item.QueueStatusAvailable = status.QueueStatusAvailable
				item.InboxPending = status.InboxPending
				item.FinancialRetry = status.FinancialRetry
				item.ReviewPending = status.ReviewPending
				item.OutboxPending = status.OutboxPending
				item.OutboxUnknown = status.OutboxUnknown
				item.DeliveryReview = status.DeliveryReview
			}
			if !store.Enabled {
				item.ConnectionState = "store_disabled"
			}
			out = append(out, item)
		}
		respond(w, http.StatusOK, map[string]any{"enabled": true, "stores": out, "can_configure": session.User.Role == "owner"})
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
}
