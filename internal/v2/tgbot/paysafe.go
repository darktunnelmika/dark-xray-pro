package tgbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Safe card payments (hubcore/paycards.go): the phone check before a card
// is shown, the card chosen for each payment, the duplicate receipt check
// and the payer's last four digits.

// photoRef is a received photo: the id to send it on and Telegram's
// file_unique_id (the same for the same file, in every bot).
type photoRef struct {
	ID   string
	Uniq string
}

// fileAPI downloads a file the bot received (receipt checks).
type fileAPI interface {
	FileBytes(ctx context.Context, fileID string) ([]byte, error)
}

// contactAPI asks for the user's phone number with a reply keyboard.
type contactAPI interface {
	AskContact(ctx context.Context, chat int64, text, button string) error
	DropKeyboard(ctx context.Context, chat int64, text string) error
}

func (a *HTTPAPI) FileBytes(ctx context.Context, fileID string) ([]byte, error) {
	var f struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := a.callJSON(ctx, "getFile", map[string]any{"file_id": fileID}, &f); err != nil {
		return nil, err
	}
	if f.Path == "" || f.Size > 20<<20 {
		return nil, errors.New("telegram getFile: no file")
	}
	base := a.Base
	if base == "" {
		base = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/file/bot"+a.Token+"/"+f.Path, nil)
	if err != nil {
		return nil, errors.New("telegram file: bad request")
	}
	res, err := a.client().Do(req)
	if err != nil {
		return nil, errors.New("telegram file: request failed") // never echo the URL: it holds the token
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram file: %s", res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 20<<20))
}

func (a *HTTPAPI) AskContact(ctx context.Context, chat int64, text, button string) error {
	return a.callJSON(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML",
		"reply_markup": map[string]any{"keyboard": [][]map[string]any{{{"text": button, "request_contact": true}}}, "resize_keyboard": true, "one_time_keyboard": true}}, nil)
}

func (a *HTTPAPI) DropKeyboard(ctx context.Context, chat int64, text string) error {
	return a.callJSON(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML",
		"reply_markup": map[string]any{"remove_keyboard": true}}, nil)
}

const iranOnlyText = "⚠️ پرداخت کارت‌به‌کارت در این فروشگاه فقط با شمارهٔ موبایل ایران ممکن است. برای پرداخت به پشتیبانی پیام دهید."

// phoneGate tells whether the customer may see a card now. When the seller
// wants a verified phone number first, it asks for it and remembers what to
// show next ("inv:<order>", "top", "tamt:<amount>").
func (b *Bot) phoneGate(ctx context.Context, chat int64, next string) bool {
	if b.Store.PhoneOK(ctx, b.Seller, chat) {
		return true
	}
	api, ok := b.API.(contactAPI)
	if !ok {
		return true // test fakes: no reply keyboards
	}
	cs := b.Store.CardSafetyFor(ctx, b.Seller)
	if p := b.Store.TgPhone(ctx, chat); p != "" && cs.IranOnly && !hubcore.IranianPhone(p) {
		b.send(ctx, chat, iranOnlyText, &Keyboard{Rows: [][]Button{row(cbBtn("💬 پشتیبانی", "support"), cbBtn("« منو", "home"))}})
		return false
	}
	text := "🔐 <b>تأیید شماره</b>\n" + line + "\nبرای امنیت پرداخت، یک بار شمارهٔ موبایل همین حساب تلگرام را با دکمهٔ پایین صفحه بفرستید. شماره فقط برای تأیید پرداخت نگه داشته می‌شود."
	if cs.IranOnly {
		text += "\n(فقط شمارهٔ ایران پذیرفته می‌شود.)"
	}
	b.setAwait(chat, "phone:"+next)
	if err := api.AskContact(ctx, chat, text, "📱 ارسال شمارهٔ من"); err != nil {
		b.logf("tgbot ask contact: %v", err)
		b.send(ctx, chat, "الان امکان تأیید شماره نیست. کمی بعد دوباره امتحان کنید.", homeKB)
	}
	return false
}

