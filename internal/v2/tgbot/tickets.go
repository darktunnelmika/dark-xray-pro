package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Support tickets in the bot. A customer's message joins their open ticket
// with this bot's seller (or opens one); the seller's staff get it with
// "answer" and "close" buttons and can also just Reply to it. Answers typed
// in the panel come through the outbox on each tick.

func (b *Bot) isTicketStaff(ctx context.Context, chat int64) bool {
	if b.Seller == "" {
		return b.Owners[chat]
	}
	rep, _ := b.repOf(ctx, chat)
	return rep == b.Seller
}

func (b *Bot) ticketPrincipal(chat int64) hubcore.Principal {
	if b.Seller == "" {
		return hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "owner"}
	}
	return hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "rep", RepID: b.Seller}
}

func ticketKB(id int64) *Keyboard {
	s := strconv.FormatInt(id, 10)
	return &Keyboard{Rows: [][]Button{{{Text: "✍️ پاسخ", Data: "T:r:" + s}, {Text: "✅ بستن تیکت", Data: "T:c:" + s}}}}
}

var customerSupportKB = &Keyboard{Rows: [][]Button{{{Text: "💬 پاسخ / پیام جدید", Data: "support"}, {Text: "« منو", Data: "home"}}}}

func (b *Bot) ticket(ctx context.Context, chat int64, name, username, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	id, isNew, err := b.Store.CustomerTicket(ctx, b.Seller, chat, name, text)
	if err != nil {
		b.logf("tgbot ticket: %v", err)
		b.send(ctx, chat, "پیام ثبت نشد؛ کوتاه‌تر بنویسید و دوباره بفرستید.", customerSupportKB)
		return
	}
	who := esc(name)
	if username != "" {
		who += " (@" + esc(username) + ")"
	}
	emails, _ := b.clients(ctx, chat)
	head := "🆕 تیکت جدید"
	if !isNew {
		head = "💬 پیام تازه در تیکت"
	}
	svc := "—"
	if len(emails) > 0 {
		svc = strings.Join(emails, ", ")
	}
	msg := fmt.Sprintf("%s <b>#%d</b>\n👤 %s\n📦 سرویس‌ها: %s\n%s\n%s\n\n<i>با «پاسخ» یا Reply روی همین پیام جواب بدهید.</i>",
		head, id, who, esc(svc), line, esc(text))
	for staff, mid := range b.notify(ctx, "tickets", msg, ticketKB(id)) {
		if mid != 0 {
			b.Store.TgMapMessage(ctx, staff, mid, id)
		}
	}
	if isNew {
		b.send(ctx, chat, fmt.Sprintf("✅ پیام شما ثبت شد (تیکت #%d).\nپاسخ پشتیبانی همین‌جا برایتان می‌آید.", id), nil)
	} else {
		b.send(ctx, chat, fmt.Sprintf("✅ به تیکت #%d اضافه شد.", id), nil)
	}
}

// ticketPhoto adds a customer's picture to its ticket and shows it to staff.
func (b *Bot) ticketPhoto(ctx context.Context, chat int64, name, username, caption, fileID string) {
	id, isNew, err := b.Store.CustomerTicketPhoto(ctx, b.Seller, chat, name, caption, fileID)
	if err != nil {
		b.send(ctx, chat, "عکس ثبت نشد؛ دوباره بفرستید.", customerSupportKB)
		return
	}
	who := esc(name)
	if username != "" {
		who += " (@" + esc(username) + ")"
	}
	head := "🆕 تیکت جدید (عکس)"
	if !isNew {
		head = "📷 عکس تازه در تیکت"
	}
	note := fmt.Sprintf("%s <b>#%d</b>\n👤 %s", head, id, who)
	if strings.TrimSpace(caption) != "" {
		note += "\n" + esc(caption)
	}
	b.notifyPhoto(ctx, "tickets", fileID, note, ticketKB(id))
	b.send(ctx, chat, fmt.Sprintf("✅ عکس به تیکت #%d اضافه شد. می‌توانید توضیح هم بنویسید.", id), customerSupportKB)
	b.setAwait(chat, "support")
}

