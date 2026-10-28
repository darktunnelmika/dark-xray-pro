package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type sellerOrderFixtureV1 struct {
	s          *Store
	path       string
	owner, rep LoginSession
	seller     SellerQuotaV2
}

func sellerOrdersFixtureV1(t *testing.T) sellerOrderFixtureV1 {
	t.Helper()
	ctx := context.Background()
	s, path, owner, _ := setupSellerAccessV2(t)
	rep := grantSellerAccessV2(t, s, owner)
	seller := createAccessSellerV2(t, s, rep)
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerOrdersV1(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransferSellerQuotaV2As(ctx, rep.Token, SellerQuotaTransferV2{EventID: "seller-orders-allocate-all-0001", RepresentativeID: "repAccessA", SellerID: seller.ID, Action: "allocate", Bytes: 1000, Units: 100, Reason: "Fund synthetic seller orders"}); err != nil {
		t.Fatal(err)
	}
	return sellerOrderFixtureV1{s, path, owner, rep, seller}
}

func sellerOrderRequestV1(id string, volume int64) SellerClientCreateV1 {
	return SellerClientCreateV1{EventID: "seller-order-create-" + id, RepresentativeID: "repAccessA", SellerID: "sellerAlice", ClientID: id,
		Name: "Synthetic " + id, DurationDays: 30, IPCount: 2, VolumeBytes: volume, Reason: "Synthetic seller sale"}
}
func sellerOrderCreateV1(t *testing.T, f sellerOrderFixtureV1, id string, volume int64) SellerOrderReceiptV1 {
	t.Helper()
	r, e := f.s.CreateSellerClientV1(context.Background(), f.rep.Token, sellerOrderRequestV1(id, volume))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func sellerOrderBalancesV1(t *testing.T, f sellerOrderFixtureV1, free, reserve, units int64) {
	t.Helper()
	ctx := context.Background()
	r, e := f.s.Representative(ctx, "repAccessA")
	if e != nil {
		t.Fatal(e)
	}
	q, e := f.s.SellerV2(ctx, "repAccessA", "sellerAlice")
	if e != nil {
		t.Fatal(e)
	}
	if r.VolumeCreditBytes != free || r.UnlimitedCreditUnits != 0 || q.ReservedBytes != reserve || q.ReservedUnits != units {
		t.Fatalf("bad conservation: rep=%+v seller=%+v", r, q)
	}
}

func TestSellerOrdersGateSchemaAndNoImplicitMigration(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if err := s.EnableSellerOrdersV1(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("implicit metering", err)
	}
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerOrdersV1(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("implicit sellers", err)
	}
	if err := s.EnableSellerQuotaV2(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSellerClientV1(ctx, strings.Repeat("a", 64), sellerOrderRequestV1("gate", 100)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("default-on orders", err)
	}
	var before, after int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerOrdersV1(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerOrdersV1(ctx); err != nil {
		t.Fatal("repeat enable", err)
	}
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&after); err != nil || before != after {
		t.Fatal("unrelated schema change", before, after, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER seller_order_receipts_no_update_v1`); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerOrdersV1(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("silently repaired financial schema", err)
	}
	t.Run("partial schema", func(t *testing.T) {
		f := sellerOrdersFixtureV1(t)
		if _, e := f.s.db.Exec(`DROP INDEX seller_order_creation_once_v1`); e != nil {
			t.Fatal(e)
		}
		if _, e := f.s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1("partial", 100)); !errors.Is(e, ErrUnavailable) {
			t.Fatal(e)
		}
		sellerOrderBalancesV1(t, f, 0, 1000, 100)
	})
}

func TestSellerOrdersCreateExactReceiptReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	req := sellerOrderRequestV1("stable", 100)
	r, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Replayed || r.VolumeDebitBytes != 100 || r.UnlimitedDebitUnits != 0 || r.Client.ChargedVolumeBytes != 100 || r.ReservedBytesBefore != 1000 || r.ReservedBytesAfter != 900 {
		t.Fatal(r)
	}
	c, err := f.s.Client(ctx, req.ClientID)
	if err != nil || c.UUID == "" || c.RepresentativeID != req.RepresentativeID {
		t.Fatal(c, err)
	}
	u, err := f.s.ClientUsage(ctx, c.ID)
	if err != nil || !u.Known || u.Total != 0 {
		t.Fatal("not seeded atomically", u, err)
	}
	var freeDelta, unitDelta int64
	if err = f.s.db.QueryRow(`SELECT volume_delta_bytes,unlimited_delta_units FROM events WHERE event_id=?`, req.EventID).Scan(&freeDelta, &unitDelta); err != nil || freeDelta != 0 || unitDelta != 0 {
		t.Fatal("invented representative debit", err)
	}
	sellerOrderBalancesV1(t, f, 0, 900, 100)
	again, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, req)
	if err != nil || !again.Replayed {
		t.Fatal(again, err)
	}
	again.Replayed = false
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(again)
	if string(a) != string(b) {
		t.Fatal("receipt changed on replay")
	}
	conflict := req
	conflict.Name = "Different sale"
	if _, err = f.s.CreateSellerClientV1(ctx, f.rep.Token, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting replay accepted", err)
	}
	if _, err = f.s.CreateSellerClientV1(ctx, f.owner.Token, req); !errors.Is(err, ErrConflict) {
		t.Fatal("replay changed actor", err)
	}
	_ = sellerOrderCreateV1(t, f, "second", 150)
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.Close()
	again, err = f.s.CreateSellerClientV1(ctx, f.rep.Token, req)
	if err != nil || !again.Replayed || again.ReservedBytesAfter != 900 {
		t.Fatal("lost persisted snapshot", again, err)
	}
	sellerOrderBalancesV1(t, f, 0, 750, 100)
}

func TestSellerOrdersInsufficientAndReceiptFailureRollbackEverything(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	if _, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1("too-large", 1001)); !errors.Is(err, ErrInsufficient) {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec(`CREATE TRIGGER reject_test_seller_order BEFORE INSERT ON seller_order_receipts_v1 BEGIN SELECT RAISE(ABORT,'injected durable receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1("rollback", 100)); err == nil {
		t.Fatal("accepted failed receipt")
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 100)
	for _, table := range []string{"clients", "seller_order_clients_v1", "seller_order_receipts_v1", "pro_usage_accounts"} {
		var n int
		if err := f.s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial order leaked", table, n, err)
		}
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE object_id='rollback'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("journal survived rollback", n, err)
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER reject_test_seller_order`); err != nil {
		t.Fatal(err)
	}
	_ = sellerOrderCreateV1(t, f, "rollback", 100)
}

func TestSellerOrdersPermissionsRecheckedForWritesAndReplays(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"manage_sellers", "create_clients", "renew_clients", "seller_disabled", "representative_disabled", "session_expired"} {
		t.Run(mode, func(t *testing.T) {
			f := sellerOrdersFixtureV1(t)
			req := sellerOrderRequestV1("permission", 100)
			r := sellerOrderCreateV1(t, f, req.ClientID, 100)
			ext := SellerClientExtendV1{EventID: "seller-order-extend-permission", RepresentativeID: req.RepresentativeID, SellerID: req.SellerID, ClientID: req.ClientID, AdditionalDays: 30, AdditionalVolumeBytes: 20, Reason: "Synthetic renewal"}
			if _, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "manage_sellers":
				if _, err := f.s.UpdateSellerPermissionV2(ctx, f.owner.Token, SellerPermissionChangeV2{EventID: "seller-order-revoke-manage", RepresentativeID: req.RepresentativeID, ExpectedRevision: 1, ManageSellers: false, Reason: "Revoke before retry"}); err != nil {
					t.Fatal(err)
				}
				f.rep, _ = f.s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
			case "create_clients", "renew_clients":
				control, err := f.s.RepresentativeControl(ctx, req.RepresentativeID)
				if err != nil {
					t.Fatal(err)
				}
				permissions := control.Permissions
				if mode == "create_clients" {
					permissions.CreateClients = false
				} else {
					permissions.RenewClients = false
				}
				if _, err = f.s.UpdateRepresentativeControl(ctx, RepresentativeControlChange{EventID: "seller-order-revoke-" + mode, RepresentativeID: req.RepresentativeID, ExpectedRevision: control.Revision, DisplayName: control.Representative.DisplayName, Permissions: permissions, Reason: "Revoke before retry"}); err != nil {
					t.Fatal(err)
				}
				f.rep, err = f.s.Login(ctx, "seller-repaccessa", sellerAccessPassword)
				if err != nil {
					t.Fatal(err)
				}
			case "seller_disabled":
				seller, err := f.s.SellerV2(ctx, req.RepresentativeID, req.SellerID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.UpdateSellerStateV2(ctx, f.rep.Token, SellerStateChangeV2{EventID: "seller-order-disable-seller", RepresentativeID: req.RepresentativeID, SellerID: req.SellerID, ExpectedRevision: seller.Revision, Enabled: false, Reason: "Suspend seller"}); err != nil {
					t.Fatal(err)
				}
			case "representative_disabled":
				if _, err := f.s.db.Exec(`UPDATE representatives SET enabled=0 WHERE id=?`, req.RepresentativeID); err != nil {
					t.Fatal(err)
				}
			case "session_expired":
				if _, err := f.s.db.Exec(`UPDATE auth_sessions SET expires_unix=0 WHERE token_sha256=?`, digestSession(f.rep.Token)); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "renew_clients" {
				if _, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, req); err == nil {
					t.Fatal("revoked creation replay", r)
				}
				fresh := sellerOrderRequestV1("denied", 100)
				if _, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, fresh); err == nil {
					t.Fatal("revoked new order")
				}
			}
			if mode != "create_clients" {
				if _, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); err == nil {
					t.Fatal("revoked renewal replay")
				}
				ext.EventID = "seller-order-new-denied-renew"
				if _, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); err == nil {
					t.Fatal("revoked new renewal")
				}
			}
			sellerOrderBalancesV1(t, f, 0, 880, 100)
		})
	}
}

func TestSellerOrdersTenantAndSellerOwnershipCannotBeRebound(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "binding", 100)
	other, err := f.s.Login(ctx, "seller-repaccessb", sellerAccessPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateSellerClientV1(ctx, other.Token, sellerOrderRequestV1("foreign", 10)); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("foreign principal", err)
	}
	if _, err = f.s.CreateSellerClientV1(ctx, strings.Repeat("a", 64), sellerOrderRequestV1("forged-token", 10)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("forged token", err)
	}
	if _, err = f.s.CreateSellerV2As(ctx, f.rep.Token, CreateSellerV2{EventID: "seller-orders-create-bob", RepresentativeID: "repAccessA", SellerID: "sellerBob", Name: "Bob", TelegramUserID: 67890}); err != nil {
		t.Fatal(err)
	}
	ext := SellerClientExtendV1{EventID: "seller-orders-cross-seller-renew", RepresentativeID: "repAccessA", SellerID: "sellerBob", ClientID: r.Client.ID, AdditionalDays: 30, AdditionalVolumeBytes: 10, Reason: "Invalid cross-seller renewal"}
	if _, err = f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("cross-seller renewal", err)
	}
	grant(t, f.s, "repAccessA", "seller-orders-legacy-credit", 50, 0)
	legacy, err := f.s.CreateClient(ctx, NewClient{EventID: "seller-orders-legacy-create", ClientID: "legacy", RepresentativeID: "repAccessA", Name: "Legacy native", DurationDays: 30, VolumeBytes: 50})
	if err != nil {
		t.Fatal(err)
	}
	ext.EventID = "seller-orders-rebind-legacy"
	ext.SellerID = "sellerAlice"
	ext.ClientID = legacy.ID
	if _, err = f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); !errors.Is(err, ErrSellerPermissionDenied) {
		t.Fatal("silently rebound existing client", err)
	}
	for _, q := range []string{`UPDATE seller_order_clients_v1 SET seller_id='sellerBob'`, `DELETE FROM seller_order_clients_v1`, `UPDATE seller_order_receipts_v1 SET volume_debit_bytes=0`, `DELETE FROM seller_order_receipts_v1`} {
		if _, err = f.s.db.Exec(q); err == nil {
			t.Fatal("mutable funding history", q)
		}
	}
	sellerOrderBalancesV1(t, f, 0, 900, 100)
}

func TestSellerOrdersConcurrentSpendingReplayAndQueuedRevocation(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := f.s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1(fmt.Sprintf("concurrent-%02d", i), 100))
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrInsufficient) {
			t.Fatal(e)
		}
	}
	if success != 10 {
		t.Fatal("overspend or lost capacity", success)
	}
	sellerOrderBalancesV1(t, f, 0, 0, 100)
	t.Run("same event across independent connections", func(t *testing.T) {
		f := sellerOrdersFixtureV1(t)
		second, e := Open(f.path)
		if e != nil {
			t.Fatal(e)
		}
		defer second.Close()
		receipts := make(chan SellerOrderReceiptV1, 12)
		errs := make(chan error, 12)
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				s := f.s
				if i%2 == 1 {
					s = second
				}
				var r SellerOrderReceiptV1
				e := refundRetrySQLiteBusy(func() error {
					var err error
					r, err = s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1("one-event", 100))
					return err
				})
				receipts <- r
				errs <- e
			}(i)
		}
		wg.Wait()
		close(errs)
		close(receipts)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		applied := 0
		for r := range receipts {
			if !r.Replayed {
				applied++
			}
		}
		if applied != 1 {
			t.Fatal("duplicate debit", applied)
		}
		sellerOrderBalancesV1(t, f, 0, 900, 100)
	})
	t.Run("queued call cannot use old grant", func(t *testing.T) {
		f := sellerOrdersFixtureV1(t)
		tx, e := f.s.db.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(`UPDATE representative_seller_permissions_v2 SET manage_sellers=0 WHERE representative_id='repAccessA'`); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() {
			_, e := f.s.CreateSellerClientV1(ctx, f.rep.Token, sellerOrderRequestV1("queued-revoke", 100))
			done <- e
		}()
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, ErrSellerPermissionDenied) {
			t.Fatal("cached grant used", e)
		}
		sellerOrderBalancesV1(t, f, 0, 1000, 100)
	})
}

func TestSellerOrdersRenewRefundFinalityAndRepresentativeReuse(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "refund-funded", 100)
	ext := SellerClientExtendV1{EventID: "seller-orders-topup-before-refund", RepresentativeID: r.RepresentativeID, SellerID: r.SellerID, ClientID: r.Client.ID, AdditionalDays: 15, AdditionalVolumeBytes: 50, Reason: "Fund renewal from seller"}
	r2, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext)
	if err != nil || r2.VolumeDebitBytes != 50 || r2.Client.ChargedVolumeBytes != 150 {
		t.Fatal(r2, err)
	}
	replay, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext)
	if err != nil || !replay.Replayed || replay.ReservedBytesAfter != 850 {
		t.Fatal(replay, err)
	}
	ext.AdditionalVolumeBytes = 51
	if _, err = f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting renewal", err)
	}
	c, err := f.s.Client(ctx, r.Client.ID)
	if err != nil {
		t.Fatal(err)
	}
	epoch, box := refundNative(t, f.s, c, "tail", 40, false)
	if _, err = f.s.SetClientState(ctx, SetClientState{EventID: "seller-orders-archive-refund", ClientID: c.ID, Action: "archive", Reason: "Synthetic customer deletion"}); err != nil {
		t.Fatal(err)
	}
	pending, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || pending.Applied || pending.Status.State == "funding_unproven" {
		t.Fatal("wrong pending result", pending, err)
	}
	sellerOrderBalancesV1(t, f, 0, 850, 100)
	final, err := box.Observe([]uc.Usage{{ClientID: c.ID, Uplink: 20, Downlink: 20}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, final); err != nil {
		t.Fatal(err)
	}
	paid, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !paid.Applied || paid.Receipt == nil || paid.Receipt.RefundedBytes != 110 {
		t.Fatal(paid, err)
	}
	sellerOrderBalancesV1(t, f, 110, 850, 100)
	if _, err = f.s.CreateClient(ctx, NewClient{EventID: "seller-orders-reuse-returned", ClientID: "reuse-returned", RepresentativeID: c.RepresentativeID, Name: "Reuse returned credit", DurationDays: 30, VolumeBytes: 110}); err != nil {
		t.Fatal(err)
	}
	if again, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c)); err != nil || !again.Replayed || again.Applied {
		t.Fatal(again, err)
	}
	sellerOrderBalancesV1(t, f, 0, 850, 100)
	usage, err := f.s.ClientUsage(ctx, c.ID)
	if err != nil || usage.Total != 40 {
		t.Fatal("usage reset", usage, err)
	}
}

func TestSellerOrdersExpiryRefundAfterReopenAndTimeOnlyRenewal(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "expired-funded", 100)
	c, _ := f.s.Client(ctx, r.Client.ID)
	refundNative(t, f.s, c, "final", 30, true)
	c = refundExpire(t, f.s, c)
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.Close()
	paid, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !paid.Applied || paid.Receipt.RefundedBytes != 70 {
		t.Fatal("reopen lost proof", paid, err)
	}
	ext := SellerClientExtendV1{EventID: "seller-orders-time-only-renew", RepresentativeID: c.RepresentativeID, SellerID: r.SellerID, ClientID: c.ID, AdditionalDays: 30, Reason: "Time only cannot revive returned bytes"}
	renewed, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext)
	if err != nil || renewed.Client.VolumeBytes != 30 || renewed.VolumeDebitBytes != 0 {
		t.Fatal(renewed, err)
	}
	sellerOrderBalancesV1(t, f, 70, 900, 100)
	u, err := f.s.ClientUsage(ctx, c.ID)
	if err != nil || u.Total != 30 || u.Eligible {
		t.Fatal("returned capacity revived", u, err)
	}
}

func TestSellerOrdersUnlimitedUnitsAndExactLargeVolume(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	req := sellerOrderRequestV1("unlimited", 0)
	req.Unlimited = true
	req.DurationDays = 60
	req.IPCount = 3
	r, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, req)
	if err != nil || r.UnlimitedDebitUnits != 6 || r.VolumeDebitBytes != 0 {
		t.Fatal(r, err)
	}
	ext := SellerClientExtendV1{EventID: "seller-orders-unlimited-15a", RepresentativeID: req.RepresentativeID, SellerID: req.SellerID, ClientID: req.ClientID, AdditionalDays: 15, Reason: "First partial month"}
	a, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext)
	if err != nil || a.UnlimitedDebitUnits != 2 {
		t.Fatal(a, err)
	}
	ext.EventID = "seller-orders-unlimited-15b"
	b, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext)
	if err != nil || b.UnlimitedDebitUnits != 1 || b.Client.ChargedUnlimitedUnits != 9 {
		t.Fatal(b, err)
	}
	c, _ := f.s.Client(ctx, req.ClientID)
	c = refundExpire(t, f.s, c)
	result, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || result.Applied {
		t.Fatal("unlimited refunded", result, err)
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 91)
	const large int64 = 9007199254740993
	grant(t, f.s, "repAccessA", "seller-orders-large-credit", large, 0)
	if _, err = f.s.TransferSellerQuotaV2As(ctx, f.rep.Token, SellerQuotaTransferV2{EventID: "seller-orders-large-allocation", RepresentativeID: "repAccessA", SellerID: "sellerAlice", Action: "allocate", Bytes: large, Reason: "Exact byte accounting"}); err != nil {
		t.Fatal(err)
	}
	big := sellerOrderCreateV1(t, f, "large", large)
	raw, err := json.Marshal(big)
	if err != nil || !strings.Contains(string(raw), `"volume_debit_bytes":"9007199254740993"`) || !strings.Contains(string(raw), `"charged_volume_bytes":"9007199254740993"`) {
		t.Fatal("JSON rounded volume", string(raw), err)
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 91)
}

func TestSellerOrdersNonceIsolationAndCorruptFundingRefused(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	req := sellerOrderRequestV1("nonce", 100)
	r := sellerOrderCreateV1(t, f, req.ClientID, 100)
	if _, err := f.s.AdjustCredits(ctx, CreditChange{EventID: req.EventID, RepresentativeID: req.RepresentativeID, VolumeDeltaBytes: 100, Reason: "Conflicting credit"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := f.s.TransferSellerQuotaV2As(ctx, f.rep.Token, SellerQuotaTransferV2{EventID: req.EventID, RepresentativeID: req.RepresentativeID, SellerID: req.SellerID, Action: "reclaim", Bytes: 100, Reason: "Conflicting reclaim"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	old := sellerOrderRequestV1("old-nonce", 10)
	old.EventID = "seller-orders-allocate-all-0001"
	if _, err := f.s.CreateSellerClientV1(ctx, f.rep.Token, old); !errors.Is(err, ErrConflict) {
		t.Fatal("reused allocation nonce", err)
	}
	ext := SellerClientExtendV1{EventID: "seller-order-renew-nonce", RepresentativeID: req.RepresentativeID, SellerID: req.SellerID, ClientID: req.ClientID, AdditionalDays: 1, AdditionalVolumeBytes: 10, Reason: "Renewal nonce"}
	if _, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, ext); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AdjustCredits(ctx, CreditChange{EventID: ext.EventID, RepresentativeID: req.RepresentativeID, VolumeDeltaBytes: 100, Reason: "Conflicting renewal credit"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	sellerOrderBalancesV1(t, f, 0, 890, 100)
	for _, mode := range []string{"second_initial_funder", "forged_free_debit", "missing_immutability", "unexplained_charge"} {
		t.Run(mode, func(t *testing.T) {
			f := sellerOrdersFixtureV1(t)
			r := sellerOrderCreateV1(t, f, "tamper", 100)
			c, _ := f.s.Client(ctx, r.Client.ID)
			refundNative(t, f.s, c, "final", 20, true)
			c = refundExpire(t, f.s, c)
			switch mode {
			case "second_initial_funder":
				_, err := f.s.db.Exec(`INSERT INTO events(event_id,fingerprint,kind,representative_id,object_id,reason,volume_delta_bytes,unlimited_delta_units,created_unix) VALUES('seller-orders-forged-origin','forged','client_created','repAccessA','tamper','ambiguous',-100,0,?)`, time.Now().Unix())
				if err != nil {
					t.Fatal(err)
				}
			case "forged_free_debit":
				if _, err := f.s.db.Exec(`DROP TRIGGER events_immutable_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(`UPDATE events SET volume_delta_bytes=-100 WHERE event_id=?`, r.EventID); err != nil {
					t.Fatal(err)
				}
			case "missing_immutability":
				if _, err := f.s.db.Exec(`DROP TRIGGER seller_order_receipts_no_update_v1`); err != nil {
					t.Fatal(err)
				}
			case "unexplained_charge":
				if _, err := f.s.db.Exec(`UPDATE clients SET volume_bytes=200,charged_volume_bytes=200 WHERE id='tamper'`); err != nil {
					t.Fatal(err)
				}
				c, _ = f.s.Client(ctx, c.ID)
			}
			result, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			if err == nil && result.Applied {
				t.Fatal("corrupt funding credited", mode, result)
			}
			sellerOrderBalancesV1(t, f, 0, 900, 100)
		})
	}
	_ = r
}

func TestSellerOrdersRenewalReceiptFailureRollsBackClientDebitAndUsage(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "renew-rollback", 100)
	before, err := f.s.Client(ctx, r.Client.ID)
	if err != nil {
		t.Fatal(err)
	}
	refundNative(t, f.s, before, "existing", 25, true)
	uBefore, err := f.s.ClientUsage(ctx, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := SellerClientExtendV1{EventID: "seller-orders-renewal-rollback", RepresentativeID: before.RepresentativeID, SellerID: r.SellerID, ClientID: before.ID, AdditionalDays: 15, AdditionalVolumeBytes: 50, Reason: "Synthetic renewal"}
	tooLarge := req
	tooLarge.EventID = "seller-orders-renewal-no-reserve"
	tooLarge.AdditionalVolumeBytes = 901
	if _, err = f.s.ExtendSellerClientV1(ctx, f.rep.Token, tooLarge); !errors.Is(err, ErrInsufficient) {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`CREATE TRIGGER reject_test_seller_renewal BEFORE INSERT ON seller_order_receipts_v1 WHEN NEW.action='extend' BEGIN SELECT RAISE(ABORT,'injected renewal receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ExtendSellerClientV1(ctx, f.rep.Token, req); err == nil {
		t.Fatal("renewal without durable receipt")
	}
	after, err := f.s.Client(ctx, before.ID)
	if err != nil || after != before {
		t.Fatal("partial client renewal", before, after, err)
	}
	uAfter, err := f.s.ClientUsage(ctx, before.ID)
	if err != nil || uAfter != uBefore {
		t.Fatal("usage changed after rollback", uBefore, uAfter, err)
	}
	var n int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM client_operations WHERE event_id=?`, req.EventID).Scan(&n); err != nil || n != 0 {
		t.Fatal("renewal journal leaked", n, err)
	}
	sellerOrderBalancesV1(t, f, 0, 900, 100)
	if _, err = f.s.db.Exec(`DROP TRIGGER reject_test_seller_renewal`); err != nil {
		t.Fatal(err)
	}
	paid, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, req)
	if err != nil || paid.VolumeDebitBytes != 50 {
		t.Fatal(paid, err)
	}
	uAfter, err = f.s.ClientUsage(ctx, before.ID)
	if err != nil || uAfter.Total != 25 {
		t.Fatal("renewal reset prior usage", uAfter, err)
	}
	sellerOrderBalancesV1(t, f, 0, 850, 100)
}

