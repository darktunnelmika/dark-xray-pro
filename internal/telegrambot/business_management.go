package telegrambot

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegramreport"
)

func (b *BusinessBackend) management(ctx context.Context, r Request, name string) (Result, error) {
	a := actor(r.Access.Identity)
	lang := r.Language
	if r.Access.Role == "customer" {
		return Result{}, ErrAccess
	}
	back := backButton(lang, "manage", "", "")
	switch name {
	case "representatives":
		offset := parsePage(r.Action.Value)
		reps, err := b.store.ListTelegramRepresentatives(ctx, a, 9, offset)
		if err != nil {
			return Result{}, err
		}
		rows := [][]Button{}
		for _, rep := range reps[:min(8, len(reps))] {
			rows = append(rows, []Button{actionButton(clipped(rep.DisplayName, 75), "", "rep_report", rep.ID, "")})
		}
		if offset > 0 {
			rows = append(rows, []Button{actionButton(tr(lang, "قبلی", "Previous"), "", "representatives", "", strconv.Itoa(max(0, offset-8)))})
		}
		if len(reps) > 8 {
			rows = append(rows, []Button{actionButton(tr(lang, "بعدی", "Next"), "", "representatives", "", strconv.Itoa(offset+8))})
		}
		rows = append(rows, []Button{actionButton(tr(lang, "➕ ساخت نماینده", "➕ Create representative"), "success", "representative_new", "", "")}, []Button{back})
		return oneScreen(tr(lang, "👥 <b>نمایندگان هاب</b>", "👥 <b>HUB representatives</b>"), rows), nil
	case "representative_new":
		if r.Access.Role != "owner" || r.Access.RepresentativeID != "owner" {
			return Result{}, ErrAccess
		}
		out := oneScreen(tr(lang, "شناسهٔ انگلیسی و نام نماینده را با | جدا کنید.\nمثال: <code>rep_milo | میلو</code>", "Send the representative ID and name separated by |.\nExample: <code>rep_milo | Milo</code>"), [][]Button{{back}})
		out.Input = &Action{Name: "representative_input"}
		return out, nil
	case "representative_input":
		if r.Access.Role != "owner" || r.Access.RepresentativeID != "owner" {
			return Result{}, ErrAccess
		}
		parts := strings.SplitN(r.Incoming.Text, "|", 2)
		if len(parts) != 2 {
			return Result{}, ErrProtocol
		}
		id, label := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if !validOpaque(id, 64) || !validText(label, 128) {
			return Result{}, ErrProtocol
		}
		out := oneScreen(tr(lang, "ساخت نماینده را تأیید کنید:", "Confirm creating this representative:")+"\n<code>"+escaped(id)+"</code>\n"+escaped(label), [][]Button{{actionButton(tr(lang, "✅ تأیید ساخت", "✅ Confirm creation"), "success", "representative_create", id, label)}, {back}})
		out.ClearInput = true
		return out, nil
	case "representative_create":
		rep, err := b.store.CreateTelegramRepresentative(ctx, a, business.TelegramRepresentativeCreate{EventID: r.EventID, ID: r.Action.Resource, Name: r.Action.Value})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "نماینده ثبت شد: ", "Representative created: ") + escaped(rep.DisplayName), Buttons: [][]Button{{actionButton(tr(lang, "اعتبار نماینده", "Representative credit"), "primary", "rep_report", rep.ID, "")}, {back}}}}, nil
	case "rep_report", "credits":
		id := r.Action.Resource
		if name == "credits" {
			if !r.Access.Has("finance") {
				return Result{}, ErrAccess
			}
			id = r.Access.RepresentativeID
		} else {
			if r.Access.Role != "owner" || r.Access.RepresentativeID != "owner" {
				return Result{}, ErrAccess
			}
		}
		if id == "owner" {
			return oneScreen(tr(lang, "حساب مالک دفتر اعتبار نمایندگی ندارد. اعتبار آزاد و رزرو‌شدهٔ هر نماینده از بخش «نمایندگان و اعتبار» قابل بررسی است.", "The owner account does not use a representative credit allowance. Inspect each representative's free and reserved credit under Representatives and credit."), [][]Button{{back}}), nil
		}
		report, err := b.store.OwnerRepresentativeLedgerReport(ctx, id)
		if err != nil {
			return Result{}, err
		}
		text := ""
		if lang == "fa" && id != "owner" {
			text, err = telegramreport.FormatOwnerRepresentativeFA(telegramreport.OwnerRepresentativeCard{RepresentativeID: report.RepresentativeID, DisplayName: report.DisplayName, Enabled: report.Enabled, FreeVolumeBytes: report.FreeVolumeBytes, FreeUnlimitedUnits: report.FreeUnlimitedUnits, SellerQuotaState: report.SellerQuotaState, SellerCount: report.SellerCount, ReservedVolumeBytes: report.ReservedVolumeBytes, ReservedUnlimitedUnits: report.ReservedUnlimitedUnits, BalanceSemantics: report.BalanceSemantics})
			if err != nil {
				return Result{}, ErrProtocol
			}
		} else {
			text = fmt.Sprintf("<b>%s</b>\n%s: %s\n%s: %s", escaped(report.DisplayName), tr(lang, "اعتبار آزاد حجمی (بایت)", "Free volume credit (bytes)"), report.FreeVolumeBytes, tr(lang, "اعتبار آزاد نامحدود (واحد)", "Free unlimited credit (units)"), report.FreeUnlimitedUnits)
		}
		rows := [][]Button{}
		if r.Access.Role == "owner" && id != "owner" {
			rows = append(rows, []Button{actionButton(tr(lang, "💳 تغییر اعتبار", "💳 Adjust credit"), "", "credit_prompt", id, "")})
		}
		rows = append(rows, []Button{back})
		return oneScreen(text, rows), nil
	case "credit_prompt":
		if r.Access.Role != "owner" || r.Access.RepresentativeID != "owner" {
			return Result{}, ErrAccess
		}
		out := oneScreen(tr(lang, "تغییر حجم برحسب GiB و تغییر واحد نامحدود را با فاصله بفرستید. عدد منفی یعنی کاهش اعتبار.\nمثال: <code>100 20</code>\nقبل از ثبت، پیش‌نمایش می‌بینید.", "Send the volume change in GiB and unlimited-unit change, separated by a space. Negative values reduce credit.\nExample: <code>100 20</code>\nYou will review a confirmation before applying."), [][]Button{{back}})
		out.Input = &Action{Name: "credit_input", Resource: r.Action.Resource}
		return out, nil
	case "credit_input":
		if r.Access.Role != "owner" || r.Access.RepresentativeID != "owner" {
			return Result{}, ErrAccess
		}
		fields := strings.Fields(r.Incoming.Text)
		if len(fields) != 2 {
			return Result{}, ErrProtocol
		}
		gib, ok := new(big.Rat).SetString(fields[0])
		if !ok || len(fields[0]) > 30 {
			return Result{}, ErrProtocol
		}
		gib.Mul(gib, big.NewRat(1<<30, 1))
		if !gib.IsInt() || !gib.Num().IsInt64() {
			return Result{}, ErrProtocol
		}
		volume := gib.Num().Int64()
		units, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || (volume == 0 && units == 0) {
			return Result{}, ErrProtocol
		}
		text := fmt.Sprintf(tr(lang, "تأیید تغییر اعتبار <code>%s</code>\nحجم: %+d بایت\nنامحدود: %+d واحد", "Confirm credit adjustment for <code>%s</code>\nVolume: %+d bytes\nUnlimited: %+d units"), escaped(r.Action.Resource), volume, units)
		out := oneScreen(text, [][]Button{{actionButton(tr(lang, "✅ ثبت تغییر اعتبار", "✅ Apply credit adjustment"), "success", "credit_confirm", r.Action.Resource, fmt.Sprintf("%d:%d", volume, units))}, {back}})
		out.ClearInput = true
		return out, nil
	case "credit_confirm":
		parts := strings.Split(r.Action.Value, ":")
		if len(parts) != 2 {
			return Result{}, ErrProtocol
		}
		volume, e1 := strconv.ParseInt(parts[0], 10, 64)
		units, e2 := strconv.ParseInt(parts[1], 10, 64)
		if e1 != nil || e2 != nil {
			return Result{}, ErrProtocol
		}
		rep, err := b.store.AdjustTelegramRepresentativeCredits(ctx, a, business.CreditChange{EventID: r.EventID, RepresentativeID: r.Action.Resource, VolumeDeltaBytes: volume, UnlimitedDeltaUnits: units, Reason: "confirmed by HUB Telegram owner"})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "✅ تغییر اعتبار ثبت شد.", "✅ Credit adjustment recorded.") + fmt.Sprintf("\n%d bytes · %d units", rep.VolumeCreditBytes, rep.UnlimitedCreditUnits), Buttons: [][]Button{{actionButton(tr(lang, "گزارش تازه", "Refresh report"), "primary", "rep_report", rep.ID, "")}, {back}}}}, nil
	case "prices", "price", "price_input", "price_set":
		if !r.Access.Has("manage_products") {
			return Result{}, ErrAccess
		}
		products, err := b.store.ListTelegramManagedProducts(ctx, a)
		if err != nil {
			return Result{}, err
		}
		if name == "prices" {
			rows := [][]Button{}
			for _, p := range products[:min(20, len(products))] {
				rows = append(rows, []Button{actionButton(clipped(fmt.Sprintf("%s · %d ⭐", p.Product.Name, p.PriceStars), 90), "", "price", p.Product.ID, strconv.FormatInt(p.PriceRevision, 10))})
			}
			rows = append(rows, []Button{back})
			return oneScreen(tr(lang, "🏷 <b>قیمت محصولات منتشرشده</b>", "🏷 <b>Published product prices</b>"), rows), nil
		}
		if name == "price" {
			out := oneScreen(tr(lang, "قیمت جدید را به عدد صحیح Telegram Stars بفرستید.", "Send the new integer price in Telegram Stars."), [][]Button{{back}})
			out.Input = &Action{Name: "price_input", Resource: r.Action.Resource, Value: r.Action.Value}
			return out, nil
		}
		if name == "price_input" {
			amount, err := strconv.ParseInt(strings.TrimSpace(r.Incoming.Text), 10, 64)
			if err != nil || amount < 1 || amount > 1_000_000_000 {
				return Result{}, ErrProtocol
			}
			out := oneScreen(fmt.Sprintf(tr(lang, "قیمت جدید: %d ⭐\nتغییر را تأیید می‌کنید؟", "New price: %d ⭐\nConfirm this change?"), amount), [][]Button{{actionButton(tr(lang, "ثبت قیمت", "Save price"), "success", "price_set", r.Action.Resource, r.Action.Value+":"+strconv.FormatInt(amount, 10))}, {back}})
			out.ClearInput = true
			return out, nil
		}
		parts := strings.Split(r.Action.Value, ":")
		if len(parts) != 2 {
			return Result{}, ErrProtocol
		}
		revision, e1 := strconv.ParseInt(parts[0], 10, 64)
		amount, e2 := strconv.ParseInt(parts[1], 10, 64)
		if e1 != nil || e2 != nil {
			return Result{}, ErrProtocol
		}
		product, err := b.store.SetTelegramProductPrice(ctx, a, business.TelegramProductPrice{EventID: r.EventID, ProductID: r.Action.Resource, PriceStars: amount, ExpectedRevision: revision})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, Screen: &Screen{Text: fmt.Sprintf(tr(lang, "✅ قیمت ثبت شد: %d ⭐", "✅ Price saved: %d ⭐"), product.PriceStars), Buttons: [][]Button{{back}}}}, nil
	case "admins", "admin", "admin_disable", "admin_disable_confirm", "admin_invite", "invite_prompt", "invite_input":
		return b.admins(ctx, r, name)
	}
	return Result{}, ErrProtocol
}

