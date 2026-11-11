//go:build linux

package corebarrierv2_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Turns the complete protocol/transport matrix into a real autonomous-lease
// client as well. Both modes remain mandatory: compatibility and new capture.
func attachFixtureAutonomousLease(t *testing.T, cmd *exec.Cmd, bin string, config map[string]any, data []byte, controlled bool) (*os.File, func()) {
	t.Helper()
	if !controlled || os.Getenv("DXP_NATIVE_LEASE_MATRIX") != "1" {
		return nil, nil
	}
	var port uint16
	inbounds, _ := config["inbounds"].([]any)
	for _, v := range inbounds {
		m, _ := v.(map[string]any)
		if m["tag"] != barrier.ControlTag {
			continue
		}
		b, e := json.Marshal(m["port"])
		if e != nil {
			t.Fatal(e)
		}
		n, e := strconv.ParseUint(strings.Trim(string(b), "\""), 10, 16)
		if e != nil || n == 0 {
			t.Fatal("fixture control port missing")
		}
		port = uint16(n)
	}
	if port == 0 {
		t.Fatal("autonomous fixture must have a private control endpoint")
	}
	fd, e := unix.MemfdCreate("fixture-native-final", unix.MFD_ALLOW_SEALING|unix.MFD_CLOEXEC)
	if e != nil {
		t.Fatal(e)
	}
	capture := os.NewFile(uintptr(fd), "fixture-native-final")
	binding := make([]byte, 32)
	if _, e = rand.Read(binding); e != nil {
		capture.Close()
		t.Fatal(e)
	}
	digest := sha256.Sum256(data)
	childFD := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, capture)
	cmd.Env = append(cmd.Env, "DARK_PRO_NATIVE_FINAL_FD="+strconv.Itoa(childFD), "DARK_PRO_NATIVE_FINAL_BINDING="+fmt.Sprintf("%x", binding), "DARK_PRO_NATIVE_CONFIG_SHA256="+fmt.Sprintf("%x", digest), "DARK_PRO_NATIVE_LEASE_DEADLINE="+strconv.FormatInt(time.Now().Add(30*time.Second).UnixNano(), 10))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			nonce := make([]byte, 32)
			if _, e := rand.Read(nonce); e != nil {
				return
			}
			pattern := barrier.Protocol + "lease/" + strconv.FormatInt(time.Now().Add(30*time.Second).UnixNano(), 10) + "/" + fmt.Sprintf("%x", nonce)
			call, stop := context.WithTimeout(ctx, 3*time.Second)
			p := exec.CommandContext(call, bin, "api", "statsquery", "--server="+endpoint(port), "--timeout=2", "-pattern", pattern)
			p.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp", "GORACE=halt_on_error=1 atexit_sleep_ms=0"}
			_, _ = p.CombinedOutput()
			stop()
		}
	}()
	var once sync.Once
	return capture, func() { once.Do(func() { cancel(); <-done }) }
}
