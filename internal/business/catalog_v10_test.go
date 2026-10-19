package business

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestV10OwnerCatalogDraftPublishArchiveAndFinancialIsolation(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	for _, id := range []string{"seller-a", "seller-b"} {
		newRep(t, s, id)
		grant(t, s, id, "v10-grant-inventory-"+id+"-01", 20<<30, 30)
	}
	before, err := s.Representative(ctx, "seller-a")
	if err != nil {
		t.Fatal(err)
	}
	spec := CatalogSpec{ID: "v10-plan-a", RepresentativeID: "seller-a",
		Name: "مولتی توربو", PlanKind: "volume", DurationDays: 30, IPCount: 0,
		VolumeBytes: 10 << 30, PriceToman: 100000}
	request := CatalogCreate{CatalogSpec: spec, EventID: "v10-create-catalog-plan-0001", Reason: "Owner defined retail offer"}
	item, err := s.CreateCatalogProduct(ctx, request)
	if err != nil || item.Status != "draft" || item.Revision != 1 || item.IPCount != 0 {
		t.Fatal(item, err)
	}
	if got, err := s.CreateCatalogProduct(ctx, request); err != nil || got.ID != item.ID {
		t.Fatal("idempotent create", got, err)
	}
	if products, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50}, "seller-a"); err != nil || products.Total != 0 {
		t.Fatalf("representative saw draft %+v %v", products, err)
	}
	ownerPage, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50, State: "all"}, "")
	if err != nil || ownerPage.Total != 1 || len(ownerPage.Products) != 1 {
		t.Fatal("owner draft lookup", ownerPage, err)
	}
	if _, err = s.CatalogProduct(ctx, item.ID, "seller-b"); !errors.Is(err, ErrConflict) {
		t.Fatal("cross-tenant product leaked", err)
	}
	if _, err = s.CatalogProduct(ctx, item.ID, "seller-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("draft leaked to seller", err)
	}
	change := CatalogUpdate{CatalogSpec: spec, Status: "published", ExpectedRevision: 1,
		EventID: "v10-publish-catalog-plan-0001", Reason: "Owner approves test product"}
	pub, err := s.UpdateCatalogProduct(ctx, change)
	if err != nil || pub.Status != "published" || pub.Revision != 2 {
		t.Fatal(pub, err)
	}
	if p, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50, State: "all"}, "seller-a"); err != nil || p.Total != 1 || p.Products[0].ID != item.ID {
		t.Fatal("rep published scope", p, err)
	}
	if p, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50, State: "all"}, "seller-b"); err != nil || p.Total != 0 {
		t.Fatal("rep cross-tenant product", p, err)
	}
	if _, err = s.SearchCatalog(ctx, CatalogSearch{Limit: 50, State: "draft"}, "seller-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("representative forced forbidden draft filter", err)
	}
	change.EventID = "v10-archive-catalog-plan-00001"
	change.ExpectedRevision = 2
	change.Status = "archived"
	archived, err := s.UpdateCatalogProduct(ctx, change)
	if err != nil || archived.Status != "archived" || archived.Revision != 3 {
		t.Fatal("archive failed", err)
	}
	if p, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50}, "seller-a"); err != nil || p.Total != 0 {
		t.Fatal("archived published product leaked", p, err)
	}
	if _, err = s.UpdateCatalogProduct(ctx, CatalogUpdate{CatalogSpec: spec, Status: "published", ExpectedRevision: 3, EventID: "v10-reactivate-archived-plan-01", Reason: "Should refuse"}); !errors.Is(err, ErrConflict) {
		t.Fatal("archived product unexpectedly republished", err)
	}
	audits, err := s.CatalogAudit(ctx, item.ID, 50, 0)
	if err != nil || len(audits) != 3 || audits[0].Action != "update" || !strings.Contains(audits[0].AfterJSON, "archived") {
		t.Fatal("immutable catalog audit missing", audits, err)
	}
	if _, err = s.db.ExecContext(ctx, "DELETE FROM catalog_events"); err == nil {
		t.Fatal("audit deletion permitted")
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE catalog_events SET reason='modified'"); err == nil {
		t.Fatal("audit rewrite permitted")
	}
	after, err := s.Representative(ctx, "seller-a")
	if err != nil || after.VolumeCreditBytes != before.VolumeCreditBytes || after.UnlimitedCreditUnits != before.UnlimitedCreditUnits {
		t.Fatal("metadata changed representative balance", after, err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients").Scan(&count); err != nil || count != 0 {
		t.Fatal("product publication silently created customers", count, err)
	}
	var version int
	if err = s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("catalog broke prior schema rollback", version, err)
	}
}
func TestV10CatalogOptimisticPricingAndValidation(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "seller-a")
	newRep(t, s, "seller-b")
	req := CatalogCreate{
		CatalogSpec: CatalogSpec{ID: "v10-unlimited", RepresentativeID: "seller-a", Name: "Full users",
			PlanKind: "unlimited", DurationDays: 60, IPCount: 5, PriceToman: 0},
		EventID: "v10-create-unlimited-0000001", Reason: "Draft without public price",
	}
	product, err := s.CreateCatalogProduct(ctx, req)
	if err != nil || product.Status != "draft" {
		t.Fatal(product, err)
	}
	change := CatalogUpdate{CatalogSpec: req.CatalogSpec, EventID: "v10-unpriced-publish-000001",
		ExpectedRevision: 1, Status: "published", Reason: "No price"}
	if _, err = s.UpdateCatalogProduct(ctx, change); !errors.Is(err, ErrInvalid) {
		t.Fatal("published zero price allowed", err)
	}
	change.CatalogSpec.PriceToman = 250000
	change.EventID = "v10-priced-publish-0000001"
	if _, err = s.UpdateCatalogProduct(ctx, change); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCatalogProduct(ctx, change); err != nil {
		t.Fatal("retry double updated", err)
	}
	collision := change
	collision.Name = "Tamper with same event"
	if _, err = s.UpdateCatalogProduct(ctx, collision); !errors.Is(err, ErrConflict) {
		t.Fatal("changed retry accepted", err)
	}
	stale := change
	stale.EventID = "v10-stale-offer-00000001"
	if _, err = s.UpdateCatalogProduct(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale version applied", err)
	}
	foreign := change
	foreign.EventID = "v10-foreign-product-000001"
	foreign.ExpectedRevision = 2
	foreign.CatalogSpec.RepresentativeID = "seller-b"
	if _, err = s.UpdateCatalogProduct(ctx, foreign); !errors.Is(err, ErrConflict) {
		t.Fatal("product shifted to foreign rep", err)
	}
	for _, bad := range []CatalogSpec{
		{ID: "unsafe", RepresentativeID: "seller-a", Name: "invalid", PlanKind: "unlimited", DurationDays: 30, IPCount: 0},
		{ID: "unsafe", RepresentativeID: "seller-a", Name: "invalid", PlanKind: "volume", DurationDays: 30, IPCount: 0, VolumeBytes: 0},
		{ID: "unsafe", RepresentativeID: "seller-a", Name: "invalid", PlanKind: "volume", DurationDays: 30, VolumeBytes: 10, PriceToman: -1},
		{ID: "unsafe", RepresentativeID: "seller-a", Name: "invalid", PlanKind: "volume", DurationDays: 3651, VolumeBytes: 10},
		{ID: "unsafe", RepresentativeID: "seller-a", Name: "invalid", PlanKind: "volume", DurationDays: 30, VolumeBytes: MaxCatalogVolume + 1},
	} {
		if _, err = s.CreateCatalogProduct(ctx, CatalogCreate{
			CatalogSpec: bad, EventID: "v10-invalid-create-00000001", Reason: "Invalid input",
		}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid catalog input accepted %+v %v", bad, err)
		}
	}
	if _, err = s.SearchCatalog(ctx, CatalogSearch{Limit: 101}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded catalog page allowed", err)
	}
	if _, err = s.SearchCatalog(ctx, CatalogSearch{Limit: 50, Query: "hello\nworld"}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("control char accepted", err)
	}
	// Other financial tables cannot reuse a catalog event nonce.
	if _, err = s.AdjustCredits(ctx, CreditChange{EventID: req.EventID,
		RepresentativeID: "seller-a", UnlimitedDeltaUnits: 1, Reason: "Collision"}); !errors.Is(err, ErrConflict) {
		t.Fatal("catalog nonce reused by credit", err)
	}
	if _, err = s.ListRepresentatives(ctx, 50, 0); err != nil {
		t.Fatal("V9 function was damaged", err)
	}
}
func TestV10CatalogLiteralGlobalSearchAndPagination(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "seller-a")
	for i := 0; i < 53; i++ {
		spec := CatalogSpec{ID: fmt.Sprintf("catalog-%03d", i), RepresentativeID: "seller-a",
			Name: fmt.Sprintf("Product %%_%d", i), PlanKind: "volume", DurationDays: 30, VolumeBytes: 1 << 30,
			PriceToman: 1000}
		if _, err := s.CreateCatalogProduct(ctx, CatalogCreate{
			CatalogSpec: spec, EventID: fmt.Sprintf("v10-catalog-create-%03d-event-01", i),
			Reason: "Pagination test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 25, State: "draft", Query: "%_"}, "")
	if err != nil || first.Total != 53 || len(first.Products) != 25 || !first.HasMore {
		t.Fatal("literal search failed", first, err)
	}
	last, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 25, Offset: 50, State: "draft"}, "")
	if err != nil || last.Total != 53 || len(last.Products) != 3 || last.HasMore {
		t.Fatal("last page failed", last, err)
	}
	rep, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50, State: "published"}, "seller-a")
	if err != nil || rep.Total != 0 {
		t.Fatal("representative should see no draft offers", rep, err)
	}
}
