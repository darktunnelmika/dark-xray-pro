package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

type ownershipAPI struct {
	s                                 *Server
	h                                 http.Handler
	owner, source, target             *http.Cookie
	ownerCSRF, sourceCSRF, targetCSRF string
}

func newOwnershipAPI(t *testing.T) ownershipAPI {
	return newOwnershipAPIWithServer(t, enabledOwner(t))
}
func newOwnershipAPIWithServer(t *testing.T, s *Server) ownershipAPI {
	t.Helper()
	ctx := context.Background()
	if e := s.business.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableClientOwnershipV1Lab(ctx); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"transfer-source", "transfer-target"} {
		if _, e := s.business.CreateRepresentative(ctx, id, id); e != nil {
			t.Fatal(e)
		}
		if _, e := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "ownership-api-fund-" + id, RepresentativeID: id, VolumeDeltaBytes: 1000, UnlimitedDeltaUnits: 100, Reason: "Synthetic funding"}); e != nil {
			t.Fatal(e)
		}
		if _, e := s.business.ProvisionRepresentativeLogin(ctx, id, id, repPassword); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.business.CreateClient(ctx, business.NewClient{EventID: "ownership-api-new-client-0001", ClientID: "transfer-client", RepresentativeID: "transfer-source", Name: "Transfer client", DurationDays: 30, VolumeBytes: 100, IPCount: 2}); e != nil {
		t.Fatal(e)
	}
	o, oc := browserLogin(t, s, ownerUsername, ownerPassword)
	a, ac := browserLogin(t, s, "transfer-source", repPassword)
	b, bc := browserLogin(t, s, "transfer-target", repPassword)
	return ownershipAPI{s, s.BrowserHandler(), o, a, b, oc, ac, bc}
}
func ownershipBody() business.ClientOwnershipIntent {
	return business.ClientOwnershipIntent{ClientIDs: []string{"transfer-client"}, RepresentativeID: "transfer-target", Reason: "Owner assigns existing customer"}
}
func ownershipAPIQuote(t *testing.T, f ownershipAPI) business.ClientOwnershipQuote {
	t.Helper()
	w := browserRequest(t, f.h, "POST", ownershipBase+"/quote", ownershipBody(), f.owner, f.ownerCSRF, "https://"+browserHost)
	var q business.ClientOwnershipQuote
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil || q.Count != 1 || q.BalanceChanged || q.UsageReset {
		t.Fatal("quote", w.Code, w.Body.String())
	}
	return q
}
func TestOwnershipAPITwoRepresentativesReallyChangeManagementNotBalances(t *testing.T) {
	f := newOwnershipAPI(t)
	ctx := context.Background()
	before, e := f.s.business.Client(ctx, "transfer-client")
	if e != nil {
		t.Fatal(e)
	}
	q := ownershipAPIQuote(t, f)
	request := business.ClientOwnershipCommit{ClientOwnershipIntent: q.Intent, EventID: "ownership-api-commit-0001", QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}
	w := browserRequest(t, f.h, "POST", ownershipBase+"/commit", request, f.owner, f.ownerCSRF, "https://"+browserHost)
	var receipt business.ClientOwnershipReceipt
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Count != 1 || receipt.Replayed || receipt.CredentialsChanged {
		t.Fatal(w.Code, w.Body.String())
	}
	current, e := f.s.business.Client(ctx, before.ID)
	before.RepresentativeID = "transfer-target"
	if e != nil || current != before {
		t.Fatal("identity changed", e)
	}
	for _, auth := range []struct {
		cookie *http.Cookie
		csrf   string
		want   int
	}{{f.source, f.sourceCSRF, 403}, {f.target, f.targetCSRF, 200}} {
		for _, suffix := range []string{"", "/usage"} {
			w = browserRequest(t, f.h, "GET", "/api/v1/workspace/clients/transfer-client"+suffix, nil, auth.cookie, "", "https://"+browserHost)
			if w.Code != auth.want {
				t.Fatal("scoped read", suffix, w.Code, w.Body.String())
			}
		}
		w = browserRequest(t, f.h, "POST", "/api/v1/workspace/clients/transfer-client/extend", business.ExtendClient{EventID: "ownership-api-renew-00001", ClientID: "transfer-client", AdditionalVolumeBytes: 25, AdditionalDays: 30, Reason: "New manager funds extension"}, auth.cookie, auth.csrf, "https://"+browserHost)
		if w.Code != auth.want {
			t.Fatal("scoped renewal", w.Code, w.Body.String())
		}
	}
	for _, pair := range []struct {
		id   string
		want int64
	}{{"transfer-source", 900}, {"transfer-target", 975}} {
		r, e := f.s.business.Representative(ctx, pair.id)
		if e != nil || r.VolumeCreditBytes != pair.want {
			t.Fatal("wrong credits", e)
		}
	}
	// Replaying the already completed transfer after a paid renewal returns its
	// original receipt and must not roll entitlement or counters backwards.
	w = browserRequest(t, f.h, "POST", ownershipBase+"/commit", request, f.owner, f.ownerCSRF, "https://"+browserHost)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	after, e := f.s.business.Client(ctx, "transfer-client")
	if e != nil || after.VolumeBytes != 125 || after.RepresentativeID != "transfer-target" {
		t.Fatal("replay replaced renewed client", e)
	}
	w = browserRequest(t, f.h, "GET", ownershipBase+"/receipts/"+request.EventID, nil, f.owner, "", "https://"+browserHost)
	if w.Code != 200 || !strings.Contains(w.Body.String(), request.EventID) {
		t.Fatal("receipt lookup", w.Code)
	}
	// The former manager must not obtain the current Client response by replaying
	// the original creation request, which predates ownership reassignment.
	w = browserRequest(t, f.h, "POST", "/api/v1/workspace/clients", business.NewClient{EventID: "ownership-api-new-client-0001", ClientID: "transfer-client", RepresentativeID: "transfer-source", Name: "Transfer client", DurationDays: 30, VolumeBytes: 100, IPCount: 2}, f.source, f.sourceCSRF, "https://"+browserHost)
	if w.Code != 403 {
		t.Fatal("creator replay leaked new management state", w.Code, w.Body.String())
	}
}
func TestOwnershipAPIOwnerOnlyCSRFOriginExactJSONAndAgentIsolation(t *testing.T) {
	f := newOwnershipAPI(t)
	q := ownershipAPIQuote(t, f)
	request := business.ClientOwnershipCommit{ClientOwnershipIntent: q.Intent, EventID: "ownership-api-strict-0001", QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}
	for _, route := range []struct {
		method, path string
		body         any
	}{{"POST", ownershipBase + "/quote", ownershipBody()}, {"POST", ownershipBase + "/commit", request}, {"GET", ownershipBase + "/receipts/" + request.EventID, nil}} {
		for _, auth := range []struct {
			cookie *http.Cookie
			csrf   string
			want   int
		}{{nil, "", 401}, {f.source, f.sourceCSRF, 403}, {f.target, f.targetCSRF, 403}} {
			w := browserRequest(t, f.h, route.method, route.path, route.body, auth.cookie, auth.csrf, "https://"+browserHost)
			if w.Code != auth.want {
				t.Fatal(route.path, w.Code, w.Body.String())
			}
		}
		w := browserRequest(t, f.s.AgentHandler(), route.method, route.path, route.body, f.owner, f.ownerCSRF, "https://"+browserHost)
		if w.Code == 200 {
			t.Fatal("Agent exposed owner transfer")
		}
	}
	for _, a := range []struct{ csrf, origin string }{{"", "https://" + browserHost}, {f.ownerCSRF, "https://foreign.example"}} {
		w := browserRequest(t, f.h, "POST", ownershipBase+"/commit", request, f.owner, a.csrf, a.origin)
		if w.Code != 403 {
			t.Fatal("CSRF/origin", w.Code)
		}
	}
	raw, _ := json.Marshal(ownershipBody())
	body := string(raw)
	for _, bad := range []string{
		strings.TrimSuffix(body, "}") + `,"representative_id":"transfer-source"}`,
		strings.TrimSuffix(body, "}") + `,"actor_id":"owner"}`,
		strings.Replace(body, `"client_ids"`, `"Client_IDs"`, 1),
		strings.Replace(body, `["transfer-client"]`, `null`, 1),
		strings.TrimSuffix(body, "}") + `,"path":"/etc/passwd"}`,
	} {
		r := httptest.NewRequest("POST", "https://"+browserHost+ownershipBase+"/quote", strings.NewReader(bad))
		r.AddCookie(f.owner)
		r.Header.Set("Origin", "https://"+browserHost)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.ownerCSRF)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("ambiguous JSON", w.Code, w.Body.String())
		}
	}
	for _, suffix := range []string{"?", "?actor_id=owner"} {
		w := browserRequest(t, f.h, "POST", ownershipBase+"/quote"+suffix, ownershipBody(), f.owner, f.ownerCSRF, "https://"+browserHost)
		if w.Code != 422 {
			t.Fatal("query", w.Code)
		}
	}
	c, e := f.s.business.Client(context.Background(), "transfer-client")
	if e != nil || c.RepresentativeID != "transfer-source" {
		t.Fatal("refused request mutated data", e)
	}
}
func TestOwnershipAPICapabilityDefaultOffAndOwnerOnly(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	o, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	if e := s.EnableClientOwnershipV1Lab(ctx); e == nil {
		t.Fatal("implicitly enabled ledger")
	}
	h := s.BrowserHandler()
	w := browserRequest(t, h, "GET", "/api/v1/workspace/me", nil, o, "", "https://"+browserHost)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"client_ownership_v1":false`) {
		t.Fatal(w.Body.String())
	}
	w = browserRequest(t, h, "POST", ownershipBase+"/quote", ownershipBody(), o, csrf, "https://"+browserHost)
	if w.Code != 404 {
		t.Fatal("default-on transfer route", w.Code)
	}
	f := newOwnershipAPI(t)
	for _, auth := range []struct {
		cookie *http.Cookie
		want   string
	}{{f.owner, `"client_ownership_v1":true`}, {f.source, `"client_ownership_v1":false`}} {
		w = browserRequest(t, f.h, "GET", "/api/v1/workspace/me", nil, auth.cookie, "", "https://"+browserHost)
		if w.Code != 200 || !strings.Contains(w.Body.String(), auth.want) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
