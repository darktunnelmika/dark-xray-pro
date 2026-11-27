package hubcore

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// A seller's bot texts kept apart per seller: the welcome shown to a new
// customer and the text above the plans. "" is the owner's main bot.

func welcomeKey(seller string) string {
	if seller == "" {
		return "tg_welcome"
	}
	return "tg_welcome:" + seller
}

func shopTextKey(seller string) string { return "shop_text:" + seller }

// Welcome is the seller's welcome text ("" = the built-in one).
func (s *Store) Welcome(ctx context.Context, seller string) string {
	return s.Setting(ctx, welcomeKey(seller))
}

func (s *Store) PutWelcome(ctx context.Context, seller, text string) error {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > 1000 {
		return fmt.Errorf("%w: at most 1000 characters", ErrInvalid)
	}
	return s.SetSetting(ctx, welcomeKey(seller), text)
}

// ShopText is the seller's text above its plans ("" = the built-in one).
func (s *Store) ShopText(ctx context.Context, seller string) string {
	return s.Setting(ctx, shopTextKey(seller))
}

func (s *Store) PutShopText(ctx context.Context, seller, text string) error {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > 1000 {
		return fmt.Errorf("%w: at most 1000 characters", ErrInvalid)
	}
	return s.SetSetting(ctx, shopTextKey(seller), text)
}

// AppBot is what the Mini App asks of a seller's running bot.
type AppBot struct {
	// Audience counts the bot's chats per group (all, active, expiring,
	// expired, none).
	Audience func(ctx context.Context, seller string) map[string]int
	// Broadcast sends text (an optional last-line [button](url)) to a group
	// from the seller's bot, reporting to chat; "test" sends only to chat.
	Broadcast func(ctx context.Context, seller string, chat int64, text, audience string) (int, error)
}

var appBot AppBot

func SetAppBot(b AppBot) { appBot = b }
