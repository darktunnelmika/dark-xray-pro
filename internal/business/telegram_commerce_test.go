package business

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type tgCommerceFixture struct {
	s              *Store
	path           string
	owner          LoginSession
	store          TelegramStore
	manager, buyer TelegramActor
	product        CatalogProduct
}

func tgCommerceSetup(t *testing.T, ownerShop bool) tgCommerceFixture {
	t.Helper()
	ctx := context.Background()
	s, path := opened(t)
	if _, err := s.BootstrapOwner(ctx, "tgowner", "synthetic-telegram-owner-password"); err != nil {
		t.Fatal(err)
	}
	owner, err := s.Login(ctx, "tgowner", "synthetic-telegram-owner-password")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableTelegramCommerce(ctx); err != nil {
		t.Fatal(err)
	}
	repID := "tg-rep-a"
	if ownerShop {
		repID = "owner"
	} else {
		newRep(t, s, repID)
		grant(t, s, repID, "tg-initial-funding-0001", 10000, 100)
		_, err = s.UpdateCatalogPolicy(ctx, CatalogPolicyChange{CatalogPolicy: CatalogPolicy{RepresentativeID: repID, ManageProducts: true, CanPublish: true, AllowVolume: true, AllowUnlimited: true, MinPriceToman: 1, MaxPriceToman: 1000000, MaxDurationDays: 365, MaxIPCount: 10, MaxVolumeBytes: 100000}, EventID: "tg-product-policy-0001", Reason: "Synthetic store permission"})
		if err != nil {
			t.Fatal(err)
		}
	}
	store, err := s.ConfigureTelegramStore(ctx, owner.Token, TelegramStoreRegistration{EventID: "tg-configure-store-0001", ID: "shop-a", BotID: 100, RepresentativeID: repID, Name: "Test shop", ManagerUserID: 1001, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	manager := TelegramActor{BotID: 100, UserID: 1001, ChatID: 1001}
	buyer := TelegramActor{BotID: 100, UserID: 2001, ChatID: 2001}
	if _, err = s.TelegramStart(ctx, manager); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TelegramStart(ctx, buyer); err != nil {
		t.Fatal(err)
	}
	spec := CatalogSpec{ID: "tg-volume-product", RepresentativeID: repID, Name: "Volume plan", PlanKind: "volume", DurationDays: 30, IPCount: 2, VolumeBytes: 100, PriceToman: 10000}
	product := tgCommercePublish(t, s, spec)
	if _, err = s.SetTelegramProductPrice(ctx, manager, TelegramProductPrice{EventID: "tg-product-stars-0001", ProductID: product.ID, PriceStars: 25}); err != nil {
		t.Fatal(err)
	}
	return tgCommerceFixture{s: s, path: path, owner: owner, store: store, manager: manager, buyer: buyer, product: product}
}

func tgCommercePublish(t *testing.T, s *Store, spec CatalogSpec) CatalogProduct {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateCatalogProduct(ctx, CatalogCreate{CatalogSpec: spec, EventID: "tg-catalog-create-" + spec.ID, Reason: "Synthetic Telegram catalog"}); err != nil {
		t.Fatal(err)
	}
	p, err := s.UpdateCatalogProduct(ctx, CatalogUpdate{CatalogSpec: spec, EventID: "tg-catalog-publish-" + spec.ID, ExpectedRevision: 1, Status: "published", Reason: "Publish synthetic offer"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func tgCommercePaidOrder(t *testing.T, f tgCommerceFixture, id, product, client string) TelegramOrder {
	t.Helper()
	ctx := context.Background()
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "tg-checkout-" + id, ProductID: product, ClientID: client})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CheckTelegramPreCheckout(ctx, f.buyer, order.ID, "XTR", order.AmountStars); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "charge-" + id, TotalAmount: order.AmountStars}); err != nil {
		t.Fatal(err)
	}
	return order
}

func TestTelegramCommerceExplicitSchemaAndNoImplicitMigration(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if err := s.EnableTelegramCommerce(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing usage accepted", err)
	}
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'telegram_commerce_*'").Scan(&count); err != nil || count != 0 {
		t.Fatal("implicit bot schema", count, err)
	}
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTelegramCommerce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTelegramCommerce(ctx); err != nil {
		t.Fatal("idempotent enable", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER telegram_commerce_payments_no_update"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTelegramCommerce(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("damaged financial schema repaired", err)
	}
}

func TestTelegramCommercePaymentsNativeAtomicityAndExactReplay(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	request := TelegramCheckoutRequest{EventID: "tg-checkout-stable-0001", ProductID: f.product.ID}
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, request)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, request)
	if err != nil || retry.ID != order.ID || !retry.Replayed {
		t.Fatal("quote replay", retry, err)
	}
	if _, err = f.s.CheckTelegramPreCheckout(ctx, f.buyer, order.ID, "XTR", order.AmountStars+1); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("changed checkout amount", err)
	}
	payment := TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "tg-charge-stable-0001", TotalAmount: order.AmountStars}
	bad := payment
	bad.Currency = "USD"
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, bad); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("foreign currency", err)
	}
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil || receipt.Client.DeploymentState != "pending_node_apply" || receipt.Order.Status != "paid_pending_provisioning" {
		t.Fatal("false activation or failed issuance", receipt, err)
	}
	replayed, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil || !replayed.Replayed || replayed.Client.UUID != receipt.Client.UUID {
		t.Fatal("fulfillment replay", replayed, err)
	}
	payRetry, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment)
	if err != nil || !payRetry.Replayed {
		t.Fatal("payment replay", payRetry, err)
	}
	rep, err := f.s.Representative(ctx, f.store.RepresentativeID)
	if err != nil || rep.VolumeCreditBytes != 9900 || rep.UnlimitedCreditUnits != 100 {
		t.Fatal("double/native debit", rep, err)
	}
	services, err := f.s.ListTelegramServices(ctx, f.buyer)
	if err != nil || len(services) != 1 || services[0].Client.ID != receipt.Client.ID {
		t.Fatal("service ownership", services, err)
	}
	if _, err = f.s.CheckTelegramPreCheckout(ctx, f.buyer, order.ID, "XTR", order.AmountStars); err == nil {
		t.Fatal("paid invoice accepted again")
	}
	if _, err = f.s.db.Exec("UPDATE telegram_commerce_payments SET amount_stars=1"); err == nil {
		t.Fatal("payment history mutable")
	}
	if _, err = f.s.db.Exec("DELETE FROM telegram_commerce_order_receipts"); err == nil {
		t.Fatal("receipt history mutable")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	replayed, err = reopened.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil || !replayed.Replayed || replayed.Client.UUID != receipt.Client.UUID {
		t.Fatal("restart duplicate issue", err)
	}
}

