//go:build linux

package backupworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const credentialTestUID = 65534

func credentialFixture(t *testing.T) *LinuxRuntime {
	t.Helper()
	dir, err := os.MkdirTemp("", "dark-backup-credentials-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	archives := filepath.Join(dir, "archives")
	if err = os.Mkdir(archives, 0710); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(archives, 0710); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(archives, 0, credentialTestUID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "backup-cli")
	// The fixture observes the kernel's actual child credentials, including
	// supplementary groups. Exit 2 is the real CLI's no-argument contract.
	script := fmt.Sprintf(`#!/bin/sh
[ "$#" -eq 0 ] || exit 11
u= g= s=
while IFS= read -r line; do
    case "$line" in
        Uid:*) set -- $line; [ "$2:$3:$4:$5" = "%[1]d:%[1]d:%[1]d:%[1]d" ] || exit 12; u=1 ;;
        Gid:*) set -- $line; [ "$2:$3:$4:$5" = "%[1]d:%[1]d:%[1]d:%[1]d" ] || exit 13; g=1 ;;
        Groups:*) set -- $line; [ "$#" -eq 1 ] || exit 14; s=1 ;;
    esac
done < /proc/self/status
[ "$u$g$s" = 111 ] || exit 15
exit 2
`, credentialTestUID)
	if err = os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0755); err != nil {
		t.Fatal(err)
	}
	return &LinuxRuntime{config: Config{BackupBinary: path}, uid: credentialTestUID, gid: credentialTestUID, archives: archives}
}

func currentCredentialCaps(t *testing.T) [2]unix.CapUserData {
	t.Helper()
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&header, &data[0]); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCredentialLaunchRejectsMissingSetuidBeforeStop(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the real UID/GID transition regression requires root")
	}
	caps := currentCredentialCaps(t)
	const required = 1<<unix.CAP_SETUID | 1<<unix.CAP_SETGID
	if caps[0].Effective&required != required {
		t.Skip("the success control requires effective SETUID and SETGID")
	}
	r := credentialFixture(t)
	if err := r.checkCredentialLaunch(context.Background()); err != nil {
		t.Fatalf("credential transition success control failed: %v", err)
	}
	// Restrict only the disposable helper process. The main test process and
	// its other tests retain their original credentials and capabilities.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCredentialLaunchMissingSetuidHelper$")
	cmd.Env = append(os.Environ(), "DARK_BACKUP_CREDENTIAL_TEST_HELPER="+r.config.BackupBinary,
		"DARK_BACKUP_CREDENTIAL_TEST_ARCHIVES="+r.archives, "GOMAXPROCS=2", "GORACE=atexit_sleep_ms=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("missing-SETUID subprocess regression: %v\n%s", err, out)
	}
	// A launched process that does not honor the no-argument contract must
	// not turn a broken or unexpected executable into a successful preflight.
	if err := os.WriteFile(r.config.BackupBinary, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := r.checkCredentialLaunch(context.Background()); err != ErrPreflight {
		t.Fatalf("unexpected exit accepted or raw error exposed: %v", err)
	}
}

func TestCredentialLaunchMissingSetuidHelper(t *testing.T) {
	path := os.Getenv("DARK_BACKUP_CREDENTIAL_TEST_HELPER")
	if path == "" {
		return
	}
	// Linux capabilities belong to threads. Keep both the capset and the
	// actual fork/exec on this thread, then let this isolated process exit.
	runtime.LockOSThread()
	caps := currentCredentialCaps(t)
	caps[0].Effective &^= 1 << unix.CAP_SETUID
	caps[0].Permitted &^= 1 << unix.CAP_SETUID
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if err := unix.Capset(&header, &caps[0]); err != nil {
		t.Fatal(err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	got := currentCredentialCaps(t)
	bounded, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, unix.CAP_SETUID, 0, 0, 0)
	if err != nil || bounded != 1 || got[0].Effective&(1<<unix.CAP_SETUID) != 0 || got[0].Permitted&(1<<unix.CAP_SETUID) != 0 || got[0].Effective&(1<<unix.CAP_SETGID) == 0 {
		t.Fatal("helper did not reproduce an effective/permitted SETUID deficit with SETGID and bounding SETUID retained")
	}
	r := &LinuxRuntime{config: Config{BackupBinary: path}, uid: credentialTestUID, gid: credentialTestUID,
		archives: os.Getenv("DARK_BACKUP_CREDENTIAL_TEST_ARCHIVES")}
	w, store, host, writer := workerFixture()
	host.preflight = r.checkCredentialLaunch
	if err = w.execute(context.Background(), store.job.ID); err != nil {
		t.Fatal(err)
	}
	j := requireFinished(t, store, "failed", "preflight_refused")
	if host.stops.Load() != 0 || host.starts.Load() != 0 || writer.creates.Load() != 0 || j.PreServiceActive || j.HubStoppedUnix != 0 || j.HubResumedUnix != 0 {
		t.Fatal("failed credential probe disturbed HUB or launched the archive writer")
	}
}

func TestCredentialLaunchRefusesMissingCredentialCapabilities(t *testing.T) {
	caps := currentCredentialCaps(t)
	const required = 1<<unix.CAP_SETUID | 1<<unix.CAP_SETGID
	if caps[0].Effective&required == required {
		t.Skip("restricted-coordinator refusal requires missing credential capabilities")
	}
	// This also covers UID 0 containers without effective capabilities. Use
	// another UID so either missing SETUID or missing SETGID fails at Start.
	r := &LinuxRuntime{config: Config{BackupBinary: "/bin/true"}, uid: uint32(os.Geteuid()) + 1, gid: uint32(os.Getegid()), archives: "/"}
	cmd := r.backupCommand(r.archives)
	if err := cmd.Start(); !errors.Is(err, unix.EPERM) {
		if err == nil {
			cmd.Wait()
		}
		t.Fatalf("credential launch did not fail with EPERM: %v", err)
	}
	if err := r.checkCredentialLaunch(context.Background()); err != ErrPreflight {
		t.Fatalf("restricted launch accepted or raw error exposed: %v", err)
	}
	w, store, host, writer := workerFixture()
	host.preflight = r.checkCredentialLaunch
	if err := w.execute(context.Background(), store.job.ID); err != nil {
		t.Fatal(err)
	}
	requireFinished(t, store, "failed", "preflight_refused")
	if host.stops.Load() != 0 || host.starts.Load() != 0 || writer.creates.Load() != 0 {
		t.Fatal("unavailable credentials were discovered after disturbing HUB")
	}
}
