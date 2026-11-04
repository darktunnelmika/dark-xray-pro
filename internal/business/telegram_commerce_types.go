package business

import (
	"errors"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

var (
	ErrTelegramDenied  = errors.New("telegram operation not permitted")
	ErrTelegramPayment = errors.New("telegram payment does not match its order")
)

// TelegramActor is populated ONLY by the authenticated transport. A callback,
// username, message body or arbitrary public HTTP request is not identity proof.
type TelegramActor struct {
	BotID  int64 `json:"bot_id"`
	UserID int64 `json:"user_id"`
	ChatID int64 `json:"chat_id"`
}

type TelegramStore struct {
	ID               string `json:"id"`
	BotID            int64  `json:"bot_id"`
	RepresentativeID string `json:"representative_id"`
	Name             string `json:"name"`
	ManagerUserID    int64  `json:"manager_user_id"`
	Enabled          bool   `json:"enabled"`
	Revision         int64  `json:"revision"`
}

type TelegramActorV1 = TelegramActor
type TelegramBotV1 = TelegramStore

type TelegramAccess struct {
	Store       TelegramStore `json:"store"`
	UserID      int64         `json:"user_id"`
	CustomerID  string        `json:"customer_id"`
	Role        string        `json:"role"`
	Permissions []string      `json:"permissions"`
}

type TelegramStoreRegistration struct {
	EventID          string `json:"event_id"`
	ID               string `json:"id"`
	BotID            int64  `json:"bot_id"`
	RepresentativeID string `json:"representative_id"`
	Name             string `json:"name"`
	ManagerUserID    int64  `json:"manager_user_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
}

type TelegramProduct struct {
	Product       CatalogProduct `json:"product"`
	PriceStars    int64          `json:"price_stars"`
	PriceRevision int64          `json:"price_revision"`
}

type TelegramProductPrice struct {
	EventID          string `json:"event_id"`
	ProductID        string `json:"product_id"`
	PriceStars       int64  `json:"price_stars"`
	ExpectedRevision int64  `json:"expected_revision"`
}

type TelegramService struct {
	Client         Client              `json:"client"`
	Usage          usageledger.Account `json:"usage"`
	UsageAvailable bool                `json:"usage_available"`
	State          string              `json:"state"`
}

type TelegramCheckoutRequest struct {
	EventID   string `json:"event_id"`
	ProductID string `json:"product_id"`
	ClientID  string `json:"client_id"`
	// Omission preserves the exact JSON/fingerprint of existing full-price quotes.
	BonusStars int64 `json:"bonus_stars,omitempty"`
}

// ID is an opaque, server-generated invoice payload. Amount and terms are
// snapshots of a published native catalog product, never callback-supplied.
type TelegramOrder struct {
	ID          string         `json:"id"`
	StoreID     string         `json:"store_id"`
	CustomerID  string         `json:"customer_id"`
	BotID       int64          `json:"bot_id"`
	UserID      int64          `json:"user_id"`
	ProductID   string         `json:"product_id"`
	ClientID    string         `json:"client_id"`
	Action      string         `json:"action"`
	Currency    string         `json:"currency"`
	AmountStars int64          `json:"amount_stars"`
	Status      string         `json:"status"`
	Product     CatalogProduct `json:"product"`
	ExpiresUnix int64          `json:"expires_unix"`
	CreatedUnix int64          `json:"created_unix"`
	Replayed    bool           `json:"replayed"`
	// AmountStars remains the actual external XTR amount. Gross is their sum.
	BonusDiscountStars int64 `json:"bonus_discount_stars,omitempty"`
}

type TelegramPayment struct {
	Payload          string `json:"payload"`
	Currency         string `json:"currency"`
	TelegramChargeID string `json:"telegram_charge_id"`
	TotalAmount      int64  `json:"total_amount"`
}

type TelegramPaymentReceipt struct {
	Order            TelegramOrder `json:"order"`
	TelegramChargeID string        `json:"telegram_charge_id"`
	CreatedUnix      int64         `json:"created_unix"`
	Replayed         bool          `json:"replayed"`
}

type TelegramOrderReceipt struct {
	Order       TelegramOrder `json:"order"`
	Client      Client        `json:"client"`
	CreatedUnix int64         `json:"created_unix"`
	Replayed    bool          `json:"replayed"`
}

type TelegramAdminInvitationRequest struct {
	EventID     string   `json:"event_id"`
	UserID      int64    `json:"user_id"`
	Permissions []string `json:"permissions"`
}

type TelegramAdminInvitation struct {
	ID          string   `json:"id"`
	StoreID     string   `json:"store_id"`
	UserID      int64    `json:"user_id"`
	Permissions []string `json:"permissions"`
	ExpiresUnix int64    `json:"expires_unix"`
	Accepted    bool     `json:"accepted"`
}

type TelegramAdmin struct {
	UserID      int64    `json:"user_id"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	Enabled     bool     `json:"enabled"`
	Revision    int64    `json:"revision"`
}

type TelegramAdminChange struct {
	EventID          string   `json:"event_id"`
	UserID           int64    `json:"user_id"`
	ExpectedRevision int64    `json:"expected_revision"`
	Permissions      []string `json:"permissions"`
	Enabled          bool     `json:"enabled"`
}

type TelegramWallet struct {
	Currency            string `json:"currency"`
	PaidStars           int64  `json:"paid_stars"`
	BonusStars          int64  `json:"bonus_stars"`
	AvailableBonusStars int64  `json:"available_bonus_stars"`
	ReservedBonusStars  int64  `json:"reserved_bonus_stars"`
	// Ledger net alone is not checkout or withdrawal capability.
	CheckoutRedemptionEnabled bool `json:"checkout_redemption_enabled"`
}

type TelegramRepresentativeCreate struct {
	EventID string `json:"event_id"`
	ID      string `json:"id"`
	Name    string `json:"name"`
}

type TelegramTrialPolicy struct {
	StoreID       string `json:"store_id"`
	Enabled       bool   `json:"enabled"`
	VolumeBytes   int64  `json:"volume_bytes,string"`
	Revision      int64  `json:"revision"`
	DurationHours int64  `json:"duration_hours"`
	StartsAt      string `json:"starts_at"`
}

type TelegramTrialPolicyChange struct {
	EventID          string `json:"event_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Enabled          bool   `json:"enabled"`
	VolumeBytes      int64  `json:"volume_bytes,string"`
}