func TestTelegramCommerceReceiptFailureRollsBackNativeDebitAndIdentity(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	order := tgCommercePaidOrder(t, f, "rollback-0001", f.product.ID, "")
	if _, err := f.s.db.Exec("CREATE TRIGGER test_reject_telegram_receipt BEFORE INSERT ON telegram_commerce_order_receipts BEGIN SELECT RAISE(ABORT,'synthetic durable receipt failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err == nil {
		t.Fatal("receipt fault ignored")
	}
	rep, err := f.s.Representative(ctx, f.store.RepresentativeID)
	if err != nil || rep.VolumeCreditBytes != 10000 {
		t.Fatal("partial debit", rep, err)
	}
	if _, err = f.s.Client(ctx, order.ClientID); !errors.Is(err, ErrNotFound) {
		t.Fatal("orphan client", err)
	}
	var status string
	if err = f.s.db.QueryRow("SELECT status FROM telegram_commerce_orders WHERE order_id=?", order.ID).Scan(&status); err != nil || status != "paid_pending_fulfillment" {
		t.Fatal("lost payment", status, err)
	}
	if _, err = f.s.db.Exec("DROP TRIGGER test_reject_telegram_receipt"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err != nil {
		t.Fatal("safe same-order retry", err)
	}
}

func TestTelegramCommerceCustomerBotTenantAndBrowserScope(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	newRep(t, f.s, "tg-rep-b")
	other, err := f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "tg-configure-other-0001", ID: "shop-b", BotID: 101, RepresentativeID: "tg-rep-b", Name: "Other shop", ManagerUserID: 1002, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	otherBuyer := f.buyer
	otherBuyer.BotID = other.BotID
	one, err := f.s.ResolveTelegramActor(ctx, f.buyer)
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.s.TelegramStart(ctx, otherBuyer)
	if err != nil || one.CustomerID == two.CustomerID {
		t.Fatal("cross-store customer merged", one, two, err)
	}
	if _, err = f.s.PrepareTelegramCheckout(ctx, otherBuyer, TelegramCheckoutRequest{EventID: "tg-cross-product-0001", ProductID: f.product.ID}); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("cross product", err)
	}
	order := tgCommercePaidOrder(t, f, "tenant-0001", f.product.ID, "")
	if _, err = f.s.RecordTelegramPayment(ctx, otherBuyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "cross-charge", TotalAmount: order.AmountStars}); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("cross-bot payment", err)
	}
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.FulfillTelegramOrder(ctx, otherBuyer, order.ID); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("cross order", err)
	}
	wrongUser := f.buyer
	wrongUser.UserID++
	wrongUser.ChatID++
	if _, err = f.s.TelegramStart(ctx, wrongUser); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PrepareTelegramCheckout(ctx, wrongUser, TelegramCheckoutRequest{EventID: "tg-steal-client-0001", ProductID: f.product.ID, ClientID: receipt.Client.ID}); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("cross customer renewal", err)
	}
	if _, err = f.s.ProvisionRepresentativeLogin(ctx, f.store.RepresentativeID, "tgmanager", "synthetic-representative-password"); err != nil {
		t.Fatal(err)
	}
	login, err := f.s.Login(ctx, "tgmanager", "synthetic-representative-password")
	if err != nil {
		t.Fatal(err)
	}
	stores, err := f.s.ListTelegramStoresAs(ctx, login.Token)
	if err != nil || len(stores) != 1 || stores[0].ID != f.store.ID {
		t.Fatal("browser representative leak", stores, err)
	}
	_, err = f.s.ConfigureTelegramStore(ctx, login.Token, TelegramStoreRegistration{EventID: "tg-rep-self-config-0001", ID: "self-other", BotID: 1000, RepresentativeID: f.store.RepresentativeID, Name: "No", ManagerUserID: 1001, Enabled: true})
	if !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("rep registered arbitrary bot", err)
	}
}

