package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestAuthenticatedBulkRenewalQuoteCommitScopeAndReplay(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"v7repA", "v7repB"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Private "+id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
			EventID: "v7-api-grant-event-" + id, RepresentativeID: id,
			VolumeDeltaBytes: 5000, UnlimitedDeltaUnits: 50, Reason: "API test"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.CreateClient(ctx, business.NewClient{
			EventID: "v7-api-client-event-" + id, ClientID: "v7-client-" + id, RepresentativeID: id,
			Name: "QA " + id, DurationDays: 30, VolumeBytes: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx,
		"v7repA", "v7-representative-user", "private-bulk-renewal-password-123"); err != nil {
		t.Fatal(err)
	}
	ownerCookie, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	repCookie, repCSRF := browserLogin(t, s, "v7-representative-user", "private-bulk-renewal-password-123")
	quotePath := "/api/v1/workspace/clients/bulk/extend/quote"
	commitPath := "/api/v1/workspace/clients/bulk/extend/commit"
	intent := business.BulkExtensionIntent{
		ClientIDs:      []string{"v7-client-v7repA", "v7-client-v7repB"},
		AdditionalDays: 30, Reason: "Batch extension verification",
	}
	call := func(path string, cookie *http.Cookie, csrf string, body any, origin string) *httpResponse {
		r := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, origin)
		return &httpResponse{Code: r.Code, Body: r.Body.String()}
	}
	for _, path := range []string{quotePath, commitPath} {
		if got := call(path, nil, "", intent, "").Code; got != 401 {
			t.Fatalf("anonymous reached %s: %d", path, got)
		}
		if got := call(path, repCookie, "", intent, "https://"+browserHost).Code; got != 403 {
			t.Fatalf("missing CSRF %s %d", path, got)
		}
		if got := call(path, repCookie, repCSRF, intent, "https://evil.test").Code; got != 403 {
			t.Fatalf("cross-origin %s %d", path, got)
		}
		if got := browserRequest(t, s.AgentHandler(), "POST", path, intent, ownerCookie, ownerCSRF, "https://"+browserHost).Code; got == 200 {
			t.Fatalf("Agent exported financial endpoint %s", path)
		}
	}
	if got := call(quotePath, repCookie, repCSRF, intent, "https://"+browserHost).Code; got != 403 {
		t.Fatalf("representative quoted foreign client: %d", got)
	}
	if got := call(commitPath, repCookie, repCSRF, intent, "https://"+browserHost).Code; got != 403 {
		t.Fatalf("representative attempted cross-owner billable mutation %d", got)
	}
	own := business.BulkExtensionIntent{ClientIDs: []string{"v7-client-v7repA"}, AdditionalDays: 30, Reason: "Only own credit"}
	result := call(quotePath, repCookie, repCSRF, own, "https://"+browserHost)
	if result.Code != 200 {
		t.Fatalf("rep scoped quote %d %s", result.Code, result.Body)
	}
	var q business.BulkExtensionQuote
	if err := json.Unmarshal([]byte(result.Body), &q); err != nil {
		t.Fatal(err)
	}
	if !q.Sufficient || q.Count != 1 || q.Representatives[0].AvailableVolumeBytes != "4900" || q.AppliedToXray {
		t.Fatalf("inaccurate quote: %+v", q)
	}
	before, _ := s.business.Representative(ctx, "v7repA")
	if before.VolumeCreditBytes != 4900 {
		t.Fatal("quote changed credit", before)
	}
	commit := business.BulkExtensionCommit{BulkExtensionIntent: own, EventID: "v7-api-commit-event-00001",
		QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}
	if res := call(commitPath, repCookie, repCSRF, commit, "https://"+browserHost); res.Code != 200 {
		t.Fatalf("commit failed %d %s", res.Code, res.Body)
	}
	commit.QuoteUnix = 1 // check exact ack-loss replay after quote expiry
	if res := call(commitPath, repCookie, repCSRF, commit, "https://"+browserHost); res.Code != 200 {
		t.Fatalf("replay returned %d %s", res.Code, res.Body)
	} else {
		var r business.BulkExtensionResult
		if err := json.Unmarshal([]byte(res.Body), &r); err != nil || !r.Replayed || r.AppliedToXray || r.ClientOrUsageHistoryDeleted {
			t.Fatalf("replay unsafe: %+v %v", r, err)
		}
	}
	client, err := s.business.Client(ctx, "v7-client-v7repA")
	if err != nil || client.DurationDays != 60 {
		t.Fatal("client extension missing", err)
	}
	after, err := s.business.Representative(ctx, "v7repA")
	if err != nil || after.VolumeCreditBytes != 4900 {
		t.Fatal("volume changed for duration-only update", after)
	}
	ops, err := s.business.ListClientOperations(ctx, "v7repA", 50, 0)
	if err != nil || len(ops) != 1 {
		t.Fatal("duplicate persisted operation after replay", len(ops), err)
	}
	// Old read-only client and usage endpoints must still be accessible.
	if got := browserRequest(t, s.Handler(), "GET", "/api/v1/workspace/clients/v7-client-v7repA",
		nil, repCookie, "", "").Code; got != 200 {
		t.Fatalf("client history inaccessible: %d", got)
	}
	if got := call(quotePath, ownerCookie, ownerCSRF, intent, "https://"+browserHost).Code; got != 200 {
		t.Fatalf("owner unable to quote multiple reps %d", got)
	}
	// Unknown/malformed quote data must never charge.
	if got := call(quotePath, ownerCookie, ownerCSRF, map[string]any{
		"client_ids": []string{"v7-client-v7repA"}, "additional_days": 1, "reason": "test", "extra_field": true,
	}, "https://"+browserHost).Code; got != 400 {
		t.Fatalf("unknown quote input accepted %d", got)
	}
	if got := call(quotePath, ownerCookie, ownerCSRF, business.BulkExtensionIntent{
		ClientIDs: []string{"v7-client-v7repA", "v7-client-v7repA"}, AdditionalDays: 1, Reason: "duplicate",
	}, "https://"+browserHost).Code; got != 422 {
		t.Fatalf("duplicate clients accepted %d", got)
	}
}
