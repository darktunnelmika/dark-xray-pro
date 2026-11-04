package business

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

func TestTelegramDeliveryRequiresNativeProofAndDefiniteScopedReceipt(t *testing.T) {
	f := tgCommerceSetup(t, true)
	ctx := context.Background()
	order := tgCommercePaidOrder(t, f, "delivery-first", f.product.ID, "")
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x62}, 32)
	plan := NativeInboundPlanInput{EventID: "tg-delivery-native-plan-0001", Profile: InboundInput{ID: 501, Name: "Telegram native delivery", Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Settings: map[string]any{"decryption": "none"}}}, Nodes: []NativeInboundPlanNode{{NodeID: "remote-a", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10", DirectPort: 21185, DirectHosts: []string{"a.example.org"}, TunnelHosts: []string{}}}, Reason: "Synthetic delivery proof"}
	if _, err = f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
		t.Fatal(err)
	}
	native := &executionFixture{s: f.s, key: key, path: f.path, plan: plan, client: receipt.Client}
	if _, err = f.s.TelegramServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, "uri"); err == nil {
		t.Fatal("paid pending service falsely delivered")
	}
	native.command(t, "tg-delivery-activate-0001", "activate")
	b := native.bundle(t)
	epoch := native.birth(t, b, "tg-delivery-epoch-0001")
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, key, executionReport(b, epoch, 1, 0, 0, false)); err != nil {
		t.Fatal(err)
	}
	observation := executionObservation(b, epoch)
	observation.HealthState = "engine_ready"
	if err = f.s.ObserveInboundExecution(ctx, key, observation); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.TelegramServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, "uri"); err == nil {
		t.Fatal("engine readiness treated as verified service")
	}
	observation = executionObservation(b, epoch)
	observation.HealthState = "healthy"
	if err = f.s.ObserveInboundExecution(ctx, key, observation); err != nil {
		t.Fatal(err)
	}
	delivery, err := f.s.TelegramServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, "uri")
	if err != nil || !strings.Contains(delivery.Content, "vless://") || delivery.ExpiresUnixNano <= time.Now().UnixNano() {
		t.Fatal("native verified export unavailable", err)
	}
	var count int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM telegram_commerce_order_deliveries").Scan(&count); err != nil || count != 0 {
		t.Fatal("preparing content marked delivery", err, count)
	}
	other := TelegramActor{BotID: f.buyer.BotID, UserID: 3333, ChatID: 3333}
	if _, err = f.s.TelegramStart(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.TelegramServiceDelivery(ctx, key, other, receipt.Client.ID, "uri"); !errors.Is(err, ErrTelegramDenied) {
		t.Fatal("cross-customer secret disclosure", err)
	}
	if err = f.s.TelegramConfirmServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, strings.Repeat("0", 64), delivery.TermsSHA256, delivery.ExpiresUnixNano, 100); err == nil {
		t.Fatal("forged proof marked delivery")
	}
	if err = f.s.TelegramConfirmServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, delivery.SHA256, delivery.TermsSHA256, time.Now().Add(-time.Second).UnixNano(), 100); err == nil {
		t.Fatal("expired proof marked delivery")
	}
	if err = f.s.TelegramConfirmServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, delivery.SHA256, delivery.TermsSHA256, delivery.ExpiresUnixNano, 0); err == nil {
		t.Fatal("unknown send marked delivered")
	}
	for i := 0; i < 2; i++ {
		if err = f.s.TelegramConfirmServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, delivery.SHA256, delivery.TermsSHA256, delivery.ExpiresUnixNano, 100); err != nil {
			t.Fatal("definite send confirmation", err)
		}
	}
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM telegram_commerce_order_deliveries").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate native delivery receipt", count, err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := telegramOrderDeliveryVerifiedTx(ctx, tx, order.ID)
	tx.Rollback()
	if err != nil || !verified {
		t.Fatal("verified handoff did not authorize reward evaluation", verified, err)
	}
	renew := tgCommercePaidOrder(t, f, "delivery-renewal", f.product.ID, receipt.Client.ID)
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, renew.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.TelegramServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, "uri"); err == nil {
		t.Fatal("old runtime terms delivered after renewal")
	}
	tx, err = f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	verified, err = telegramOrderDeliveryVerifiedTx(ctx, tx, renew.ID)
	tx.Rollback()
	if err != nil || verified {
		t.Fatal("old handoff unlocked renewal reward", verified, err)
	}
}

