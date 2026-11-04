package telegrambot

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func (b *BusinessBackend) support(ctx context.Context, r Request, name string) (Result, error) {
	a := actor(r.Access.Identity)
	lang := r.Language
	back := backButton(lang, "tickets", "", "")
	if r.Access.Has("support") {
		back = backButton(lang, "support_queue", "", "")
	}
	switch name {
	case "representative_apply", "representative_application_input":
		if r.Access.RepresentativeID != "owner" || r.Access.Role != "customer" {
			return Result{}, ErrAccess
		}
		if name == "representative_apply" {
			out := oneScreen(tr(lang, "🤝 <b>درخواست نمایندگی</b>\nنام و توضیح کوتاهی دربارهٔ فعالیت و نیاز خود بفرستید. درخواست برای بررسی در پشتیبانی هاب ثبت می‌شود و پاسخ را در همین ربات پیگیری می‌کنید.", "🤝 <b>Representative application</b>\nSend your name and a short description of your business and requirements. HUB support will review your request; follow the reply in this bot."), [][]Button{{homeButton(lang)}})
			out.Input = &Action{Name: "representative_application_input"}
			return out, nil
		}
		body := strings.TrimSpace(r.Incoming.Text)
		if r.Incoming.Kind != "message" || !validText(body, 3500) {
			return Result{}, ErrProtocol
		}
		receipt, err := b.store.TelegramCreateTicketV1(ctx, a, business.TelegramTicketCreateV1{EventID: r.EventID, Subject: tr(lang, "درخواست نمایندگی", "Representative application"), Text: body})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "✅ درخواست نمایندگی برای بررسی ثبت شد.", "✅ Your representative application was submitted for review."), Buttons: [][]Button{{actionButton(tr(lang, "پیگیری درخواست", "Track application"), "primary", "ticket", receipt.Ticket.ID, "")}, {homeButton(lang)}}}}, nil
	case "tickets", "support_queue":
		own := name != "support_queue"
		if !own && !r.Access.Has("support") {
			return Result{}, ErrAccess
		}
		offset := parsePage(r.Action.Value)
		tickets, err := b.store.TelegramTicketsV1(ctx, a, business.TelegramTicketFilterV1{Own: own, Limit: 9, Offset: offset})
		if err != nil {
			return Result{}, err
		}
		rows := [][]Button{}
		for _, t := range tickets[:min(8, len(tickets))] {
			rows = append(rows, []Button{actionButton(clipped(t.Subject, 70)+" · "+ticketState(lang, t.Status), "", "ticket", t.ID, "")})
		}
		if offset > 0 {
			rows = append(rows, []Button{actionButton(tr(lang, "قبلی", "Previous"), "", name, "", strconv.Itoa(max(0, offset-8)))})
		}
		if len(tickets) > 8 {
			rows = append(rows, []Button{actionButton(tr(lang, "بعدی", "Next"), "", name, "", strconv.Itoa(offset+8))})
		}
		rows = append(rows, []Button{actionButton(tr(lang, "➕ تیکت جدید", "➕ New ticket"), "success", "ticket_new", "", "")}, []Button{homeButton(lang)})
		return Result{ClearInput: true, Screen: &Screen{Text: tr(lang, "💬 <b>پشتیبانی</b>", "💬 <b>Support</b>"), Buttons: rows}}, nil
	case "ticket_new":
		out := oneScreen(tr(lang, "پیام خود را بفرستید. اگر از صفحهٔ سرویس آمده‌اید، تیکت به همان سرویس وصل می‌شود.", "Send your message. If you opened support from a service, the ticket will be linked to that service."), [][]Button{{back}})
		out.Input = &Action{Name: "ticket_input", Resource: r.Action.Resource}
		return out, nil
	case "ticket_input":
		text := strings.TrimSpace(r.Incoming.Text)
		if !validText(text, 3500) {
			return Result{}, ErrProtocol
		}
		subject := clipped(strings.SplitN(text, "\n", 2)[0], 100)
		receipt, err := b.store.TelegramCreateTicketV1(ctx, a, business.TelegramTicketCreateV1{EventID: r.EventID, ClientID: r.Action.Resource, Subject: subject, Text: text})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "✅ تیکت ثبت شد.", "✅ Ticket created."), Buttons: [][]Button{{actionButton(tr(lang, "مشاهدهٔ تیکت", "View ticket"), "primary", "ticket", receipt.Ticket.ID, "")}, {back}}}}, nil
	case "ticket_reply":
		if _, err := b.store.TelegramTicketByIDV1(ctx, a, r.Action.Resource); err != nil {
			return Result{}, err
		}
		out := oneScreen(tr(lang, "پاسخ خود را بفرستید.", "Send your reply."), [][]Button{{backButton(lang, "ticket", r.Action.Resource, "")}})
		out.Input = &Action{Name: "reply_input", Resource: r.Action.Resource}
		return out, nil
	case "reply_input":
		text := strings.TrimSpace(r.Incoming.Text)
		if !validText(text, 3500) {
			return Result{}, ErrProtocol
		}
		receipt, err := b.store.TelegramReplyTicketV1(ctx, a, business.TelegramTicketReplyV1{EventID: r.EventID, TicketID: r.Action.Resource, Text: text})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "✅ پاسخ ثبت شد.", "✅ Reply recorded."), Buttons: [][]Button{{actionButton(tr(lang, "مشاهدهٔ تیکت", "View ticket"), "primary", "ticket", receipt.Ticket.ID, "")}, {back}}}}, nil
	case "ticket_close", "ticket_reopen", "ticket_claim", "ticket_unassign":
		t, err := b.store.TelegramTicketByIDV1(ctx, a, r.Action.Resource)
		if err != nil {
			return Result{}, err
		}
		revision, err := strconv.ParseInt(r.Action.Value, 10, 64)
		if err != nil {
			return Result{}, ErrProtocol
		}
		status, assignee := t.Status, t.AssignedTelegramUserID
		message := tr(lang, "تیکت بسته شد.", "Ticket closed.")
		switch name {
		case "ticket_close":
			status = "closed"
		case "ticket_reopen":
			status = "open"
			message = tr(lang, "تیکت دوباره باز شد؛ می‌توانید گفتگو را ادامه دهید.", "Ticket reopened. You can continue the conversation.")
		case "ticket_claim":
			if !r.Access.Has("support") || t.Status == "closed" || assignee != 0 && assignee != r.Access.Identity.UserID {
				return Result{}, ErrAccess
			}
			assignee = r.Access.Identity.UserID
			message = tr(lang, "پیگیری تیکت به شما سپرده شد.", "You are now assigned to this ticket.")
		case "ticket_unassign":
			if !r.Access.Has("support") || assignee != r.Access.Identity.UserID {
				return Result{}, ErrAccess
			}
			assignee = 0
			message = tr(lang, "تیکت به صف رسیدگی برگشت.", "Ticket returned to the support queue.")
		}
		_, err = b.store.TelegramSetTicketStateV1(ctx, a, business.TelegramTicketChangeV1{EventID: r.EventID, TicketID: t.ID, ExpectedRevision: revision, Status: status, AssignedTelegramUserID: assignee})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: message, Buttons: [][]Button{{actionButton(tr(lang, "مشاهدهٔ تیکت", "View ticket"), "primary", "ticket", t.ID, "")}, {back}}}}, nil
	case "ticket":
		t, err := b.store.TelegramTicketByIDV1(ctx, a, r.Action.Resource)
		if err != nil {
			return Result{}, err
		}
		offset, part := threadPage(r.Action.Value)
		messages, err := b.store.TelegramTicketMessagesV1(ctx, a, t.ID, 2, offset)
		if err != nil {
			return Result{}, err
		}
		text := "<b>" + escaped(t.Subject) + "</b>\n" + tr(lang, "وضعیت: ", "Status: ") + escaped(ticketState(lang, t.Status))
		if r.Access.Has("support") && t.AssignedTelegramUserID > 0 {
			text += "\n" + tr(lang, "مسئول رسیدگی: ", "Assigned supporter: ") + strconv.FormatInt(t.AssignedTelegramUserID, 10)
		}
		rows := [][]Button{}
		if len(messages) > 0 {
			m := messages[0]
			who := tr(lang, "مشتری", "Customer")
			if m.FromSupport {
				who = tr(lang, "پشتیبانی", "Support")
			}
			pages := escapedChunks(m.Text, 2800)
			part = min(part, len(pages)-1)
			text += "\n\n<b>" + who + "</b> · " + time.Unix(m.CreatedUnix, 0).UTC().Format("2006-01-02 15:04 UTC") + "\n" + pages[part]
			if len(pages) > 1 {
				text += fmt.Sprintf(tr(lang, "\n\nبخش %d از %d", "\n\nPart %d of %d"), part+1, len(pages))
			}
			if m.Attachment != nil {
				text += "\n" + tr(lang, "پیوست در پروندهٔ تیکت ثبت شده است.", "An attachment is retained in the ticket record.")
			}
			nav := []Button{}
			if part > 0 {
				nav = append(nav, actionButton(tr(lang, "بخش قبلی", "Previous part"), "", "ticket", t.ID, fmt.Sprintf("%d:%d", offset, part-1)))
			} else if offset > 0 {
				nav = append(nav, actionButton(tr(lang, "پیام قدیمی‌تر", "Older message"), "", "ticket", t.ID, fmt.Sprintf("%d:0", offset-1)))
			}
			if part+1 < len(pages) {
				nav = append(nav, actionButton(tr(lang, "ادامهٔ پیام", "Continue message"), "", "ticket", t.ID, fmt.Sprintf("%d:%d", offset, part+1)))
			} else if len(messages) > 1 {
				nav = append(nav, actionButton(tr(lang, "پیام جدیدتر", "Newer message"), "", "ticket", t.ID, fmt.Sprintf("%d:0", offset+1)))
			}
			if len(nav) > 0 {
				rows = append(rows, nav)
			}
		}
		rows = append(rows, []Button{actionButton(tr(lang, "تازه‌سازی گفتگو", "Refresh thread"), "", "ticket", t.ID, fmt.Sprintf("%d:%d", offset, part))})
		if t.Status != "closed" {
			rows = append(rows, []Button{actionButton(tr(lang, "✍️ پاسخ", "✍️ Reply"), "primary", "ticket_reply", t.ID, ""), actionButton(tr(lang, "بستن تیکت", "Close ticket"), "", "ticket_close", t.ID, strconv.FormatInt(t.Revision, 10))})
			if r.Access.Has("support") {
				if t.AssignedTelegramUserID == 0 {
					rows = append(rows, []Button{actionButton(tr(lang, "🙋 قبول پیگیری", "🙋 Assign to me"), "", "ticket_claim", t.ID, strconv.FormatInt(t.Revision, 10))})
				} else if t.AssignedTelegramUserID == r.Access.Identity.UserID {
					rows = append(rows, []Button{actionButton(tr(lang, "بازگرداندن به صف", "Return to queue"), "", "ticket_unassign", t.ID, strconv.FormatInt(t.Revision, 10))})
				}
			}
		} else {
			rows = append(rows, []Button{actionButton(tr(lang, "🔓 بازگشایی تیکت", "🔓 Reopen ticket"), "primary", "ticket_reopen", t.ID, strconv.FormatInt(t.Revision, 10))})
		}
		rows = append(rows, []Button{back})
		return oneScreen(text, rows), nil
	}
	return Result{}, ErrProtocol
}

