package hubcore

import (
	"context"
	"fmt"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// SubSettings shape what clients see in their apps and on the page.
type SubSettings struct {
	RemarkTemplate string `json:"remark_template"` // "" = "<remark> | <path>"
	InfoLines      bool   `json:"info_lines"`      // remaining volume and days as list entries
	Announce       string `json:"announce"`        // shown on the page and in apps that support it
	SupportURL     string `json:"support_url"`     // e.g. https://t.me/your_support
	Title          string `json:"title"`           // page and profile title ("" = client name)
	// Fragment settings for the Xray JSON format (?format=xray).
	Fragment         bool   `json:"fragment"`
	FragmentLength   string `json:"fragment_length"`
	FragmentInterval string `json:"fragment_interval"`
	Noise            bool   `json:"noise"`
	// Telegram Mini App store (the bot opens it; needs an HTTPS address).
	MiniApp    bool   `json:"miniapp"`
	MiniAppURL string `json:"miniapp_url"` // "" = <public address>/app/
	// Customer page branding.
	Accent string `json:"accent"` // #rrggbb, "" = the default neon
	Logo   string `json:"logo"`   // small image as a data: URL
	// Order configs by node health (on by default) and leave out a down
	// node's configs (off by default; never all of them).
	SmartOrder bool `json:"smart_order"`
	HideDown   bool `json:"hide_down"`
}

func (s *Store) SubSettings(ctx context.Context) SubSettings {
	return SubSettings{
		RemarkTemplate: s.Setting(ctx, "sub_remark"),
		InfoLines:      s.Setting(ctx, "sub_info") == "1",
		Announce:       s.Setting(ctx, "sub_announce"),
		SupportURL:     s.Setting(ctx, "sub_support"),
		Title:          s.Setting(ctx, "sub_title"),
		Fragment:       s.Setting(ctx, "sub_frag") == "1",
		FragmentLength: s.Setting(ctx, "sub_frag_len"), FragmentInterval: s.Setting(ctx, "sub_frag_int"),
		Noise:   s.Setting(ctx, "sub_noise") == "1",
		MiniApp: s.Setting(ctx, "miniapp") == "1", MiniAppURL: s.Setting(ctx, "miniapp_url"),
		Accent: s.Setting(ctx, "sub_accent"), Logo: s.Setting(ctx, "sub_logo"),
		SmartOrder: s.Setting(ctx, "sub_smart") != "0", HideDown: s.Setting(ctx, "sub_hide_down") == "1",
	}
}

func (s *Store) PutSubSettings(ctx context.Context, v SubSettings) error {
	if len(v.RemarkTemplate) > 200 || len(v.Announce) > 1000 || len(v.Title) > 80 {
		return fmt.Errorf("%w: too long", ErrInvalid)
	}
	if v.SupportURL != "" && !strings.HasPrefix(v.SupportURL, "https://") && !strings.HasPrefix(v.SupportURL, "tg://") {
		return fmt.Errorf("%w: support link must start with https:// or tg://", ErrInvalid)
	}
	info := ""
	if v.InfoLines {
		info = "1"
	}
	rangeRE := func(x string) bool {
		if x == "" {
			return true
		}
		var a, b int
		n, _ := fmt.Sscanf(x, "%d-%d", &a, &b)
		return n == 2 && a > 0 && b >= a && b <= 5000
	}
	if !rangeRE(v.FragmentLength) || !rangeRE(v.FragmentInterval) {
		return fmt.Errorf("%w: fragment ranges look like 100-200", ErrInvalid)
	}
	b01 := func(x bool) string {
		if x {
			return "1"
		}
		return ""
	}
	if v.Accent != "" && !accentRE.MatchString(v.Accent) {
		return fmt.Errorf("%w: colour must look like #2de2e6", ErrInvalid)
	}
	if v.Logo != "" && !validLogo(v.Logo) {
		return fmt.Errorf("%w: logo must be a PNG, JPEG, WebP or SVG image under 150 KB", ErrInvalid)
	}
	if err := s.SetSetting(ctx, "sub_accent", v.Accent); err != nil {
		return err
	}
	if err := s.SetSetting(ctx, "sub_logo", v.Logo); err != nil {
		return err
	}
	v.MiniAppURL = strings.TrimSpace(v.MiniAppURL)
	if v.MiniAppURL != "" && !strings.HasPrefix(v.MiniAppURL, "https://") {
		return fmt.Errorf("%w: Telegram opens Mini Apps only over https://", ErrInvalid)
	}
	smart := "0"
	if v.SmartOrder {
		smart = "1"
	}
	for k, val := range map[string]string{"sub_smart": smart, "sub_hide_down": b01(v.HideDown), "miniapp": b01(v.MiniApp), "miniapp_url": v.MiniAppURL, "sub_frag": b01(v.Fragment), "sub_frag_len": v.FragmentLength, "sub_frag_int": v.FragmentInterval, "sub_noise": b01(v.Noise)} {
		if err := s.SetSetting(ctx, k, val); err != nil {
			return err
		}
	}
	for k, val := range map[string]string{"sub_remark": strings.TrimSpace(v.RemarkTemplate), "sub_info": info, "sub_announce": strings.TrimSpace(v.Announce),
		"sub_support": strings.TrimSpace(v.SupportURL), "sub_title": strings.TrimSpace(v.Title)} {
		if err := s.SetSetting(ctx, k, val); err != nil {
			return err
		}
	}
	return nil
}

// gbText formats bytes as GB for config names and info lines.
func gbText(b int64) string {
	if b < 0 {
		return "∞"
	}
	g := float64(b) / (1 << 30)
	if g >= 10 {
		return fmt.Sprintf("%.0f GB", g)
	}
	return fmt.Sprintf("%.2f GB", g)
}

// subAccount adds the settings-driven naming to an account.
func (s *Store) subAccount(ctx context.Context, c Client, u Usage) sub.Account {
	set, _ := s.brandFor(ctx, c.Email)
	name := s.publicName(ctx, c.Email)
	acc := sub.Account{Email: name, UUID: c.UUID, Secret: c.Secret, IranDirect: s.Routing(ctx).ClientIranDirect, RemarkTemplate: set.RemarkTemplate}
	left := Remaining(c, u)
	exp := EffectiveExpiry(c, u)
	days := "∞"
	if exp > 0 {
		d := (exp - s.now().Unix() + 86399) / 86400
		if d < 0 {
			d = 0
		}
		days = fmt.Sprint(d)
	}
	total := "∞"
	if c.VolumeBytes >= 0 {
		total = gbText(c.VolumeBytes)
	}
	acc.Vars = map[string]string{"email": name, "used": gbText(u.Up + u.Down), "left": gbText(left), "total": total, "days": days}
	if set.Fragment {
		acc.Fragment = &sub.Fragment{Length: set.FragmentLength, Interval: set.FragmentInterval, Noise: set.Noise}
	}
	if set.InfoLines {
		// One line that says it all: who, volume left, time left. The
		// client's name lives here only, not on every config.
		vol := "📊 " + gbText(left) + " / " + total
		if c.VolumeBytes < 0 {
			vol = "📊 نامحدود"
		} else if left <= 0 {
			vol = "📊 حجم تمام شد"
		}
		var when string
		switch {
		case exp > 0 && exp <= s.now().Unix():
			when = "⏳ زمان تمام شد"
		case exp > 0:
			when = "⏳ " + days + " روز"
		case c.DurationSeconds > 0:
			when = fmt.Sprintf("⏳ %d روز از اولین اتصال", (c.DurationSeconds+86399)/86400)
		default:
			when = "⏳ بدون انقضا"
		}
		acc.InfoLines = []string{"👤 " + name + " | " + vol + " | " + when}
	}
	return acc
}
