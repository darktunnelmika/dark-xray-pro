//go:build linux

package hubstate

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestResumeBeforeSQLiteRecoveryPreservesCapturedWAL(t *testing.T) {
	lock, inv := fixture(t)
	source := filepath.Join(lock.root, "business.sqlite")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashWALWriterHelper$")
	cmd.Env = append(os.Environ(), "HUBSTATE_TEST_CRASH_WAL=1", "HUBSTATE_TEST_DB="+source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("WAL fixture: %v %s", err, out)
	}
	dest := privateTemp(t)
	ready := false
	_, err := SnapshotWithBusinessTransform(context.Background(), lock, inv, dest, nil, func() error {
		ready = true
		if _, e := os.Stat(filepath.Join(dest, "data/business.sqlite")); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("SQLite recovery still occurs before source release", e)
		}
		if e := lock.Close(); e != nil {
			return e
		}
		// Simulate a resumed source being replaced. Recovery must use captured DB+WAL.
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if e := os.Rename(source+suffix, source+suffix+".replaced"); e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
		return nil
	})
	if err != nil || !ready {
		t.Fatal("snapshot after early resume", err)
	}
	v, err := business.ValidateOfflineBackup(context.Background(), filepath.Join(dest, "data/business.sqlite"))
	if err != nil || v.TableCounts["representatives"] != 2 {
		t.Fatal("committed WAL row lost", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dest, "sqlite-work-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("captured inputs leaked", leftovers, err)
	}
}

func TestFailedSQLiteRecoveryAfterResumeDoesNotPublishInventory(t *testing.T) {
	lock, inv := fixture(t)
	dest := privateTemp(t)
	ready := false
	_, err := SnapshotWithBusinessTransform(context.Background(), lock, inv, dest, nil, func() error {
		ready = true
		inputs, e := filepath.Glob(filepath.Join(dest, "sqlite-work-*", "source.sqlite"))
		if e != nil || len(inputs) == 0 {
			t.Fatal("no captured SQLite inputs", e)
		}
		for _, input := range inputs {
			if e = os.WriteFile(input, []byte("invalid SQLite input"), 0600); e != nil {
				return e
			}
		}
		return lock.Close()
	})
	if err == nil || !ready {
		t.Fatal("invalid private input accepted", err)
	}
	if _, e := os.Stat(filepath.Join(dest, "inventory.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("invalid snapshot published inventory", e)
	}
	leftovers, e := filepath.Glob(filepath.Join(dest, "sqlite-work-*"))
	if e != nil || len(leftovers) != 0 {
		t.Fatal("failed capture cleanup", leftovers, e)
	}
}
