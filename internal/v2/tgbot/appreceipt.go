package tgbot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// AppReceipt takes a receipt photo uploaded in the Mini App. The photo is
// sent to the buyer's own chat (so this bot gets a file id for it) and then
// handled like a photo the buyer sent here: kind order | topup | panel.
func (b *Bot) AppReceipt(ctx context.Context, chat int64, name, kind string, id int64, img []byte) error {
	up, ok := b.API.(photoUploader)
	if !ok {
		return errors.New("tgbot: this API cannot upload photos")
	}
	fid, err := up.UploadPhoto(ctx, chat, "receipt.jpg", img, "🧾 رسید شما از مینی‌اپ فرستاده شد.", nil)
	if isBlocked(err) {
		return hubcore.ErrNoBotChat
	}
	if err != nil {
		return err
	}
	if fid == "" {
		return errors.New("tgbot: no file id for the receipt")
	}
	sid := strconv.FormatInt(id, 10)
	var done bool
	switch kind {
	case "order":
		done = b.onReceipt(ctx, chat, name, photoRef{ID: fid}, "receipt:"+sid)
	case "topup":
		done = b.onTopupReceipt(ctx, chat, name, photoRef{ID: fid}, "topup:"+sid)
	case "panel":
		done = b.repSaleReceipt(ctx, chat, name, fid, "P:receipt:"+sid)
	}
	if !done {
		return hubcore.ErrNotFound
	}
	return nil
}

// AppAudience counts this bot's chats per group, for a message from the
// Mini App.
func (b *Bot) AppAudience(ctx context.Context) map[string]int {
	out := map[string]int{}
	for _, k := range []string{"all", "active", "expiring", "expired", "none"} {
		out[k] = len(b.audience(ctx, k))
	}
	return out
}

// AppBroadcast sends a message written in the Mini App to a group of this
// bot's customers; "test" sends it only to the writer. The report comes to
// the writer's chat when it is done.
func (b *Bot) AppBroadcast(ctx context.Context, chat int64, text, audience string) int {
	d := &bcDraft{text: text, audience: audience, cb: "a:"}
	if b.Seller != "" {
		d.cb = "R:"
	}
	if m := buttonRE.FindStringSubmatch(text); m != nil {
		d.button = &Button{Text: m[1], URL: m[2]}
		d.text = strings.TrimSpace(strings.TrimSuffix(text, m[0]))
	}
	if audience == "test" {
		d.chats, d.title = []int64{chat}, "🧪 پیام آزمایشی"
	} else {
		d.chats = b.audience(ctx, audience)
	}
	if len(d.chats) == 0 {
		return 0
	}
	b.mu.Lock()
	b.bcStop = false
	b.mu.Unlock()
	go b.broadcast(context.WithoutCancel(ctx), chat, d)
	return len(d.chats)
}
