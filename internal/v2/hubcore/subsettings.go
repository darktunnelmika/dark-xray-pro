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
		Noise: s.Setting(ctx, "sub_noise") == "1",
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
	for k, val := range map[string]string{"sub_frag": b01(v.Fragment), "sub_frag_len": v.FragmentLength, "sub_frag_int": v.FragmentInterval, "sub_noise": b01(v.Noise)} {
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
	set := s.SubSettings(ctx)
	acc := sub.Account{Email: c.Email, UUID: c.UUID, Secret: c.Secret, IranDirect: s.Routing(ctx).ClientIranDirect, RemarkTemplate: set.RemarkTemplate}
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
	acc.Vars = map[string]string{"email": c.Email, "used": gbText(u.Up + u.Down), "left": gbText(left), "total": total, "days": days}
	if set.Fragment {
		acc.Fragment = &sub.Fragment{Length: set.FragmentLength, Interval: set.FragmentInterval, Noise: set.Noise}
	}
	if set.InfoLines {
		acc.InfoLines = []string{"📊 " + gbText(left) + " / " + total, "⏳ " + days + " days"}
	}
	return acc
}
