package telegrambot

import (
	"context"
	"fmt"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

type bonusPurchase struct {
	ClientID   string `json:"client_id,omitempty"`
	BonusStars int64  `json:"bonus_stars"`
}

func (b *BusinessBackend) wallet(ctx context.Context, r Request) (Result, error) {
	if b.BonusEnabled {
		if _, err := b.store.ReleaseExpiredTelegramBonusQuotesAsV1(ctx, actor(r.Access.Identity), 200); err != nil {
			return Result{}, err
		}
	}
	w, err := b.store.TelegramWallet(ctx, actor(r.Access.Identity))
	if err != nil {
		return Result{}, err
	}
	lang := r.Language
	text := fmt.Sprintf(tr(lang, "💳 <b>کیف پاداش فروشگاه</b>\nاعتبار پرداختی ثبت‌شده: %d XTR\nپاداش آزاد: %d\nپاداش رزروشده برای فاکتورها: %d\nواحد ارزش پاداش: XTR", "💳 <b>Store reward wallet</b>\nRecorded paid credit: %d XTR\nAvailable bonus: %d\nBonus reserved for invoices: %d\nBonus value unit: XTR"), w.PaidStars, w.AvailableBonusStars, w.ReservedBonusStars)
	if w.BonusStars < 0 {
		text += fmt.Sprintf(tr(lang, "\nکسری پاداش پس از بازپرداخت یا ثبت دیرهنگام: %d\nپاداش‌های بعدی ابتدا این کسری را جبران می‌کنند.", "\nBonus deficit after a refund or late payment capture: %d\nFuture rewards offset this deficit first."), -w.BonusStars)
	}
	rows := [][]Button{}
	if b.BonusEnabled && w.CheckoutRedemptionEnabled {
		text += "\n\n" + tr(lang, "استفاده از پاداش در خرید فعال است. در صفحهٔ محصول، انتخاب «استفاده از پاداش» بخشی از قیمت را کم می‌کند؛ حداقل ۱ ستاره برای پرداخت باقی می‌ماند. رزرو فاکتور پرداخت‌نشده پس از انقضا یا لغو آزاد می‌شود.", "Bonus redemption is enabled. On the product page, choose Use bonus to reduce the price; at least 1 Star remains payable. An unpaid invoice's reservation is released after expiry or cancellation.")
		rows = append(rows, []Button{actionButton(tr(lang, "🛒 خرید با انتخاب پاداش", "🛒 Shop with optional bonus"), "success", "products", "", "")})
	} else {
		text += "\n\n" + tr(lang, "استفاده از پاداش در خرید برای این فروشگاه فعال نیست. مدیر فروشگاه می‌تواند آن را از تنظیمات فعال کند.", "Bonus redemption at checkout is not enabled for this store. The store manager can enable it in settings.")
	}
	text += "\n\n" + tr(lang, "این دفتر، موجودی شخصی Telegram Stars شما نیست. شارژ کیف پول و برداشت وجه در این نسخه فعال نیست.", "This ledger is not your personal Telegram Stars balance. Wallet funding and cash withdrawal are not enabled in this version.")
	rows = append(rows, []Button{actionButton(tr(lang, "🧾 سفارش‌ها و رزروها", "🧾 Orders and reservations"), "", "orders", "", "")})
	if b.RewardsEnabled {
		rows = append(rows, []Button{actionButton(tr(lang, "🎁 تاریخچهٔ پاداش", "🎁 Reward history"), "", "rewards", "", "")})
	}
	rows = append(rows, []Button{homeButton(lang)})
	return oneScreen(text, rows), nil
}

func (b *BusinessBackend) bonusProductChoice(ctx context.Context, r Request, p business.TelegramProduct) ([]Button, string) {
	if !b.BonusEnabled || p.PriceStars <= 1 {
		return nil, ""
	}
	if _, err := b.store.ReleaseExpiredTelegramBonusQuotesAsV1(ctx, actor(r.Access.Identity), 200); err != nil {
		return nil, ""
	}
	wallet, err := b.store.TelegramWallet(ctx, actor(r.Access.Identity))
	if err != nil || !wallet.CheckoutRedemptionEnabled || wallet.AvailableBonusStars < 1 {
		return nil, ""
	}
	discount := min(wallet.AvailableBonusStars, p.PriceStars-1)
	net := p.PriceStars - discount
	choice := bonusPurchase{ClientID: r.Action.Value, BonusStars: discount}
	label := fmt.Sprintf(tr(r.Language, "🎁 استفاده از %d پاداش · پرداخت %d ⭐", "🎁 Use %d bonus · pay %d ⭐"), discount, net)
	explanation := fmt.Sprintf(tr(r.Language, "انتخاب اختیاری پاداش: %d از قیمت کم می‌شود؛ پرداخت نهایی %d ⭐ است. پاداش با ساخت فاکتور رزرو می‌شود.", "Optional bonus: reduce the price by %d and pay %d ⭐. Bonus is reserved when the invoice is created."), discount, net)
	return []Button{actionButton(label, "primary", "buy_bonus", p.Product.ID, settingsValue(choice))}, explanation
}

func bonusPolicyText(lang string, enabled bool) string {
	state := tr(lang, "خاموش", "Off")
	if enabled {
		state = tr(lang, "روشن", "On")
	}
	text := fmt.Sprintf(tr(lang, "💳 <b>تخفیف با پاداش</b>\nوضعیت: %s\n\nمشتری با انتخاب خودش می‌تواند پاداش آزاد فروشگاه را از قیمت خرید یا تمدید کم کند. حداقل ۱ ستاره برای پرداخت باقی می‌ماند. پاداش هنگام ساخت فاکتور رزرو و پس از انقضا یا لغو فاکتور پرداخت‌نشده آزاد می‌شود.\n\nبازپرداخت تأییدشدهٔ تلگرام، پاداش مصرف‌شدهٔ همان سفارش را برمی‌گرداند. پاداش دعوت سفارش تخفیف‌دار بر پایهٔ مبلغ واقعی پرداختی و حداکثر تا همان مبلغ محاسبه می‌شود. شارژ کیف پول و برداشت نقدی فعال نیست.", "💳 <b>Bonus discount policy</b>\nState: %s\n\nCustomers may explicitly use available store bonus to reduce a purchase or renewal price. At least 1 Star remains payable. Bonus is reserved when an invoice is created and released after an unpaid invoice expires or is cancelled.\n\nA verified Telegram refund restores that order's consumed bonus. Referral rewards for discounted orders use the actual amount paid and cannot exceed it. Wallet funding and cash withdrawal are not enabled."), state)
	return text + "\n\n" + tr(lang, "غیرفعال‌کردن این گزینه، ساخت فاکتور پاداش جدید را می‌بندد؛ فاکتورهای معتبر قبلی شرایط خود را حفظ می‌کنند.", "Disabling this setting prevents new bonus invoices. Existing valid reserved invoices retain their terms.")
}

func invoiceTerms(lang string, order business.TelegramOrder) string {
	terms := productTerms(lang, business.TelegramProduct{Product: order.Product, PriceStars: order.AmountStars})
	if order.BonusDiscountStars == 0 {
		return terms
	}
	return fmt.Sprintf(tr(lang, "قیمت اولیه: %d ⭐\nتخفیف پاداش: %d\nپرداخت نهایی: %d ⭐\n", "Original price: %d ⭐\nBonus discount: %d\nFinal payment: %d ⭐\n"), order.AmountStars+order.BonusDiscountStars, order.BonusDiscountStars, order.AmountStars) + terms
}

func bonusState(lang, state string) string {
	switch state {
	case "none":
		return tr(lang, "بدون پاداش", "No bonus")
	case "reserved":
		return tr(lang, "رزروشده", "Reserved")
	case "released":
		return tr(lang, "آزادشده", "Released")
	case "consumed":
		return tr(lang, "در تخفیف مصرف شده", "Used as discount")
	case "refunded":
		return tr(lang, "پس از بازپرداخت برگشته", "Restored after refund")
	default:
		return tr(lang, "نیازمند بررسی", "Requires review")
	}
}
