// Package telegramhub adapts authenticated HUB bot input to the native PRO
// ledger. It does not receive network updates, load credentials or send messages.
package telegramhub

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegramreport"
)

const MaxOwnerReportCommandBytes = 160

var (
	ErrInvalidConfig     = errors.New("invalid HUB owner report configuration")
	ErrUnauthorized      = errors.New("HUB owner report access denied")
	ErrInvalidCommand    = errors.New("invalid HUB owner report command")
	ErrReportNotFound    = errors.New("representative report not found")
	ErrReportUnavailable = errors.New("representative report unavailable")
	botUsernamePattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
	representativeID     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// OwnerReportConfig is trusted server configuration, never update-supplied
// authority. The chat binding is explicit and independent of a username.
// An empty BotUsername accepts only the unmentioned /rep_credit command.
type OwnerReportConfig struct {
	OwnerUserID int64
	OwnerChatID int64
	BotUsername string
}

// OwnerReportMessage is a normalized ordinary text-message update. A future
// receiver MUST authenticate its source and populate all origin flags. These
// fields alone are not proof of Telegram identity and must never be accepted
// from an unauthenticated public request. Edited/forwarded/on-behalf-of messages
// and non-message updates must not be normalized as ordinary owner messages.
type OwnerReportMessage struct {
	UpdateID     int64
	MessageID    int64
	SenderID     int64
	ChatID       int64
	ChatType     string
	SenderIsBot  bool
	SenderChatID int64
	Forwarded    bool
	Edited       bool
	ViaBot       bool
	Text         string
}

// RepresentativeReporter is implemented by the existing native business Store.
// Its returned values remain authoritative; this adapter keeps no wallet/cache.
type RepresentativeReporter interface {
	OwnerRepresentativeLedgerReport(context.Context, string) (business.OwnerRepLedgerReport, error)
}

var _ RepresentativeReporter = (*business.Store)(nil)

// PreparedOwnerReply is a delivery proposal for an authorized private chat.
// It is NOT a delivery receipt, an outbox entry or proof that a message was sent.
// The caller owns durable replay/dispatch policy and must revalidate the current
// owner/chat binding before any delayed send. No transport is invoked here.
type PreparedOwnerReply struct {
	OwnerUserID      int64
	ChatID           int64
	UpdateID         int64
	ReplyToMessageID int64
	RepresentativeID string
	ParseMode        string
	Text             string
}

type OwnerReportHandler struct {
	config  OwnerReportConfig
	reports RepresentativeReporter
}

func NewOwnerReportHandler(config OwnerReportConfig, reports RepresentativeReporter) (*OwnerReportHandler, error) {
	if config.OwnerUserID <= 0 || config.OwnerChatID <= 0 || reports == nil ||
		(config.BotUsername != "" && !botUsernamePattern.MatchString(config.BotUsername)) {
		return nil, ErrInvalidConfig
	}
	return &OwnerReportHandler{config: config, reports: reports}, nil
}

// Prepare authenticates the configured principal before parsing or reading any
// representative data. Errors contain no report, SQL error or untrusted text.
// Repeated calls make fresh read-only reports; there is deliberately no in-memory
// claim of exactly-once delivery. A future inbox/outbox owns duplicate updates
// and ambiguous transport outcomes across process restarts.
func (h *OwnerReportHandler) Prepare(ctx context.Context, message OwnerReportMessage) (PreparedOwnerReply, error) {
	var empty PreparedOwnerReply
	if h == nil || h.reports == nil || h.config.OwnerUserID <= 0 || h.config.OwnerChatID <= 0 {
		return empty, ErrInvalidConfig
	}
	if message.SenderID != h.config.OwnerUserID || message.ChatID != h.config.OwnerChatID ||
		message.ChatType != "private" || message.SenderIsBot || message.SenderChatID != 0 ||
		message.Forwarded || message.Edited || message.ViaBot || message.UpdateID < 0 || message.MessageID <= 0 {
		return empty, ErrUnauthorized
	}
	id, err := h.parseCommand(message.Text)
	if err != nil {
		return empty, err
	}
	if ctx == nil {
		return empty, ErrInvalidConfig
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	report, err := h.reports.OwnerRepresentativeLedgerReport(ctx, id)
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if errors.Is(err, business.ErrNotFound) {
		return empty, ErrReportNotFound
	}
	if err != nil || report.RepresentativeID != id {
		return empty, ErrReportUnavailable
	}
	text, err := telegramreport.FormatOwnerRepresentativeFA(telegramreport.OwnerRepresentativeCard{
		RepresentativeID: report.RepresentativeID, DisplayName: report.DisplayName,
		Enabled: report.Enabled, FreeVolumeBytes: report.FreeVolumeBytes,
		FreeUnlimitedUnits: report.FreeUnlimitedUnits, SellerQuotaState: report.SellerQuotaState,
		SellerCount: report.SellerCount, ReservedVolumeBytes: report.ReservedVolumeBytes,
		ReservedUnlimitedUnits: report.ReservedUnlimitedUnits, BalanceSemantics: report.BalanceSemantics,
	})
	if err != nil {
		return empty, ErrReportUnavailable
	}
	return PreparedOwnerReply{
		OwnerUserID: h.config.OwnerUserID, ChatID: h.config.OwnerChatID,
		UpdateID: message.UpdateID, ReplyToMessageID: message.MessageID,
		RepresentativeID: id, ParseMode: "HTML", Text: text,
	}, nil
}

func (h *OwnerReportHandler) parseCommand(text string) (string, error) {
	if len(text) == 0 || len(text) > MaxOwnerReportCommandBytes || !utf8.ValidString(text) {
		return "", ErrInvalidCommand
	}
	// Command and ID are ASCII. Reject tabs, newlines, invisible characters and
	// Unicode whitespace instead of letting them hide extra command arguments.
	for _, b := range []byte(text) {
		if b < ' ' || b > '~' {
			return "", ErrInvalidCommand
		}
	}
	parts := strings.Fields(text)
	if len(parts) != 2 || !representativeID.MatchString(parts[1]) || parts[1] == "owner" {
		return "", ErrInvalidCommand
	}
	command, mention, mentioned := strings.Cut(parts[0], "@")
	if command != "/rep_credit" || (mentioned && (h.config.BotUsername == "" ||
		!strings.EqualFold(mention, h.config.BotUsername))) {
		return "", ErrInvalidCommand
	}
	return parts[1], nil
}
