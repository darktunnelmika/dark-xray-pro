package main

import "errors"

// Validate the diagnostic opt-in before any state, store or listener is opened.
// This is an internal LAB diagnostic; it adds no user-facing endpoint.
func businessPhaseDiagnosticsEnabled(getenv func(string) string) (bool, error) {
	switch getenv("DARK_PRO_ENABLE_BUSINESS_PHASE_DIAGNOSTICS") {
	case "", "0":
		return false, nil
	case "1":
		if getenv("DARK_PRO_ENABLE_BUSINESS_CORE") != "1" {
			return false, errors.New("business phase diagnostics require the explicit private PRO business store")
		}
		return true, nil
	default:
		return false, errors.New("DARK_PRO_ENABLE_BUSINESS_PHASE_DIAGNOSTICS must be 0 or 1")
	}
}
