package telegrambot

import (
	"context"
	"fmt"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func (b *BusinessBackend) orders(ctx context.Context, r Request, name string) (Result, error) {
	a := actor(r.Access.Identity)
	lang := r.Language
	back := backButton(lang, "orders", "", "")
	if name == "orders" {
		offset := parsePage(r.Action.Value)
		orders, err := b.store.ListTelegramOrders(ctx, a, 9, offset)
		if err != nil {
			return Result{}, err
		}
		rows := [][]Button{}
		for _, o := range orders[:min(8, len(orders))] {
			bonusState := ""
			if b.BonusEnabled && o.Status == "quoted" && o.BonusDiscountStars > 0 {
				q, err := b.store.TelegramBonusQuoteV1(ctx, a, o.ID)
				if err != nil {
					return Result{}, err
				}
				bonusState = q.State
			}
			rows = append(rows, []Button{actionButton(clipped(o.Product.Name, 40)+" · "+orderDisplayState(lang, o, bonusState), "", "order", o.ID, "")})
		}
		if page := pageButtons(lang, "orders", "", offset, offset+len(orders)); len(page) > 0 {
			rows = append(rows, page)
		}
		rows = append(rows, []Button{homeButton(lang)})
		text := tr(lang, "🧾 <b>سفارش‌ها و پیگیری پرداخت</b>", "🧾 <b>Orders and payment status</b>")
		if len(orders) == 0 {
			text += "\n\n" + tr(lang, "هنوز سفارشی ثبت نشده است.", "No orders have been registered.")
		}
		return oneScreen(text, rows), nil
	}
	o, err := b.store.TelegramOrderByID(ctx, a, r.Action.Resource)
	if err != nil {
		return Result{}, err
	}
	if name == "bonus_cancel_preview" || name == "bonus_cancel" {
		if !b.BonusEnabled || o.BonusDiscountStars < 1 || o.Status != "quoted" {
			return Result{}, ErrAccess
		}
		q, err := b.store.TelegramBonusQuoteV1(ctx, a, o.ID)
		if err != nil {
			return Result{}, err
		}
		if name == "bonus_cancel_preview" {
			if q.State != "reserved" {
				return oneScreen(tr(lang, "رزرو این فاکتور قبلاً تعیین تکلیف شده است. وضعیت سفارش را تازه‌سازی کنید.", "This invoice's reservation has already been settled. Refresh the order status."), [][]Button{{backButton(lang, "order", o.ID, "")}}), nil
			}
			text := fmt.Sprintf(tr(lang, "رزرو %d پاداش برای این فاکتور آزاد می‌شود و تأیید پرداخت جدید با آن بسته می‌شود.\nاین کار بازپرداخت Telegram Stars نیست؛ اگر پرداخت را آغاز کرده‌اید، پیش از ساخت فاکتور دیگری وضعیت همین سفارش را تازه‌سازی کنید.", "Release %d reserved bonus and block new payment approval for this invoice.\nThis does not refund Telegram Stars. If you already started payment, refresh this order before creating another invoice."), q.DiscountStars)
			return oneScreen(text, [][]Button{{actionButton(tr(lang, "✅ آزادکردن رزرو فاکتور", "✅ Release invoice reservation"), "", "bonus_cancel", o.ID, "")}, {backButton(lang, "order", o.ID, "")}}), nil
		}
		q, err = b.store.CancelTelegramBonusQuoteV1(ctx, a, o.ID, r.EventID)
		if err != nil {
			return Result{}, err
		}
		text := tr(lang, "رزرو پاداش آزاد شد و تأیید پرداخت جدید با این فاکتور بسته است. اگر پرداخت را آغاز کرده‌اید، پیش از ساخت فاکتور دیگری وضعیت همین سفارش را تازه‌سازی کنید.", "Bonus reservation released; new payment approval is blocked for this invoice. If you already started payment, refresh this order before creating another invoice.")
		if q.State == "consumed" || q.State == "refunded" {
			// A replay may observe a financial event committed after the earlier
			// cancellation. Report the current state, not the historical release.
			text = tr(lang, "وضعیت پاداش این سفارش: ", "Current order bonus state: ") + bonusState(lang, q.State) + "\n" + tr(lang, "وضعیت مالی سفارش تغییر کرده است؛ پیش از اقدام دیگری همین سفارش را تازه‌سازی کنید.", "The order's financial state has changed. Refresh this order before taking another action.")
		} else if q.State != "released" {
			return Result{}, ErrProtocol
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: text, Buttons: [][]Button{{actionButton(tr(lang, "تازه‌سازی همین سفارش", "Refresh this order"), "primary", "order", o.ID, "")}, {actionButton(tr(lang, "💳 کیف پاداش", "💳 Reward wallet"), "", "wallet", "", "")}}}}, nil
	}
	if name == "order_service" {
		if o.Status != "paid_pending_provisioning" {
			return Result{}, ErrAccess
		}
		receipt, err := b.store.FulfillTelegramOrder(ctx, a, o.ID)
		if err != nil {
			return Result{}, err
		}
		r.Action = Action{Name: "service", Resource: receipt.Client.ID}
		return b.commerceScreen(ctx, r, "service")
	}
	if name == "order_retry" {
		if o.Status != "paid_pending_fulfillment" {
			return Result{}, ErrAccess
		}
		return oneScreen(tr(lang, "پرداخت این سفارش قبلاً ثبت شده است. تکمیل همان سفارش را دوباره بررسی می‌کنیم؛ پرداخت جدیدی لازم نیست.", "Payment is already recorded. Retry fulfillment of this same order; no new payment is needed."), [][]Button{{actionButton(tr(lang, "✅ تلاش مجدد برای تکمیل", "✅ Retry fulfillment"), "success", "order_fulfill", o.ID, "")}, {backButton(lang, "order", o.ID, "")}}), nil
	}
	if name == "order_fulfill" {
		if o.Status != "paid_pending_fulfillment" && o.Status != "paid_pending_provisioning" {
			return Result{}, ErrAccess
		}
		receipt, err := b.store.FulfillTelegramOrder(ctx, a, o.ID)
		if err != nil {
			return oneScreen(tr(lang, "پرداخت محفوظ است و تکمیل سفارش هنوز نیازمند بررسی است. از پشتیبانی همین سفارش پیگیری کنید؛ دوباره پرداخت نکنید.", "Payment remains recorded and fulfillment still needs review. Contact support about this order; do not pay again."), [][]Button{{backButton(lang, "order", o.ID, "")}}), nil
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "ثبت سرویس انجام شد. آماده‌بودن اتصال هنگام دریافت تنظیمات بررسی می‌شود.", "Service registration is complete. Connection readiness is verified when requesting its configuration."), Buttons: [][]Button{{actionButton(tr(lang, "مشاهدهٔ سرویس", "View service"), "primary", "service", receipt.Client.ID, "")}, {back}}}}, nil
	}
	var bonusQuoteState string
	if b.BonusEnabled && o.BonusDiscountStars > 0 {
		q, err := b.store.TelegramBonusQuoteV1(ctx, a, o.ID)
		if err != nil {
			return Result{}, err
		}
		bonusQuoteState = q.State
	}
	text := "<b>" + escaped(o.Product.Name) + "</b>\n" + tr(lang, "وضعیت: ", "Status: ") + orderDisplayState(lang, o, bonusQuoteState) + "\n" + fmt.Sprintf(tr(lang, "مبلغ سفارش: %d ⭐", "Order amount: %d ⭐"), o.AmountStars) + "\n" + time.Unix(o.CreatedUnix, 0).UTC().Format("2006-01-02 15:04 UTC") + "\n<code>" + escaped(o.ID) + "</code>"
	rows := [][]Button{{actionButton(tr(lang, "تازه‌سازی وضعیت", "Refresh status"), "primary", "order", o.ID, "")}}
	if o.BonusDiscountStars > 0 {
		text += fmt.Sprintf(tr(lang, "\nقیمت اولیه: %d ⭐\nتخفیف پاداش: %d", "\nOriginal price: %d ⭐\nBonus discount: %d"), o.AmountStars+o.BonusDiscountStars, o.BonusDiscountStars)
		if bonusQuoteState != "" {
			text += "\n" + tr(lang, "وضعیت پاداش: ", "Bonus state: ") + bonusState(lang, bonusQuoteState)
		}
	}
	switch o.Status {
	case "quoted":
		text += "\n\n" + tr(lang, "پرداختی برای این سفارش ثبت نشده است. اعتبار فاکتور تا: ", "No payment is recorded for this order. Quote expiry: ") + time.Unix(o.ExpiresUnix, 0).UTC().Format("2006-01-02 15:04 UTC")
		if bonusQuoteState == "reserved" {
			rows = append(rows, []Button{actionButton(tr(lang, "آزادکردن پاداش این فاکتور", "Release this invoice's bonus"), "", "bonus_cancel_preview", o.ID, "")})
		} else if bonusQuoteState == "released" {
			text += "\n" + tr(lang, "تأیید پرداخت جدید با این فاکتور بسته است. اگر پرداخت را آغاز کرده‌اید، پیش از ساخت فاکتور دیگری وضعیت همین سفارش را تازه‌سازی کنید.", "New payment approval is blocked for this invoice. If you already started payment, refresh this order before creating another invoice.")
		}
	case "paid_pending_fulfillment":
		text += "\n\n" + tr(lang, "دوباره پرداخت نکنید. تکمیل همین سفارش را پیگیری کنید.", "Do not pay again. Continue fulfillment of this same order.")
		rows = append(rows, []Button{actionButton(tr(lang, "پیگیری تکمیل سفارش", "Retry order fulfillment"), "success", "order_retry", o.ID, "")})
	case "paid_pending_provisioning":
		rows = append(rows, []Button{actionButton(tr(lang, "مشاهدهٔ سرویس", "View service"), "success", "order_service", o.ID, "")})
	case "refunded":
		text += "\n\n" + tr(lang, "بازپرداخت مالی ثبت شده است؛ وضعیت اتصال را باید جداگانه در سرویس بررسی کنید.", "The financial refund is recorded; connection state is checked separately on the service page.")
	}
	if b.SupportEnabled {
		rows = append(rows, []Button{actionButton(tr(lang, "💬 پشتیبانی پرداخت", "💬 Payment support"), "", "ticket_new", "", "")})
	}
	rows = append(rows, []Button{back, homeButton(lang)})
	return oneScreen(text, rows), nil
}

func orderDisplayState(lang string, order business.TelegramOrder, bonusState string) string {
	if order.Status == "quoted" {
		if bonusState == "released" {
			return tr(lang, "رزرو آزادشده", "Reservation released")
		}
		if order.ExpiresUnix <= time.Now().Unix() {
			return tr(lang, "فاکتور منقضی", "Invoice expired")
		}
	}
	return orderState(lang, order.Status)
}

func orderState(lang, state string) string {
	switch state {
	case "quoted":
		return tr(lang, "در انتظار پرداخت", "Awaiting payment")
	case "paid_pending_fulfillment":
		return tr(lang, "پرداخت ثبت؛ منتظر تکمیل", "Paid; awaiting fulfillment")
	case "paid_pending_provisioning":
		return tr(lang, "سفارش ثبت؛ بررسی آماده‌بودن سرویس", "Registered; verify service readiness")
	case "refunded":
		return tr(lang, "بازپرداخت ثبت‌شده", "Refund recorded")
	default:
		return tr(lang, "نیازمند بررسی", "Requires review")
	}
}
