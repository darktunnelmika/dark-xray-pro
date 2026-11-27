package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The reseller's own bot from inside Telegram, and growing the reseller
// tree (hubcore/repgrow.go): invite links, "become a reseller" requests and
// the owner's trial panel.

// ---- the reseller's own bot ----

// repBotMenu shows the reseller's bot and what can be done with it.
func (b *Bot) repBotMenu(ctx context.Context, chat int64, rep string) {
	st := b.Store.RepBotOf(ctx, rep)
	var sb strings.Builder
	sb.WriteString("🤖 <b>ربات فروش شما</b>\n" + line + "\n")
	kb := &Keyboard{}
	if st.HasToken {
		state := "⏸ خاموش"
		if st.Running {
			state = "🟢 روشن"
		} else if st.Enabled {
			state = "⏳ در حال روشن شدن"
		}
		if st.Username != "" {
			fmt.Fprintf(&sb, "ربات: @%s\n", esc(st.Username))
		}
		fmt.Fprintf(&sb, "وضعیت: %s\n", state)
		if st.Error != "" {
			fmt.Fprintf(&sb, "⚠️ خطا: %s\n", esc(st.Error))
		}
		sb.WriteString("\nبا عوض کردن توکن (ربات تازه)، مشتری‌ها، سفارش‌ها، کیف پول‌ها و تنظیمات همه می‌مانند.")
		toggle := repBtn("⏸ خاموش کن", "botoff")
		if !st.Enabled {
			toggle = repBtn("▶️ روشن کن", "boton")
		}
		kb.Rows = append(kb.Rows, []Button{repBtn("🔑 تعویض توکن", "bottok"), toggle}, []Button{repBtn("🗑 حذف ربات", "botdel")})
	} else {
		sb.WriteString("هنوز رباتی وصل نکرده‌اید. با ربات خودتان، مشتری‌ها به اسم و برند شما خرید و تمدید می‌کنند.\n\n" +
			"۱. در @BotFather دستور /newbot را بزنید و اسم و آیدی ربات را بدهید.\n" +
			"۲. توکنی که BotFather می‌دهد را با دکمهٔ زیر همین‌جا بفرستید.\n" +
			"ربات در کمتر از یک دقیقه روشن می‌شود.")
		kb.Rows = append(kb.Rows, []Button{repBtn("🔑 ثبت توکن ربات", "bottok")})
	}
	kb.Rows = append(kb.Rows, []Button{repBtn("🏠 داشبورد", "home")})
	b.send(ctx, chat, sb.String(), kb)
}

// botTokenCheck asks Telegram who a token belongs to ("" with an error
// when it is not a working token). Test fakes skip the check.
func (b *Bot) botTokenCheck(ctx context.Context, tok string) (string, error) {
	cur, ok := b.API.(*HTTPAPI)
	if !ok {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return (&HTTPAPI{Token: tok, Base: cur.Base, Client: cur.Client}).Me(ctx)
}

// repBotToken takes a BotFather token from the reseller.
func (b *Bot) repBotToken(ctx context.Context, chat int64, rep, text string) {
	tok := strings.TrimSpace(text)
	again := &Keyboard{Rows: [][]Button{{repBtn("🔁 دوباره", "bottok"), repBtn("« ربات من", "bot")}}}
	user, err := b.botTokenCheck(ctx, tok)
	if err != nil {
		b.send(ctx, chat, "❌ این توکن کار نمی‌کند. توکن را کامل و بدون فاصله از @BotFather کپی کنید.", again)
		return
	}
	if err := b.Store.PutRepBot(ctx, rep, hubcore.RepBot{Token: tok, Enabled: true}); err != nil {
		b.send(ctx, chat, "❌ ثبت نشد: "+esc(faErr(err)), again)
		return
	}
	who := "ربات شما"
	if user != "" {
		who = "ربات @" + esc(user)
	}
	b.send(ctx, chat, "✅ "+who+" وصل شد و تا یک دقیقهٔ دیگر روشن می‌شود؛ بعد در خود ربات /start بزنید.\n\n"+
		"🔐 برای امنیت، پیامی که توکن در آن بود را از این گفتگو پاک کنید.", &Keyboard{Rows: [][]Button{{repBtn("🤖 ربات من", "bot"), repBtn("🏠 داشبورد", "home")}}})
}

// repBotCallback handles R:bot… buttons (the reseller's own chats only).
func (b *Bot) repBotCallback(ctx context.Context, chat int64, rep, role, verb string) {
	if role != "owner" {
		b.send(ctx, chat, "ربات را فقط خود نماینده مدیریت می‌کند.", repBackKB)
		return
	}
	switch verb {
	case "bot":
		b.repBotMenu(ctx, chat, rep)
	case "bottok":
		b.setAwait(chat, "r:bottok")
		b.send(ctx, chat, "🔑 توکن ربات را از @BotFather کپی کنید و همین‌جا بفرستید (مثل <code>123456789:AAH…</code>).", &Keyboard{Rows: [][]Button{{repBtn("« انصراف", "bot")}}})
	case "boton", "botoff":
		if err := b.Store.PutRepBot(ctx, rep, hubcore.RepBot{Enabled: verb == "boton"}); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), repBackKB)
			return
		}
		b.repBotMenu(ctx, chat, rep)
	case "botdel":
		b.send(ctx, chat, "ربات از پنل جدا شود؟ مشتری‌ها و سفارش‌ها می‌مانند و با وصل کردن ربات دیگر برمی‌گردند.",
			&Keyboard{Rows: [][]Button{{repBtn("🗑 بله، جدا کن", "botdelok"), repBtn("« نه", "bot")}}})
	case "botdelok":
		if err := b.Store.PutRepBot(ctx, rep, hubcore.RepBot{Token: "-"}); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), repBackKB)
			return
		}
		b.repBotMenu(ctx, chat, rep)
	}
}

