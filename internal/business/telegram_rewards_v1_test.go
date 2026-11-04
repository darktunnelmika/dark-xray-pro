package business

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func tgRewardPolicy(t *testing.T, f tgCommerceFixture, p TelegramReferralPolicyV1, revision int64, tag string) TelegramReferralPolicyV1 {
	t.Helper()
	out, err := f.s.TelegramSetReferralPolicyV1(context.Background(), f.manager, TelegramReferralPolicyChangeV1{EventID: "reward-policy-" + tag, ExpectedRevision: revision, Policy: p})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func tgRewardFixedPolicy() TelegramReferralPolicyV1 {
	return TelegramReferralPolicyV1{Enabled: true, Mode: "fixed_stars", FirstPurchase: 20, RepeatPurchase: 5, MaxPerOrderStars: 20, PerReferrerCapStars: 100, AttributionDays: 30}
}

func tgRewardAttribute(t *testing.T, f tgCommerceFixture, referrer TelegramActor) TelegramReferralSummaryV1 {
	t.Helper()
	link, err := f.s.TelegramReferralV1(context.Background(), referrer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.TelegramAttributeReferralV1(context.Background(), f.buyer, link.StartPayload); err != nil {
		t.Fatal(err)
	}
	return link
}

// This is an explicit historical proof DATA fixture for financial tests. It
// makes no network send or Xray-readiness claim. Production handoff proof can
// only be written by TelegramConfirmServiceDelivery after native validation
// and a definite transport message receipt; that writer has separate tests.
func tgRewardHistoricalDeliveryFixture(t *testing.T, f tgCommerceFixture, r TelegramOrderReceipt, terms string) {
	t.Helper()
	if terms == "" {
		terms = telegramClientTerms(r.Client)
	}
	_, err := f.s.db.Exec(`INSERT INTO telegram_commerce_order_deliveries(order_id,store_id,customer_id,client_id,content_sha256,client_terms_sha256,telegram_message_id,generation,valid_until_unix_nano,delivered_unix) VALUES(?,?,?,?,?,?,?,?,?,?)`, r.Order.ID, r.Order.StoreID, r.Order.CustomerID, r.Client.ID, strings.Repeat("a", 64), terms, 100, 1, time.Now().UTC().Add(time.Minute).UnixNano(), time.Now().UTC().Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func TestTelegramRewardsDefaultOffScopedAttributionAndFrozenTerms(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	link, err := f.s.TelegramReferralV1(ctx, referrer)
	if err != nil || link.Policy.Enabled || link.Policy.FirstPurchase != 0 || link.Policy.RepeatPurchase != 0 {
		t.Fatal("invented reward policy", link, err)
	}
	if _, err = f.s.TelegramAttributeReferralV1(ctx, f.buyer, link.Code); !errors.Is(err, ErrConflict) {
		t.Fatal("disabled policy attributed", err)
	}
	var beforeCodes, afterCodes int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_rewards_codes`).Scan(&beforeCodes); err != nil {
		t.Fatal(err)
	}
	if current, e := f.s.GetTelegramReferralPolicyV1(ctx, f.manager); e != nil || current.Enabled {
		t.Fatal("read-only policy settings", current, e)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_rewards_codes`).Scan(&afterCodes); err != nil || afterCodes != beforeCodes {
		t.Fatal("policy read created referral link", afterCodes, err)
	}
	if _, e := f.s.GetTelegramReferralPolicyV1(ctx, referrer); e == nil {
		t.Fatal("customer opened manager settings")
	}
	policy := tgRewardFixedPolicy()
	finance := tgExtensionAdmin(t, f, 3004, "finance")
	if _, err = f.s.TelegramSetReferralPolicyV1(ctx, finance, TelegramReferralPolicyChangeV1{EventID: "reward-policy-no-store-rights", Policy: policy}); err == nil {
		t.Fatal("finance role changed promotion settings")
	}
	invalid := policy
	invalid.Mode = "percent_bps"
	invalid.FirstPurchase = 10001
	if _, err = f.s.TelegramSetReferralPolicyV1(ctx, f.manager, TelegramReferralPolicyChangeV1{EventID: "reward-policy-invalid-rate", Policy: invalid}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid reward rate", err)
	}
	active := tgRewardPolicy(t, f, policy, 0, "attribution-0001")
	if _, err = f.s.TelegramAttributeReferralV1(ctx, referrer, link.Code); !errors.Is(err, ErrConflict) {
		t.Fatal("self referral", err)
	}
	other := tgExtensionOtherStore(t, f, f.buyer.UserID)
	if _, err = f.s.TelegramAttributeReferralV1(ctx, other, link.Code); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-bot attribution", err)
	}
	attr, err := f.s.TelegramAttributeReferralV1(ctx, f.buyer, link.Code)
	if err != nil {
		t.Fatal(err)
	}
	if attr.Policy.FirstPurchase != 20 {
		t.Fatal(attr)
	}
	if again, err := f.s.TelegramAttributeReferralV1(ctx, f.buyer, link.StartPayload); err != nil || again.CreatedUnix != attr.CreatedUnix {
		t.Fatal("attribution retry", again, err)
	}
	childLink, err := f.s.TelegramReferralV1(ctx, f.buyer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.TelegramAttributeReferralV1(ctx, referrer, childLink.Code); !errors.Is(err, ErrConflict) {
		t.Fatal("referral cycle", err)
	}
	changed := policy
	changed.FirstPurchase = 1
	tgRewardPolicy(t, f, changed, active.Revision, "attribution-change-0001")
	again, err := f.s.TelegramAttributeReferralV1(ctx, f.buyer, link.Code)
	if err != nil || again.Policy.FirstPurchase != 20 {
		t.Fatal("accepted terms silently changed", again, err)
	}
	late := f
	late.buyer = tgExtensionCustomer(t, f, 2009)
	tgCommercePaidOrder(t, late, "referral-too-late-0001", f.product.ID, "")
	if _, err = f.s.TelegramAttributeReferralV1(ctx, late.buyer, link.Code); !errors.Is(err, ErrConflict) {
		t.Fatal("paid customer retroactively attributed", err)
	}
}

func TestTelegramRewardsNeedFulfillmentDeliveryAndSingleAtomicGrant(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "proof-gates-0001")
	tgRewardAttribute(t, f, referrer)
	order := tgCommercePaidOrder(t, f, "reward-proof-0001", f.product.ID, "")
	if _, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, order.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("payment alone earned reward", err)
	}
	r, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	reward, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, order.ID)
	if err != nil || reward.State != "pending_delivery" || reward.AmountStars != 20 {
		t.Fatal("false delivered reward", reward, err)
	}
	wallet, err := f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.BonusStars != 0 {
		t.Fatal("provisional credit spendable", wallet, err)
	}
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	wallet, err = f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.BonusStars != 0 {
		t.Fatal("sweep bypassed delivery", wallet, err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, r, "")
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10) }()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	wallet, err = f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.BonusStars != 20 || wallet.PaidStars != 0 {
		t.Fatal("duplicate shared-ledger grant", wallet, err)
	}
	var grants, releases, entries int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_rewards_grants`).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_rewards_releases`).Scan(&releases); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if grants != 1 || releases != 1 || entries != 1 {
		t.Fatal("duplicate durable identities", grants, releases, entries)
	}
	stranger := tgExtensionCustomer(t, f, 4002)
	if _, err = f.s.TelegramSettleReferralRewardV1(ctx, stranger, order.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("other customer sees referral", err)
	}
	history, err := f.s.TelegramRewardHistoryV1(ctx, stranger, 20, 0)
	if err != nil || len(history) != 0 {
		t.Fatal("reward history isolation", history, err)
	}
	if _, err = f.s.db.Exec(`UPDATE telegram_rewards_grants SET amount_stars=999`); err == nil {
		t.Fatal("immutable reward changed")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	wallet, err = reopened.TelegramWallet(ctx, referrer)
	if err != nil || wallet.BonusStars != 20 {
		t.Fatal("restart duplicate credit", wallet, err)
	}
}

