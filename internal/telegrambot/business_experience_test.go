package telegrambot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func actionWithText(screen *Screen, text string) *Action {
	if screen != nil {
		for _, row := range screen.Buttons {
			for _, button := range row {
				if button.Text == text {
					return button.Action
				}
			}
		}
	}
	return nil
}

func TestRepresentativeApplicationUsesScopedSupportWithoutGrantingAccess(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	home, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "home"}))
	if err != nil || actionNamed(home.Screen, "representative_apply") == nil {
		t.Fatal("missing HUB application entry", err)
	}
	prompt, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "representative_apply"}))
	if err != nil || prompt.Input == nil {
		t.Fatal("missing application prompt", err)
	}
	r := f.request(t, f.buyer, *prompt.Input)
	r.Incoming.Kind, r.Incoming.Text = "message", "My shop needs volume plans. Please review my application."
	for n := 0; n < 2; n++ {
		out, err := f.backend.Handle(ctx, r)
		if err != nil || !out.Receipt || actionNamed(out.Screen, "ticket") == nil {
			t.Fatal("application not retained", err)
		}
	}
	tickets, err := f.store.TelegramTicketsV1(ctx, actor(f.buyer), business.TelegramTicketFilterV1{Own: true, Limit: 10})
	if err != nil || len(tickets) != 1 || tickets[0].Subject != "Representative application" {
		t.Fatal("duplicate or missing application", tickets, err)
	}
	access, err := f.backend.Resolve(ctx, f.buyer, false)
	if err != nil || access.Role != "customer" || access.Has("manage_store") {
		t.Fatal("application granted representative authority", access, err)
	}
	if _, err = f.store.CreateRepresentative(ctx, "application-rep", "Independent representative"); err != nil {
		t.Fatal(err)
	}
	reg := business.TelegramStoreRegistration{EventID: "tg-application-second-store-0001", ID: "application-rep-store", BotID: 101, RepresentativeID: "application-rep", Name: "Representative shop", ManagerUserID: 1002, Enabled: true}
	if _, err = f.store.ConfigureTelegramStore(ctx, f.ownerToken, reg); err != nil {
		t.Fatal(err)
	}
	other := Identity{101, 2001, 2001}
	if _, err = f.backend.Resolve(ctx, other, true); err != nil {
		t.Fatal(err)
	}
	home, err = f.backend.Handle(ctx, f.request(t, other, Action{Name: "home"}))
	if err != nil || actionNamed(home.Screen, "representative_apply") != nil {
		t.Fatal("representative shop offered HUB administration", err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, other, Action{Name: "representative_apply"})); !errors.Is(err, ErrAccess) {
		t.Fatal("application crossed HUB boundary", err)
	}
}