func TestSellerOrdersMixedRepresentativeTopupsAndProfileEditsReconcile(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "mixed-funding", 100)
	grant(t, f.s, "repAccessA", "seller-orders-mixed-topup-credit", 70, 0)
	c, err := f.s.ExtendClient(ctx, ExtendClient{EventID: "seller-orders-ordinary-topup", ClientID: r.Client.ID, AdditionalVolumeBytes: 30, Reason: "Representative funded topup"})
	if err != nil {
		t.Fatal(err)
	}
	intent := ClientProfileEditIntent{ClientID: c.ID, Name: "Edited seller client", PlanKind: c.PlanKind, IPCount: 3, AdditionalVolumeBytes: 40, Reason: "Representative funded profile growth"}
	quote, err := f.s.QuoteClientProfileEdit(ctx, intent, "repAccessA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{ClientProfileEditIntent: intent, EventID: r.EventID, QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix}, "repAccessA"); !errors.Is(err, ErrConflict) {
		t.Fatal("profile reused order event", err)
	}
	control, err := f.s.RepresentativeControl(ctx, "repAccessA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UpdateRepresentativeControl(ctx, RepresentativeControlChange{EventID: r.EventID, RepresentativeID: "repAccessA", ExpectedRevision: control.Revision, DisplayName: control.Representative.DisplayName, Permissions: control.Permissions, Reason: "Conflicting representative change"}); !errors.Is(err, ErrConflict) {
		t.Fatal("representative control reused order event", err)
	}
	edit, err := f.s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{ClientProfileEditIntent: intent, EventID: "seller-orders-ordinary-profile", QuoteHash: quote.QuoteHash, QuoteUnix: quote.QuoteUnix}, "repAccessA")
	if err != nil {
		t.Fatal(err)
	}
	if edit.Client.UUID != r.Client.UUID || edit.Client.VolumeBytes != 170 {
		t.Fatal(edit)
	}
	sellerOrderBalancesV1(t, f, 0, 900, 100)
	if _, err = f.s.ExtendSellerClientV1(ctx, f.rep.Token, SellerClientExtendV1{EventID: "seller-orders-after-rep-topups", RepresentativeID: "repAccessA", SellerID: r.SellerID, ClientID: c.ID, AdditionalVolumeBytes: 10, Reason: "Seller funds next topup"}); err != nil {
		t.Fatal(err)
	}
	c, err = f.s.Client(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	refundNative(t, f.s, c, "final", 60, true)
	c = refundExpire(t, f.s, c)
	paid, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !paid.Applied || paid.Receipt.FundedBytes != 180 || paid.Receipt.RefundedBytes != 120 {
		t.Fatal("mixed funding lost or counted twice", paid, err)
	}
	sellerOrderBalancesV1(t, f, 120, 890, 100)
	current, err := f.s.Client(ctx, c.ID)
	if err != nil || current.VolumeBytes != 60 || current.ChargeVolumeBytes != 180 {
		t.Fatal(current, err)
	}
}

