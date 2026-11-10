package main

import "testing"

func TestSnapshotReadyDescriptorIsExplicitAndRestricted(t *testing.T) {
	for _, fd := range []string{"1", "2", "4", "-1"} {
		if _, err := arguments([]string{"create", "--out", "/private/test.backup", "--recipient", "test", "--snapshot-ready-fd", fd}); err == nil {
			t.Fatal("untrusted descriptor accepted", fd)
		}
	}
	o, err := arguments([]string{"create", "--out", "/private/test.backup", "--recipient", "test", "--snapshot-ready-fd", "3"})
	if err != nil || o.snapshotReadyFD != 3 {
		t.Fatal(err)
	}
}