func TestSupportClaimReopenAndStaleCustomerActionsPreserveAssignment(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	created, err := f.store.TelegramCreateTicketV1(ctx, actor(f.buyer), business.TelegramTicketCreateV1{EventID: "tg-experience-ticket-create-0001", Subject: "Connection help", Text: "Please help with my connection."})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Ticket.ID
	managerView, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "ticket", Resource: id}))
	if err != nil {
		t.Fatal(err)
	}
	claim := actionNamed(managerView.Screen, "ticket_claim")
	if claim == nil {
		t.Fatal("support cannot accept a ticket")
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, *claim)); !errors.Is(err, ErrAccess) {
		t.Fatal("customer changed support assignment", err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.manager, *claim)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "ticket_close", Resource: id, Value: "1"})); !errors.Is(err, business.ErrConflict) {
		t.Fatal("stale close changed the current ticket", err)
	}
	current, err := f.store.TelegramTicketByIDV1(ctx, actor(f.buyer), id)
	if err != nil || current.AssignedTelegramUserID != f.manager.UserID || current.Revision != 2 {
		t.Fatal("assignment missing", current, err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "ticket_close", Resource: id, Value: "2"})); err != nil {
		t.Fatal(err)
	}
	closed, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "ticket", Resource: id}))
	if err != nil || actionNamed(closed.Screen, "ticket_reopen") == nil {
		t.Fatal("closed ticket has no reopen path", err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, *actionNamed(closed.Screen, "ticket_reopen"))); err != nil {
		t.Fatal(err)
	}
	current, err = f.store.TelegramTicketByIDV1(ctx, actor(f.buyer), id)
	if err != nil || current.Status != "open" || current.AssignedTelegramUserID != f.manager.UserID {
		t.Fatal("reopen removed supporter or failed", current, err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "ticket_unassign", Resource: id, Value: strconv.FormatInt(current.Revision, 10)})); !errors.Is(err, ErrAccess) {
		t.Fatal("customer removed supporter", err)
	}
	managerView, err = f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "ticket", Resource: id}))
	if err != nil || actionNamed(managerView.Screen, "ticket_unassign") == nil {
		t.Fatal("support cannot return ticket to queue", err)
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.manager, *actionNamed(managerView.Screen, "ticket_unassign"))); err != nil {
		t.Fatal(err)
	}
	current, err = f.store.TelegramTicketByIDV1(ctx, actor(f.buyer), id)
	if err != nil || current.AssignedTelegramUserID != 0 {
		t.Fatal("ticket did not return to queue", current, err)
	}
}

