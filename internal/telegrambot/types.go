// Package telegrambot implements the explicitly enabled PRO LAB Telegram
// commerce adapter. Telegram identity is established only by the fixed-origin
// transport; callback data never grants business authority.
package telegrambot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	EnableEnv        = "DARK_PRO_ENABLE_TELEGRAM_BOTS_V1"
	LABConfigPath    = "/etc/dark-xray-pro-lab/telegram-bots/config.json"
	LABJournalPath   = "/var/lib/dark-xray-pro-lab/telegram-bots/journal.sqlite"
	ConfigPurpose    = "isolated-pro-lab-telegram-commerce"
	maxBots          = 16
	maxResponseBytes = 1 << 20
	maxUpdateBytes   = 32 << 10
	maxBatch         = 25
	callbackLifetime = 30 * time.Minute
)

var (
	ErrConfig            = errors.New("invalid private Telegram commerce configuration")
	ErrProtocol          = errors.New("Telegram commerce response rejected")
	ErrTransport         = errors.New("Telegram commerce transport unavailable")
	ErrIdentity          = errors.New("Telegram commerce bot identity mismatch")
	ErrWebhook           = errors.New("Telegram commerce bot already has a webhook")
	ErrAccess            = errors.New("Telegram commerce access denied")
	ErrCallback          = errors.New("Telegram commerce menu expired or invalid")
	ErrJournal           = errors.New("Telegram commerce journal unavailable")
	ErrConflict          = errors.New("Telegram commerce durable history conflict")
	ErrCapacity          = errors.New("Telegram commerce journal capacity reached")
	ErrState             = errors.New("Telegram commerce worker state invalid")
	ErrFinancialMismatch = errors.New("Telegram financial receipt does not match its immutable order")
)

type Options struct {
	EnableFlag string
	LABMode    bool
	ConfigPath string
}

// Identity is populated from authenticated getUpdates responses, never an HTTP
// request, callback payload, username or forwarded message.
type Identity struct{ BotID, UserID, ChatID int64 }

// Access contains no credentials. Digest binds callbacks to the current role,
// store revision and permission set. Backends must resolve authority afresh.
type Access struct {
	Identity         Identity
	StoreID          string
	BrandName        string
	StoreRevision    int64
	RepresentativeID string
	CustomerID       string
	Role             string
	Permissions      []string
}

func (a Access) valid() bool {
	return a.Identity.BotID > 0 && a.Identity.UserID > 0 && a.Identity.ChatID == a.Identity.UserID &&
		a.StoreID != "" && a.StoreRevision >= 0 && a.RepresentativeID != "" && a.CustomerID != "" &&
		(a.Role == "owner" || a.Role == "representative" || a.Role == "admin" || a.Role == "customer")
}

