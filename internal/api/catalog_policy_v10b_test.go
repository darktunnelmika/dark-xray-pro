package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestV10bOwnerGrantedCatalogRightsAndScopedRepresentative(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"shop-a", "shop-b"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Seller "+id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "shop-a", "v10b-qa-login", "v10b-qa-password-test-123"); err != nil {
		t.Fatal(err)
	}
	owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "v10b-qa-login", "v10b-qa-password-test-123")
	get := func(path string, cookie *http.Cookie) (int, string) {
		w := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
		return w.Code, w.Body.String()
	}
	post := func(path string, cookie *http.Cookie, csrf string, body any, origin string) (int, string) {
		w := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, origin)
		return w.Code, w.Body.String()
	}
	policyURI := "/api/v1/workspace/representatives/shop-a/catalog-policy"
	policy := business.CatalogPolicyChange{
		CatalogPolicy: business.CatalogPolicy{
			RepresentativeID: "shop-a", ManageProducts: true, CanPublish: false,
			AllowVolume: true, AllowUnlimited: false,
			MinPriceToman: 20000, MaxPriceToman: 300000,
			MaxDurationDays: 60, MaxIPCount: 5, MaxVolumeBytes: 25 << 30,
		},
		EventID: "v10b-api-owner-permission-0001", ExpectedRevision: 0,
		Reason: "Owner approves only bounded volume offers",
	}
	policyBody := func(x business.CatalogPolicyChange) map[string]any {
		return map[string]any{
			"representative_id": x.RepresentativeID, "event_id": x.EventID,
			"expected_revision": x.ExpectedRevision, "reason": x.Reason,
			"manage_products": x.ManageProducts, "can_publish": x.CanPublish,
			"allow_volume": x.AllowVolume, "allow_unlimited": x.AllowUnlimited,
			"min_price_toman": x.MinPriceToman, "max_price_toman": x.MaxPriceToman,
			"max_duration_days": x.MaxDurationDays, "max_ip_count": x.MaxIPCount,
			"max_volume_bytes": x.MaxVolumeBytes,
		}
	}
	if code, _ := get(policyURI, nil); code != 401 {
		t.Fatal("anonymous owner policy GET", code)
	}
	if code, _ := get(policyURI, rep); code != 403 {
		t.Fatal("rep read owner policy", code)
	}
	if code, _ := post(policyURI, rep, repCSRF, policyBody(policy), "https://"+browserHost); code != 403 {
		t.Fatal("rep changed policy", code)
	}
	if code, _ := post(policyURI, owner, "", policyBody(policy), "https://"+browserHost); code != 403 {
		t.Fatal("CSRF bypass", code)
	}
	if code, _ := post(policyURI, owner, ownerCSRF, policyBody(policy), "https://evil.example"); code != 403 {
		t.Fatal("Origin bypass", code)
	}
	if code, _ := post(policyURI, owner, ownerCSRF, map[string]any{
		"representative_id": "shop-a", "expected_revision": 0, "manage_products": true,
		"min_price_toman": 20000, "max_price_toman": 300000, "reason": "partial policy",
	}, "https://"+browserHost); code != 422 {
		t.Fatal("missing fields accepted", code)
	}
	product := business.CatalogCreate{
		CatalogSpec: business.CatalogSpec{
			ID: "v10b-owned-draft", RepresentativeID: "shop-a", Name: "فروشگاه دستی",
			PlanKind: "volume", DurationDays: 30, VolumeBytes: 10 << 30, IPCount: 0, PriceToman: 50000,
		},
		EventID: "v10b-api-rep-draft-event-0001", Reason: "Approved catalog draft",
	}
	if code, _ := post("/api/v1/workspace/products", rep, repCSRF, product, "https://"+browserHost); code != 403 {
		t.Fatal("unprivileged rep created product", code)
	}
	if code, body := post(policyURI, owner, ownerCSRF, policyBody(policy), "https://"+browserHost); code != 200 {
		t.Fatal("owner policy save", code, body)
	}
	if code, _ := get("/api/v1/workspace/me", rep); code != 401 {
		t.Fatal("owner policy failed to revoke old sessions", code)
	}
	rep, repCSRF = browserLogin(t, s, "v10b-qa-login", "v10b-qa-password-test-123")
	if code, body := get("/api/v1/workspace/products/policy", rep); code != 200 {
		t.Fatal("own policy unavailable", code, body)
	} else {
		var own business.CatalogPolicy
		if err := json.Unmarshal([]byte(body), &own); err != nil || !own.ManageProducts || own.CanPublish {
			t.Fatal("wrong owner policy", own, err)
		}
	}
	if code, body := post("/api/v1/workspace/products", rep, repCSRF, product, "https://"+browserHost); code != 201 {
		t.Fatal("own draft denied", code, body)
	}
	if code, _ := get("/api/v1/workspace/products/v10b-owned-draft", rep); code != 409 {
		t.Fatal("draft leaked to published view", code)
	}
	if code, body := get("/api/v1/workspace/products/manage?limit=50&offset=0&state=all", rep); code != 200 {
		t.Fatal("managed draft list unavailable", code, body)
	} else {
		var page business.CatalogPage
		if err := json.Unmarshal([]byte(body), &page); err != nil || page.Total != 1 || page.Products[0].Status != "draft" {
			t.Fatal("managed page incorrect", page, err)
		}
	}
	if code, _ := get("/api/v1/workspace/products/manage?representative_id=shop-b", rep); code != 409 {
		t.Fatal("foreign draft query", code)
	}
	cheap := product
	cheap.ID = "below-floor"
	cheap.EventID = "v10b-cheap-offer-000000001"
	cheap.PriceToman = 1
	if code, _ := post("/api/v1/workspace/products", rep, repCSRF, cheap, "https://"+browserHost); code != 409 {
		t.Fatal("rep undercut price floor", code)
	}
	publishing := business.CatalogUpdate{
		CatalogSpec: product.CatalogSpec, EventID: "v10b-api-rep-publish-000001",
		ExpectedRevision: 1, Status: "published", Reason: "Publication requires owner permission",
	}
	if code, _ := post("/api/v1/workspace/products/v10b-owned-draft/update", rep, repCSRF, publishing, "https://"+browserHost); code != 409 {
		t.Fatal("rep published without permission", code)
	}
	policy.EventID = "v10b-api-owner-publish-grant-01"
	policy.ExpectedRevision = 1
	policy.CanPublish = true
	if code, _ := post(policyURI, owner, ownerCSRF, policyBody(policy), "https://"+browserHost); code != 200 {
		t.Fatal("grant publication failed", code)
	}
	rep, repCSRF = browserLogin(t, s, "v10b-qa-login", "v10b-qa-password-test-123")
	if code, body := post("/api/v1/workspace/products/v10b-owned-draft/update", rep, repCSRF, publishing, "https://"+browserHost); code != 200 {
		t.Fatal("bounded publication failed", code, body)
	}
	if code, _ := get("/api/v1/workspace/products/v10b-owned-draft", rep); code != 200 {
		t.Fatal("published rep product invisible", code)
	}
	policy.EventID = "v10b-api-owner-revoke-0000001"
	policy.ExpectedRevision = 2
	policy.ManageProducts = false
	policy.CanPublish = false
	if code, _ := post(policyURI, owner, ownerCSRF, policyBody(policy), "https://"+browserHost); code != 200 {
		t.Fatal("owner revoke failed", code)
	}
	rep, repCSRF = browserLogin(t, s, "v10b-qa-login", "v10b-qa-password-test-123")
	if code, _ := post("/api/v1/workspace/products", rep, repCSRF, product, "https://"+browserHost); code != 403 {
		t.Fatal("revoked rep created product", code)
	}
	if code, _ := get("/api/v1/workspace/products/manage", rep); code != 409 {
		t.Fatal("revoked rep saw drafts", code)
	}
	if code, _ := get("/api/v1/workspace/products?limit=50&offset=0", rep); code != 200 {
		t.Fatal("published storefront unexpectedly closed", code)
	}
	if code, _ := get(policyURI+"/events", rep); code != 403 {
		t.Fatal("owner policy audit leaked", code)
	}
	if code, _ := get(policyURI+"/events", owner); code != 200 {
		t.Fatal("owner policy audit missing", code)
	}
	if code := browserRequest(t, s.AgentHandler(), "GET", policyURI, nil, owner, "", "").Code; code == 200 {
		t.Fatal("Agent TLS policy leak")
	}
	if code := browserRequest(t, s.AgentHandler(), "GET", "/api/v1/workspace/products/manage", nil, rep, "", "").Code; code == 200 {
		t.Fatal("Agent TLS draft leak")
	}
	clients, err := s.business.ListClients(ctx, "", 50, 0)
	if err != nil || len(clients) != 0 {
		t.Fatal("catalog mutator changed client records", clients, err)
	}
	repBalance, err := s.business.Representative(ctx, "shop-a")
	if err != nil || repBalance.VolumeCreditBytes != 0 || repBalance.UnlimitedCreditUnits != 0 {
		t.Fatal("product metadata debited credit", repBalance, err)
	}
}