func TestTelegramDeliveryPreparedBeforeRenewalCannotConfirmNewOrderWithSameURI(t *testing.T) {
	f := tgCommerceSetup(t, true)
	ctx := context.Background()
	first := tgCommercePaidOrder(t, f, "two-clients-first", f.product.ID, "")
	receipt, err := f.s.FulfillTelegramOrder(ctx, f.buyer, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := f.s.CreateClient(ctx, NewClient{EventID: "tg-delivery-earlier-peer-0001", ClientID: "earlier-peer", RepresentativeID: "owner", Name: "Earlier expiry", DurationDays: 1, IPCount: 1, VolumeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x63}, 32)
	plan := NativeInboundPlanInput{EventID: "tg-delivery-two-client-plan", Profile: InboundInput{ID: 501, Name: "Shared expiry", Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Settings: map[string]any{"decryption": "none"}}}, Nodes: []NativeInboundPlanNode{{NodeID: "remote-a", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10", DirectPort: 21185, DirectHosts: []string{"a.example.org"}, TunnelHosts: []string{}}}, Reason: "Two-client entitlement binding"}
	if _, err = f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
		t.Fatal(err)
	}
	native := &executionFixture{s: f.s, key: key, path: f.path, plan: plan, client: receipt.Client}
	state, err := f.s.InboundExecution(ctx, key, 501)
	if err != nil {
		t.Fatal(err)
	}
	in := ix.CommandInput{EventID: "tg-delivery-two-client-activate", Action: "activate", ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{receipt.Client.ID, peer.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Shared current native route"}
	preview, err := f.s.PreviewInboundExecution(ctx, key, "owner", 501, in)
	if err != nil || !preview.Valid {
		t.Fatal(err, preview.Blockers)
	}
	in.ReviewSHA = preview.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	bundle := native.bundle(t)
	epoch := native.birth(t, bundle, "tg-delivery-two-client-epoch")
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, key, executionReport(bundle, epoch, 1, 0, 0, false)); err != nil {
		t.Fatal(err)
	}
	if err = f.s.ObserveInboundExecution(ctx, key, executionObservation(bundle, epoch)); err != nil {
		t.Fatal(err)
	}
	old, err := f.s.TelegramServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, "uri")
	if err != nil {
		t.Fatal(err)
	}
	renew := tgCommercePaidOrder(t, f, "two-clients-renew", f.product.ID, receipt.Client.ID)
	if _, err = f.s.FulfillTelegramOrder(ctx, f.buyer, renew.ID); err != nil {
		t.Fatal(err)
	}
	current, err := f.s.TelegramServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, "uri")
	if err != nil {
		t.Fatal("unchanged native route with current HUB entitlement", err)
	}
	if old.SHA256 != current.SHA256 || old.TermsSHA256 == current.TermsSHA256 {
		t.Fatal("test did not preserve URI while changing entitlement")
	}
	if err = f.s.TelegramConfirmServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, old.SHA256, old.TermsSHA256, old.ExpiresUnixNano, 200); err == nil {
		t.Fatal("old in-flight send confirmed new renewal")
	}
	if err = f.s.TelegramConfirmServiceDelivery(ctx, key, f.buyer, receipt.Client.ID, current.SHA256, current.TermsSHA256, current.ExpiresUnixNano, 201); err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	verified, err := telegramOrderDeliveryVerifiedTx(ctx, tx, renew.ID)
	if err != nil || !verified {
		t.Fatal("current entitlement handoff absent", verified, err)
	}
	if err = validateTelegramDeliveryDataTx(ctx, tx); err != nil {
		t.Fatal("valid current delivery rejected by backup validator", err)
	}
}
