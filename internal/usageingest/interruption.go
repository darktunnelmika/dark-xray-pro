package usageingest

import (
	"context"
	"errors"
	"net/http"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

const InterruptionPath = AgentPath + "/interrupted"

// Called only AFTER the existing TLS, Registry, method, media and size gates.
// Node/installation are authenticated; the client IDs are never in this body.
func (h *Handler) interrupted(ctx context.Context, w http.ResponseWriter, p usageledger.Principal, raw []byte) {
	var notice uc.Interruption
	if len(raw) > 1024 || usagewire.Decode(raw, &notice) != nil || !uc.ValidInterruption(notice) {
		fail(w, 400, "invalid_usage_interruption")
		return
	}
	err := h.ledger.MarkEpochUncertain(ctx, p, notice.EpochID, "Observer stopped without a verified final usage snapshot")
	if err != nil {
		switch {
		case errors.Is(err, usageledger.ErrMissing):
			fail(w, 404, "unregistered_usage_epoch")
		case errors.Is(err, usageledger.ErrAuthority):
			fail(w, 403, "usage_scope_denied")
		case errors.Is(err, usageledger.ErrConflict):
			fail(w, 409, "usage_interruption_conflict")
		default:
			fail(w, 503, "usage_interruption_unconfirmed")
		}
		return
	}
	respond(w, 200, uc.InterruptionReceipt{EpochID: notice.EpochID, ReconciliationRequired: true})
}