func (b *BusinessBackend) admins(ctx context.Context, r Request, name string) (Result, error) {
	if !r.Access.Has("manage_admins") {
		return Result{}, ErrAccess
	}
	a := actor(r.Access.Identity)
	lang := r.Language
	back := backButton(lang, "admins", "", "")
	if name == "admin_invite" {
		rows := [][]Button{}
		for _, preset := range []struct{ fa, en, permissions string }{{"پشتیبانی", "Support", "support"}, {"فروش", "Sales", "create_clients,renew_clients"}, {"مدیریت محتوا", "Content manager", "support,manage_products,broadcast_draft"}} {
			allowed := true
			for _, p := range strings.Split(preset.permissions, ",") {
				if !r.Access.Has(p) {
					allowed = false
				}
			}
			if allowed {
				rows = append(rows, []Button{actionButton(tr(lang, preset.fa, preset.en), "", "invite_prompt", "", preset.permissions)})
			}
		}
		rows = append(rows, []Button{back})
		return oneScreen(tr(lang, "دسترسی ادمین جدید را انتخاب کنید.", "Choose permissions for the new administrator."), rows), nil
	}
	if name == "invite_prompt" {
		out := oneScreen(tr(lang, "شناسهٔ عددی تلگرام فرد را بفرستید. دعوت فقط توسط همان فرد در همین ربات پذیرفته می‌شود.", "Send the person's numeric Telegram user ID. Only that person can accept the invitation in this bot."), [][]Button{{back}})
		out.Input = &Action{Name: "invite_input", Value: r.Action.Value}
		return out, nil
	}
	if name == "invite_input" {
		id, err := strconv.ParseInt(strings.TrimSpace(r.Incoming.Text), 10, 64)
		if err != nil || id <= 0 {
			return Result{}, ErrProtocol
		}
		invite, err := b.store.InviteTelegramAdmin(ctx, a, business.TelegramAdminInvitationRequest{EventID: r.EventID, UserID: id, Permissions: strings.Split(r.Action.Value, ",")})
		if err != nil {
			return Result{}, err
		}
		copyCommand := "/accept " + invite.ID
		rows := [][]Button{{{Text: tr(lang, "📋 کپی دعوت", "📋 Copy invitation"), CopyText: copyCommand}}, {back}}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "دعوت ساخته شد. فرد دعوت‌شده پس از /start این دستور را در همین ربات بفرستد:", "Invitation created. After /start, the invitee should send this command in this bot:") + "\n<code>" + escaped(copyCommand) + "</code>", Buttons: rows}}, nil
	}
	admins, err := b.store.ListTelegramAdmins(ctx, a)
	if err != nil {
		return Result{}, err
	}
	if name == "admins" {
		rows := [][]Button{}
		for _, admin := range admins[:min(20, len(admins))] {
			state := "✅"
			if !admin.Enabled {
				state = "⏸"
			}
			rows = append(rows, []Button{actionButton(state+" "+strconv.FormatInt(admin.UserID, 10)+" · "+admin.Role, "", "admin", strconv.FormatInt(admin.UserID, 10), "")})
		}
		rows = append(rows, []Button{actionButton(tr(lang, "➕ دعوت ادمین", "➕ Invite administrator"), "success", "admin_invite", "", "")}, []Button{backButton(lang, "manage", "", "")})
		return oneScreen(tr(lang, "👤 <b>ادمین‌های همین فروشگاه</b>", "👤 <b>This store's administrators</b>"), rows), nil
	}
	id, err := strconv.ParseInt(r.Action.Resource, 10, 64)
	if err != nil {
		return Result{}, ErrProtocol
	}
	var chosen *business.TelegramAdmin
	for i := range admins {
		if admins[i].UserID == id {
			chosen = &admins[i]
		}
	}
	if chosen == nil {
		return Result{}, ErrAccess
	}
	if name == "admin_disable_confirm" {
		revision, err := strconv.ParseInt(r.Action.Value, 10, 64)
		if err != nil {
			return Result{}, ErrProtocol
		}
		_, err = b.store.UpdateTelegramAdmin(ctx, a, business.TelegramAdminChange{EventID: r.EventID, UserID: id, ExpectedRevision: revision, Permissions: chosen.Permissions, Enabled: false})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "دسترسی ادمین لغو شد.", "Administrator access revoked."), Buttons: [][]Button{{back}}}}, nil
	}
	if name == "admin_disable" {
		return oneScreen(tr(lang, "لغو دسترسی این ادمین را تأیید کنید.", "Confirm revoking this administrator's access."), [][]Button{{actionButton(tr(lang, "لغو دسترسی", "Revoke access"), "danger", "admin_disable_confirm", r.Action.Resource, strconv.FormatInt(chosen.Revision, 10))}, {back}}), nil
	}
	rows := [][]Button{}
	if chosen.Role == "admin" && chosen.Enabled {
		rows = append(rows, []Button{actionButton(tr(lang, "لغو دسترسی", "Revoke access"), "danger", "admin_disable", r.Action.Resource, "")})
	}
	rows = append(rows, []Button{back})
	return oneScreen("<b>"+strconv.FormatInt(id, 10)+"</b>\n"+escaped(strings.Join(chosen.Permissions, " · ")), rows), nil
}
