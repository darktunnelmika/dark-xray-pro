package business

import (
	"context"
	"errors"
	"testing"
)

func catalogV10bSeed(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, _ := opened(t)
	ctx := context.Background()
	for _, id := range []string{"shop-a", "shop-b"} {
		newRep(t, s, id)
		grant(t, s, id, "catalog-policy-test-grant-"+id+"-01", 30<<30, 40)
	}
	if _, err := s.ProvisionRepresentativeLogin(ctx, "shop-a", "catalog-policy-login", "private-catalog-policy-password-123"); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}
func standardCatalogV10bPolicy() CatalogPolicyChange {
	return CatalogPolicyChange{
		EventID: "catalog-policy-owner-grant-0001", ExpectedRevision: 0, Reason: "Owner allows limited product draft management",
		CatalogPolicy: CatalogPolicy{
			RepresentativeID: "shop-a", ManageProducts: true, CanPublish: false,
			AllowVolume: true, AllowUnlimited: false,
			MinPriceToman: 20000, MaxPriceToman: 150000,
			MaxDurationDays: 60, MaxIPCount: 5, MaxVolumeBytes: 25 << 30,
		},
	}
}
func TestV10bOwnerPolicyGrantScopedDraftPriceAndRevocation(t *testing.T) {
	s, ctx := catalogV10bSeed(t)
	original, err := s.CatalogPolicy(ctx, "shop-a")
	if err != nil || original.ManageProducts || original.Revision != 0 {
		t.Fatal("default owner policy must be disabled", original, err)
	}
	before, err := s.Representative(ctx, "shop-a")
	if err != nil {
		t.Fatal(err)
	}
	login, err := s.Login(ctx, "catalog-policy-login", "private-catalog-policy-password-123")
	if err != nil {
		t.Fatal(err)
	}
	draft := CatalogCreate{
		CatalogSpec: CatalogSpec{ID: "shop-a-draft", RepresentativeID: "shop-a", Name: "نماینده محصول دستی",
			PlanKind: "volume", DurationDays: 30, IPCount: 0, VolumeBytes: 15 << 30, PriceToman: 30000},
		EventID: "v10b-rep-create-draft-000001", Reason: "Manual shop draft within owner price floor",
	}
	if _, err = s.CreateCatalogProductScoped(ctx, draft, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatalf("representative created without owner permission %v", err)
	}
	change := standardCatalogV10bPolicy()
	granted, err := s.UpdateCatalogPolicy(ctx, change)
	if err != nil || granted.Replayed || !granted.SessionsRevoked ||
		!granted.Policy.ManageProducts || granted.Policy.CanPublish || granted.Policy.Revision != 1 ||
		granted.CreditChanged || granted.ClientsChanged || granted.AppliedToXray {
		t.Fatalf("incorrect owner grant %+v %v", granted, err)
	}
	if _, err = s.VerifySession(ctx, login.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("prior session survived permission change %v", err)
	}
	if _, err = s.Login(ctx, "catalog-policy-login", "private-catalog-policy-password-123"); err != nil {
		t.Fatalf("rep login should remain permitted %v", err)
	}
	if rep, err := s.Representative(ctx, "shop-a"); err != nil ||
		rep.VolumeCreditBytes != before.VolumeCreditBytes || rep.UnlimitedCreditUnits != before.UnlimitedCreditUnits || !rep.Enabled {
		t.Fatal("policy grant altered customer accounting", rep, err)
	}
	created, err := s.CreateCatalogProductScoped(ctx, draft, "shop-a")
	if err != nil || created.Status != "draft" || created.ID != draft.ID {
		t.Fatalf("scoped draft creation %+v %v", created, err)
	}
	if got, err := s.CreateCatalogProductScoped(ctx, draft, "shop-a"); err != nil || got.ID != draft.ID {
		t.Fatal("idempotent draft create failed", got, err)
	}
	if _, err = s.CreateCatalogProductScoped(ctx, draft, "shop-b"); !errors.Is(err, ErrConflict) {
		t.Fatal("cross-representative product creation allowed", err)
	}
	blocked := draft
	blocked.ID = "shop-a-cheap"
	blocked.EventID = "v10b-too-cheap-product-00001"
	blocked.PriceToman = 19999
	if _, err = s.CreateCatalogProductScoped(ctx, blocked, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("price below owner floor allowed", err)
	}
	blocked = draft
	blocked.ID = "shop-a-large"
	blocked.EventID = "v10b-too-large-product-0001"
	blocked.VolumeBytes = 26 << 30
	if _, err = s.CreateCatalogProductScoped(ctx, blocked, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("volume above owner maximum allowed", err)
	}
	blocked = draft
	blocked.ID = "shop-a-long"
	blocked.EventID = "v10b-too-long-product-00001"
	blocked.DurationDays = 90
	if _, err = s.CreateCatalogProductScoped(ctx, blocked, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("duration above owner max allowed", err)
	}
	blocked = draft
	blocked.ID = "shop-a-unlimited"
	blocked.EventID = "v10b-disallowed-unlimited-0001"
	blocked.PlanKind = "unlimited"
	blocked.VolumeBytes = 0
	blocked.IPCount = 3
	if _, err = s.CreateCatalogProductScoped(ctx, blocked, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("disallowed unlimited plan allowed", err)
	}
	scopePage, err := s.SearchCatalogManaged(ctx, CatalogSearch{Limit: 50, State: "all"}, "shop-a")
	if err != nil || scopePage.Total != 1 || scopePage.Products[0].Status != "draft" {
		t.Fatal("scoped manage directory lacked owned draft", scopePage, err)
	}
	published, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50}, "shop-a")
	if err != nil || published.Total != 0 {
		t.Fatal("draft leaked into public published catalog", published, err)
	}
	repEdit := CatalogUpdate{CatalogSpec: draft.CatalogSpec, EventID: "v10b-rep-publish-blocked-0001",
		ExpectedRevision: 1, Status: "published", Reason: "Publisher access not yet granted"}
	if _, err = s.UpdateCatalogProductScoped(ctx, repEdit, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("representative published without can_publish", err)
	}
	// Owner explicitly grants publication only after setting owner price bounds.
	change.EventID = "catalog-policy-owner-publish-0001"
	change.ExpectedRevision = 1
	change.CanPublish = true
	updated, err := s.UpdateCatalogPolicy(ctx, change)
	if err != nil || updated.Policy.Revision != 2 || !updated.Policy.CanPublish {
		t.Fatal(updated, err)
	}
	item, err := s.UpdateCatalogProductScoped(ctx, repEdit, "shop-a")
	if err != nil || item.Status != "published" || item.Revision != 2 {
		t.Fatal("allowed publish not applied", item, err)
	}
	if x, err := s.SearchCatalog(ctx, CatalogSearch{Limit: 50}, "shop-a"); err != nil || x.Total != 1 {
		t.Fatal("published own product not visible", x, err)
	}
	blockedEdit := repEdit
	blockedEdit.EventID = "v10b-rep-price-downgrade-00001"
	blockedEdit.ExpectedRevision = 2
	blockedEdit.PriceToman = 1
	if _, err = s.UpdateCatalogProductScoped(ctx, blockedEdit, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("representative lowered owner price floor", err)
	}
	// Revocation is immediate on DB write and cannot leak through a stale cookie.
	change.EventID = "catalog-policy-owner-revoke-0001"
	change.ExpectedRevision = 2
	change.ManageProducts = false
	change.CanPublish = false
	revoked, err := s.UpdateCatalogPolicy(ctx, change)
	if err != nil || revoked.Policy.ManageProducts || revoked.Policy.Revision != 3 {
		t.Fatal("revoke failed", revoked, err)
	}
	if _, err = s.SearchCatalogManaged(ctx, CatalogSearch{Limit: 50}, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked rep could enumerate drafts", err)
	}
	repEdit.EventID = "v10b-rep-edit-after-revoke-01"
	repEdit.ExpectedRevision = 2
	if _, err = s.UpdateCatalogProductScoped(ctx, repEdit, "shop-a"); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked rep could update product", err)
	}
	ownerProduct, err := s.CatalogProduct(ctx, draft.ID, "")
	if err != nil || ownerProduct.Status != "published" {
		t.Fatal("revocation silently unpublishes product", ownerProduct, err)
	}
	audit, err := s.CatalogPolicyEvents(ctx, "shop-a", 50, 0)
	if err != nil || len(audit) != 3 || audit[0].Current.ManageProducts || audit[2].Current.Revision != 1 {
		t.Fatal("missing owner policy audit events", audit, err)
	}
	if _, err = s.db.ExecContext(ctx, "DELETE FROM catalog_policy_events"); err == nil {
		t.Fatal("policy audit erasable")
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE catalog_policy_events SET reason='tampered'"); err == nil {
		t.Fatal("policy audit mutable")
	}
	still, err := s.Representative(ctx, "shop-a")
	if err != nil || still.VolumeCreditBytes != before.VolumeCreditBytes || still.UnlimitedCreditUnits != before.UnlimitedCreditUnits {
		t.Fatal("catalog controls touched credit ledger", still, err)
	}
	var rows int
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("catalog controls created customers", rows, err)
	}
}
func TestV10bPolicyIdempotencyOptimismAndFinancialNonce(t *testing.T) {
	s, ctx := catalogV10bSeed(t)
	input := standardCatalogV10bPolicy()
	saved, err := s.UpdateCatalogPolicy(ctx, input)
	if err != nil || saved.Policy.Revision != 1 {
		t.Fatal(saved, err)
	}
	again, err := s.UpdateCatalogPolicy(ctx, input)
	if err != nil || !again.Replayed || again.Policy.Revision != 1 {
		t.Fatal("replay charged twice or incremented policy", again, err)
	}
	malicious := input
	malicious.MinPriceToman = 1
	if _, err = s.UpdateCatalogPolicy(ctx, malicious); !errors.Is(err, ErrConflict) {
		t.Fatal("nonce changed payload allowed", err)
	}
	stale := input
	stale.EventID = "v10b-policy-stale-00000001"
	if _, err = s.UpdateCatalogPolicy(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale expected revision accepted", err)
	}
	foreign := input
	foreign.EventID = "v10b-policy-other-rep-000001"
	foreign.RepresentativeID = "shop-b"
	if _, err = s.UpdateCatalogPolicy(ctx, foreign); err != nil {
		t.Fatal("separate rep independent policy", err)
	}
	owner := input
	owner.EventID = "v10b-policy-owner-00000001"
	owner.RepresentativeID = "owner"
	if _, err = s.UpdateCatalogPolicy(ctx, owner); !errors.Is(err, ErrInvalid) {
		t.Fatal("owner policy editable", err)
	}
	noPrice := input
	noPrice.EventID = "v10b-policy-bad-price-0000001"
	noPrice.ExpectedRevision = 1
	noPrice.MinPriceToman = 0
	if _, err = s.UpdateCatalogPolicy(ctx, noPrice); !errors.Is(err, ErrInvalid) {
		t.Fatal("zero price floor accepted", err)
	}
	noType := input
	noType.EventID = "v10b-policy-no-plan-00000001"
	noType.ExpectedRevision = 1
	noType.AllowVolume = false
	noType.AllowUnlimited = false
	if _, err = s.UpdateCatalogPolicy(ctx, noType); !errors.Is(err, ErrInvalid) {
		t.Fatal("management without plan type allowed", err)
	}
	if _, err = s.AdjustCredits(ctx, CreditChange{EventID: input.EventID, RepresentativeID: "shop-a",
		UnlimitedDeltaUnits: 1, Reason: "Attempt to reuse catalog owner nonce"}); !errors.Is(err, ErrConflict) {
		t.Fatal("policy nonce reused in credit accounting", err)
	}
	if _, err = s.CreateCatalogProduct(ctx, CatalogCreate{
		CatalogSpec: CatalogSpec{ID: "nonce-product", RepresentativeID: "shop-a", Name: "test", PlanKind: "volume",
			IPCount: 0, DurationDays: 30, VolumeBytes: 1 << 30, PriceToman: 50000},
		EventID: input.EventID, Reason: "Reuse policy nonce",
	}); !errors.Is(err, ErrConflict) {
		t.Fatal("catalog reused owner policy nonce", err)
	}
	var version int
	if err = s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("V10a rollback user_version changed", version, err)
	}
}
