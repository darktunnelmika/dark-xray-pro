package telegrambot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// BusinessBackend never owns a wallet, user database or bot token. Every
// operation delegates to the same native business Store used by the HUB.
type BusinessBackend struct {
	store            *business.Store
	SupportEnabled   bool
	CampaignsEnabled bool
	RewardsEnabled   bool
	BonusEnabled     bool
	Delivery         func(context.Context, business.TelegramActor, string) (business.TelegramDelivery, error)
	DeliveryAck      func(context.Context, business.TelegramActor, DeliveryProof, int64) error
}

func NewBusinessBackend(store *business.Store) *BusinessBackend {
	return &BusinessBackend{store: store}
}
func actor(id Identity) business.TelegramActor {
	return business.TelegramActor{BotID: id.BotID, UserID: id.UserID, ChatID: id.ChatID}
}
func accessFrom(id Identity, a business.TelegramAccess) Access {
	return Access{Identity: id, StoreID: a.Store.ID, BrandName: a.Store.Name, StoreRevision: a.Store.Revision, RepresentativeID: a.Store.RepresentativeID, CustomerID: a.CustomerID, Role: a.Role, Permissions: append([]string(nil), a.Permissions...)}
}

func (b *BusinessBackend) Resolve(ctx context.Context, id Identity, start bool) (Access, error) {
	if b == nil || b.store == nil || id.BotID <= 0 || id.UserID <= 0 || id.ChatID != id.UserID {
		return Access{}, ErrAccess
	}
	var a business.TelegramAccess
	var err error
	if start {
		a, err = b.store.TelegramStart(ctx, actor(id))
	} else {
		a, err = b.store.ResolveTelegramActor(ctx, actor(id))
	}
	if err != nil || !a.Store.Enabled || a.Store.BotID != id.BotID || a.UserID != id.UserID {
		return Access{}, ErrAccess
	}
	out := accessFrom(id, a)
	if !out.valid() {
		return Access{}, ErrAccess
	}
	return out, nil
}

