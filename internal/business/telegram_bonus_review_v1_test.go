package business

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// These adversarial tests obtain bonus through an actual paid referral order.
// The historical handoff fixture is deliberately synthetic, as documented by
// tgRewardHistoricalDeliveryFixture; no network or live service is involved.
func tgBonusReviewSeed(t *testing.T) (tgCommerceFixture, TelegramActor, TelegramPayment) {
	t.Helper()
	ctx := context.Background()
	f := tgExtensionSetup(t)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "bonus-review-seed")
	earningBuyer := tgExtensionCustomer(t, f, 2901)
	earning := f
	earning.buyer = earningBuyer
	tgRewardAttribute(t, earning, f.buyer)
	order := tgCommercePaidOrder(t, earning, "bonus-review-seed", f.product.ID, "")
	receipt, err := f.s.FulfillTelegramOrder(ctx, earningBuyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, earning, receipt, "")
	grant, err := f.s.TelegramSettleReferralRewardV1(ctx, f.buyer, order.ID)
	if err != nil || grant.AmountStars != 20 || grant.ReleasedUnix == 0 {
		t.Fatal("referral credit fixture was not actually released", grant, err)
	}
	return f, earningBuyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "charge-bonus-review-seed", TotalAmount: order.AmountStars}
}

func tgBonusReviewEnable(t *testing.T, f tgCommerceFixture) {
	t.Helper()
	ctx := context.Background()
	if err := f.s.EnableTelegramBonusV1(ctx); err != nil {
		t.Fatal(err)
	}
	policy, err := f.s.GetTelegramBonusPolicyV1(ctx, f.manager)
	if err != nil || policy.Enabled || policy.Revision != 0 {
		t.Fatal("redemption was not initially off", policy, err)
	}
	policy, err = f.s.SetTelegramBonusPolicyV1(ctx, f.manager, TelegramBonusPolicyChangeV1{EventID: "bonus-review-enable", ExpectedRevision: policy.Revision, Enabled: true})
	if err != nil || !policy.Enabled {
		t.Fatal("enable redemption", policy, err)
	}
}

func tgBonusReviewWallet(t *testing.T, f tgCommerceFixture, actor TelegramActor, bonus, reserved int64) {
	t.Helper()
	wallet, err := f.s.TelegramWallet(context.Background(), actor)
	available := bonus
	if available < 0 {
		available = 0
	}
	if err != nil || wallet.PaidStars != 0 || wallet.BonusStars != bonus || wallet.AvailableBonusStars != available || wallet.ReservedBonusStars != reserved {
		t.Fatalf("wallet conservation: want bonus=%d reserved=%d available=%d paid=0, got %+v, err=%v", bonus, reserved, available, wallet, err)
	}
}

func tgBonusReviewQuote(t *testing.T, f tgCommerceFixture, tag string, bonus int64) TelegramOrder {
	t.Helper()
	ctx := context.Background()
	order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: "bonus-review-quote-" + tag, ProductID: f.product.ID, BonusStars: bonus})
	if err != nil || order.AmountStars != 25-bonus || order.BonusDiscountStars != bonus {
		t.Fatal("exact requested discount was changed", order, err)
	}
	quote, err := f.s.TelegramBonusQuoteV1(ctx, f.buyer, order.ID)
	if err != nil || quote.State != "reserved" || quote.GrossStars != 25 || quote.NetStars != order.AmountStars || quote.DiscountStars != bonus || quote.OrderID != order.ID {
		t.Fatal("reserved quote does not bind its invoice", quote, err)
	}
	return order
}

func tgBonusReviewPayment(order TelegramOrder, tag string) TelegramPayment {
	return TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "bonus-review-charge-" + tag, TotalAmount: order.AmountStars}
}

