package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

// Prove the actual compiled Agent CLI, not only an in-process client helper.
// This test uses a disposable TLS server and NEW identities only.
func TestNewNodeProbeAndEnrollOnlyWithRealPinnedTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	dir := t.TempDir()
	prog := filepath.Join(dir, "dark-pro-agent")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", prog, ".")
	buildOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cannot build isolated Agent CLI: %v %s", err, buildOutput)
	}

	reg, err := noderegistry.Open(filepath.Join(dir, "hub-private", "registry.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := reg.Issue("fresh-qa-node", "Fresh test-only QA Agent")
	if err != nil {
		t.Fatal(err)
	}
	fleet := telemetry.NewStore(15*time.Second, nil)
	srv, err := api.NewServer(fleet, reg, strings.Repeat("h", 64))
	if err != nil {
		t.Fatal(err)
	}
	hub := httptest.NewTLSServer(srv.AgentHandler())
	defer hub.Close()
	raw := hub.Certificate().Raw
	sum := sha256.Sum256(raw)
	pin := hex.EncodeToString(sum[:])
	ca := filepath.Join(dir, "trusted-test-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(dir, "node-private", "identity.json")
	args := []string{"-id", "fresh-qa-node", "-hub", hub.URL,
		"-hub-cert-sha256", pin, "-hub-ca-file", ca, "-identity-file", identity}
	run := func(code string, extra ...string) (string, error) {
		a := append(append([]string(nil), args...), extra...)
		cmd := exec.CommandContext(ctx, prog, a...)
		cmd.Env = append(os.Environ(),
			"DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_REMOTE_AGENT=1", "DARK_PRO_PAIR_CODE="+code)
		bytes, e := cmd.CombinedOutput()
		return string(bytes), e
	}
	var output string
	// Strict probe does not create a filesystem identity or submit a Pair Code.
	if output, err = run("", "-probe-only"); err != nil {
		t.Fatalf("verified TLS probe failed: %s %v", output, err)
	}
	if _, err = os.Stat(identity); !os.IsNotExist(err) {
		t.Fatal("probe-only created credentials")
	}
	// Explicitly wrong pin must fail before accepting ticket or creating identity.
	wrong := strings.Repeat("0", 64)
	if wrong == pin {
		wrong = strings.Repeat("1", 64)
	}
	wrongArgs := append([]string(nil), args...)
	for i, a := range wrongArgs {
		if a == "-hub-cert-sha256" {
			wrongArgs[i+1] = wrong
			break
		}
	}
	bad := exec.CommandContext(ctx, prog, append(wrongArgs, "-probe-only")...)
	bad.Env = append(os.Environ(), "DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_REMOTE_AGENT=1")
	if b, e := bad.CombinedOutput(); e == nil {
		t.Fatalf("mismatched certificate pin accepted: %s", b)
	}
	if _, err = os.Stat(identity); !os.IsNotExist(err) {
		t.Fatal("wrong TLS pin created identity")
	}

	// One-use code is sent only after verified HTTPS and secret remains private.
	if output, err = run(ticket.PairCode, "-enroll-only"); err != nil {
		t.Fatalf("isolated enrollment-only failed: %v %s", err, output)
	}
	contents, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(identity)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 || strings.Contains(string(contents), ticket.PairCode) ||
		!strings.Contains(string(contents), `"paired":true`) {
		t.Fatalf("private paired Agent identity not sealed: %s", contents)
	}
	if len(reg.List()) != 1 || reg.List()[0].NodeID != "fresh-qa-node" {
		t.Fatal("HUB registry not paired to isolated Node")
	}
	// Enroll-only does NOT launch host telemetry, customer Xray or listeners.
	if fleet.Snapshot().Total != 0 {
		t.Fatal("enroll-only unexpectedly created live Node telemetry")
	}
	if output, err = run("", "-enroll-only"); err != nil {
		t.Fatalf("paired restart altered identity or refused verified TLS: %v %s", err, output)
	}
	next, err := os.ReadFile(identity)
	if err != nil || string(next) != string(contents) {
		t.Fatal("paired repeat mutated persisted token")
	}
}
