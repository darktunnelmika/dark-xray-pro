package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func initialHealthBase() string { return "/api/v1/workspace/native-inbound-plans/8123/initial-health" }

func promoteInitialHealthFixture(t *testing.T, f *promotionAPIFixture) business.NativeInboundPlanPromotionReceipt {
	t.Helper()
	f.input.ReviewSHA256 = f.review(t).ReviewSHA256
	w := f.post(t, f.base(), nativeInboundPlanPromotionWire{NativeInboundPlanPromotionInput: f.input, Confirmed: true})
	var receipt business.NativeInboundPlanPromotionReceipt
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || !receipt.Promoted {
		t.Fatal("promoted initial-health fixture", w.Code, w.Body.String())
	}
	return receipt
}

func TestInitialApplyHealthStartupAndDefaultOff(t *testing.T) {
	f := newPromotionAPIFixture(t, true)
	get := func() *httptest.ResponseRecorder {
		return browserRequest(t, f.s.Handler(), http.MethodGet, initialHealthBase(), nil, f.cookie, "", "")
	}
	w := get()
	var off struct {
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &off) != nil || off.Enabled || off.Scope != business.InitialApplyHealthScope {
		t.Fatal("initial-health must remain default-off", w.Code, w.Body.String())
	}
	if err := f.s.EnableMeteredHealth(roundEndpoint()); err == nil {
		t.Fatal("first-Apply opt-in weakened two-Node handover gate")
	}
	if err := f.s.EnableInboundInitialHealth(mx.ProtocolHealthEndpoint{}); err == nil || f.s.initialApplyHealthEnabled || f.s.healthEndpoint != nil {
		t.Fatal("invalid verifier enabled initial health")
	}
	if err := f.s.EnableLabSubscriptions(); err == nil {
		t.Fatal("subscription opt-in accepted without a proof scope")
	}
	if err := f.s.EnableInboundInitialHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	if f.s.healthRoundsEnabled || f.s.labSubscriptionsEnabled || f.s.consumerManifestsEnabled {
		t.Fatal("initial health granted another startup capability")
	}
	if err := f.s.EnableMeteredHealthRounds(); err == nil || f.s.healthRoundsEnabled {
		t.Fatal("single-Node initial proof enabled the handover round capability")
	}
	if err := f.s.EnableInboundInitialHealth(roundEndpoint()); err == nil {
		t.Fatal("initial health allowed reconfiguration")
	}
	if err := f.s.EnableLabSubscriptions(); err != nil {
		t.Fatal("explicit initial proof subscription opt-in", err)
	}
	if f.s.healthRoundsEnabled || f.s.consumerManifestsEnabled {
		t.Fatal("initial proof delivery enabled handover or consumer manifests")
	}
	other := newPromotionAPIFixture(t, false)
	if err := other.s.EnableInboundInitialHealth(roundEndpoint()); err == nil {
		t.Fatal("unreserved Node enabled first-Apply health")
	}
}

