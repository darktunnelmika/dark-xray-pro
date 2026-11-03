package usageledger

import "encoding/json"

// Keep Account value-comparable for financial before/after checks. Optional
// remaining budget uses a known bit internally and an absent JSON field when
// unknown; no pointer identity enters financial equality.
func (a Account) MarshalJSON() ([]byte, error) {
	type value Account
	var remaining *int64
	if a.RemainingNativeBudgetKnown {
		remaining = &a.RemainingNativeBudgetBytes
	}
	return json.Marshal(struct {
		value
		Remaining *int64 `json:"remaining_native_budget_bytes,omitempty,string"`
	}{value(a), remaining})
}
func (a *Account) UnmarshalJSON(raw []byte) error {
	type value Account
	var wire struct {
		value
		Remaining *int64 `json:"remaining_native_budget_bytes,omitempty,string"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	if wire.Remaining != nil && *wire.Remaining < 0 {
		return ErrSchema
	}
	*a = Account(wire.value)
	if wire.Remaining != nil {
		a.RemainingNativeBudgetKnown = true
		a.RemainingNativeBudgetBytes = *wire.Remaining
	}
	return nil
}