func ticketState(lang, state string) string {
	switch state {
	case "open":
		return tr(lang, "باز", "Open")
	case "waiting_support":
		return tr(lang, "منتظر پشتیبانی", "Waiting for support")
	case "waiting_customer":
		return tr(lang, "منتظر پاسخ شما", "Waiting for customer")
	case "closed":
		return tr(lang, "بسته‌شده", "Closed")
	default:
		return tr(lang, "نیازمند بررسی", "Requires review")
	}
}

func (b *BusinessBackend) campaigns(ctx context.Context, r Request, name string) (Result, error) {
	a := actor(r.Access.Identity)
	lang := r.Language
	back := backButton(lang, "campaigns", "", "")
	if name == "consent" {
		c, err := b.store.TelegramMarketingConsentV1(ctx, a)
		if err != nil {
			return Result{}, err
		}
		state := tr(lang, "خاموش", "Off")
		if c.Enabled {
			state = tr(lang, "روشن", "On")
		}
		return oneScreen(tr(lang, "اعلان تبلیغاتی: ", "Marketing notifications: ")+state, [][]Button{{actionButton(tr(lang, "فعال‌کردن", "Enable"), "success", "consent_set", "", "on"), actionButton(tr(lang, "خاموش‌کردن", "Disable"), "", "consent_set", "", "off")}, {homeButton(lang)}}), nil
	}
	if name == "consent_set" {
		if r.Action.Value != "on" && r.Action.Value != "off" {
			return Result{}, ErrProtocol
		}
		_, err := b.store.TelegramSetMarketingConsentV1(ctx, a, r.EventID, r.Action.Value == "on")
		if err != nil {
			return Result{}, err
		}
		return oneScreen(tr(lang, "تنظیم دریافت پیام تبلیغاتی ذخیره شد.", "Marketing preference saved."), [][]Button{{homeButton(lang)}}), nil
	}
	if !r.Access.Has("broadcast_draft") && !r.Access.Has("broadcast_send") {
		return Result{}, ErrAccess
	}
	switch name {
	case "campaigns":
		offset := parsePage(r.Action.Value)
		list, err := b.store.TelegramCampaignsV1(ctx, a, 9, offset)
		if err != nil {
			return Result{}, err
		}
		rows := [][]Button{}
		for _, c := range list[:min(8, len(list))] {
			rows = append(rows, []Button{actionButton(clipped(c.Title, 60)+" · "+c.State, "", "campaign", c.ID, "")})
		}
		if len(list) > 8 {
			rows = append(rows, []Button{actionButton(tr(lang, "بعدی", "Next"), "", "campaigns", "", strconv.Itoa(offset+8))})
		}
		if offset > 0 {
			rows = append(rows, []Button{actionButton(tr(lang, "قبلی", "Previous"), "", "campaigns", "", strconv.Itoa(max(0, offset-8)))})
		}
		if r.Access.Has("broadcast_draft") {
			rows = append(rows, []Button{actionButton(tr(lang, "➕ پیام جدید", "➕ New broadcast"), "success", "campaign_new", "", "")})
		}
		rows = append(rows, []Button{backButton(lang, "manage", "", "")})
		return oneScreen(tr(lang, "📣 <b>پیام‌های همگانی</b>", "📣 <b>Broadcast campaigns</b>"), rows), nil
	case "campaign_new":
		if !r.Access.Has("broadcast_draft") {
			return Result{}, ErrAccess
		}
		return oneScreen(tr(lang, "گروه مخاطبان دارای رضایت را انتخاب کنید. وضعیت فعال براساس پروندهٔ سرویس است.", "Choose an audience of consenting customers. Active status is based on service records."), [][]Button{{actionButton(tr(lang, "همه", "All"), "", "campaign_prompt", "all", "")}, {actionButton(tr(lang, "دارای سرویس فعال", "With active services"), "", "campaign_prompt", "active", "")}, {actionButton(tr(lang, "دارای سرویس منقضی", "With expired services"), "", "campaign_prompt", "expired", "")}, {actionButton(tr(lang, "انقضا تا ۳ روز آینده", "Expiring within 3 days"), "", "campaign_prompt", "expiring", "")}, {back}}), nil
	case "campaign_prompt":
		if !r.Access.Has("broadcast_draft") || !campaignAudience(r.Action.Resource) {
			return Result{}, ErrAccess
		}
		out := oneScreen(tr(lang, "متن پیام همگانی را بفرستید. ابتدا پیش‌نویس ساخته می‌شود؛ ارسال پس از نمایش مخاطبان و تأیید شماست.", "Send the broadcast text. This creates a draft; sending requires an audience preview and your confirmation."), [][]Button{{back}})
		out.Input = &Action{Name: "campaign_input", Resource: r.Action.Resource}
		return out, nil
	case "campaign_input":
		if !campaignAudience(r.Action.Resource) {
			return Result{}, ErrProtocol
		}
		text := strings.TrimSpace(r.Incoming.Text)
		if !validText(text, 3500) {
			return Result{}, ErrProtocol
		}
		days := int64(0)
		if r.Action.Resource == "expiring" {
			days = 3
		}
		c, err := b.store.TelegramSaveCampaignV1(ctx, a, business.TelegramCampaignSaveV1{EventID: r.EventID, Title: clipped(strings.SplitN(text, "\n", 2)[0], 100), Content: business.TelegramCampaignContentV1{Text: text}, Audience: r.Action.Resource, ExpiringDays: days})
		if err != nil {
			return Result{}, err
		}
		return Result{ClearInput: true, Receipt: true, Screen: &Screen{Text: tr(lang, "پیش‌نویس ساخته شد؛ هنوز ارسال نشده است.", "Draft created; no messages have been sent."), Buttons: [][]Button{{actionButton(tr(lang, "پیش‌نمایش مخاطبان و پیام", "Preview message and audience"), "primary", "campaign_preview", c.Campaign.ID, "")}, {back}}}}, nil
	case "campaign_preview":
		p, err := b.store.TelegramPreviewCampaignV1(ctx, a, r.Action.Resource)
		if err != nil {
			return Result{}, err
		}
		pages := escapedChunks(p.Campaign.Content.Text, 2800)
		part := min(parsePage(r.Action.Value), len(pages)-1)
		text := "<b>" + escaped(p.Campaign.Title) + "</b>\n\n" + pages[part] + fmt.Sprintf(tr(lang, "\n\nمخاطبان دارای رضایت: %d\nبخش %d از %d · ارسال با صف انجام می‌شود.", "\n\nConsenting recipients: %d\nPart %d of %d · Delivery uses a queued send process."), p.RecipientCount, part+1, len(pages))
		rows := [][]Button{}
		if part > 0 {
			rows = append(rows, []Button{actionButton(tr(lang, "بخش قبلی", "Previous part"), "", "campaign_preview", p.Campaign.ID, strconv.Itoa(part-1))})
		}
		if part+1 < len(pages) {
			rows = append(rows, []Button{actionButton(tr(lang, "ادامهٔ پیش‌نمایش", "Continue preview"), "primary", "campaign_preview", p.Campaign.ID, strconv.Itoa(part+1))})
		}
		if r.Access.Has("broadcast_send") && part+1 == len(pages) {
			rows = append(rows, []Button{actionButton(tr(lang, "✅ تأیید و آغاز ارسال", "✅ Confirm and start sending"), "success", "campaign_approve", p.Campaign.ID, p.ID+":"+strconv.FormatInt(p.Campaign.Revision, 10)+":"+p.Digest)})
		}
		rows = append(rows, []Button{back})
		return oneScreen(text, rows), nil
	case "campaign_approve":
		parts := strings.Split(r.Action.Value, ":")
		if len(parts) != 3 {
			return Result{}, ErrProtocol
		}
		revision, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return Result{}, ErrProtocol
		}
		c, err := b.store.TelegramApproveCampaignV1(ctx, a, business.TelegramCampaignApprovalV1{EventID: r.EventID, CampaignID: r.Action.Resource, ExpectedRevision: revision, PreviewID: parts[0], Digest: parts[2]})
		if err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, Screen: &Screen{Text: tr(lang, "ارسال تأیید و در صف قرار گرفت.", "Broadcast approved and queued."), Buttons: [][]Button{{actionButton(tr(lang, "وضعیت ارسال", "Delivery status"), "primary", "campaign", c.Campaign.ID, "")}, {back}}}}, nil
	case "campaign_pause", "campaign_cancel", "campaign_resume":
		value := r.Action.Value
		digest := ""
		if name == "campaign_resume" {
			parts := strings.Split(value, ":")
			if len(parts) != 2 || !hexDigest(parts[1]) {
				return Result{}, ErrProtocol
			}
			value, digest = parts[0], parts[1]
		}
		revision, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return Result{}, ErrProtocol
		}
		action := "pause"
		if name == "campaign_cancel" {
			action = "cancel"
		}
		if name == "campaign_resume" {
			action = "resume"
		}
		_, err = b.store.TelegramControlCampaignV1(ctx, a, business.TelegramCampaignControlV1{EventID: r.EventID, CampaignID: r.Action.Resource, ExpectedRevision: revision, Action: action, ApprovalDigest: digest})
		if err != nil {
			return Result{}, err
		}
		return oneScreen(tr(lang, "وضعیت صف به‌روزرسانی شد؛ پیام‌های ارسال‌شده پس گرفته نمی‌شوند.", "Queue state updated; messages already sent are not withdrawn."), [][]Button{{back}}), nil
	case "campaign", "campaign_resume_preview":
		report, err := b.store.TelegramCampaignReportV1(ctx, a, r.Action.Resource)
		if err != nil {
			return Result{}, err
		}
		c := report.Campaign
		if name == "campaign_resume_preview" {
			if c.State != "paused" || !r.Access.Has("broadcast_send") || !hexDigest(c.ApprovalDigest) {
				return Result{}, ErrAccess
			}
			pages := escapedChunks(c.Content.Text, 2800)
			part := min(parsePage(r.Action.Value), len(pages)-1)
			text := "<b>" + escaped(c.Title) + "</b>\n\n" + pages[part] + fmt.Sprintf(tr(lang, "\n\nادامهٔ همان صف تأییدشده: %d پیام مانده. مخاطب جدیدی اضافه نمی‌شود. رضایت و دسترسی هنگام ارسال دوباره بررسی می‌شوند.\nبخش %d از %d", "\n\nResume the same approved queue: %d pending messages. No new recipients are added. Consent and permission are rechecked at dispatch.\nPart %d of %d"), report.Pending, part+1, len(pages))
			rows := [][]Button{}
			if part > 0 {
				rows = append(rows, []Button{actionButton(tr(lang, "بخش قبلی", "Previous part"), "", "campaign_resume_preview", c.ID, strconv.Itoa(part-1))})
			}
			if part+1 < len(pages) {
				rows = append(rows, []Button{actionButton(tr(lang, "ادامهٔ پیش‌نمایش", "Continue preview"), "primary", "campaign_resume_preview", c.ID, strconv.Itoa(part+1))})
			} else {
				rows = append(rows, []Button{actionButton(tr(lang, "✅ تأیید ادامهٔ ارسال", "✅ Confirm resume"), "success", "campaign_resume", c.ID, strconv.FormatInt(c.Revision, 10)+":"+c.ApprovalDigest)})
			}
			rows = append(rows, []Button{back})
			return oneScreen(text, rows), nil
		}
		text := fmt.Sprintf(tr(lang, "📣 <b>%s</b>\nوضعیت: %s\nدر صف: %d\nارسال موفق: %d\nناموفق: %d\nنامعلوم: %d\nلغوشده: %d\n\nاین اعداد گزارش خوانده‌شدن پیام نیستند.", "📣 <b>%s</b>\nState: %s\nPending: %d\nSent: %d\nFailed: %d\nUnknown: %d\nCancelled: %d\n\nThese figures are delivery outcomes, not read receipts."), escaped(c.Title), escaped(c.State), report.Pending, report.Sent, report.Failed, report.Unknown, report.Cancelled)
		rows := [][]Button{{actionButton(tr(lang, "تازه‌سازی", "Refresh"), "primary", "campaign", c.ID, "")}}
		if c.State == "draft" {
			rows = append(rows, []Button{actionButton(tr(lang, "پیش‌نمایش", "Preview"), "", "campaign_preview", c.ID, "")})
		}
		if c.State == "queued" {
			rows = append(rows, []Button{actionButton(tr(lang, "مکث ارسال", "Pause"), "", "campaign_pause", c.ID, strconv.FormatInt(c.Revision, 10))})
		}
		if c.State == "paused" && r.Access.Has("broadcast_send") {
			rows = append(rows, []Button{actionButton(tr(lang, "پیش‌نمایش و ادامهٔ ارسال", "Review and resume"), "primary", "campaign_resume_preview", c.ID, "")})
		}
		if c.State == "queued" || c.State == "paused" {
			rows = append(rows, []Button{actionButton(tr(lang, "لغو ادامهٔ صف", "Cancel pending"), "danger", "campaign_cancel", c.ID, strconv.FormatInt(c.Revision, 10))})
		}
		rows = append(rows, []Button{back})
		return oneScreen(text, rows), nil
	}
	return Result{}, ErrProtocol
}

