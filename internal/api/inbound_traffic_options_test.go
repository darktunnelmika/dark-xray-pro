package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestCreationPlanAPITrafficOptionsStrictWireIntentAndHistoricalReplay(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	capability := browserRequest(t, s.Handler(), "GET", base+"/capabilities", nil, cookie, "", "")
	if capability.Code != http.StatusOK || !strings.Contains(capability.Body.String(), `"profile_sniffing":true`) || !strings.Contains(capability.Body.String(), `"profile_fallbacks":true`) {
		t.Fatal("traffic options capability absent", capability.Code)
	}
	in.Profile.Options = &xrayconfig.ProfileOptions{RealityMaxTimeDiff: 60000,
		Sniffing:  &xrayconfig.SniffingOptions{Enabled: true, DestOverride: []string{"http", "tls"}, RouteOnly: true, DomainsExcluded: []string{"full:excluded.example.org"}},
		Fallbacks: []xrayconfig.Fallback{{Dest: "8080"}, {Path: "/health", Dest: "127.0.0.1:8081", Xver: 1}},
	}
	created := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
	var receipt business.NativeInboundPlanReceipt
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &receipt) != nil || receipt.Plan.Profile.Options == nil || receipt.Plan.Profile.Options.Sniffing == nil || len(receipt.Plan.Profile.Options.Fallbacks) != 2 {
		t.Fatal("HTTP create lost traffic options", created.Code, created.Body.String())
	}
	get := browserRequest(t, s.Handler(), "GET", base+"/501", nil, cookie, "", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"full:excluded.example.org"`) || !strings.Contains(get.Body.String(), `"/health"`) {
		t.Fatal("HTTP reopen lost traffic settings", get.Code)
	}
	editBody := func(options any) map[string]any {
		t.Helper()
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		body["event_id"] = "traffic-api-edit-0001"
		body["expected_plan_revision"], body["expected_inbound_revision"] = 1, 1
		body["profile"].(map[string]any)["options"] = options
		return body
	}
	for _, tc := range []struct {
		name    string
		options any
		status  int
		code    string
	}{
		{"old scalar browser", map[string]any{"reality_max_time_diff": 60000}, 409, "profile_sniffing_required"},
		{"old empty options", map[string]any{}, 409, "profile_sniffing_required"},
		{"null sniffing", map[string]any{"sniffing": nil, "fallbacks": []any{}}, 409, "profile_sniffing_required"},
		{"omitted fallback", map[string]any{"sniffing": map[string]any{}}, 409, "profile_fallbacks_required"},
		{"null fallback", map[string]any{"sniffing": map[string]any{}, "fallbacks": nil}, 409, "profile_fallbacks_required"},
		{"unknown sniffing key", map[string]any{"sniffing": map[string]any{"enabled": true, "unknown": true}}, 422, ""},
		{"mistyped sniffing flag", map[string]any{"sniffing": map[string]any{"enabled": "true"}}, 422, ""},
		{"unknown fallback key", map[string]any{"fallbacks": []any{map[string]any{"dest": "8080", "unknown": true}}}, 422, ""},
		{"invalid regular expression", map[string]any{"sniffing": map[string]any{"enabled": true, "domains_excluded": []string{"regexp:(?=unsupported)"}}}, 422, "sniffing_invalid"},
		{"invalid fallback destination", map[string]any{"fallbacks": []any{map[string]any{"dest": "http://example.org:80"}}}, 422, "fallbacks_invalid"},
		{"unsupported REALITY ALPN", map[string]any{"fallbacks": []any{map[string]any{"dest": "8080", "alpn": "http/1.1"}}}, 422, "fallbacks_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := browserRequest(t, s.Handler(), "PUT", base+"/501", editBody(tc.options), cookie, csrf, "https://"+browserHost)
			if response.Code != tc.status || tc.code != "" && !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatal("wire validation/intent mismatch", response.Code, response.Body.String())
			}
		})
	}
	// Empty fallback slices must be sent as [] on the wire, not omitted by a
	// server-side omitempty encoder. This also proves presence survives the
	// API's replay check and the second normalization inside the save path.
	clearBody := editBody(map[string]any{"sniffing": map[string]any{}, "fallbacks": []any{}})
	clearBody["event_id"] = "traffic-api-clear-0001"
	preview := browserRequest(t, s.Handler(), "POST", base+"/preview", clearBody, cookie, csrf, "https://"+browserHost)
	if preview.Code != http.StatusOK {
		t.Fatal("explicit clear preview", preview.Code, preview.Body.String())
	}
	cleared := browserRequest(t, s.Handler(), "PUT", base+"/501", clearBody, cookie, csrf, "https://"+browserHost)
	var update business.NativeInboundPlanReceipt
	if cleared.Code != http.StatusOK || json.Unmarshal(cleared.Body.Bytes(), &update) != nil || update.Plan.Profile.Options != nil || update.Plan.InboundRevision != 2 || update.Plan.Profile.PublicKey != receipt.Plan.Profile.PublicKey {
		t.Fatal("explicit wire reset did not clear exactly", cleared.Code, cleared.Body.String())
	}
	replay := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
	var history business.NativeInboundPlanReceipt
	if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &history) != nil || !history.Replayed || history.Plan.InboundRevision != 1 || history.Plan.Profile.Options == nil || len(history.Plan.Profile.Options.Fallbacks) != 2 {
		t.Fatal("historical receipt lost the original traffic settings", replay.Code, replay.Body.String())
	}
}
