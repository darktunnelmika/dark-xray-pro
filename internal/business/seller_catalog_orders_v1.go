package business

import (
	"context"
	"database/sql"
	"errors"
)

// SellerCatalogClientCreateV1 is an internal accounting bridge, not a public
// checkout or payment contract. The caller must repeat the exact published
// product snapshot it reviewed. Only whole 30-day unlimited months are
// accepted; debit is months multiplied by user seats.
type SellerCatalogClientCreateV1 struct {
	EventID                 string `json:"event_id"`
	RepresentativeID        string `json:"representative_id"`
	SellerID                string `json:"seller_id"`
	ProductID               string `json:"product_id"`
	ExpectedProductRevision int64  `json:"expected_product_revision"`
	ExpectedPriceToman      int64  `json:"expected_price_toman"`
	ExpectedDurationDays    int64  `json:"expected_duration_days"`
	ExpectedUserCount       int64  `json:"expected_user_count"`
	ClientID                string `json:"client_id"`
	Name                    string `json:"name"`
	Reason                  string `json:"reason"`
}

// SellerOrderCatalogV1 is stored inside the immutable seller order receipt.
// Price is evidence only: no payment or cash ledger is applied here.
type SellerOrderCatalogV1 struct {
	ProductID       string `json:"product_id"`
	ProductRevision int64  `json:"product_revision"`
	PriceToman      int64  `json:"price_toman"`
	DurationDays    int64  `json:"duration_days"`
	Months          int64  `json:"months"`
	UserCount       int64  `json:"user_count"`
	CheckoutEnabled bool   `json:"checkout_enabled"`
	PaymentApplied  bool   `json:"payment_applied"`
}

type sellerCatalogBindingV1 struct {
	ProductID       string `json:"product_id"`
	ProductRevision int64  `json:"product_revision"`
	PriceToman      int64  `json:"price_toman"`
	DurationDays    int64  `json:"duration_days"`
	UserCount       int64  `json:"user_count"`
}

func (r SellerCatalogClientCreateV1) intent() (sellerOrderIntentV1, error) {
	if !idRE.MatchString(r.ProductID) || r.ExpectedProductRevision < 1 ||
		r.ExpectedProductRevision > 1_000_000_000 || r.ExpectedPriceToman < 1 ||
		r.ExpectedPriceToman > MaxCatalogPriceToman ||
		r.ExpectedDurationDays < 30 || r.ExpectedDurationDays > MaxDays ||
		r.ExpectedDurationDays%30 != 0 ||
		r.ExpectedUserCount < 1 || r.ExpectedUserCount > MaxIPLimit {
		return sellerOrderIntentV1{}, ErrInvalid
	}
	months := r.ExpectedDurationDays / 30
	if months > MaxUnlimitedCredit/r.ExpectedUserCount {
		return sellerOrderIntentV1{}, ErrInvalid
	}
	create := SellerClientCreateV1{
		EventID: r.EventID, RepresentativeID: r.RepresentativeID,
		SellerID: r.SellerID, ClientID: r.ClientID, Name: r.Name,
		DurationDays: r.ExpectedDurationDays, IPCount: r.ExpectedUserCount,
		Unlimited: true, Reason: r.Reason,
	}
	intent := sellerOrderIntentV1{Create: &create, Catalog: &sellerCatalogBindingV1{
		ProductID: r.ProductID, ProductRevision: r.ExpectedProductRevision,
		PriceToman: r.ExpectedPriceToman, DurationDays: r.ExpectedDurationDays,
		UserCount: r.ExpectedUserCount,
	}}
	if _, err := intent.identity(); err != nil {
		return sellerOrderIntentV1{}, err
	}
	return intent, nil
}

// CreateSellerClientFromCatalogV1 consumes only seller reserved unlimited
// units. It deliberately has no HTTP wiring and never records a payment.
func (s *Store) CreateSellerClientFromCatalogV1(ctx context.Context, token string, req SellerCatalogClientCreateV1) (SellerOrderReceiptV1, error) {
	intent, err := req.intent()
	if err != nil {
		return SellerOrderReceiptV1{}, err
	}
	return s.executeSellerOrderV1(ctx, token, intent)
}

func validateSellerCatalogOrderV1(ctx context.Context, tx *sql.Tx, intent sellerOrderIntentV1) (*SellerOrderCatalogV1, error) {
	if intent.Catalog == nil {
		return nil, nil
	}
	if intent.Create == nil || intent.Extend != nil {
		return nil, ErrInvalid
	}
	binding := intent.Catalog
	create := intent.Create
	product, err := readCatalog(ctx, tx, binding.ProductID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	if product.Status != "published" || product.PlanKind != "unlimited" ||
		product.RepresentativeID != create.RepresentativeID ||
		product.Revision != binding.ProductRevision ||
		product.PriceToman != binding.PriceToman ||
		product.DurationDays != binding.DurationDays ||
		product.IPCount != binding.UserCount ||
		product.VolumeBytes != 0 ||
		create.DurationDays != binding.DurationDays ||
		create.IPCount != binding.UserCount ||
		!create.Unlimited || create.VolumeBytes != 0 ||
		binding.DurationDays < 30 || binding.DurationDays%30 != 0 {
		return nil, ErrConflict
	}
	months := binding.DurationDays / 30
	if months < 1 || binding.UserCount < 1 ||
		months > MaxUnlimitedCredit/binding.UserCount {
		return nil, ErrInvalid
	}
	return &SellerOrderCatalogV1{
		ProductID: binding.ProductID, ProductRevision: binding.ProductRevision,
		PriceToman: binding.PriceToman, DurationDays: binding.DurationDays,
		Months: months, UserCount: binding.UserCount,
		CheckoutEnabled: false, PaymentApplied: false,
	}, nil
}

func verifySellerCatalogReceiptV1(intent sellerOrderIntentV1, receipt SellerOrderReceiptV1) error {
	if (intent.Catalog == nil) != (receipt.Catalog == nil) {
		return ErrUnavailable
	}
	if intent.Catalog == nil {
		return nil
	}
	if intent.Create == nil || intent.Extend != nil {
		return ErrUnavailable
	}
	binding := intent.Catalog
	catalog := receipt.Catalog
	months := binding.DurationDays / 30
	if binding.DurationDays < 30 || binding.DurationDays%30 != 0 ||
		binding.UserCount < 1 || months < 1 ||
		months > MaxUnlimitedCredit/binding.UserCount ||
		catalog.ProductID != binding.ProductID ||
		catalog.ProductRevision != binding.ProductRevision ||
		catalog.PriceToman != binding.PriceToman ||
		catalog.DurationDays != binding.DurationDays ||
		catalog.Months != months || catalog.UserCount != binding.UserCount ||
		catalog.CheckoutEnabled || catalog.PaymentApplied ||
		receipt.Client.PlanKind != "unlimited" ||
		receipt.Client.DurationDays != binding.DurationDays ||
		receipt.Client.IPCount != binding.UserCount ||
		receipt.Client.VolumeBytes != 0 ||
		receipt.VolumeDebitBytes != 0 ||
		receipt.UnlimitedDebitUnits != months*binding.UserCount {
		return ErrUnavailable
	}
	return nil
}
