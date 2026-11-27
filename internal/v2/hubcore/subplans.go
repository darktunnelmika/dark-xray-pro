package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// A reseller's sub-sellers (the panels it sold, the sellers it made) may
// set their own plans and prices, never below the floor the reseller sets:
// toman per GB for volume plans, toman per 30 days for unlimited ones.
// Without a saved choice they may, and the floor is the reseller's own
// cheapest plan, so nobody under it undercuts it.

// SubPlans is a reseller's rule for its sub-sellers' own plans.
type SubPlans struct {
	Allow    bool  `json:"allow"`
	MinGB    int64 `json:"min_gb"`    // toman per GB, 0 = no floor
	MinMonth int64 `json:"min_month"` // toman per 30 days of an unlimited plan, 0 = no floor
	Saved    bool  `json:"saved"`     // false: the defaults above, not saved yet
}

func subPlansKey(parent string) string { return "sub_plans:" + parent }

// SubPlansOf is the rule of a reseller.
func (s *Store) SubPlansOf(ctx context.Context, parent string) SubPlans {
	var p SubPlans
	if v := s.Setting(ctx, subPlansKey(parent)); v != "" && json.Unmarshal([]byte(v), &p) == nil {
		p.Saved = true
		return p
	}
	p.Allow = true
	p.MinGB, p.MinMonth = s.cheapestPlans(ctx, parent)
	return p
}

// cheapestPlans is the lowest price per GB and per 30 days of a seller's
// published plans (rounded down to 100 toman).
func (s *Store) cheapestPlans(ctx context.Context, seller string) (perGB, perMonth int64) {
	rows, err := s.ro.QueryContext(ctx, `SELECT plan,gb,days,max_ips,price FROM products WHERE seller=? AND status='published' AND price>0`, seller)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()
	for rows.Next() {
		var plan string
		var gb, days, price int64
		var ips int
		if rows.Scan(&plan, &gb, &days, &ips, &price) != nil {
			continue
		}
		switch {
		case plan == "volume" && gb > 0:
			if v := price / gb / 100 * 100; perGB == 0 || v < perGB {
				perGB = v
			}
		case plan == "unlimited" && days > 0:
			users := int64(max(ips, 1))
			if v := price * 30 / days / users / 100 * 100; perMonth == 0 || v < perMonth {
				perMonth = v
			}
		}
	}
	return perGB, perMonth
}

// PutSubPlans saves a reseller's rule.
func (s *Store) PutSubPlans(ctx context.Context, parent string, p SubPlans) error {
	if p.MinGB < 0 || p.MinGB > 10_000_000 || p.MinMonth < 0 || p.MinMonth > 100_000_000 {
		return fmt.Errorf("%w: check the minimum prices", ErrInvalid)
	}
	p.Saved = false
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, subPlansKey(parent), string(b))
}

// subMayPlan reports whether a sub-seller may set its own plans.
func (s *Store) subMayPlan(ctx context.Context, rep string) bool {
	parent, err := s.parentOf(ctx, s.ro, rep)
	return err == nil && parent != "" && s.SubPlansOf(ctx, parent).Allow
}

// subPlanFloor checks a sub-seller's own plan against its reseller's floor.
func (s *Store) subPlanFloor(ctx context.Context, rep string, p Product) error {
	parent, err := s.parentOf(ctx, s.ro, rep)
	if err != nil || parent == "" {
		return nil
	}
	f := s.SubPlansOf(ctx, parent)
	switch {
	case p.Plan == "volume" && f.MinGB > 0 && p.Price < p.GB*f.MinGB:
		return fmt.Errorf("%w: the lowest price is %d toman for %d GB", ErrPolicy, p.GB*f.MinGB, p.GB)
	case p.Plan == "unlimited" && f.MinMonth > 0:
		users := int64(max(p.MaxIPs, 1))
		if low := f.MinMonth * p.Days * users / 30; p.Price < low {
			return fmt.Errorf("%w: the lowest price is %d toman for this plan", ErrPolicy, low)
		}
	}
	return nil
}

func registerSubPlans(mux *http.ServeMux, s *Store, mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool), deny func(http.ResponseWriter, miniMgr, string) bool) {
	may := func(w http.ResponseWriter, r *http.Request) (miniMgr, bool) {
		m, ok := mgr(w, r)
		if !ok || deny(w, m, "sellers") {
			return m, false
		}
		if m.Owner || s.IsSubSeller(r.Context(), m.Seller) {
			http.Error(w, "این تنظیم برای نماینده‌ای است که زیرمجموعه دارد.", http.StatusForbidden)
			return m, false
		}
		return m, true
	}
	mux.HandleFunc("GET /app/api/m/subplans", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok {
			return
		}
		reply(w, s.SubPlansOf(r.Context(), m.Seller), nil)
	})
	mux.HandleFunc("POST /app/api/m/subplans", func(w http.ResponseWriter, r *http.Request) {
		m, ok := may(w, r)
		if !ok {
			return
		}
		var in SubPlans
		if !decode(w, r, &in) {
			return
		}
		if err := s.PutSubPlans(r.Context(), m.Seller, in); err != nil {
			miniErr(w, err)
			return
		}
		reply(w, s.SubPlansOf(r.Context(), m.Seller), nil)
	})
}
