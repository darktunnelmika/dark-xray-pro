package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

type telegramStatusFixture []telegrambot.BotStatus

func (s telegramStatusFixture) Statuses() []telegrambot.BotStatus {
	return append([]telegrambot.BotStatus(nil), s...)
}

func TestTelegramWorkspaceScopeCSRFAndRealConnectionState(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	ownerCookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTelegramBotsV1(ctx); err != nil {
		t.Fatal(err)
	}
	for _, rep := range []string{"tg-rep-a", "tg-rep-b"} {
		if _, err := s.business.CreateRepresentative(ctx, rep, rep); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.ProvisionRepresentativeLogin(ctx, rep, rep, repPassword); err != nil {
			t.Fatal(err)
		}
	}
	h := s.BrowserHandler()
	create := business.TelegramStoreRegistration{EventID: "api-telegram-store-a-0001", ID: "shop-a", BotID: 101, RepresentativeID: "tg-rep-a", Name: "Store A", ManagerUserID: 1001, Enabled: true}
	if w := browserRequest(t, h, "POST", "/api/v1/workspace/telegram/stores", create, ownerCookie, "", "https://"+browserHost); w.Code != http.StatusForbidden {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	if w := browserRequest(t, h, "POST", "/api/v1/workspace/telegram/stores", create, ownerCookie, csrf, "https://foreign.example"); w.Code != http.StatusForbidden {
		t.Fatal("foreign origin accepted", w.Code)
	}
	if w := browserRequest(t, h, "POST", "/api/v1/workspace/telegram/stores", create, ownerCookie, csrf, "https://"+browserHost); w.Code != http.StatusOK {
		t.Fatal("owner setup", w.Code, w.Body.String())
	}
	validBody, _ := json.Marshal(create)
	for _, body := range []string{
		strings.Replace(string(validBody), `,"enabled":true`, "", 1),
		strings.Replace(string(validBody), `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(string(validBody), `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		strings.Replace(string(validBody), `"enabled":true`, `"Enabled":true`, 1),
	} {
		r := httptest.NewRequest("POST", "https://"+browserHost+"/api/v1/workspace/telegram/stores", strings.NewReader(body))
		r.AddCookie(ownerCookie)
		r.Header.Set("Origin", "https://"+browserHost)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("ambiguous store registration accepted", w.Code)
		}
	}
	create.EventID, create.ID, create.BotID, create.RepresentativeID = "api-telegram-store-b-0001", "shop-b", 102, "tg-rep-b"
	if w := browserRequest(t, h, "POST", "/api/v1/workspace/telegram/stores", create, ownerCookie, csrf, "https://"+browserHost); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	s.SetTelegramBotStatusSource(telegramStatusFixture{{BotID: 101, State: "polling", LastPollUnix: 100, QueueStatusAvailable: true, InboxPending: 8, FinancialRetry: 2, ReviewPending: 3, OutboxPending: 5, OutboxUnknown: 7, DeliveryReview: 11}, {BotID: 102, State: "error", FinancialRetry: 40}, {BotID: 999, State: "polling"}})
	ownerView := browserRequest(t, h, "GET", "/api/v1/workspace/telegram", nil, ownerCookie, "", "")
	if ownerView.Code != 200 || !strings.Contains(ownerView.Body.String(), `"connection_state":"polling"`) || !strings.Contains(ownerView.Body.String(), `"connection_state":"error"`) || strings.Contains(ownerView.Body.String(), `999`) {
		t.Fatal("status falsely synthesized or unregistered bot exposed", ownerView.Code, ownerView.Body.String())
	}
	repCookie, repCSRF := browserLogin(t, s, "tg-rep-a", repPassword)
	w := browserRequest(t, h, "GET", "/api/v1/workspace/telegram", nil, repCookie, "", "")
	var payload struct {
		Stores       []telegramWorkspaceStore `json:"stores"`
		CanConfigure bool                     `json:"can_configure"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &payload) != nil || len(payload.Stores) != 1 || payload.Stores[0].ID != "shop-a" || payload.CanConfigure {
		t.Fatal("representative scope", w.Code, w.Body.String())
	}
	status := payload.Stores[0]
	if !status.QueueStatusAvailable || status.InboxPending != 8 || status.FinancialRetry != 2 || status.ReviewPending != 3 || status.OutboxPending != 5 || status.OutboxUnknown != 7 || status.DeliveryReview != 11 {
		t.Fatal("queue status missing or leaked across stores", w.Body.String())
	}
	if w := browserRequest(t, h, "GET", "/api/v1/workspace/telegram?representative_id=tg-rep-b", nil, repCookie, "", ""); w.Code != 422 {
		t.Fatal("unreviewed scope query", w.Code)
	}
	if w := browserRequest(t, h, "POST", "/api/v1/workspace/telegram/stores", create, repCookie, repCSRF, "https://"+browserHost); w.Code != 403 {
		t.Fatal("representative registered arbitrary bot", w.Code)
	}
	if w := browserRequest(t, h, "GET", "/api/v1/workspace/telegram", nil, nil, "", ""); w.Code != 401 {
		t.Fatal("anonymous store metadata", w.Code)
	}
	me := browserRequest(t, h, "GET", "/api/v1/workspace/me", nil, repCookie, "", "")
	if !strings.Contains(me.Body.String(), `"telegram_bots_v1":true`) {
		t.Fatal("missing capability")
	}
}

func TestTelegramWorkspaceDisabledDoesNotOpenCommerceSchema(t *testing.T) {
	s := enabledOwner(t)
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	w := browserRequest(t, s.BrowserHandler(), "GET", "/api/v1/workspace/telegram", nil, cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) || !strings.Contains(w.Body.String(), `"stores":[]`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = browserRequest(t, s.BrowserHandler(), "POST", "/api/v1/workspace/telegram/stores", business.TelegramStoreRegistration{}, cookie, csrf, "https://"+browserHost)
	if w.Code != 409 {
		t.Fatal("disabled feature mutated", w.Code)
	}
	if err := s.business.VerifyTelegramCommerceSchema(context.Background()); err == nil {
		t.Fatal("page viewing migrated bot schema")
	}
}
