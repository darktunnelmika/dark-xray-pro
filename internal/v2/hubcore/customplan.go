package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// A seller's "your own volume and days": the customer picks the GB and the
// days with sliders and pays a price per GB plus a price per 10 days. Each
// picked size is kept as a product of its own ("cx-…") so ordering, paying
// and delivery stay the normal ones; the shop lists never show them.

// CustomPlan is a seller's offer of custom sizes.
type CustomPlan struct {
	Enabled bool  `json:"enabled"`
	PriceGB int64 `json:"price_gb"`  // toman per GB
	Price10 int64 `json:"price_10d"` // toman per started 10 days
	MinGB   int64 `json:"min_gb"`
	MaxGB   int64 `json:"max_gb"`
	MinDays int64 `json:"min_days"`
	MaxDays int64 `json:"max_days"`
	IPs     int   `json:"ips"` // users at once (0 = no cap)
}

func customKey(seller string) string { return "custom_plan:" + seller }

func (s *Store) CustomPlanFor(ctx context.Context, seller string) CustomPlan {
	c := CustomPlan{MinGB: 10, MaxGB: 200, MinDays: 10, MaxDays: 90}
	_ = json.Unmarshal([]byte(s.Setting(ctx, customKey(seller))), &c)
	return c
}

func (s *Store) PutCustomPlanFor(ctx context.Context, seller string, c CustomPlan) error {
	if seller != "" && s.IsSubSeller(ctx, seller) {
		return fmt.Errorf("%w: sub-sellers sell the products their reseller defines", ErrPolicy)
	}
	if c.PriceGB < 0 || c.Price10 < 0 || c.MinGB < 1 || c.MaxGB < c.MinGB || c.MaxGB > 10000 || c.MinDays < 1 || c.MaxDays < c.MinDays || c.MaxDays > 3650 || c.IPs < 0 {
		return fmt.Errorf("%w: check the custom plan ranges", ErrInvalid)
	}
	if c.Enabled && c.PriceGB == 0 {
		return fmt.Errorf("%w: set the price per GB", ErrInvalid)
	}
	if seller != "" {
		pol, err := s.RepPolicyOf(ctx, seller)
		if err != nil {
			return err
		}
		if pol.MaxDays > 0 && c.MaxDays > pol.MaxDays {
			c.MaxDays = pol.MaxDays
		}
		if pol.MaxClientGB > 0 && c.MaxGB > pol.MaxClientGB {
			c.MaxGB = pol.MaxClientGB
		}
		if c.MinGB > c.MaxGB || c.MinDays > c.MaxDays {
			return fmt.Errorf("%w: check the custom plan ranges", ErrInvalid)
		}
	}
	b, _ := json.Marshal(c)
	return s.SetSetting(ctx, customKey(seller), string(b))
}

// Price is what gb for days costs.
func (c CustomPlan) Price(gb, days int64) int64 {
	return gb*c.PriceGB + (days+9)/10*c.Price10
}

func isCustomProduct(id string) bool { return strings.HasPrefix(id, "cx-") }

// CustomProduct makes (or refreshes) the hidden product for gb and days at
// the seller's current prices.
func (s *Store) CustomProduct(ctx context.Context, seller string, gb, days int64) (Product, error) {
	c := s.CustomPlanFor(ctx, seller)
	if !c.Enabled || (seller != "" && s.IsSubSeller(ctx, seller)) {
		return Product{}, ErrNotFound
	}
	if gb < c.MinGB || gb > c.MaxGB || days < c.MinDays || days > c.MaxDays {
		return Product{}, fmt.Errorf("%w: volume %d-%d GB, days %d-%d", ErrInvalid, c.MinGB, c.MaxGB, c.MinDays, c.MaxDays)
	}
	id := fmt.Sprintf("cx-%s-%dg-%dd", strings.ToLower(seller), gb, days)
	if seller == "" {
		id = fmt.Sprintf("cx-%dg-%dd", gb, days)
	}
	if len(id) > 63 {
		id = id[:63]
	}
	p := Product{ID: id, Seller: seller, Name: fmt.Sprintf("%d گیگ · %d روز (دلخواه)", gb, days), Plan: "volume", GB: gb, Days: days, MaxIPs: c.IPs,
		Price: c.Price(gb, days), Status: "published"}
	if err := s.PutProduct(ctx, Principal{User: "custom", Role: "owner"}, p); err != nil {
		return Product{}, err
	}
	return p, nil
}