// ---- growing the tree ----

// botUser is the @username customers reach a seller through ("" if unknown).
func (b *Bot) botUser(ctx context.Context, seller string) string {
	if seller != "" {
		if u := b.Store.RepBotOf(ctx, seller).Username; u != "" {
			return u
		}
	}
	return b.Store.Setting(ctx, "tg_username")
}

// repGrowMenu: the invite link and the request switch (top-level resellers).
func (b *Bot) repGrowMenu(ctx context.Context, chat int64, rep, role, verb string) {
	if role != "owner" {
		b.send(ctx, chat, "این بخش فقط برای خود نماینده است.", repBackKB)
		return
	}
	if b.Store.IsSubSeller(ctx, rep) {
		b.send(ctx, chat, "زیرفروشنده‌ها نمی‌توانند نماینده بگیرند.", repBackKB)
		return
	}
	switch verb {
	case "reqon", "reqoff":
		_ = b.Store.SetRepRequestsOn(ctx, rep, verb == "reqon")
	case "inv":
		code, err := b.Store.NewSellerInvite(ctx, rep)
		u := b.botUser(ctx, rep)
		if err != nil || u == "" {
			b.send(ctx, chat, "ساخت لینک ممکن نشد.", repBackKB)
			return
		}
		b.send(ctx, chat, "🔗 <b>لینک دعوت نماینده</b> (۷ روز اعتبار):\n<code>https://t.me/"+esc(u)+"?start=rsinv_"+code+"</code>\n\n"+
			"هر کس با این لینک ربات را باز کند، همان لحظه نمایندهٔ زیرمجموعهٔ شما می‌شود و پنل خودش را می‌گیرد. اعتبار ندارد تا شما از اعتبار خودتان به او بدهید.",
			&Keyboard{Rows: [][]Button{{repBtn("« جذب نماینده", "grow")}}})
		return
	}
	on := b.Store.RepRequestsOn(ctx, rep)
	state, tg := "خاموش", repBtn("🙋 روشن کردن دکمهٔ درخواست", "reqon")
	if on {
		state, tg = "روشن", repBtn("🙋 خاموش کردن دکمهٔ درخواست", "reqoff")
	}
	b.send(ctx, chat, "🧩 <b>جذب نماینده</b>\n"+line+"\n"+
		"🔗 <b>لینک دعوت:</b> کسی که با لینک بیاید بی‌درنگ زیرمجموعهٔ شما می‌شود.\n"+
		"🙋 <b>دکمهٔ «درخواست نمایندگی»</b> در منوی مشتری‌ها: درخواست برای شما می‌آید و با یک لمس قبول یا رد می‌کنید. الان: <b>"+state+"</b>",
		&Keyboard{Rows: [][]Button{{repBtn("🔗 ساخت لینک دعوت", "inv")}, {tg}, {repBtn("🏠 داشبورد", "home")}}})
}

