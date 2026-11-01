package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

const ownershipPassword = "synthetic-ownership-test-password"

type ownershipFixture struct {
	s     *Store
	path  string
	owner LoginSession
	rep   LoginSession
}

func ownershipSetup(t *testing.T) ownershipFixture {
	t.Helper()
	s, path := opened(t)
	ctx := context.Background()
	if _, e := s.BootstrapOwner(ctx, "ownership-owner", ownershipPassword); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"rep-source", "rep-target", "rep-third"} {
		newRep(t, s, id)
		grant(t, s, id, "ownership-credit-"+id, 9007199254741993, 100)
		if _, e := s.ProvisionRepresentativeLogin(ctx, id, id, ownershipPassword); e != nil {
			t.Fatal(e)
		}
	}
	for _, enable := range []func(context.Context) error{s.EnableUsageLedger, s.EnableVolumeExpiryRefunds, s.EnableClientOwnershipV1} {
		if e := enable(ctx); e != nil {
			t.Fatal(e)
		}
	}
	owner, e := s.Login(ctx, "ownership-owner", ownershipPassword)
	if e != nil {
		t.Fatal(e)
	}
	rep, e := s.Login(ctx, "rep-source", ownershipPassword)
	if e != nil {
		t.Fatal(e)
	}
	return ownershipFixture{s, path, owner, rep}
}
func ownershipNew(t *testing.T, f ownershipFixture, id string, unlimited bool) Client {
	t.Helper()
	v := int64(100)
	if unlimited {
		v = 0
	}
	c, e := f.s.CreateClient(context.Background(), NewClient{EventID: "ownership-new-" + id, ClientID: id, RepresentativeID: "rep-source", Name: "Synthetic " + id, DurationDays: 30, VolumeBytes: v, IPCount: 2, Unlimited: unlimited})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func ownershipIntent(ids ...string) ClientOwnershipIntent {
	return ClientOwnershipIntent{ClientIDs: ids, RepresentativeID: "rep-target", Reason: "Owner-authorized synthetic reassignment"}
}
func ownershipRequest(t *testing.T, s *Store, token, event string, intent ClientOwnershipIntent) ClientOwnershipCommit {
	t.Helper()
	q, e := s.QuoteClientOwnership(context.Background(), token, intent)
	if e != nil {
		t.Fatal(e)
	}
	return ClientOwnershipCommit{ClientOwnershipIntent: q.Intent, EventID: event, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix}
}
func ownershipMove(t *testing.T, f ownershipFixture, event string, intent ClientOwnershipIntent) ClientOwnershipReceipt {
	t.Helper()
	r, e := f.s.CommitClientOwnership(context.Background(), f.owner.Token, ownershipRequest(t, f.s, f.owner.Token, event, intent))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func ownershipBalances(t *testing.T, s *Store) []Representative {
	t.Helper()
	r, e := s.ListRepresentatives(context.Background(), 100, 0)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestOwnershipPreservesIdentityCountersCreditsAndGroups(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "volume", false)
	u := ownershipNew(t, f, "unlimited", true)
	epoch, box := refundNative(t, f.s, c, "transfer", 40, false)
	for _, g := range []NewGroup{{ID: "old-group", RepresentativeID: "rep-source", Name: "Before"}, {ID: "new-group", RepresentativeID: "rep-target", Name: "After"}} {
		if _, e := f.s.CreateGroup(ctx, g); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := f.s.AssignGroupMembers(ctx, "old-group", "rep-source", []string{c.ID, u.ID}); e != nil {
		t.Fatal(e)
	}
	before := ownershipBalances(t, f.s)
	var generation int64
	if e := f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generation); e != nil {
		t.Fatal(e)
	}
	req := ownershipRequest(t, f.s, f.owner.Token, "ownership-transfer-two-0001", ClientOwnershipIntent{ClientIDs: []string{u.ID, c.ID}, RepresentativeID: "rep-target", GroupID: "new-group", Reason: "Move selected customer records"})
	// Usage advances after preview; the commit captures the fresh cursor instead
	// of resetting it or forcing a continuously active customer to stop.
	report, e := box.Observe([]uc.Usage{{ClientID: c.ID, Uplink: 25, Downlink: 30}}, false)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := f.s.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, report)
	if e != nil {
		t.Fatal(e)
	}
	out, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req)
	if e != nil || out.Count != 2 || out.Replayed || out.BalanceChanged || out.UsageReset || out.CredentialsChanged {
		t.Fatal(out, e)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("transfer debited/credited an account")
	}
	for _, old := range []Client{c, u} {
		now, e := f.s.Client(ctx, old.ID)
		if e != nil {
			t.Fatal(e)
		}
		old.RepresentativeID = "rep-target"
		if now != old {
			t.Fatal("identity, quota or expiry changed")
		}
	}
	a, e := f.s.ClientUsage(ctx, c.ID)
	if e != nil || a.Total != 55 || !a.Known || a.RepresentativeID != "rep-target" {
		t.Fatal(a, e)
	}
	for _, item := range out.Clients {
		if item.PreviousGroupID != "old-group" || item.GroupID != "new-group" {
			t.Fatal(item)
		}
		if item.ClientID == c.ID && (item.Uplink != 25 || item.Downlink != 30) {
			t.Fatal("commit-time counters missing")
		}
	}
	var oldCount, newCount int
	f.s.db.QueryRow(`SELECT COUNT(*) FROM client_group_members WHERE group_id='old-group'`).Scan(&oldCount)
	f.s.db.QueryRow(`SELECT COUNT(*) FROM client_group_members WHERE group_id='new-group'`).Scan(&newCount)
	if oldCount != 0 || newCount != 2 {
		t.Fatal("group ownership leaked", oldCount, newCount)
	}
	var afterGeneration int64
	f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&afterGeneration)
	if generation != afterGeneration {
		t.Fatal("ownership invalidated runtime configuration")
	}
	replay, e := f.s.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, report)
	if e != nil || replay != receipt {
		t.Fatal("usage replay changed after transfer", e)
	}
	after, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req)
	if e != nil || !after.Replayed {
		t.Fatal(e)
	}
	after.Replayed = false
	if !reflect.DeepEqual(out, after) {
		t.Fatal("immutable transfer receipt changed")
	}
	req.Reason = "Changed request"
	if _, e = f.s.CommitClientOwnership(ctx, f.owner.Token, req); !errors.Is(e, ErrConflict) {
		t.Fatal("conflicting replay", e)
	}
}
func TestOwnershipRealCurrentScopeAndDestinationRenewal(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "scoped", false)
	original := NewClient{EventID: "ownership-new-scoped", ClientID: c.ID, RepresentativeID: "rep-source", Name: "Synthetic scoped", DurationDays: 30, VolumeBytes: 100, IPCount: 2}
	before := ownershipBalances(t, f.s)
	ownershipMove(t, f, "ownership-current-scope-0001", ownershipIntent(c.ID))
	ext := ExtendClient{EventID: "ownership-renew-after-0001", ClientID: c.ID, AdditionalDays: 30, AdditionalVolumeBytes: 20, Reason: "Destination pays only this renewal"}
	state := SetClientState{EventID: "ownership-state-after-0001", ClientID: c.ID, Action: "disable", Reason: "Current manager control"}
	for _, run := range []func() error{
		func() error { _, e := f.s.ExtendClientScoped(ctx, ext, "rep-source"); return e },
		func() error { _, e := f.s.SetClientStateScoped(ctx, state, "rep-source"); return e },
		func() error { _, e := f.s.CreateClientScoped(ctx, original, "rep-source"); return e },
	} {
		if e := run(); !errors.Is(e, ErrOwnershipDenied) {
			t.Fatal("old manager retained mutation/replay access", e)
		}
	}
	if _, e := f.s.ExtendClientScoped(ctx, ext, "rep-target"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ExtendClientScoped(ctx, ext, "rep-target"); e != nil {
		t.Fatal("renewal replay", e)
	}
	if _, e := f.s.SetClientStateScoped(ctx, state, "rep-target"); e != nil {
		t.Fatal(e)
	}
	after := ownershipBalances(t, f.s)
	for i := range before {
		want := before[i]
		if want.ID == "rep-target" {
			want.VolumeCreditBytes -= 20
		}
		if want != after[i] {
			t.Fatal("wrong renewal payer")
		}
	}
	page, e := f.s.SearchClients(ctx, ClientSearch{RepresentativeID: "rep-source", Limit: 50})
	if e != nil || page.Total != 0 {
		t.Fatal("old directory still owns client", e)
	}
	page, e = f.s.SearchClients(ctx, ClientSearch{RepresentativeID: "rep-target", Limit: 50})
	if e != nil || page.Total != 1 {
		t.Fatal("new directory missing client", e)
	}
}
func TestOwnershipGroupPreviewAllOrNothingAndStaleMembership(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	a := ownershipNew(t, f, "group-a", false)
	b := ownershipNew(t, f, "group-b", false)
	c := ownershipNew(t, f, "group-c", false)
	if _, e := f.s.CreateGroup(ctx, NewGroup{ID: "whole-group", RepresentativeID: "rep-source", Name: "Whole group"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.AssignGroupMembers(ctx, "whole-group", "rep-source", []string{a.ID, b.ID}); e != nil {
		t.Fatal(e)
	}
	intent := ClientOwnershipIntent{ClientIDs: []string{}, SourceGroupID: "whole-group", RepresentativeID: "rep-target", Reason: "Whole group assignment"}
	req := ownershipRequest(t, f.s, f.owner.Token, "ownership-whole-group-0001", intent)
	if _, e := f.s.AssignGroupMembers(ctx, "whole-group", "rep-source", []string{c.ID}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req); !errors.Is(e, ErrConflict) {
		t.Fatal("stale group snapshot accepted", e)
	}
	for _, id := range []string{a.ID, b.ID, c.ID} {
		v, _ := f.s.Client(ctx, id)
		if v.RepresentativeID != "rep-source" {
			t.Fatal("partial stale transfer")
		}
	}
	req = ownershipRequest(t, f.s, f.owner.Token, "ownership-whole-group-0002", intent)
	if _, e := f.s.db.Exec(`CREATE TRIGGER reject_test_transfer BEFORE INSERT ON client_ownership_entries_v1 WHEN NEW.client_id='group-b' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req); e == nil {
		t.Fatal("injected failure accepted")
	}
	for _, table := range []string{"client_ownership_receipts_v1", "client_ownership_entries_v1", "client_ownership_origins_v1"} {
		var n int
		f.s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		if n != 0 {
			t.Fatal("partial receipt/origin leaked", table)
		}
	}
	f.s.db.Exec(`DROP TRIGGER reject_test_transfer`)
	r, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req)
	if e != nil || r.Count != 3 {
		t.Fatal(r, e)
	}
	var groups int
	f.s.db.QueryRow(`SELECT COUNT(*) FROM client_groups WHERE id='whole-group'`).Scan(&groups)
	if groups != 1 {
		t.Fatal("deleted source group")
	}
}
func TestOwnershipOwnerAuthenticationGateAndReopen(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "auth", false)
	req := ownershipRequest(t, f.s, f.owner.Token, "ownership-auth-transfer-0001", ownershipIntent(c.ID))
	if _, e := f.s.CommitClientOwnership(ctx, f.rep.Token, req); !errors.Is(e, ErrOwnershipDenied) {
		t.Fatal("representative transferred clients", e)
	}
	if _, e := f.s.QuoteClientOwnership(ctx, strings.Repeat("f", 64), req.ClientOwnershipIntent); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("forged owner", e)
	}
	r, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req)
	if e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"client_ownership_origins_v1", "client_ownership_receipts_v1", "client_ownership_entries_v1"} {
		if _, e = f.s.db.Exec(`DELETE FROM ` + table); e == nil {
			t.Fatal("mutable transfer history", table)
		}
	}
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e := Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	after, e := s.CommitClientOwnership(ctx, f.owner.Token, req)
	if e != nil || !after.Replayed {
		t.Fatal("reopen lost receipt", e)
	}
	after.Replayed = false
	if !reflect.DeepEqual(r, after) {
		t.Fatal("reopened receipt changed")
	}
	if _, e = s.db.Exec(`UPDATE auth_sessions SET expires_unix=0 WHERE token_sha256=?`, digestSession(f.owner.Token)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CommitClientOwnership(ctx, f.owner.Token, req); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("expired owner replay allowed", e)
	}
}
func TestOwnershipNoDefaultSchemaAndCorruptSchemaRefused(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if e := s.EnableClientOwnershipV1(ctx); !errors.Is(e, ErrUnavailable) {
		t.Fatal("implicit accounting enable", e)
	}
	if e := s.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'client_ownership_*'`).Scan(&n)
	if n != 0 {
		t.Fatal("default-on migration")
	}
	if e := s.EnableClientOwnershipV1(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableClientOwnershipV1(ctx); e != nil {
		t.Fatal("repeat migration", e)
	}
	if _, e := s.db.Exec(`DROP TRIGGER client_ownership_entries_v1_no_update`); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableClientOwnershipV1(ctx); !errors.Is(e, ErrUnavailable) {
		t.Fatal("silently repaired provenance", e)
	}
}
func TestOwnershipUnknownUsageNotConvertedToKnownZero(t *testing.T) {
	ctx := context.Background()
	s, path := opened(t)
	newRep(t, s, "rep-source")
	newRep(t, s, "rep-target")
	grant(t, s, "rep-source", "ownership-unknown-credit-0001", 100, 0)
	c, e := s.CreateClient(ctx, NewClient{EventID: "ownership-unknown-create-0001", ClientID: "unknown", RepresentativeID: "rep-source", Name: "Historical unknown", DurationDays: 30, VolumeBytes: 100})
	if e != nil {
		t.Fatal(e)
	}
	s.BootstrapOwner(ctx, "ownership-owner", ownershipPassword)
	owner, e := s.Login(ctx, "ownership-owner", ownershipPassword)
	if e != nil {
		t.Fatal(e)
	}
	s.EnableUsageLedger(ctx)
	s.EnableClientOwnershipV1(ctx)
	f := ownershipFixture{s, path, owner, LoginSession{}}
	r := ownershipMove(t, f, "ownership-unknown-transfer-0001", ownershipIntent(c.ID))
	if r.Clients[0].UsagePresent || r.Clients[0].UsageComplete {
		t.Fatal("unknown receipt fabricated baseline")
	}
	u, e := s.ClientUsage(ctx, c.ID)
	if e != nil || u.Known || u.Eligible {
		t.Fatal("unknown usage became known/eligible", e)
	}
}
func TestOwnershipRefundFollowsProvenOriginalPayer(t *testing.T) {
	ctx := context.Background()
	for _, target := range []string{"rep-target", "owner"} {
		t.Run(target, func(t *testing.T) {
			f := ownershipSetup(t)
			c := ownershipNew(t, f, "refund-payer", false)
			refundNative(t, f.s, c, "closed", 40, true)
			before := ownershipBalances(t, f.s)
			in := ownershipIntent(c.ID)
			in.RepresentativeID = target
			ownershipMove(t, f, "ownership-refund-transfer-0001", in)
			if _, e := f.s.SetClientState(ctx, SetClientState{EventID: "ownership-refund-archive-0001", ClientID: c.ID, Action: "archive", Reason: "Synthetic end of service"}); e != nil {
				t.Fatal(e)
			}
			candidates, e := f.s.ListVolumeExpiryRefundCandidates(ctx, "", 100)
			if e != nil || len(candidates) != 1 {
				t.Fatal("original payer lost on sweep", e, len(candidates))
			}
			r, e := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			if e != nil || !r.Applied || r.Receipt == nil || r.Receipt.RepresentativeID != "rep-source" || r.Receipt.RefundedBytes != 60 {
				t.Fatal("wrong refund beneficiary", r, e)
			}
			after := ownershipBalances(t, f.s)
			for i := range before {
				want := before[i]
				if want.ID == "rep-source" {
					want.VolumeCreditBytes += 60
				}
				if want != after[i] {
					t.Fatal("refund redirected to manager")
				}
			}
			again, e := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			if e != nil || again.Applied || !again.Replayed {
				t.Fatal("duplicate refund", e)
			}
		})
	}
}
func TestOwnershipMixedPayerRefundNeverInventsARecipient(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "mixed", false)
	refundNative(t, f.s, c, "final", 40, true)
	ownershipMove(t, f, "ownership-mixed-transfer-0001", ownershipIntent(c.ID))
	c, e := f.s.ExtendClientScoped(ctx, ExtendClient{EventID: "ownership-mixed-renew-0001", ClientID: c.ID, AdditionalVolumeBytes: 50, Reason: "New representative funds a new extension"}, "rep-target")
	if e != nil {
		t.Fatal(e)
	}
	f.s.SetClientState(ctx, SetClientState{EventID: "ownership-mixed-archive-0001", ClientID: c.ID, Action: "archive", Reason: "Synthetic closure"})
	before := ownershipBalances(t, f.s)
	r, e := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if e != nil || r.Applied || r.Status.State != "funding_unproven" || r.Status.Reason != "transferred_mixed_or_unproven_funding_requires_review" {
		t.Fatal(r, e)
	}
	if !reflect.DeepEqual(before, ownershipBalances(t, f.s)) {
		t.Fatal("guessed mixed refund")
	}
}
func TestOwnershipSellerHistoryRemainsImmutableAndOldSellerCannotRenew(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	created := sellerOrderCreateV1(t, f, "seller-transferred", 100)
	if e := f.s.EnableClientOwnershipV1(ctx); e != nil {
		t.Fatal(e)
	}
	req := ownershipRequest(t, f.s, f.owner.Token, "ownership-seller-transfer-0001", ClientOwnershipIntent{ClientIDs: []string{created.Client.ID}, RepresentativeID: "repAccessB", Reason: "Transfer management only"})
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req); e != nil {
		t.Fatal(e)
	}
	replay, e := f.s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1(created.Client.ID, 100))
	if e != nil || !replay.Replayed {
		t.Fatal("historical seller receipt invalidated", e)
	}
	replay.Replayed = false
	if !reflect.DeepEqual(replay, created) {
		t.Fatal("seller receipt rewritten")
	}
	if _, e = f.s.ExtendSellerClientV1(ctx, f.rep.Token, SellerClientExtendV1{EventID: "ownership-oldseller-extend-0001", RepresentativeID: "repAccessA", SellerID: "sellerAlice", ClientID: created.Client.ID, AdditionalDays: 30, AdditionalVolumeBytes: 20, Reason: "Old seller denied"}); !errors.Is(e, ErrSellerPermissionDenied) {
		t.Fatal("old seller still manages client", e)
	}
	c, _ := f.s.Client(ctx, created.Client.ID)
	refundNative(t, f.s, c, "final", 40, true)
	f.s.SetClientState(ctx, SetClientState{EventID: "ownership-seller-archive-0001", ClientID: c.ID, Action: "archive", Reason: "Synthetic closure"})
	r, e := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if e != nil || !r.Applied || r.Receipt.RepresentativeID != "repAccessA" || r.Receipt.RefundedBytes != 60 {
		t.Fatal("seller's original funding lost", r, e)
	}
}
func TestOwnershipConcurrentTransfersAndEventNamespace(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "concurrent", false)
	req := ownershipRequest(t, f.s, f.owner.Token, "ownership-concurrent-event-0001", ownershipIntent(c.ID))
	second, e := Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	results := make(chan ClientOwnershipReceipt, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := f.s
			if i%2 == 1 {
				s = second
			}
			var r ClientOwnershipReceipt
			e := refundRetrySQLiteBusy(func() error { var e error; r, e = s.CommitClientOwnership(ctx, f.owner.Token, req); return e })
			errs <- e
			results <- r
		}(i)
	}
	wg.Wait()
	close(errs)
	close(results)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	applied := 0
	for r := range results {
		if !r.Replayed {
			applied++
		}
	}
	if applied != 1 {
		t.Fatal("duplicate transfer", applied)
	}
	if _, e = f.s.AdjustCredits(ctx, CreditChange{EventID: req.EventID, RepresentativeID: "rep-source", VolumeDeltaBytes: 1, Reason: "Nonce collision"}); !errors.Is(e, ErrConflict) {
		t.Fatal("transfer nonce reused as money", e)
	}
	if _, e = f.s.SetClientState(ctx, SetClientState{EventID: req.EventID, ClientID: c.ID, Action: "disable", Reason: "Nonce collision"}); !errors.Is(e, ErrConflict) {
		t.Fatal("transfer nonce reused as state change", e)
	}
}
func TestOwnershipExistingSubscriptionBodyTokenAndProofRemainValid(t *testing.T) {
	ctx := context.Background()
	f, round, secret := labSubscriptionFixture(t, true)
	materializeSubscription(t, f, round)
	before, e := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if e != nil {
		t.Fatal(e)
	}
	newRep(t, f.s, "subscription-target")
	owner, e := f.s.Login(ctx, "owner-lab", "synthetic-subscription-password")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.EnableClientOwnershipV1(ctx); e != nil {
		t.Fatal(e)
	}
	in := ClientOwnershipIntent{ClientIDs: []string{f.client.ID}, RepresentativeID: "subscription-target", Reason: "Preserve delivered config identity"}
	if _, e = f.s.CommitClientOwnership(ctx, owner.Token, ownershipRequest(t, f.s, owner.Token, "ownership-subscription-0001", in)); e != nil {
		t.Fatal(e)
	}
	after, e := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if e != nil || !bytes.Equal(before.Raw, after.Raw) || !after.Snapshot.Current || after.Snapshot.ID != before.Snapshot.ID {
		t.Fatal("existing subscription revoked by management transfer", e)
	}
	again, e := f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if e != nil || again.Token != secret.Token {
		t.Fatal("subscription credential rotated", e)
	}
}
func TestOwnershipInvalidSelectionsAndStaleClient(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "invalid", false)
	for i, in := range []ClientOwnershipIntent{ownershipIntent(), ownershipIntent(c.ID, c.ID), {ClientIDs: []string{c.ID}, SourceGroupID: "both", RepresentativeID: "rep-target", Reason: "Invalid"}, ownershipIntent("../escape")} {
		if _, e := f.s.QuoteClientOwnership(ctx, f.owner.Token, in); !errors.Is(e, ErrInvalid) {
			t.Fatal(i, e)
		}
	}
	req := ownershipRequest(t, f.s, f.owner.Token, "ownership-stale-client-0001", ownershipIntent(c.ID))
	f.s.ExtendClient(ctx, ExtendClient{EventID: "ownership-before-quote-renew", ClientID: c.ID, AdditionalDays: 1, Reason: "Concurrent entitlement update"})
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, req); !errors.Is(e, ErrConflict) {
		t.Fatal("stale entitlement accepted", e)
	}
	// Exact integer wire amounts never round at 2^53.
	encoded, _ := json.Marshal(ClientOwnershipItem{Uplink: 9007199254740993})
	if !bytes.Contains(encoded, []byte(`"uplink_bytes":"9007199254740993"`)) {
		t.Fatal(string(encoded))
	}
}