func TestTelegramCommerceOwnerStoreUsesNativeOwnerCatalog(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, true)
	access, err := f.s.ResolveTelegramActor(ctx, f.manager)
	if err != nil || access.Role != "owner" {
		t.Fatal(access, err)
	}
	order := tgCommercePaidOrder(t, f, "owner-shop-0001", f.product.ID, "")
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil || receipt.Client.RepresentativeID != "owner" {
		t.Fatal("owner retail missing", receipt, err)
	}
	owner, err := f.s.Representative(ctx, "owner")
	if err != nil || owner.VolumeCreditBytes != 0 || owner.UnlimitedCreditUnits != 0 {
		t.Fatal("invented owner credit", owner, err)
	}
}

func TestTelegramCommercePaymentSurvivesDisabledOrUnderfundedShop(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "tg-late-payment-order-0001", ProductID: f.product.ID})
	if err != nil {
		t.Fatal(err)
	}
	reg := TelegramStoreRegistration{EventID: "tg-store-disable-0001", ID: f.store.ID, BotID: f.store.BotID, RepresentativeID: f.store.RepresentativeID, Name: f.store.Name, ManagerUserID: f.store.ManagerUserID, ExpectedRevision: 1, Enabled: false}
	if _, err = f.s.ConfigureTelegramStore(ctx, f.owner.Token, reg); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "late-paid-charge", TotalAmount: order.AmountStars}); err != nil {
		t.Fatal("paid fact lost after revoke", err)
	}
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("disabled fulfillment", err)
	}
	reg.Enabled = true
	reg.ExpectedRevision = 2
	reg.EventID = "tg-store-enable-0001"
	if _, err = f.s.ConfigureTelegramStore(ctx, f.owner.Token, reg); err != nil {
		t.Fatal(err)
	}
	grant(t, f.s, f.store.RepresentativeID, "tg-remove-free-credit-0001", -10000, 0)
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); !errors.Is(err, ErrInsufficient) {
		t.Fatal("unfunded issue", err)
	}
	grant(t, f.s, f.store.RepresentativeID, "tg-replenish-credit-0001", 1000, 0)
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err != nil {
		t.Fatal("paid order recovery", err)
	}
	rep, _ := f.s.Representative(ctx, f.store.RepresentativeID)
	if rep.VolumeCreditBytes != 900 {
		t.Fatal(rep)
	}
}