func tgBonusReviewReopen(t *testing.T, f *tgCommerceFixture) {
	t.Helper()
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(context.Background(), f.path); err != nil {
		t.Fatal("valid financial state cannot be restored before restart", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.s = reopened
	t.Cleanup(func() { _ = reopened.Close() })
}

func tgBonusReviewCount(t *testing.T, f tgCommerceFixture, query string, args ...any) int {
	t.Helper()
	var count int
	if err := f.s.db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestTelegramBonusReviewConcurrentQuotesAndScopedCancellation(t *testing.T) {
	ctx := context.Background()
	f, _, _ := tgBonusReviewSeed(t)
	tgBonusReviewEnable(t, f)
	request := TelegramCheckoutRequest{EventID: "bonus-review-concurrent-stable", ProductID: f.product.ID, BonusStars: 10}
	type result struct {
		order TelegramOrder
		err   error
	}
	const contenders = 8
	start := make(chan struct{})
	results := make(chan result, contenders)
	var workers sync.WaitGroup
	for i := 0; i < contenders; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, request)
			results <- result{order, err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var first TelegramOrder
	created := 0
	for got := range results {
		if got.err != nil {
			t.Fatal("same authenticated quote did not replay", got.err)
		}
		if first.ID == "" {
			first = got.order
		}
		if got.order.ID != first.ID || got.order.AmountStars != 15 || got.order.BonusDiscountStars != 10 {
			t.Fatal("concurrent replay changed invoice", got.order)
		}
		if !got.order.Replayed {
			created++
		}
	}
	if created != 1 {
		t.Fatal("concurrent quote had multiple original writes", created)
	}
	tgBonusReviewWallet(t, f, f.buyer, 10, 10)
	changed := request
	changed.BonusStars = 11
	if _, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("event replay changed selected bonus", err)
	}

	// Distinct invoices compete for the ten remaining stars. Only one may
	// reserve them; no caller may receive a silently reduced discount.
	start = make(chan struct{})
	results = make(chan result, contenders)
	for i := 0; i < contenders; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			order, err := f.s.PrepareTelegramCheckout(ctx, f.buyer, TelegramCheckoutRequest{EventID: fmt.Sprintf("bonus-review-distinct-%02d", i), ProductID: f.product.ID, BonusStars: 10})
			results <- result{order, err}
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	var second TelegramOrder
	winners := 0
	for got := range results {
		if got.err == nil {
			winners++
			second = got.order
			if got.order.AmountStars != 15 || got.order.BonusDiscountStars != 10 {
				t.Fatal("concurrent discount was clamped", got.order)
			}
		} else if !errors.Is(got.err, ErrInsufficient) {
			t.Fatal("unexpected competing quote failure", got.err)
		}
	}
	if winners != 1 {
		t.Fatal("same bonus funded multiple distinct invoices", winners)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 20)

	stranger := tgExtensionCustomer(t, f, 2902)
	otherStore := tgExtensionOtherStore(t, f, f.buyer.UserID)
	for i, actor := range []TelegramActor{stranger, otherStore} {
		if _, err := f.s.TelegramBonusQuoteV1(ctx, actor, first.ID); err == nil {
			t.Fatal("foreign customer read reserved quote", actor.BotID, actor.UserID)
		}
		if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, actor, first.ID, fmt.Sprintf("bonus-review-foreign-cancel-%d", i)); err == nil {
			t.Fatal("foreign customer released reserved credit", actor.BotID, actor.UserID)
		}
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 20)

	errorsCh := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, first.ID, "bonus-review-concurrent-cancel")
			errorsCh <- err
		}()
	}
	workers.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal("concurrent cancellation replay", err)
		}
	}
	tgBonusReviewWallet(t, f, f.buyer, 10, 10)
	if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, second.ID, "bonus-review-concurrent-cancel"); !errors.Is(err, ErrConflict) {
		t.Fatal("cancellation event transplanted to another invoice", err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 10, 10)
	if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, second.ID, "bonus-review-second-cancel"); err != nil {
		t.Fatal(err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 20, 0)
	if _, err := f.s.CheckTelegramPreCheckout(ctx, f.buyer, first.ID, "XTR", first.AmountStars); err == nil {
		t.Fatal("released reservation passed precheckout")
	}
}

