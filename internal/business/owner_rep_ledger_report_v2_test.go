package business

import (
	"context"
	"errors"
	"testing"
)

func TestOwnerLedgerReportWithoutSellerSchemaNeverReportsFakeZero(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "repReportA")
	grant(t, s, "repReportA", "owner-report-grant-0000001", 5<<30, 200)
	report, err := s.OwnerRepresentativeLedgerReport(ctx, "repReportA")
	if err != nil {
		t.Fatal(err)
	}
	if report.SellerQuotaState != "not_enabled" || report.ReservedVolumeBytes != "" ||
		report.ReservedUnlimitedUnits != "" || report.FreeVolumeBytes != "5368709120" ||
		report.FreeUnlimitedUnits != "200" || !report.Enabled || report.SellerCount != 0 {
		t.Fatalf("report invented escrow state: %+v", report)
	}
	if _, err := s.OwnerRepresentativeLedgerReport(ctx, "owner"); !errors.Is(err, ErrInvalid) {
		t.Fatal("owner must not impersonate rep", err)
	}
	if _, err := s.OwnerRepresentativeLedgerReport(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown rep was reported", err)
	}
}

func TestOwnerLedgerReportShowsExactFreeAndEscrow(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "repReportB")
	grant(t, s, "repReportB", "owner-report-grant-0000002", 1024, 100)
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateSellerV2(ctx, CreateSellerV2{EventID: "owner-report-seller-create-001",
		RepresentativeID: "repReportB", SellerID: "salesA", Name: "A", TelegramUserID: 77111})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateSellerV2(ctx, CreateSellerV2{EventID: "owner-report-seller-create-002",
		RepresentativeID: "repReportB", SellerID: "salesB", Name: "B", TelegramUserID: 77222})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		event, seller string
		bytes, units  int64
	}{
		{"owner-report-allocate-00001", "salesA", 256, 20},
		{"owner-report-allocate-00002", "salesB", 512, 30},
	} {
		if _, err = s.TransferSellerQuotaV2(ctx, SellerQuotaTransferV2{EventID: item.event,
			RepresentativeID: "repReportB", SellerID: item.seller, Action: "allocate",
			Bytes: item.bytes, Units: item.units, Reason: "Verified owner allocation"}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.OwnerRepresentativeLedgerReport(ctx, "repReportB")
	if err != nil {
		t.Fatal(err)
	}
	if report.SellerQuotaState != "enabled" || report.SellerCount != 2 ||
		report.FreeVolumeBytes != "256" || report.FreeUnlimitedUnits != "50" ||
		report.ReservedVolumeBytes != "768" || report.ReservedUnlimitedUnits != "50" {
		t.Fatalf("inconsistent finance snapshot: %+v", report)
	}
	if report.BalanceSemantics != "authoritative_representative_free_credit_not_total_sales_or_live_traffic" {
		t.Fatal("unknown accounting metric", report)
	}
}

func TestOwnerLedgerReportRejectsPartialOrCorruptSellerSchema(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "repReportC")
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE seller_quota_v2_meta(id INTEGER PRIMARY KEY,version INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OwnerRepresentativeLedgerReport(ctx, "repReportC"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("partial schema mistaken for zero escrow: %v", err)
	}
}

func TestOwnerLedgerReportDistinguishesEmptySchemaAndScopesExactBalances(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "reportExactA")
	newRep(t, s, "reportExactB")
	const reserve int64 = 9_007_199_254_740_993
	grant(t, s, "reportExactA", "report-exact-credit-00001", reserve+4, 10)
	grant(t, s, "reportExactB", "report-exact-credit-00002", 999, 20)
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	empty, err := s.OwnerRepresentativeLedgerReport(ctx, "reportExactA")
	if err != nil || empty.SellerQuotaState != "enabled" || empty.SellerCount != 0 ||
		empty.ReservedVolumeBytes != "0" || empty.ReservedUnlimitedUnits != "0" {
		t.Fatalf("configured zero quota mistaken for unavailable: %+v %v", empty, err)
	}
	for i, id := range []string{"reportExactA", "reportExactB"} {
		suffix := string(rune('a' + i))
		_, err := s.CreateSellerV2(ctx, CreateSellerV2{EventID: "report-exact-create-0000" + suffix,
			RepresentativeID: id, SellerID: "sameSellerID", Name: "Seller", TelegramUserID: int64(9876 + i)})
		if err != nil {
			t.Fatal(err)
		}
		amount := reserve
		if i == 1 {
			amount = 999
		}
		_, err = s.TransferSellerQuotaV2(ctx, SellerQuotaTransferV2{EventID: "report-exact-allocate-000" + suffix,
			RepresentativeID: id, SellerID: "sameSellerID", Action: "allocate", Bytes: amount, Units: int64(i + 1), Reason: "Owner allocation"})
		if err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.OwnerRepresentativeLedgerReport(ctx, "reportExactA")
	if err != nil || report.SellerCount != 1 || report.FreeVolumeBytes != "4" ||
		report.ReservedVolumeBytes != "9007199254740993" || report.FreeUnlimitedUnits != "9" ||
		report.ReservedUnlimitedUnits != "1" {
		t.Fatalf("lost integer precision or included another representative: %+v %v", report, err)
	}
}

func TestOwnerLedgerReportRejectsMissingSellerSchemaVersion(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "reportMissingMeta")
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM seller_quota_v2_meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OwnerRepresentativeLedgerReport(ctx, "reportMissingMeta"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing version treated as a valid zero balance: %v", err)
	}
}

func TestOwnerLedgerReportRejectsOverflowingSellerAggregate(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "reportOverflow")
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		suffix := string(rune('a' + i))
		grant(t, s, "reportOverflow", "report-overflow-grant-000"+suffix, MaxVolumeCredit, 0)
		_, err := s.CreateSellerV2(ctx, CreateSellerV2{EventID: "report-overflow-create-000" + suffix,
			RepresentativeID: "reportOverflow", SellerID: "seller" + suffix, Name: "Seller", TelegramUserID: int64(5000 + i)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.TransferSellerQuotaV2(ctx, SellerQuotaTransferV2{EventID: "report-overflow-allocate-000" + suffix,
			RepresentativeID: "reportOverflow", SellerID: "seller" + suffix, Action: "allocate", Bytes: MaxVolumeCredit, Reason: "Owner allocation"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.OwnerRepresentativeLedgerReport(ctx, "reportOverflow"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("overflowing aggregate returned a misleading amount: %v", err)
	}
}
