package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestP0ENativeOwnerPreviewSaveAndSecurity(t *testing.T) {
	ctx := context.Background()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0xaa}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct{ id, name, install, token string }{
		{"nl", "Upstream Netherlands", strings.Repeat("a", 32), strings.Repeat("2", 64)},
		{"de", "Downstream Germany", strings.Repeat("b", 32), strings.Repeat("3", 64)},
	} {
		ticket, err := s.registry.Issue(e.id, e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.registry.Claim(ticket.PairCode, e.id, e.install, e.token); err != nil {
			t.Fatal(err)
		}
	}
	in := business.InboundInput{ID: 1185, Name: "Main Reality", Transport: "tcp",
		RealityTarget: "www.example.org:443", ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aa11"}}
	if _, err := s.business.CreateInbound(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	for _, target := range []business.InboundTarget{
		{InboundID: 1185, NodeID: "nl", InstallationID: strings.Repeat("a", 32),
			ListenIP: "198.51.100.10", SwapPort: 26000},
		{InboundID: 1185, NodeID: "de", InstallationID: strings.Repeat("b", 32),
			ListenIP: "198.51.100.20", DirectPort: 21185, SwapPort: 26001},
	} {
		if err := s.business.PutInboundTarget(ctx, target); err != nil {
			t.Fatal(err)
		}
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	base := "/api/v1/workspace/native-inbounds/1185/route-drafts"
	input := business.NativeRouteDraftInput{
		InboundID: 1185, NodeID: "de", InstallationID: strings.Repeat("b", 32),
		ExpectedInboundRevision: 1, DirectPort: 21185,
		DirectHosts: []string{"de.example.org"},
		SwapEnabled: true, SwapUpstreamNodeID: "nl", SwapTunnelHost: "nl-tunnel.example.org",
		SwapTunnelPort: 26000, SwapNodePort: 26001,
		EventID: "native-route-api-safe-draft-0001", Reason: "Owner reviewed draft only",
	}
	post := func(path string, body any, actor *http.Cookie, token, origin string) int {
		w := browserRequest(t, s.Handler(), "POST", path, body, actor, token, origin)
		return w.Code
	}
	if code := post(base+"/preview", input, nil, csrf, "https://"+browserHost); code != 401 {
		t.Fatal("anonymous route preview succeeded", code)
	}
	if code := post(base+"/preview", input, cookie, "bad", "https://"+browserHost); code != 403 {
		t.Fatal("CSRF bypassed", code)
	}
	if code := post(base+"/preview", input, cookie, csrf, "https://evil.example"); code != 403 {
		t.Fatal("foreign origin bypassed", code)
	}
	if code := post(base+"/preview", input, cookie, csrf, "https://"+browserHost); code != 200 {
		t.Fatal("read-only preview rejected", code)
	}
	if code := post(base, input, cookie, csrf, "https://"+browserHost); code != 201 {
		t.Fatal("owner Save draft rejected", code)
	}
	if code := post(base, input, cookie, csrf, "https://"+browserHost); code != 200 {
		t.Fatal("same-event retry created a duplicate", code)
	}
	w := browserRequest(t, s.Handler(), "GET", base+"/de", nil, cookie, "", "")
	if w.Code != 200 {
		t.Fatal("owner cannot read saved draft", w.Code)
	}
	var draft business.NativeRouteDraft
	if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil || draft.ApplyAllowed ||
		draft.AgentAckVerified || draft.SubscriptionPublished || draft.BillingVerified ||
		draft.BillingSourceNodeID != "de" {
		t.Fatal("draft falsely marked applied", draft, err)
	}
	if code := post(base, struct {
		Unexpected string `json:"unexpected"`
	}{Unexpected: "yes"},
		cookie, csrf, "https://"+browserHost); code != 422 {
		t.Fatal("incomplete request was not rejected", code)
	}
	if code := browserRequest(t, s.AgentHandler(), "GET", base+"/de", nil, cookie, "", "").Code; code == 200 {
		t.Fatal("Agent TLS exposed owner route draft")
	}
	if _, err := s.business.CreateRepresentative(ctx, "rep-preview", "Rep Preview"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "rep-preview",
		"rep-route-preview", "password-for-route-preview-123"); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, s, "rep-route-preview", "password-for-route-preview-123")
	if code := browserRequest(t, s.Handler(), "GET", base+"/de", nil, repCookie, "", "").Code; code != 403 {
		t.Fatal("representative read privileged route draft", code)
	}
	if code := post(base, input, repCookie, csrf, "https://"+browserHost); code != 403 {
		t.Fatal("representative saved privileged route draft", code)
	}
}
