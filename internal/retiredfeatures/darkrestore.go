// Package retiredfeatures rejects configuration for removed product capabilities.
package retiredfeatures

import "fmt"

// Check runs before opening stores, pairing agents or starting workers.
// A stale enabled switch must not silently become a normal runtime.
func Check(getenv func(string) string) error {
	for _, key := range []string{"DARK_PRO_ENABLE_DARKRESTORE_QUARANTINE", "DARK_PRO_ENABLE_DARKRESTORE_RUNTIME", "DARK_PRO_ENABLE_IMPORTED_CONNECTION_CHECK", "DARK_PRO_ENABLE_IMPORTED_SYSTEM_PROBE_CHECK"} {
		if value := getenv(key); value != "" && value != "0" {
			return fmt.Errorf("%s: Dark Restore was removed; remove obsolete configuration", key)
		}
	}
	for _, key := range []string{"DARK_PRO_QUARANTINE_PLAN_SHA", "DARK_PRO_QUARANTINE_SIGNER_FILE", "DARK_PRO_QUARANTINE_RECEIPTS_DIR", "DARK_PRO_IMPORTED_VERIFIER_JSON"} {
		if getenv(key) != "" {
			return fmt.Errorf("%s: Dark Restore configuration is no longer supported", key)
		}
	}
	return nil
}
