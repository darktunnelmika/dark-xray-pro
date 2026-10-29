//go:build linux

package backupworker

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// backupCommand keeps the preflight probe and archive writer on the same
// credential, environment, and process-group path. In particular, the child
// must not inherit the root coordinator's supplementary groups.
func (r *LinuxRuntime) backupCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(r.config.BackupBinary, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "DARK_PRO_LAB_MODE=1"}
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: r.uid, Gid: r.gid, Groups: []uint32{}}}
	return cmd
}

// checkCredentialLaunch proves the actual fork/credential/exec path before a
// job can stop HUB. EUID 0 and the capability bounding set alone do not prove
// that the launching thread has the effective capabilities needed to drop UID.
// The pinned CLI rejects an empty argument list with exit 2 before accessing
// HUB state or creating any output. No writer check belongs in service recovery.
func (r *LinuxRuntime) checkCredentialLaunch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return ErrPreflight
	}
	cmd := r.backupCommand(r.archives)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if cmd.Start() != nil {
		return ErrPreflight
	}
	err := waitOwnedGroup(ctx, cmd)
	var exitErr *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		return ErrPreflight
	}
	return nil
}
