package telegramhub

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestOwnerReportReadsRealStoreWithoutChangingCreditOrEvents(t *testing.T) {
	store, err := business.Open(filepath.Join(t.TempDir(), "private", "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	for _, id := range []string{"repA", "repB"} {
		if _, err := store.CreateRepresentative(ctx, id, "نماینده <"+id+">"); err != nil {
			t.Fatal(err)
		}
	}
	const funded int64 = 9007199254740994
	if _, err := store.AdjustCredits(ctx, business.CreditChange{
		EventID: "owner-bot-test-credit-001", RepresentativeID: "repA",
		VolumeDeltaBytes: funded, UnlimitedDeltaUnits: 100, Reason: "Isolated owner report test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSellerV2(ctx, business.CreateSellerV2{
		EventID: "owner-bot-test-seller-001", RepresentativeID: "repA",
		SellerID: "sellerA", Name: "Test seller", TelegramUserID: 12345,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransferSellerQuotaV2(ctx, business.SellerQuotaTransferV2{
		EventID: "owner-bot-test-reserve-001", RepresentativeID: "repA", SellerID: "sellerA",
		Action: "allocate", Bytes: funded - 1, Units: 20, Reason: "Isolated seller reserve",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := store.OwnerRepresentativeLedgerReport(ctx, "repA")
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := store.ListEvents(ctx, "repA", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewOwnerReportHandler(ownerConfig(), store)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := h.Prepare(ctx, ownerMessage())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"&lt;repA&gt;", "(1 بایت)", "9007199254740993 بایت", "80 واحد", "20 واحد"} {
		if !strings.Contains(reply.Text, want) {
			t.Fatalf("real-store report lost %q: %s", want, reply.Text)
		}
	}
	after, err := store.OwnerRepresentativeLedgerReport(ctx, "repA")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("read-only command changed authoritative credit", before, after, err)
	}
	eventsAfter, err := store.ListEvents(ctx, "repA", 100, 0)
	if err != nil || !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("report command appended financial events", eventsBefore, eventsAfter, err)
	}
	other, err := store.OwnerRepresentativeLedgerReport(ctx, "repB")
	if err != nil || other.FreeVolumeBytes != "0" || other.FreeUnlimitedUnits != "0" || other.SellerCount != 0 {
		t.Fatal("other representative was changed", other, err)
	}
}
