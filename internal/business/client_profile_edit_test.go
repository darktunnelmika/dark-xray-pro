package business

import (
	"context"
	"errors"
	"testing"
	"time"
)

func profileTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "editA")
	newRep(t, s, "editB")
	grant(t, s, "editA", "editor-grant-credit-event-a-01", 20<<30, 100)
	grant(t, s, "editB", "editor-grant-credit-event-b-01", 20<<30, 100)
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range []NewClient{
		{EventID: "editor-new-unlimited-client-0001", ClientID: "editor-unlimited", RepresentativeID: "editA", Name: "Original unlimited", Unlimited: true, IPCount: 2, DurationDays: 30},
		{EventID: "editor-new-volume-client-00001", ClientID: "editor-volume", RepresentativeID: "editA", Name: "Original volume", VolumeBytes: 1 << 30, DurationDays: 30},
		{EventID: "editor-new-foreign-client-0001", ClientID: "editor-foreign", RepresentativeID: "editB", Name: "Foreign", VolumeBytes: 1 << 30, DurationDays: 30},
	} {
		if _, err := s.CreateClient(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	return s, ctx
}

func TestProfileEditorQuoteCommitAndReplay(t *testing.T) {
	s, ctx := profileTestStore(t)
	before, _ := s.Client(ctx, "editor-unlimited")
	account, _ := s.Representative(ctx, "editA")
	intent := ClientProfileEditIntent{ClientID: before.ID, Name: "نام دلخواه مشتری", PlanKind: "unlimited", IPCount: 5, AdditionalDays: 15, Reason: "Approved seat and duration upgrade"}
	quote, err := s.QuoteClientProfileEdit(ctx, intent, "editA")
	if err != nil || quote.UnlimitedCreditDebitUnits != "6" || !quote.Sufficient || quote.AppliedToXray {
		t.Fatalf("invalid quote %+v %v", quote, err)
	}
	afterQuote, _ := s.Client(ctx, before.ID)
	if afterQuote.IPCount != 2 || afterQuote.Name != before.Name {
		t.Fatal("quote wrote database")
	}
	input := ClientProfileEditCommit{ClientProfileEditIntent: intent, EventID: "v8-profile-save-event-000001", QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix}
	receipt, err := s.CommitClientProfileEdit(ctx, input, "editA")
	if err != nil || receipt.Client.UUID != before.UUID || receipt.Client.DurationDays != 45 ||
		receipt.Client.ChargeUnlimitedUnits != 8 || receipt.Client.IPCount != 5 || receipt.Client.Name != intent.Name ||
		receipt.AppliedToXray || receipt.CustomerTrafficChanged {
		t.Fatalf("edit changed identity or arithmetic %+v %v", receipt, err)
	}
	changed, _ := s.Representative(ctx, "editA")
	if changed.UnlimitedCreditUnits != account.UnlimitedCreditUnits-6 {
		t.Fatal("credit mismatch")
	}
	input.QuoteUnix = 1
	again, err := s.CommitClientProfileEdit(ctx, input, "editA")
	if err != nil || !again.Replayed {
		t.Fatalf("duplicate payment %+v %v", again, err)
	}
	current, _ := s.Representative(ctx, "editA")
	if current.UnlimitedCreditUnits != changed.UnlimitedCreditUnits {
		t.Fatal("replay debited twice")
	}
	input.Name = "Changed intent with reused event"
	if _, err = s.CommitClientProfileEdit(ctx, input, "editA"); !errors.Is(err, ErrConflict) {
		t.Fatal("event collision not rejected", err)
	}
	history, err := s.ClientProfileEditHistory(ctx, before.ID, "editA", 50, 0)
	if err != nil || len(history) != 1 || history[0].PreviousIPCount != 2 || history[0].NewIPCount != 5 ||
		history[0].UnlimitedCreditDebitUnits != "6" {
		t.Fatalf("audit not durable %+v %v", history, err)
	}
	if _, err = s.db.Exec("DELETE FROM client_profile_edits"); err == nil {
		t.Fatal("audit deleted")
	}
	usage, err := s.ClientUsage(ctx, before.ID)
	if err != nil || !usage.Known || usage.Total != 0 {
		t.Fatal("usage changed", usage, err)
	}
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("V7 rollback incompatible", version, err)
	}
}

