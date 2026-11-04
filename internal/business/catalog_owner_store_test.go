package business

import (
	"context"
	"errors"
	"testing"
)

func TestOwnerStoreUsesNativeCatalogWithoutRepresentativeAuthorityLeak(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "retail-rep")
	spec := CatalogSpec{ID: "owner-retail-plan", RepresentativeID: "owner", Name: "Owner retail",
		PlanKind: "unlimited", DurationDays: 30, IPCount: 2, PriceToman: 100000}
	req := CatalogCreate{CatalogSpec: spec, EventID: "owner-retail-create-00001", Reason: "Create owner storefront product"}
	draft, err := s.CreateCatalogProduct(ctx, req)
	if err != nil || draft.Status != "draft" || draft.RepresentativeID != "owner" {
		t.Fatal("owner draft", draft, err)
	}
	if _, err = s.CreateCatalogProductScoped(ctx, req, "retail-rep"); err == nil {
		t.Fatal("representative created an owner product")
	}
	if _, err = s.CreateCatalogProductScoped(ctx, req, "owner"); !errors.Is(err, ErrInvalid) {
		t.Fatal("representative API accepted owner scope", err)
	}
	update := CatalogUpdate{CatalogSpec: spec, EventID: "owner-retail-publish-00001", ExpectedRevision: draft.Revision,
		Status: "published", Reason: "Publish owner storefront product"}
	pub, err := s.UpdateCatalogProduct(ctx, update)
	if err != nil || pub.Status != "published" {
		t.Fatal("owner publish", pub, err)
	}
	if _, err = s.CatalogProduct(ctx, spec.ID, "retail-rep"); !errors.Is(err, ErrConflict) {
		t.Fatal("owner product leaked to another representative", err)
	}
	page, err := s.SearchCatalog(ctx, CatalogSearch{RepresentativeID: "owner", State: "published", Limit: 10}, "")
	if err != nil || page.Total != 1 || page.Products[0].ID != spec.ID {
		t.Fatal("owner catalog filter", page, err)
	}
	update.EventID = "owner-retail-archive-00001"
	update.ExpectedRevision = pub.Revision
	update.Status = "archived"
	if _, err = s.UpdateCatalogProductScoped(ctx, update, "retail-rep"); err == nil {
		t.Fatal("representative archived owner product")
	}
	if _, err = s.UpdateCatalogProduct(ctx, update); err != nil {
		t.Fatal("owner archive", err)
	}
	clients, err := s.ListClients(ctx, "owner", 10, 0)
	if err != nil || len(clients) != 0 {
		t.Fatal("catalog changed native clients", clients, err)
	}
}
