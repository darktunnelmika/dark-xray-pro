package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestProfileEditBrowserOwnerRepresentativeCSRFAndPrivateAudit(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"profile-a", "profile-b"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Representative "+id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
			EventID: "v8-api-profile-grant-" + id, RepresentativeID: id,
			UnlimitedDeltaUnits: 100, VolumeDeltaBytes: 10 << 30, Reason: "QA only",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.CreateClient(ctx, business.NewClient{
			EventID: "v8-api-profile-create-" + id, ClientID: "client-" + id,
			RepresentativeID: id, Name: "Before " + id, IPCount: 2, DurationDays: 30, Unlimited: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "profile-a", "profile-editor-login", "private-editor-login-password-0001"); err != nil {
		t.Fatal(err)
	}
	owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "profile-editor-login", "private-editor-login-password-0001")
	quoteURL := "/api/v1/workspace/clients/client-profile-a/edit/quote"
	commitURL := "/api/v1/workspace/clients/client-profile-a/edit/commit"
	historyURL := "/api/v1/workspace/clients/client-profile-a/edits?limit=25&offset=0"
	intent := business.ClientProfileEditIntent{
		ClientID: "client-profile-a", Name: "Changed QA name", PlanKind: "unlimited",
		IPCount: 4, Reason: "Paid upgrade",
	}
	post := func(path string, cookie *http.Cookie, csrf string, body any, origin string) (int, string) {
		w := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, origin)
		return w.Code, w.Body.String()
	}
	for _, path := range []string{quoteURL, commitURL} {
		if code, _ := post(path, nil, "", intent, ""); code != 401 {
			t.Fatalf("anonymous request exposed %s", path)
		}
		if code, _ := post(path, rep, "", intent, "https://"+browserHost); code != 403 {
			t.Fatalf("missing CSRF permitted %s: %d", path, code)
		}
		if code, _ := post(path, rep, repCSRF, intent, "https://evil.example"); code != 403 {
			t.Fatalf("foreign origin permitted: %d", code)
		}
		agent := browserRequest(t, s.AgentHandler(), "POST", path, intent, rep, repCSRF, "https://"+browserHost)
		if agent.Code == 200 {
			t.Fatal("Node Agent saw private browser account edits")
		}
	}
	other := business.ClientProfileEditIntent{
		ClientID: "client-profile-b", Name: "Cross account", PlanKind: "unlimited", IPCount: 4,
		Reason: "Must refuse other representative",
	}
	if code, _ := post("/api/v1/workspace/clients/client-profile-b/edit/quote", rep, repCSRF, other, "https://"+browserHost); code != 403 {
		t.Fatalf("representative enumerated another client's profile: %d", code)
	}
	if code, _ := post(quoteURL, rep, repCSRF, map[string]any{
		"client_id": "client-profile-a", "name": "Unknown key", "ip_count": 3,
		"plan_kind": "unlimited", "reason": "Invalid", "unexpected": true,
	}, "https://"+browserHost); code != 400 {
		t.Fatalf("malformed request accepted: %d", code)
	}
	if code, _ := post(quoteURL, rep, repCSRF, other, "https://"+browserHost); code != 422 {
		t.Fatalf("path/client mismatch %d", code)
	}
	code, body := post(quoteURL, rep, repCSRF, intent, "https://"+browserHost)
	if code != 200 {
		t.Fatalf("valid edit quote: %d %s", code, body)
	}
	var quote business.ClientProfileEditQuote
	if err := json.Unmarshal([]byte(body), &quote); err != nil {
		t.Fatal(err)
	}
	if quote.UnlimitedCreditDebitUnits != "2" || quote.AppliedToXray || !quote.Sufficient {
		t.Fatal("quote shape/billing invalid", quote)
	}
	commit := business.ClientProfileEditCommit{
		ClientProfileEditIntent: intent, EventID: "v8-api-edit-commit-event-0001",
		QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix,
	}
	if code, body = post(commitURL, rep, repCSRF, commit, "https://"+browserHost); code != 200 {
		t.Fatalf("valid commit rejected: %d %s", code, body)
	}
	if code, body = post(commitURL, rep, repCSRF, commit, "https://"+browserHost); code != 200 {
		t.Fatalf("idempotent retry rejected: %d %s", code, body)
	}
	var receipt business.ClientProfileEditReceipt
	if err := json.Unmarshal([]byte(body), &receipt); err != nil || !receipt.Replayed || receipt.AppliedToXray {
		t.Fatalf("invalid repeated receipt %+v %v", receipt, err)
	}
	row, err := s.business.Client(ctx, intent.ClientID)
	if err != nil || row.Name != intent.Name || row.IPCount != 4 {
		t.Fatalf("client not edited once %+v %v", row, err)
	}
	for _, tc := range []struct {
		cookie *http.Cookie
		want   int
	}{
		{rep, 200}, {owner, 200}, {nil, 401},
	} {
		got := browserRequest(t, s.Handler(), "GET", historyURL, nil, tc.cookie, "", "")
		if got.Code != tc.want {
			t.Fatalf("audit isolation want %d got %d", tc.want, got.Code)
		}
		if tc.want == 200 {
			var history struct {
				Edits []business.ClientProfileEditAudit `json:"edits"`
			}
			if err := json.Unmarshal(got.Body.Bytes(), &history); err != nil || len(history.Edits) != 1 ||
				history.Edits[0].PreviousIPCount != 2 || history.Edits[0].NewIPCount != 4 {
				t.Fatal("real persisted audit missing", history, err)
			}
		}
	}
	wrongAudit := browserRequest(t, s.Handler(), "GET",
		"/api/v1/workspace/clients/client-profile-b/edits", nil, rep, "", "")
	if wrongAudit.Code != 403 {
		t.Fatalf("cross-rep history leaked: %d", wrongAudit.Code)
	}
	if got := browserRequest(t, s.AgentHandler(), "GET", historyURL, nil, owner, "", "").Code; got == 200 {
		t.Fatal("Agent TLS exported private audit")
	}
	if got := browserRequest(t, s.Handler(), "GET",
		"/api/v1/workspace/clients/client-profile-a/edits?limit=200", nil, owner, "", "").Code; got != 422 {
		t.Fatal("unbounded audit listing allowed", got)
	}
	_ = ownerCSRF
}
