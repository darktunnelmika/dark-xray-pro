package main

import "testing"

func TestSellerOrdersLabScopeDefaultAndStrictFlag(t *testing.T) {
	for _, value := range []string{"", "0"} {
		t.Run("disabled_"+value, func(t *testing.T) {
			enabled, err := sellerOrdersLabEnabled(func(key string) string {
				if key != "DARK_PRO_ENABLE_SELLER_ORDERS_V1" {
					t.Fatalf("disabled gate consulted prerequisite %s", key)
				}
				return value
			})
			if enabled || err != nil {
				t.Fatalf("disabled flag: enabled=%v err=%v", enabled, err)
			}
		})
	}
	for _, value := range []string{"true", "yes", "2", "01", " 1", "1\n"} {
		enabled, err := sellerOrdersLabEnabled(func(string) string { return value })
		if enabled || err == nil {
			t.Fatalf("accepted ambiguous flag %q: enabled=%v err=%v", value, enabled, err)
		}
	}
}

func TestSellerOrdersLabScopeRequiresEveryAccountingDependency(t *testing.T) {
	config := map[string]string{
		"DARK_PRO_ENABLE_SELLER_ORDERS_V1":     "1",
		"DARK_PRO_LAB_MODE":                    "1",
		"DARK_PRO_ENABLE_BUSINESS_CORE":        "1",
		"DARK_PRO_ENABLE_USAGE_LEDGER":         "1",
		"DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2": "1",
		"DARK_PRO_ENABLE_NATIVE_INBOUNDS":      "1",
		"DARK_PRO_ENABLE_VOLUME_REFUNDS_V1":    "1",
	}
	getenv := func(key string) string { return config[key] }
	if enabled, err := sellerOrdersLabEnabled(getenv); !enabled || err != nil {
		t.Fatalf("complete isolated scope rejected: enabled=%v err=%v", enabled, err)
	}
	for key := range config {
		if key == "DARK_PRO_ENABLE_SELLER_ORDERS_V1" {
			continue
		}
		for _, value := range []string{"", "0", "true", " 1"} {
			config[key] = value
			if enabled, err := sellerOrdersLabEnabled(getenv); enabled || err == nil {
				t.Fatalf("accepted missing or ambiguous prerequisite %s=%q", key, value)
			}
		}
		config[key] = "1"
	}
}