func TestAllPublishedPricesAndCompatibleRenewalsRemainReachable(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	f.price(t)
	order, err := f.store.PrepareTelegramCheckout(ctx, actor(f.buyer), business.TelegramCheckoutRequest{EventID: "tg-pagination-original-order-0001", ProductID: f.product.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.RecordTelegramPayment(ctx, actor(f.buyer), business.TelegramPayment{Payload: order.ID, Currency: "XTR", TotalAmount: 25, TelegramChargeID: "tg-pagination-original-charge"}); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.store.FulfillTelegramOrder(ctx, actor(f.buyer), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 21; n++ {
		spec := business.CatalogSpec{ID: fmt.Sprintf("pagination-plan-%02d", n), RepresentativeID: "owner", Name: fmt.Sprintf("Plan %02d", n), PlanKind: "volume", DurationDays: 30, IPCount: 2, VolumeBytes: 20 << 30, PriceToman: 10000}
		if _, err = f.store.CreateCatalogProduct(ctx, business.CatalogCreate{CatalogSpec: spec, EventID: fmt.Sprintf("tg-pagination-create-%02d-0001", n), Reason: "Synthetic pagination coverage"}); err != nil {
			t.Fatal(err)
		}
		if _, err = f.store.UpdateCatalogProduct(ctx, business.CatalogUpdate{CatalogSpec: spec, EventID: fmt.Sprintf("tg-pagination-publish-%02d-0001", n), ExpectedRevision: 1, Status: "published", Reason: "Synthetic pagination coverage"}); err != nil {
			t.Fatal(err)
		}
		if _, err = f.store.SetTelegramProductPrice(ctx, actor(f.manager), business.TelegramProductPrice{EventID: fmt.Sprintf("tg-pagination-price-%02d-0001", n), ProductID: spec.ID, PriceStars: 30}); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []struct {
		actor  Identity
		action Action
		item   string
	}{{f.manager, Action{Name: "prices"}, "price"}, {f.buyer, Action{Name: "renew", Resource: receipt.Client.ID}, "product"}} {
		seen := map[string]bool{}
		action := scenario.action
		for page := 0; page < 5; page++ {
			out, err := f.backend.Handle(ctx, f.request(t, scenario.actor, action))
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range out.Screen.Buttons {
				for _, button := range row {
					if button.Action != nil && button.Action.Name == scenario.item {
						seen[button.Action.Resource] = true
						if scenario.item == "product" && button.Action.Value != receipt.Client.ID {
							t.Fatal("renewal lost owned client on later page")
						}
					}
				}
			}
			next := actionWithText(out.Screen, "Next")
			if next == nil {
				break
			}
			action = *next
		}
		if len(seen) != 22 {
			t.Fatal("catalog entries hidden by first-page limit", scenario.action.Name, len(seen))
		}
	}
}

func TestPublishedProductPriceAcceptsPersianDigitsAfterConfirmation(t *testing.T) {
	f := businessSetup(t)
	r := f.request(t, f.manager, Action{Name: "price_input", Resource: f.product.ID, Value: "0"})
	r.Incoming.Kind, r.Incoming.Text = "message", "۳۵"
	out, err := f.backend.Handle(context.Background(), r)
	if err != nil || actionNamed(out.Screen, "price_set") == nil {
		t.Fatal("localized price rejected", err)
	}
	if _, err = f.backend.Handle(context.Background(), f.request(t, f.manager, *actionNamed(out.Screen, "price_set"))); err != nil {
		t.Fatal(err)
	}
	products, err := f.store.ListTelegramProducts(context.Background(), actor(f.buyer))
	if err != nil || len(products) != 1 || products[0].PriceStars != 35 {
		t.Fatal("confirmed localized price not saved exactly", products, err)
	}
}

func TestBusinessQRDeliveryKeepsOriginalProofAndFallsBackWithoutTruncatingRoutes(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	client := "qr-private-client"
	content := "vless://12345678-1234-1234-1234-123456789abc@lab.example.invalid:443?security=tls&type=tcp#synthetic\n"
	expires := time.Now().Add(time.Minute).UnixNano()
	acknowledged := false
	f.backend.Delivery = func(_ context.Context, a business.TelegramActor, id string) (business.TelegramDelivery, error) {
		if a != actor(f.buyer) || id != client {
			return business.TelegramDelivery{}, business.ErrTelegramDenied
		}
		sum := sha256.Sum256([]byte(content))
		return business.TelegramDelivery{ClientID: client, Content: content, ContentType: "text/plain", SHA256: hex.EncodeToString(sum[:]), TermsSHA256: strings.Repeat("a", 64), Generation: 1, ExpiresUnixNano: expires}, nil
	}
	f.backend.DeliveryAck = func(context.Context, business.TelegramActor, DeliveryProof, int64) error {
		acknowledged = true
		return nil
	}
	out, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "delivery_qr", Resource: client}))
	if err != nil || out.Document == nil || out.Document.ContentType != "image/png" || out.Delivery == nil || out.Document.SourceSHA256 != out.Delivery.SHA256 || out.Document.ContentSHA256 == out.Delivery.SHA256 || acknowledged {
		t.Fatal("QR was not bound to original unacknowledged configuration", err)
	}
	content += "trojan://synthetic-password@second.example.invalid:443?security=tls#second\n"
	out, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "delivery_qr", Resource: client}))
	sum := sha256.Sum256([]byte(content))
	if err != nil || out.Document == nil || out.Document.Name != "dark-service.txt" || string(out.Document.Content) != content || out.Delivery == nil || out.Delivery.SHA256 != hex.EncodeToString(sum[:]) || acknowledged {
		t.Fatal("multi-route fallback discarded a route or faked delivery", err)
	}
	expires = time.Now().Add(-time.Second).UnixNano()
	out, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "delivery_qr", Resource: client}))
	if !errors.Is(err, ErrProtocol) || out.Document != nil || out.Delivery != nil || acknowledged {
		t.Fatal("expired QR proof became a fallback send", err)
	}
	other := Identity{100, 3001, 3001}
	if _, err = f.backend.Resolve(ctx, other, true); err != nil {
		t.Fatal(err)
	}
	out, err = f.backend.Handle(ctx, f.request(t, other, Action{Name: "delivery_qr", Resource: client}))
	if err != nil || out.Document != nil || out.Delivery != nil || strings.Contains(out.Screen.Text, "vless://") {
		t.Fatal("private QR escaped failed ownership check", err)
	}
}
