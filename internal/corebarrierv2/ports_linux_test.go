//go:build linux

package corebarrierv2_test

import (
	"sync"
	"testing"
)

func TestFixturePortsRemainDistinctAfterProbeSocketsClose(t *testing.T) {
	// freePort returns a number after closing its probe sockets. The fixture
	// selects several endpoints before starting either core, so availability
	// alone is insufficient: a later allocation must not return an earlier one.
	seen := make(map[uint16]bool)
	for i := 0; i < 512; i++ {
		port := freePort(t)
		if seen[port] {
			t.Fatalf("fixture port %d was reused after %d allocations", port, i)
		}
		seen[port] = true
	}
}

func TestFixturePortsRemainDistinctAcrossConcurrentFixtures(t *testing.T) {
	var seen sync.Map
	for i := 0; i < 8; i++ {
		t.Run("fixture", func(t *testing.T) {
			t.Parallel()
			for j := 0; j < 32; j++ {
				port := freePort(t)
				if _, exists := seen.LoadOrStore(port, true); exists {
					t.Fatalf("concurrent fixtures reused port %d", port)
				}
			}
		})
	}
}
