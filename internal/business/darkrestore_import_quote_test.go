package business

import (
	"context"
	"errors"
	"fmt"
	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"reflect"
	"strings"
	"testing"
	"time"
)

func syntheticImportIntent(groups int) DarkRestoreImportIntentV1 {
	now := time.Now().Unix()
	r := darkrestore.Review{Version: 1, Mode: "offline_operator_review_only", Status: "ready_for_review", SourceSHA256: strings.Repeat("a", 64), SourceSizeBytes: 4096, AsOfUnix: now,
		Summary: darkrestore.Report{Status: "ready_for_manual_review", Subscriptions: int64(groups), Groups: int64(groups), TotalConsumedBytes: fmt.Sprint(groups * 7)}}
	intent := DarkRestoreImportIntentV1{}
	for i := 0; i < groups; i++ {
		id := fmt.Sprintf("group-%03d", i)
		r.Groups = append(r.Groups, darkrestore.ReviewGroup{SourceGroupID: id, Name: id, Count: 1, HistoricalBytes: "7"})
		r.Clients = append(r.Clients, darkrestore.ReviewClient{SourceID: fmt.Sprintf("source-%03d", i), SourceGroupID: id, Label: "Synthetic customer", UUID: fmt.Sprintf("01234567-89ab-4cde-8fab-%012d", i), QuotaKind: "volume", QuotaBytes: "1000000", HistoricalBytes: "7", ExpiryUnix: now + 86400, Status: "requires_manual_review"})
		intent.Mappings = append(intent.Mappings, DarkRestoreGroupMappingV1{SourceGroupID: id, RepresentativeID: "rep-target"})
	}
	intent.Review = r
	return intent
}
func TestDarkRestoreQuoteMultipleGroupsNoCreditOrClientMutation(t *testing.T) {
	f := ownershipSetup(t)
	ctx := context.Background()
	if err := f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal("schema replay", err)
	}
	before := ownershipBalances(t, f.s)
	q, err := f.s.QuoteDarkRestoreImportV1(ctx, f.owner.Token, syntheticImportIntent(17))
	if err != nil {
		t.Fatal(err)
	}
	if q.Importable != 17 || q.Blocked != 0 || q.CreditChanged || q.UsageReset || q.XrayChanged {
		t.Fatal("unsafe quote", q)
	}
	for _, g := range q.Groups {
		if g.Importable != 1 || g.HistoricalBytes != "7" {
			t.Fatal("group counters lost", g)
		}
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("quote changed balances")
	}
	var count int
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM clients").Scan(&count); err != nil || count != 0 {
		t.Fatal("quote created clients", count, err)
	}
}
func TestDarkRestoreQuoteFailsClosedForRepresentativeAndUnknownMapping(t *testing.T) {
	f := ownershipSetup(t)
	ctx := context.Background()
	if err := f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal(err)
	}
	intent := syntheticImportIntent(1)
	if _, err := f.s.QuoteDarkRestoreImportV1(ctx, f.rep.Token, intent); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("representative quote permitted", err)
	}
	intent.Mappings[0].RepresentativeID = "missing-rep"
	if _, err := f.s.QuoteDarkRestoreImportV1(ctx, f.owner.Token, intent); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown representative accepted", err)
	}
}
func TestDarkRestoreQuotePromotedSkippedAndUncertainUUIDBlocked(t *testing.T) {
	f := ownershipSetup(t)
	ctx := context.Background()
	if err := f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal(err)
	}
	intent := syntheticImportIntent(2)
	intent.Review.Clients[0].Status = "skip_already_promoted"
	intent.Review.Clients[0].ReviewReasons = []string{"promoted_identity_must_not_be_imported_twice"}
	intent.Review.Groups[0].Promoted = 1
	intent.Review.Clients[1].UUID = ""
	intent.Review.Clients[1].ReviewReasons = []string{"uuid_not_verified"}
	q, err := f.s.QuoteDarkRestoreImportV1(ctx, f.owner.Token, intent)
	if err != nil || q.SkippedPromoted != 1 || q.Blocked != 1 || q.Importable != 0 {
		t.Fatal("unsafe identities accepted", q, err)
	}
}

func TestDarkRestoreQuoteDoesNotReactivateClientExpiredSinceReview(t *testing.T) {
	f := ownershipSetup(t)
	ctx := context.Background()
	if err := f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal(err)
	}
	intent := syntheticImportIntent(1)
	intent.Review.AsOfUnix = time.Now().Unix() - 86400
	intent.Review.Clients[0].ExpiryUnix = time.Now().Unix() - 1
	q, err := f.s.QuoteDarkRestoreImportV1(ctx, f.owner.Token, intent)
	if err != nil || q.Disabled != 1 || q.Importable != 1 {
		t.Fatal("expired client reactivated", q, err)
	}
}
