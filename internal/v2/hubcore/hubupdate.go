package hubcore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Updating the HUB from the panel: the owner uploads a new dark-hub2
// binary. It must run and name its version before it replaces the current
// one; the current binary is kept as .prev, and a binary that fails to stay
// up three starts in a row is swapped back by the next start (TrialStart).

var hubVersionRE = regexp.MustCompile(`^dark-hub2 (v\d+\.\d+\.\d+)`)

func hubExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// StageHubUpdate checks and installs an uploaded HUB binary; the caller
// restarts the HUB.
func StageHubUpdate(ctx context.Context, r io.Reader) (from, to string, err error) {
	exe, err := hubExe()
	if err != nil {
		return "", "", err
	}
	tmp := exe + ".new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", "", fmt.Errorf("%w: the HUB cannot write next to its program (%v) — update its service file (ReadWritePaths) or update from the server", ErrInvalid, err)
	}
	_, err = io.Copy(f, io.LimitReader(r, 200<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	head := make([]byte, 4)
	if fh, err := os.Open(tmp); err == nil {
		_, _ = io.ReadFull(fh, head)
		fh.Close()
	}
	if !bytes.Equal(head, []byte("\x7fELF")) {
		os.Remove(tmp)
		return "", "", fmt.Errorf("%w: this is not a Linux program (dark-hub2)", ErrInvalid)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, tmp, "version").CombinedOutput()
	m := hubVersionRE.FindStringSubmatch(strings.TrimSpace(string(out)))
	if err != nil || m == nil {
		os.Remove(tmp)
		return "", "", fmt.Errorf("%w: the uploaded file does not run as dark-hub2 here (wrong file or wrong CPU type)", ErrInvalid)
	}
	if err := os.Rename(exe, exe+".prev"); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(exe+".prev", exe)
		return "", "", err
	}
	_ = os.WriteFile(exe+".trial", []byte("0"), 0o600)
	return HubVersion, m[1], nil
}

// TrialStart runs first thing at start-up. After an update, three starts
// that never stayed up put the previous binary back.
func TrialStart(logf func(string, ...any)) {
	exe, err := hubExe()
	if err != nil {
		return
	}
	trial := exe + ".trial"
	b, err := os.ReadFile(trial)
	if err != nil {
		return
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &n)
	if n >= 3 {
		if _, err := os.Stat(exe + ".prev"); err == nil {
			if err := os.Rename(exe+".prev", exe); err == nil {
				os.Remove(trial)
				logf("the updated HUB failed to stay up; the previous version is back — restarting")
				os.Exit(1)
			}
		}
		os.Remove(trial)
		return
	}
	_ = os.WriteFile(trial, []byte(fmt.Sprint(n+1)), 0o600)
	go func() {
		time.Sleep(90 * time.Second)
		os.Remove(trial) // stayed up: the update is kept
	}()
}