func TestTelegramCommerceAdminInvitationBoundAcceptanceRevocation(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	admin := TelegramActor{BotID: f.store.BotID, UserID: 3001, ChatID: 3001}
	if _, err := f.s.TelegramStart(ctx, admin); err != nil {
		t.Fatal(err)
	}
	invite, err := f.s.InviteTelegramAdmin(ctx, f.manager, TelegramAdminInvitationRequest{EventID: "tg-admin-invite-0001", UserID: admin.UserID, Permissions: []string{"support", "broadcast_draft"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptTelegramAdmin(ctx, f.buyer, invite.ID); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("stolen invite accepted", err)
	}
	access, err := f.s.AcceptTelegramAdmin(ctx, admin, invite.ID)
	if err != nil || access.Role != "admin" || !telegramHasPermission(access.Permissions, "support") || telegramHasPermission(access.Permissions, "finance") {
		t.Fatal(access, err)
	}
	if _, err = f.s.InviteTelegramAdmin(ctx, admin, TelegramAdminInvitationRequest{EventID: "tg-admin-elevate-0001", UserID: 4001, Permissions: []string{"finance"}}); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("admin elevated role", err)
	}
	_, err = f.s.UpdateTelegramAdmin(ctx, f.manager, TelegramAdminChange{EventID: "tg-admin-revoke-0001", UserID: admin.UserID, ExpectedRevision: 1, Enabled: false, Permissions: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptTelegramAdmin(ctx, admin, invite.ID); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("accepted invitation resurrected role", err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = authorizeTelegramTx(ctx, tx, admin, "support"); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("queued work used revoked grant", err)
	}
	tx.Rollback()
}

func TestTelegramCommerceUnlimitedRenewalKeepsCumulativeNativeUnits(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	spec := CatalogSpec{ID: "tg-unlimited-product", RepresentativeID: f.store.RepresentativeID, Name: "Unlimited", PlanKind: "unlimited", DurationDays: 15, IPCount: 3, PriceToman: 20000}
	product := tgCommercePublish(t, f.s, spec)
	if _, err := f.s.SetTelegramProductPrice(ctx, f.manager, TelegramProductPrice{EventID: "tg-unlimited-price-0001", ProductID: product.ID, PriceStars: 40}); err != nil {
		t.Fatal(err)
	}
	clientID := ""
	for i, want := range []int64{2, 3, 5, 6} {
		order := tgCommercePaidOrder(t, f, fmt.Sprintf("unlimited-%04d", i), product.ID, clientID)
		receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
		if err != nil || receipt.Client.ChargeUnlimitedUnits != want {
			t.Fatal(i, receipt, err)
		}
		clientID = receipt.Client.ID
	}
	rep, _ := f.s.Representative(ctx, f.store.RepresentativeID)
	if rep.UnlimitedCreditUnits != 94 || rep.VolumeCreditBytes != 10000 {
		t.Fatal("renewal re-rounded each step", rep)
	}
}

func TestTelegramCommerceConcurrentFulfillmentChargesOnce(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	order := tgCommercePaidOrder(t, f, "concurrent-0001", f.product.ID, "")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	rep, _ := f.s.Representative(ctx, f.store.RepresentativeID)
	if rep.VolumeCreditBytes != 9900 {
		t.Fatal("concurrent duplicate charge", rep)
	}
}

func TestTelegramCommerceWalletBonusDebtPaidIsolationAndRefundFact(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	access, err := f.s.ResolveTelegramActor(ctx, f.buyer)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		event, bucket string
		delta         int64
	}{{"tg-wallet-paid-0001", "paid", 30}, {"tg-wallet-bonus-0001", "bonus", 10}, {"tg-wallet-spent-0001", "bonus", -8}} {
		if err = appendTelegramWalletTx(ctx, tx, v.event, f.store.ID, access.CustomerID, v.bucket, "XTR", v.delta, "Synthetic wallet event"); err != nil {
			t.Fatal(err)
		}
	}
	if err = appendTelegramBonusReversalTx(ctx, tx, "tg-bonus-reverse-0001", f.store.ID, access.CustomerID, 10, "Verified synthetic refund"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wallet, err := f.s.TelegramWallet(ctx, f.buyer)
	if err != nil || wallet.PaidStars != 30 || wallet.BonusStars != -8 || wallet.AvailableBonusStars != 0 {
		t.Fatal("bonus clawback took paid money", wallet, err)
	}
	order := tgCommercePaidOrder(t, f, "refunded-0001", f.product.ID, "")
	refund := TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "charge-refunded-0001", TotalAmount: order.AmountStars}
	result, err := f.s.RecordTelegramRefund(ctx, f.buyer, refund)
	if err != nil || result.Status != "refunded" {
		t.Fatal(result, err)
	}
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("refunded order issued", err)
	}
	result, err = f.s.RecordTelegramRefund(ctx, f.buyer, refund)
	if err != nil || !result.Replayed {
		t.Fatal("refund replay", result, err)
	}
	if _, err = f.s.db.Exec("DELETE FROM telegram_commerce_payment_refunds"); err == nil {
		t.Fatal("refund history mutable")
	}
}

func TestTelegramCommerceOwnerActionsUseSharedNativeLedger(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, true)
	request := TelegramRepresentativeCreate{EventID: "tg-owner-new-rep-0001", ID: "from-bot", Name: "Created in owner bot"}
	rep, err := f.s.CreateTelegramRepresentative(ctx, f.manager, request)
	if err != nil || rep.ID != request.ID {
		t.Fatal(rep, err)
	}
	if _, err = f.s.CreateTelegramRepresentative(ctx, f.buyer, TelegramRepresentativeCreate{EventID: "tg-user-new-rep-0001", ID: "forbidden", Name: "Forbidden"}); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("customer created representative", err)
	}
	credit := CreditChange{EventID: "tg-owner-credit-0001", RepresentativeID: rep.ID, VolumeDeltaBytes: 500, UnlimitedDeltaUnits: 3, Reason: "Owner synthetic grant"}
	result, err := f.s.AdjustTelegramRepresentativeCredits(ctx, f.manager, credit)
	if err != nil || result.VolumeCreditBytes != 500 || result.UnlimitedCreditUnits != 3 {
		t.Fatal(result, err)
	}
	result, err = f.s.AdjustTelegramRepresentativeCredits(ctx, f.manager, credit)
	if err != nil || result.VolumeCreditBytes != 500 {
		t.Fatal("duplicate credit", result, err)
	}
	conflict := credit
	conflict.VolumeDeltaBytes = 501
	if _, err = f.s.AdjustTelegramRepresentativeCredits(ctx, f.manager, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("reused credit intent changed", err)
	}
	if _, err = f.s.db.Exec("CREATE TRIGGER test_tg_owner_receipt_failure BEFORE INSERT ON telegram_commerce_events WHEN NEW.action='owner_adjust_credits' BEGIN SELECT RAISE(ABORT,'test owner audit fault'); END"); err != nil {
		t.Fatal(err)
	}
	credit.EventID = "tg-owner-failed-credit-0001"
	if _, err = f.s.AdjustTelegramRepresentativeCredits(ctx, f.manager, credit); err == nil {
		t.Fatal("missing owner audit permitted credit")
	}
	current, _ := f.s.Representative(ctx, rep.ID)
	if current.VolumeCreditBytes != 500 || current.UnlimitedCreditUnits != 3 {
		t.Fatal("partial owner credit", current)
	}
	if _, err = f.s.db.Exec("DROP TRIGGER test_tg_owner_receipt_failure"); err != nil {
		t.Fatal(err)
	}
	if err = f.s.VerifyTelegramCommerceData(ctx); err != nil {
		t.Fatal("valid owner snapshot", err)
	}
}