// sendNewPanel gives a new reseller its login and dashboard.
func (b *Bot) sendNewPanel(ctx context.Context, chat int64, np hubcore.NewPanel, intro string) {
	text := intro + "\n\n🔐 <b>ورود به پنل وب</b>\n" + esc(strings.TrimRight(b.SubBase, "/")) +
		"\nنام کاربری: <code>" + esc(np.Username) + "</code>\nرمز: <code>" + esc(np.Password) + "</code>\n(رمز را بعد از ورود عوض کنید.)"
	kb := &Keyboard{}
	if r := b.panelAppRow(); r != nil {
		kb.Rows = append(kb.Rows, r)
	}
	b.send(ctx, chat, text, kb)
	if b.repInScope(ctx, np.RepID) {
		b.ensureRepKeyboard(ctx, chat)
		b.repHome(ctx, chat, np.RepID, "owner")
	}
}

// claimInvite: /start rsinv_<code>.
func (b *Bot) claimInvite(ctx context.Context, chat int64, name, username, code string) {
	np, err := b.Store.ClaimSellerInvite(ctx, code, chat, name, username)
	if err != nil {
		msg := "این لینک دعوت معتبر نیست یا منقضی شده."
		if errors.Is(err, hubcore.ErrInvalid) {
			msg = "❌ " + esc(faErr(err))
		}
		b.send(ctx, chat, msg, homeKB)
		return
	}
	b.sendNewPanel(ctx, chat, np, "🎉 <b>نمایندگی شما فعال شد!</b>\nشما حالا نمایندهٔ زیرمجموعه هستید. برای ساخت کانفیگ، اعتبار را از نمایندهٔ بالایی بگیرید.")
	for _, c := range b.Store.TgRepOwners(ctx, np.Parent) {
		b.send(ctx, c, fmt.Sprintf("🎉 <b>%s</b> با لینک دعوت شما نماینده شد (شناسه <code>%s</code>).\nبرای شروع کار، از اعتبار خودتان به او بدهید.", esc(name), esc(np.RepID)), nil)
	}
}

