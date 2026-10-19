package business

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func bulkRenewSeed(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "renewA")
	newRep(t, s, "renewB")
	grant(t, s, "renewA", "renew-v7-grant-a-00001", 2000, 200)
	grant(t, s, "renewB", "renew-v7-grant-b-00001", 2000, 200)
	for i, item := range []struct {
		id, rep   string
		unlimited bool
		slots     int64
	}{
		{"v7-a-volume", "renewA", false, 0}, {"v7-a-unlimited", "renewA", true, 5},
		{"v7-b-volume", "renewB", false, 0}, {"v7-owner", "owner", false, 0},
	} {
		n := NewClient{EventID: fmt.Sprintf("renew-created-event-%04d", i),
			ClientID: item.id, RepresentativeID: item.rep, Name: "Test " + item.id,
			DurationDays: 30, IPCount: item.slots, Unlimited: item.unlimited}
		if !item.unlimited {
			n.VolumeBytes = 100
		}
		if _, err := s.CreateClient(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	return s, ctx
}
func TestBulkExtensionExactPriceAtomicMixedRepresentativesAndRetry(t *testing.T) {
	s, ctx := bulkRenewSeed(t)
	intent := BulkExtensionIntent{
		ClientIDs:      []string{"v7-owner", "v7-b-volume", "v7-a-unlimited", "v7-a-volume"},
		AdditionalDays: 30, Reason: "audited 30-day review",
	}
	before, err := s.Representative(ctx, "renewA")
	if err != nil {
		t.Fatal(err)
	}
	quote, err := s.QuoteBulkExtension(ctx, intent, "")
	if err != nil || quote.Count != 4 || !quote.Sufficient || quote.QuoteHash == "" || len(quote.Representatives) != 3 {
		t.Fatalf("invalid authoritative quote %+v %v", quote, err)
	}
	if quote.Representatives[0].RepresentativeID != "owner" || !quote.Representatives[0].OwnerExempt {
		t.Fatal("owner must not lose representative credit", quote.Representatives)
	}
	if quote.Representatives[1].RepresentativeID != "renewA" ||
		quote.Representatives[1].UnlimitedDebitUnits != "5" ||
		quote.Representatives[1].AvailableUnits != "195" {
		t.Fatal("cumulative user-month delta incorrect", quote.Representatives[1])
	}
	if quote.Representatives[2].RepresentativeID != "renewB" ||
		quote.Representatives[2].VolumeDebitBytes != "0" {
		t.Fatal("mixed representative group incorrect")
	}
	// Quote must neither deduct credit nor write client audit.
	check, err := s.Representative(ctx, "renewA")
	if err != nil || check.VolumeCreditBytes != before.VolumeCreditBytes || check.UnlimitedCreditUnits != before.UnlimitedCreditUnits {
		t.Fatal("quote mutated credit", err)
	}
	ops, err := s.ListClientOperations(ctx, "", 200, 0)
	if err != nil || len(ops) != 0 {
		t.Fatal("quote created immutable events")
	}
	c, err := s.Client(ctx, "v7-a-unlimited")
	if err != nil {
		t.Fatal(err)
	}
	uuid := c.UUID
	commit := BulkExtensionCommit{BulkExtensionIntent: intent, EventID: "renew-batch-commit-00001",
		QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix}
	result, err := s.CommitBulkExtension(ctx, commit, "")
	if err != nil || result.Count != 4 || result.Replayed || result.AppliedToXray || result.CustomerTrafficChanged {
		t.Fatalf("failed to commit full batch %+v %v", result, err)
	}
	after, err := s.Representative(ctx, "renewA")
	if err != nil || after.UnlimitedCreditUnits != before.UnlimitedCreditUnits-5 ||
		after.VolumeCreditBytes != before.VolumeCreditBytes {
		t.Fatal("incorrect representative debit", after, err)
	}
	c, err = s.Client(ctx, "v7-a-unlimited")
	if err != nil || c.UUID != uuid || c.DurationDays != 60 || c.ChargeUnlimitedUnits != 10 {
		t.Fatal("renewal identity/billing corrupted", c, err)
	}
	ops, err = s.ListClientOperations(ctx, "", 200, 0)
	if err != nil || len(ops) != 4 {
		t.Fatalf("expected 4 immutable operations, got %d: %v", len(ops), err)
	}
	for _, op := range ops {
		if op.Kind != "extend" || op.AddedDays != 30 || op.VolumeCreditDebit != 0 {
			t.Fatalf("inconsistent bulk audit: %+v", op)
		}
	}
	commit.ClientIDs = []string{"v7-a-volume", "v7-owner", "v7-a-unlimited", "v7-b-volume"}
	commit.QuoteUnix = 1 // identical lost-ACK replay still succeeds without new debit
	again, err := s.CommitBulkExtension(ctx, commit, "")
	if err != nil || !again.Replayed || again.Count != 4 {
		t.Fatalf("lost acknowledgement retry: %+v %v", again, err)
	}
	repeat, err := s.Representative(ctx, "renewA")
	if err != nil || repeat.UnlimitedCreditUnits != after.UnlimitedCreditUnits {
		t.Fatal("retry charged twice", err)
	}
	collision := commit
	collision.ClientIDs = []string{"v7-a-volume", "v7-owner"} // same event, DIFFERENT batch
	if _, err = s.CommitBulkExtension(ctx, collision, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused event id changed selection %v", err)
	}
	ops, err = s.ListClientOperations(ctx, "", 200, 0)
	if err != nil || len(ops) != 4 {
		t.Fatal("retry/collision duplicated audit")
	}
}
func TestBulkExtensionInsufficientOrCrossTenantCannotPartiallyCharge(t *testing.T) {
	s, ctx := bulkRenewSeed(t)
	intent := BulkExtensionIntent{ClientIDs: []string{"v7-a-volume", "v7-b-volume"}, AdditionalVolumeBytes: 3000, Reason: "must reject"}
	quote, err := s.QuoteBulkExtension(ctx, intent, "")
	if err != nil || quote.Sufficient {
		t.Fatal("low credits not detected", quote, err)
	}
	before, _ := s.Representative(ctx, "renewA")
	req := BulkExtensionCommit{BulkExtensionIntent: intent, EventID: "v7-insufficient-batch-0001", QuoteUnix: quote.QuoteUnix, QuoteHash: quote.QuoteHash}
	if _, err = s.CommitBulkExtension(ctx, req, ""); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("insufficient charged? %v", err)
	}
	a, _ := s.Client(ctx, "v7-a-volume")
	b, _ := s.Client(ctx, "v7-b-volume")
	if a.VolumeBytes != 100 || b.VolumeBytes != 100 {
		t.Fatal("partial bulk extension on insufficient credit")
	}
	current, _ := s.Representative(ctx, "renewA")
	if current.VolumeCreditBytes != before.VolumeCreditBytes {
		t.Fatal("credit partially debited")
	}
	if _, err = s.QuoteBulkExtension(ctx, BulkExtensionIntent{
		ClientIDs: []string{"v7-a-volume", "v7-b-volume"}, AdditionalDays: 30, Reason: "tenant test"}, "renewA"); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-representative quote succeeded: %v", err)
	}
	valid := BulkExtensionIntent{ClientIDs: []string{"v7-a-volume"}, AdditionalDays: 30, Reason: "own scope"}
	q, err := s.QuoteBulkExtension(ctx, valid, "renewA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitBulkExtension(ctx, BulkExtensionCommit{
		BulkExtensionIntent: valid, EventID: "v7-cross-owner-attempt-00001",
		QuoteUnix: q.QuoteUnix, QuoteHash: q.QuoteHash,
	}, "renewB"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign commit accepted %v", err)
	}
	c, _ := s.Client(ctx, "v7-a-volume")
	if c.DurationDays != 30 {
		t.Fatal("foreign commit mutated client")
	}
}
func TestBulkExtensionStaleReviewAndTamperingFailClosed(t *testing.T) {
	s, ctx := bulkRenewSeed(t)
	intent := BulkExtensionIntent{ClientIDs: []string{"v7-a-unlimited"}, AdditionalDays: 15, Reason: "review"}
	q, err := s.QuoteBulkExtension(ctx, intent, "renewA")
	if err != nil || q.Representatives[0].UnlimitedDebitUnits != "3" {
		t.Fatal("wrong 15 day cost", q, err)
	}
	req := BulkExtensionCommit{BulkExtensionIntent: intent, EventID: "v7-stale-batch-00000001",
		QuoteHash: strings.Repeat("0", 64), QuoteUnix: q.QuoteUnix}
	if _, err = s.CommitBulkExtension(ctx, req, "renewA"); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered snapshot accepted: %v", err)
	}
	req.QuoteHash = q.QuoteHash
	req.QuoteUnix = q.QuoteUnix - 250
	if _, err = s.CommitBulkExtension(ctx, req, "renewA"); !errors.Is(err, ErrConflict) {
		t.Fatal("expired quote accepted", err)
	}
	req.QuoteUnix = q.QuoteUnix
	if _, err = s.AdjustCredits(ctx, CreditChange{EventID: "v7-external-balance-change-001",
		RepresentativeID: "renewA", UnlimitedDeltaUnits: 1, Reason: "concurrent balance change"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitBulkExtension(ctx, req, "renewA"); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed credit accepted stale quote: %v", err)
	}
	q, err = s.QuoteBulkExtension(ctx, intent, "renewA")
	if err != nil {
		t.Fatal(err)
	}
	req.QuoteHash = q.QuoteHash
	req.QuoteUnix = q.QuoteUnix
	if _, err = s.CommitBulkExtension(ctx, req, "renewA"); err != nil {
		t.Fatal("fresh quote should commit", err)
	}
	c, _ := s.Client(ctx, "v7-a-unlimited")
	if c.DurationDays != 45 || c.ChargeUnlimitedUnits != 8 {
		t.Fatalf("15-day split formula %v", c)
	}
	nextIntent := BulkExtensionIntent{ClientIDs: []string{"v7-a-unlimited"}, AdditionalDays: 15, Reason: "second half"}
	next, err := s.QuoteBulkExtension(ctx, nextIntent, "renewA")
	if err != nil || next.Representatives[0].UnlimitedDebitUnits != "2" {
		t.Fatal("differential overcharged 15+15", next, err)
	}
	if _, err = s.CommitBulkExtension(ctx, BulkExtensionCommit{
		BulkExtensionIntent: nextIntent, EventID: "v7-next-half-0000000001",
		QuoteHash: next.QuoteHash, QuoteUnix: next.QuoteUnix,
	}, "renewA"); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Client(ctx, "v7-a-unlimited")
	if c.ChargeUnlimitedUnits != 10 || c.DurationDays != 60 {
		t.Fatal("cumulative debit drift", c)
	}
}
func TestBulkExtensionValidationArchiveExpiryAndAllOrNone(t *testing.T) {
	s, ctx := bulkRenewSeed(t)
	for _, bad := range []BulkExtensionIntent{
		{ClientIDs: []string{"v7-a-volume"}, Reason: "nothing"},
		{ClientIDs: []string{"v7-a-volume", "v7-a-volume"}, AdditionalDays: 2, Reason: "duplicates"},
		{ClientIDs: []string{"v7-a-volume"}, AdditionalDays: 3651, Reason: "too long"},
		{ClientIDs: []string{"v7-a-volume"}, AdditionalVolumeBytes: -1, Reason: "invalid bytes"},
		{ClientIDs: []string{"v7-a-volume"}, AdditionalDays: 1, Reason: "bad\nreason"},
		{ClientIDs: []string{"bad;DROP"}, AdditionalDays: 1, Reason: "SQL"},
	} {
		if _, err := s.QuoteBulkExtension(ctx, bad, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid intent accepted: %+v %v", bad, err)
		}
	}
	tooMany := BulkExtensionIntent{AdditionalDays: 1, Reason: "too many"}
	for i := 0; i < 51; i++ {
		tooMany.ClientIDs = append(tooMany.ClientIDs, fmt.Sprintf("v7-client-%03d", i))
	}
	if _, err := s.QuoteBulkExtension(ctx, tooMany, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf(">50 accepted: %v", err)
	}
	if _, err := s.SetClientState(ctx, SetClientState{
		EventID: "v7-archive-client-0000001", ClientID: "v7-b-volume", Action: "archive", Reason: "preserve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QuoteBulkExtension(ctx, BulkExtensionIntent{
		ClientIDs: []string{"v7-a-volume", "v7-b-volume"}, AdditionalDays: 10, Reason: "atomic archive"}, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("archived member not rejected: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE clients SET expires_unix=? WHERE id=?", time.Now().Unix()-500, "v7-a-volume"); err != nil {
		t.Fatal(err)
	}
	expiryIntent := BulkExtensionIntent{ClientIDs: []string{"v7-a-volume"}, AdditionalDays: 10, Reason: "expired renewal"}
	quote, err := s.QuoteBulkExtension(ctx, expiryIntent, "renewA")
	if err != nil || quote.Clients[0].EstimatedExpiresUnix <= time.Now().Unix() {
		t.Fatal("expiry not updated", err)
	}
	if _, err := s.CommitBulkExtension(ctx, BulkExtensionCommit{
		BulkExtensionIntent: expiryIntent, EventID: "v7-expired-commit-0000001", QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix,
	}, "renewA"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.Client(ctx, "v7-a-volume")
	now := time.Now().Unix()
	if c.ExpiresUnix < now+10*86400-5 || c.ExpiresUnix > now+10*86400+5 {
		t.Fatal("expired renewal did not restart at commit", c.ExpiresUnix)
	}
}
