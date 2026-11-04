package business

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDarkRestoreCutoverSnapshotBindsRenewalAndCurrentPayer(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "cutover-snapshot-import-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	source := intent.Review.Clients[0].SourceID
	sha := intent.Review.SourceSHA256
	beforeBalances := ownershipBalances(t, f.s)
	first, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("unstable read snapshot", err)
	}
	if first.AuthorizesMutation || first.NativeBaselineComplete || !first.NativeAccountPresent ||
		first.NativeUplink == nil || *first.NativeUplink != 0 || first.NativeDownlink == nil || *first.NativeDownlink != 0 ||
		len(first.StateSHA) != 64 || len(first.ImportReceiptSHA) != 64 || len(first.NativeBaselineSHA) != 64 ||
		first.ImportedHistoricalBytes != "7" || first.DeploymentState != "disabled" {
		t.Fatalf("fabricated baseline or invalid bindings: %+v", first)
	}
	if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
		t.Fatal("read changed balances")
	}
	ownershipMove(t, f, "cutover-snapshot-move-0001", ClientOwnershipIntent{ClientIDs: []string{first.ClientID}, RepresentativeID: "rep-third", Reason: "Synthetic cutover snapshot payer transfer"})
	moved, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil || moved.StateSHA == first.StateSHA || moved.RepresentativeID != "rep-third" ||
		moved.ImportReceiptSHA != first.ImportReceiptSHA || moved.NativeBaselineSHA != first.NativeBaselineSHA {
		t.Fatal("assignment did not invalidate destination snapshot", moved, err)
	}
	renewal := ExtendClient{EventID: "cutover-snapshot-renew-0001", ClientID: first.ClientID, AdditionalVolumeBytes: 20, Reason: "Synthetic snapshot current payer renewal"}
	if _, err = f.s.ExtendClientScoped(ctx, renewal, "rep-target"); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("previous payer retained access", err)
	}
	if _, err = f.s.ExtendClientScoped(ctx, renewal, "rep-third"); err != nil {
		t.Fatal(err)
	}
	renewed, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil || renewed.StateSHA == moved.StateSHA || renewed.CurrentEntitlement != moved.CurrentEntitlement+20 ||
		renewed.ImportReceiptSHA != first.ImportReceiptSHA || renewed.ImportedHistoricalBytes != first.ImportedHistoricalBytes ||
		renewed.NativeBaselineComplete {
		t.Fatal("renewal lost history or fabricated baseline", renewed, err)
	}
	if _, err = f.s.ExtendClientScoped(ctx, renewal, "rep-third"); err != nil {
		t.Fatal(err)
	}
	replayed, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil || replayed.StateSHA != renewed.StateSHA {
		t.Fatal("replay changed snapshot", err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil || !reflect.DeepEqual(recovered, replayed) {
		t.Fatal("snapshot changed after recovery", err)
	}
}

func TestDarkRestoreCutoverSnapshotRefusesInvalidOrMissingIdentity(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	if _, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, strings.Repeat("g", 64), "source"); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid digest accepted", err)
	}
	if _, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, strings.Repeat("a", 64), "source"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing identity manufactured", err)
	}
}

// The fixture seeds an isolated synthetic database; it is not an assertion about LAB or production measurements.
func TestDarkRestoreCutoverSnapshotKeepsMeasuredCountersAndHoldsDistinct(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "cutover-snapshot-measured-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	sha, source := intent.Review.SourceSHA256, intent.Review.Clients[0].SourceID
	original, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec("UPDATE pro_usage_accounts SET baseline_complete=1,uplink=3,downlink=4,holds=2 WHERE client_id=?", original.ClientID); err != nil {
		t.Fatal(err)
	}
	measured, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil || !measured.NativeBaselineComplete || *measured.NativeUplink != 3 || *measured.NativeDownlink != 4 || *measured.NativeHolds != 2 || measured.AuthorizesMutation || measured.StateSHA == original.StateSHA || measured.NativeBaselineSHA == original.NativeBaselineSHA {
		t.Fatal("measurement/hold snapshot incorrect", measured, err)
	}
}
