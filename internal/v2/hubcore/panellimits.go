package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Caps on panels: how many reseller panels one Telegram account may own
// (bought or waiting for approval), and how many panels one special
// reseller may sell. Both are 50 unless the owner changes them.

type PanelLimits struct {
	PerBuyer  int `json:"per_buyer"`
	PerSeller int `json:"per_seller"`
}

const defaultPanelCap = 50

func (s *Store) PanelLimitsOf(ctx context.Context) PanelLimits {
	l := PanelLimits{PerBuyer: defaultPanelCap, PerSeller: defaultPanelCap}
	_ = json.Unmarshal([]byte(s.Setting(ctx, "panel_limits")), &l)
	if l.PerBuyer <= 0 {
		l.PerBuyer = defaultPanelCap
	}
	if l.PerSeller <= 0 {
		l.PerSeller = defaultPanelCap
	}
	return l
}

func (s *Store) PutPanelLimits(ctx context.Context, l PanelLimits) error {
	if l.PerBuyer < 1 || l.PerBuyer > 10000 || l.PerSeller < 1 || l.PerSeller > 100000 {
		return fmt.Errorf("%w: limits are 1 to 10000 panels a buyer and 1 to 100000 a reseller", ErrInvalid)
	}
	b, _ := json.Marshal(l)
	return s.SetSetting(ctx, "panel_limits", string(b))
}

// BuyerPanels counts the chat's panels and its new-panel orders waiting.
func (s *Store) BuyerPanels(ctx context.Context, chat int64) int {
	if chat == 0 {
		return 0
	}
	var open int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_orders WHERE chat_id=? AND kind='new' AND status IN ('await_receipt','pending')`, chat).Scan(&open)
	return len(s.ChatPanels(ctx, chat)) + open
}

// SellerPanels counts the panels a special reseller sold (still there) and
// its new-panel orders waiting.
func (s *Store) SellerPanels(ctx context.Context, seller string) int {
	var sold, open int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(DISTINCT o.rep_id) FROM rep_orders o JOIN reps r ON r.id=o.rep_id
WHERE o.seller=? AND o.kind='new' AND o.status='approved'`, seller).Scan(&sold)
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_orders WHERE seller=? AND kind='new' AND status IN ('await_receipt','pending')`, seller).Scan(&open)
	return sold + open
}

// CheckPanelCaps refuses a new panel over either cap (nil = room left).
func (s *Store) CheckPanelCaps(ctx context.Context, seller string, chat int64) error {
	l := s.PanelLimitsOf(ctx)
	if chat != 0 && s.BuyerPanels(ctx, chat) >= l.PerBuyer {
		return fmt.Errorf("%w: one account may have at most %d panels", ErrPolicy, l.PerBuyer)
	}
	if seller != "" && s.SellerPanels(ctx, seller) >= l.PerSeller {
		return fmt.Errorf("%w: this reseller's panel capacity (%d) is full", ErrPolicy, l.PerSeller)
	}
	return nil
}

func registerPanelLimits(mux *http.ServeMux, s *Store, mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool) {
	mux.HandleFunc("GET /app/api/m/panellimits", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "sellers") {
			return
		}
		out := map[string]any{"limits": s.PanelLimitsOf(r.Context())}
		if !m.Owner {
			out["sold"] = s.SellerPanels(r.Context(), m.Seller)
		}
		reply(w, out, nil)
	})
	mux.HandleFunc("POST /app/api/m/panellimits", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "sellers") {
			return
		}
		if !m.Owner {
			http.Error(w, "فقط مدیر پنل.", http.StatusForbidden)
			return
		}
		var in PanelLimits
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.PutPanelLimits(r.Context(), in); err != nil {
			http.Error(w, "عدد معتبر بنویسید (حداقل ۱).", http.StatusBadRequest)
			return
		}
		reply(w, map[string]any{"ok": true}, nil)
	})
}
