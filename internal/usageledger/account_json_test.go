package usageledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountBudgetJSONPreservesValueEqualityAndUnknown(t *testing.T) {
	unknown := Account{ClientID: "unknown", Plan: "volume", Limit: 1000}
	raw, err := json.Marshal(unknown)
	if err != nil || strings.Contains(string(raw), "remaining_native_budget_bytes") {
		t.Fatal("unknown remaining budget became zero", string(raw), err)
	}
	for _, remaining := range []int64{0, 9007199254740993} {
		original := Account{ClientID: "known", Plan: "volume", Known: true, Limit: 9223372036854775807, ReservedHistoricalBytes: 7, RemainingNativeBudgetKnown: true, RemainingNativeBudgetBytes: remaining}
		raw, err = json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Account
		if err = json.Unmarshal(raw, &decoded); err != nil || decoded != original {
			t.Fatal("budget JSON/value equality changed financial snapshot", original, decoded, err)
		}
		var again Account
		if err = json.Unmarshal(raw, &again); err != nil || again != decoded {
			t.Fatal("identical account snapshots compare unequal", err)
		}
		if !strings.Contains(string(raw), "\"remaining_native_budget_bytes\":\"") {
			t.Fatal("budget lost exact string encoding", string(raw))
		}
	}
	var invalid Account
	if err = json.Unmarshal([]byte("{\"remaining_native_budget_bytes\":\"-1\"}"), &invalid); err == nil {
		t.Fatal("negative remaining budget accepted")
	}
}
