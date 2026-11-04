package telegrambot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

type businessFixture struct {
	store          *business.Store
	backend        *BusinessBackend
	manager, buyer Identity
	registration   business.TelegramStoreRegistration
	ownerToken     string
	product        business.CatalogProduct
}

func businessSetup(t *testing.T) businessFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := business.Open(filepath.Join(dir, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err = s.BootstrapOwner(ctx, "telegramowner", "synthetic-private-owner-passphrase"); err != nil {
		t.Fatal(err)
	}
	login, err := s.Login(ctx, "telegramowner", "synthetic-private-owner-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	for _, enable := range []func(context.Context) error{s.EnableUsageLedger, s.EnableTelegramCommerce, s.EnableTelegramSupportV1, s.EnableTelegramCampaignsV1, s.EnableTelegramRewardsV1, s.EnableTelegramBonusV1} {
		if err = enable(ctx); err != nil {
			t.Fatal(err)
		}
	}
	reg := business.TelegramStoreRegistration{EventID: "tg-adapter-store-register-0001", ID: "owner-shop", BotID: 100, RepresentativeID: "owner", Name: "DARK shop", ManagerUserID: 1001, Enabled: true}
	if _, err = s.ConfigureTelegramStore(ctx, login.Token, reg); err != nil {
		t.Fatal(err)
	}
	manager, buyer := Identity{100, 1001, 1001}, Identity{100, 2001, 2001}
	for _, id := range []Identity{manager, buyer} {
		if _, err = s.TelegramStart(ctx, actor(id)); err != nil {
			t.Fatal(err)
		}
	}
	spec := business.CatalogSpec{ID: "adapter-volume-plan", RepresentativeID: "owner", Name: "Volume plan", PlanKind: "volume", DurationDays: 30, IPCount: 2, VolumeBytes: 20 << 30, PriceToman: 10000}
	if _, err = s.CreateCatalogProduct(ctx, business.CatalogCreate{CatalogSpec: spec, EventID: "tg-adapter-create-product-0001", Reason: "Synthetic adapter test"}); err != nil {
		t.Fatal(err)
	}
	product, err := s.UpdateCatalogProduct(ctx, business.CatalogUpdate{CatalogSpec: spec, EventID: "tg-adapter-publish-product-0001", ExpectedRevision: 1, Status: "published", Reason: "Synthetic adapter test"})
	if err != nil {
		t.Fatal(err)
	}
	b := NewBusinessBackend(s)
	b.SupportEnabled = true
	b.CampaignsEnabled = true
	b.RewardsEnabled = true
	b.BonusEnabled = true
	return businessFixture{s, b, manager, buyer, reg, login.Token, product}
}
func (f businessFixture) request(t *testing.T, id Identity, action Action) Request {
	t.Helper()
	access, err := f.backend.Resolve(context.Background(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	return Request{Incoming: Incoming{Identity: id, Kind: "callback", BotUsername: "DarkLabBot", ReceivedUnix: time.Now().Unix()}, Access: access, Action: action, EventID: stableEvent(id.BotID, action.Name+action.Resource+action.Value), Language: "en"}
}
func (f businessFixture) price(t *testing.T) {
	t.Helper()
	if _, err := f.store.SetTelegramProductPrice(context.Background(), actor(f.manager), business.TelegramProductPrice{EventID: "tg-adapter-price-initial-0001", ProductID: f.product.ID, PriceStars: 25}); err != nil {
		t.Fatal(err)
	}
}
func actionNamed(screen *Screen, name string) *Action {
	if screen == nil {
		return nil
	}
	for _, row := range screen.Buttons {
		for _, button := range row {
			if button.Action != nil && button.Action.Name == name {
				return button.Action
			}
		}
	}
	return nil
}

func TestBusinessAdapterFirstStarsPriceAndCustomerAuthority(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	out, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "prices"}))
	if err != nil || actionNamed(out.Screen, "price") == nil {
		t.Fatal("fresh store cannot set first Stars price", err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "prices"})); err == nil {
		t.Fatal("customer obtained manager pricing")
	}
	f.price(t)
	out, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "product", Resource: f.product.ID}))
	if err != nil || !strings.Contains(out.Screen.Text, "20 GiB") || strings.Contains(out.Screen.Text, "21474836480 bytes") {
		t.Fatal("product volume not human readable", err)
	}
	out, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "wallet"}))
	if err != nil || !strings.Contains(out.Screen.Text, "redemption at checkout is not enabled") || actionNamed(out.Screen, "wallet_pay") != nil {
		t.Fatal("wallet overpromises redemption", err)
	}
	out, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "home"}))
	if err != nil || actionNamed(out.Screen, "trial") != nil || actionNamed(out.Screen, "orders") == nil {
		t.Fatal("disabled trial or missing order recovery menu", err)
	}
}

