package telegrambot

import (
	"context"
	"fmt"
	"time"
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
			rows = append(rows, []Button{actionButton(clipped(o.Product.Name, 40)+" · "+orderState(lang, o.Status), "", "order", o.ID, "")})
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
	text := "<b>" + escaped(o.Product.Name) + "</b>\n" + tr(lang, "وضعیت: ", "Status: ") + orderState(lang, o.Status) + "\n" + fmt.Sprintf(tr(lang, "مبلغ سفارش: %d ⭐", "Order amount: %d ⭐"), o.AmountStars) + "\n" + time.Unix(o.CreatedUnix, 0).UTC().Format("2006-01-02 15:04 UTC") + "\n<code>" + escaped(o.ID) + "</code>"
	rows := [][]Button{{actionButton(tr(lang, "تازه‌سازی وضعیت", "Refresh status"), "primary", "order", o.ID, "")}}
	switch o.Status {
	case "quoted":
		text += "\n\n" + tr(lang, "پرداختی برای این سفارش ثبت نشده است. اعتبار فاکتور تا: ", "No payment is recorded for this order. Quote expiry: ") + time.Unix(o.ExpiresUnix, 0).UTC().Format("2006-01-02 15:04 UTC")
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
