package main

import "github.com/darktunnelmika/dark-xray-pro/internal/hubstate"

const zeroDeltaProjectionFeature = "usage_zero_delta_projection"

func backupFeatures(inv hubstate.Inventory) map[string]int {
	out := features(inv)
	out[zeroDeltaProjectionFeature] = 1
	return out
}

// Legacy archives without a projection remain supported. Unknown projection
// versions and unrelated feature mismatches fail closed; do not mutate metadata.
func compatibleBackupFeatures(inv hubstate.Inventory, actual map[string]int) bool {
	copy := make(map[string]int, len(actual))
	for name, version := range actual {
		if name == zeroDeltaProjectionFeature {
			if version != 1 {
				return false
			}
			continue
		}
		copy[name] = version
	}
	return sameFeatures(features(inv), copy)
}