func TestBusinessAdapterPaymentAndRefundCaptureSurviveDisable(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	f.price(t)
	req := f.request(t, f.buyer, Action{Name: "buy", Resource: f.product.ID})
	invoice, err := f.backend.Handle(ctx, req)
	if err != nil || invoice.Invoice == nil {
		t.Fatal("invoice", err)
	}
	p := &Payment{Payload: invoice.Invoice.Payload, Currency: "XTR", Amount: invoice.Invoice.Amount, TelegramChargeID: "tg-adapter-late-charge"}
	incoming := Incoming{UpdateID: 2, Identity: f.buyer, Kind: "payment", Payment: p, ReceivedUnix: time.Now().Unix()}
	reg := f.registration
	reg.EventID = "tg-adapter-disable-store-0001"
	reg.ExpectedRevision = 1
	reg.Enabled = false
	if _, err = f.store.ConfigureTelegramStore(ctx, f.ownerToken, reg); err != nil {
		t.Fatal(err)
	}
	if _, err = f.backend.Handle(ctx, Request{Incoming: incoming, Access: req.Access, Language: "en"}); !errors.Is(err, ErrAccess) {
		t.Fatal("disabled store fulfilled", err)
	}
	paid, err := f.store.RecordTelegramPayment(ctx, actor(f.buyer), business.TelegramPayment{Payload: p.Payload, Currency: p.Currency, TotalAmount: p.Amount, TelegramChargeID: p.TelegramChargeID})
	if err != nil || !paid.Replayed || paid.Order.Status != "paid_pending_fulfillment" {
		t.Fatal("adapter lost money before auth", paid.Order.Status, err)
	}
	if _, err = f.store.FulfillTelegramOrder(ctx, actor(f.buyer), p.Payload); err == nil {
		t.Fatal("disabled service issued")
	}
	incoming.Kind = "refund"
	if _, err = f.backend.Handle(ctx, Request{Incoming: incoming, Access: req.Access, Language: "en"}); !errors.Is(err, ErrAccess) {
		t.Fatal("refund bypass granted access", err)
	}
	refunded, err := f.store.RecordTelegramRefund(ctx, actor(f.buyer), business.TelegramPayment{Payload: p.Payload, Currency: p.Currency, TotalAmount: p.Amount, TelegramChargeID: p.TelegramChargeID})
	if err != nil || refunded.Status != "refunded" {
		t.Fatal("authenticated refund lost", err)
	}
	p.Amount++
	if err = f.backend.CaptureFinancialEvent(ctx, incoming); err == nil {
		t.Fatal("mismatched payment amount captured")
	}
}

