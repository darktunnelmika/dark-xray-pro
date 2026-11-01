package business

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// A committed ownership request must not become a different business mutation
// when a caller retries the same event through another family of endpoints.
func TestOwnershipNonceRejectedAcrossOtherMutationFamilies(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, table string
		prepare     func(*testing.T, ownershipFixture, Client) func(string) error
	}{
		{"profile", "client_profile_edits", func(t *testing.T, f ownershipFixture, c Client) func(string) error {
			intent := ClientProfileEditIntent{ClientID: c.ID, Name: "Edited transferred client", PlanKind: c.PlanKind, IPCount: c.IPCount, AdditionalDays: 1, AdditionalVolumeBytes: 7, Reason: "Synthetic profile update"}
			q, err := f.s.QuoteClientProfileEdit(ctx, intent, "rep-target")
			if err != nil {
				t.Fatal(err)
			}
			return func(event string) error {
				_, err := f.s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{ClientProfileEditIntent: intent, EventID: event, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}, "rep-target")
				return err
			}
		}},
		{"catalog", "catalog_events", func(t *testing.T, f ownershipFixture, c Client) func(string) error {
			return func(event string) error {
				_, err := f.s.CreateCatalogProduct(ctx, CatalogCreate{CatalogSpec: CatalogSpec{ID: "ownership-product", RepresentativeID: "rep-target", Name: "Synthetic product", PlanKind: "volume", DurationDays: 30, VolumeBytes: 10, PriceToman: 1000}, EventID: event, Reason: "Synthetic catalog creation"})
				return err
			}
		}},
		{"catalog-policy", "catalog_policy_events", func(t *testing.T, f ownershipFixture, c Client) func(string) error {
			change := standardCatalogV10bPolicy()
			change.RepresentativeID = "rep-target"
			return func(event string) error {
				change.EventID = event
				_, err := f.s.UpdateCatalogPolicy(ctx, change)
				return err
			}
		}},
		{"representative-control", "representative_control_events", func(t *testing.T, f ownershipFixture, c Client) func(string) error {
			return func(event string) error {
				_, err := f.s.UpdateRepresentativeControl(ctx, RepresentativeControlChange{EventID: event, RepresentativeID: "rep-target", DisplayName: "Renamed target", Permissions: DefaultRepresentativePermissions(), Reason: "Synthetic representative update"})
				return err
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ownershipSetup(t)
			c := ownershipNew(t, f, "nonce-client", false)
			const event = "ownership-cross-family-nonce-0001"
			ownershipMove(t, f, event, ownershipIntent(c.ID))
			mutate := tc.prepare(t, f, c)
			before := ownershipBalances(t, f.s)
			beforeClient, err := f.s.Client(ctx, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = mutate(event); !errors.Is(err, ErrConflict) {
				t.Fatalf("ownership event reused for %s: %v", tc.name, err)
			}
			afterClient, err := f.s.Client(ctx, c.ID)
			if err != nil || afterClient != beforeClient || !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
				t.Fatal("rejected request changed client or representative state", err)
			}
			var count int
			if err = f.s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tc.table).Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected request wrote a second business receipt", count, err)
			}
			if err = mutate("ownership-distinct-" + tc.name + "-0001"); err != nil {
				t.Fatal("distinct event unexpectedly refused", err)
			}
			paid := ownershipBalances(t, f.s)
			changed, err := f.s.Client(ctx, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = mutate("ownership-distinct-" + tc.name + "-0001"); err != nil {
				t.Fatal("legitimate replay unexpectedly refused", err)
			}
			replayed, err := f.s.Client(ctx, c.ID)
			if err != nil || replayed != changed || !reflect.DeepEqual(paid, ownershipBalances(t, f.s)) {
				t.Fatal("replay changed client or balance twice", err)
			}
			if err = f.s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tc.table).Scan(&count); err != nil || count != 1 {
				t.Fatal("replay duplicated business receipt", count, err)
			}
			if _, err = f.s.ClientOwnershipReceipt(ctx, f.owner.Token, event); err != nil {
				t.Fatal("original ownership receipt no longer available", err)
			}
		})
	}
}
