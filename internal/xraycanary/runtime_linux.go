//go:build linux

// SandboxRuntime performs REAL process-level apply/rollback on isolated
// ephemeral LOOPBACK listeners only. It never supervises a system Xray
// service, changes public ports, edits /etc or publishes subscriptions.
package xraycanary

import (
  "context"
  "errors"
  "io"
  "net"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"
  "sync"
  "syscall"
  "time"

  "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

var (
  ErrSandboxDisabled = errors.New("sandbox runtime requires non-root isolated LAB opt-in")
  ErrSandboxClosed = errors.New("sandbox runtime is closed")
  ErrSandboxPending = errors.New("previous runtime rollback snapshot is still pending")
  ErrSandboxNoRollback = errors.New("no healthy previous sandbox process exists")
  ErrSandboxNotReady = errors.New("isolated Xray sandbox process did not become ready")
)

type SandboxSnapshot struct {
  Generation uint64 `json:"generation"`
  ActiveConfigSHA string `json:"active_config_sha"`
  PreviousConfigSHA string `json:"previous_config_sha"`
  ActiveLoopbackBound bool `json:"active_loopback_bound"`
  PreviousLoopbackBound bool `json:"previous_loopback_bound"`
  LastEvent string `json:"last_event"`
  CustomerDataPlaneVerified bool `json:"customer_data_plane_verified"`
  CanPublishSubscriptions bool `json:"can_publish_subscriptions"`
}

type sandboxProcess struct {
  command *exec.Cmd
  tempDir string
  configSHA string
  ports []int
  done chan struct{}
}

type SandboxRuntime struct {
  mu sync.Mutex
  binary, sha string
  current, previous *sandboxProcess
  generation uint64
  lastEvent string
  closed bool
}

func sandboxAllowed() bool {
  return os.Geteuid() != 0 &&
    os.Getenv("DARK_PRO_LAB_MODE") == "1" &&
    os.Getenv("DARK_PRO_ENABLE_SANDBOX_APPLY") == "1"
}

// NewSandboxRuntime is an opt-in in-memory rehearsal controller, not a
// Node Manager API, systemd adapter or a durable rollout engine.
func NewSandboxRuntime(binary, trustedSHA string) (*SandboxRuntime, error) {
  if !sandboxAllowed() { return nil, ErrSandboxDisabled }
  if !filepath.IsAbs(binary) || len(trustedSHA) != 64 {
    return nil, xrayconfig.ErrCoreUnavailable
  }
  return &SandboxRuntime{binary: binary, sha: trustedSHA, lastEvent: "not_started"}, nil
}

func sandboxDead(p *sandboxProcess) bool {
  if p == nil { return true }
  select { case <-p.done: return true; default: return false }
}

func sandboxStop(p *sandboxProcess) {
  if p == nil { return }
  // The direct subprocess may already have been reaped. Never send a
  // negative-PID group kill after Wait completed: that PID could be reused.
  if !sandboxDead(p) && p.command.Process != nil {
    // Kill ONLY the process group created by this exact unprivileged sandbox.
    _ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
    _ = p.command.Process.Kill()
  }
  select { case <-p.done: case <-time.After(1400 * time.Millisecond): }
  _ = os.RemoveAll(p.tempDir)
}

func sandboxPortsBound(ports []int) bool {
  if len(ports) == 0 { return false }
  for _, port := range ports {
    con, err := net.DialTimeout("tcp4",
      net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 65 * time.Millisecond)
    if err != nil { return false }
    _ = con.Close()
  }
  return true
}

// sandboxStart ALWAYS hashes/validates the immutable candidate with a locally
// pinned Xray executable and remaps the config before creating any listener.
// Config bytes and core output never appear in status, audit, or HTTP response.
func sandboxStart(ctx context.Context, c xrayconfig.Candidate, binary, sha string) (*sandboxProcess, error) {
  if !sandboxAllowed() { return nil, ErrSandboxDisabled }
  if _, err := xrayconfig.ValidateOffline(ctx, c, binary, sha); err != nil { return nil, err }
  private, ports, err := remap(c)
  if err != nil { return nil, err }
  dir, err := os.MkdirTemp("", "dark-pro-sandbox-only-")
  if err != nil { return nil, ErrSandboxNotReady }
  path := filepath.Join(dir, "candidate.json")
  if err := os.WriteFile(path, private, 0600); err != nil {
    _ = os.RemoveAll(dir)
    return nil, ErrSandboxNotReady
  }
  cmd := exec.Command(binary, "run", "-config", path)
  cmd.Dir = dir
  cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
  cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
  cmd.WaitDelay = 250 * time.Millisecond
  cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
  if err := cmd.Start(); err != nil {
    _ = os.RemoveAll(dir)
    return nil, ErrSandboxNotReady
  }
  p := &sandboxProcess{
    command: cmd, tempDir: dir, configSHA: c.SHA256,
    ports: ports, done: make(chan struct{}),
  }
  go func() { _ = cmd.Wait(); close(p.done) }()
  startup, cancel := context.WithTimeout(ctx, 6 * time.Second)
  defer cancel()
  ticker := time.NewTicker(60 * time.Millisecond)
  defer ticker.Stop()
  for {
    if sandboxDead(p) {
      sandboxStop(p)
      return nil, ErrSandboxNotReady
    }
    if sandboxPortsBound(ports) && !sandboxDead(p) {
      return p, nil
    }
    select {
    case <-startup.Done():
      sandboxStop(p)
      return nil, ErrSandboxNotReady
    case <-p.done:
      sandboxStop(p)
      return nil, ErrSandboxNotReady
    case <-ticker.C:
    }
  }
}

func (s *SandboxRuntime) snapshotLocked() SandboxSnapshot {
  view := SandboxSnapshot{
    Generation: s.generation, LastEvent: s.lastEvent,
    CustomerDataPlaneVerified: false, CanPublishSubscriptions: false,
  }
  if s.current != nil && !sandboxDead(s.current) {
    view.ActiveConfigSHA = s.current.configSHA
    view.ActiveLoopbackBound = true
  }
  if s.previous != nil && !sandboxDead(s.previous) {
    view.PreviousConfigSHA = s.previous.configSHA
    view.PreviousLoopbackBound = true
  }
  return view
}

// When a newly selected real Xray sandbox process exits, keep the previous
// still-running snapshot as the active rehearsal revision without any user
// action. With no healthy snapshot, fail closed (no active process).
func (s *SandboxRuntime) observe(p *sandboxProcess) {
  <-p.done
  s.mu.Lock()
  defer s.mu.Unlock()
  if s.closed { return }
  switch {
  case s.current == p:
    _ = os.RemoveAll(p.tempDir)
    if s.previous != nil && !sandboxDead(s.previous) {
      s.current, s.previous = s.previous, nil
      s.lastEvent = "auto_rollback_after_process_exit"
    } else {
      if s.previous != nil {
        sandboxStop(s.previous)
      }
      s.current, s.previous = nil, nil
      s.lastEvent = "fail_closed_after_process_exit"
    }
    s.generation++
  case s.previous == p:
    _ = os.RemoveAll(p.tempDir)
    s.previous = nil
    s.lastEvent = "rollback_snapshot_exited"
    s.generation++
  default:
    _ = os.RemoveAll(p.tempDir)
  }
}

func (s *SandboxRuntime) reconcileLocked() {
  if s.current != nil && sandboxDead(s.current) {
    dead := s.current
    s.current = nil
    _ = os.RemoveAll(dead.tempDir)
    if s.previous != nil && !sandboxDead(s.previous) {
      s.current, s.previous = s.previous, nil
      s.lastEvent = "auto_rollback_after_process_exit"
    } else {
      if s.previous != nil { sandboxStop(s.previous) }
      s.previous = nil
      s.lastEvent = "fail_closed_after_process_exit"
    }
    s.generation++
  }
  if s.previous != nil && sandboxDead(s.previous) {
    _ = os.RemoveAll(s.previous.tempDir)
    s.previous = nil
    s.lastEvent = "rollback_snapshot_exited"
    s.generation++
  }
}

func (s *SandboxRuntime) Snapshot() SandboxSnapshot {
  s.mu.Lock()
  defer s.mu.Unlock()
  s.reconcileLocked()
  return s.snapshotLocked()
}

// Apply starts a new *real* Xray process only at 127.0.0.1:ephemeral and
// verifies socket binds BEFORE selecting it. Current stays running if new
// process fails; on success it remains as a separate rollback process.
// A second rollout is refused until Rollback or Finalize.
func (s *SandboxRuntime) Apply(ctx context.Context, c xrayconfig.Candidate) (SandboxSnapshot, error) {
  s.mu.Lock()
  defer s.mu.Unlock()
  if !sandboxAllowed() { return s.snapshotLocked(), ErrSandboxDisabled }
  if s.closed { return s.snapshotLocked(), ErrSandboxClosed }
  s.reconcileLocked()
  if s.previous != nil { return s.snapshotLocked(), ErrSandboxPending }
  proc, err := sandboxStart(ctx, c, s.binary, s.sha)
  if err != nil { return s.snapshotLocked(), err }
  s.previous, s.current = s.current, proc
  s.generation++
  s.lastEvent = "loopback_process_applied_not_published"
  go s.observe(proc)
  return s.snapshotLocked(), nil
}

// Rollback promotes the still-live previous process without rebinding any
// original Node port. No previous healthy process means fail-closed refusal.
func (s *SandboxRuntime) Rollback() (SandboxSnapshot, error) {
  s.mu.Lock()
  defer s.mu.Unlock()
  if s.closed { return s.snapshotLocked(), ErrSandboxClosed }
  if !sandboxAllowed() { return s.snapshotLocked(), ErrSandboxDisabled }
  s.reconcileLocked()
  if s.previous == nil { return s.snapshotLocked(), ErrSandboxNoRollback }
  if sandboxDead(s.previous) {
    sandboxStop(s.current)
    sandboxStop(s.previous)
    s.current, s.previous = nil, nil
    s.generation++
    s.lastEvent = "fail_closed_previous_process_unavailable"
    return s.snapshotLocked(), ErrSandboxNoRollback
  }
  active := s.current
  s.current, s.previous = s.previous, nil
  s.generation++
  s.lastEvent = "manual_rollback_to_previous_loopback_process"
  sandboxStop(active)
  return s.snapshotLocked(), nil
}

// Finalize retires the previous sandbox process only when the currently
// selected new process is still running. It NEVER commits a subscription.
func (s *SandboxRuntime) Finalize() (SandboxSnapshot, error) {
  s.mu.Lock()
  defer s.mu.Unlock()
  if s.closed { return s.snapshotLocked(), ErrSandboxClosed }
  if !sandboxAllowed() { return s.snapshotLocked(), ErrSandboxDisabled }
  s.reconcileLocked()
  if s.current == nil || sandboxDead(s.current) {
    return s.snapshotLocked(), ErrSandboxNotReady
  }
  if s.previous != nil {
    old := s.previous
    s.previous = nil
    sandboxStop(old)
    s.generation++
    s.lastEvent = "previous_sandbox_process_retired_not_published"
  }
  return s.snapshotLocked(), nil
}

// Close only terminates process groups spawned by THIS sandbox manager.
// It never runs systemctl/killall or accesses another user's Xray service.
func (s *SandboxRuntime) Close() {
  s.mu.Lock()
  defer s.mu.Unlock()
  if s.closed { return }
  s.closed = true
  sandboxStop(s.current)
  sandboxStop(s.previous)
  s.current, s.previous = nil, nil
  s.generation++
  s.lastEvent = "sandbox_closed"
}
