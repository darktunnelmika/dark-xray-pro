package tgbot

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The main menu: a banner, a greeting by name, one short card per service
// (usage bar, what is left) and buttons that lead with what this customer
// most likely wants next: renew an ended service, open the running one, or
// try and buy when there is none.

//go:embed banner.jpg
var bannerJPG []byte

// bannerKey names the stored Telegram file id of the banner; change the
// suffix when banner.jpg changes.
const bannerKey = "tg_banner_v1"

// photoUploader is the part of the API that can upload a picture and report
// its file id (the real API can; test doubles need not).
type photoUploader interface {
	UploadPhoto(ctx context.Context, chat int64, name string, img []byte, caption string, kb *Keyboard) (string, error)
}

const menuCards = 3 // services shown as cards; the rest are counted

// sizeShort is size without needless decimals: "۴۰ گیگ", not "۴۰٫۰۰ گیگ".
func sizeShort(n int64) string {
	return strings.NewReplacer("٫۰۰", "", ".00", "").Replace(size(n))
}

func usageBar(share float64) string {
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}
	n := int(share*10 + 0.5)
	return strings.Repeat("▰", n) + strings.Repeat("▱", 10-n)
}

// planTitle is a readable name for a service: the product it was bought
// with, or its size and length.
func (b *Bot) planTitle(ctx context.Context, c hubcore.ClientView) string {
	name := b.Store.ClientPlanName(ctx, c.Email)
	// Drop a leading emoji of the product's name: the card has its own icon.
	name = strings.TrimLeftFunc(name, func(r rune) bool { return r > 0x2000 || r == ' ' || r == '️' })
	if name != "" {
		return name
	}
	var parts []string
	switch {
	case c.VolumeBytes > 0:
		parts = append(parts, sizeShort(c.VolumeBytes))
	case c.VolumeBytes < 0:
		parts = append(parts, "نامحدود")
	}
	if c.DurationSeconds >= 86400 {
		parts = append(parts, faInt(c.DurationSeconds/86400)+" روزه")
	}
	if len(parts) == 0 {
		return c.Email
	}
	return strings.Join(parts, " · ")
}

// menuCard draws one service for the menu and reports whether it has ended
// (so the menu can lead with renewing it).
func (b *Bot) menuCard(ctx context.Context, c hubcore.ClientView, showName bool) (card string, ended bool) {
	now := b.now().Unix()
	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>📦 %s</b>", esc(b.planTitle(ctx, c)))
	if showName {
		fmt.Fprintf(&sb, " — <code>%s</code>", esc(c.Email))
	}
	sb.WriteString("\n")
	used := c.Usage.Up + c.Usage.Down
	switch {
	case !c.Enabled:
		sb.WriteString("⛔ این سرویس غیرفعال است.")
		return sb.String(), false
	case c.Remaining == 0:
		sb.WriteString(usageBar(1) + " تمام شد\nحجم این سرویس تمام شده است. با تمدید، همین لینک دوباره فعال می‌شود.")
		return sb.String(), true
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		sb.WriteString("⌛ زمان این سرویس تمام شده است. با تمدید، همین لینک دوباره فعال می‌شود.")
		return sb.String(), true
	}
	if c.VolumeBytes > 0 {
		share := float64(used) / float64(c.VolumeBytes)
		left := c.VolumeBytes - used
		if left < 0 {
			left = 0
		}
		fmt.Fprintf(&sb, "%s %s٪\nحجم مانده: <b>%s</b> از %s\n", usageBar(share), faInt(int64(share*100+0.5)), sizeShort(left), sizeShort(c.VolumeBytes))
	} else {
		sb.WriteString("حجم: <b>نامحدود</b>\n")
	}
	switch {
	case c.EffectiveUntil > now:
		left := c.EffectiveUntil - now
		if left >= 86400 {
			fmt.Fprintf(&sb, "زمان مانده: <b>%s روز</b>", faInt((left+86399)/86400))
		} else {
			fmt.Fprintf(&sb, "زمان مانده: <b>%s ساعت</b>", faInt((left+3599)/3600))
		}
	case c.DurationSeconds > 0 && c.Usage.FirstSeen == 0:
		fmt.Fprintf(&sb, "🆕 آماده؛ %s روز از اولین اتصال", faInt(c.DurationSeconds/86400))
	default:
		sb.WriteString("زمان: <b>بدون محدودیت</b>")
	}
	return sb.String(), false
}

