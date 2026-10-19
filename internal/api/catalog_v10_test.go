package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestV10CatalogOwnerOnlyMutationsAndPublishedTenantScope(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"shop-a", "shop-b"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Shop "+id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "shop-a", "shop-a-login", "private-shop-password-123"); err != nil {
		t.Fatal(err)
	}
	owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "shop-a-login", "private-shop-password-123")
	post := func(path string, cookie *http.Cookie, csrf string, body any, origin string) (int, string) {
		w := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, origin)
		return w.Code, w.Body.String()
	}
	get := func(path string, cookie *http.Cookie) (int, string) {
		w := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
		return w.Code, w.Body.String()
	}
	product := business.CatalogCreate{
		CatalogSpec: business.CatalogSpec{ID: "my-retail-01", RepresentativeID: "shop-a", Name: "دو گیگ آزمایشی",
			PlanKind: "volume", VolumeBytes: 2 << 30, DurationDays: 30, IPCount: 0, PriceToman: 25000},
		EventID: "v10-api-draft-created-00001", Reason: "Owner assigned test-only product",
	}
	route := "/api/v1/workspace/products"
	if code, _ := post(route, nil, "", product, ""); code != 401 {
		t.Fatal("anonymous catalog write", code)
	}
	if code, _ := post(route, rep, repCSRF, product, "https://"+browserHost); code != 403 {
		t.Fatal("rep cannot create owner-priced product", code)
	}
	if code, _ := post(route, owner, "", product, "https://"+browserHost); code != 403 {
		t.Fatal("CSRF missing on catalog mutation", code)
	}
	if code, _ := post(route, owner, ownerCSRF, product, "https://not-our-host.example"); code != 403 {
		t.Fatal("foreign origin on catalog mutation", code)
	}
	if code, _ := post(route, owner, ownerCSRF, product, "https://"+browserHost); code != 201 {
		t.Fatal("owner catalog draft rejected", code)
	}
	if code, body := get(route+"?limit=50&offset=0", rep); code != 200 {
		t.Fatal("rep published product list unavailable", code, body)
	} else {
		var p struct {
			Total int64 `json:"total"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil || p.Total != 0 {
			t.Fatal("draft leaked", p, err)
		}
	}
	if code, _ := get(route+"/my-retail-01", rep); code != 409 {
		t.Fatal("draft offered to rep", code)
	}
	if code, _ := get(route+"?state=draft", rep); code != 409 {
		t.Fatal("draft filter leak to rep", code)
	}
	if code, _ := get(route+"?q=x&q=y", owner); code != 422 {
		t.Fatal("duplicate filters accepted", code)
	}
	update := business.CatalogUpdate{
		CatalogSpec: product.CatalogSpec, EventID: "v10-api-product-publish-00001",
		ExpectedRevision: 1, Status: "published", Reason: "Publish approved test offer",
	}
	if code, body := post(route+"/my-retail-01/update", owner, ownerCSRF, update, "https://"+browserHost); code != 200 {
		t.Fatal("publish failed", code, body)
	}
	if code, body := get(route+"?limit=50&offset=0", rep); code != 200 {
		t.Fatal("rep published list", code, body)
	} else {
		var page struct {
			Products []business.CatalogProduct `json:"products"`
			Total    int64                     `json:"total"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil || page.Total != 1 ||
			page.Products[0].RepresentativeID != "shop-a" || page.Products[0].Status != "published" {
			t.Fatal("wrong own published scope", page, err)
		}
	}
	if code, _ := get(route+"/my-retail-01", rep); code != 200 {
		t.Fatal("own published detail not accessible", code)
	}
	if code, _ := get(route+"/my-retail-01/events", rep); code != 403 {
		t.Fatal("catalog owner audit leaked to rep", code)
	}
	if code, _ := get(route+"/my-retail-01/events", owner); code != 200 {
		t.Fatal("owner audit unavailable", code)
	}
	if code, _ := get(route+"?representative_id=shop-b", rep); code != 409 {
		t.Fatal("foreign rep catalog scope allowed", code)
	}
	if code, _ := post(route+"/my-retail-01/update", rep, repCSRF, update, "https://"+browserHost); code != 403 {
		t.Fatal("rep modified owner catalog", code)
	}
	if code := browserRequest(t, s.AgentHandler(), "GET", route, nil, owner, "", "").Code; code == 200 {
		t.Fatal("Agent TLS exposed browser product catalog")
	}
	// Product definitions cannot cause credit or client mutations.
	rows, err := s.business.ListClients(ctx, "", 50, 0)
	if err != nil || len(rows) != 0 {
		t.Fatal("metadata altered customer records", len(rows), err)
	}
	repData, err := s.business.Representative(ctx, "shop-a")
	if err != nil || repData.VolumeCreditBytes != 0 || repData.UnlimitedCreditUnits != 0 {
		t.Fatal("metadata altered available credit", repData, err)
	}
}