func TestTelegramBonusReviewClawbackReleaseLateCaptureAndRestart(t *testing.T) {
	ctx := context.Background()
	f, earningBuyer, earningPayment := tgBonusReviewSeed(t)
	tgBonusReviewEnable(t, f)
	order := tgBonusReviewQuote(t, f, "clawback", 20)
	tgBonusReviewWallet(t, f, f.buyer, 0, 20)
	if _, err := f.s.RecordTelegramRefund(ctx, earningBuyer, earningPayment); err != nil {
		t.Fatal("verified source award refund", err)
	}
	tgBonusReviewWallet(t, f, f.buyer, -20, 20)
	if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, order.ID, "bonus-review-clawback-release"); err != nil {
		t.Fatal("reservation could not release into bonus debt", err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	tgBonusReviewReopen(t, &f)
	quote, err := f.s.TelegramBonusQuoteV1(ctx, f.buyer, order.ID)
	if err != nil || quote.State != "released" || quote.DiscountStars != 20 {
		t.Fatal("restart lost released original discount", quote, err)
	}
	if _, err = f.s.CheckTelegramPreCheckout(ctx, f.buyer, order.ID, "XTR", order.AmountStars); err == nil {
		t.Fatal("restart made released invoice payable again")
	}
	payment := tgBonusReviewPayment(order, "clawback")
	wrong := payment
	wrong.TotalAmount += order.BonusDiscountStars
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, wrong); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("gross amount substituted for external payment", err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	if _, err = f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil {
		t.Fatal("valid delayed payment lost after bonus clawback", err)
	}
	tgBonusReviewWallet(t, f, f.buyer, -20, 0)
	if replay, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil || !replay.Replayed {
		t.Fatal("delayed capture replay", replay, err)
	}
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil || receipt.Order.AmountStars != 5 || receipt.Order.BonusDiscountStars != 20 {
		t.Fatal("bonus debt prevented paid native fulfillment", receipt, err)
	}
	if again, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err != nil || !again.Replayed || again.Client.ID != receipt.Client.ID {
		t.Fatal("fulfillment replay duplicated paid service", again, err)
	}
	tgBonusReviewWallet(t, f, f.buyer, -20, 0)
	if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, order.ID, "bonus-review-stale-paid-cancel"); err == nil {
		t.Fatal("stale cancellation restored consumed credit")
	}
	tgBonusReviewWallet(t, f, f.buyer, -20, 0)
	if _, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err != nil {
		t.Fatal(err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	tgBonusReviewReopen(t, &f)
	if replay, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil || !replay.Replayed {
		t.Fatal("old paid fact failed replay after refund/restart", replay, err)
	}
	if replay, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err != nil || !replay.Replayed {
		t.Fatal("refund replay after restart", replay, err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	quote, err = f.s.TelegramBonusQuoteV1(ctx, f.buyer, order.ID)
	if err != nil || quote.State != "refunded" || quote.DiscountStars != 20 {
		t.Fatal("refund lost consumed funding identity", quote, err)
	}
	for _, table := range []string{"telegram_commerce_payments", "telegram_commerce_payment_refunds", "telegram_commerce_order_receipts"} {
		if n := tgBonusReviewCount(t, f, "SELECT COUNT(*) FROM "+table+" WHERE order_id=?", order.ID); n != 1 {
			t.Fatal("duplicate or missing durable financial identity", table, n)
		}
	}
}

func TestTelegramBonusReviewReleasedCreditSpentElsewhereAndDisabledCapture(t *testing.T) {
	ctx := context.Background()
	f, _, _ := tgBonusReviewSeed(t)
	tgBonusReviewEnable(t, f)
	late := tgBonusReviewQuote(t, f, "released-late", 20)
	if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, late.ID, "bonus-review-release-before-spend"); err != nil {
		t.Fatal(err)
	}
	other := tgBonusReviewQuote(t, f, "spent-elsewhere", 20)
	otherPayment := tgBonusReviewPayment(other, "spent-elsewhere")
	if _, err := f.s.RecordTelegramPayment(ctx, f.buyer, otherPayment); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, other.ID); err != nil {
		t.Fatal(err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	access, err := f.s.ResolveTelegramActor(ctx, f.buyer)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "bonus-review-disable-store", ID: f.store.ID, BotID: f.store.BotID, RepresentativeID: f.store.RepresentativeID, Name: f.store.Name, ManagerUserID: f.manager.UserID, ExpectedRevision: f.store.Revision, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	assertDisabledBalance := func(want int64) {
		t.Helper()
		var bonus, paid int64
		if err := f.s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN bucket='bonus' THEN delta ELSE 0 END),0),COALESCE(SUM(CASE WHEN bucket='paid' THEN delta ELSE 0 END),0) FROM telegram_commerce_wallet_ledger WHERE store_id=? AND customer_id=?`, f.store.ID, access.CustomerID).Scan(&bonus, &paid); err != nil || bonus != want || paid != 0 {
			t.Fatal("disabled-store financial conservation", bonus, paid, want, err)
		}
	}
	payment := tgBonusReviewPayment(late, "released-late")
	wrongActor := f.buyer
	wrongActor.BotID++
	if _, err := f.s.RecordTelegramPayment(ctx, wrongActor, payment); !errors.Is(err, ErrTelegramPayment) {
		t.Fatal("foreign bot triggered a late compensation", err)
	}
	assertDisabledBalance(0)

	// A storage error in the compensation must leave capture retryable and
	// must not publish a partial paid fact or consume the discount twice.
	if _, err := f.s.db.Exec(`CREATE TRIGGER bonus_review_abort_late_debit BEFORE INSERT ON telegram_commerce_wallet_ledger WHEN NEW.bucket='bonus' AND NEW.delta<0 BEGIN SELECT RAISE(ABORT,'synthetic late-debit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment); err == nil {
		t.Fatal("late capture ignored compensation storage failure")
	}
	assertDisabledBalance(0)
	if n := tgBonusReviewCount(t, f, `SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=?`, late.ID); n != 0 {
		t.Fatal("partial late-capture transaction", n)
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER bonus_review_abort_late_debit`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil {
		t.Fatal("disabled shop lost a valid late financial event", err)
	}
	assertDisabledBalance(-20)
	if replay, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil || !replay.Replayed {
		t.Fatal("disabled shop capture replay", replay, err)
	}
	assertDisabledBalance(-20)
	if _, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err != nil {
		t.Fatal("disabled shop blocked verified restoration", err)
	}
	assertDisabledBalance(0)
	if replay, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err != nil || !replay.Replayed {
		t.Fatal("disabled shop refund replay", replay, err)
	}
	assertDisabledBalance(0)
	if _, err := f.s.ConfigureTelegramStore(ctx, f.owner.Token, TelegramStoreRegistration{EventID: "bonus-review-enable-store-again", ID: f.store.ID, BotID: f.store.BotID, RepresentativeID: f.store.RepresentativeID, Name: f.store.Name, ManagerUserID: f.manager.UserID, ExpectedRevision: disabled.Revision, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	if _, err := f.s.RecordTelegramRefund(ctx, f.buyer, otherPayment); err != nil {
		t.Fatal(err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 20, 0)
	if _, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, late.ID, "bonus-review-stale-refunded-cancel"); err == nil {
		t.Fatal("stale cancel added another refund")
	}
	tgBonusReviewWallet(t, f, f.buyer, 20, 0)
}

func TestTelegramBonusReviewCaptureCancellationRaceAndConcurrentRefund(t *testing.T) {
	ctx := context.Background()
	f, _, _ := tgBonusReviewSeed(t)
	tgBonusReviewEnable(t, f)
	order := tgBonusReviewQuote(t, f, "capture-cancel-race", 20)
	payment := tgBonusReviewPayment(order, "capture-cancel-race")
	start := make(chan struct{})
	captureResult := make(chan error, 1)
	cancelResult := make(chan error, 1)
	go func() {
		<-start
		_, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment)
		captureResult <- err
	}()
	go func() {
		<-start
		_, err := f.s.CancelTelegramBonusQuoteV1(ctx, f.buyer, order.ID, "bonus-review-racing-cancel")
		cancelResult <- err
	}()
	close(start)
	captureErr, cancelErr := <-captureResult, <-cancelResult
	if captureErr != nil {
		t.Fatal("cancellation race lost authenticated payment", captureErr)
	}
	if cancelErr != nil && !errors.Is(cancelErr, ErrConflict) {
		t.Fatal("unexpected cancellation race result", cancelErr)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	quote, err := f.s.TelegramBonusQuoteV1(ctx, f.buyer, order.ID)
	if err != nil || quote.State != "consumed" {
		t.Fatal("paid race winner did not own consumed credit", quote, err)
	}
	const contenders = 8
	refundResults := make(chan error, contenders)
	var workers sync.WaitGroup
	for i := 0; i < contenders; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment)
			refundResults <- err
		}()
	}
	workers.Wait()
	close(refundResults)
	for err := range refundResults {
		if err != nil {
			t.Fatal("concurrent verified refund did not replay", err)
		}
	}
	tgBonusReviewWallet(t, f, f.buyer, 20, 0)
	if n := tgBonusReviewCount(t, f, `SELECT COUNT(*) FROM telegram_commerce_payment_refunds WHERE order_id=?`, order.ID); n != 1 {
		t.Fatal("duplicate refund fact", n)
	}
}

func TestTelegramBonusReviewDiscountRewardCapLegacyGrantAndRefundAtomicity(t *testing.T) {
	ctx := context.Background()
	f, _, _ := tgBonusReviewSeed(t)
	referrer := tgExtensionCustomer(t, f, 4901)
	legacyBuyer := tgExtensionCustomer(t, f, 2903)
	legacy := f
	legacy.buyer = legacyBuyer
	tgRewardAttribute(t, legacy, referrer)
	spec := f.product.CatalogSpec
	spec.ID = "bonus-review-legacy-cheap"
	spec.Name = "Legacy fixed reward offer"
	cheap := tgCommercePublish(t, f.s, spec)
	if _, err := f.s.SetTelegramProductPrice(ctx, f.manager, TelegramProductPrice{EventID: "bonus-review-cheap-price", ProductID: cheap.ID, PriceStars: 5}); err != nil {
		t.Fatal(err)
	}
	legacyOrder := tgCommercePaidOrder(t, legacy, "bonus-review-legacy-cheap", cheap.ID, "")
	legacyReceipt, err := f.s.FulfillTelegramOrder(ctx, legacyBuyer, legacyOrder.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, legacy, legacyReceipt, "")
	legacyGrant, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, legacyOrder.ID)
	if err != nil || legacyGrant.AmountStars != 20 || legacyGrant.ReleasedUnix == 0 {
		t.Fatal("legacy fixed reward semantics changed before feature enable", legacyGrant, err)
	}
	// The original accepted policy is frozen before enabling discounts, but
	// a new discounted purchase must not reward more than external net XTR.
	tgRewardAttribute(t, f, referrer)
	tgBonusReviewEnable(t, f)
	if replay, err := f.s.FulfillTelegramOrder(ctx, legacyBuyer, legacyOrder.ID); err != nil || !replay.Replayed || replay.Client.ID != legacyReceipt.Client.ID || replay.Order.BonusDiscountStars != 0 {
		t.Fatal("optional bonus extension invalidated a legacy receipt", replay, err)
	}
	order := tgBonusReviewQuote(t, f, "reward-cap", 20)
	payment := tgBonusReviewPayment(order, "reward-cap")
	if _, err := f.s.RecordTelegramPayment(ctx, f.buyer, payment); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, receipt, "")
	grant, err := f.s.TelegramSettleReferralRewardV1(ctx, referrer, order.ID)
	if err != nil || grant.AmountStars != order.AmountStars || grant.AmountStars != 5 || grant.ReleasedUnix == 0 {
		t.Fatal("discount rewarded recycled internal credit", grant, err)
	}
	tgBonusReviewWallet(t, f, referrer, 25, 0)
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	if _, err := f.s.db.Exec(`CREATE TRIGGER bonus_review_abort_refund_restore BEFORE INSERT ON telegram_commerce_wallet_ledger WHEN NEW.bucket='bonus' AND NEW.delta=20 BEGIN SELECT RAISE(ABORT,'synthetic refund restoration failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err == nil {
		t.Fatal("refund ignored bonus restoration storage failure")
	}
	if n := tgBonusReviewCount(t, f, `SELECT COUNT(*) FROM telegram_commerce_payment_refunds WHERE order_id=?`, order.ID); n != 0 {
		t.Fatal("partial verified refund despite failed restoration", n)
	}
	if n := tgBonusReviewCount(t, f, `SELECT COUNT(*) FROM telegram_rewards_reversals WHERE order_id=?`, order.ID); n != 0 {
		t.Fatal("reward reversed separately from failed restoration", n)
	}
	tgBonusReviewWallet(t, f, f.buyer, 0, 0)
	tgBonusReviewWallet(t, f, referrer, 25, 0)
	if _, err := f.s.db.Exec(`DROP TRIGGER bonus_review_abort_refund_restore`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err != nil {
		t.Fatal(err)
	}
	if replay, err := f.s.RecordTelegramRefund(ctx, f.buyer, payment); err != nil || !replay.Replayed {
		t.Fatal("discount refund replay", replay, err)
	}
	tgBonusReviewWallet(t, f, f.buyer, 20, 0)
	tgBonusReviewWallet(t, f, referrer, 20, 0)
	legacyGrant, err = f.s.TelegramSettleReferralRewardV1(ctx, referrer, legacyOrder.ID)
	if err != nil || legacyGrant.AmountStars != 20 || legacyGrant.ReversedUnix != 0 {
		t.Fatal("new discounted grant cap rewrote historical fixed grant", legacyGrant, err)
	}
	if err := validateTelegramEngagementData(ctx, f.s.db); err != nil {
		t.Fatal("restorable reward history rejected after discount/refund", err)
	}
}

func tgBonusReviewForeignKeysClean(t *testing.T, f tgCommerceFixture) {
	t.Helper()
	rows, err := f.s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("threat fixture only broke a foreign key instead of semantic binding")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func tgBonusReviewRejectCorruptSnapshot(t *testing.T, f tgCommerceFixture) {
	t.Helper()
	ctx := context.Background()
	tgBonusReviewForeignKeysClean(t, f)
	if err := f.s.VerifyTelegramBonusV1(ctx); err == nil {
		t.Fatal("bonus semantic verifier accepted transplanted/orphan funding")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(ctx, f.path); err == nil {
		t.Fatal("offline native restore accepted transplanted/orphan funding")
	}
}

func TestTelegramBonusReviewBackupRejectsOrphanLateDebitsWithAndWithoutFamily(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("bonus-family-%t", enabled), func(t *testing.T) {
			ctx := context.Background()
			f, _, earningPayment := tgBonusReviewSeed(t)
			if enabled {
				tgBonusReviewEnable(t, f)
			}
			if err := f.s.VerifyTelegramBonusV1(ctx); err != nil {
				t.Fatal("clean pre-attack bonus state rejected", err)
			}
			tgBonusReviewReopen(t, &f)
			access, err := f.s.ResolveTelegramActor(ctx, f.buyer)
			if err != nil {
				t.Fatal(err)
			}
			// This is an imported-row threat fixture, not a production financial
			// operation. Its wallet fingerprint is valid, but no discounted order
			// or authenticated settlement authorizes this reserved event prefix.
			tx, err := f.s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := appendTelegramWalletEntryTx(ctx, tx, "tg-bonus-late:"+earningPayment.Payload, f.store.ID, access.CustomerID, "bonus", "XTR", -1, telegramBonusLateReason, true); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			tgBonusReviewRejectCorruptSnapshot(t, f)
		})
	}
}

func TestTelegramBonusReviewBackupRejectsCrossStoreQuoteWithValidForeignKeys(t *testing.T) {
	ctx := context.Background()
	f, _, _ := tgBonusReviewSeed(t)
	tgBonusReviewEnable(t, f)
	order := tgBonusReviewQuote(t, f, "backup-transplant", 10)
	other := tgExtensionOtherStore(t, f, f.buyer.UserID)
	otherManager := TelegramActor{BotID: other.BotID, UserID: 1002, ChatID: 1002}
	if _, err := f.s.TelegramStart(ctx, otherManager); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetTelegramBonusPolicyV1(ctx, otherManager, TelegramBonusPolicyChangeV1{EventID: "bonus-review-other-store-policy", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	otherAccess, err := f.s.ResolveTelegramActor(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.VerifyTelegramBonusV1(ctx); err != nil {
		t.Fatal("valid multi-store bonus state rejected", err)
	}
	tgBonusReviewReopen(t, &f)
	// Restore the original immutable DDL after replacing the row, so passing
	// schema/FK checks cannot mask a missing order/store/customer binding.
	tgEngagementRestoreMutation(t, f.s, telegramBonusSchemaV1, "telegram_bonus_quotes_no_update", `UPDATE telegram_bonus_quotes SET store_id=?,customer_id=? WHERE order_id=?`, otherAccess.Store.ID, otherAccess.CustomerID, order.ID)
	tgBonusReviewRejectCorruptSnapshot(t, f)
}
