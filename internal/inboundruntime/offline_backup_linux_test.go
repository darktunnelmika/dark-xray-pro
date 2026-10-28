//go:build linux

package inboundruntime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineJournalValidationHasNoRecoveryWrites(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	key := []byte(strings.Repeat("k", 64))
	node := "hub-local"
	install := strings.Repeat("a", 32)
	j, _, e := openJournal(dir, key, node, install)
	if e != nil {
		t.Fatal(e)
	}
	j.close()
	path := filepath.Join(dir, "state.enc")
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	got, e := ValidateOfflineJournal(path, key, node, install)
	if e != nil || got.Phase != "idle" || len(got.Epochs) != 0 {
		t.Fatal("valid empty journal rejected", e)
	}
	after, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("offline validation mutated recovery state")
	}
	if _, e = ValidateOfflineJournal(path, []byte(strings.Repeat("x", 64)), node, install); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = ValidateOfflineJournal(path, key, node, strings.Repeat("b", 32)); e == nil {
		t.Fatal("wrong installation accepted")
	}
	if e = os.WriteFile(path, append(before, 0), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateOfflineJournal(path, key, node, install); e == nil {
		t.Fatal("corrupt journal accepted")
	}
}
