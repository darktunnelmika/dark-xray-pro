package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// The look of a bot's main menu, set by its seller ("" = the owner's main
// bot, else a reseller's own bot): which buttons, in what order, under what
// names and colours, and the picture above the menu. Nothing saved means the
// built-in look.

// BotLookButton is one button of the main menu.
type BotLookButton struct {
	Key    string `json:"key"`
	Label  string `json:"label,omitempty"` // "" = the built-in name
	Style  string `json:"style,omitempty"` // "" = by meaning, primary (blue), success (green), danger (red), plain
	Hidden bool   `json:"hidden,omitempty"`
	Wide   bool   `json:"wide,omitempty"` // a row of its own
}

// BotLook is a seller's saved look.
type BotLook struct {
	Buttons []BotLookButton `json:"buttons"`
	// Banner: "" = built-in (the main bot's picture, none in a reseller's
	// bot), "custom" = the uploaded picture, "none" = text only.
	Banner string `json:"banner"`
}

// BotMenuButton describes a button the menu can show, for the editor.
type BotMenuButton struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Wide  bool   `json:"wide"`
	Note  string `json:"note,omitempty"` // when it shows
}

// BotMenuButtons lists the main menu's buttons in their built-in order.
var BotMenuButtons = []BotMenuButton{
	{Key: "app", Label: "📱 باز کردن اپ", Wide: true, Note: "مینی‌اپ فروشگاه؛ وقتی آدرس https دارد"},
	{Key: "trial", Label: "🎁 تست رایگان", Wide: true, Note: "فقط وقتی تست رایگان روشن است و مشتری هنوز نگرفته"},
	{Key: "renew", Label: "🔄 تمدید سرویس", Wide: true, Note: "فقط وقتی سرویسی تمام شده باشد"},
	{Key: "mine", Label: "📦 سرویس‌های من", Note: "فقط برای مشتری‌ای که سرویس دارد"},
	{Key: "shop", Label: "🛍 خرید اشتراک"},
	{Key: "wallet", Label: "👛 کیف پول", Note: "وقتی پرداخت کارتی یا کیف پول در دسترس است"},
	{Key: "guide", Label: "📚 آموزش اتصال"},
	{Key: "status", Label: "🌐 وضعیت سرورها"},
	{Key: "invite", Label: "👥 دعوت دوستان", Note: "فقط در ربات اصلی و وقتی دعوت دوستان روشن است"},
	{Key: "coupon", Label: "🎟 کد تخفیف"},
	{Key: "wheel", Label: "🎡 گردونهٔ شانس", Note: "وقتی گردونه روشن است"},
	{Key: "me", Label: "👤 حساب من"},
	{Key: "panel", Label: "💼 خرید پنل نمایندگی", Note: "فقط وقتی فروش پنل روشن است"},
	{Key: "link", Label: "🔗 افزودن با لینک"},
	{Key: "support", Label: "💬 پشتیبانی"},
	{Key: "more", Label: "➕ بیشتر", Note: "وضعیت سرورها، کد تخفیف، افزودن با لینک و دعوت؛ فقط در چیدمان پیش‌فرض"},
}

func botLookKey(seller string) string {
	if seller == "" {
		return "tg_look"
	}
	return "tg_look:" + seller
}

// BotLook returns a seller's saved look (empty = built-in).
func (s *Store) BotLook(ctx context.Context, seller string) BotLook {
	var l BotLook
	if v := s.Setting(ctx, botLookKey(seller)); v != "" {
		_ = json.Unmarshal([]byte(v), &l)
	}
	return l
}

// PutBotLook saves a seller's look. No buttons and no banner choice clears it.
func (s *Store) PutBotLook(ctx context.Context, seller string, in BotLook) error {
	known := map[string]bool{}
	for _, b := range BotMenuButtons {
		known[b.Key] = true
	}
	seen := map[string]bool{}
	shown := 0
	for i := range in.Buttons {
		b := &in.Buttons[i]
		b.Label = strings.TrimSpace(b.Label)
		switch {
		case !known[b.Key] || seen[b.Key]:
			return fmt.Errorf("%w: unknown or repeated button %q", ErrInvalid, b.Key)
		case utf8.RuneCountInString(b.Label) > 40 || strings.ContainsAny(b.Label, "\n\r"):
			return fmt.Errorf("%w: a button name is one line of at most 40 characters", ErrInvalid)
		case b.Style != "" && b.Style != "primary" && b.Style != "success" && b.Style != "danger" && b.Style != "plain":
			return fmt.Errorf("%w: unknown button colour %q", ErrInvalid, b.Style)
		}
		seen[b.Key] = true
		if !b.Hidden {
			shown++
		}
	}
	if len(in.Buttons) > 0 && shown == 0 {
		return fmt.Errorf("%w: at least one button must stay visible", ErrInvalid)
	}
	switch in.Banner {
	case "", "none":
	case "custom":
		if _, _, ok := s.BotBanner(ctx, seller); !ok {
			return fmt.Errorf("%w: upload a picture first", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown banner choice", ErrInvalid)
	}
	if len(in.Buttons) == 0 && in.Banner == "" {
		return s.SetSetting(ctx, botLookKey(seller), "")
	}
	b, _ := json.Marshal(in)
	return s.SetSetting(ctx, botLookKey(seller), string(b))
}

func (s *Store) ensureBotLookSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS bot_banners (
  seller TEXT PRIMARY KEY,
  img BLOB NOT NULL,
  mime TEXT NOT NULL,
  file_id TEXT NOT NULL DEFAULT '',
  updated_unix INTEGER NOT NULL
)`)
	return err
}

// MaxBotBanner is the largest banner picture taken.
const MaxBotBanner = 2 << 20

// PutBotBanner stores a seller's own menu picture (JPEG or PNG).
func (s *Store) PutBotBanner(ctx context.Context, seller string, img []byte) error {
	mime := ""
	switch {
	case bytes.HasPrefix(img, []byte{0xFF, 0xD8, 0xFF}):
		mime = "image/jpeg"
	case bytes.HasPrefix(img, []byte("\x89PNG\r\n\x1a\n")):
		mime = "image/png"
	}
	if mime == "" {
		return fmt.Errorf("%w: the picture must be a JPEG or a PNG", ErrInvalid)
	}
	if len(img) > MaxBotBanner {
		return fmt.Errorf("%w: the picture must be under 2 MB", ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO bot_banners(seller,img,mime,file_id,updated_unix) VALUES(?,?,?,'',?)
ON CONFLICT(seller) DO UPDATE SET img=excluded.img, mime=excluded.mime, file_id='', updated_unix=excluded.updated_unix`, seller, img, mime, s.now().Unix())
	return err
}