func TestTelegramCommerceTrialPolicyOnceAndNativeAtomicity(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	policy, err := f.s.GetTelegramTrialPolicy(ctx, f.buyer)
	if err != nil || policy.Enabled || policy.Revision != 0 || policy.DurationHours != 24 || policy.StartsAt != "issuance" {
		t.Fatal(policy, err)
	}
	if _, err = f.s.ClaimTelegramTrial(ctx, f.buyer, "tg-trial-disabled-0001"); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("default enabled trial", err)
	}
	policy, err = f.s.SetTelegramTrialPolicy(ctx, f.manager, TelegramTrialPolicyChange{EventID: "tg-trial-policy-0001", Enabled: true, VolumeBytes: 200})
	if err != nil {
		t.Fatal(err)
	}
	trial, err := f.s.ClaimTelegramTrial(ctx, f.buyer, "tg-trial-claim-0001")
	if err != nil || trial.Client.DurationDays != 1 || trial.Client.ExpiresUnix-trial.Client.CreatedUnix != 86400 || trial.Client.IPCount != 1 || trial.State != "pending_node_apply" {
		t.Fatal("untrue trial terms", trial, err)
	}
	repeated, err := f.s.ClaimTelegramTrial(ctx, f.buyer, "tg-trial-claim-0002")
	if err != nil || repeated.Client.ID != trial.Client.ID {
		t.Fatal("second test identity", repeated, err)
	}
	rep, _ := f.s.Representative(ctx, f.store.RepresentativeID)
	if rep.VolumeCreditBytes != 9800 {
		t.Fatal("trial not native or double charged", rep)
	}
	other := f.buyer
	other.UserID++
	other.ChatID++
	if _, err = f.s.TelegramStart(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec("CREATE TRIGGER test_tg_trial_receipt_failure BEFORE INSERT ON telegram_commerce_trials BEGIN SELECT RAISE(ABORT,'test trial receipt fault'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ClaimTelegramTrial(ctx, other, "tg-trial-failed-0001"); err == nil {
		t.Fatal("missing trial receipt accepted")
	}
	rep, _ = f.s.Representative(ctx, f.store.RepresentativeID)
	if rep.VolumeCreditBytes != 9800 {
		t.Fatal("trial partial debit", rep)
	}
	if _, err = f.s.db.Exec("DROP TRIGGER test_tg_trial_receipt_failure"); err != nil {
		t.Fatal(err)
	}
	if err = f.s.VerifyTelegramCommerceData(ctx); err != nil {
		t.Fatal("trial omitted from snapshot proof", err)
	}
}

