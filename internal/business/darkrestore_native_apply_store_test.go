package business

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDarkRestoreNativeApplyStoreAdapterRequiresWholeChain(t *testing.T) {
	t.Run("prepared delivery alone is closed", func(t *testing.T) {
		ctx := context.Background()
		f, evidence, key, _ := preparedCutoverFixture(t)
		input := DarkRestorePreparedDeliveryInputV1{EventID: "native-apply-adapter-delivery-0001", InboundID: 801, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}
		if _, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, evidence.ClientID, input); err != nil {
			t.Fatal(err)
		}
		before := ownershipBalances(t, f.s)
		if _, err := f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, key, f.owner.Token, evidence.ClientID, 801, routeTestID(1701), DarkRestoreNativeApplyPinsV1{}); !errors.Is(err, ErrDarkRestoreCutover) {
			t.Fatal("prepared delivery invented apply evidence", err)
		}
		if _, err := f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, key, f.rep.Token, evidence.ClientID, 801, routeTestID(1701), DarkRestoreNativeApplyPinsV1{}); !errors.Is(err, ErrOwnershipDenied) {
			t.Fatal("representative projected owner evidence", err)
		}
		if got := ownershipBalances(t, f.s); !reflect.DeepEqual(before, got) {
			t.Fatal("read-only projection changed balances")
		}
	})

	t.Run("native apply alone is closed", func(t *testing.T) {
		ctx := context.Background()
		f, _ := initialApplied(t)
		round, _, _ := initialCompleteRound(t, f, 1702)
		client := f.clients[0].ID
		owner, err := f.s.Login(ctx, "initial-owner", "synthetic-initial-health-password")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.ProjectDarkRestoreNativeApplyEvidenceV1(ctx, f.key, owner.Token, client, 501, round.ID, DarkRestoreNativeApplyPinsV1{}); !errors.Is(err, ErrDarkRestoreCutover) {
			t.Fatal("native receipts invented a Dark Restore transition", err)
		}
	})
}