// growCallback handles the customers' request and trial-panel buttons and
// the seller's decisions on requests. It reports whether it handled one.
func (b *Bot) growCallback(ctx context.Context, q *CallbackQuery) bool {
	chat := q.From.ID
	name := strings.TrimSpace(q.From.FirstName + " " + q.From.LastName)
	verb, rest, _ := strings.Cut(q.Data, ":")
	switch verb {
	case "rq":
		if !b.Store.RepRequestsOn(ctx, b.Seller) {
			b.menu(ctx, chat)
			return true
		}
		b.send(ctx, chat, "🙋 <b>درخواست نمایندگی</b>\n"+line+"\nبا نمایندگی، پنل فروش خودتان را می‌گیرید و برای مشتری‌هایتان کانفیگ می‌سازید. درخواست شما برای مدیر فروشگاه فرستاده می‌شود و نتیجه همین‌جا خبر داده می‌شود.",
			&Keyboard{Rows: [][]Button{row(cbBtn("✅ ثبت درخواست", "rqok"), cbBtn("« منو", "home"))}})
	case "rqok":
		added, err := b.Store.AddRepRequest(ctx, b.Seller, chat, name, q.From.Username)
		switch {
		case err != nil:
			b.send(ctx, chat, "ثبت نشد: "+esc(faErr(err)), homeKB)
			return true
		case !added:
			b.send(ctx, chat, "درخواست شما قبلاً ثبت شده و در حال بررسی است.", homeKB)
			return true
		}
		b.send(ctx, chat, "✅ درخواست شما ثبت شد. نتیجه همین‌جا اعلام می‌شود.", homeKB)
		who := esc(name)
		if q.From.Username != "" {
			who += " (@" + esc(q.From.Username) + ")"
		}
		msg := fmt.Sprintf("🙋 <b>درخواست نمایندگی</b>\n%s · <code>%d</code>", who, chat)
		id := strconv.FormatInt(chat, 10)
		kb := &Keyboard{Rows: [][]Button{row(cbBtn("✅ قبول", "Q:ok:"+id), cbBtn("❌ رد", "Q:no:"+id))}}
		if b.Seller == "" {
			b.notify(ctx, "reps", msg, kb)
		} else {
			for _, c := range b.Store.TgRepOwners(ctx, b.Seller) {
				b.send(ctx, c, msg, kb)
			}
		}
	case "Q":
		decision, idS, _ := strings.Cut(rest, ":")
		who, _ := strconv.ParseInt(idS, 10, 64)
		allowed := b.Owners[chat]
		if b.Seller != "" {
			allowed = b.roleOn(ctx, chat, b.Seller) == "owner"
		}
		if !allowed || who == 0 {
			return true
		}
		np, err := b.Store.DecideRepRequest(ctx, b.Seller, who, decision == "ok", "tg:"+strconv.FormatInt(chat, 10))
		switch {
		case errors.Is(err, hubcore.ErrNotFound):
			b.send(ctx, chat, "این درخواست قبلاً بررسی شده است.", nil)
		case err != nil:
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), nil)
		case decision != "ok":
			b.send(ctx, chat, "درخواست رد شد.", nil)
			b.send(ctx, who, "درخواست نمایندگی شما پذیرفته نشد. برای توضیح بیشتر با پشتیبانی در تماس باشید.", homeKB)
		default:
			b.send(ctx, chat, fmt.Sprintf("✅ نمایندگی <code>%s</code> ساخته شد. برای شروع کار به او اعتبار بدهید.", esc(np.RepID)), nil)
			b.sendNewPanel(ctx, who, np, "🎉 <b>درخواست نمایندگی شما قبول شد!</b>\nپنل شما آماده است؛ برای ساخت کانفیگ اعتبار شارژ کنید.")
		}
	case "tpn":
		t := b.Store.RepTrialPolicy(ctx)
		if b.Seller != "" || !t.On {
			b.menu(ctx, chat)
			return true
		}
		b.send(ctx, chat, fmt.Sprintf("🎁 <b>پنل نمایندگی تستی</b>\n%s\nیک پنل نمایندگی با <b>%s گیگ</b> اعتبار رایگان می‌گیرید تا کانفیگ بسازید و امتحان کنید. هر حساب تلگرام فقط یک بار. اگر بعداً شارژ کنید، همین پنل با همهٔ کانفیگ‌هایش می‌ماند.", line, faNum(strconv.FormatFloat(t.GB, 'f', -1, 64))),
			&Keyboard{Rows: [][]Button{row(cbBtn("✅ بساز", "tpnok"), cbBtn("« منو", "home"))}})
	case "tpnok":
		if b.Seller != "" {
			return true
		}
		np, err := b.Store.ClaimRepTrial(ctx, chat, name, q.From.Username)
		switch {
		case errors.Is(err, hubcore.ErrTrialUsed):
			b.send(ctx, chat, "شما قبلاً پنل تستی گرفته‌اید.", homeKB)
		case err != nil:
			b.send(ctx, chat, "ساخت پنل ممکن نشد: "+esc(faErr(err)), homeKB)
		default:
			b.sendNewPanel(ctx, chat, np, "🎉 <b>پنل نمایندگی تستی شما آماده است!</b>")
			b.notify(ctx, "reps", fmt.Sprintf("🎁 پنل تستی نمایندگی برای %s ساخته شد (<code>%s</code>).", esc(name), esc(np.RepID)), nil)
		}
	default:
		return false
	}
	return true
}

// growButtons are the customer menu's request and trial-panel buttons.
func (b *Bot) growButtons(ctx context.Context, chat int64) []menuButton {
	if b.Owners[chat] {
		return nil
	}
	if rep, _ := b.Store.TgRepRole(ctx, chat); rep != "" {
		return nil
	}
	var out []menuButton
	if b.Seller == "" {
		if t := b.Store.RepTrialPolicy(ctx); t.On && !b.Store.RepTrialUsed(ctx, chat) {
			out = append(out, menuButton{key: "trialpanel", btn: cbBtn("🎁 پنل نمایندگی تستی", "tpn")})
		}
	}
	if b.Store.RepRequestsOn(ctx, b.Seller) {
		out = append(out, menuButton{key: "reqrep", btn: cbBtn("🙋 درخواست نمایندگی", "rq")})
	}
	return out
}