func TestTelegramCommerceOfflineDataValidationChecksMoneyAndNativeReceipts(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	order := tgCommercePaidOrder(t, f, "validated-0001", f.product.ID, "")
	if _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.VerifyTelegramCommerceData(ctx); err != nil {
		t.Fatal("valid finance rejected", err)
	}
	if _, err := f.s.db.Exec("UPDATE telegram_commerce_orders SET status='paid_pending_fulfillment' WHERE order_id=?", order.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.VerifyTelegramCommerceData(ctx); err == nil {
		t.Fatal("inconsistent paid/fulfilled state accepted")
	}
}

func TestTelegramCommerceUnpricedProductsManagementOnly(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	p := tgCommercePublish(t, f.s, CatalogSpec{ID: "tg-unpriced-new", RepresentativeID: f.store.RepresentativeID, Name: "New unpriced product", PlanKind: "volume", DurationDays: 30, IPCount: 2, VolumeBytes: 100, PriceToman: 10000})
	public, err := f.s.ListTelegramProducts(ctx, f.buyer)
	if err != nil || len(public) != 1 {
		t.Fatal("unpriced public product", public, err)
	}
	managed, err := f.s.ListTelegramManagedProducts(ctx, f.manager)
	if err != nil || len(managed) != 2 {
		t.Fatal("cannot configure first Stars price", managed, err)
	}
	var found bool
	for _, item := range managed {
		if item.Product.ID == p.ID {
			found = true
			if item.PriceStars != 0 || item.PriceRevision != 0 {
				t.Fatal(item)
			}
		}
	}
	if !found {
		t.Fatal("unconfigured product absent")
	}
	if _, err = f.s.ListTelegramManagedProducts(ctx, f.buyer); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("customer read management products", err)
	}
	if _, err = f.s.SetTelegramProductPrice(ctx, f.manager, TelegramProductPrice{EventID: "tg-new-price-bootstrap-0001", ProductID: p.ID, PriceStars: 10}); err != nil {
		t.Fatal(err)
	}
	public, err = f.s.ListTelegramProducts(ctx, f.buyer)
	if err != nil || len(public) != 2 {
		t.Fatal(public, err)
	}
}

