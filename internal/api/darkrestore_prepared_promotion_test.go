package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestDarkRestorePreparedPromotionWireAndStartupGate(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		f := newPromotionAPIFixture(t, enabled)
		handoff := strings.Repeat("a", 64)
		in := darkRestorePromotionRequest{HandoffSHA: handoff, EventID: "prepared-wire-test", PromotionEventID: "prepared-native-wire-test", InboundID: 8123, PlanRevision: f.plan.PlanRevision, InboundRevision: f.plan.InboundRevision, Reason: "Owner reviewed synthetic transfer"}
		raw, _ := json.Marshal(in)
		for _, tc := range []struct {
			name, body string
			preview    bool
			want       int
		}{
			{"unknown client", strings.TrimSuffix(string(raw), "}") + `,"client_id":"foreign"}`, true, 422},
			{"duplicate", strings.TrimSuffix(string(raw), "}") + `,"inbound_id":8123}`, true, 422},
			{"wrong handoff", strings.Replace(string(raw), handoff, strings.Repeat("b", 64), 1), true, 422},
			{"commit unconfirmed", string(raw), false, 422},
			{"preview confirmed", strings.Replace(string(raw), `"confirmed":false`, `"confirmed":true`, 1), true, 422},
			{"oversized", strings.Replace(string(raw), in.Reason, strings.Repeat("z", 5000), 1), true, 422},
		} {
			t.Run(tc.name+map[bool]string{true: "-enabled", false: "-disabled"}[enabled], func(t *testing.T) {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("POST", restoreBase+"/handoff/promotion", strings.NewReader(tc.body))
				f.s.prepareDarkRestorePromotion(w, r, f.cookie.Value, f.clients[0].ID, handoff, tc.preview)
				if w.Code != tc.want {
					t.Fatal(w.Code, w.Body.String())
				}
				f.assertPaused(t)
			})
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", restoreBase+"/handoff/promotion/preview", strings.NewReader(string(raw)))
		f.s.prepareDarkRestorePromotion(w, r, f.cookie.Value, f.clients[0].ID, handoff, true)
		if w.Code < 400 {
			t.Fatal("native client without imported prepared receipt was accepted", w.Code, w.Body.String())
		}
		if !enabled && (w.Code != 409 || !strings.Contains(w.Body.String(), "promotion_disabled")) {
			t.Fatal("startup gate bypass", w.Code, w.Body.String())
		}
		f.assertPaused(t)
		for _, before := range f.clients {
			after, err := f.s.business.Client(context.Background(), before.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejection changed client", err)
			}
		}
	}
}

func TestDarkRestorePreparedPromotionOwnerHTTPBoundary(t *testing.T) {
	f := stagedRestoreAPI(t)
	// An invalid staged file deliberately proves auth/query checks run before
	// reading server evidence. Positive receipt semantics are tested in business.
	f.s.darkRestoreHandoff = &restoreHandoffStage{path: "/nonexistent/synthetic-handoff", digest: strings.Repeat("a", 64)}
	h := f.s.BrowserHandler()
	origin := "https://" + browserHost
	for _, path := range []string{restoreBase + "/handoff/promotion", restoreBase + "/handoff/promotion/preview"} {
		if w := browserRequest(t, h, "POST", path, map[string]any{}, f.target, f.targetCSRF, origin); w.Code != 403 {
			t.Fatal("representative reached promotion", w.Code)
		}
		if w := browserRequest(t, h, "POST", path, map[string]any{}, f.owner, "", origin); w.Code != 403 {
			t.Fatal("missing CSRF reached promotion", w.Code)
		}
		if w := browserRequest(t, h, "POST", path+"?client_id=foreign", map[string]any{}, f.owner, f.ownerCSRF, origin); w.Code != 422 {
			t.Fatal("query client accepted", w.Code)
		}
	}
	if w := browserRequest(t, h, "GET", restoreBase+"/handoff/promotion", nil, f.target, "", ""); w.Code != 403 {
		t.Fatal("representative read receipt", w.Code)
	}
	if w := browserRequest(t, h, "GET", restoreBase+"/handoff/promotion?client_id=foreign", nil, f.owner, "", ""); w.Code != 422 {
		t.Fatal("query receipt client accepted", w.Code)
	}
}
