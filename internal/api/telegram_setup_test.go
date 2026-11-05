package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

func telegramSetupPublishedProduct(t *testing.T, s *Server, id, representative string) business.CatalogProduct {
	t.Helper()
	ctx := context.Background()
	spec := business.CatalogSpec{ID: id, RepresentativeID: representative, Name: id,
		PlanKind: "volume", DurationDays: 30, IPCount: 1, VolumeBytes: 100, PriceToman: 10000}
	if _, err := s.business.CreateCatalogProduct(ctx, business.CatalogCreate{CatalogSpec: spec, EventID: "api-setup-create-" + id, Reason: "Synthetic setup catalog"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.business.UpdateCatalogProduct(ctx, business.CatalogUpdate{CatalogSpec: spec, EventID: "api-setup-publish-" + id, ExpectedRevision: 1, Status: "published", Reason: "Synthetic published offer"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTelegramWorkspaceSetupSnapshotAndCurrentScope(t *testing.T) {
	ctx := context.Background()
	f := newTelegramConnectionFixture(t)
	f.m.mu.Lock()
	f.m.state.Connections = []telegrambot.ManagedConnection{{BotID: 101, Configured: true, Username: "LabStoreBot", State: "polling"}}
	f.m.mu.Unlock()
	telegramSetupPublishedProduct(t, f.s, "setup-owner-product", "owner")
	for _, id := range []string{"setup-api-a", "setup-api-b"} {
		if _, err := f.s.business.CreateRepresentative(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	f.register(t, "setup-a-first", 201, "setup-api-a")
	f.register(t, "setup-a-second", 202, "setup-api-a")
	f.register(t, "setup-b", 301, "setup-api-b")
	productA := telegramSetupPublishedProduct(t, f.s, "setup-a-product", "setup-api-a")
	telegramSetupPublishedProduct(t, f.s, "setup-b-product", "setup-api-b")
	if _, err := f.s.business.UpdateCatalogPolicy(ctx, business.CatalogPolicyChange{
		CatalogPolicy: business.CatalogPolicy{RepresentativeID: "setup-api-a", ManageProducts: true, CanPublish: true,
			AllowVolume: true, MinPriceToman: 1, MaxPriceToman: 100000, MaxDurationDays: 365, MaxIPCount: 10, MaxVolumeBytes: 10000},
		EventID: "api-setup-catalog-policy-0001", Reason: "Synthetic manager pricing grant",
	}); err != nil {
		t.Fatal(err)
	}
	manager := business.TelegramActor{BotID: 201, UserID: 1201, ChatID: 1201}
	if _, err := f.s.business.TelegramStart(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.SetTelegramProductPrice(ctx, manager, business.TelegramProductPrice{EventID: "api-setup-stars-0001", ProductID: productA.ID, PriceStars: 25}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.SetTelegramTrialPolicy(ctx, manager, business.TelegramTrialPolicyChange{EventID: "api-setup-trial-0001", Enabled: true, VolumeBytes: 200}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.ProvisionRepresentativeLogin(ctx, "setup-api-a", "setup-api-a", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, f.s, "setup-api-a", repPassword)
	read := func(cookie *http.Cookie) map[string]telegramWorkspaceStore {
		t.Helper()
		response := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, cookie, "", "")
		var payload struct {
			Stores []telegramWorkspaceStore `json:"stores"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
			t.Fatal("setup workspace response", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "volume_credit_bytes") || strings.Contains(response.Body.String(), "unlimited_credit_units") {
			t.Fatal("generic setup response exposed financial balances")
		}
		out := map[string]telegramWorkspaceStore{}
		for _, item := range payload.Stores {
			out[item.ID] = item
		}
		return out
	}

	all := read(f.cookie)
	owner := all["shop-a"]
	if len(all) != 4 || owner.ConnectionState != "polling" || owner.Setup.ManagerRegistered ||
		owner.Setup.PublishedProducts != 1 || owner.Setup.PricedProducts != 0 || owner.Setup.TrialEnabled ||
		owner.Setup.CatalogLimit != 200 || owner.Setup.TrialDurationHours != 24 || owner.Setup.TrialStartsAt != "issuance" {
		t.Fatal("polling or a Toman catalog price invented manager, Stars or trial readiness", owner)
	}
	a, second, b := all["setup-a-first"].Setup, all["setup-a-second"].Setup, all["setup-b"].Setup
	if !a.ManagerRegistered || a.PublishedProducts != 1 || a.PricedProducts != 1 || !a.TrialEnabled {
		t.Fatal("saved setup facts missing from API", a)
	}
	if second.ManagerRegistered || second.PublishedProducts != 1 || second.PricedProducts != 0 || second.TrialEnabled || b.PublishedProducts != 1 || b.PricedProducts != 0 {
		t.Fatal("setup metadata crossed a brand or native account", second, b)
	}
	visible := read(repCookie)
	if len(visible) != 2 || visible["setup-a-first"].Setup != a || visible["setup-a-second"].Setup != second {
		t.Fatal("representative setup escaped authenticated scope", visible)
	}
	response := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	var raw struct {
		Stores []map[string]json.RawMessage `json:"stores"`
	}
	if json.Unmarshal(response.Body.Bytes(), &raw) != nil {
		t.Fatal("invalid workspace JSON")
	}
	for _, store := range raw.Stores {
		var setup map[string]json.RawMessage
		if json.Unmarshal(store["setup"], &setup) != nil || len(setup) != 10 {
			t.Fatal("setup contract was omitted or changed", store["setup"])
		}
		for _, key := range []string{"account_enabled", "manager_registered", "can_create_clients", "can_renew_clients", "published_products", "priced_products", "catalog_limit", "trial_enabled", "trial_duration_hours", "trial_starts_at"} {
			if _, exists := setup[key]; !exists {
				t.Fatal("false/zero setup value was omitted", key)
			}
		}
	}
	if response := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, nil, "", ""); response.Code != 401 || strings.Contains(response.Body.String(), "setup") {
		t.Fatal("anonymous setup information", response.Code, response.Body.String())
	}
	if response := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram?representative_id=setup-api-b", nil, repCookie, "", ""); response.Code != 422 {
		t.Fatal("query parameter changed setup scope", response.Code)
	}
	if _, err := f.s.business.UpdateRepresentativeControl(ctx, business.RepresentativeControlChange{
		EventID: "api-setup-suspend-0001", RepresentativeID: "setup-api-a", DisplayName: "setup-api-a",
		Permissions: business.DefaultRepresentativePermissions(), LoginSuspended: true, Reason: "Synthetic account suspension",
	}); err != nil {
		t.Fatal(err)
	}
	if response := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, repCookie, "", ""); response.Code != 401 {
		t.Fatal("revoked browser session retained setup access", response.Code, response.Body.String())
	}
	blocked := read(f.cookie)["setup-a-first"]
	if !blocked.Enabled || blocked.Setup.AccountEnabled || blocked.Setup.CanCreateClients || blocked.Setup.CanRenewClients || !blocked.Setup.TrialEnabled || blocked.Setup.PricedProducts != 1 {
		t.Fatal("native suspension concealed saved settings or looked operational", blocked)
	}
	if f.m.calls() != 0 {
		t.Fatal("read-only setup contacted Telegram or changed the connection", f.m.calls())
	}
}

func TestTelegramWorkspaceSetupUnavailableIsNotEmptyOrDisabled(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	db, err := sql.Open("sqlite", f.db)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`DROP TABLE telegram_commerce_trial_policy`); err != nil {
		t.Fatal(err)
	}
	response := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "business_store_unavailable") || strings.Contains(response.Body.String(), "stores") || strings.Contains(response.Body.String(), "setup") {
		t.Fatal("failed setup query became an empty or disabled success", response.Code, response.Body.String())
	}
	// An explicitly disabled feature keeps its existing early-return behavior;
	// it neither queries missing optional tables nor repairs/migrates them.
	f.s.telegramBotsV1Enabled = false
	response = browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) || !strings.Contains(response.Body.String(), `"stores":[]`) {
		t.Fatal("disabled feature queried setup", response.Code, response.Body.String())
	}
	var tables int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='telegram_commerce_trial_policy'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatal("setup read repaired unavailable schema", tables, err)
	}
	if f.m.calls() != 0 {
		t.Fatal("failed/disabled setup query contacted Telegram")
	}
}