func TestTelegramCommerceOrderRecoveryReadScopesAndRetry(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	paid := tgCommercePaidOrder(t, f, "recover-order-0001", f.product.ID, "")
	if _, err := f.s.AdjustCredits(ctx, CreditChange{EventID: "tg-recover-drain-0001", RepresentativeID: f.store.RepresentativeID, VolumeDeltaBytes: -10000, Reason: "Synthetic insufficient native quota"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, paid.ID); !errors.Is(err, ErrInsufficient) {
		t.Fatal("underfunded issuance", err)
	}
	orders, err := f.s.ListTelegramOrders(ctx, f.buyer, 20, 0)
	if err != nil || len(orders) != 1 || orders[0].ID != paid.ID || orders[0].Status != "paid_pending_fulfillment" {
		t.Fatal("captured payment has no recovery view", orders, err)
	}
	if got, e := f.s.TelegramOrderByID(ctx, f.buyer, paid.ID); e != nil || got != orders[0] {
		t.Fatal("order detail mismatch", got, e)
	}
	other := TelegramActor{BotID: f.store.BotID, UserID: 2002, ChatID: 2002}
	if _, err = f.s.TelegramStart(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "tg-recover-second-store-0001", ID: "shop-recovery-b", BotID: 101, RepresentativeID: f.store.RepresentativeID, Name: "Separate brand", ManagerUserID: 1002, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	otherBot := f.buyer
	otherBot.BotID = 101
	if _, err = f.s.TelegramStart(ctx, otherBot); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []TelegramActor{other, otherBot, f.manager} {
		if got, e := f.s.ListTelegramOrders(ctx, actor, 20, 0); e != nil || len(got) != 0 {
			t.Fatal("cross-customer order listing", actor, got, e)
		}
		if _, e := f.s.TelegramOrderByID(ctx, actor, paid.ID); !errors.Is(e, ErrNotFound) {
			t.Fatal("opaque payload granted foreign order read", actor, e)
		}
	}
	quote, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "tg-recovery-pagination-0001", ProductID: f.product.ID})
	if err != nil {
		t.Fatal(err)
	}
	page1, err := f.s.ListTelegramOrders(ctx, f.buyer, 1, 0)
	page2, err2 := f.s.ListTelegramOrders(ctx, f.buyer, 1, 1)
	if err != nil || err2 != nil || len(page1) != 1 || len(page2) != 1 || page1[0].ID == page2[0].ID || page1[0].ID != paid.ID && page1[0].ID != quote.ID || page2[0].ID != paid.ID && page2[0].ID != quote.ID {
		t.Fatal("unstable bounded order pages", page1, page2, err, err2)
	}
	grant(t, f.s, f.store.RepresentativeID, "tg-recover-fund-0001", 100, 0)
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, paid.ID)
	if err != nil {
		t.Fatal("paid order cannot recover", err)
	}
	if replay, e := f.s.FulfillTelegramOrder(ctx, f.buyer, paid.ID); e != nil || !replay.Replayed || replay.Client.ID != receipt.Client.ID {
		t.Fatal("recovery replay changed native issuance", replay, e)
	}
	if got, e := f.s.TelegramOrderByID(ctx, f.buyer, paid.ID); e != nil || got.Status != "paid_pending_provisioning" {
		t.Fatal("fulfillment claimed runtime delivery", got, e)
	}
	if rep, e := f.s.Representative(ctx, f.store.RepresentativeID); e != nil || rep.VolumeCreditBytes != 0 {
		t.Fatal("recovery duplicated native debit", rep, e)
	}
	if _, err = f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "tg-recover-disable-0001", ID: f.store.ID, BotID: f.store.BotID, RepresentativeID: f.store.RepresentativeID, Name: f.store.Name, ManagerUserID: f.store.ManagerUserID, ExpectedRevision: 1, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ListTelegramOrders(ctx, f.buyer, 20, 0); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("revoked authority listed orders", err)
	}
	if _, err = f.s.TelegramOrderByID(ctx, f.buyer, paid.ID); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("revoked authority read order", err)
	}
}

