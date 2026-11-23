package tgbot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Notices in a forum group. The owner (main bot) or a reseller (its own
// bot) makes a group with Topics on, adds the bot as an admin that may
// manage topics and sends /setup there: the bot opens one topic per kind
// of notice and posts each notice in its topic, so the private chat stays
// for menus. Without a group, or if posting fails, notices go to the
// private chats as before. Only panel staff can use the buttons; anyone
// else in the group just reads.

type forumAPI interface {
	CreateForumTopic(ctx context.Context, chat int64, name string, color int) (int64, error)
	SendThread(ctx context.Context, chat, thread int64, text string, kb *Keyboard) (int64, error)
	SendPhotoIDThread(ctx context.Context, chat, thread int64, fileID, caption string, kb *Keyboard) error
	SendDocumentThread(ctx context.Context, chat, thread int64, name string, data []byte, caption string) error
}

type forumCfg struct {
	Chat   int64            `json:"chat"`
	Title  string           `json:"title"`
	Topics map[string]int64 `json:"topics"`
}

type topicDef struct {
	key, name string
	color     int
	about     string
}

// Topic colours are the six Telegram allows.
var ownerTopics = []topicDef{
	{"orders", "🛒 سفارش‌ها و پرداخت‌ها", 0x6FB9F0, "سفارش‌های تازه، رسیدهای کارت‌به‌کارت و فروش با استارز اینجا می‌آیند؛ تأیید و رد با دکمه‌ها."},
	{"tickets", "🎫 تیکت‌ها", 0xFFD67E, "پیام مشتری‌ها به پشتیبانی. برای جواب، روی پیام تیکت Reply کنید یا «پاسخ» را بزنید."},
	{"nodes", "📡 نودها و تونل‌ها", 0xFF93B2, "قطع و وصل شدن نودها و تونل‌ها."},
	{"reps", "💼 نماینده‌ها", 0xCB86DB, "اعتبار رو به اتمام نماینده‌ها و کارهای مهمشان."},
	{"reports", "📊 گزارش‌ها", 0x8EEE98, "گزارش روزانهٔ فروش و وضعیت."},
	{"backups", "💾 بکاپ‌ها", 0xFB6F5F, "فایل بکاپ رمزگذاری‌شدهٔ خودکار."},
}

var repTopics = []topicDef{
	{"orders", "🛒 سفارش‌ها و پرداخت‌ها", 0x6FB9F0, "سفارش‌های تازه و رسیدهای پرداخت مشتری‌های شما؛ تأیید و رد با دکمه‌ها."},
	{"tickets", "🎫 تیکت‌ها", 0xFFD67E, "پیام مشتری‌ها به پشتیبانی. برای جواب، روی پیام تیکت Reply کنید یا «پاسخ» را بزنید."},
	{"reports", "💳 اعتبار و گزارش", 0x8EEE98, "هشدار اعتبار رو به اتمام و خلاصهٔ روزانهٔ فروش."},
}

func (b *Bot) topicDefs() []topicDef {
	if b.Seller == "" {
		return ownerTopics
	}
	return repTopics
}

func (b *Bot) forumKey() string { return "tg_forum:" + b.Seller }

func (b *Bot) forum(ctx context.Context) (forumCfg, bool) {
	var c forumCfg
	if json.Unmarshal([]byte(b.Store.Setting(ctx, b.forumKey())), &c) != nil || c.Chat == 0 || len(c.Topics) == 0 {
		return c, false
	}
	return c, true
}

// noticeTarget: the topic for a kind of notice ("reps" falls back to
// "reports" in a reseller's group).
func (b *Bot) noticeTarget(ctx context.Context, topic string) (chat, thread int64, ok bool) {
	c, on := b.forum(ctx)
	if !on {
		return 0, 0, false
	}
	if _, isForum := b.API.(forumAPI); !isForum {
		return 0, 0, false
	}
	t, has := c.Topics[topic]
	if !has {
		t, has = c.Topics["reports"]
	}
	return c.Chat, t, has
}

// staffChats: who gets notices without a group — the owner's chats in the
// main bot, the reseller and its bot admins in a reseller's bot.
func (b *Bot) staffChats(ctx context.Context) []int64 {
	if b.Seller == "" {
		out := make([]int64, 0, len(b.Owners))
		for id := range b.Owners {
			out = append(out, id)
		}
		return out
	}
	return b.Store.TgRepChats(ctx, b.Seller)
}

// notify posts a notice in its topic, or to the staff chats. It returns the
// sent message per chat (for Reply mapping).
func (b *Bot) notify(ctx context.Context, topic, text string, kb *Keyboard) map[int64]int64 {
	if chat, thread, ok := b.noticeTarget(ctx, topic); ok {
		id, err := b.API.(forumAPI).SendThread(ctx, chat, thread, text, kb)
		if err == nil {
			return map[int64]int64{chat: id}
		}
		b.logf("tgbot forum %s: %v (sending to private chats)", topic, err)
	}
	out := map[int64]int64{}
	for _, c := range b.staffChats(ctx) {
		out[c] = b.send(ctx, c, text, kb)
	}
	return out
}

func (b *Bot) notifyPhoto(ctx context.Context, topic, fileID, caption string, kb *Keyboard) {
	if chat, thread, ok := b.noticeTarget(ctx, topic); ok {
		if err := b.API.(forumAPI).SendPhotoIDThread(ctx, chat, thread, fileID, caption, kb); err == nil {
			return
		} else {
			b.logf("tgbot forum %s: %v", topic, err)
		}
	}
	for _, c := range b.staffChats(ctx) {
		if err := b.API.SendPhotoID(ctx, c, fileID, caption, kb); err != nil {
			b.logf("tgbot receipt: %v", err)
		}
	}
}

// notifyDoc posts a file in its topic; ok=false means no group took it.
func (b *Bot) notifyDoc(ctx context.Context, topic, name string, data []byte, caption string) bool {
	chat, thread, ok := b.noticeTarget(ctx, topic)
	if !ok {
		return false
	}
	if err := b.API.(forumAPI).SendDocumentThread(ctx, chat, thread, name, data, caption); err != nil {
		b.logf("tgbot forum %s: %v", topic, err)
		return false
	}
	return true
}

// groupAdmin: may this user set the group up — an owner in the main bot,
// the reseller itself (role owner) in a reseller's bot.
func (b *Bot) groupAdmin(ctx context.Context, user int64) bool {
	if b.Seller == "" {
		return b.Owners[user]
	}
	rep, role := b.repOf(ctx, user)
	return rep == b.Seller && role == "owner"
}

func (b *Bot) groupSay(ctx context.Context, m *Message, text string) {
	if f, ok := b.API.(forumAPI); ok && m.Thread != 0 {
		if _, err := f.SendThread(ctx, m.Chat.ID, m.Thread, text, nil); err == nil {
			return
		}
	}
	_, _ = b.API.Send(ctx, m.Chat.ID, text, nil, 0)
}

// onGroupMessage handles the few things the bot does in a group: /setup,
// /unsetup, and staff answering a ticket by Reply.
func (b *Bot) onGroupMessage(ctx context.Context, m *Message) {
	text := strings.TrimSpace(m.Text)
	cmd := strings.SplitN(strings.SplitN(text, " ", 2)[0], "@", 2)[0]
	user := m.From.ID
	switch cmd {
	case "/setup":
		if !b.groupAdmin(ctx, user) {
			return
		}
		b.setupForum(ctx, m)
		return
	case "/unsetup":
		if !b.groupAdmin(ctx, user) {
			return
		}
		if c, ok := b.forum(ctx); ok && c.Chat == m.Chat.ID {
			_ = b.Store.SetSetting(ctx, b.forumKey(), "")
			b.groupSay(ctx, m, "اعلان‌ها از این گروه برداشته شد و دوباره به چت خصوصی می‌روند.")
		}
		return
	}
	// A staff Reply to a ticket notice answers the customer.
	if m.ReplyTo != nil && text != "" && !strings.HasPrefix(text, "/") && b.isTicketStaff(ctx, user) {
		if id, _, err := b.Store.TgTicketChat(ctx, m.Chat.ID, m.ReplyTo.ID); err == nil {
			t, err := b.Store.TicketReply(ctx, b.ticketPrincipal(user), id, "tg:"+strconv.FormatInt(user, 10), text, true)
			if err != nil {
				b.groupSay(ctx, m, "ارسال نشد.")
				return
			}
			b.send(ctx, t.Chat, fmt.Sprintf("📩 <b>پاسخ پشتیبانی</b> (تیکت #%d)\n%s\n%s", id, line, esc(text)), customerSupportKB)
			b.groupSay(ctx, m, fmt.Sprintf("✅ پاسخ تیکت #%d ارسال شد.", id))
		}
	}
}

func (b *Bot) setupForum(ctx context.Context, m *Message) {
	f, ok := b.API.(forumAPI)
	if !ok {
		return
	}
	if !m.Chat.IsForum {
		b.groupSay(ctx, m, "برای دسته‌بندی اعلان‌ها، اول در تنظیمات گروه «Topics» را روشن کنید، ربات را ادمین با دسترسی «مدیریت تاپیک‌ها» کنید و دوباره /setup بزنید.")
		return
	}
	c, _ := b.forum(ctx)
	if c.Chat != m.Chat.ID {
		c = forumCfg{Chat: m.Chat.ID, Topics: map[string]int64{}}
	}
	if c.Topics == nil {
		c.Topics = map[string]int64{}
	}
	c.Title = m.Chat.Title
	var made []string
	for _, t := range b.topicDefs() {
		if c.Topics[t.key] != 0 {
			continue
		}
		id, err := f.CreateForumTopic(ctx, m.Chat.ID, t.name, t.color)
		if err != nil {
			b.groupSay(ctx, m, "ساخت تاپیک ممکن نشد. ربات باید ادمین گروه با دسترسی «مدیریت تاپیک‌ها» باشد.\n"+esc(faErr(err)))
			return
		}
		c.Topics[t.key] = id
		made = append(made, t.name)
		_, _ = f.SendThread(ctx, m.Chat.ID, id, "📌 "+t.about, nil)
	}
	raw, _ := json.Marshal(c)
	if err := b.Store.SetSetting(ctx, b.forumKey(), string(raw)); err != nil {
		b.groupSay(ctx, m, "ذخیره نشد.")
		return
	}
	msg := "✅ اعلان‌ها از این به بعد در تاپیک‌های این گروه می‌آیند و چت خصوصی فقط برای منوهاست."
	if len(made) == 0 {
		msg = "✅ این گروه از قبل تنظیم است؛ تاپیک‌ها سر جایشان هستند."
	}
	msg += "\n\nدکمه‌ها (تأیید سفارش، پاسخ تیکت) فقط برای ادمین‌های پنل کار می‌کنند. برای برداشتن: /unsetup"
	b.groupSay(ctx, m, msg)
}