func TestBusinessAdapterSamePaidOrderRecoveryDoesNotRepay(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	f.price(t)
	invoice, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "buy", Resource: f.product.ID}))
	if err != nil {
		t.Fatal(err)
	}
	id := invoice.Invoice.Payload
	if err = f.backend.CaptureFinancialEvent(ctx, Incoming{Identity: f.buyer, Kind: "payment", Payment: &Payment{Payload: id, Currency: "XTR", Amount: 25, TelegramChargeID: "tg-adapter-recovery-charge"}}); err != nil {
		t.Fatal(err)
	}
	detail, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "order", Resource: id}))
	if err != nil || actionNamed(detail.Screen, "order_retry") == nil || detail.Invoice != nil {
		t.Fatal("paid recovery unavailable", err)
	}
	for n := 0; n < 2; n++ {
		out, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "order_fulfill", Resource: id}))
		if err != nil || out.Invoice != nil || actionNamed(out.Screen, "service") == nil {
			t.Fatal("same order did not recover", err)
		}
	}
	services, err := f.store.ListTelegramServices(ctx, actor(f.buyer))
	if err != nil || len(services) != 1 || services[0].State != "pending_node_apply" {
		t.Fatal("duplicate issuance or false online", services, err)
	}
	other := Identity{100, 3001, 3001}
	if _, err = f.backend.Resolve(ctx, other, true); err != nil {
		t.Fatal(err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, other, Action{Name: "order", Resource: id})); err == nil {
		t.Fatal("cross-customer paid order disclosed")
	}
}

func TestBusinessAdapterSupportFullThreadAndCampaignResume(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	body := strings.Repeat("<&سلام", 450) + "LAST-CHARACTER"
	receipt, err := f.store.TelegramCreateTicketV1(ctx, actor(f.buyer), business.TelegramTicketCreateV1{EventID: "tg-adapter-support-ticket-0001", Subject: "Full support thread", Text: body})
	if err != nil {
		t.Fatal(err)
	}
	r := f.request(t, f.buyer, Action{Name: "ticket", Resource: receipt.Ticket.ID})
	seenLast := false
	for n := 0; n < 20; n++ {
		out, err := f.backend.Handle(ctx, r)
		if err != nil || len(out.Screen.Text) > 4096 {
			t.Fatal("support page", err)
		}
		if strings.Contains(out.Screen.Text, "LAST-CHARACTER") {
			seenLast = true
			break
		}
		var next *Action
		for _, row := range out.Screen.Buttons {
			for _, button := range row {
				if button.Text == "Continue message" {
					next = button.Action
				}
			}
		}
		if next == nil {
			t.Fatal("support text truncated")
		}
		r.Action = *next
	}
	if !seenLast {
		t.Fatal("support tail inaccessible")
	}
	if _, err = f.store.TelegramSetMarketingConsentV1(ctx, actor(f.buyer), "tg-adapter-consent-0001", true); err != nil {
		t.Fatal(err)
	}
	saved, err := f.store.TelegramSaveCampaignV1(ctx, actor(f.manager), business.TelegramCampaignSaveV1{EventID: "tg-adapter-draft-0001", Title: "Approved draft", Content: business.TelegramCampaignContentV1{Text: "Announcement"}, Audience: "all"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "campaign_preview", Resource: saved.Campaign.ID}))
	if err != nil {
		t.Fatal(err)
	}
	approve := actionNamed(preview.Screen, "campaign_approve")
	if approve == nil {
		t.Fatal("missing approval")
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.manager, *approve)); err != nil {
		t.Fatal(err)
	}
	report, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "campaign", Resource: saved.Campaign.ID}))
	if err != nil {
		t.Fatal(err)
	}
	pause := actionNamed(report.Screen, "campaign_pause")
	if pause == nil {
		t.Fatal("missing pause")
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.manager, *pause)); err != nil {
		t.Fatal(err)
	}
	resume, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "campaign_resume_preview", Resource: saved.Campaign.ID}))
	if err != nil || !strings.Contains(resume.Screen.Text, "No new recipients") {
		t.Fatal("resume did not preserve audience", err)
	}
	confirm := actionNamed(resume.Screen, "campaign_resume")
	if confirm == nil {
		t.Fatal("missing concrete resume confirmation")
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.manager, *confirm)); err != nil {
		t.Fatal(err)
	}
	c, err := f.store.TelegramCampaignReportV1(ctx, actor(f.manager), saved.Campaign.ID)
	if err != nil || c.Campaign.State != "queued" {
		t.Fatal("resume fake UI", err)
	}
}