func (b *Bot) menu(ctx context.Context, chat int64) {
	var sb strings.Builder
	if name := b.Store.TgName(ctx, chat); name != "" {
		fmt.Fprintf(&sb, "<b>سلام %s 👋</b>\n", esc(name))
	} else {
		sb.WriteString("<b>سلام 👋</b>\n")
	}
	emails, _ := b.clients(ctx, chat)
	var views []hubcore.ClientView
	for _, e := range emails {
		if c, err := b.Store.ClientByEmail(ctx, e); err == nil {
			views = append(views, c)
		}
	}
	var ended []string
	for i, c := range views {
		card, end := b.menuCard(ctx, c, len(views) > 1)
		if end {
			ended = append(ended, c.Email)
		}
		if i < menuCards {
			sb.WriteString("<blockquote>" + card + "</blockquote>\n")
		}
	}
	if n := len(views) - menuCards; n > 0 {
		fmt.Fprintf(&sb, "و %s سرویس دیگر در «سرویس‌های من».\n", faInt(int64(n)))
	}
	if len(views) == 0 {
		if w := b.Store.Setting(ctx, "tg_welcome"); w != "" {
			sb.WriteString(esc(w) + "\n")
		} else {
			brand := "به <b>Dark VPN</b> خوش آمدید."
			if b.Seller != "" {
				brand = "خوش آمدید."
			}
			sb.WriteString(brand + "\nاینترنت بدون قطعی" + b.countriesPhrase(ctx) + "، با یک لینک برای همهٔ دستگاه‌ها.\n")
			if b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat) {
				sb.WriteString("اول رایگان امتحان کنید، بعد تصمیم بگیرید.\n")
			}
		}
	}
	if bal := b.Store.TomanBalance(ctx, b.Seller, chat); bal > 0 {
		sb.WriteString("👛 کیف پول: <b>" + faMoney(bal) + " تومان</b>\n")
	}
	b.sendMenu(ctx, chat, strings.TrimRight(sb.String(), "\n"), b.menuKB(ctx, chat, len(views), ended))
}

// countriesPhrase is " روی N کشور" when several servers are up.
func (b *Bot) countriesPhrase(ctx context.Context) string {
	nodes, err := b.Store.Nodes(ctx)
	if err != nil {
		return ""
	}
	n, now := 0, b.now().Unix()
	for _, x := range nodes {
		if now-x.LastSeen < 180 {
			n++
		}
	}
	if n < 2 {
		return ""
	}
	return " روی <b>" + faInt(int64(n)) + " کشور</b>"
}

// sendMenu sends the menu under the banner in the main bot; a reseller's bot
// (its own brand) and anything the banner cannot carry get plain text.
func (b *Bot) sendMenu(ctx context.Context, chat int64, text string, kb *Keyboard) {
	up, ok := b.API.(photoUploader)
	if !ok || b.Seller != "" || len(bannerJPG) == 0 || utf8.RuneCountInString(text) > 900 {
		b.send(ctx, chat, text, kb)
		return
	}
	if id := b.Store.Setting(ctx, bannerKey); id != "" {
		if err := b.API.SendPhotoID(ctx, chat, id, text, kb); err == nil {
			return
		} else if isBlocked(err) {
			return
		}
		_ = b.Store.SetSetting(ctx, bannerKey, "") // the stored id went stale: upload again
	}
	id, err := up.UploadPhoto(ctx, chat, "dark-vpn.jpg", bannerJPG, text, kb)
	if err != nil {
		b.logf("tgbot banner: %v", err)
		b.send(ctx, chat, text, kb)
		return
	}
	if id != "" {
		_ = b.Store.SetSetting(ctx, bannerKey, id)
	}
}

// menuKB is the main menu's buttons. services is how many services the chat
// has, ended the ones that ran out. The Mini App store is not repeated here
// (its button sits beside the message box) and support lives on the bottom
// keyboard.
func (b *Bot) menuKB(ctx context.Context, chat int64, services int, ended []string) *Keyboard {
	var rows [][]Button
	trial := b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat)
	var small []Button
	switch {
	case services == 0:
		if trial {
			rows = append(rows, row(cbBtn("🎁 تست رایگان", "trial")))
		}
		rows = append(rows, row(cbBtn("🛍 خرید اشتراک", "shop")))
	default:
		if len(ended) == 1 && len("rnw:"+ended[0]) <= 64 {
			rows = append(rows, row(cbBtn("🔄 تمدید سرویس", "rnw:"+ended[0])))
		} else if len(ended) > 0 {
			rows = append(rows, row(cbBtn("🔄 تمدید سرویس‌ها", "mine")))
		}
		mine := "📦 سرویس من"
		if services > 1 {
			mine = "📦 سرویس‌های من"
		}
		rows = append(rows, row(cbBtn(mine, "mine"), cbBtn("🛍 خرید اشتراک", "shop")))
		if trial {
			small = append(small, cbBtn("🎁 تست رایگان", "trial"))
		}
	}
	// The wallet is always there when it can be used (to top it up), not
	// only once it holds something.
	if b.Seller != "" || b.cardOn(ctx) || b.Store.WalletBalance(ctx, chat) > 0 || b.Store.TomanBalance(ctx, b.Seller, chat) > 0 {
		small = append(small, cbBtn("👛 کیف پول", "wallet"))
	}
	small = append(small, cbBtn("📚 آموزش اتصال", "guide"), cbBtn("🌐 وضعیت سرورها", "status"))
	// Inviting friends is the owner's shop's; resellers' bots do not have it.
	if b.Seller == "" && b.Store.ReferralPolicy(ctx).Enabled {
		small = append(small, cbBtn("👥 دعوت دوستان", "invite"))
	}
	small = append(small, cbBtn("🎟 کد تخفیف", "cpn:"), cbBtn("👤 حساب من", "me"))
	if btn, ok := b.repSaleButton(ctx, chat); ok {
		small = append(small, btn)
	}
	small = append(small, cbBtn("🔗 افزودن با لینک", "link"))
	for i := 0; i < len(small); i += 2 {
		if i+1 < len(small) {
			rows = append(rows, row(small[i], small[i+1]))
		} else {
			rows = append(rows, row(small[i]))
		}
	}
	return &Keyboard{Rows: rows}
}
