package usagecheckpoint

// A report receipt proves durable bytes, not final settlement: an uncertain
// epoch still accepts known bytes. SettlementReceipt separately confirms that
// the HUB sealed this exact final cursor without an unresolved epoch tail.
type SettlementReceipt struct {
	Receipt Receipt `json:"receipt"`
	Sealed  bool    `json:"sealed"`
}

func ValidReceipt(r Receipt) bool {
	return hexID(r.EpochID, 32) && r.Sequence > 0 && hexID(r.SHA, 64)
}
