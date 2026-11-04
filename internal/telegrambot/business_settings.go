package telegrambot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func settingDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '۰' && r <= '۹' {
			return '0' + r - '۰'
		}
		if r >= '٠' && r <= '٩' {
			return '0' + r - '٠'
		}
		if r == '٫' {
			return '.'
		}
		return r
	}, s)
}

// Parse hundredths of a percent without floating point money/rate rounding.
func settingRate(s string) (int64, error) {
	s = settingDigits(s)
	parts := strings.Split(s, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 3 {
		return 0, ErrProtocol
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0, ErrProtocol
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > 100 {
		return 0, ErrProtocol
	}
	fraction := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, ErrProtocol
		}
		for _, r := range parts[1] {
			if r < '0' || r > '9' {
				return 0, ErrProtocol
			}
		}
		fraction, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, ErrProtocol
		}
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	rate := whole*100 + fraction
	if rate > 10000 {
		return 0, ErrProtocol
	}
	return rate, nil
}

func settingsValue(value any) string { raw, _ := json.Marshal(value); return string(raw) }

type referralPolicyEdit struct {
	Revision    int64 `json:"revision"`
	ExpiresUnix int64 `json:"expires_unix"`
}

func referralPolicyText(lang string, p business.TelegramReferralPolicyV1) string {
	state := tr(lang, "خاموش", "Off")
	if p.Enabled {
		state = tr(lang, "روشن", "On")
		if p.ExpiresUnix != 0 && p.ExpiresUnix <= time.Now().Unix() {
			state = tr(lang, "مهلت تمام شده", "Expired")
		}
	}
	expiry := tr(lang, "بدون تاریخ پایان", "No end date")
	if p.ExpiresUnix > 0 {
		expiry = time.Unix(p.ExpiresUnix, 0).UTC().Format("2006-01-02 15:04 UTC")
	}
	rate := fmt.Sprintf("%d / %d XTR", p.FirstPurchase, p.RepeatPurchase)
	if p.Mode == "percent_bps" {
		rate = fmt.Sprintf("%.2f%% / %.2f%%", float64(p.FirstPurchase)/100, float64(p.RepeatPurchase)/100)
	}
	return fmt.Sprintf(tr(lang, "🎁 <b>سیاست دعوت</b>\nوضعیت: %s\nخرید اول / خریدهای بعدی: %s\nسقف هر سفارش: %d XTR\nسقف هر معرف: %d XTR\nزمان انتظار: %d ساعت\nمهلت دعوت: %d روز\nپایان طرح: %s\n\nپاداش، اعتبار تبلیغاتی فروشگاه است. استفاده از آن در خرید به تنظیم «تخفیف با پاداش» بستگی دارد؛ برداشت نقدی فعال نیست. شروط دعوت‌های قبلی حفظ می‌شوند.", "🎁 <b>Referral policy</b>\nState: %s\nFirst / repeat purchase: %s\nPer-order cap: %d XTR\nPer-referrer cap: %d XTR\nHold: %d hours\nAttribution window: %d days\nPolicy end: %s\n\nRewards are promotional store credit. Checkout use depends on the Bonus discount policy; cash withdrawal is not enabled. Existing invitations retain their original terms."), state, rate, p.MaxPerOrderStars, p.PerReferrerCapStars, p.HoldHours, p.AttributionDays, expiry)
}

