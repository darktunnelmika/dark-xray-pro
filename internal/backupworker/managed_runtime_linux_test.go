//go:build linux

package backupworker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedRuntimeBackupAdmission(t *testing.T) {
	root := t.TempDir()
	fixed := filepath.Join(root, "inbound-execution")
	for _, tc := range []struct {
		name, flag, dir, native, ledger string
		accepted                        bool
	}{
		{"disabled", "", "", "", "", true},
		{"enabled", "1", fixed, "1", "1", true},
		{"retained journal", "0", fixed, "", "", true},
		{"missing journal directory", "1", "", "1", "1", false},
		{"external directory", "1", "/tmp/inbound-execution", "1", "1", false},
		{"noncanonical alias", "1", root + "/x/../inbound-execution", "1", "1", false},
		{"unknown flag", "true", fixed, "1", "1", false},
		{"missing ledger", "1", fixed, "1", "", false},
		{"missing native ownership", "1", fixed, "", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := managedRuntimeDirectory(map[string]string{
				"DARK_PRO_ENABLE_INBOUND_EXECUTION": tc.flag,
				"DARK_PRO_INBOUND_RUNTIME_DIR":      tc.dir,
				"DARK_PRO_ENABLE_NATIVE_INBOUNDS":   tc.native,
				"DARK_PRO_ENABLE_USAGE_LEDGER":      tc.ledger,
			}, root)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
			if err == nil && got != tc.dir {
				t.Fatalf("directory changed: %q", got)
			}
		})
	}
}

func TestManagedRuntimeBackupPrivateDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inbound-execution")
	host := &LinuxRuntime{uid: uint32(os.Geteuid())}
	if err := host.checkManagedRuntimeDirectory(dir); err == nil {
		t.Fatal("missing directory admitted")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := host.checkManagedRuntimeDirectory(dir); err != nil {
		t.Fatal(err)
	}
	other := &LinuxRuntime{uid: uint32(os.Geteuid() + 1)}
	if err := other.checkManagedRuntimeDirectory(dir); err == nil {
		t.Fatal("wrong owner admitted")
	}
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := host.checkManagedRuntimeDirectory(dir); err == nil {
		t.Fatal("group-readable journal directory admitted")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if err := host.checkManagedRuntimeDirectory(alias); err == nil {
		t.Fatal("symlink admitted")
	}
	file := filepath.Join(root, "regular")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := host.checkManagedRuntimeDirectory(file); err == nil {
		t.Fatal("regular file admitted")
	}
}