func (b *BusinessBackend) Handle(ctx context.Context, r Request) (Result, error) {
	if (r.Incoming.Kind == "payment" || r.Incoming.Kind == "refund") && !r.financialCaptured {
		if err := b.CaptureFinancialEvent(ctx, r.Incoming); err != nil {
			return Result{}, err
		}
	}
	current, err := b.Resolve(ctx, r.Incoming.Identity, false)
	if err != nil || current.digest() != r.Access.digest() {
		return Result{}, ErrAccess
	}
	r.Access = current
	lang := language(r.Language)
	a := actor(current.Identity)
	if r.Incoming.Kind == "pre_checkout" {
		p := r.Incoming.Payment
		if p == nil {
			return Result{}, ErrProtocol
		}
		order, err := b.store.CheckTelegramPreCheckout(ctx, a, p.Payload, p.Currency, p.Amount)
		if err != nil || order.BotID != a.BotID || order.UserID != a.UserID || order.ID != p.Payload || order.Currency != "XTR" || order.AmountStars != p.Amount {
			return Result{Checkout: &CheckoutDecision{ErrorMessage: tr(lang, "سفارش یا قیمت آن معتبر نیست؛ دوباره از منوی خرید اقدام کنید.", "This order or price is no longer valid. Open the purchase menu again.")}}, nil
		}
		return Result{Checkout: &CheckoutDecision{OK: true}}, nil
	}
	if r.Incoming.Kind == "payment" {
		p := r.Incoming.Payment
		if p == nil {
			return Result{}, ErrProtocol
		}
		fulfilled, err := b.store.FulfillTelegramOrder(ctx, a, p.Payload)
		if err != nil {
			return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "✅ پرداخت ثبت شد.\nسفارش برای بررسی و تکمیل نگه‌داری شده است؛ دوباره پرداخت نکنید.", "✅ Payment recorded.\nYour order is retained for review and fulfillment. Do not pay again.") + "\n<code>" + escaped(p.Payload) + "</code>", Buttons: [][]Button{{homeButton(lang)}}}}, nil
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "✅ پرداخت و ثبت سفارش انجام شد.\nآماده‌بودن اتصال در صفحهٔ سرویس جداگانه بررسی می‌شود.", "✅ Payment and order recorded.\nConnection readiness is checked separately on the service page.") + "\n<code>" + escaped(fulfilled.Client.ID) + "</code>", Buttons: [][]Button{{actionButton(tr(lang, "مشاهدهٔ سرویس", "View service"), "primary", "service", fulfilled.Client.ID, "")}, {homeButton(lang)}}}}, nil
	}
	if r.Incoming.Kind == "refund" {
		p := r.Incoming.Payment
		if p == nil {
			return Result{}, ErrProtocol
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "بازپرداخت تأییدشدهٔ تلگرام ثبت شد. وضعیت سرویس از صفحهٔ سرویس قابل بررسی است.", "The verified Telegram refund was recorded. Check the service page for its current state.") + "\n<code>" + escaped(p.Payload) + "</code>", Buttons: [][]Button{{homeButton(lang)}}}}, nil
	}
	name := r.Action.Name
	if r.Incoming.Kind == "message" && name == "" {
		cmd, args := command(r.Incoming.Text)
		switch cmd {
		case "/start", "/menu", "/cancel":
			if cmd == "/start" && len(args) == 1 && strings.HasPrefix(args[0], "invite_") {
				return b.acceptInvitation(ctx, r, strings.TrimPrefix(args[0], "invite_"))
			}
			if cmd == "/start" && len(args) == 1 && strings.HasPrefix(args[0], "ref_") && b.RewardsEnabled {
				if _, err := b.store.TelegramAttributeReferralV1(ctx, a, args[0]); err != nil {
					return oneScreen(tr(lang, "این دعوت برای حساب شما قابل ثبت نیست؛ می‌توانید از منوی اصلی ادامه دهید.", "This referral cannot be applied to your account. Continue from the main menu."), [][]Button{{homeButton(lang)}}), nil
				}
			}
			name = "home"
		case "/help", "/terms":
			name = "help"
		case "/support", "/paysupport":
			name = "tickets"
		case "/accept":
			if len(args) != 1 {
				return Result{}, ErrProtocol
			}
			return b.acceptInvitation(ctx, r, args[0])
		default:
			name = "home"
		}
	}
	switch name {
	case "home":
		screen := homeScreen(current, lang, b.SupportEnabled, b.CampaignsEnabled, b.RewardsEnabled)
		if policy, err := b.store.GetTelegramTrialPolicy(ctx, a); err == nil && policy.Enabled {
			row := []Button{actionButton(tr(lang, "🧪 تست ۲۴ ساعته", "🧪 24-hour trial"), "", "trial", "", "")}
			screen.Buttons = append(screen.Buttons[:2], append([][]Button{row}, screen.Buttons[2:]...)...)
		}
		return Result{ClearInput: true, Screen: screen}, nil
	case "trial":
		policy, err := b.store.GetTelegramTrialPolicy(ctx, a)
		if err != nil || !policy.Enabled {
			return Result{}, ErrAccess
		}
		return oneScreen(fmt.Sprintf(tr(lang, "🧪 تست رایگان: %s · یک اتصال\n۲۴ ساعت از زمان صدور؛ آماده‌شدن اتصال جداگانه بررسی می‌شود.", "🧪 Free trial: %s · one connection\n24 hours from issuance; connection readiness is verified separately."), formatBytes(lang, policy.VolumeBytes)), [][]Button{{actionButton(tr(lang, "دریافت تست", "Claim trial"), "success", "trial_claim", "", "")}, {homeButton(lang)}}), nil
	case "trial_claim":
		service, err := b.store.ClaimTelegramTrial(ctx, a, r.EventID)
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "تست ثبت شد. زمان ۲۴ ساعته از صدور آغاز شده است.", "Trial registered. Its 24-hour period starts at issuance."), Buttons: [][]Button{{actionButton(tr(lang, "مشاهدهٔ سرویس تست", "View trial service"), "primary", "service", service.Client.ID, "")}, {homeButton(lang)}}}}, nil
	case "manage":
		if current.Role == "customer" {
			return Result{}, ErrAccess
		}
		return Result{ClearInput: true, Screen: managementScreen(current, lang, b.SupportEnabled, b.CampaignsEnabled)}, nil
	case "settings", "trial_policy", "trial_policy_input", "trial_policy_confirm", "referral_policy", "referral_policy_input", "referral_policy_confirm", "bonus_policy", "bonus_policy_preview", "bonus_policy_confirm":
		return b.settings(ctx, r, name)
	case "products", "product", "buy", "buy_bonus", "renew", "renew_list", "services", "service":
		return b.commerceScreen(ctx, r, name)
	case "orders", "order", "order_retry", "order_fulfill", "order_service", "bonus_cancel_preview", "bonus_cancel":
		return b.orders(ctx, r, name)
	case "wallet":
		return b.wallet(ctx, r)
	case "help", "help_device", "help_payment", "help_trial":
		return b.help(r, name)
	case "delivery", "delivery_qr":
		return b.deliver(ctx, r)
	case "representatives", "representative_new", "representative_input", "representative_create", "credit_prompt", "credit_input", "credit_confirm", "rep_report", "credits", "prices", "price", "price_input", "price_set", "admins", "admin", "admin_invite", "invite_prompt", "invite_input", "admin_disable", "admin_disable_confirm":
		return b.management(ctx, r, name)
	case "tickets", "ticket", "ticket_new", "ticket_input", "ticket_reply", "reply_input", "ticket_close", "ticket_reopen", "ticket_claim", "ticket_unassign", "support_queue", "representative_apply", "representative_application_input":
		if !b.SupportEnabled {
			return Result{}, ErrAccess
		}
		return b.support(ctx, r, name)
	case "consent", "consent_set", "campaigns", "campaign", "campaign_new", "campaign_prompt", "campaign_input", "campaign_preview", "campaign_approve", "campaign_pause", "campaign_resume_preview", "campaign_resume", "campaign_cancel":
		if !b.CampaignsEnabled {
			return Result{}, ErrAccess
		}
		return b.campaigns(ctx, r, name)
	case "referral", "rewards":
		if !b.RewardsEnabled {
			return Result{}, ErrAccess
		}
		return b.rewards(ctx, r, name)
	default:
		return Result{}, ErrProtocol
	}
}

