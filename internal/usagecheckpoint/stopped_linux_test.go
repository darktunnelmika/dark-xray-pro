//go:build linux

package usagecheckpoint

import (
	"strings"
	"testing"
)

func TestFinalTCPStoppedBirthDoesNotGuessFromUnreadableState(t *testing.T) {
	stat := func(state, tick string) []byte {
		return []byte("123 (name with ) parentheses) " + state + strings.Repeat(" 0", 18) + " " + tick + " 0")
	}
	for _, state := range []string{"R", "S", "D", "T", "I"} {
		if stoppedBirth(stat(state, "456"), 456) {
			t.Fatal("live birth treated as stopped", state)
		}
	}
	for _, state := range []string{"Z", "X"} {
		if !stoppedBirth(stat(state, "456"), 456) {
			t.Fatal("exited birth rejected", state)
		}
	}
	if !stoppedBirth(stat("S", "457"), 456) {
		t.Fatal("recycled PID confused with old child")
	}
	for _, raw := range [][]byte{nil, []byte("123 (bad"), []byte("123 (short) Z"), stat("Z", "0"), stat("Z", "bad")} {
		if stoppedBirth(raw, 456) {
			t.Fatal("invalid /proc evidence treated as exit")
		}
	}
}
