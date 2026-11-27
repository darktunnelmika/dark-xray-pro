package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// A seller's banner over its reseller-panel sale (Mini App "پنل نمایندگی"
// page), with an optional real discount on the panel packages until a date.
// The discount is applied when the order is made (createRepOrder), so the
// bot, the app and the receipt all charge the same; a reseller's discount
// never goes below what it pays wholesale.

type PanelPromo struct {
	On      bool   `json:"on"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	Badge   string `json:"badge"` // short tag on the banner, e.g. "فقط امروز"
	Color   string `json:"color"` // violet | gold | green | rose | blue
	Image   string `json:"image"` // optional banner picture (data: URL)
	Percent int64  `json:"percent"`
	Until   int64  `json:"until"` // unix; 0 = no end
}

var promoColors = map[string]bool{"": true, "violet": true, "gold": true, "green": true, "rose": true, "blue": true}

func promoKey(seller string) string { return "panel_promo:" + seller }

func (s *Store) PanelPromoFor(ctx context.Context, seller string) PanelPromo {
	var p PanelPromo
	_ = json.Unmarshal([]byte(s.Setting(ctx, promoKey(seller))), &p)
	return p
}

func (s *Store) PutPanelPromoFor(ctx context.Context, seller string, p PanelPromo) error {
	p.Title, p.Text, p.Badge = strings.TrimSpace(p.Title), strings.TrimSpace(p.Text), strings.TrimSpace(p.Badge)
	switch {
	case utf8.RuneCountInString(p.Title) > 80, utf8.RuneCountInString(p.Text) > 300, utf8.RuneCountInString(p.Badge) > 24:
		return fmt.Errorf("%w: the banner text is too long", ErrInvalid)
	case !promoColors[p.Color]:
		return fmt.Errorf("%w: unknown colour", ErrInvalid)
	case p.Image != "" && !validLogo(p.Image):
		return fmt.Errorf("%w: the picture is too big or not an image", ErrInvalid)
	case p.Percent < 0 || p.Percent > 90:
		return fmt.Errorf("%w: discount is 0 to 90 percent", ErrInvalid)
	case p.Until < 0:
		return fmt.Errorf("%w: check the end date", ErrInvalid)
	}
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, promoKey(seller), string(b))
}

// Live reports whether the banner shows now.
func (p PanelPromo) Live(now int64) bool { return p.On && (p.Until == 0 || p.Until > now) }

// PanelPrice is what a customer of seller pays for a package now, and the
// price before any discount.
func (s *Store) PanelPrice(ctx context.Context, seller string, p RepPackage) (price, base int64) {
	base, floor := p.Price, int64(0)
	if seller != "" {
		base, floor = s.Retail(ctx, seller, p), s.Wholesale(ctx, seller, p)
	}
	price = base
	if pr := s.PanelPromoFor(ctx, seller); pr.Live(s.now().Unix()) && pr.Percent > 0 && base > 0 {
		price = (base*(100-pr.Percent)/100 + 500) / 1000 * 1000
		price = max(price, floor, 1000)
		price = min(price, base)
	}
	return price, base
}

// promoView is the public part shown to buyers ("" when off).
func (s *Store) promoView(ctx context.Context, seller string) map[string]any {
	p := s.PanelPromoFor(ctx, seller)
	if !p.Live(s.now().Unix()) {
		return nil
	}
	return map[string]any{"title": p.Title, "text": p.Text, "badge": p.Badge, "color": p.Color, "image": p.Image, "percent": p.Percent, "until": p.Until}
}

func registerPanelPromo(mux *http.ServeMux, s *Store, mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool) {
	may := func(w http.ResponseWriter, r *http.Request) (miniMgr, bool) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "sellers") {
			return m, false
		}
		if !m.Owner && (s.IsSubSeller(r.Context(), m.Seller) || !s.ResaleOf(r.Context(), m.Seller).Allowed) {
			http.Error(w, "فروش پنل برای شما فعال نیست.", http.StatusForbidden)
			return m, false
		}
		return m, true
	}
	mux.HandleFunc("GET /app/api/m/panelpromo", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok {
			return
		}
		reply(w, s.PanelPromoFor(r.Context(), m.Seller), nil)
	})
	mux.HandleFunc("POST /app/api/m/panelpromo", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok {
			return
		}
		var in PanelPromo
		// A banner picture is bigger than the usual 64 KB body.
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 300<<10)).Decode(&in); err != nil {
			http.Error(w, "عکس بنر خیلی بزرگ است.", http.StatusBadRequest)
			return
		}
		if err := s.PutPanelPromoFor(r.Context(), m.Seller, in); err != nil {
			miniErr(w, err)
			return
		}
		reply(w, map[string]any{"ok": true}, nil)
	})
}
