//go:build linux

package xraycanary

import (
  "context"
  "errors"
  "net"
  "os"
  "strconv"
  "testing"
  "time"

  "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func sandboxLab(t *testing.T) {
  t.Helper()
  lab(t) // rejects root and sets the independent loopback-canary flag
  t.Setenv("DARK_PRO_ENABLE_SANDBOX_APPLY", "1")
}

func assertSandboxPorts(t *testing.T, proc *sandboxProcess) {
  t.Helper()
  if proc == nil || len(proc.ports) == 0 { t.Fatal("missing real sandbox process") }
  if sandboxDead(proc) || !sandboxPortsBound(proc.ports) { t.Fatal("selected sandbox process not listening") }
  for _, port := range proc.ports {
    conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
    if err != nil { t.Fatal("sandbox listener not reachable:", err) }
    _ = conn.Close()
    if port == 18569 || port == 11185 { t.Fatal("original Node listener port used") }
  }
}

func TestSandboxRealProcessApplyAndManualRollback(t *testing.T) {
  sandboxLab(t)
  binary, sha := fixture(t, "listen")
  s, err := NewSandboxRuntime(binary, sha)
  if err != nil { t.Fatal(err) }
  defer s.Close()

  original := candidate(t, true)
  alternate := candidate(t, false)
  first, err := s.Apply(context.Background(), original)
  if err != nil { t.Fatal("initial sandbox apply:", err) }
  if first.ActiveConfigSHA != original.SHA256 || first.PreviousConfigSHA != "" ||
    first.CanPublishSubscriptions || first.CustomerDataPlaneVerified {
    t.Fatalf("initial Apply falsely claimed public activation: %+v", first)
  }
  previous := s.current
  assertSandboxPorts(t, previous)
  firstPorts := append([]int(nil), previous.ports...)

  second, err := s.Apply(context.Background(), alternate)
  if err != nil { t.Fatal("second sandbox apply:", err) }
  if second.ActiveConfigSHA != alternate.SHA256 ||
    second.PreviousConfigSHA != original.SHA256 ||
    !second.ActiveLoopbackBound || !second.PreviousLoopbackBound {
    t.Fatalf("did not keep the running previous process: %+v", second)
  }
  assertSandboxPorts(t, s.current)
  assertSandboxPorts(t, s.previous)
  if _, err := s.Apply(context.Background(), alternate); !errors.Is(err, ErrSandboxPending) {
    t.Fatalf("second rollout was allowed before rollback/finalize: %v", err)
  }

  back, err := s.Rollback()
  if err != nil { t.Fatal("manual sandbox rollback:", err) }
  if back.ActiveConfigSHA != original.SHA256 || back.PreviousConfigSHA != "" ||
    back.LastEvent != "manual_rollback_to_previous_loopback_process" {
    t.Fatalf("rollback did not restore the original live process: %+v", back)
  }
  for _, port := range firstPorts {
    conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
    if err != nil { t.Fatalf("previous port %d lost after rollback: %v", port, err) }
    _ = conn.Close()
  }
  if _, err := s.Rollback(); !errors.Is(err, ErrSandboxNoRollback) {
    t.Fatalf("repeated rollback unexpectedly toggled: %v", err)
  }
}

func TestSandboxFailedCandidateNeverStopsPreviousProcess(t *testing.T) {
  sandboxLab(t)
  binary, sha := fixture(t, "listen")
  s, err := NewSandboxRuntime(binary, sha)
  if err != nil { t.Fatal(err) }
  defer s.Close()
  healthy := candidate(t, true)
  _, err = s.Apply(context.Background(), healthy)
  if err != nil { t.Fatal(err) }
  original := s.current
  corrupted := candidate(t, false)
  corrupted.Config = append([]byte(nil), corrupted.Config...)
  corrupted.Config[0] ^= 1
  if _, err := s.Apply(context.Background(), corrupted); !errors.Is(err, xrayconfig.ErrCandidateInvalid) {
    t.Fatalf("tampered candidate was not rejected: %v", err)
  }
  assertSandboxPorts(t, original)
  if s.previous != nil || s.current != original {
    t.Fatal("failed apply disturbed previous running process")
  }

  ctx, cancel := context.WithCancel(context.Background())
  cancel()
  if _, err := s.Apply(ctx, candidate(t, false)); err == nil {
    t.Fatal("cancelled apply was accepted")
  }
  assertSandboxPorts(t, original)
}