func TestTelegramRewardsVerifiedRefundAtomicityAndBonusDebt(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "refund-0001")
	tgRewardAttribute(t, f, referrer)
	order := tgCommercePaidOrder(t, f, "reward-refund-0001", f.product.ID, "")
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, receipt, "")
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	access, err := f.s.ResolveTelegramActor(ctx, referrer)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = appendTelegramWalletTx(ctx, tx, "reward-paid-fixture-0001", f.store.ID, access.CustomerID, "paid", "XTR", 7, "synthetic paid-bucket independence"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = appendTelegramWalletTx(ctx, tx, "reward-spend-fixture-0001", f.store.ID, access.CustomerID, "bonus", "XTR", -15, "synthetic prior bonus consumption"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	refund := TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "charge-reward-refund-0001", TotalAmount: order.AmountStars}
	wrong := refund
	wrong.TelegramChargeID = "not-the-payment"
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, wrong); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("unverified refund accepted", err)
	}
	if _, err = f.s.db.Exec(`CREATE TRIGGER test_reject_reward_reversal BEFORE INSERT ON telegram_rewards_reversals BEGIN SELECT RAISE(ABORT,'synthetic compensation fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, refund); err == nil {
		t.Fatal("refund ignored compensation failure")
	}
	var refunds int
	var status string
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_commerce_payment_refunds`).Scan(&refunds); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT status FROM telegram_commerce_orders WHERE order_id=?`, order.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	wallet, err := f.s.TelegramWallet(ctx, referrer)
	if err != nil || refunds != 0 || status != "paid_pending_provisioning" || wallet.BonusStars != 5 {
		t.Fatal("partial refund transaction", refunds, status, wallet, err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER test_reject_reward_reversal`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, refund); err != nil {
		t.Fatal(err)
	}
	if replay, err := f.s.RecordTelegramRefund(ctx, f.buyer, refund); err != nil || !replay.Replayed {
		t.Fatal("refund replay", replay, err)
	}
	wallet, err = f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.PaidStars != 7 || wallet.BonusStars != -15 || wallet.AvailableBonusStars != 0 {
		t.Fatal("paid funds changed or bonus debt erased", wallet, err)
	}
	// A later repeat earning offsets existing debt; it need not cover all debt.
	next := tgCommercePaidOrder(t, f, "reward-after-refund-0002", f.product.ID, "")
	nextReceipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, nextReceipt, "")
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	wallet, err = f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.PaidStars != 7 || wallet.BonusStars != -10 || wallet.AvailableBonusStars != 0 {
		t.Fatal("later bonus failed to offset debt", wallet, err)
	}
	summary, err := f.s.TelegramReferralV1(ctx, referrer)
	if err != nil || summary.ReleasedStars != 5 || summary.ReversedStars != 20 {
		t.Fatal("refund stats", summary, err)
	}
}

