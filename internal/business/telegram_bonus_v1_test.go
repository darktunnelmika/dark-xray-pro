package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// These tests use explicit synthetic ledger funding to isolate quote, schema
// and native-debit behavior. The independent review suite obtains spendable
// credit through the complete referral/payment/historical-delivery path.
func tgBonusTestCredit(t *testing.T, f tgCommerceFixture, actor TelegramActor, stars int64, tag string) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = appendTelegramWalletTx(ctx, tx, "bonus-test-credit-"+tag, a.Store.ID, a.CustomerID, "bonus", "XTR", stars, "synthetic bonus test funding"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func tgBonusTestEnable(t *testing.T, f tgCommerceFixture) {
	t.Helper()
	ctx := context.Background()
	if err := f.s.EnableTelegramBonusV1(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetTelegramBonusPolicyV1(ctx, f.manager, TelegramBonusPolicyChangeV1{EventID: "bonus-test-enable", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramBonusExplicitPolicyPermissionsAndExactAmounts(t *testing.T) {
	ctx := context.Background()
	s, path := opened(t)
	if err := s.VerifyTelegramBonusV1(ctx); err != nil {
		t.Fatal("old database required an optional migration", err)
	}
	if err := s.EnableTelegramBonusV1(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("bonus enabled without commerce prerequisites", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = reopened.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'telegram_bonus_*'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("opening old database implicitly installed bonus schema", count, err)
	}
	_ = reopened.Close()

	f := tgCommerceSetup(t, true)
	tgBonusTestCredit(t, f, f.buyer, 30, "policy")
	policy, err := f.s.GetTelegramBonusPolicyV1(ctx, f.buyer)
	if err != nil || policy.StoreID != f.store.ID || policy.Enabled || policy.Revision != 0 {
		t.Fatal("absent policy was not explicitly disabled", policy, err)
	}
	if _, err = f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-test-absent-family", ProductID: f.product.ID, BonusStars: 10}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("discount implicitly enabled its schema", err)
	}
	if err = f.s.EnableTelegramBonusV1(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.s.EnableTelegramBonusV1(ctx); err != nil {
		t.Fatal("idempotent explicit enable", err)
	}
	if _, err = f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-test-disabled-policy", ProductID: f.product.ID, BonusStars: 10}); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("new store policy defaulted on", err)
	}
	finance := tgExtensionAdmin(t, f, 3102, "finance")
	if _, err = f.s.InviteTelegramAdmin(ctx, f.manager, TelegramAdminInvitationRequest{EventID: "bonus-test-elevated-admin", UserID: 3103, Permissions: []string{"manage_store", "finance"}}); !errors.Is(err, ErrInvalid) {
		t.Fatal("owner/store-manager authority became delegable", err)
	}
	for i, actor := range []TelegramActor{f.buyer, finance, {BotID: f.manager.BotID, UserID: f.manager.UserID, ChatID: -1}} {
		if _, err = f.s.SetTelegramBonusPolicyV1(ctx, actor, TelegramBonusPolicyChangeV1{EventID: fmt.Sprintf("bonus-test-denied-%d", i), Enabled: true}); err == nil {
			t.Fatal("policy lacked private current store and finance authority", actor)
		}
	}
	change := TelegramBonusPolicyChangeV1{EventID: "bonus-test-allowed-admin", Enabled: true}
	policy, err = f.s.SetTelegramBonusPolicyV1(ctx, f.manager, change)
	if err != nil || !policy.Enabled || policy.Revision != 1 {
		t.Fatal("scoped finance manager cannot opt in", policy, err)
	}
	if again, e := f.s.SetTelegramBonusPolicyV1(ctx, f.manager, change); e != nil || again != policy {
		t.Fatal("policy event did not replay exactly", again, e)
	}
	wrong := change
	wrong.Enabled = false
	if _, err = f.s.SetTelegramBonusPolicyV1(ctx, f.manager, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("policy event accepted new intent", err)
	}
	if _, err = f.s.SetTelegramBonusPolicyV1(ctx, f.manager, TelegramBonusPolicyChangeV1{EventID: "bonus-test-stale-policy", ExpectedRevision: 0, Enabled: false}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale manager overwrote current policy", err)
	}
	tgExtensionRevoke(t, f, finance)
	if _, err = f.s.SetTelegramBonusPolicyV1(ctx, finance, change); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("revoked admin replay bypassed current authority", err)
	}

	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-test-one-external-star", ProductID: f.product.ID, BonusStars: 24})
	if err != nil || order.AmountStars != 1 || order.BonusDiscountStars != 24 {
		t.Fatal("minimum real invoice did not retain exact discount", order, err)
	}
	for i, discount := range []int64{-1, 25, 26} {
		if _, err = f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: fmt.Sprintf("bonus-test-invalid-amount-%d", i), ProductID: f.product.ID, BonusStars: discount}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid or full bonus invoice accepted", discount, err)
		}
	}
	if _, err = f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-test-insufficient-exact", ProductID: f.product.ID, BonusStars: 10}); !errors.Is(err, ErrInsufficient) {
		t.Fatal("insufficient exact request was silently clamped", err)
	}
	if _, err = f.s.SetTelegramBonusPolicyV1(ctx, f.manager, TelegramBonusPolicyChangeV1{EventID: "bonus-test-disable-new-orders", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-test-disabled-new-order", ProductID: f.product.ID, BonusStars: 1}); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("disabled policy still accepted a new reservation", err)
	}
	// Disabling future offers does not silently rewrite an already reserved,
	// unexpired invoice. The stored policy revision remains the agreed proof.
	if _, err = f.s.CheckTelegramPreCheckout(ctx, f.buyer, order.ID, "XTR", 1); err != nil {
		t.Fatal("policy change rewrote original reserved quote", err)
	}
	wallet, err := f.s.TelegramWallet(ctx, f.buyer)
	if err != nil || wallet.BonusStars != 6 || wallet.ReservedBonusStars != 24 || wallet.CheckoutRedemptionEnabled {
		t.Fatal("wallet double-subtracted or enabled future redemption", wallet, err)
	}
	if err = f.s.VerifyTelegramBonusV1(ctx); err != nil {
		t.Fatal("valid policy revision history rejected", err)
	}
}

func TestTelegramBonusLegacyZeroDiscountBytesAndRestart(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	request := TelegramCheckoutRequest{EventID: "bonus-legacy-zero-order", ProductID: f.product.ID}
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, request)
	if err != nil {
		t.Fatal(err)
	}
	var intentRaw, receiptRaw []byte
	var intentHash string
	if err = f.s.db.QueryRow(`SELECT intent_json,fingerprint FROM telegram_commerce_orders WHERE order_id=?`, order.ID).Scan(&intentRaw, &intentHash); err != nil {
		t.Fatal(err)
	}
	var intent telegramCheckoutIntent
	if err = json.Unmarshal(intentRaw, &intent); err != nil {
		t.Fatal(err)
	}
	// Frozen pre-feature request fields, order and JSON tags. This independent
	// shape detects adding a default field to old checkout fingerprints.
	type legacyRequest struct {
		EventID   string `json:"event_id"`
		ProductID string `json:"product_id"`
		ClientID  string `json:"client_id"`
	}
	legacy := struct {
		Actor              TelegramActor
		Request            legacyRequest
		StoreID            string
		CustomerID         string
		Product            TelegramProduct
		ClientID           string
		ExpectedClientHash string
	}{intent.Actor, legacyRequest{request.EventID, request.ProductID, request.ClientID}, intent.StoreID, intent.CustomerID, intent.Product, intent.ClientID, intent.ExpectedClientHash}
	legacyRaw, _ := json.Marshal(legacy)
	if !bytes.Equal(legacyRaw, intentRaw) || fingerprint("telegram-checkout", legacy) != intentHash {
		t.Fatal("zero-discount quote changed the published intent representation")
	}
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TotalAmount: 25, TelegramChargeID: "bonus-legacy-charge"}); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT receipt_json FROM telegram_commerce_order_receipts WHERE order_id=?`, order.ID).Scan(&receiptRaw); err != nil || bytes.Contains(receiptRaw, []byte("bonus_discount_stars")) {
		t.Fatal("legacy receipt acquired a new default financial field", err)
	}
	tgBonusTestEnable(t, f)
	if replay, e := f.s.PrepareTelegramCheckout(ctx, f.buyer, request); e != nil || replay.ID != order.ID || !replay.Replayed || replay.BonusDiscountStars != 0 {
		t.Fatal("installing bonus invalidated legacy request replay", replay, e)
	}
	q, err := f.s.TelegramBonusQuoteV1(ctx, f.buyer, order.ID)
	if err != nil || q.State != "none" || q.NetStars != 25 || q.GrossStars != 25 || q.DiscountStars != 0 {
		t.Fatal("legacy receipt manufactured funding proof", q, err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("legacy receipt with optional bonus cannot be backed up", err)
	}
	s, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var afterIntent, afterReceipt []byte
	var afterHash string
	if err = s.db.QueryRow(`SELECT o.intent_json,o.fingerprint,r.receipt_json FROM telegram_commerce_orders o JOIN telegram_commerce_order_receipts r ON r.order_id=o.order_id WHERE o.order_id=?`, order.ID).Scan(&afterIntent, &afterHash, &afterReceipt); err != nil || !bytes.Equal(afterIntent, intentRaw) || !bytes.Equal(afterReceipt, receiptRaw) || afterHash != intentHash {
		t.Fatal("enable/restart rewrote historical money or receipt", err)
	}
	if got, e := s.FulfillTelegramOrder(ctx, f.buyer, order.ID); e != nil || !got.Replayed || got.Client != receipt.Client {
		t.Fatal("legacy fulfillment no longer safely replays", got, e)
	}
}

// Seed a valid historical unpaid reservation at an explicit past time. This
// DATA fixture never rewrites immutable rows or changes the production clock.
func tgBonusPastQuote(t *testing.T, f tgCommerceFixture, actor TelegramActor, tag string, bonus, created int64) TelegramOrder {
	t.Helper()
	ctx := context.Background()
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := telegramProductTx(ctx, tx, a.Store, f.product.ID)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := telegramBonusPolicyTx(ctx, tx, a.Store.ID)
	if err != nil || !policy.Enabled {
		t.Fatal(policy, err)
	}
	id, err := telegramNonce()
	if err != nil {
		t.Fatal(err)
	}
	o := TelegramOrder{ID: id, StoreID: a.Store.ID, CustomerID: a.CustomerID, BotID: actor.BotID, UserID: actor.UserID, ProductID: p.Product.ID, ClientID: "tg-" + id[:32], Action: "create", Currency: "XTR", AmountStars: p.PriceStars - bonus, BonusDiscountStars: bonus, Status: "quoted", Product: p.Product, CreatedUnix: created, ExpiresUnix: created + 900}
	req := TelegramCheckoutRequest{EventID: "bonus-past-" + tag, ProductID: p.Product.ID, BonusStars: bonus}
	intent := telegramCheckoutIntent{Actor: actor, Request: req, StoreID: o.StoreID, CustomerID: o.CustomerID, Product: p, ClientID: o.ClientID}
	terms, _ := json.Marshal(o.Product)
	raw, _ := json.Marshal(intent)
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_commerce_orders(order_id,event_id,store_id,customer_id,telegram_user_id,product_id,client_id,action,currency,amount_stars,status,product_json,intent_json,fingerprint,expires_unix,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, req.EventID, o.StoreID, o.CustomerID, o.UserID, o.ProductID, o.ClientID, o.Action, o.Currency, o.AmountStars, o.Status, terms, raw, fingerprint("telegram-checkout", intent), o.ExpiresUnix, created); err != nil {
		t.Fatal(err)
	}
	reserve := "tg-bonus-reserve:" + id
	if err = appendTelegramWalletTx(ctx, tx, reserve, o.StoreID, o.CustomerID, "bonus", "XTR", -bonus, telegramBonusReserveReason); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_bonus_quotes(order_id,store_id,customer_id,gross_stars,discount_stars,net_stars,policy_revision,reserve_event_id,reward_rule,created_unix,expires_unix) VALUES(?,?,?,?,?,?,?,?,'net_paid_cap_v1',?,?)`, id, o.StoreID, o.CustomerID, p.PriceStars, bonus, o.AmountStars, policy.Revision, reserve, created, o.ExpiresUnix); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestTelegramBonusExpiryScopedCleanupAndFreshCheckout(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, true)
	tgBonusTestEnable(t, f)
	tgBonusTestCredit(t, f, f.buyer, 50, "expiry-buyer")
	other := tgExtensionCustomer(t, f, 3201)
	tgBonusTestCredit(t, f, other, 30, "expiry-other")
	active, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-expiry-active", ProductID: f.product.ID, BonusStars: 10})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Unix()
	expired := tgBonusPastQuote(t, f, f.buyer, "own", 15, old)
	otherExpired := tgBonusPastQuote(t, f, other, "other", 15, old)
	if _, err = f.s.CheckTelegramPreCheckout(ctx, f.buyer, expired.ID, "XTR", expired.AmountStars); err == nil {
		t.Fatal("expired outstanding reservation accepted at precheckout")
	}
	wallet, err := f.s.TelegramWallet(ctx, f.buyer)
	if err != nil || wallet.BonusStars != 25 || wallet.ReservedBonusStars != 25 {
		t.Fatal("read-only wallet mutated historical holds", wallet, err)
	}
	if n, e := f.s.ReleaseExpiredTelegramBonusQuotesAsV1(ctx, f.buyer, 1); e != nil || n != 1 {
		t.Fatal("customer refresh did not release own expired hold", n, e)
	}
	if q, e := f.s.TelegramBonusQuoteV1(ctx, other, otherExpired.ID); e != nil || q.State != "reserved" {
		t.Fatal("customer refresh crossed customer boundary", q, e)
	}
	if q, e := f.s.TelegramBonusQuoteV1(ctx, f.buyer, active.ID); e != nil || q.State != "reserved" {
		t.Fatal("expiry release touched an unexpired invoice", q, e)
	}
	lazy := tgBonusPastQuote(t, f, f.buyer, "lazy", 24, old)
	// Only 16 stars are presently available; the new exact 20-star request
	// succeeds by releasing this customer's expired 24-star hold in its TX.
	newOrder, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-expiry-lazy-new", ProductID: f.product.ID, BonusStars: 20})
	if err != nil || newOrder.BonusDiscountStars != 20 || newOrder.AmountStars != 5 {
		t.Fatal("new quote waited for a global expiry backlog", newOrder, err)
	}
	if q, e := f.s.TelegramBonusQuoteV1(ctx, f.buyer, lazy.ID); e != nil || q.State != "released" {
		t.Fatal("lazy quote cleanup did not durably release", q, e)
	}
	if _, err = f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "bonus-expiry-disable-shop", ID: f.store.ID, BotID: f.store.BotID, RepresentativeID: f.store.RepresentativeID, Name: f.store.Name, ManagerUserID: f.store.ManagerUserID, ExpectedRevision: f.store.Revision, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReleaseExpiredTelegramBonusQuotesAsV1(ctx, f.buyer, 10); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("disabled identity obtained customer cleanup authority", err)
	}
	if n, e := f.s.ReleaseExpiredTelegramBonusQuotesV1(ctx, f.store.BotID, 10); e != nil || n != 1 {
		t.Fatal("trusted worker stranded disabled-store unpaid hold", n, e)
	}
	if n, e := f.s.ReleaseExpiredTelegramBonusQuotesV1(ctx, f.store.BotID, 10); e != nil || n != 0 {
		t.Fatal("expiry maintenance released twice", n, e)
	}
	// A valid paid event is still captured after the expiry release and shop
	// disable; it consumes the original exact discount, without current policy.
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, TelegramPayment{Payload: expired.ID, Currency: "XTR", TotalAmount: expired.AmountStars, TelegramChargeID: "bonus-expired-late-charge"}); err != nil {
		t.Fatal("late authenticated payment lost after expiry", err)
	}
	if err = f.s.VerifyTelegramBonusV1(ctx); err != nil {
		t.Fatal("valid expiry and late capture rejected", err)
	}
}