// BotBanner returns a seller's own picture and the Telegram file id it was
// last sent under ("" = not sent yet).
func (s *Store) BotBanner(ctx context.Context, seller string) (img []byte, fileID string, ok bool) {
	err := s.ro.QueryRowContext(ctx, `SELECT img,file_id FROM bot_banners WHERE seller=?`, seller).Scan(&img, &fileID)
	return img, fileID, err == nil && len(img) > 0
}

// SetBotBannerFileID remembers the Telegram file id of a seller's picture.
func (s *Store) SetBotBannerFileID(ctx context.Context, seller, id string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE bot_banners SET file_id=? WHERE seller=?`, id, seller)
}

// DeleteBotBanner removes a seller's picture; a look that used it goes back
// to the built-in banner.
func (s *Store) DeleteBotBanner(ctx context.Context, seller string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM bot_banners WHERE seller=?`, seller); err != nil {
		return err
	}
	if l := s.BotLook(ctx, seller); l.Banner == "custom" {
		l.Banner = ""
		return s.PutBotLook(ctx, seller, l)
	}
	return nil
}

// botLookRoutes serves one seller's look under base (…/look, …/banner).
func botLookRoutes(s *Store, mux *docMux, base string, wrap func(http.HandlerFunc) http.HandlerFunc, sellerOf func(*http.Request) (string, bool)) {
	view := func(ctx context.Context, seller string) map[string]any {
		_, _, has := s.BotBanner(ctx, seller)
		l := s.BotLook(ctx, seller)
		if l.Buttons == nil {
			l.Buttons = []BotLookButton{}
		}
		return map[string]any{"look": l, "defaults": BotMenuButtons, "has_banner": has, "main": seller == ""}
	}
	with := func(h func(w http.ResponseWriter, r *http.Request, seller string)) http.HandlerFunc {
		return wrap(func(w http.ResponseWriter, r *http.Request) {
			seller, ok := sellerOf(r)
			if !ok {
				http.NotFound(w, r)
				return
			}
			h(w, r, seller)
		})
	}
	mux.HandleFunc("GET "+base+"/look", with(func(w http.ResponseWriter, r *http.Request, seller string) {
		reply(w, view(r.Context(), seller), nil)
	}))
	mux.HandleFunc("PUT "+base+"/look", with(func(w http.ResponseWriter, r *http.Request, seller string) {
		var in BotLook
		if !decode(w, r, &in) {
			return
		}
		err := s.PutBotLook(r.Context(), seller, in)
		reply(w, view(r.Context(), seller), err)
	}))
	mux.HandleFunc("GET "+base+"/banner", with(func(w http.ResponseWriter, r *http.Request, seller string) {
		var img []byte
		var mime string
		if err := s.ro.QueryRowContext(r.Context(), `SELECT img,mime FROM bot_banners WHERE seller=?`, seller).Scan(&img, &mime); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(img)
	}))
	mux.HandleFunc("PUT "+base+"/banner", with(func(w http.ResponseWriter, r *http.Request, seller string) {
		img, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBotBanner+1))
		if err != nil {
			reply(w, nil, fmt.Errorf("%w: the picture must be under 2 MB", ErrInvalid))
			return
		}
		err = s.PutBotBanner(r.Context(), seller, img)
		reply(w, view(r.Context(), seller), err)
	}))
	mux.HandleFunc("DELETE "+base+"/banner", with(func(w http.ResponseWriter, r *http.Request, seller string) {
		err := s.DeleteBotBanner(r.Context(), seller)
		reply(w, view(r.Context(), seller), err)
	}))
}
