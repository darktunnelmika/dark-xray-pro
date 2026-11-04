package business

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Restore threat fixtures alter data, then restore the exact immutable trigger
// DDL. These represent corrupted imported SQLite rows, not supported APIs.
func tgEngagementRestoreMutation(t *testing.T, s *Store, ddl []string, trigger, query string, args ...any) {
	t.Helper()
	var definition string
	for _, statement := range ddl {
		if strings.HasPrefix(statement, "CREATE TRIGGER "+trigger+" ") {
			definition = statement
			break
		}
	}
	if definition == "" {
		t.Fatal("trigger fixture missing", trigger)
	}
	if _, err := s.db.Exec("DROP TRIGGER " + trigger); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(definition); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramEngagementValidationPreservesValidHistoricalRows(t *testing.T) {
	ctx := context.Background()
	old, _ := opened(t)
	if err := validateTelegramEngagementData(ctx, old.db); err != nil {
		t.Fatal("legacy optional absence", err)
	}
	f := tgExtensionSetup(t)
	if _, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, TelegramTicketCreateV1{EventID: "validation-support-create-0001", Subject: "Help", Text: "A customer message"}); err != nil {
		t.Fatal(err)
	}
	tgCampaignConsent(t, f, f.buyer, "validation-0001", true)
	c := tgCampaignApprove(t, f, f.manager, tgCampaignDraft(t, f, f.manager, "validation-0001"), "validation-0001")
	d, ok, err := f.s.TelegramClaimCampaignDeliveryV1(ctx, f.store.BotID, time.Now().UTC())
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err = f.s.TelegramFinishCampaignDeliveryV1(ctx, f.store.BotID, TelegramCampaignDeliveryResultV1{DeliveryID: d.DeliveryID, AttemptID: d.AttemptID, Outcome: "sent", MessageID: 501}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	tgCampaignConsent(t, f, f.buyer, "validation-later-optout-0001", false)
	referrer := tgExtensionCustomer(t, f, 4001)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "validation-0001")
	tgRewardAttribute(t, f, referrer)
	order := tgCommercePaidOrder(t, f, "validation-reward-0001", f.product.ID, "")
	r, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, r, "")
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	if err = validateTelegramEngagementData(ctx, f.s.db); err != nil {
		t.Fatal("valid release/sent/optout history rejected", c.ID, err)
	}
	if _, err = f.s.RecordTelegramRefund(ctx, f.buyer, TelegramPayment{Payload: order.ID, Currency: "XTR", TelegramChargeID: "charge-validation-reward-0001", TotalAmount: order.AmountStars}); err != nil {
		t.Fatal(err)
	}
	if err = validateTelegramEngagementData(ctx, f.s.db); err != nil {
		t.Fatal("valid refund history rejected", err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("complete native snapshot rejected valid post-refund history", err)
	}
	reopened, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = validateTelegramEngagementData(ctx, reopened.db); err != nil {
		t.Fatal("reopened valid history", err)
	}
}

func TestTelegramEngagementValidationRejectsCrossStoreTicket(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	tgExtensionOtherStore(t, f, f.buyer.UserID)
	r, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, TelegramTicketCreateV1{EventID: "validation-ticket-scope-0001", Subject: "Help", Text: "Private request"})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateTelegramEngagementData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`UPDATE telegram_support_tickets SET store_id='shop-b' WHERE id=?`, r.Ticket.ID); err != nil {
		t.Fatal(err)
	}
	if err = validateTelegramEngagementData(ctx, f.s.db); err == nil {
		t.Fatal("individually valid FK rows merged customer stores")
	}
}

func TestTelegramEngagementValidationRecomputesApprovedAudience(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	tgCampaignConsent(t, f, f.buyer, "validation-audience-0001", true)
	c := tgCampaignApprove(t, f, f.manager, tgCampaignDraft(t, f, f.manager, "validation-audience-0001"), "validation-audience-0001")
	tgCampaignConsent(t, f, f.buyer, "validation-audience-off-0001", false)
	tgCampaignConsent(t, f, f.buyer, "validation-audience-on-0001", true)
	if err := validateTelegramEngagementData(ctx, f.s.db); err != nil {
		t.Fatal("new consent invalidated immutable old approval", err)
	}
	tgEngagementRestoreMutation(t, f.s, telegramCampaignSchemaV1, "telegram_campaign_deliveries_immutable_target", `UPDATE telegram_campaign_deliveries SET consent_revision=2 WHERE campaign_id=?`, c.ID)
	if err := validateTelegramEngagementData(ctx, f.s.db); err == nil {
		t.Fatal("changed approval snapshot accepted with valid FK and DDL")
	}
}

func TestTelegramEngagementValidationBindsReleasedBonusToActualReferrer(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	referrer := tgExtensionCustomer(t, f, 4001)
	other := tgExtensionCustomer(t, f, 4002)
	tgRewardPolicy(t, f, tgRewardFixedPolicy(), 0, "validation-wallet-0001")
	tgRewardAttribute(t, f, referrer)
	order := tgCommercePaidOrder(t, f, "validation-wallet-0001", f.product.ID, "")
	r, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	tgRewardHistoricalDeliveryFixture(t, f, r, "")
	if err = f.s.TelegramSettleDueRewardsV1(ctx, f.store.BotID, 10); err != nil {
		t.Fatal(err)
	}
	a, err := f.s.ResolveTelegramActor(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = appendTelegramWalletTx(ctx, tx, "validation-other-bonus-0001", f.store.ID, a.CustomerID, "bonus", "XTR", 20, "another valid synthetic bonus"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = validateTelegramEngagementData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	tgEngagementRestoreMutation(t, f.s, telegramRewardsSchemaV1, "telegram_rewards_releases_no_update", `UPDATE telegram_rewards_releases SET wallet_event_id='validation-other-bonus-0001' WHERE order_id=?`, order.ID)
	if err = validateTelegramEngagementData(ctx, f.s.db); err == nil {
		t.Fatal("other customer's valid ledger entry substituted for reward release")
	}
}

func TestTelegramEngagementReplayCannotSubstituteAuditMessage(t *testing.T) {
	ctx := context.Background()
	f := tgExtensionSetup(t)
	req := TelegramTicketCreateV1{EventID: "validation-replay-receipt-0001", Subject: "Private support", Text: "Original request"}
	r, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, req)
	if err != nil {
		t.Fatal(err)
	}
	r.Message.Text = "A substituted message from another thread"
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	tgEngagementRestoreMutation(t, f.s, telegramSupportSchemaV1, "telegram_support_events_no_update", `UPDATE telegram_support_events SET receipt_json=? WHERE event_id=?`, raw, req.EventID)
	if _, err = f.s.TelegramCreateTicketV1(ctx, f.buyer, req); err == nil {
		t.Fatal("idempotent retry trusted corrupt audit JSON")
	}
	if err = validateTelegramEngagementData(ctx, f.s.db); err == nil {
		t.Fatal("backup accepted receipt different from immutable message")
	}
}
