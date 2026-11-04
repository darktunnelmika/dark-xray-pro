//go:build linux

package business

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDarkRestoreCutoverRefreshBeforePreparedDelivery(t *testing.T) {
	for _, mode := range []string{"current payer renewal", "owner assignment"} {
		t.Run(mode, func(t *testing.T) {
			f, evidence, key, _ := preparedCutoverFixture(t)
			ctx := context.Background()
			client, err := f.s.Client(ctx, evidence.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			previous, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, evidence.SourceSHA256, evidence.SourceID)
			if err != nil || previous.StateSHA != evidence.DestinationStateSHA {
				t.Fatal("approved prestate unavailable", err)
			}
			beforeBalances := ownershipBalances(t, f.s)
			cause := ""
			switch mode {
			case "current payer renewal":
				cause = "m5-refresh-renew-cause-0001"
				if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: cause, ClientID: client.ID, AdditionalDays: 1, AdditionalVolumeBytes: 20, Reason: "Synthetic current-payer pre-delivery renewal"}, client.RepresentativeID); err != nil {
					t.Fatal(err)
				}
			case "owner assignment":
				cause = "m5-refresh-owner-cause-0001"
				ownershipMove(t, f, cause, ClientOwnershipIntent{ClientIDs: []string{client.ID}, RepresentativeID: "rep-third", Reason: "Synthetic pre-delivery payer assignment"})
			}
			delivery := DarkRestorePreparedDeliveryInputV1{EventID: "m5-refresh-delivery-0001", InboundID: 801, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}
			if got, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, delivery); err == nil || len(got.Subscription.Content) != 0 {
				t.Fatal("unattributed state drift delivered credentials", err)
			}
			input := DarkRestoreCutoverRefreshInputV1{EventID: "m5-refresh-receipt-0001", CauseEventID: cause, Reason: "Bind one audited post-opening state change", PreviousSnapshot: previous}
			if _, err = f.s.RefreshDarkRestoreCutoverV1(ctx, f.rep.Token, evidence.SourceSHA256, evidence.SourceID, input); !errors.Is(err, ErrOwnershipDenied) {
				t.Fatal("representative refreshed cutover", err)
			}
			bad := input
			bad.EventID = "m5-refresh-bad-cause-0001"
			bad.CauseEventID = "m5-refresh-unknown-cause-0001"
			if _, err = f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, bad); !errors.Is(err, ErrDarkRestoreCutover) {
				t.Fatal("unknown cause refreshed cutover", err)
			}
			if _, err = f.s.db.Exec(`CREATE TRIGGER synthetic_refresh_failure BEFORE INSERT ON darkrestore_cutover_refresh_receipts_v1 BEGIN SELECT RAISE(ABORT,'synthetic refresh failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, input); err == nil {
				t.Fatal("failed refresh committed")
			}
			if _, err = f.s.db.Exec(`DROP TRIGGER synthetic_refresh_failure`); err != nil {
				t.Fatal(err)
			}
			var refreshRows int
			if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM darkrestore_cutover_refresh_receipts_v1`).Scan(&refreshRows); err != nil || refreshRows != 0 {
				t.Fatal("failed refresh left receipt", refreshRows, err)
			}
			refreshed, err := f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, input)
			if err != nil || refreshed.Replayed || refreshed.AuthorizesMutation || refreshed.Revision != 1 ||
				refreshed.PreviousSnapshot.StateSHA != previous.StateSHA ||
				refreshed.CurrentSnapshot.StateSHA == previous.StateSHA ||
				refreshed.TransitionSHA != evidence.TransitionSHA {
				t.Fatal("audited refresh failed", refreshed, err)
			}
			if mode == "current payer renewal" {
				if refreshed.CauseKind != "client_extension" || refreshed.CurrentFiniteEntitlement != previous.CurrentEntitlement+20 ||
					refreshed.RemainingBudgetBytes != evidence.RemainingBudgetBytes+20 {
					t.Fatal("renewal refresh accounting mismatch", refreshed)
				}
			} else {
				if refreshed.CauseKind != "ownership_assignment" || refreshed.CurrentSnapshot.RepresentativeID != "rep-third" ||
					refreshed.CurrentFiniteEntitlement != previous.CurrentEntitlement ||
					refreshed.RemainingBudgetBytes != evidence.RemainingBudgetBytes {
					t.Fatal("assignment refresh accounting mismatch", refreshed)
				}
				if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
					t.Fatal("assignment refresh changed balances")
				}
			}
			replay, err := f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, input)
			if err != nil || !replay.Replayed || replay.CurrentSnapshot.StateSHA != refreshed.CurrentSnapshot.StateSHA {
				t.Fatal("refresh replay changed receipt", replay, err)
			}
			delivered, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, delivery)
			if err != nil || delivered.Applied || delivered.Subscription.PathVerified ||
				!bytes.Contains(delivered.Subscription.Content, []byte(client.UUID)) ||
				delivered.Receipt.CurrentFiniteEntitlement != refreshed.CurrentFiniteEntitlement ||
				delivered.Receipt.RemainingBudgetBytes != refreshed.RemainingBudgetBytes {
				t.Fatal("refreshed canonical preparation failed", delivered.Receipt, err)
			}
			finalReplay, err := f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, input)
			if err != nil || !finalReplay.Replayed || finalReplay.CurrentSnapshot.StateSHA != refreshed.CurrentSnapshot.StateSHA {
				t.Fatal("immutable refresh replay disappeared after delivery", err)
			}
			afterDelivery := input
			afterDelivery.EventID = "m5-refresh-after-delivery-0001"
			if _, err = f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, afterDelivery); !errors.Is(err, ErrConflict) {
				t.Fatal("immutable final delivery was refreshed", err)
			}
			if err = f.s.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
				t.Fatal("refresh v16 backup failed", err)
			}
			reopened, err := Open(f.path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			recovered, err := reopened.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, delivery)
			if err != nil || !recovered.Receipt.Replayed || !bytes.Equal(recovered.Subscription.Content, delivered.Subscription.Content) {
				t.Fatal("refresh/delivery chain lost after reopen", err)
			}
		})
	}
}

func TestDarkRestoreCutoverRefreshRejectsCombinedDrift(t *testing.T) {
	f, evidence, _, _ := preparedCutoverFixture(t)
	ctx := context.Background()
	client, err := f.s.Client(ctx, evidence.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, evidence.SourceSHA256, evidence.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	cause := "m5-refresh-combined-renew-0001"
	if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: cause, ClientID: client.ID, AdditionalVolumeBytes: 20, Reason: "Synthetic first drift"}, client.RepresentativeID); err != nil {
		t.Fatal(err)
	}
	ownershipMove(t, f, "m5-refresh-combined-owner-0001", ClientOwnershipIntent{ClientIDs: []string{client.ID}, RepresentativeID: "rep-third", Reason: "Synthetic second drift"})
	input := DarkRestoreCutoverRefreshInputV1{EventID: "m5-refresh-combined-receipt", CauseEventID: cause, Reason: "Must not aggregate two causes", PreviousSnapshot: previous}
	if _, err = f.s.RefreshDarkRestoreCutoverV1(ctx, f.owner.Token, evidence.SourceSHA256, evidence.SourceID, input); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("combined unattributed drift refreshed", err)
	}
	var rows int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM darkrestore_cutover_refresh_receipts_v1`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("combined drift left receipt", rows, err)
	}
}