func (b *BusinessBackend) commerceScreen(ctx context.Context, r Request, name string) (Result, error) {
	lang := r.Language
	a := actor(r.Access.Identity)
	if name == "services" || name == "renew_list" || name == "service" {
		services, err := b.store.ListTelegramServices(ctx, a)
		if err != nil {
			return Result{}, err
		}
		if name == "service" {
			for _, s := range services {
				if s.Client.ID == r.Action.Resource {
					text := "💎 <b>" + escaped(s.Client.Name) + "</b>\n" + tr(lang, "وضعیت: ", "State: ") + escaped(serviceState(lang, s.State)) + "\n" + tr(lang, "تعداد اتصال در مشخصات طرح: ", "Connections in plan terms: ") + strconv.FormatInt(s.Client.IPCount, 10)
					if s.UsageAvailable && s.Usage.Known {
						text += "\n" + tr(lang, "مصرف ثبت‌شده: ", "Recorded usage: ") + formatBytes(lang, s.Usage.Total)
					} else {
						text += "\n" + tr(lang, "مصرف: در حال حاضر نامشخص", "Usage: currently unknown")
					}
					if s.Client.ExpiresUnix > 0 {
						text += "\n" + tr(lang, "انقضا: ", "Expires: ") + time.Unix(s.Client.ExpiresUnix, 0).UTC().Format("2006-01-02 15:04 UTC")
					} else {
						text += "\n" + tr(lang, "تاریخ انقضا هنوز تعیین نشده است.", "An expiry timestamp has not been assigned yet.")
					}
					rows := [][]Button{{actionButton(tr(lang, "🔄 تمدید", "🔄 Renew"), "success", "renew", s.Client.ID, "")}}
					if b.Delivery != nil && b.DeliveryAck != nil {
						rows = append(rows, []Button{actionButton(tr(lang, "📥 دریافت تنظیمات اتصال", "📥 Get connection settings"), "primary", "delivery", s.Client.ID, "")})
						rows = append(rows, []Button{actionButton(tr(lang, "🔳 دریافت QR اتصال", "🔳 Get connection QR"), "", "delivery_qr", s.Client.ID, "")})
					}
					if b.SupportEnabled {
						rows = append(rows, []Button{actionButton(tr(lang, "💬 پشتیبانی این سرویس", "💬 Service support"), "", "ticket_new", s.Client.ID, "")})
					}
					rows = append(rows, []Button{backButton(lang, "services", "", ""), homeButton(lang)})
					return oneScreen(text, rows), nil
				}
			}
			return Result{}, ErrAccess
		}
		text := tr(lang, "💎 <b>سرویس‌های من</b>", "💎 <b>My services</b>")
		if len(services) == 0 {
			text += "\n\n" + tr(lang, "هنوز سرویسی برای این حساب ثبت نشده است.", "No services have been registered for this account.")
		}
		rows := [][]Button{}
		offset := min(parsePage(r.Action.Value), len(services))
		for _, s := range services[offset:min(offset+8, len(services))] {
			action := "service"
			if name == "renew_list" {
				action = "renew"
			}
			rows = append(rows, []Button{actionButton(clipped(s.Client.Name, 70), "", action, s.Client.ID, "")})
		}
		if page := pageButtons(lang, name, "", offset, len(services)); len(page) > 0 {
			rows = append(rows, page)
		}
		rows = append(rows, []Button{homeButton(lang)})
		return oneScreen(text, rows), nil
	}
	products, err := b.store.ListTelegramProducts(ctx, a)
	if err != nil {
		return Result{}, err
	}
	if name == "buy" || name == "buy_bonus" {
		request := business.TelegramCheckoutRequest{EventID: r.EventID, ProductID: r.Action.Resource, ClientID: r.Action.Value}
		if name == "buy_bonus" {
			var selected bonusPurchase
			if !b.BonusEnabled || len(r.Action.Value) > 1024 || decodeStrictJSON([]byte(r.Action.Value), &selected) != nil || selected.BonusStars < 1 {
				return Result{}, ErrProtocol
			}
			request.ClientID, request.BonusStars = selected.ClientID, selected.BonusStars
		}
		order, err := b.store.PrepareTelegramCheckout(ctx, a, request)
		if err != nil {
			return Result{}, err
		}
		if order.BotID != a.BotID || order.UserID != a.UserID || order.Currency != "XTR" || order.Status != "quoted" {
			return Result{}, ErrAccess
		}
		return Result{Invoice: &Invoice{Title: clipped(order.Product.Name, 32), Description: clipped(invoiceTerms(lang, order), 255), Payload: order.ID, Currency: "XTR", Amount: order.AmountStars}, Receipt: true, ClearInput: true}, nil
	}
	if name == "product" {
		if r.Action.Value != "" {
			products, err = b.compatibleRenewals(ctx, a, r.Action.Value, products)
			if err != nil {
				return Result{}, err
			}
		}
		for _, p := range products {
			if p.Product.ID == r.Action.Resource {
				rows := [][]Button{{actionButton(tr(lang, "✅ ادامه به فاکتور", "✅ Continue to invoice"), "success", "buy", p.Product.ID, r.Action.Value)}}
				text := "<b>" + escaped(p.Product.Name) + "</b>\n\n" + escaped(productTerms(lang, p))
				if row, explanation := b.bonusProductChoice(ctx, r, p); len(row) > 0 {
					rows = append(rows, row)
					text += "\n\n" + explanation
				}
				rows = append(rows, []Button{backButton(lang, "products", "", ""), homeButton(lang)})
				return oneScreen(text, rows), nil
			}
		}
		return Result{}, ErrAccess
	}
	client := ""
	if name == "renew" {
		client = r.Action.Resource
		products, err = b.compatibleRenewals(ctx, a, client, products)
		if err != nil {
			return Result{}, err
		}
	}
	text := tr(lang, "🛒 <b>انتخاب سرویس</b>", "🛒 <b>Choose a service</b>")
	if len(products) == 0 {
		text += "\n\n" + tr(lang, "در حال حاضر محصول قابل فروش وجود ندارد.", "No published products are currently available.")
	}
	rows := [][]Button{}
	offset := min(parsePage(r.Action.Value), len(products))
	for _, p := range products[offset:min(offset+8, len(products))] {
		rows = append(rows, []Button{actionButton(clipped(fmt.Sprintf("%s · %d ⭐", p.Product.Name, p.PriceStars), 90), "success", "product", p.Product.ID, client)})
	}
	pageAction := "products"
	if client != "" {
		pageAction = "renew"
	}
	if page := pageButtons(lang, pageAction, client, offset, len(products)); len(page) > 0 {
		rows = append(rows, page)
	}
	rows = append(rows, []Button{homeButton(lang)})
	return oneScreen(text, rows), nil
}

