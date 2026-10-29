package main

import "testing"

func TestBackupControlRequiresExplicitOfflineLABScope(t *testing.T) {
	for _, flag := range []string{"", "0"} {
		enabled, err := backupControlEnabled(func(k string) string {
			if k == "DARK_PRO_ENABLE_BACKUP_CONTROL_V1" {
				return flag
			}
			t.Fatal("disabled gate inspected prerequisites")
			return ""
		})
		if enabled || err != nil {
			t.Fatal("disabled gate rejected", flag, err)
		}
	}
	values := map[string]string{"DARK_PRO_ENABLE_BACKUP_CONTROL_V1": "1", "DARK_PRO_LAB_MODE": "1", "DARK_PRO_ENABLE_BUSINESS_CORE": "1", "DARK_PRO_ENABLE_OFFLINE_BACKUP_V1": "1"}
	getenv := func(k string) string { return values[k] }
	if enabled, err := backupControlEnabled(getenv); !enabled || err != nil {
		t.Fatal("valid explicit configuration", err)
	}
	for key := range values {
		for _, bad := range []string{"true", " 1", "2", "01"} {
			values[key] = bad
			if enabled, err := backupControlEnabled(getenv); enabled || err == nil {
				t.Fatal("accepted ambiguous gate", key, bad)
			}
		}
		values[key] = "1"
	}
}