func (b *BusinessBackend) settings(ctx context.Context, r Request, name string) (Result, error) {
	if !r.Access.Has("manage_store") {
		return Result{}, ErrAccess
	}
	if (strings.HasPrefix(name, "referral_policy") || strings.HasPrefix(name, "bonus_policy")) && !r.Access.Has("finance") {
		return Result{}, ErrAccess
	}
	a := actor(r.Access.Identity)
	lang := r.Language
	back := backButton(lang, "settings", "", "")
	switch name {
	case "settings":
		rows := [][]Button{{actionButton(tr(lang, "🧪 تنظیم تست ۲۴ساعته", "🧪 Configure 24-hour trial"), "", "trial_policy", "", "")}}
		if b.RewardsEnabled && r.Access.Has("finance") {
			rows = append(rows, []Button{actionButton(tr(lang, "🎁 سیاست پاداش دعوت", "🎁 Referral reward policy"), "", "referral_policy", "", "")})
		}
		if b.BonusEnabled && r.Access.Has("finance") {
			rows = append(rows, []Button{actionButton(tr(lang, "💳 تخفیف با پاداش", "💳 Bonus discount policy"), "", "bonus_policy", "", "")})
		}
		rows = append(rows, []Button{backButton(lang, "manage", "", "")})
		return oneScreen(tr(lang, "⚙️ <b>تنظیمات فروشگاه</b>\nتغییرها فقط در همین فروشگاه اعمال می‌شوند.", "⚙️ <b>Store settings</b>\nChanges apply only to this store."), rows), nil
	case "bonus_policy":
		if !b.BonusEnabled {
			return Result{}, ErrAccess
		}
		p, err := b.store.GetTelegramBonusPolicyV1(ctx, a)
		if err != nil {
			return Result{}, err
		}
		label := tr(lang, "فعال‌کردن استفاده از پاداش", "Enable bonus discounts")
		if p.Enabled {
			label = tr(lang, "غیرفعال‌کردن استفاده از پاداش", "Disable bonus discounts")
		}
		change := business.TelegramBonusPolicyChangeV1{ExpectedRevision: p.Revision, Enabled: !p.Enabled}
		return oneScreen(bonusPolicyText(lang, p.Enabled), [][]Button{{actionButton(label, "primary", "bonus_policy_preview", "", settingsValue(change))}, {back}}), nil
	case "bonus_policy_preview", "bonus_policy_confirm":
		var change business.TelegramBonusPolicyChangeV1
		if !b.BonusEnabled || len(r.Action.Value) > 1024 || decodeStrictJSON([]byte(r.Action.Value), &change) != nil || change.ExpectedRevision < 0 {
			return Result{}, ErrProtocol
		}
		if name == "bonus_policy_preview" {
			return oneScreen(bonusPolicyText(lang, change.Enabled)+"\n\n"+tr(lang, "تنظیم جدید را پیش از ذخیره بررسی کنید.", "Review the new setting before saving."), [][]Button{{actionButton(tr(lang, "✅ ذخیرهٔ همین تنظیم", "✅ Save this setting"), "success", "bonus_policy_confirm", "", settingsValue(change))}, {backButton(lang, "bonus_policy", "", "")}}), nil
		}
		change.EventID = r.EventID
		if _, err := b.store.SetTelegramBonusPolicyV1(ctx, a, change); err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "تنظیم تخفیف با پاداش ذخیره شد.", "Bonus discount policy saved."), Buttons: [][]Button{{backButton(lang, "bonus_policy", "", "")}}}}, nil
	case "trial_policy":
		p, err := b.store.GetTelegramTrialPolicy(ctx, a)
		if err != nil {
			return Result{}, err
		}
		state := tr(lang, "خاموش", "Off")
		if p.Enabled {
			state = tr(lang, "روشن", "On")
		}
		text := fmt.Sprintf(tr(lang, "🧪 <b>تست ۲۴ساعته</b>\nوضعیت: %s\nحجم: %.2f MiB\nهر مشتری یک بار در این فروشگاه. زمان از صدور شروع می‌شود؛ آماده‌شدن اتصال جداگانه بررسی می‌شود. حجم تست نماینده از اعتبار اصلی او کم می‌شود.", "🧪 <b>24-hour trial</b>\nState: %s\nVolume: %.2f MiB\nOnce per customer in this store. Time starts at issuance; connection readiness is verified separately. Representative trials debit the native volume allowance."), state, float64(p.VolumeBytes)/(1<<20))
		rows := [][]Button{{actionButton(tr(lang, "تعیین حجم و فعال‌کردن", "Set volume and enable"), "primary", "trial_policy_input", "", strconv.FormatInt(p.Revision, 10))}}
		if p.Enabled {
			rows = append(rows, []Button{actionButton(tr(lang, "خاموش‌کردن تست جدید", "Disable new trials"), "", "trial_policy_confirm", "", settingsValue(business.TelegramTrialPolicyChange{ExpectedRevision: p.Revision, Enabled: false, VolumeBytes: p.VolumeBytes}))})
		}
		rows = append(rows, []Button{back})
		return oneScreen(text, rows), nil
	case "trial_policy_input":
		if r.Incoming.Kind != "message" {
			out := oneScreen(tr(lang, "حجم هر تست را به MiB بفرستید؛ مثلاً 100. پیش از ذخیره، نتیجه نمایش داده می‌شود.", "Send each trial's volume in MiB, for example 100. Review the result before saving."), [][]Button{{backButton(lang, "trial_policy", "", "")}})
			out.Input = &Action{Name: name, Value: r.Action.Value}
			return out, nil
		}
		revision, err := strconv.ParseInt(r.Action.Value, 10, 64)
		if err != nil || revision < 0 {
			return Result{}, ErrProtocol
		}
		volume, err := strconv.ParseInt(strings.TrimSpace(settingDigits(r.Incoming.Text)), 10, 64)
		if err != nil || volume < 1 || volume > business.MaxCatalogVolume/(1<<20) {
			return Result{}, ErrProtocol
		}
		in := business.TelegramTrialPolicyChange{ExpectedRevision: revision, Enabled: true, VolumeBytes: volume * (1 << 20)}
		out := oneScreen(fmt.Sprintf(tr(lang, "تأیید تنظیم تست: %d MiB، مدت ۲۴ ساعت از صدور، یک تست برای هر مشتری.\nتست جدید از اعتبار حجمی نماینده کسر می‌شود.", "Confirm trial settings: %d MiB, 24 hours from issuance, one trial per customer.\nNew trials debit the representative's native volume allowance."), volume), [][]Button{{actionButton(tr(lang, "✅ ذخیره و فعال‌کردن", "✅ Save and enable"), "success", "trial_policy_confirm", "", settingsValue(in))}, {back}})
		out.ClearInput = true
		return out, nil
	case "trial_policy_confirm":
		var in business.TelegramTrialPolicyChange
		if len(r.Action.Value) > 1024 || json.Unmarshal([]byte(r.Action.Value), &in) != nil {
			return Result{}, ErrProtocol
		}
		in.EventID = r.EventID
		if _, err := b.store.SetTelegramTrialPolicy(ctx, a, in); err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "تنظیم تست ذخیره شد.", "Trial settings saved."), Buttons: [][]Button{{backButton(lang, "trial_policy", "", "")}}}}, nil
	case "referral_policy":
		if !b.RewardsEnabled {
			return Result{}, ErrAccess
		}
		p, err := b.store.GetTelegramReferralPolicyV1(ctx, a)
		if err != nil {
			return Result{}, err
		}
		rows := [][]Button{{actionButton(tr(lang, "تعیین درصد و سقف‌ها", "Set percentage and caps"), "primary", "referral_policy_input", "", settingsValue(referralPolicyEdit{Revision: p.Revision, ExpiresUnix: p.ExpiresUnix}))}}
		if p.Enabled {
			revision := p.Revision
			disabled := p
			disabled.Revision = 0
			disabled.Enabled = false
			rows = append(rows, []Button{actionButton(tr(lang, "غیرفعال‌کردن دعوت جدید", "Disable new rewarded invitations"), "", "referral_policy_confirm", "", settingsValue(business.TelegramReferralPolicyChangeV1{ExpectedRevision: revision, Policy: disabled}))})
		}
		rows = append(rows, []Button{back})
		return oneScreen(referralPolicyText(lang, p), rows), nil
	case "referral_policy_input":
		if !b.RewardsEnabled {
			return Result{}, ErrAccess
		}
		if r.Incoming.Kind != "message" {
			out := oneScreen(tr(lang, "این شش عدد را با فاصله بفرستید:\nدرصد خرید اول، درصد خرید بعدی، سقف هر سفارش (XTR)، سقف کل هر معرف (XTR)، ساعت انتظار، مهلت دعوت به روز.\n\nنمونهٔ قالب: <code>10 3 100 1000 24 30</code>\nتاریخ پایان فعلی طرح حفظ می‌شود. این نمونه خودکار ذخیره نمی‌شود؛ نتیجهٔ ورودی شما پیش از فعال‌شدن نمایش داده می‌شود.", "Send six space-separated numbers:\nFirst-purchase percent, repeat percent, per-order cap (XTR), total per-referrer cap (XTR), hold hours, attribution days.\n\nFormat example: <code>10 3 100 1000 24 30</code>\nThe current policy end date is preserved. This example is not saved automatically. Review your entered terms before activation."), [][]Button{{backButton(lang, "referral_policy", "", "")}})
			out.Input = &Action{Name: name, Value: r.Action.Value}
			return out, nil
		}
		fields := strings.Fields(settingDigits(r.Incoming.Text))
		if len(fields) != 6 {
			return Result{}, ErrProtocol
		}
		first, err := settingRate(fields[0])
		if err != nil {
			return Result{}, err
		}
		repeat, err := settingRate(fields[1])
		if err != nil {
			return Result{}, err
		}
		var values [4]int64
		for i := range values {
			values[i], err = strconv.ParseInt(fields[i+2], 10, 64)
			if err != nil {
				return Result{}, ErrProtocol
			}
		}
		if first == 0 && repeat == 0 || values[0] < 1 || values[0] > 1_000_000_000 || values[1] < 1 || values[1] > 1_000_000_000 || values[2] < 0 || values[2] > 720 || values[3] < 1 || values[3] > 3650 {
			return Result{}, ErrProtocol
		}
		var edit referralPolicyEdit
		if len(r.Action.Value) > 1024 || json.Unmarshal([]byte(r.Action.Value), &edit) != nil || edit.Revision < 0 || edit.ExpiresUnix < 0 {
			return Result{}, ErrProtocol
		}
		in := business.TelegramReferralPolicyChangeV1{ExpectedRevision: edit.Revision, Policy: business.TelegramReferralPolicyV1{Enabled: true, Mode: "percent_bps", FirstPurchase: first, RepeatPurchase: repeat, MaxPerOrderStars: values[0], PerReferrerCapStars: values[1], HoldHours: values[2], AttributionDays: values[3], ExpiresUnix: edit.ExpiresUnix}}
		out := oneScreen(referralPolicyText(lang, in.Policy), [][]Button{{actionButton(tr(lang, "✅ ذخیرهٔ همین سیاست", "✅ Save these terms"), "success", "referral_policy_confirm", "", settingsValue(in))}, {back}})
		out.ClearInput = true
		return out, nil
	case "referral_policy_confirm":
		if !b.RewardsEnabled {
			return Result{}, ErrAccess
		}
		var in business.TelegramReferralPolicyChangeV1
		if len(r.Action.Value) > 2048 || json.Unmarshal([]byte(r.Action.Value), &in) != nil {
			return Result{}, ErrProtocol
		}
		in.EventID = r.EventID
		if _, err := b.store.TelegramSetReferralPolicyV1(ctx, a, in); err != nil {
			return Result{}, err
		}
		return Result{Receipt: true, ClearInput: true, Screen: &Screen{Text: tr(lang, "سیاست دعوت ذخیره شد. شروط دعوت‌های قبلی حفظ شدند.", "Referral policy saved. Earlier invitation terms are preserved."), Buttons: [][]Button{{backButton(lang, "referral_policy", "", "")}}}}, nil
	}
	return Result{}, ErrProtocol
}
