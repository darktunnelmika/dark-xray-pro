//go:build linux

package backupacceptance_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The subprocess acceptance uses the same economic fixture as the library
// acceptance. This export is forbidden on installed hosts and does not run in
// ordinary go test. The caller creates a disposable GitHub-hosted VM layout.
func TestExportDisposablePanelRestoreFixture(t *testing.T) {
	if os.Getenv("DARK_PRO_DISPOSABLE_RESTORE_ACCEPTANCE") != "1" {
		t.Skip("disposable systemd fixture export is opt-in")
	}
	if os.Geteuid() != 0 || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		t.Fatal("fixture export requires the dedicated disposable GitHub-hosted job")
	}
	const root = "/var/lib/dark-xray-pro-lab"
	for _, name := range []string{"business.sqlite", "backup-inventory.json", "restore-in-progress.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("existing installation or fixture refused")
		}
	}
	if _, err := os.Stat("/run/dark-pro-disposable-restore-acceptance"); err != nil {
		t.Fatal("disposable setup marker missing")
	}
	f := newEconomicFixture(t)
	in, err := os.Open(f.path)
	require(t, err)
	defer in.Close()
	out, err := os.OpenFile(filepath.Join(root, "business.sqlite"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require(t, err)
	defer out.Close()
	_, err = io.Copy(out, in)
	require(t, err)
	require(t, out.Sync())
	require(t, out.Close())
	t.Log("exported synthetic economic fixture; no customer data")
}
