package business

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

const sellerAccessPassword = "seller-access-test-password-123"

func setupSellerAccessV2(t *testing.T) (*Store, string, LoginSession, LoginSession) {
	t.Helper()
	s, path := opened(t)
	ctx := context.Background()
	if _, err := s.BootstrapOwner(ctx, "seller-owner", sellerAccessPassword); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"repAccessA", "repAccessB"} {
		newRep(t, s, id)
		grant(t, s, id, "seller-access-initial-credit-"+id, 1000, 100)
		if _, err := s.ProvisionRepresentativeLogin(ctx, id, "seller-"+strings.ToLower(id), sellerAccessPassword); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := s.Login(ctx, "seller-owner", sellerAccessPassword)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
	if err != nil {
		t.Fatal(err)
	}
	return s, path, owner, rep
}

func grantSellerAccessV2(t *testing.T, s *Store, owner LoginSession) LoginSession {
	t.Helper()
	ctx := context.Background()
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSellerPermissionV2(ctx, owner.Token, SellerPermissionChangeV2{
		EventID: "seller-access-owner-grant-0001", RepresentativeID: "repAccessA",
		ManageSellers: true, Reason: "Enable reviewed seller manager",
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func createAccessSellerV2(t *testing.T, s *Store, rep LoginSession) SellerQuotaV2 {
	t.Helper()
	seller, err := s.CreateSellerV2As(context.Background(), rep.Token, CreateSellerV2{
		EventID: "seller-access-create-alice-0001", RepresentativeID: "repAccessA",
		SellerID: "sellerAlice", Name: "Alice", TelegramUserID: 12345,
	})
	if err != nil {
		t.Fatal(err)
	}
	return seller
}

func TestSellerAccessDefaultsAndHistoricalV9Fingerprint(t *testing.T) {
	s, _, owner, rep := setupSellerAccessV2(t)
	ctx := context.Background()
	if rep.User.Permissions.ManageSellers || !rep.User.Permissions.AllEnabled() {
		t.Fatal("new permission widened legacy rights or changed AllEnabled", rep.User.Permissions)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name='seller_management_access_v2_meta'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("seller schema was automatically migrated", count, err)
	}
	control := RepresentativeControlChange{
		EventID: "seller-legacy-v9-rename-00001", RepresentativeID: "repAccessA", DisplayName: "Legacy renamed",
		Permissions: DefaultRepresentativePermissions(), Reason: "Historical seven-key event",
	}
	// Literal V9 JSON from before the new field: an eighth false key would
	// change the immutable fingerprint and reject historical exact replays.
	legacy := `{"kind":"representative-control-v9","value":{"event_id":"seller-legacy-v9-rename-00001","representative_id":"repAccessA","expected_revision":0,"display_name":"Legacy renamed","login_suspended":false,"permissions":{"view_clients":true,"create_clients":true,"renew_clients":true,"edit_clients":true,"manage_clients":true,"manage_groups":true,"view_finance":true},"reason":"Historical seven-key event"}}`
	expected := sha256.Sum256([]byte(legacy))
	if got := fingerprint("representative-control-v9", control); got != hex.EncodeToString(expected[:]) {
		t.Fatalf("historical V9 fingerprint changed: %s", got)
	}
	if _, err := s.UpdateRepresentativeControl(ctx, control); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
	if err != nil {
		t.Fatal(err)
	}
	permission, err := s.SellerPermissionV2(ctx, rep.Token, "repAccessA")
	if err != nil || permission.ManageSellers || permission.Revision != 0 {
		t.Fatal("permission did not default deny", permission, err)
	}
	if _, err = s.SellerPermissionV2(ctx, rep.Token, "repAccessB"); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("ungranted representative read another permission", err)
	}
	if _, err = s.ListSellersV2As(ctx, rep.Token, "repAccessA", 25, 0); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("legacy ManageClients implicitly granted sellers", err)
	}
	rep = grantSellerAccessV2(t, s, owner)
	if !rep.User.Permissions.ManageSellers || !rep.User.Permissions.AllEnabled() {
		t.Fatal("session did not load independent permission", rep.User.Permissions)
	}
	replayed, err := s.UpdateRepresentativeControl(ctx, control)
	if err != nil || !replayed.Replayed {
		t.Fatal("historical V9 event stopped replaying after grant", replayed, err)
	}
	permission, err = s.SellerPermissionV2(ctx, owner.Token, "repAccessA")
	if err != nil || !permission.ManageSellers || permission.Revision != 1 {
		t.Fatal("legacy replay modified independent grant", permission, err)
	}
	control.Permissions.ManageSellers = true
	if _, err = s.UpdateRepresentativeControl(ctx, control); !errors.Is(err, ErrInvalid) {
		t.Fatal("V9 update accepted independent permission", err)
	}
}

func TestSellerAccessRevocationRechecksStoredPrincipalBeforeWritesAndReplays(t *testing.T) {
	s, _, owner, before := setupSellerAccessV2(t)
	ctx := context.Background()
	rep := grantSellerAccessV2(t, s, owner)
	if _, err := s.VerifySession(ctx, before.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("grant did not invalidate prior session", err)
	}
	createAccessSellerV2(t, s, rep)
	allocation := SellerQuotaTransferV2{EventID: "seller-access-allocation-0001", RepresentativeID: "repAccessA",
		SellerID: "sellerAlice", Action: "allocate", Bytes: 200, Units: 20, Reason: "Scoped reserve"}
	if _, err := s.TransferSellerQuotaV2As(ctx, rep.Token, allocation); err != nil {
		t.Fatal(err)
	}
	foreign := allocation
	foreign.EventID = "seller-access-foreign-transfer-0001"
	foreign.RepresentativeID = "repAccessB"
	if _, err := s.TransferSellerQuotaV2As(ctx, rep.Token, foreign); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("cross-parent transfer accepted", err)
	}
	change := SellerPermissionChangeV2{EventID: "seller-access-owner-revoke-0001", RepresentativeID: "repAccessA",
		ExpectedRevision: 1, ManageSellers: false, Reason: "Revoke seller management"}
	if _, err := s.UpdateSellerPermissionV2(ctx, rep.Token, change); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("representative edited own access", err)
	}
	// Model the race boundary: API authentication succeeds, then an owner
	// revoke commits before the request enters the financial transaction.
	checked, err := s.VerifySession(ctx, rep.Token)
	if err != nil || !checked.User.Permissions.ManageSellers {
		t.Fatal(err)
	}
	revoked, err := s.UpdateSellerPermissionV2(ctx, owner.Token, change)
	if err != nil || revoked.Revision != 2 || !revoked.SessionsRevoked || revoked.ManageSellers {
		t.Fatal("revoke failed", revoked, err)
	}
	for _, input := range []SellerQuotaTransferV2{allocation, {
		EventID: "seller-access-after-revoke-0001", RepresentativeID: "repAccessA", SellerID: "sellerAlice",
		Action: "allocate", Bytes: 100, Units: 10, Reason: "Must reject stale request",
	}} {
		if _, err := s.TransferSellerQuotaV2As(ctx, checked.Token, input); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("revoked in-flight write/replay accepted", err)
		}
	}
	if _, err := s.CreateSellerV2As(ctx, checked.Token, CreateSellerV2{EventID: "seller-access-stale-create-0001",
		RepresentativeID: "repAccessA", SellerID: "stale", Name: "Stale", TelegramUserID: 222}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revoked in-flight create accepted", err)
	}
	fresh, err := s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
	if err != nil || fresh.User.Permissions.ManageSellers {
		t.Fatal("new session retained revoked permission", fresh.User.Permissions, err)
	}
	if _, err = s.TransferSellerQuotaV2As(ctx, fresh.Token, allocation); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("current denied session replayed private financial result", err)
	}
	replay, err := s.UpdateSellerPermissionV2(ctx, owner.Token, change)
	if err != nil || !replay.Replayed || replay.SessionsRevoked || replay.Revision != 2 {
		t.Fatal("revoke replay has side effects", replay, err)
	}
	if _, err = s.VerifySession(ctx, fresh.Token); err != nil {
		t.Fatal("exact revoke replay revoked a later session", err)
	}
	change.ManageSellers = true
	if _, err = s.UpdateSellerPermissionV2(ctx, owner.Token, change); !errors.Is(err, ErrConflict) {
		t.Fatal("permission event reuse accepted", err)
	}
	balance, _ := s.Representative(ctx, "repAccessA")
	seller, _ := s.SellerV2(ctx, "repAccessA", "sellerAlice")
	if balance.VolumeCreditBytes != 800 || balance.UnlimitedCreditUnits != 80 || seller.ReservedBytes != 200 || seller.ReservedUnits != 20 {
		t.Fatal("revocation/replays changed money", balance, seller)
	}
}