func TestTelegramBonusNativeReceiptRecoveryAndCumulativeRenewal(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, false)
	tgBonusTestEnable(t, f)
	tgBonusTestCredit(t, f, f.buyer, 80, "native-accounting")
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-native-create", ProductID: f.product.ID, BonusStars: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TotalAmount: 15, TelegramChargeID: "bonus-native-charge"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`CREATE TRIGGER test_bonus_receipt_failure BEFORE INSERT ON telegram_commerce_order_receipts BEGIN SELECT RAISE(ABORT,'synthetic receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err == nil {
		t.Fatal("native receipt failure ignored")
	}
	rep, err := f.s.Representative(ctx, f.store.RepresentativeID)
	if err != nil || rep.VolumeCreditBytes != 10000 || rep.UnlimitedCreditUnits != 100 {
		t.Fatal("failed receipt partially debited representative", rep, err)
	}
	wallet, err := f.s.TelegramWallet(ctx, f.buyer)
	if err != nil || wallet.BonusStars != 70 || wallet.ReservedBonusStars != 0 || wallet.PaidStars != 0 {
		t.Fatal("native failure undid or re-debited captured funding", wallet, err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER test_bonus_receipt_failure`); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil || receipt.Order.BonusDiscountStars != 10 || receipt.Client.DeploymentState != "pending_node_apply" {
		t.Fatal("same paid order could not safely recover", receipt, err)
	}
	if replay, e := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); e != nil || !replay.Replayed || replay.Client != receipt.Client {
		t.Fatal("recovery created a second native service", replay, e)
	}
	spec := CatalogSpec{ID: "bonus-native-unlimited", RepresentativeID: f.store.RepresentativeID, Name: "Unlimited bonus fixture", PlanKind: "unlimited", DurationDays: 15, IPCount: 3, PriceToman: 20000}
	p := tgCommercePublish(t, f.s, spec)
	if _, err = f.s.SetTelegramProductPrice(ctx, f.manager, TelegramProductPrice{EventID: "bonus-native-unlimited-price", ProductID: p.ID, PriceStars: 40}); err != nil {
		t.Fatal(err)
	}
	clientID := ""
	for i, wantUnits := range []int64{2, 3, 5, 6} {
		o, e := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: fmt.Sprintf("bonus-native-unlimited-%d", i), ProductID: p.ID, ClientID: clientID, BonusStars: 10})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.CheckTelegramPreCheckout(ctx, f.buyer, o.ID, "XTR", 30); e != nil {
			t.Fatal(e)
		}
		if _, e = f.s.RecordTelegramPayment(ctx, f.buyer, TelegramPayment{Payload: o.ID, Currency: "XTR", TotalAmount: 30, TelegramChargeID: fmt.Sprintf("bonus-native-unlimited-charge-%d", i)}); e != nil {
			t.Fatal(e)
		}
		r, e := f.s.FulfillTelegramOrder(ctx, f.buyer, o.ID)
		if e != nil || r.Client.ChargeUnlimitedUnits != wantUnits {
			t.Fatal("retail discount changed native cumulative user-month cost", r, e)
		}
		clientID = r.Client.ID
	}
	rep, err = f.s.Representative(ctx, f.store.RepresentativeID)
	if err != nil || rep.VolumeCreditBytes != 9900 || rep.UnlimitedCreditUnits != 94 {
		t.Fatal("discount was incorrectly applied to native credit", rep, err)
	}
	wallet, err = f.s.TelegramWallet(ctx, f.buyer)
	if err != nil || wallet.BonusStars != 30 || wallet.ReservedBonusStars != 0 || wallet.PaidStars != 0 {
		t.Fatal("checkout and native renewal both consumed bonus", wallet, err)
	}
	if err = f.s.VerifyTelegramCommerceData(ctx); err != nil {
		t.Fatal("discounted native create/renew receipts lost provenance", err)
	}
}

func TestTelegramBonusUnauditedCancellationFailsVerifyOpenAndBackup(t *testing.T) {
	ctx := context.Background()
	f := tgCommerceSetup(t, true)
	tgBonusTestEnable(t, f)
	tgBonusTestCredit(t, f, f.buyer, 10, "unaudited-cancel")
	o, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-unaudited-order", ProductID: f.product.ID, BonusStars: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.VerifyTelegramBonusV1(ctx); err != nil {
		t.Fatal("clean reservation invalid before fault", err)
	}
	// Deliberately inject only a compensation fact without the authenticated
	// cancellation audit. Public Cancel writes both atomically.
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := readTelegramBonusRecordTx(ctx, tx, o.ID)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = releaseTelegramBonusTx(ctx, tx, r, "cancelled", time.Now().UTC().Unix()); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = f.s.VerifyTelegramBonusV1(ctx); err == nil {
		t.Fatal("release and ledger alone invented cancellation authority")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err == nil {
		t.Fatal("offline backup accepted unaudited compensation")
	}
	if reopened, e := Open(f.path); e == nil {
		reopened.Close()
		t.Fatal("native open accepted unaudited compensation")
	}
}
