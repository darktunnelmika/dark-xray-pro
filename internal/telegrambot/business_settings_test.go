package telegrambot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestReferralRateInputUsesExactHundredthsAndLocalizedDigits(t *testing.T) {
	for input, want := range map[string]int64{"0": 0, "10": 1000, "3.5": 350, "100.00": 10000, "۱۲٫۳۴": 1234, "١٠": 1000} {
		got, err := settingRate(input)
		if err != nil || got != want {
			t.Fatal(input, got, err)
		}
	}
	for _, input := range []string{"100.01", "-1", "1e2", "1.234", "1.", ".5", "+10", "NaN", "1..2", "", "1000"} {
		if _, err := settingRate(input); err == nil {
			t.Fatal("invalid financial rate accepted", input)
		}
	}
}

func TestReferralSettingsPreserveAndDisplayPolicyExpiry(t *testing.T) {
	f := businessSetup(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(7 * 24 * time.Hour).Unix()
	policy := business.TelegramReferralPolicyV1{Enabled: true, Mode: "percent_bps", FirstPurchase: 1000, RepeatPurchase: 300, MaxPerOrderStars: 100, PerReferrerCapStars: 1000, HoldHours: 24, AttributionDays: 30, ExpiresUnix: expires}
	if _, err := f.store.TelegramSetReferralPolicyV1(ctx, actor(f.manager), business.TelegramReferralPolicyChangeV1{EventID: "tg-settings-expiry-initial-0001", Policy: policy}); err != nil {
		t.Fatal(err)
	}
	view, err := f.backend.Handle(ctx, f.request(t, f.manager, Action{Name: "referral_policy"}))
	date := time.Unix(expires, 0).UTC().Format("2006-01-02 15:04 UTC")
	if err != nil || view.Screen == nil || !strings.Contains(view.Screen.Text, date) || !strings.Contains(view.Screen.Text, "State: On") {
		t.Fatal("policy state or expiry is hidden", err)
	}
	edit := actionNamed(view.Screen, "referral_policy_input")
	if edit == nil {
		t.Fatal("missing policy edit")
	}
	prompt, err := f.backend.Handle(ctx, f.request(t, f.manager, *edit))
	if err != nil || prompt.Input == nil {
		t.Fatal("missing input continuation", err)
	}
	r := f.request(t, f.manager, *prompt.Input)
	r.Incoming.Kind, r.Incoming.Text = "message", "۱۲٫۵ ۴ ۱۲۰ ۲۰۰۰ ۴۸ ۶۰"
	preview, err := f.backend.Handle(ctx, r)
	if err != nil || preview.Screen == nil || !strings.Contains(preview.Screen.Text, date) {
		t.Fatal("confirmation lost end date", err)
	}
	confirm := actionNamed(preview.Screen, "referral_policy_confirm")
	var change business.TelegramReferralPolicyChangeV1
	if confirm == nil || json.Unmarshal([]byte(confirm.Value), &change) != nil || change.Policy.ExpiresUnix != expires || change.ExpectedRevision != 1 || change.Policy.FirstPurchase != 1250 {
		t.Fatal("editing silently replaced immutable confirmation terms")
	}
	if _, err = f.backend.Handle(ctx, f.request(t, f.manager, *confirm)); err != nil {
		t.Fatal(err)
	}
	saved, err := f.store.GetTelegramReferralPolicyV1(ctx, actor(f.manager))
	if err != nil || saved.ExpiresUnix != expires || saved.Revision != 2 || saved.FirstPurchase != 1250 {
		t.Fatal("save reset policy expiry", saved, err)
	}
	policy.ExpiresUnix = time.Now().Unix() - 60
	if text := referralPolicyText("en", policy); !strings.Contains(text, "State: Expired") {
		t.Fatal("expired policy appears active")
	}
	policy.ExpiresUnix = 0
	if text := referralPolicyText("en", policy); !strings.Contains(text, "Policy end: No end date") {
		t.Fatal("unbounded policy omitted from review")
	}
}