// staffAnswer sends a staff answer to the customer and records it.
func (b *Bot) staffAnswer(ctx context.Context, staff, id int64, text string) {
	t, err := b.Store.TicketReply(ctx, b.ticketPrincipal(staff), id, "tg:"+strconv.FormatInt(staff, 10), text, true)
	if err != nil {
		b.send(ctx, staff, "ارسال نشد: تیکت پیدا نشد یا پیام خالی است.", nil)
		return
	}
	b.send(ctx, t.Chat, fmt.Sprintf("📩 <b>پاسخ پشتیبانی</b> (تیکت #%d)\n%s\n%s", id, line, esc(text)), customerSupportKB)
	b.send(ctx, staff, fmt.Sprintf("✅ پاسخ تیکت #%d ارسال شد.", id), &Keyboard{Rows: [][]Button{{{Text: "✅ بستن تیکت", Data: "T:c:" + strconv.FormatInt(id, 10)}}}})
}

// ticketCallback handles "T:" buttons.
func (b *Bot) ticketCallback(ctx context.Context, chat int64, data string) bool {
	if !strings.HasPrefix(data, "T:") {
		return false
	}
	if !b.isTicketStaff(ctx, chat) {
		return true
	}
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		return true
	}
	id, _ := strconv.ParseInt(parts[2], 10, 64)
	switch parts[1] {
	case "r":
		b.setAwait(chat, "T:"+parts[2])
		b.send(ctx, chat, fmt.Sprintf("✍️ پاسخ تیکت #%d را بنویسید و بفرستید:", id), &Keyboard{Rows: [][]Button{{{Text: "« انصراف", Data: "T:x:" + parts[2]}}}})
	case "x":
		b.takeAwait(chat)
		b.send(ctx, chat, "لغو شد.", nil)
	case "c":
		t, err := b.Store.TicketClose(ctx, b.ticketPrincipal(chat), id, false)
		if err != nil {
			b.send(ctx, chat, "تیکت پیدا نشد.", nil)
			return true
		}
		b.send(ctx, t.Chat, fmt.Sprintf("✅ تیکت #%d بسته شد. اگر باز سؤالی داشتید، دوباره پیام بدهید.", id), customerSupportKB)
		b.send(ctx, chat, fmt.Sprintf("🔒 تیکت #%d بسته شد.", id), nil)
	}
	return true
}

// ticketMessage handles a staff Reply to a ticket message or a typed
// answer after "answer"; false = not a ticket answer.
func (b *Bot) ticketMessage(ctx context.Context, m *Message, text string) bool {
	chat := m.Chat.ID
	if text == "" || strings.HasPrefix(text, "/") || !b.isTicketStaff(ctx, chat) {
		return false
	}
	if w := b.peekAwait(chat); strings.HasPrefix(w, "T:") {
		b.takeAwait(chat)
		id, _ := strconv.ParseInt(strings.TrimPrefix(w, "T:"), 10, 64)
		b.staffAnswer(ctx, chat, id, text)
		return true
	}
	if m.ReplyTo != nil {
		if id, _, err := b.Store.TgTicketChat(ctx, chat, m.ReplyTo.ID); err == nil {
			b.staffAnswer(ctx, chat, id, text)
			return true
		}
	}
	return false
}

// deliverTickets sends answers and close notices typed in the panel.
func (b *Bot) deliverTickets(ctx context.Context) {
	for _, o := range b.Store.TicketOutbox(ctx, b.Seller) {
		if o.Close {
			b.send(ctx, o.Chat, fmt.Sprintf("✅ تیکت #%d بسته شد. اگر باز سؤالی داشتید، دوباره پیام بدهید.", o.Ticket), customerSupportKB)
		} else {
			b.send(ctx, o.Chat, fmt.Sprintf("📩 <b>پاسخ پشتیبانی</b> (تیکت #%d)\n%s\n%s", o.Ticket, line, esc(o.Text)), customerSupportKB)
		}
		b.Store.TicketSent(ctx, o)
	}
}
