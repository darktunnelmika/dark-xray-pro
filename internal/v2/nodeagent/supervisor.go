package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Supervisor keeps one stock Xray process running with the last config that
// passed `xray run -test`. A rejected config never replaces a working one,
// and the active config survives agent and host restarts on disk.
type Supervisor struct {
	Binary string
	Dir    string
	Logf   func(string, ...any)

	mu       sync.Mutex
	cmd      *exec.Cmd
	exited   chan struct{}
	restarts uint64
	lastErr  string
	activeID string
	wake     chan struct{}
}

func NewSupervisor(binary, dir string, logf func(string, ...any)) (*Supervisor, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &Supervisor{Binary: binary, Dir: dir, Logf: logf, wake: make(chan struct{}, 1)}
	if b, err := os.ReadFile(s.activePath()); err == nil {
		s.activeID = configID(b)
	}
	return s, nil
}

func (s *Supervisor) activePath() string    { return filepath.Join(s.Dir, "active.json") }
func (s *Supervisor) candidatePath() string { return filepath.Join(s.Dir, "candidate.json") }

func configID(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Test validates cfg with the real Xray binary without touching the
// running process.
func (s *Supervisor) Test(ctx context.Context, cfg []byte) error {
	if err := writeFileAtomic(s.candidatePath(), cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, s.Binary, "run", "-test", "-c", s.candidatePath())
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := out.String()
		if len(msg) > 600 {
			msg = msg[len(msg)-600:]
		}
		return fmt.Errorf("xray rejected config: %v: %s", err, msg)
	}
	return nil
}

// Apply installs cfg if it differs from the active one and passes Test,
// then restarts Xray. It returns changed=false when cfg is already active.
func (s *Supervisor) Apply(ctx context.Context, cfg []byte) (changed bool, err error) {
	id := configID(cfg)
	s.mu.Lock()
	same := id == s.activeID
	s.mu.Unlock()
	if same {
		return false, nil
	}
	if err := s.Test(ctx, cfg); err != nil {
		s.setErr(err)
		return false, err
	}
	if err := os.Rename(s.candidatePath(), s.activePath()); err != nil {
		return false, err
	}
	s.mu.Lock()
	s.activeID = id
	s.mu.Unlock()
	s.restart()
	return true, nil
}

func (s *Supervisor) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.lastErr = ""
	} else {
		s.lastErr = err.Error()
	}
}

// restart stops the current process; Run starts it again immediately with
// the new active config.
func (s *Supervisor) restart() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Status reports whether Xray is running and how often it was restarted.
func (s *Supervisor) Status() (running bool, restarts uint64, lastErr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil, s.restarts, s.lastErr
}

// Run keeps Xray alive until ctx ends. Crashes are restarted with a capped
// backoff; a missing active config just waits for the first Apply.
func (s *Supervisor) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if _, err := os.Stat(s.activePath()); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.setErr(err)
			s.Logf("xray exited: %v", err)
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			backoff = time.Second
		case <-time.After(backoff):
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}
}

func (s *Supervisor) runOnce(ctx context.Context) error {
	cmd := exec.Command(s.Binary, "run", "-c", s.activePath())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	s.mu.Lock()
	s.cmd = cmd
	s.restarts++
	s.mu.Unlock()
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case err = <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			err = <-done
		}
	}
	s.mu.Lock()
	s.cmd = nil
	s.mu.Unlock()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == -1 {
		// Terminated by our own restart signal.
		return nil
	}
	return err
}