func TestSellerOrdersConcurrentRefundRenewalConservesBothFunds(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "refund-race", 100)
	c, _ := f.s.Client(ctx, r.Client.ID)
	refundNative(t, f.s, c, "final", 30, true)
	c = refundExpire(t, f.s, c)
	other, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		<-start
		done <- refundRetrySQLiteBusy(func() error {
			_, e := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			if errors.Is(e, ErrConflict) {
				return nil
			}
			return e
		})
	}()
	go func() {
		<-start
		done <- refundRetrySQLiteBusy(func() error {
			_, e := other.ExtendSellerClientV1(ctx, f.rep.Token, SellerClientExtendV1{EventID: "seller-orders-renew-with-refund", RepresentativeID: c.RepresentativeID, SellerID: r.SellerID, ClientID: c.ID, AdditionalDays: 30, AdditionalVolumeBytes: 50, Reason: "Concurrent renewal"})
			return e
		})
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	rep, err := f.s.Representative(ctx, c.RepresentativeID)
	if err != nil {
		t.Fatal(err)
	}
	seller, err := f.s.SellerV2(ctx, c.RepresentativeID, r.SellerID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.s.Client(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seller.ReservedBytes != 850 || seller.ReservedUnits != 100 || rep.UnlimitedCreditUnits != 0 || rep.VolumeCreditBytes+seller.ReservedBytes+current.VolumeBytes != 1000 || current.ChargeVolumeBytes != 150 {
		t.Fatal("refund/renew double credit or lost capacity", rep, seller, current)
	}
	u, err := f.s.ClientUsage(ctx, c.ID)
	if err != nil || u.Total != 30 {
		t.Fatal("concurrent renewal reset usage", u, err)
	}
}

func TestSellerOrdersFundingIncludesHistoryBeyondOnePage(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	r := sellerOrderCreateV1(t, f, "paged-funding", 100)
	plan, err := f.s.db.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT `+sellerOrderColumnsV1+` FROM seller_order_receipts_v1 WHERE client_id=? AND event_id>? ORDER BY event_id LIMIT 64`, r.Client.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	usesKeysetIndex := false
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			plan.Close()
			t.Fatal(err)
		}
		if strings.Contains(detail, "seller_order_client_history_v1") {
			usesKeysetIndex = true
		}
		if strings.Contains(detail, "TEMP B-TREE") {
			plan.Close()
			t.Fatal("keyset history requires a temporary sort", detail)
		}
	}
	if err := plan.Err(); err != nil {
		plan.Close()
		t.Fatal(err)
	}
	if err := plan.Close(); err != nil {
		t.Fatal(err)
	}
	if !usesKeysetIndex {
		t.Fatal("history query did not use its client/event keyset index")
	}
	for i := 0; i < 65; i++ {
		if _, err := f.s.ExtendSellerClientV1(ctx, f.rep.Token, SellerClientExtendV1{EventID: fmt.Sprintf("seller-orders-paged-renew-%03d", i), RepresentativeID: r.RepresentativeID, SellerID: r.SellerID, ClientID: r.Client.ID, AdditionalVolumeBytes: 1, Reason: "Small exact topup"}); err != nil {
			t.Fatal(i, err)
		}
	}
	c, err := f.s.Client(ctx, r.Client.ID)
	if err != nil {
		t.Fatal(err)
	}
	refundNative(t, f.s, c, "final", 40, true)
	c = refundExpire(t, f.s, c)
	paid, err := f.s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !paid.Applied || paid.Receipt.FundedBytes != 165 || paid.Receipt.RefundedBytes != 125 {
		t.Fatal("truncated funding history", paid, err)
	}
	sellerOrderBalancesV1(t, f, 125, 835, 100)
}
