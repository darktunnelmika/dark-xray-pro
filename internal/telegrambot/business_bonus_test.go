package telegrambot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestBusinessBonusPolicyRequiresReviewCurrentAuthorityAndRevision(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	view, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "settings"}))
	if err != nil || actionNamed(view.Screen, "bonus_policy") == nil {
		t.Fatal("manager cannot find bonus policy", err)
	}
	view, err = f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "bonus_policy"}))
	if err != nil || view.Screen == nil || !strings.Contains(view.Screen.Text, "State: Off") {
		t.Fatal("default-off policy is hidden", err)
	}
	previewAction := actionNamed(view.Screen, "bonus_policy_preview")
	if previewAction == nil {
		t.Fatal("missing policy preview")
	}
	preview, err := f.backend.Handle(ctx, f.request(t, f.manager, *previewAction))
	if err != nil || preview.Screen == nil || !strings.Contains(preview.Screen.Text, "at least 1 Star") && !strings.Contains(preview.Screen.Text, "At least 1 Star") {
		t.Fatal("review omits the actual Stars payment requirement", err)
	}
	policy, err := f.store.GetTelegramBonusPolicyV1(ctx, actor(f.manager))
	if err != nil || policy.Enabled || policy.Revision != 0 {
		t.Fatal("view or preview mutated financial policy", policy, err)
	}
	confirm := actionNamed(preview.Screen, "bonus_policy_confirm")
	if confirm == nil {
		t.Fatal("missing reviewed confirmation")
	}
	for _, action := range []Action{{Name: "bonus_policy"}, *previewAction, *confirm} {
		if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, action)); !errors.Is(err, ErrAccess) {
			t.Fatal("customer reached finance settings", action.Name, err)
		}
	}
	saved, err := f.backend.Handle(ctx, f.request(t, f.manager, *confirm))
	if err != nil || !saved.Receipt {
		t.Fatal("reviewed policy was not saved", err)
	}
	policy, err = f.store.GetTelegramBonusPolicyV1(ctx, actor(f.manager))
	if err != nil || !policy.Enabled || policy.Revision != 1 {
		t.Fatal("policy confirmation changed intended terms", policy, err)
	}
	stale := f.request(t, f.manager, *confirm)
	stale.EventID = "tg-adapter-bonus-stale-confirm-0001"
	if _, err = f.backend.Handle(ctx, stale); !errors.Is(err, business.ErrConflict) {
		t.Fatal("old policy confirmation overwrote a newer revision", err)
	}
	forged := f.request(t, f.manager, Action{Name: "bonus_policy_confirm", Value: `{"expected_revision":1,"enabled":false,"wallet_stars":900}`})
	if _, err = f.backend.Handle(ctx, forged); !errors.Is(err, ErrProtocol) {
		t.Fatal("unknown financial policy input accepted", err)
	}
	current, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "bonus_policy"}))
	if err != nil {
		t.Fatal(err)
	}
	oldAccess := f.request(t, f.manager, *actionNamed(current.Screen, "bonus_policy_preview"))
	reg := f.registration
	reg.EventID, reg.ExpectedRevision, reg.ManagerUserID = "tg-adapter-bonus-replace-manager", 1, 5001
	if _, err = f.store.ConfigureTelegramStore(ctx, f.ownerToken, reg); err != nil {
		t.Fatal(err)
	}
	if _, err = f.backend.Handle(ctx, oldAccess); !errors.Is(err, ErrAccess) {
		t.Fatal("revoked manager continued an old financial action", err)
	}
}

func TestBusinessBonusSelectionCannotInventCreditOrChangeInvoiceAmount(t *testing.T) {
	f := businessSetup(t)
	f.price(t)
	ctx := context.Background()
	bonus := Action{Name: "buy_bonus", Resource: f.product.ID, Value: `{"bonus_stars":1}`}
	if _, err := f.backend.Handle(ctx, f.request(t, f.buyer, bonus)); err == nil {
		t.Fatal("discount was enabled by a customer action")
	}
	if _, err := f.store.SetTelegramBonusPolicyV1(ctx, actor(f.manager), business.TelegramBonusPolicyChangeV1{EventID: "tg-adapter-bonus-enable-0001", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	product, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "product", Resource: f.product.ID}))
	if err != nil || actionNamed(product.Screen, "buy") == nil || actionNamed(product.Screen, "buy_bonus") != nil {
		t.Fatal("empty wallet offered an imaginary discount or hid normal purchase", err)
	}
	for _, value := range []string{
		`{"bonus_stars":0}`, `{"bonus_stars":-1}`, `{"bonus_stars":25}`, `{"bonus_stars":26}`,
		`{"bonus_stars":1}`, `{"bonus_stars":1,"amount_stars":1}`, `{"bonus_stars":1,"client_id":"foreign-client"}`,
	} {
		bonus.Value = value
		out, err := f.backend.Handle(ctx, f.request(t, f.buyer, bonus))
		if err == nil || out.Invoice != nil {
			t.Fatal("forged bonus produced an invoice", value, err)
		}
	}
	orders, err := f.store.ListTelegramOrders(ctx, actor(f.buyer), 100, 0)
	if err != nil || len(orders) != 0 {
		t.Fatal("failed financial choice persisted an order", len(orders), err)
	}
	wallet, err := f.store.TelegramWallet(ctx, actor(f.buyer))
	if err != nil || wallet.PaidStars != 0 || wallet.BonusStars != 0 || wallet.ReservedBonusStars != 0 {
		t.Fatal("invalid choice modified money", wallet, err)
	}
	plain, err := f.backend.Handle(ctx, f.request(t, f.buyer, *actionNamed(product.Screen, "buy")))
	if err != nil || plain.Invoice == nil || plain.Invoice.Amount != 25 || plain.Invoice.Currency != "XTR" {
		t.Fatal("normal purchase price changed when bonus policy was enabled", err)
	}
	checkout := f.request(t, f.buyer, Action{})
	checkout.Incoming.Kind = "pre_checkout"
	checkout.Incoming.Payment = &Payment{Payload: plain.Invoice.Payload, Currency: "XTR", Amount: 24}
	decision, err := f.backend.Handle(ctx, checkout)
	if err != nil || decision.Checkout == nil || decision.Checkout.OK {
		t.Fatal("lower external payment passed pre-checkout", err)
	}
	checkout.Incoming.Payment.Amount = 25
	decision, err = f.backend.Handle(ctx, checkout)
	if err != nil || decision.Checkout == nil || !decision.Checkout.OK {
		t.Fatal("unchanged real invoice no longer passes pre-checkout", err)
	}
	for _, name := range []string{"bonus_cancel_preview", "bonus_cancel"} {
		if _, err = f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: name, Resource: plain.Invoice.Payload})); !errors.Is(err, ErrAccess) {
			t.Fatal("non-bonus invoice entered bonus cancellation", name, err)
		}
	}
	walletView, err := f.backend.Handle(ctx, f.request(t, f.buyer, Action{Name: "wallet"}))
	if err != nil || walletView.Screen == nil || !strings.Contains(walletView.Screen.Text, "Bonus reserved for invoices: 0") || !strings.Contains(walletView.Screen.Text, "at least 1 Star") || actionNamed(walletView.Screen, "orders") == nil {
		t.Fatal("wallet lost reservation visibility or net-payment terms", err)
	}
	// Bonus cleanup remains available even if referral earning is disabled.
	f.backend.RewardsEnabled = false
	if err = f.backend.Maintain(ctx, f.buyer.BotID); err != nil {
		t.Fatal("bonus maintenance incorrectly depends on referral earning", err)
	}
}
