//go:build linux

package backupacceptance_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Seed the shared fixture so encrypted, replacement and disposable systemd
// recovery all carry a real catalog charge and its retired product.
func seedCatalogRecovery(t *testing.T, s *business.Store, f *economicFixture) {
	t.Helper()
	ctx := context.Background()
	spec := business.CatalogSpec{ID: "backup-catalog-plan", RepresentativeID: "backup-rep-a",
		Name: "Recovery catalog plan", PlanKind: "unlimited", DurationDays: 60, IPCount: 3, PriceToman: 250000}
	draft, err := s.CreateCatalogProduct(ctx, business.CatalogCreate{CatalogSpec: spec,
		EventID: "backup-catalog-create-0001", Reason: "Synthetic recovery product"})
	require(t, err)
	product, err := s.UpdateCatalogProduct(ctx, business.CatalogUpdate{CatalogSpec: spec,
		EventID: "backup-catalog-publish-0001", ExpectedRevision: draft.Revision, Status: "published", Reason: "Publish recovery product"})
	require(t, err)
	f.catalogRequest = business.SellerCatalogClientCreateV1{EventID: "backup-catalog-order-0001",
		RepresentativeID: spec.RepresentativeID, SellerID: "seller-alice", ProductID: product.ID,
		ExpectedProductRevision: product.Revision, ExpectedPriceToman: 250000, ExpectedDurationDays: 60, ExpectedUserCount: 3,
		ClientID: "backup-catalog-client", Name: "Recovery catalog customer", Reason: "Synthetic recovery purchase"}
	f.catalogReceipt, err = s.CreateSellerClientFromCatalogV1(ctx, f.token, f.catalogRequest)
	require(t, err)
	receipt := f.catalogReceipt
	if receipt.Replayed || receipt.UnlimitedDebitUnits != 6 || receipt.ReservedUnitsBefore != 91 ||
		receipt.ReservedUnitsAfter != 85 || receipt.VolumeDebitBytes != 0 || receipt.Catalog == nil ||
		receipt.Catalog.Months != 2 || receipt.Catalog.UserCount != 3 || receipt.Catalog.PriceToman != 250000 ||
		receipt.Catalog.PaymentApplied || receipt.Catalog.CheckoutEnabled {
		t.Fatal("catalog recovery fixture did not establish an exact six-unit charge")
	}
	_, err = s.UpdateCatalogProduct(ctx, business.CatalogUpdate{CatalogSpec: spec,
		EventID: "backup-catalog-archive-0001", ExpectedRevision: product.Revision, Status: "archived", Reason: "Retire product before backup"})
	require(t, err)
}

func assertCatalogRecoveryReplay(t *testing.T, s *business.Store, f economicFixture) {
	t.Helper()
	ctx := context.Background()
	replay, err := s.CreateSellerClientFromCatalogV1(ctx, f.token, f.catalogRequest)
	require(t, err)
	if !replay.Replayed {
		t.Fatal("restored catalog purchase was charged again")
	}
	replay.Replayed = false
	if !reflect.DeepEqual(replay, f.catalogReceipt) {
		t.Fatal("restored catalog receipt terms or client identity changed")
	}
	conflicting := f.catalogRequest
	conflicting.ExpectedPriceToman++
	if _, err = s.CreateSellerClientFromCatalogV1(ctx, f.token, conflicting); !errors.Is(err, business.ErrConflict) {
		t.Fatal("restored catalog nonce accepted different price", err)
	}
	fresh := f.catalogRequest
	fresh.EventID = "backup-catalog-retired-order-0001"
	fresh.ClientID = "backup-catalog-retired-client"
	if _, err = s.CreateSellerClientFromCatalogV1(ctx, f.token, fresh); !errors.Is(err, business.ErrConflict) {
		t.Fatal("restored retired catalog accepted a fresh order", err)
	}
	if _, err = s.Client(ctx, fresh.ClientID); !errors.Is(err, business.ErrNotFound) {
		t.Fatal("rejected restored catalog order leaked a client", err)
	}
}