func TestSellerAccessStateRevisionConservationAndDurableAudit(t *testing.T) {
	s, path, owner, _ := setupSellerAccessV2(t)
	ctx := context.Background()
	rep := grantSellerAccessV2(t, s, owner)
	seller := createAccessSellerV2(t, s, rep)
	if seller.Revision != 0 {
		t.Fatal("unexpected initial state revision", seller)
	}
	allocation := SellerQuotaTransferV2{EventID: "seller-state-initial-reserve-0001", RepresentativeID: "repAccessA",
		SellerID: seller.ID, Action: "allocate", Bytes: 200, Units: 20, Reason: "Initial reserve"}
	if _, err := s.TransferSellerQuotaV2As(ctx, rep.Token, allocation); err != nil {
		t.Fatal(err)
	}
	change := SellerStateChangeV2{EventID: "seller-state-disable-alice-0001", RepresentativeID: "repAccessA",
		SellerID: seller.ID, Enabled: false, Reason: "Disable seller without moving funds"}
	result, err := s.UpdateSellerStateV2(ctx, rep.Token, change)
	if err != nil || result.Seller.Enabled || result.Seller.Revision != 1 || result.Seller.ReservedBytes != 200 || result.Seller.ReservedUnits != 20 {
		t.Fatal("disable altered reserve or revision", result, err)
	}
	replay, err := s.UpdateSellerStateV2(ctx, rep.Token, change)
	if err != nil || !replay.Replayed || replay.Seller.Revision != 1 {
		t.Fatal("state replay failed", replay, err)
	}
	changed := change
	changed.Reason = "Different command"
	if _, err = s.UpdateSellerStateV2(ctx, rep.Token, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("state replay changed command accepted", err)
	}
	changed.EventID = "seller-state-stale-enable-0001"
	changed.Enabled = true
	if _, err = s.UpdateSellerStateV2(ctx, rep.Token, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("stale state revision accepted", err)
	}
	allocation.EventID = "seller-state-disabled-allocate-0001"
	if _, err = s.TransferSellerQuotaV2As(ctx, rep.Token, allocation); !errors.Is(err, ErrConflict) {
		t.Fatal("disabled seller received new reserve", err)
	}
	allocation.EventID = "seller-state-disabled-reclaim-0001"
	allocation.Action = "reclaim"
	allocation.Bytes, allocation.Units = 100, 10
	if _, err = s.TransferSellerQuotaV2As(ctx, rep.Token, allocation); err != nil {
		t.Fatal("unused disabled reserve cannot be reclaimed", err)
	}
	changed.ExpectedRevision = 1
	if _, err = s.UpdateSellerStateV2(ctx, owner.Token, changed); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"representative_seller_permission_events_v2", "seller_state_events_v2", "seller_quota_actors_v2"} {
		if _, err = s.db.ExecContext(ctx, "DELETE FROM "+table); err == nil {
			t.Fatal("deletable audit", table)
		}
		if _, err = s.db.ExecContext(ctx, "UPDATE "+table+" SET event_id='tampered'"); err == nil {
			t.Fatal("editable audit", table)
		}
	}
	var actor string
	if err = s.db.QueryRowContext(ctx, `SELECT actor_user_id FROM seller_state_events_v2 WHERE event_id=?`, change.EventID).Scan(&actor); err != nil || actor != rep.User.ID {
		t.Fatal("state audit actor was not authenticated source", actor, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err := reopened.SellerV2As(ctx, rep.Token, "repAccessA", seller.ID)
	if err != nil || !current.Enabled || current.Revision != 2 || current.ReservedBytes != 100 || current.ReservedUnits != 10 {
		t.Fatal("restart lost seller state/reserve", current, err)
	}
	balance, err := reopened.Representative(ctx, "repAccessA")
	if err != nil || balance.VolumeCreditBytes+current.ReservedBytes != 1000 || balance.UnlimitedCreditUnits+current.ReservedUnits != 100 {
		t.Fatal("state/reclaim broke conservation", balance, current, err)
	}
	list, err := reopened.ListSellersV2As(ctx, rep.Token, "repAccessA", 25, 0)
	if err != nil || len(list) != 1 || list[0].Revision != 2 {
		t.Fatal("list missing persisted revision", list, err)
	}
}

func TestSellerAccessConcurrentRevisionAndTransferRetries(t *testing.T) {
	s, _, owner, _ := setupSellerAccessV2(t)
	ctx := context.Background()
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsOut := make(chan error, 2)
	for _, id := range []string{"seller-concurrent-grant-0001", "seller-concurrent-grant-0002"} {
		wg.Add(1)
		go func(eventID string) {
			defer wg.Done()
			_, err := s.UpdateSellerPermissionV2(ctx, owner.Token, SellerPermissionChangeV2{EventID: eventID,
				RepresentativeID: "repAccessA", ManageSellers: true, Reason: "Concurrent revision review"})
			errorsOut <- err
		}(id)
	}
	wg.Wait()
	close(errorsOut)
	success, conflicts := 0, 0
	for err := range errorsOut {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent permission edit lost revision", success, conflicts)
	}
	rep, err := s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
	if err != nil {
		t.Fatal(err)
	}
	createAccessSellerV2(t, s, rep)
	input := SellerQuotaTransferV2{EventID: "seller-concurrent-same-transfer-0001", RepresentativeID: "repAccessA",
		SellerID: "sellerAlice", Action: "allocate", Bytes: 200, Units: 20, Reason: "Same request retries"}
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.TransferSellerQuotaV2As(ctx, rep.Token, input); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	seller, err := s.SellerV2(ctx, "repAccessA", "sellerAlice")
	if err != nil || seller.ReservedBytes != 200 || seller.ReservedUnits != 20 {
		t.Fatal("concurrent authenticated retries duplicated debit", seller, err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seller_quota_actors_v2 WHERE event_id=?`, input.EventID).Scan(&count); err != nil || count != 1 {
		t.Fatal("actor audit duplicated", count, err)
	}
	// A session snapshot is not a source of authorization. Even if a grant
	// changes without deleting the session, the writer re-reads the grant.
	if _, err = s.db.ExecContext(ctx, `UPDATE representative_seller_permissions_v2 SET manage_sellers=0 WHERE representative_id='repAccessA'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransferSellerQuotaV2As(ctx, rep.Token, input); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("writer trusted previously cached permission", err)
	}
	encoded, err := json.Marshal(DefaultRepresentativePermissions())
	if err != nil || strings.Contains(string(encoded), "manage_sellers") {
		t.Fatal("legacy permission JSON gained false eighth key", string(encoded), err)
	}
}
