package tgbot

import (
	"context"
	"errors"
	"strconv"

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