func TestOwnershipBackupRoundtripAndContradictoryLineageRefused(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "backup-transfer", false)
	original := ownershipRequest(t, f.s, f.owner.Token, "ownership-backup-transfer-0001", ownershipIntent(c.ID))
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, original); e != nil {
		t.Fatal(e)
	}
	if e := f.s.Close(); e != nil {
		t.Fatal(e)
	}
	result, e := ValidateOfflineBackup(ctx, f.path)
	if e != nil || result.TableCounts["client_ownership_entries_v1"] != 1 || result.TableCounts["client_ownership_origins_v1"] != 1 {
		t.Fatal("backup omitted transfer metadata", result, e)
	}
	s, e := Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := s.CommitClientOwnership(ctx, f.owner.Token, original)
	if e != nil || !replay.Replayed {
		t.Fatal("reopen replay", e)
	}
	if _, e = s.db.Exec(`UPDATE clients SET representative_id='rep-third' WHERE id='backup-transfer'`); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateOfflineBackup(ctx, f.path); !errors.Is(e, ErrOfflineBackupInvalid) {
		t.Fatal("contradictory lineage certified for backup", e)
	}
	if reopened, e := Open(f.path); e == nil {
		reopened.Close()
		t.Fatal("contradictory current owner accepted")
	}
}
func TestOwnershipRepeatedTransfersKeepOriginalHistoryAndOldReceipt(t *testing.T) {
	ctx := context.Background()
	f := ownershipSetup(t)
	c := ownershipNew(t, f, "returning", false)
	first := ownershipRequest(t, f.s, f.owner.Token, "ownership-first-hop-0001", ownershipIntent(c.ID))
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, first); e != nil {
		t.Fatal(e)
	}
	next := ownershipIntent(c.ID)
	next.RepresentativeID = "rep-third"
	ownershipMove(t, f, "ownership-second-hop-0001", next)
	next.RepresentativeID = "rep-source"
	r := ownershipMove(t, f, "ownership-third-hop-0001", next)
	if r.Clients[0].Revision != 3 {
		t.Fatal("revision reset")
	}
	if _, e := f.s.CommitClientOwnership(ctx, f.owner.Token, first); e != nil {
		t.Fatal(e)
	}
	current, e := f.s.Client(ctx, c.ID)
	if e != nil || current != c {
		t.Fatal("old receipt rewound later transfer", e)
	}
	if e = validateOwnershipData(ctx, f.s.db); e != nil {
		t.Fatal("valid lineage rejected", e)
	}
}