func (b *BusinessBackend) rewards(ctx context.Context, r Request, name string) (Result, error) {
	a := actor(r.Access.Identity)
	lang := r.Language
	if name == "referral" {
		s, err := b.store.TelegramReferralV1(ctx, a)
		if err != nil {
			return Result{}, err
		}
		text := fmt.Sprintf(tr(lang, "🎁 <b>معرفی به دوستان</b>\nدعوت‌شده‌ها: %d\nخرید واجد شرایط: %d\nپاداش در انتظار: %d ⭐\nپاداش آزادشده: %d ⭐\n۳۰ روز اخیر: %d ⭐", "🎁 <b>Invite friends</b>\nInvited customers: %d\nQualifying purchases: %d\nPending rewards: %d ⭐\nReleased rewards: %d ⭐\nLast 30 days: %d ⭐"), s.InvitedCustomers, s.QualifiedPurchases, s.PendingStars, s.ReleasedStars, s.Recent30DayStars)
		rows := [][]Button{}
		if s.Policy.Enabled && (s.Policy.ExpiresUnix == 0 || s.Policy.ExpiresUnix > time.Now().Unix()) && r.Incoming.BotUsername != "" {
			link := "https://t.me/" + r.Incoming.BotUsername + "?start=" + url.QueryEscape(s.StartPayload)
			text += "\n\n<code>" + escaped(link) + "</code>\n" + tr(lang, "پاداش پس از پرداخت، آماده‌شدن و تحویل تأییدشدهٔ سرویس و زمان انتظار آزاد می‌شود.", "Rewards release after payment, verified service delivery and the configured hold period.")
			rows = append(rows, []Button{{Text: tr(lang, "📋 کپی لینک دعوت", "📋 Copy invite link"), CopyText: link}}, []Button{{Text: tr(lang, "↗️ معرفی به دوستان", "↗️ Share with friends"), URL: "https://t.me/share/url?url=" + url.QueryEscape(link)}})
		} else {
			text += "\n\n" + tr(lang, "دعوت پاداش‌دار اکنون فعال نیست.", "Rewarded invitations are not currently enabled.")
		}
		text += "\n\n" + referralPolicyText(lang, s.Policy)
		rows = append(rows, []Button{actionButton(tr(lang, "💳 کیف پاداش", "💳 Reward wallet"), "", "wallet", "", "")})
		rows = append(rows, []Button{homeButton(lang)})
		return oneScreen(text, rows), nil
	}
	offset := parsePage(r.Action.Value)
	items, err := b.store.TelegramRewardHistoryV1(ctx, a, 9, offset)
	if err != nil {
		return Result{}, err
	}
	text := tr(lang, "🏅 <b>تاریخچهٔ پاداش</b>", "🏅 <b>Reward history</b>")
	for _, item := range items[:min(8, len(items))] {
		text += fmt.Sprintf("\n%d ⭐ · %s", item.AmountStars, escaped(item.State))
	}
	if len(items) == 0 {
		text += "\n" + tr(lang, "هنوز پاداشی ثبت نشده است.", "No rewards have been recorded yet.")
	}
	rows := [][]Button{}
	if nav := pageButtons(lang, "rewards", "", offset, offset+len(items)); len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, []Button{actionButton(tr(lang, "💳 کیف پاداش", "💳 Reward wallet"), "", "wallet", "", ""), homeButton(lang)})
	return oneScreen(text, rows), nil
}

func campaignAudience(s string) bool {
	return s == "all" || s == "active" || s == "expired" || s == "expiring"
}
func threadPage(s string) (int, int) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return parsePage(s), 0
	}
	return parsePage(parts[0]), parsePage(parts[1])
}

// Split after escaping, without clipping or losing any original character.
// Each part fits inside a Telegram HTML message together with its controls.
func escapedChunks(text string, budget int) []string {
	parts := []string{}
	var part strings.Builder
	for _, r := range text {
		encoded := escaped(string(r))
		if part.Len() > 0 && part.Len()+len(encoded) > budget {
			parts = append(parts, part.String())
			part.Reset()
		}
		part.WriteString(encoded)
	}
	if part.Len() > 0 || len(parts) == 0 {
		parts = append(parts, part.String())
	}
	return parts
}