func TestProfileEditorVolumeSlotsAccountingAndSafety(t *testing.T) {
	s, ctx := profileTestStore(t)
	old, _ := s.Client(ctx, "editor-volume")
	before, _ := s.Representative(ctx, "editA")
	intent := ClientProfileEditIntent{
		ClientID: old.ID, Name: "Volume custom name", PlanKind: "volume", IPCount: 5,
		AdditionalVolumeBytes: 2 << 30, Reason: "Confirmed 2GB additional volume",
	}
	q, err := s.QuoteClientProfileEdit(ctx, intent, "editA")
	if err != nil || q.VolumeCreditDebitBytes != "2147483648" || q.UnlimitedCreditDebitUnits != "0" {
		t.Fatalf("incorrect volume quote %+v %v", q, err)
	}
	got, err := s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{
		ClientProfileEditIntent: intent, EventID: "editor-volume-edit-0000000001",
		QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix,
	}, "editA")
	if err != nil || got.Client.VolumeBytes != 3<<30 || got.Client.UUID != old.UUID || got.Client.IPCount != 5 {
		t.Fatal("volume changed historical client identity", got, err)
	}
	after, _ := s.Representative(ctx, "editA")
	if after.VolumeCreditBytes != before.VolumeCreditBytes-(2<<30) || after.UnlimitedCreditUnits != before.UnlimitedCreditUnits {
		t.Fatal("volume/IP financial overcharge", after)
	}
	ipOnly := ClientProfileEditIntent{
		ClientID: old.ID, Name: got.Client.Name, PlanKind: "volume", IPCount: 0, Reason: "Restore unlimited IP count",
	}
	q, err = s.QuoteClientProfileEdit(ctx, ipOnly, "editA")
	if err != nil || q.VolumeCreditDebitBytes != "0" || q.UnlimitedCreditDebitUnits != "0" {
		t.Fatal("IP only edit charged", q, err)
	}
	paid, err := s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{
		ClientProfileEditIntent: ipOnly, EventID: "editor-ip-reset-vol-00000001",
		QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix,
	}, "editA")
	if err != nil || paid.Client.IPCount != 0 || paid.Client.VolumeBytes != 3<<30 {
		t.Fatal("IP only edit corrupted quota", paid, err)
	}
	if _, err = s.ClientProfileEditHistory(ctx, old.ID, "editB", 50, 0); !errors.Is(err, ErrConflict) {
		t.Fatal("representative saw another tenant's audit")
	}
}

func TestProfileEditorStaleSnapshotNoRefundExpiryAndNoDowngrade(t *testing.T) {
	s, ctx := profileTestStore(t)
	original, _ := s.Client(ctx, "editor-unlimited")
	for _, bad := range []ClientProfileEditIntent{
		{ClientID: original.ID, Name: "Decreased seats", PlanKind: "unlimited", IPCount: 1, Reason: "Do not silently refund"},
		{ClientID: original.ID, Name: "Wrong plan", PlanKind: "volume", IPCount: 2, Reason: "Unsafe kind conversion"},
	} {
		if _, err := s.QuoteClientProfileEdit(ctx, bad, "editA"); !errors.Is(err, ErrConflict) {
			t.Fatal("unsafe plan change accepted", err)
		}
	}
	intent := ClientProfileEditIntent{ClientID: original.ID, Name: "Manual rename", PlanKind: "unlimited",
		IPCount: 2, Reason: "Approved rename"}
	if _, err := s.QuoteClientProfileEdit(ctx, intent, "editB"); !errors.Is(err, ErrConflict) {
		t.Fatal("cross-tenant preview succeeded", err)
	}
	q, err := s.QuoteClientProfileEdit(ctx, intent, "editA")
	if err != nil || q.UnlimitedCreditDebitUnits != "0" {
		t.Fatal("rename charge", q, err)
	}
	if _, err = s.AdjustCredits(ctx, CreditChange{
		EventID: "v8-change-available-credit-00001", RepresentativeID: "editA",
		UnlimitedDeltaUnits: 1, Reason: "Another owner operation",
	}); err != nil {
		t.Fatal(err)
	}
	edit := ClientProfileEditCommit{
		ClientProfileEditIntent: intent, EventID: "v8-stale-snapshot-save-000001",
		QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix,
	}
	if _, err = s.CommitClientProfileEdit(ctx, edit, "editA"); !errors.Is(err, ErrConflict) {
		t.Fatalf("outdated preview not blocked %v", err)
	}
	current, _ := s.Client(ctx, original.ID)
	if current.Name != original.Name {
		t.Fatal("stale request wrote client")
	}
	q, err = s.QuoteClientProfileEdit(ctx, intent, "editA")
	if err != nil {
		t.Fatal(err)
	}
	edit.QuoteHash = q.QuoteHash
	edit.QuoteUnix = q.QuoteUnix - 190
	if _, err = s.CommitClientProfileEdit(ctx, edit, "editA"); !errors.Is(err, ErrConflict) {
		t.Fatal("expired quote committed", err)
	}
	edit.QuoteUnix = q.QuoteUnix
	if _, err = s.CommitClientProfileEdit(ctx, edit, "editA"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE clients SET expires_unix=? WHERE id=?",
		time.Now().Unix()-100, "editor-volume"); err != nil {
		t.Fatal(err)
	}
	renewal := ClientProfileEditIntent{ClientID: "editor-volume", Name: "Expired extended",
		PlanKind: "volume", IPCount: 0, AdditionalDays: 10, Reason: "Additional ten days"}
	next, err := s.QuoteClientProfileEdit(ctx, renewal, "editA")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{
		ClientProfileEditIntent: renewal, EventID: "v8-expired-edit-save-00000001",
		QuoteHash: next.QuoteHash, QuoteUnix: next.QuoteUnix,
	}, "editA")
	if err != nil || receipt.Client.ExpiresUnix < time.Now().Unix()+10*86400-5 {
		t.Fatalf("expiry incorrectly reused old past timestamp %+v %v", receipt, err)
	}
}
