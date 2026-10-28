package telegramreport

import (
	"strings"
	"testing"
)

const verified = "authoritative_representative_free_credit_not_total_sales_or_live_traffic"

func TestOwnerReportEscapesHTMLWithoutFakeReservedCredit(t *testing.T) {
	c := OwnerRepresentativeCard{
		RepresentativeID: "repQA", DisplayName: "<b>Alice & Bob</b>", Enabled: true,
		FreeVolumeBytes: "1073741824", FreeUnlimitedUnits: "200",
		SellerQuotaState: "not_enabled", BalanceSemantics: verified,
	}
	result, err := FormatOwnerRepresentativeFA(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "&lt;b&gt;Alice &amp; Bob&lt;/b&gt;") ||
		strings.Contains(result, "<b>Alice & Bob</b>") ||
		!strings.Contains(result, "هنوز فعال نشده") ||
		strings.Contains(result, "فروشندگان: 0") {
		t.Fatal("unescaped owner name or fake escrow claim", result)
	}
	if !strings.Contains(result, "1.00 GiB") || !strings.Contains(result, "200 واحد") {
		t.Fatal("credit formatting wrong", result)
	}
}
func TestOwnerReportTwoDimensionReservedAccounting(t *testing.T) {
	c := OwnerRepresentativeCard{RepresentativeID: "repA", DisplayName: "مهدی", Enabled: false,
		FreeVolumeBytes: "536870912", FreeUnlimitedUnits: "80", SellerQuotaState: "enabled",
		SellerCount: 2, ReservedVolumeBytes: "2147483648", ReservedUnlimitedUnits: "20",
		BalanceSemantics: verified}
	result, err := FormatOwnerRepresentativeFA(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"0.50 GiB", "2.00 GiB", "80 واحد", "20 واحد", "فروشندگان: 2", "🔴"} {
		if !strings.Contains(result, field) {
			t.Fatal("missing required report field", field, result)
		}
	}
}
func TestOwnerReportFailsClosedForUnverifiedMetrics(t *testing.T) {
	good := OwnerRepresentativeCard{RepresentativeID: "repA", DisplayName: "Valid",
		FreeVolumeBytes: "0", FreeUnlimitedUnits: "10", SellerQuotaState: "not_enabled", BalanceSemantics: verified}
	bad := []OwnerRepresentativeCard{}
	zero := good
	zero.SellerQuotaState = "corrupt"
	bad = append(bad, zero)
	zero = good
	zero.FreeUnlimitedUnits = "-1"
	bad = append(bad, zero)
	zero = good
	zero.FreeVolumeBytes = "01"
	bad = append(bad, zero)
	zero = good
	zero.BalanceSemantics = "online_traffic"
	bad = append(bad, zero)
	zero = good
	zero.SellerCount = 2
	bad = append(bad, zero)
	zero = good
	zero.SellerQuotaState = "enabled"
	zero.ReservedVolumeBytes = ""
	bad = append(bad, zero)
	for i, item := range bad {
		if _, err := FormatOwnerRepresentativeFA(item); err == nil {
			t.Fatalf("invalid report %d accepted", i)
		}
	}
}

func TestOwnerReportPreservesExactByteBalances(t *testing.T) {
	c := OwnerRepresentativeCard{RepresentativeID: "repA", DisplayName: "Representative",
		FreeVolumeBytes: "1", FreeUnlimitedUnits: "0", SellerQuotaState: "enabled", SellerCount: 1,
		ReservedVolumeBytes: "9007199254740993", ReservedUnlimitedUnits: "1", BalanceSemantics: verified}
	result, err := FormatOwnerRepresentativeFA(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"≈ 0.00 GiB (1 بایت)", "(9007199254740993 بایت)"} {
		if !strings.Contains(result, want) {
			t.Fatalf("exact balance missing: %q in %q", want, result)
		}
	}
}

func TestOwnerReportAcceptsFullUnicodeNameAndRejectsInvalidIdentity(t *testing.T) {
	good := OwnerRepresentativeCard{RepresentativeID: "repA", DisplayName: strings.Repeat("🦊", 128),
		FreeVolumeBytes: "0", FreeUnlimitedUnits: "0", SellerQuotaState: "not_enabled", BalanceSemantics: verified}
	result, err := FormatOwnerRepresentativeFA(good)
	if err != nil || !strings.Contains(result, good.DisplayName) {
		t.Fatalf("valid 128-rune name rejected: %v", err)
	}
	for _, name := range []string{strings.Repeat("a", 129), "\nInjected status", " ", string([]byte{0xff})} {
		bad := good
		bad.DisplayName = name
		if _, err := FormatOwnerRepresentativeFA(bad); err == nil {
			t.Fatalf("invalid name accepted: %q", name)
		}
	}
	for _, id := range []string{"owner", "bad/id", "<b>rep</b>", strings.Repeat("a", 65)} {
		bad := good
		bad.RepresentativeID = id
		if _, err := FormatOwnerRepresentativeFA(bad); err == nil {
			t.Fatalf("invalid identity accepted: %q", id)
		}
	}
}

func TestOwnerReportRejectsAmountsOutsideLedgerIntegerRange(t *testing.T) {
	good := OwnerRepresentativeCard{RepresentativeID: "repA", DisplayName: "Representative",
		FreeVolumeBytes: "9223372036854775807", FreeUnlimitedUnits: "0", SellerQuotaState: "enabled",
		ReservedVolumeBytes: "0", ReservedUnlimitedUnits: "0", BalanceSemantics: verified}
	if _, err := FormatOwnerRepresentativeFA(good); err != nil {
		t.Fatalf("int64 maximum rejected: %v", err)
	}
	for _, field := range []string{"free_bytes", "free_units", "reserved_bytes", "reserved_units"} {
		bad := good
		switch field {
		case "free_bytes":
			bad.FreeVolumeBytes = "9223372036854775808"
		case "free_units":
			bad.FreeUnlimitedUnits = "9223372036854775808"
		case "reserved_bytes":
			bad.ReservedVolumeBytes = "9223372036854775808"
		case "reserved_units":
			bad.ReservedUnlimitedUnits = "9223372036854775808"
		}
		if _, err := FormatOwnerRepresentativeFA(bad); err == nil {
			t.Fatalf("out-of-range %s accepted", field)
		}
	}
}
