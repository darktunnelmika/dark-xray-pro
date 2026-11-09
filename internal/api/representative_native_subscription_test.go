package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestNativeSubscriptionRepresentativeRoutePermissions(t *testing.T) {
	base := "/api/v1/workspace/clients/own/inbound-subscription"
	for _, tc := range []struct {
		method, suffix, key string
		allowed bool
	}{
		{"GET", "", "view_clients", true},
		{"GET", "/export", "view_clients", true},
		{"POST", "/issue", "manage_clients", true},
		{"POST", "/rotate", "manage_clients", true},
		{"POST", "/revoke", "manage_clients", true},
		{"POST", "/reveal", "manage_clients", true},
		{"POST", "/unknown", "", false},
		{"POST", "/issue/extra", "", false},
		{"POST", "", "", false},
		{"DELETE", "/revoke", "", false},
		{"GET", "/reveal", "", false},
	} {
		r := httptest.NewRequest(tc.method, base+tc.suffix, nil)
		key, allowed := requiredRepresentativePermission(r)
		if key != tc.key || allowed != tc.allowed {
			t.Fatalf("%s %s: got %q/%v want %q/%v", tc.method, tc.suffix, key, allowed, tc.key, tc.allowed)
		}
	}
}

func TestNativeSubscriptionRepresentativeOwnLifecycleAndForeignDenial(t *testing.T) {
	s, _, _, foreign := subscriptionViewAPIFixture(t)
	ctx := context.Background()
	if _, err := s.business.CreateRepresentative(ctx, "native-rep", "Native representative"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
		EventID: "native-rep-credit-0001", RepresentativeID: "native-rep",
		VolumeDeltaBytes: 1 << 30, Reason: "Synthetic test credit",
	}); err != nil {
		t.Fatal(err)
	}
	own, err := s.business.CreateClient(ctx, business.NewClient{
		EventID: "native-rep-client-0001", ClientID: "native-rep-client",
		RepresentativeID: "native-rep", Name: "Own client", DurationDays: 30, VolumeBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "native-rep", "native-rep-login", "native-rep-test-password-123"); err != nil {
		t.Fatal(err)
	}
	applySubscriptionViewFixture(t, s, own)
	cookie, csrf := browserLogin(t, s, "native-rep-login", "native-rep-test-password-123")
	h := s.BrowserHandler()
	base := "/api/v1/workspace/clients/" + own.ID + "/inbound-subscription"
	foreignBase := "/api/v1/workspace/clients/" + foreign.ID + "/inbound-subscription"
	for _, action := range []string{"issue", "rotate", "revoke", "reveal"} {
		body := map[string]any{"event_id": "native-forbidden-event-0001", "expected_generation": 0}
		if action == "reveal" {
			body = map[string]any{"expected_generation": 1}
		}
		if w := browserRequest(t, h, "POST", foreignBase+"/"+action, body, cookie, csrf, "https://"+browserHost); w.Code != http.StatusForbidden {
			t.Fatalf("foreign %s: %d", action, w.Code)
		}
		if w := browserRequest(t, h, "POST", base+"/"+action, body, cookie, "", "https://"+browserHost); w.Code != http.StatusForbidden {
			t.Fatalf("missing CSRF %s: %d", action, w.Code)
		}
	}
	issue := map[string]any{"event_id": "native-rep-issue-0001", "expected_generation": 0}
	for attempt := 0; attempt < 2; attempt++ {
		w := browserRequest(t, h, "POST", base+"/issue", issue, cookie, csrf, "https://"+browserHost)
		var receipt business.SubscriptionTokenReceipt
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Generation != 1 || receipt.Replayed != (attempt == 1) {
			t.Fatalf("own issue/replay: %d", w.Code)
		}
	}
	if w := browserRequest(t, h, "POST", base+"/reveal", map[string]any{"expected_generation": 1}, cookie, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatalf("own reveal: %d", w.Code)
	}
	for i, action := range []string{"rotate", "revoke"} {
		body := map[string]any{"event_id": "native-rep-"+action+"-0001", "expected_generation": i+1}
		if w := browserRequest(t, h, "POST", base+"/"+action, body, cookie, csrf, "https://"+browserHost); w.Code != 200 {
			t.Fatalf("own %s: %d", action, w.Code)
		}
	}
	permissions := business.DefaultRepresentativePermissions()
	permissions.ManageClients = false
	if _, err := s.business.UpdateRepresentativeControl(ctx, business.RepresentativeControlChange{
		EventID: "native-rep-permissions-0001", RepresentativeID: "native-rep", ExpectedRevision: 0,
		DisplayName: "Native representative", Permissions: permissions, Reason: "Revoke subscription management",
	}); err != nil {
		t.Fatal(err)
	}
	cookie, csrf = browserLogin(t, s, "native-rep-login", "native-rep-test-password-123")
	for _, action := range []string{"issue", "rotate", "revoke", "reveal"} {
		w := browserRequest(t, h, "POST", base+"/"+action, map[string]any{"expected_generation": 3}, cookie, csrf, "https://"+browserHost)
		if w.Code != 403 {
			t.Fatalf("disabled manage_clients %s: %d", action, w.Code)
		}
	}
}
