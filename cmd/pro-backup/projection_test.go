package main

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"testing"
)

func TestBackupProjectionCompatibility(t *testing.T) {
	inv := hubstate.Inventory{Environment: map[string]string{"DARK_PRO_ENABLE_NATIVE_INBOUNDS": "1"}}
	legacy := features(inv)
	current := backupFeatures(inv)
	if !compatibleBackupFeatures(inv, legacy) || !compatibleBackupFeatures(inv, current) {
		t.Fatal("supported backup rejected")
	}
	if sameFeatures(legacy, current) {
		t.Fatal("old reader would silently accept new projection")
	}
	for _, version := range []int{0, 2, -1} {
		invalid := backupFeatures(inv)
		invalid[zeroDeltaProjectionFeature] = version
		if compatibleBackupFeatures(inv, invalid) {
			t.Fatal("unknown projection accepted", version)
		}
	}
	invalid := backupFeatures(inv)
	invalid["unknown"] = 1
	if compatibleBackupFeatures(inv, invalid) {
		t.Fatal("unknown feature accepted")
	}
	if current[zeroDeltaProjectionFeature] != 1 {
		t.Fatal("metadata mutated")
	}
}