func productTerms(lang string, p business.TelegramProduct) string {
	volume := formatBytes(lang, p.Product.VolumeBytes)
	if p.Product.PlanKind == "unlimited" {
		volume = tr(lang, "نامحدود", "Unlimited")
	}
	return fmt.Sprintf(tr(lang, "%s\nمدت: %d روز · اتصال در مشخصات طرح: %d\nمبلغ: %d ⭐ Telegram Stars", "%s\nDuration: %d days · Connections in plan terms: %d\nPrice: %d ⭐ Telegram Stars"), volume, p.Product.DurationDays, p.Product.IPCount, p.PriceStars)
}
func serviceState(lang, state string) string {
	switch state {
	case "pending_node_apply":
		return tr(lang, "در انتظار اعمال روی نود", "Awaiting node application")
	case "active":
		return tr(lang, "ثبت فعال؛ آماده‌بودن اتصال هنگام دریافت بررسی می‌شود", "Active record; connection readiness is checked on delivery")
	case "expired":
		return tr(lang, "منقضی", "Expired")
	case "exhausted", "quota_exhausted":
		return tr(lang, "حجم تمام شده", "Quota exhausted")
	case "disabled":
		return tr(lang, "غیرفعال", "Disabled")
	default:
		return tr(lang, "نیازمند بررسی وضعیت", "Status requires verification")
	}
}