func (a Access) digest() string {
	p := append([]string(nil), a.Permissions...)
	sort.Strings(p)
	raw, _ := json.Marshal(struct {
		Identity                           Identity
		StoreID                            string
		Revision                           int64
		RepresentativeID, CustomerID, Role string
		Permissions                        []string
	}{a.Identity, a.StoreID, a.StoreRevision, a.RepresentativeID, a.CustomerID, a.Role, p})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (a Access) Has(permission string) bool {
	for _, p := range a.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

type Action struct {
	Name     string `json:"name"`
	Resource string `json:"resource,omitempty"`
	Value    string `json:"value,omitempty"`
}

type Payment struct {
	Payload          string `json:"payload"`
	Currency         string `json:"currency"`
	Amount           int64  `json:"amount"`
	TelegramChargeID string `json:"telegram_charge_id,omitempty"`
}

type Incoming struct {
	UpdateID     int64    `json:"update_id"`
	Identity     Identity `json:"identity"`
	Kind         string   `json:"kind"`
	MessageID    int64    `json:"message_id,omitempty"`
	QueryID      string   `json:"query_id,omitempty"`
	Text         string   `json:"text,omitempty"`
	Language     string   `json:"language,omitempty"`
	BotUsername  string   `json:"bot_username,omitempty"`
	CallbackData string   `json:"callback_data,omitempty"`
	Payment      *Payment `json:"payment,omitempty"`
	ReceivedUnix int64    `json:"received_unix"`
}

type Request struct {
	Incoming Incoming
	Access   Access
	Action   Action
	// EventID is stable across restarts and repeated presses of one action.
	EventID           string
	Language          string
	financialCaptured bool
}

// Backend is the only bridge to business state. Handle must use durable
// idempotency for mutations. Resolve is called again before delayed delivery.
type Backend interface {
	Resolve(context.Context, Identity, bool) (Access, error)
	Handle(context.Context, Request) (Result, error)
}

// financialRecorder captures only authenticated immutable Telegram financial
// receipts. It grants no user, store, fulfillment or message-delivery authority.
// This deliberately precedes Resolve: revoking a shop cannot erase money that
// Telegram already collected or refunded for an existing, exactly bound order.
type financialRecorder interface {
	CaptureFinancialEvent(context.Context, Incoming) error
}

type financialDispatchValidator interface {
	ValidateFinancialDispatch(context.Context, Identity, Payment) error
}

type delivery struct {
	ID                      string
	BotID, UpdateID, ChatID int64
	Kind                    string
	Payload                 []byte
	Access                  Access
	Anonymous               bool
	ExpiresUnix             int64
	Attempts                int
	Proof                   *DeliveryProof
}

type Result struct {
	Access     *Access
	Screen     *Screen
	Invoice    *Invoice
	Checkout   *CheckoutDecision
	Document   *Document
	Delivery   *DeliveryProof
	Input      *Action
	ClearInput bool
	// Receipts are sent as new messages, never by editing a menu or invoice.
	Receipt bool
}

type Document struct {
	Name    string `json:"name"`
	Content []byte `json:"content"`
	Caption string `json:"caption,omitempty"`
}

// DeliveryProof contains no subscription/configuration material. An optional
// backend acknowledgment is called only for an authenticated Telegram receipt.
type DeliveryProof struct {
	ClientID        string `json:"client_id"`
	SHA256          string `json:"sha256"`
	TermsSHA256     string `json:"terms_sha256"`
	ExpiresUnixNano int64  `json:"expires_unix_nano"`
}
type deliveryAcknowledger interface {
	AcknowledgeDelivery(context.Context, Identity, DeliveryProof, int64) error
}

type Screen struct {
	Text    string
	Buttons [][]Button
}

// A button contains exactly one action. Callbacks are minted by the durable
// journal, not by renderers. Native styles are Telegram's three supported ones.
type Button struct {
	Text     string
	Style    string
	Action   *Action
	URL      string
	CopyText string
}

type Invoice struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Payload     string `json:"payload"`
	Currency    string `json:"currency"`
	Amount      int64  `json:"amount"`
}

type CheckoutDecision struct {
	OK           bool
	ErrorMessage string
}

type BotStatus struct {
	BotID                int64  `json:"bot_id,string"`
	State                string `json:"state"`
	LastPollUnix         int64  `json:"last_poll_unix"`
	LastError            string `json:"last_error,omitempty"`
	InboxPending         int64  `json:"inbox_pending"`
	FinancialRetry       int64  `json:"financial_retry"`
	ReviewPending        int64  `json:"review_pending"`
	OutboxPending        int64  `json:"outbox_pending"`
	OutboxUnknown        int64  `json:"outbox_unknown"`
	DeliveryReview       int64  `json:"delivery_review"`
	QueueStatusAvailable bool   `json:"queue_status_available"`
}

type DeliveryOutcome struct {
	State      string
	Reason     string
	MessageID  int64
	RetryAfter time.Duration
}

// APIError intentionally retains no URL, token, Telegram description or body.
type APIError struct {
	Code       int
	Definitive bool
	Fatal      bool
	RetryAfter time.Duration
	Reason     string
}

func (*APIError) Error() string { return ErrTransport.Error() }

func stableEvent(botID int64, source string) string {
	sum := sha256.Sum256([]byte(source))
	return fmt.Sprintf("tg:%d:%s", botID, hex.EncodeToString(sum[:16]))
}

func language(value string) string {
	if strings.HasPrefix(strings.ToLower(value), "en") {
		return "en"
	}
	return "fa"
}

func tr(lang, fa, en string) string {
	if lang == "en" {
		return en
	}
	return fa
}

func hexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