func TestInitialApplyHealthOwnerPreparationAndStageGate(t *testing.T) {
	f := newPromotionAPIFixture(t, true)
	if err := f.s.EnableInboundInitialHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	promotion := promoteInitialHealthFixture(t, &f)
	get := func() business.InitialApplyHealthState {
		t.Helper()
		w := browserRequest(t, f.s.Handler(), http.MethodGet, initialHealthBase(), nil, f.cookie, "", "")
		var state struct {
			business.InitialApplyHealthState
			Enabled   bool `json:"enabled"`
			Available bool `json:"available"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil || !state.Enabled || !state.Available || state.PromotionEventID != promotion.EventID {
			t.Fatal("initial-health discovery", w.Code, w.Body.String())
		}
		for _, secret := range []string{f.clients[0].UUID, f.clients[1].UUID, "private_key", "\"config\":"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("health discovery leaked credentials")
			}
		}
		return state.InitialApplyHealthState
	}
	state := get()
	if state.Prepared || !state.CanPrepare || state.Eligible || state.InitialApplySHA != "" {
		t.Fatal("promotion claimed runtime proof before preparation/Apply", state)
	}
	stage := business.MeteredRouteCommandRequest{ID: fmt.Sprintf("%032x", 1930), Action: "stage", RuntimeInstance: f.status.Instance, Expected: mx.Condition(f.status.Observed), Usage: f.status.Usage, Drafts: []business.RouteDraftRef{{InboundID: 8123, InboundRevision: promotion.InboundRevision, DraftRevision: promotion.DraftRevision}}}
	path := "/api/v1/workspace/native-nodes/" + f.node.NodeID + "/metered-route-transactions"
	if w := f.post(t, path, stage); w.Code != http.StatusConflict {
		t.Fatal("Stage bypassed explicit probe preparation", w.Code, w.Body.String())
	}
	input := map[string]any{"event_id": fmt.Sprintf("%032x", 1940), "promotion_event_id": promotion.EventID, "confirmed": true}
	w := f.post(t, initialHealthBase()+"/prepare", input)
	var prep business.InitialApplyHealthPreparation
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &prep) != nil || !prep.Prepared || prep.Replayed || prep.PromotionEventID != promotion.EventID || prep.InboundID != 8123 || prep.NodeID != f.node.NodeID || prep.InstallationID != f.node.InstallationID {
		t.Fatal("explicit preparation", w.Code, w.Body.String())
	}
	state = get()
	if !state.Prepared || state.CanPrepare || state.Eligible || state.Preparation == nil || state.Preparation.EventID != prep.EventID {
		t.Fatal("preparation did not stay non-activating", state)
	}
	if w = f.post(t, path, stage); w.Code != http.StatusAccepted {
		t.Fatal("prepared Stage blocked", w.Code, w.Body.String())
	}
	w = f.post(t, initialHealthBase()+"/prepare", input)
	var replay business.InitialApplyHealthPreparation
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &replay) != nil || !replay.Replayed || replay.CreatedUnixNano != prep.CreatedUnixNano {
		t.Fatal("exact preparation replay after Stage", w.Code, w.Body.String())
	}
	if w = f.post(t, initialHealthBase()+"/rounds", map[string]any{"id": fmt.Sprintf("%032x", 1941), "initial_apply_sha": strings.Repeat("a", 64), "confirmed": true}); w.Code != http.StatusConflict {
		t.Fatal("Stage mistaken for first Apply proof", w.Code, w.Body.String())
	}
}

func TestInitialApplyHealthWireAndOwnerBoundaries(t *testing.T) {
	f := newPromotionAPIFixture(t, true)
	if err := f.s.EnableInboundInitialHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	promotion := promoteInitialHealthFixture(t, &f)
	h := f.s.Handler()
	valid := map[string]any{"event_id": fmt.Sprintf("%032x", 1950), "promotion_event_id": promotion.EventID, "confirmed": true}
	for _, tc := range []struct {
		method, path string
		cookie       *http.Cookie
		csrf, origin string
		want         int
	}{
		{"GET", initialHealthBase(), nil, "", "", 401},
		{"POST", initialHealthBase() + "/prepare", nil, f.csrf, "https://" + browserHost, 401},
		{"POST", initialHealthBase() + "/prepare", f.cookie, "wrong", "https://" + browserHost, 403},
		{"POST", initialHealthBase() + "/prepare", f.cookie, f.csrf, "https://foreign.example.org", 403},
		{"GET", initialHealthBase() + "?refresh=1", f.cookie, "", "", 422},
		{"GET", "/api/v1/workspace/native-inbound-plans/08123/initial-health", f.cookie, "", "", 422},
		{"GET", initialHealthBase() + "/rounds/" + strings.Repeat("A", 32), f.cookie, "", "", 422},
	} {
		w := browserRequest(t, h, tc.method, tc.path, valid, tc.cookie, tc.csrf, tc.origin)
		if w.Code != tc.want {
			t.Fatal("initial health authorization", tc.path, w.Code, w.Body.String())
		}
	}
	for _, raw := range []string{
		`{"event_id":"` + fmt.Sprintf("%032x", 1950) + `","promotion_event_id":"` + promotion.EventID + `","confirmed":false}`,
		`{"event_id":"` + fmt.Sprintf("%032x", 1950) + `","promotion_event_id":"` + promotion.EventID + `","confirmed":false,"confirmed":true}`,
		`{"event_id":"` + fmt.Sprintf("%032x", 1950) + `","promotion_event_id":"` + promotion.EventID + `","confirmed":true,"verifier":"https://example.org"}`,
		`{"event_id":"` + fmt.Sprintf("%032x", 1950) + `","promotion_event_id":null,"confirmed":true}`,
		`{"event_id":"` + fmt.Sprintf("%032x", 1950) + `","confirmed":true}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "https://"+browserHost+initialHealthBase()+"/prepare", bytes.NewBufferString(raw))
		r.Host = browserHost
		r.AddCookie(f.cookie)
		r.Header.Set("Origin", "https://"+browserHost)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatal("ambiguous preparation accepted", w.Code, w.Body.String())
		}
	}
	for _, path := range []string{initialHealthBase() + "/rounds", initialHealthBase() + "/snapshot"} {
		if w := f.post(t, path, map[string]any{"confirmed": true}); w.Code != http.StatusUnprocessableEntity {
			t.Fatal("missing exact reviewed identity accepted", path, w.Code)
		}
	}
	valid["promotion_event_id"] = strings.Repeat("f", 32)
	if w := f.post(t, initialHealthBase()+"/prepare", valid); w.Code != http.StatusConflict {
		t.Fatal("foreign promotion prepared", w.Code, w.Body.String())
	}
}