func (b *BusinessBackend) deliver(ctx context.Context, r Request) (Result, error) {
	if b.Delivery == nil || b.DeliveryAck == nil {
		return Result{}, ErrAccess
	}
	d, err := b.Delivery(ctx, actor(r.Access.Identity), r.Action.Resource)
	if err != nil {
		return oneScreen(tr(r.Language, "تنظیمات اتصال هنوز با وضعیت فعلی نود تأیید نشده است. پس از آماده‌شدن نود دوباره بررسی کنید.", "Connection settings are not verified against current node state yet. Check again when the node is ready."), [][]Button{{backButton(r.Language, "service", r.Action.Resource, "")}, {homeButton(r.Language)}}), nil
	}
	if d.ClientID != r.Action.Resource || !hexDigest(d.SHA256) || !hexDigest(d.TermsSHA256) || d.ExpiresUnixNano <= time.Now().UnixNano() || len(d.Content) < 1 || len(d.Content) > 32768 || !utf8.ValidString(d.Content) {
		return Result{}, ErrProtocol
	}
	sum := sha256.Sum256([]byte(d.Content))
	if hex.EncodeToString(sum[:]) != d.SHA256 {
		return Result{}, ErrProtocol
	}
	proof := &DeliveryProof{ClientID: d.ClientID, SHA256: d.SHA256, TermsSHA256: d.TermsSHA256, ExpiresUnixNano: d.ExpiresUnixNano}
	caption := tr(r.Language, "تنظیمات اختصاصی سرویس شما", "Your private service configuration")
	if r.Action.Name == "delivery_qr" {
		document, err := prepareQRDelivery(d.Content, *proof, escaped(caption))
		if err == nil {
			return Result{Receipt: true, Delivery: proof, Document: document}, nil
		}
		if !errors.Is(err, ErrQRUnavailable) {
			return Result{}, err
		}
		caption = tr(r.Language, "برای این تنظیمات QR تکی در دسترس نیست. فایل اصلی اتصال را در برنامهٔ سازگار وارد کنید.", "A single QR is not available for these settings. Import this original configuration file into a compatible app.")
		return Result{Receipt: true, Delivery: proof, Document: &Document{Name: "dark-service.txt", Content: []byte(d.Content), Caption: escaped(caption)}}, nil
	}
	if len(escaped(d.Content)) < 3000 {
		rows := [][]Button{}
		if utf8.RuneCountInString(d.Content) <= 256 {
			rows = append(rows, []Button{{Text: tr(r.Language, "📋 کپی تنظیمات", "📋 Copy configuration"), CopyText: d.Content}})
		}
		rows = append(rows, []Button{backButton(r.Language, "service", d.ClientID, "")})
		return Result{Receipt: true, Delivery: proof, Screen: &Screen{Text: escaped(caption) + "\n<code>" + escaped(d.Content) + "</code>", Buttons: rows}}, nil
	}
	return Result{Receipt: true, Delivery: proof, Document: &Document{Name: "dark-service.txt", Content: []byte(d.Content), Caption: escaped(caption)}}, nil
}

