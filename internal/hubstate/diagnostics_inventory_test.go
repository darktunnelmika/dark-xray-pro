//go:build linux

package hubstate

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestInventoryBusinessPhaseDiagnosticsRoundTrip(t *testing.T) {
	const flag = "DARK_PRO_ENABLE_BUSINESS_PHASE_DIAGNOSTICS"
	for _, tc := range []struct {
		name, value string
		present     bool
	}{
		{name: "absent"},
		{name: "disabled", value: "0", present: true},
		{name: "enabled", value: "1", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := fixtureOptions(privateTemp(t))
			baseline, err := BuildInventory(o)
			if err != nil {
				t.Fatal(err)
			}
			if tc.present {
				o.Environment = append(o.Environment, flag+"="+tc.value)
			}
			inv, err := BuildInventory(o)
			if err != nil {
				t.Fatal("diagnostic opt-in was rejected by the offline inventory", err)
			}
			if !reflect.DeepEqual(inv.Resources, baseline.Resources) || !reflect.DeepEqual(inv.Flags, baseline.Flags) {
				t.Fatal("diagnostic opt-in changed the persistent resource or listener inventory")
			}
			raw, err := json.Marshal(inv)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Inventory
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if err := ValidateArchiveInventory(decoded); err != nil {
				t.Fatal("serialized diagnostic inventory was rejected", err)
			}
			if value, present := decoded.Environment[flag]; value != tc.value || present != tc.present {
				t.Fatal("diagnostic opt-in presence or value changed during inventory serialization")
			}
		})
	}
}

func TestInventoryBusinessPhaseDiagnosticsKeepsStrictEnvironment(t *testing.T) {
	const flag = "DARK_PRO_ENABLE_BUSINESS_PHASE_DIAGNOSTICS"
	for _, tc := range []struct {
		name        string
		environment []string
	}{
		{name: "malformed_value", environment: []string{flag + "=true"}},
		{name: "duplicate_value", environment: []string{flag + "=1", flag + "=1"}},
		{name: "conflicting_value", environment: []string{flag + "=1", flag + "=0"}},
		{name: "unknown_enabled", environment: []string{flag + "=1", "DARK_PRO_ENABLE_UNRECOGNIZED_DIAGNOSTIC=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := fixtureOptions(privateTemp(t))
			o.Environment = append(o.Environment, tc.environment...)
			if _, err := BuildInventory(o); !errors.Is(err, ErrUnsafe) {
				t.Fatal("unsafe diagnostic environment was accepted", err)
			}
		})
	}
}
