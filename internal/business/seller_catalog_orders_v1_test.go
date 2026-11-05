package business

import (
	"context"
	"errors"
	"testing"
)

func publishUnlimitedCatalogOrderV1(t *testing.T, f sellerOrderFixtureV1, id string, days, users, price int64) CatalogProduct {
	t.Helper()
	ctx := context.Background()
	spec := CatalogSpec{
		ID: id, RepresentativeID: "repAccessA", Name: "Synthetic unlimited " + id,
		PlanKind: "unlimited", DurationDays: days, IPCount: users,
		PriceToman: price,
	}
	draft, err := f.s.CreateCatalogProduct(ctx, CatalogCreate{
		CatalogSpec: spec, EventID: "catalog-create-" + id,
		Reason: "Synthetic catalog contract",
	})
	if err != nil {
		t.Fatal(err)
	}
	product, err := f.s.UpdateCatalogProduct(ctx, CatalogUpdate{
		CatalogSpec: spec, EventID: "catalog-publish-" + id,
		ExpectedRevision: draft.Revision, Status: "published",
		Reason: "Publish synthetic catalog contract",
	})
	if err != nil {
		t.Fatal(err)
	}
	return product
}

func catalogSellerRequestV1(product CatalogProduct, event, client string) SellerCatalogClientCreateV1 {
	return SellerCatalogClientCreateV1{
		EventID: event, RepresentativeID: product.RepresentativeID,
		SellerID: "sellerAlice", ProductID: product.ID,
		ExpectedProductRevision: product.Revision,
		ExpectedPriceToman:      product.PriceToman,
		ExpectedDurationDays:    product.DurationDays,
		ExpectedUserCount:       product.IPCount,
		ClientID:                client, Name: "Catalog customer " + client,
		Reason: "Synthetic catalog order",
	}
}

func TestSellerCatalogUnlimitedExactMonthsReplayArchiveAndReopen(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	product := publishUnlimitedCatalogOrderV1(t, f, "store-unlimited-2m3u", 60, 3, 250000)
	req := catalogSellerRequestV1(product, "store-order-exact-2m3u-0001", "store-client-exact")

	receipt, err := f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Replayed || receipt.VolumeDebitBytes != 0 ||
		receipt.UnlimitedDebitUnits != 6 ||
		receipt.ReservedUnitsBefore != 100 || receipt.ReservedUnitsAfter != 94 ||
		receipt.Catalog == nil ||
		receipt.Catalog.ProductID != product.ID ||
		receipt.Catalog.ProductRevision != product.Revision ||
		receipt.Catalog.PriceToman != product.PriceToman ||
		receipt.Catalog.Months != 2 || receipt.Catalog.UserCount != 3 ||
		receipt.Catalog.CheckoutEnabled || receipt.Catalog.PaymentApplied ||
		receipt.Client.PlanKind != "unlimited" ||
		receipt.Client.DurationDays != 60 || receipt.Client.IPCount != 3 ||
		receipt.Client.ChargedUnlimitedUnits != 6 {
		t.Fatalf("bad catalog receipt: %+v", receipt)
	}
	replayed, err := f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req)
	if err != nil || !replayed.Replayed {
		t.Fatal("exact replay failed", replayed, err)
	}

	product.Status = "archived"
	archived, err := f.s.UpdateCatalogProduct(ctx, CatalogUpdate{
		CatalogSpec: product.CatalogSpec, EventID: "catalog-archive-store-unlimited-2m3u",
		ExpectedRevision: product.Revision, Status: "archived",
		Reason: "Retire synthetic product",
	})
	if err != nil || archived.Status != "archived" {
		t.Fatal(archived, err)
	}
	replayed, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req)
	if err != nil || !replayed.Replayed || replayed.Catalog == nil ||
		replayed.Catalog.ProductRevision != 2 {
		t.Fatal("archive rewrote immutable order", replayed, err)
	}
	freshReq := req
	freshReq.EventID = "store-order-after-archive-0001"
	freshReq.ClientID = "store-client-after-archive"
	if _, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, freshReq); !errors.Is(err, ErrConflict) {
		t.Fatal("archived catalog accepted for a new order", err)
	}
	if _, err = f.s.Client(ctx, freshReq.ClientID); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed archived order leaked a client", err)
	}

	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.Close()
	replayed, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req)
	if err != nil || !replayed.Replayed ||
		replayed.UnlimitedDebitUnits != 6 || replayed.ReservedUnitsAfter != 94 {
		t.Fatal("reopen lost immutable catalog order", replayed, err)
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 94)
}