func TestSandboxFinalizeAndCloseRetireOnlyItsOwnProcesses(t *testing.T) {
  sandboxLab(t)
  binary, sha := fixture(t, "listen")
  s, err := NewSandboxRuntime(binary, sha)
  if err != nil { t.Fatal(err) }
  one := candidate(t, true)
  two := candidate(t, false)
  if _, err := s.Apply(context.Background(), one); err != nil { t.Fatal(err) }
  old := s.current
  if _, err := s.Apply(context.Background(), two); err != nil { t.Fatal(err) }
  state, err := s.Finalize()
  if err != nil { t.Fatal(err) }
  if state.PreviousConfigSHA != "" || state.ActiveConfigSHA != two.SHA256 {
    t.Fatalf("finalize changed new config: %+v", state)
  }
  select { case <-old.done: case <-time.After(2*time.Second): t.Fatal("previous process not retired") }
  active := s.current
  assertSandboxPorts(t, active)
  s.Close()
  s.Close()
  select { case <-active.done: case <-time.After(2*time.Second): t.Fatal("sandbox failed to close its process") }
  state = s.Snapshot()
  if state.ActiveConfigSHA != "" || state.PreviousConfigSHA != "" || state.CanPublishSubscriptions {
    t.Fatalf("closed sandbox retained a live candidate: %+v", state)
  }
  if _, err := s.Apply(context.Background(), one); !errors.Is(err, ErrSandboxClosed) {
    t.Fatalf("closed manager restarted a process: %v", err)
  }
}

func TestSandboxOptInAndWrongCorePinFailClosed(t *testing.T) {
  binary, sha := fixture(t, "listen")
  t.Setenv("DARK_PRO_LAB_MODE", "")
  t.Setenv("DARK_PRO_ENABLE_SANDBOX_APPLY", "1")
  if _, err := NewSandboxRuntime(binary, sha); !errors.Is(err, ErrSandboxDisabled) {
    t.Fatalf("unexpected sandbox activation without LAB: %v", err)
  }
  t.Setenv("DARK_PRO_LAB_MODE", "1")
  t.Setenv("DARK_PRO_ENABLE_SANDBOX_APPLY", "")
  if _, err := NewSandboxRuntime(binary, sha); !errors.Is(err, ErrSandboxDisabled) {
    t.Fatalf("unexpected sandbox activation without opt-in: %v", err)
  }
  if os.Geteuid() == 0 { return } // permanently disallowed for root
  t.Setenv("DARK_PRO_ENABLE_SANDBOX_APPLY", "1")
  forgedSHA := "0000000000000000000000000000000000000000000000000000000000000000"
  s, err := NewSandboxRuntime(binary, forgedSHA)
  if err != nil { t.Fatal(err) }
  defer s.Close()
  state, err := s.Apply(context.Background(), candidate(t, false))
  if !errors.Is(err, xrayconfig.ErrCoreMismatch) || state.ActiveConfigSHA != "" {
    t.Fatalf("wrong executable SHA started a process: %+v %v", state, err)
  }
}

func TestRealPinnedXraySandboxApplyRollbackOptIn(t *testing.T) {
  binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
  if binary == "" && pin == "" { t.Skip("separately pinned real Xray unavailable") }
  if binary == "" || pin == "" { t.Fatal("both real core executable and SHA required") }
  sandboxLab(t)
  s, err := NewSandboxRuntime(binary, pin)
  if err != nil { t.Fatal(err) }
  defer s.Close()
  before := candidate(t, true)
  after := candidate(t, false)
  ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
  defer cancel()
  if _, err := s.Apply(ctx, before); err != nil { t.Fatal("real Xray first apply:", err) }
  original := s.current
  assertSandboxPorts(t, original)
  if _, err := s.Apply(ctx, after); err != nil { t.Fatal("real Xray second apply:", err) }
  assertSandboxPorts(t, s.current)
  assertSandboxPorts(t, s.previous)
  restored, err := s.Rollback()
  if err != nil { t.Fatal("real Xray rollback:", err) }
  if restored.ActiveConfigSHA != before.SHA256 || restored.CanPublishSubscriptions ||
    restored.CustomerDataPlaneVerified || restored.PreviousConfigSHA != "" {
    t.Fatalf("real process rollback incorrectly published traffic: %+v", restored)
  }
  assertSandboxPorts(t, s.current)
}