func TestTelegramRewardsPendingRefundAndDisabledStoreReconciliation(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "pending-refund-0001")
	tgRewardAttribute(t, f, referrer)
	order := tgCommercePaidOrder(t, f, "reward-pending-refund-0001", f.product.ID, "")
	if _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, order.ID); err != nil {
		t.Fatal(err)
	}
	_, err := f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "reward-disable-store-0001", ID: f.store.ID, BotID: f.store.BotID, RepresentativeID: f.store.RepresentativeID, Name: f.store.Name, ManagerUserID: f.manager.UserID, ExpectedRevision: f.store.Revision, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("disabled store released reward", err)
	}
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "charge-reward-pending-refund-0001", TotalAmount: order.AmountStars}); err != nil {
		t.Fatal("disabled store prevented verified financial compensation", err)
	}
	var reversals, entries int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_rewards_reversals WHERE released=0`).Scan(&reversals); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM telegram_commerce_wallet_ledger`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if reversals != 1 || entries != 0 {
		t.Fatal("provisional refund moved actual wallet", reversals, entries)
	}
}

func TestTelegramRewardsPaymentCaptureOrderCapAndHold(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	policy := tgRewardFixedPolicy()
	policy.PerReferrerCapStars = 23
	policy.HoldHours = 1
	tgRewardPolicy(t, f, policy, 0, "cap-hold-0001")
	tgRewardAttribute(t, f, referrer)
	first := tgCommercePaidOrder(t, f, "reward-first-0001", f.product.ID, "")
	second := tgCommercePaidOrder(t, f, "reward-second-0002", f.product.ID, "")
	for _, o := range []TelegramOrder{first, second} {
		r, e := f.s.FulfillTelegramOrder(ctx, f.buyer, o.ID)
		if e != nil {
			t.Fatal(e)
		}
		tgRewardHistoricalDeliveryFixture(t, f, r, "")
	}
	if err := f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	one, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, first.ID)
	if err != nil || one.AmountStars != 20 || one.State != "holding" {
		t.Fatal("first capture not first purchase", one, err)
	}
	two, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, second.ID)
	if err != nil || two.AmountStars != 3 || two.State != "holding" {
		t.Fatal("cap not atomic", two, err)
	}
	wallet, err := f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.BonusStars != 0 {
		t.Fatal("hold bypassed", wallet, err)
	}
	third := tgCommercePaidOrder(t, f, "reward-third-0003", f.product.ID, "")
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, third.ID); err != nil {
		t.Fatal(err)
	}
	zero, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, third.ID)
	if err != nil || zero.AmountStars != 0 || zero.State != "ineligible" {
		t.Fatal("capped grant issued", zero, err)
	}
}

func TestTelegramRewardsOldDeliveryTermsCannotQualifyRenewal(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "renewal-proof-0001")
	tgRewardAttribute(t, f, referrer)
	initial := tgCommercePaidOrder(t, f, "reward-initial-0001", f.product.ID, "")
	old, err := f.s.FulfillTelegramOrder(ctx, f.buyer, initial.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, old, "")
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	renewal := tgCommercePaidOrder(t, f, "reward-renewal-0002", f.product.ID, old.Client.ID)
	newReceipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, renewal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if telegramClientTerms(old.Client) == telegramClientTerms(newReceipt.Client) {
		t.Fatal("renewal fixture did not change entitlement")
	}
	tgRewardHistoricalDeliveryFixture(t, f, newReceipt, telegramClientTerms(old.Client))
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); !errors.Is(err, ErrUnavailable) {
		t.Fatal("stale native entitlement unlocked renewal reward", err)
	}
	wallet, err := f.s.TelegramWallet(ctx, referrer)
	if err != nil || wallet.BonusStars != 20 {
		t.Fatal("unverified renewal changed wallet", wallet, err)
	}
}
