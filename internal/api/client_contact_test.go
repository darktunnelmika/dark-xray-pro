package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestClientContactBrowserScopeCSRFAndPermissions(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"contact-a", "contact-b"} {
		if _, e := s.business.CreateRepresentative(ctx, id, id); e != nil {
			t.Fatal(e)
		}
		if _, e := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "api-contact-grant-" + id, RepresentativeID: id, VolumeDeltaBytes: 1000, Reason: "test"}); e != nil {
			t.Fatal(e)
		}
		if _, e := s.business.CreateClient(ctx, business.NewClient{EventID: "api-contact-create-" + id, ClientID: "client-" + id, RepresentativeID: id, Name: id, DurationDays: 30, VolumeBytes: 100}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.business.ProvisionRepresentativeLogin(ctx, "contact-a", "contact-login", "private-contact-password-0001"); e != nil {
		t.Fatal(e)
	}
	cookie, csrf := browserLogin(t, s, "contact-login", "private-contact-password-0001")
	path := "/api/v1/workspace/clients/client-contact-a/contact"
	input := business.ClientContactChange{EventID: "api-contact-update-0001", ClientContactInput: business.ClientContactInput{TelegramUsername: "@Alice", TelegramID: "1234567"}}
	if w := browserRequest(t, s.Handler(), "POST", path, input, nil, "", ""); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", path, input, cookie, "", "https://"+browserHost); w.Code != 403 {
		t.Fatal("csrf", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", path, input, cookie, csrf, "https://evil.example"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/clients/client-contact-b/contact", input, cookie, csrf, "https://"+browserHost); w.Code != 403 {
		t.Fatal("cross tenant", w.Code, w.Body.String())
	}
	if w := browserRequest(t, s.Handler(), "POST", path, input, cookie, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatal("valid", w.Code, w.Body.String())
	}
	if w := browserRequest(t, s.Handler(), "POST", path, input, cookie, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatal("replay", w.Code)
	}
	if permission, ok := requiredRepresentativePermission(httptest.NewRequest("POST", path, nil)); !ok || permission != "edit_clients" {
		t.Fatal("permission mapping", permission, ok)
	}
}
