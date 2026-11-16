package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// RepBrand is a reseller's own look for their customers: the subscription
// page, the names apps show, and optionally their own address for links.
// Empty fields fall back to the parent reseller's, then the panel's.
type RepBrand struct {
	Title          string `json:"title"`
	Announce       string `json:"announce"`
	SupportURL     string `json:"support_url"`
	Accent         string `json:"accent"`
	Logo           string `json:"logo"`
	RemarkTemplate string `json:"remark_template"`
	BaseURL        string `json:"base_url"` // e.g. https://sub.myshop.com (points at the HUB's subscription address)
}

func (s *Store) RepBrand(ctx context.Context, repID string) RepBrand {
	var b RepBrand
	if repID != "" {
		_ = json.Unmarshal([]byte(s.Setting(ctx, "rep_brand:"+repID)), &b)
	}
	return b
}

func (s *Store) PutRepBrand(ctx context.Context, repID string, b RepBrand) error {
	var x int
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM reps WHERE id=?`, repID).Scan(&x) != nil {
		return ErrNotFound
	}
	b.Title, b.Announce, b.SupportURL = strings.TrimSpace(b.Title), strings.TrimSpace(b.Announce), strings.TrimSpace(b.SupportURL)
	b.RemarkTemplate, b.BaseURL = strings.TrimSpace(b.RemarkTemplate), strings.TrimRight(strings.TrimSpace(b.BaseURL), "/")
	switch {
	case len(b.Title) > 80 || len(b.Announce) > 1000 || len(b.RemarkTemplate) > 200:
		return fmt.Errorf("%w: too long", ErrInvalid)
	case b.SupportURL != "" && !strings.HasPrefix(b.SupportURL, "https://") && !strings.HasPrefix(b.SupportURL, "tg://"):
		return fmt.Errorf("%w: support link must start with https:// or tg://", ErrInvalid)
	case b.Accent != "" && !accentRE.MatchString(b.Accent):
		return fmt.Errorf("%w: colour must look like #2de2e6", ErrInvalid)
	case b.Logo != "" && !validLogo(b.Logo):
		return fmt.Errorf("%w: logo must be a PNG, JPEG, WebP or SVG image under 150 KB", ErrInvalid)
	}
	if b.BaseURL != "" {
		u, err := url.Parse(b.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			return fmt.Errorf("%w: the address looks like https://sub.example.com", ErrInvalid)
		}
	}
	raw, _ := json.Marshal(b)
	if b == (RepBrand{}) {
		raw = nil
	}
	return s.SetSetting(ctx, "rep_brand:"+repID, string(raw))
}

// repChain is the client's reseller and its parents, nearest first.
func (s *Store) repChain(ctx context.Context, email string) []string {
	var rep string
	if s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, email).Scan(&rep) != nil || rep == "" {
		return nil
	}
	var out []string
	for i := 0; rep != "" && i < 4; i++ {
		out = append(out, rep)
		var parent string
		if s.ro.QueryRowContext(ctx, `SELECT parent_id FROM reps WHERE id=?`, rep).Scan(&parent) != nil {
			break
		}
		rep = parent
	}
	return out
}

// brandFor is the subscription settings a client sees: the panel's, with
// their reseller's (and its parent's) own choices on top.
func (s *Store) brandFor(ctx context.Context, email string) (SubSettings, string) {
	set := s.SubSettings(ctx)
	base := ""
	chain := s.repChain(ctx, email)
	for i := len(chain) - 1; i >= 0; i-- { // parent first, nearest reseller last
		b := s.RepBrand(ctx, chain[i])
		pick := func(dst *string, v string) {
			if v != "" {
				*dst = v
			}
		}
		pick(&set.Title, b.Title)
		pick(&set.Announce, b.Announce)
		pick(&set.SupportURL, b.SupportURL)
		pick(&set.Accent, b.Accent)
		pick(&set.Logo, b.Logo)
		pick(&set.RemarkTemplate, b.RemarkTemplate)
		pick(&base, b.BaseURL)
	}
	return set, base
}

// repBase is the address a reseller's clients use (their own, else their
// parent's), "" for none. Only rep rows are read, not the panel settings.
func (s *Store) repBase(ctx context.Context, rep string) string {
	base := ""
	for i := 0; rep != "" && i < 4; i++ {
		if base == "" {
			base = s.RepBrand(ctx, rep).BaseURL
		}
		var parent string
		if s.ro.QueryRowContext(ctx, `SELECT parent_id FROM reps WHERE id=?`, rep).Scan(&parent) != nil {
			break
		}
		rep = parent
	}
	return base
}

// SubBaseFor is the address a client's links use: their reseller's own
// address if set, otherwise def.
func (s *Store) SubBaseFor(ctx context.Context, email, def string) string {
	if _, b := s.brandFor(ctx, email); b != "" {
		return b
	}
	return def
}
