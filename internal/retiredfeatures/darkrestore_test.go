package retiredfeatures

import (
	"strings"
	"testing"
)

func TestRejectRetiredRuntimeBeforeFallback(t *testing.T) {
	switches := []string{"DARK_PRO_ENABLE_DARKRESTORE_QUARANTINE", "DARK_PRO_ENABLE_DARKRESTORE_RUNTIME", "DARK_PRO_ENABLE_IMPORTED_CONNECTION_CHECK", "DARK_PRO_ENABLE_IMPORTED_SYSTEM_PROBE_CHECK"}
	for _, key := range switches {
		for _, value := range []string{"1", "true", "invalid", " "} {
			err := Check(func(k string) string {
				if k == key {
					return value
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%q accepted", key, value)
			}
		}
		for _, value := range []string{"", "0"} {
			if err := Check(func(k string) string {
				if k == key {
					return value
				}
				return ""
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, key := range []string{"DARK_PRO_QUARANTINE_PLAN_SHA", "DARK_PRO_QUARANTINE_SIGNER_FILE", "DARK_PRO_QUARANTINE_RECEIPTS_DIR", "DARK_PRO_IMPORTED_VERIFIER_JSON"} {
		err := Check(func(k string) string {
			if k == key {
				return "private-value"
			}
			return ""
		})
		if err == nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("invalid configuration diagnostic: %v", err)
		}
	}
	if err := Check(func(k string) string {
		if k == "DARK_PRO_ENABLE_NATIVE_RUNTIME" {
			return "1"
		}
		return ""
	}); err != nil {
		t.Fatal(err)
	}
}
