package usageingest

import (
	"context"
	"errors"
	"net/http"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

const SettlementPath = AgentPath + "/settled"

func (h *Handler) settled(ctx context.Context, w http.ResponseWriter, p usageledger.Principal, raw []byte) {
	var receipt uc.Receipt
	if len(raw) > 1024 || usagewire.Decode(raw, &receipt) != nil || !uc.ValidReceipt(receipt) {
		fail(w, 400, "invalid_settlement_receipt")
		return
	}
	result, err := h.ledger.ConfirmSettlement(ctx, p, receipt)
	if err != nil {
		switch {
		case errors.Is(err, usageledger.ErrMissing):
			fail(w, 404, "unregistered_usage_epoch")
		case errors.Is(err, usageledger.ErrAuthority):
			fail(w, 403, "usage_scope_denied")
		case errors.Is(err, usageledger.ErrConflict):
			fail(w, 409, "usage_settlement_pending")
		default:
			fail(w, 503, "usage_settlement_unconfirmed")
		}
		return
	}
	respond(w, 200, result)
}
