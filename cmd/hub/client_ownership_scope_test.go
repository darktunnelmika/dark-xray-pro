package main

import "testing"

func TestClientOwnershipScopeDefaultOffAndExplicitPrerequisites(t *testing.T) {
	env := map[string]string{"DARK_PRO_ENABLE_CLIENT_OWNERSHIP_V1": "1", "DARK_PRO_LAB_MODE": "1", "DARK_PRO_ENABLE_BUSINESS_CORE": "1", "DARK_PRO_ENABLE_USAGE_LEDGER": "1"}
	get := func(k string) string { return env[k] }
	if enabled, e := clientOwnershipLabEnabled(get); e != nil || !enabled {
		t.Fatal(enabled, e)
	}
	for _, key := range []string{"DARK_PRO_LAB_MODE", "DARK_PRO_ENABLE_BUSINESS_CORE", "DARK_PRO_ENABLE_USAGE_LEDGER"} {
		env[key] = "0"
		if enabled, e := clientOwnershipLabEnabled(get); e == nil || enabled {
			t.Fatal(key, enabled, e)
		}
		env[key] = "1"
	}
	for _, value := range []string{"", "0", "true", "yes", "2"} {
		env["DARK_PRO_ENABLE_CLIENT_OWNERSHIP_V1"] = value
		enabled, e := clientOwnershipLabEnabled(get)
		if enabled || (e == nil) != (value == "" || value == "0") {
			t.Fatal(value, enabled, e)
		}
	}
}