func TestSellerCatalogUnlimitedRejectsUnknownStalePartialAndInsufficient(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	product := publishUnlimitedCatalogOrderV1(t, f, "store-strict-unlimited", 90, 4, 300000)
	base := catalogSellerRequestV1(product, "store-strict-base-event-0001", "store-strict-base")

	tests := []struct {
		name string
		edit func(*SellerCatalogClientCreateV1)
		want error
	}{
		{"unknown product", func(r *SellerCatalogClientCreateV1) { r.ProductID = "store-unknown-product" }, ErrConflict},
		{"stale revision", func(r *SellerCatalogClientCreateV1) { r.ExpectedProductRevision-- }, ErrConflict},
		{"stale price", func(r *SellerCatalogClientCreateV1) { r.ExpectedPriceToman++ }, ErrConflict},
		{"stale duration", func(r *SellerCatalogClientCreateV1) { r.ExpectedDurationDays = 60 }, ErrConflict},
		{"stale users", func(r *SellerCatalogClientCreateV1) { r.ExpectedUserCount = 5 }, ErrConflict},
		{"partial month", func(r *SellerCatalogClientCreateV1) { r.ExpectedDurationDays = 45 }, ErrInvalid},
		{"zero users", func(r *SellerCatalogClientCreateV1) { r.ExpectedUserCount = 0 }, ErrInvalid},
	}
	for i, tc := range tests {
		req := base
		req.EventID = base.EventID + "-" + string(rune('a'+i))
		req.ClientID = base.ClientID + "-" + string(rune('a'+i))
		tc.edit(&req)
		if _, err := f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req); !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, err, tc.want)
		}
		if _, err := f.s.Client(ctx, req.ClientID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s leaked client: %v", tc.name, err)
		}
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 100)

	draftSpec := CatalogSpec{ID: "store-draft-unlimited", RepresentativeID: "repAccessA",
		Name: "Draft unlimited", PlanKind: "unlimited", DurationDays: 30,
		IPCount: 2, PriceToman: 100000}
	draft, err := f.s.CreateCatalogProduct(ctx, CatalogCreate{
		CatalogSpec: draftSpec, EventID: "catalog-create-store-draft-unlimited",
		Reason: "Keep synthetic product as draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	draftReq := catalogSellerRequestV1(draft, "store-draft-order-event-0001", "store-draft-client")
	if _, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, draftReq); !errors.Is(err, ErrConflict) {
		t.Fatal("draft catalog accepted", err)
	}

	volumeSpec := CatalogSpec{ID: "store-volume-product", RepresentativeID: "repAccessA",
		Name: "Volume product", PlanKind: "volume", DurationDays: 30,
		VolumeBytes: 1024, PriceToman: 100000}
	volumeDraft, err := f.s.CreateCatalogProduct(ctx, CatalogCreate{
		CatalogSpec: volumeSpec, EventID: "catalog-create-store-volume",
		Reason: "Synthetic volume product",
	})
	if err != nil {
		t.Fatal(err)
	}
	volume, err := f.s.UpdateCatalogProduct(ctx, CatalogUpdate{
		CatalogSpec: volumeSpec, EventID: "catalog-publish-store-volume",
		ExpectedRevision: volumeDraft.Revision, Status: "published",
		Reason: "Publish synthetic volume product",
	})
	if err != nil {
		t.Fatal(err)
	}
	volumeReq := catalogSellerRequestV1(volume, "store-volume-order-event-0001", "store-volume-client")
	volumeReq.ExpectedUserCount = 1
	if _, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, volumeReq); !errors.Is(err, ErrConflict) {
		t.Fatal("volume catalog entered unlimited order", err)
	}

	newRep(t, f.s, "repCatalogOther")
	foreignSpec := CatalogSpec{ID: "store-foreign-unlimited", RepresentativeID: "repCatalogOther",
		Name: "Foreign unlimited", PlanKind: "unlimited", DurationDays: 30,
		IPCount: 2, PriceToman: 100000}
	foreignDraft, err := f.s.CreateCatalogProduct(ctx, CatalogCreate{
		CatalogSpec: foreignSpec, EventID: "catalog-create-store-foreign",
		Reason: "Synthetic foreign product",
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := f.s.UpdateCatalogProduct(ctx, CatalogUpdate{
		CatalogSpec: foreignSpec, EventID: "catalog-publish-store-foreign",
		ExpectedRevision: foreignDraft.Revision, Status: "published",
		Reason: "Publish synthetic foreign product",
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignReq := catalogSellerRequestV1(foreign, "store-foreign-order-event-0001", "store-foreign-client")
	if _, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, foreignReq); err == nil {
		t.Fatal("representative bought from a foreign tenant")
	}
	if _, err = f.s.Client(ctx, foreignReq.ClientID); !errors.Is(err, ErrNotFound) {
		t.Fatal("unauthorized order leaked a client", err)
	}

	expensive := publishUnlimitedCatalogOrderV1(t, f, "store-insufficient-unlimited", 120, 1000, 400000)
	insufficient := catalogSellerRequestV1(expensive, "store-insufficient-order-0001", "store-insufficient-client")
	if _, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, insufficient); !errors.Is(err, ErrInsufficient) {
		t.Fatal("insufficient reserve accepted", err)
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 100)
	var receipts int
	if err = f.s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seller_order_receipts_v1 WHERE event_id=?`, insufficient.EventID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("failed order wrote receipt", receipts, err)
	}
}

func TestSellerCatalogUnlimitedReceiptIsImmutableAndNoPaymentClaim(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	product := publishUnlimitedCatalogOrderV1(t, f, "store-receipt-unlimited", 30, 7, 700000)
	req := catalogSellerRequestV1(product, "store-receipt-order-0001", "store-receipt-client")
	receipt, err := f.s.CreateSellerClientFromCatalogV1(ctx, f.owner.Token, req)
	if err != nil || receipt.ActorUserID != f.owner.User.ID ||
		receipt.UnlimitedDebitUnits != 7 || receipt.Catalog == nil ||
		receipt.Catalog.CheckoutEnabled || receipt.Catalog.PaymentApplied {
		t.Fatal(receipt, err)
	}
	if _, err = f.s.db.ExecContext(ctx, `UPDATE seller_order_receipts_v1 SET receipt_json='{}' WHERE event_id=?`, req.EventID); err == nil {
		t.Fatal("immutable catalog order receipt was writable")
	}
	conflict := req
	conflict.ExpectedPriceToman++
	if _, err = f.s.CreateSellerClientFromCatalogV1(ctx, f.owner.Token, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("same nonce accepted a changed product snapshot", err)
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 93)
}