func TestTelegramCommerceRefundWaitsForEarlierPaymentCapture(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "tg-refund-before-capture-0001", ProductID: f.product.ID})
	if err != nil {
		t.Fatal(err)
	}
	refund := TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "late-capture-refund-0001", TotalAmount: order.AmountStars}
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, refund); !errors.Is(err, ErrUnavailable) {
		t.Fatal("refund before payment capture became permanent mismatch", err)
	}
	if got, e := f.s.TelegramOrderByID(ctx, f.buyer, order.ID); e != nil || got.Status != "quoted" {
		t.Fatal("unmatched refund mutated invoice", got, e)
	}
	wrongAmount := refund
	wrongAmount.TotalAmount++
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, wrongAmount); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("actual invoice mismatch became retryable", err)
	}
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, refund); err != nil {
		t.Fatal("late successful payment cannot be captured", err)
	}
	wrongCharge := refund
	wrongCharge.TelegramChargeID = "different-refund-charge"
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, wrongCharge); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("different charge for known paid order became retryable", err)
	}
	got, err := f.s.RecordTelegramRefund(ctx, f.buyer, refund)
	if err != nil || got.Status != "refunded" || got.Replayed {
		t.Fatal("retained refund failed after late payment capture", got, err)
	}
	if replay, e := f.s.RecordTelegramRefund(ctx, f.buyer, refund); e != nil || !replay.Replayed || replay.ID != got.ID {
		t.Fatal("refund retry duplicated financial fact", replay, e)
	}
	if rep, e := f.s.Representative(ctx, f.store.RepresentativeID); e != nil || rep.VolumeCreditBytes != 10000 {
		t.Fatal("refund changed native quota without fulfillment", rep, e)
	}
	if err = f.s.VerifyTelegramCommerceData(ctx); err != nil {
		t.Fatal("reordered capture corrupted immutable accounting", err)
	}
}
