package main

import (
	"strings"
	"testing"
)

func TestBusinessPhaseDiagnosticsOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, flag, business string
		want, wantError      bool
	}{
		{name: "absent"},
		{name: "disabled", flag: "0", business: "1"},
		{name: "enabled", flag: "1", business: "1", want: true},
		{name: "missing_store", flag: "1", wantError: true},
		{name: "disabled_store", flag: "1", business: "0", wantError: true},
		{name: "unsupported_value", flag: "true", business: "1", wantError: true},
		{name: "whitespace", flag: " 1", business: "1", wantError: true},
		{name: "private_invalid_value", flag: "private-invalid-value", business: "1", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(name string) string {
				switch name {
				case "DARK_PRO_ENABLE_BUSINESS_PHASE_DIAGNOSTICS":
					return tc.flag
				case "DARK_PRO_ENABLE_BUSINESS_CORE":
					return tc.business
				default:
					t.Fatalf("unexpected environment lookup %q", name)
					return ""
				}
			}
			got, err := businessPhaseDiagnosticsEnabled(getenv)
			if got != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("enabled=%v, error=%v; want enabled=%v, error present=%v", got, err, tc.want, tc.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "private-invalid-value") {
				t.Fatal("configuration error included the supplied value")
			}
		})
	}
}
