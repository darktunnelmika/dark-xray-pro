package usagecheckpoint

// Interruption never claims a zero or final counter. The authenticated HUB
// resolves the epoch's authoritative bindings and places reconciliation holds.
type Interruption struct {
	EpochID string `json:"epoch_id"`
}
type InterruptionReceipt struct {
	EpochID                string `json:"epoch_id"`
	ReconciliationRequired bool   `json:"reconciliation_required"`
}

func ValidInterruption(n Interruption) bool { return hexID(n.EpochID, 32) }
