package main

import "fmt"

func clientOwnershipLabEnabled(getenv func(string) string) (bool, error) {
	flag := getenv("DARK_PRO_ENABLE_CLIENT_OWNERSHIP_V1")
	if flag == "" || flag == "0" {
		return false, nil
	}
	if flag != "1" {
		return false, fmt.Errorf("DARK_PRO_ENABLE_CLIENT_OWNERSHIP_V1 must be 0 or 1")
	}
	for _, key := range []string{"DARK_PRO_LAB_MODE", "DARK_PRO_ENABLE_BUSINESS_CORE", "DARK_PRO_ENABLE_USAGE_LEDGER"} {
		if getenv(key) != "1" {
			return false, fmt.Errorf("client ownership requires explicit %s=1", key)
		}
	}
	return true, nil
}
