package main

import "fmt"

// sellerOrdersLabEnabled validates the opt-in before any Store is opened.
// The API gate also checks the configured dependencies before adding routes.
func sellerOrdersLabEnabled(getenv func(string) string) (bool, error) {
	flag := getenv("DARK_PRO_ENABLE_SELLER_ORDERS_V1")
	if flag == "" || flag == "0" {
		return false, nil
	}
	if flag != "1" {
		return false, fmt.Errorf("DARK_PRO_ENABLE_SELLER_ORDERS_V1 must be 0 or 1")
	}
	for _, prerequisite := range []string{
		"DARK_PRO_LAB_MODE",
		"DARK_PRO_ENABLE_BUSINESS_CORE",
		"DARK_PRO_ENABLE_USAGE_LEDGER",
		"DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2",
		"DARK_PRO_ENABLE_NATIVE_INBOUNDS",
		"DARK_PRO_ENABLE_VOLUME_REFUNDS_V1",
	} {
		if getenv(prerequisite) != "1" {
			return false, fmt.Errorf("seller orders require explicit %s=1", prerequisite)
		}
	}
	return true, nil
}