func (b *BusinessBackend) AcknowledgeDelivery(ctx context.Context, id Identity, p DeliveryProof, messageID int64) error {
	if b.DeliveryAck == nil || messageID <= 0 {
		return ErrAccess
	}
	return b.DeliveryAck(ctx, actor(id), p, messageID)
}

func (b *BusinessBackend) acceptInvitation(ctx context.Context, r Request, id string) (Result, error) {
	a, err := b.store.AcceptTelegramAdmin(ctx, actor(r.Access.Identity), id)
	if err != nil {
		return Result{}, err
	}
	current := accessFrom(r.Access.Identity, a)
	return Result{Access: &current, ClearInput: true, Receipt: true, Screen: homeScreen(current, r.Language, b.SupportEnabled, b.CampaignsEnabled, b.RewardsEnabled)}, nil
}

var _ Backend = (*BusinessBackend)(nil)
var _ deliveryAcknowledger = (*BusinessBackend)(nil)

func (b *BusinessBackend) CaptureFinancialEvent(ctx context.Context, in Incoming) error {
	if b == nil || b.store == nil || in.Identity.BotID <= 0 || in.Identity.UserID <= 0 || in.Identity.ChatID != in.Identity.UserID || in.Payment == nil {
		return ErrProtocol
	}
	p := in.Payment
	if p.Currency != "XTR" || p.Amount <= 0 || p.Amount > 1_000_000_000 || !validOpaque(p.Payload, 128) || !validOpaque(p.TelegramChargeID, 512) {
		return ErrProtocol
	}
	payment := business.TelegramPayment{Payload: p.Payload, Currency: p.Currency, TotalAmount: p.Amount, TelegramChargeID: p.TelegramChargeID}
	switch in.Kind {
	case "payment":
		_, err := b.store.RecordTelegramPayment(ctx, actor(in.Identity), payment)
		return classifyCaptureError(err)
	case "refund":
		_, err := b.store.RecordTelegramRefund(ctx, actor(in.Identity), payment)
		return classifyCaptureError(err)
	default:
		return ErrProtocol
	}
}

func classifyCaptureError(err error) error {
	if errors.Is(err, business.ErrTelegramPayment) || errors.Is(err, business.ErrTelegramDenied) || errors.Is(err, business.ErrInvalid) || errors.Is(err, business.ErrNotFound) || errors.Is(err, business.ErrConflict) {
		return ErrFinancialMismatch
	}
	return err
}

func (b *BusinessBackend) ValidateFinancialDispatch(ctx context.Context, id Identity, p Payment) error {
	if b == nil || b.store == nil || id.ChatID != id.UserID || !validPayment(p, false) {
		return ErrAccess
	}
	o, err := b.store.CheckTelegramPreCheckout(ctx, actor(id), p.Payload, p.Currency, p.Amount)
	if err != nil || o.BotID != id.BotID || o.UserID != id.UserID || o.ID != p.Payload || o.Currency != p.Currency || o.AmountStars != p.Amount {
		return ErrAccess
	}
	return nil
}

func (b *BusinessBackend) compatibleRenewals(ctx context.Context, a business.TelegramActor, clientID string, products []business.TelegramProduct) ([]business.TelegramProduct, error) {
	services, err := b.store.ListTelegramServices(ctx, a)
	if err != nil {
		return nil, err
	}
	for _, s := range services {
		if s.Client.ID == clientID {
			out := []business.TelegramProduct{}
			for _, p := range products {
				if p.Product.PlanKind == s.Client.PlanKind && p.Product.IPCount == s.Client.IPCount {
					out = append(out, p)
				}
			}
			return out, nil
		}
	}
	return nil, ErrAccess
}

func formatBytes(lang string, n int64) string {
	for _, unit := range []struct {
		name string
		size int64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}} {
		if n >= unit.size {
			if n%unit.size == 0 {
				return fmt.Sprintf("%d %s", n/unit.size, unit.name)
			}
			return fmt.Sprintf(tr(lang, "%.2f %s (%d بایت)", "%.2f %s (%d bytes)"), float64(n)/float64(unit.size), unit.name, n)
		}
	}
	return fmt.Sprintf(tr(lang, "%d بایت", "%d bytes"), n)
}
