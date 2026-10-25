//go:build linux

package inboundruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// This helper is a separate executable with no inherited credentials or open
// directory descriptors. A missing socket is not accepted as isolation proof.
func TestNativePrivateUnixDeniedUIDHelper(t *testing.T) {
	if os.Getenv("DXP_NATIVE_DENIED_UID_HELPER") != "1" {
		return
	}
	if os.Getuid() != 65534 || os.Getgid() != 65534 {
		t.Fatal("helper retained privileged identity")
	}
	for _, path := range []string{os.Getenv("DXP_NATIVE_CONTROL_DIRECT"), os.Getenv("DXP_NATIVE_CONTROL_FD")} {
		if path == "" {
			t.Fatal("missing private path")
		}
		conn, err := net.DialTimeout("unix", path, time.Second)
		if err == nil {
			_ = conn.Close()
			t.Fatal("foreign UID reached private control")
		}
		if !errors.Is(err, syscall.EACCES) && !errors.Is(err, syscall.EPERM) {
			t.Fatalf("expected access denial, got %v", err)
		}
	}
}

func TestNativePrivateUnixControlLifecycleAndCrossUID(t *testing.T) {
	path := os.Getenv("DARK_PRO_TEST_INBOUND_CORE")
	if path == "" {
		t.Skip("set the separately pinned native acceptance executable")
	}
	if os.Geteuid() != 0 {
		t.Skip("run this isolation acceptance as root to drop the helper to uid/gid 65534")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := OpenBinary(path, hashBytes(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	port, err := reservePort()
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{
		"inbounds":  []any{map[string]any{"tag": "private-control-http", "protocol": "http", "listen": "127.0.0.1", "port": port, "settings": map[string]any{"accounts": []any{map[string]any{"user": "private-control-user", "pass": "private-pass"}}, "userLevel": 0}}},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}},
	})
	bundle := ix.Bundle{NativeConfig: config, Listeners: []ix.Listener{{Tag: "private-control-http", Engine: "xray", Listen: "127.0.0.1", Port: port, Network: "tcp", ResourceKind: "socket"}}, Principals: []ix.Principal{{ID: "private-control-principal", Engine: "xray", Counter: "user>>>private-control-user>>>traffic"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	process, err := StartNative(ctx, t.TempDir(), binary, bundle, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer process.Stop()
	for _, entry := range []struct {
		path string
		mode os.FileMode
	}{{process.dir, 0700}, {filepath.Join(process.dir, "stats.sock"), 0600}} {
		info, err := os.Lstat(entry.path)
		if err != nil || info.Mode().Perm() != entry.mode {
			t.Fatalf("private control mode %s: %v %v", entry.path, info, err)
		}
	}
	if !process.ownsUnixControl() {
		t.Fatal("socket is not owned by the exact child")
	}
	before, err := process.Counters(ctx)
	if err != nil || len(before) != 1 || before[0].Upload != 0 || before[0].Download != 0 {
		t.Fatalf("zero receipt: %v %v", before, err)
	}
	// Put only the helper executable in a temporary traversable directory.
	// The target birth and both control path forms remain untouched/private.
	helperDir, err := os.MkdirTemp("", "dxp-native-denied-uid-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(helperDir)
	if err = os.Chmod(helperDir, 0755); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	helper := filepath.Join(helperDir, "probe.test")
	output, err := os.OpenFile(helper, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, source)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(copyErr, closeErr)
	}
	if err = os.Chmod(helper, 0755); err != nil {
		t.Fatal(err)
	}
	// Drop credentials after exec in the helper process. Using setpriv also
	// avoids a pre-exec credential transition in the supervisor's vfork child.
	setpriv, err := exec.LookPath("setpriv")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, setpriv, "--reuid=65534", "--regid=65534", "--clear-groups", "--", helper, "-test.run=^TestNativePrivateUnixDeniedUIDHelper$", "-test.v")
	command.Dir = helperDir
	command.Env = []string{"PATH=/usr/bin:/bin", "DXP_NATIVE_DENIED_UID_HELPER=1", "DXP_NATIVE_CONTROL_DIRECT=" + filepath.Join(process.dir, "stats.sock"), "DXP_NATIVE_CONTROL_FD=" + process.apiSocket}
	if text, err := command.CombinedOutput(); err != nil {
		t.Fatalf("foreign UID isolation: %v %s", err, text)
	}
	if err = process.Activate(ctx, strings.Repeat("a", 64)); err != nil {
		markers, probeErr := process.query(ctx, nativeProtocol+"health")
		t.Fatalf("same-UID activation after foreign-UID denial: %v; alive=%v owned=%v health=%v query=%v", err, process.checkIdentity(), process.ownsUnixControl(), markers, probeErr)
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "owned private-control fixture")
	}))
	defer target.Close()
	proxy, _ := url.Parse("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	proxy.User = url.UserPassword("private-control-user", "private-pass")
	transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != 200 || string(body) != "owned private-control fixture" {
		t.Fatal("authenticated local traffic failed", readErr)
	}
	final, err := process.Final(ctx, strings.Repeat("b", 64))
	if err != nil || len(final) != 1 || final[0].Upload == 0 || final[0].Download == 0 {
		t.Fatalf("final receipt: %v %v", final, err)
	}
	replay, err := process.Final(ctx, strings.Repeat("b", 64))
	if err != nil || !reflect.DeepEqual(final, replay) {
		t.Fatal("final replay changed", err)
	}
	if err = process.Activate(ctx, strings.Repeat("a", 64)); err == nil {
		t.Fatal("sealed process reopened")
	}
	if !process.ownsUnixControl() {
		t.Fatal("final removed the control service")
	}
}
