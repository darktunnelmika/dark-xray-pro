//go:build linux

package restoreapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreSpaceIncludesOriginalWALAndAtomicRollback(t *testing.T) {
	root, work := t.TempDir(), t.TempDir()
	for _, p := range []string{root, work} {
		if err := os.Chmod(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "business.sqlite"), make([]byte, 19), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "business.sqlite-wal"), make([]byte, 23), 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(make([]byte, 3))
	replacements := []Replacement{
		{Destination: "business.sqlite", Present: true, Size: 3, SHA256: hex.EncodeToString(h[:])},
		{Destination: "business.sqlite-wal"},
		{Destination: "business.sqlite-shm"},
	}
	needs, err := spaceNeeds(root, work, replacements, os.Geteuid())
	if err != nil {
		t.Fatal(err)
	}
	var copies, temporary int64
	for _, need := range needs {
		copies += need.copies
		temporary += need.temporary
	}
	if copies != 45 || temporary != 23 {
		t.Fatalf("incomplete originals/candidate/rollback budget: %d %d", copies, temporary)
	}
}

func TestRestoreSpaceRefusesUnsafeSourceBeforeWriterFence(t *testing.T) {
	root, work := t.TempDir(), t.TempDir()
	for _, p := range []string{root, work} {
		if err := os.Chmod(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "hub-state.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(work, "outside"), filepath.Join(root, "business.sqlite")); err != nil {
		t.Fatal(err)
	}
	tx, err := Open(root, work, "11111111111111111111111111111111", os.Geteuid(), os.Getegid())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if err = tx.Prepare(context.Background(), []Replacement{{Destination: "business.sqlite"}}); err == nil {
		t.Fatal("unsafe target accepted")
	}
	if _, err = os.Stat(filepath.Join(work, "plan.json")); !os.IsNotExist(err) {
		t.Fatal("unsafe target published plan")
	}
}
