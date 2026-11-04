package business

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
)

func TestTelegramNativeBackupRetainsFinancialAndOptionalEngagementState(t *testing.T) {
	f := tgCommerceSetup(t, false)
	ctx := context.Background()
	for _, enable := range []func(context.Context) error{f.s.EnableTelegramSupportV1, f.s.EnableTelegramCampaignsV1, f.s.EnableTelegramRewardsV1} {
		if err := enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	order := tgCommercePaidOrder(t, f, "backup-order", f.product.ID, "")
	if _, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TelegramCreateTicketV1(ctx, f.buyer, TelegramTicketCreateV1{EventID: "tg-backup-ticket-0001", Subject: "Connection assistance", Text: "Synthetic request"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ValidateOfflineBackup(ctx, f.path)
	if err != nil {
		t.Fatal("native bot snapshot rejected", err)
	}
	for table, want := range map[string]int64{"telegram_commerce_payments": 1, "telegram_commerce_order_receipts": 1, "telegram_support_tickets": 1, "telegram_rewards_meta": 1, "telegram_campaign_meta": 1} {
		if result.TableCounts[table] != want {
			t.Fatal("optional bot state not verified", table, result.TableCounts[table])
		}
	}
	after, err := os.ReadFile(f.path)
	if err != nil || sha256.Sum256(after) != sha256.Sum256(before) {
		t.Fatal("offline verification mutated bot state", err)
	}
	mutateOfflineFixture(t, f.path, "DROP TRIGGER telegram_commerce_payments_no_update")
	if _, err := ValidateOfflineBackup(ctx, f.path); err == nil {
		t.Fatal("missing money immutability guard accepted")
	}
}