// onContact takes the phone number the customer shared after phoneGate.
func (b *Bot) onContact(ctx context.Context, chat int64, m *Message) bool {
	if m.Contact == nil || m.From == nil {
		return false
	}
	next, ok := strings.CutPrefix(b.peekAwait(chat), "phone:")
	if !ok {
		// Shared without being asked here (the Mini App asks too): keep
		// the customer's own number and say so.
		if m.Contact.UserID != m.From.ID || b.Store.PutTgPhone(ctx, chat, m.Contact.PhoneNumber) != nil {
			return false
		}
		b.send(ctx, chat, "✅ شماره تأیید شد.", nil)
		return true
	}
	if m.Contact.UserID != m.From.ID {
		b.send(ctx, chat, "فقط شمارهٔ خود همین حساب تلگرام پذیرفته می‌شود؛ از دکمهٔ «ارسال شمارهٔ من» استفاده کنید.", nil)
		return true
	}
	if err := b.Store.PutTgPhone(ctx, chat, m.Contact.PhoneNumber); err != nil {
		b.send(ctx, chat, "این شماره معتبر نیست.", nil)
		return true
	}
	b.takeAwait(chat)
	if api, ok := b.API.(contactAPI); ok {
		_ = api.DropKeyboard(ctx, chat, "✅ شماره تأیید شد.")
	}
	if !b.Store.PhoneOK(ctx, b.Seller, chat) {
		b.send(ctx, chat, iranOnlyText, &Keyboard{Rows: [][]Button{row(cbBtn("💬 پشتیبانی", "support"), cbBtn("« منو", "home"))}})
		return true
	}
	switch {
	case strings.HasPrefix(next, "inv:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(next, "inv:"), 10, 64)
		if o, err := b.Store.OrderByID(ctx, id); err == nil && o.Buyer == hubcore.TgBuyer(chat) && o.Status == "pending" {
			b.invoice(ctx, chat, id)
		}
	case next == "top":
		b.askTopup(ctx, chat)
	case strings.HasPrefix(next, "tamt:"):
		b.startTopup(ctx, chat, strings.TrimPrefix(next, "tamt:"))
	}
	return true
}

// noCardText is shown when no card can take a payment today.
const noCardText = "💳 امروز امکان پرداخت کارت‌به‌کارت در این فروشگاه نیست (ظرفیت کارت‌ها پر شده). کمی بعد دوباره امتحان کنید یا به پشتیبانی پیام دهید."

// receiptNotes checks a receipt photo against earlier ones and describes the
// card it was paid to, for the admins' caption ("" when nothing to add).
func (b *Bot) receiptNotes(ctx context.Context, seller, ref string, chat int64, ph photoRef) string {
	var notes []string
	if c, _ := b.Store.PaymentCard(ctx, ref); c.Number != "" {
		n := c.Number
		if len(n) > 4 {
			n = n[len(n)-4:]
		}
		t := "💳 به کارت: <code>…" + n + "</code>"
		if c.Bank != "" {
			t += " · " + esc(c.Bank)
		}
		notes = append(notes, t)
	}
	sha := ""
	if api, ok := b.API.(fileAPI); ok && ph.ID != "" {
		dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if data, err := api.FileBytes(dctx, ph.ID); err == nil {
			sha = hubcore.ReceiptHash(data)
		} else {
			b.logf("tgbot receipt download: %v", err)
		}
		cancel()
	}
	if d := b.Store.NoteReceipt(ctx, seller, ref, chat, ph.Uniq, sha); d != nil {
		t := "⚠️ <b>رسید تکراری!</b> همین عکس قبلاً "
		switch {
		case strings.HasPrefix(d.Ref, "o:"):
			t += "برای سفارش #" + faNum(strings.TrimPrefix(d.Ref, "o:"))
		case strings.HasPrefix(d.Ref, "t:"):
			t += "برای شارژ کیف پول #" + faNum(strings.TrimPrefix(d.Ref, "t:"))
		default:
			t += "برای پرداخت دیگری"
		}
		t += " (" + faDate(d.Unix) + ")"
		if d.SameBuyer {
			t += " از همین مشتری"
		}
		t += " فرستاده شده است. قبل از تأیید، واریز را با دقت بررسی کنید."
		notes = append(notes, t)
	}
	return strings.Join(notes, "\n")
}

// askLast4 asks the payer for the last four digits of their card when the
// seller wants it.
func (b *Bot) askLast4(ctx context.Context, seller, ref string, chat int64) {
	if !b.Store.CardSafetyFor(ctx, seller).AskLast4 {
		return
	}
	b.setAwait(chat, "last4:"+ref)
	b.send(ctx, chat, "🔢 لطفاً <b>۴ رقم آخر کارتی</b> که با آن واریز کردید را بفرستید (مثلاً <code>1234</code>). این کار تأیید را سریع‌تر می‌کند.", nil)
}

// takeLast4 keeps the four digits and tells the seller's staff.
func (b *Bot) takeLast4(ctx context.Context, chat int64, ref, text string) {
	if err := b.Store.SetPayerLast4(ctx, ref, text); err != nil {
		b.setAwait(chat, "last4:"+ref)
		b.send(ctx, chat, "فقط ۴ رقم بفرستید (مثلاً <code>1234</code>).", nil)
		return
	}
	_, d := b.Store.PaymentCard(ctx, ref)
	b.send(ctx, chat, "✅ ثبت شد. بعد از بررسی خبرتان می‌کنیم.", homeKB)
	var what, seller string
	switch {
	case strings.HasPrefix(ref, "o:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(ref, "o:"), 10, 64)
		o, err := b.Store.OrderByID(ctx, id)
		if err != nil {
			return
		}
		what, seller = "سفارش #"+faInt(o.ID), o.Seller
	case strings.HasPrefix(ref, "t:"):
		what, seller = "شارژ کیف پول #"+faNum(strings.TrimPrefix(ref, "t:")), b.Seller
	default:
		return
	}
	msg := fmt.Sprintf("🔢 %s: ۴ رقم آخر کارت واریزکننده <code>%s</code>", what, esc(d))
	if seller == b.Seller {
		b.notify(ctx, "orders", msg, nil)
		return
	}
	for _, c := range b.Store.TgRepChats(ctx, seller) {
		b.send(ctx, c, msg, nil)
	}
}
