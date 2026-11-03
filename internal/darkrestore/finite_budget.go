package darkrestore

import (
	"errors"
	"math"
)

var ErrFiniteBudget = errors.New("invalid dark restore finite budget")

type NativeCutoverCountersV1 struct {
	Uplink   int64
	Downlink int64
}
type FiniteCutoverBudgetV1 struct {
	Scope                string   `json:"scope"`
	HistoricalBytes      *int64   `json:"historical_bytes,omitempty,string"`
	PostMigrationBytes   *int64   `json:"post_migration_bytes,omitempty,string"`
	TotalBytes           *int64   `json:"total_bytes,omitempty,string"`
	RemainingBytes       *int64   `json:"remaining_bytes,omitempty,string"`
	MissingEvidence      []string `json:"missing_evidence"`
	AuthorizesActivation bool     `json:"authorizes_activation"`
}

// ProjectFiniteCutoverBudgetV1 specifies arithmetic only. The caller must obtain
// current entitlement and counters from authoritative records in one snapshot;
// browser values cannot establish a baseline or entitlement. Nil is unknown,
// including when no native counter has yet been measured. Current entitlement
// already includes recorded renewals; they must not be added a second time.
func ProjectFiniteCutoverBudgetV1(currentEntitlement int64, settledHistorical *int64, native *NativeCutoverCountersV1) (FiniteCutoverBudgetV1, error) {
	zero := FiniteCutoverBudgetV1{}
	if currentEntitlement <= 0 || (settledHistorical != nil && *settledHistorical < 0) {
		return zero, ErrFiniteBudget
	}
	if native != nil && (native.Uplink < 0 || native.Downlink < 0 || native.Uplink > math.MaxInt64-native.Downlink) {
		return zero, ErrFiniteBudget
	}
	out := FiniteCutoverBudgetV1{Scope: "projection_only", MissingEvidence: []string{}}
	if settledHistorical == nil {
		out.MissingEvidence = append(out.MissingEvidence, "settled_history_unknown")
	} else {
		v := *settledHistorical
		out.HistoricalBytes = &v
	}
	if native == nil {
		out.MissingEvidence = append(out.MissingEvidence, "native_usage_unknown")
	} else {
		v := native.Uplink + native.Downlink
		out.PostMigrationBytes = &v
	}
	if len(out.MissingEvidence) > 0 {
		return out, nil
	}
	if *out.HistoricalBytes > math.MaxInt64-*out.PostMigrationBytes {
		return zero, ErrFiniteBudget
	}
	total := *out.HistoricalBytes + *out.PostMigrationBytes
	remaining := max(int64(0), currentEntitlement-total)
	out.TotalBytes = &total
	out.RemainingBytes = &remaining
	return out, nil
}
