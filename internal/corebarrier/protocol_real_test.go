package corebarrier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Test the actual patched protocol boundary independently of the PRO runtime:
// rejected commands never reach dispatch, active TCP has exact byte counts,
// counter reset is refused and a completed drain cannot admit another stream.
// The plain loopback VLESS fixture is not an admitted native deployment config.
func TestRealPinnedCoreFinalTCPProtocol(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("explicit experimental core required")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if !filepath.IsAbs(binary) || hex.EncodeToString(digest[:]) != pin {
		t.Fatal("pinned experimental core required")
	}
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, e := echo.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	port := func() int {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	dataPort, apiPort := port(), port()
	for dataPort == apiPort {
		apiPort = port()
	}
	api := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	doc := map[string]any{"log": map[string]any{"loglevel": "none"}, "stats": map[string]any{}, "policy": map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}}, "api": map[string]any{"tag": "api", "listen": api, "services": []string{"StatsService"}}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": dataPort, "protocol": "vless", "settings": map[string]any{"decryption": "none", "clients": []any{map[string]any{"id": "11111111-1111-4111-8111-111111111111", "email": "one@pro.invalid", "level": 0}}}}}, "outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": strconv.Itoa(echo.Addr().(*net.TCPAddr).Port)}}}}}}
	raw, _ = json.Marshal(doc)
	config := filepath.Join(t.TempDir(), "core.json")
	if err = os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "run", "-config", config)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "DARK_PRO_FINAL_TCP_V1=1"}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	query := func(pattern string, reset bool) ([]byte, error) {
		call := exec.CommandContext(ctx, binary, "api", "statsquery", "--server="+api, "--timeout=1", "-pattern="+pattern, "-reset="+strconv.FormatBool(reset))
		call.Env = []string{"PATH=/usr/bin:/bin"}
		return call.Output()
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		raw, err = query(Protocol+"capability", false)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("core capability unavailable", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !bytes.Contains(raw, []byte(Protocol+"scope/vless-direct-tcp")) {
		t.Fatal("missing capability")
	}
	connect := func(command byte) net.Conn {
		c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(dataPort)), time.Second)
		if e != nil {
			t.Fatal(e)
		}
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		id, _ := hex.DecodeString("11111111111141118111111111111111")
		header := append([]byte{0}, id...)
		header = append(header, 0, command)
		if command == 1 || command == 2 {
			p := echo.Addr().(*net.TCPAddr).Port
			header = append(header, byte(p>>8), byte(p), 1, 127, 0, 0, 1)
		}
		_, _ = c.Write(append(header, 0, 1, 42))
		return c
	}
	for _, command := range []byte{2, 3, 4} {
		c := connect(command)
		b := make([]byte, 2)
		n, e := c.Read(b)
		c.Close()
		if n != 0 || e == nil {
			t.Fatalf("unsupported command %d was processed", command)
		}
		if timeout, ok := e.(net.Error); ok && timeout.Timeout() {
			t.Fatalf("unsupported command %d was not explicitly closed", command)
		}
	}
	c := connect(1)
	defer c.Close()
	// connect sent three payload bytes, echoed after the VLESS response header.
	response := make([]byte, 5)
	if _, err = io.ReadFull(c, response); err != nil || !bytes.Equal(response, []byte{0, 0, 0, 1, 42}) {
		t.Fatal("TCP echo", response, err)
	}
	payload := bytes.Repeat([]byte{9}, 65536)
	write := make(chan error, 1)
	go func() { _, e := c.Write(payload); write <- e }()
	got := make([]byte, len(payload))
	if _, err = io.ReadFull(c, got); err != nil || !bytes.Equal(payload, got) {
		t.Fatal("active TCP payload", err)
	}
	if err = <-write; err != nil {
		t.Fatal(err)
	}
	if _, err = query("user>>>", true); err == nil {
		t.Fatal("counter reset accepted")
	}
	nonce := strings.Repeat("a", 64)
	final, err := query(Protocol+"drain/"+nonce, false)
	if err != nil {
		t.Fatal("real active drain", err)
	}
	var stats struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stat"`
	}
	if json.Unmarshal(final, &stats) != nil || len(stats.Stat) != 3 {
		t.Fatal("invalid final stats", string(final))
	}
	values := map[string]string{}
	for _, s := range stats.Stat {
		values[s.Name] = strings.Trim(string(s.Value), `"`)
	}
	if values[Protocol+"sealed/"+nonce] != "1" || values["user>>>one@pro.invalid>>>traffic>>>uplink"] != "65539" || values["user>>>one@pro.invalid>>>traffic>>>downlink"] != "65539" {
		t.Fatal("final bytes differ from exact echo payload", values)
	}
	if _, err = c.Read(make([]byte, 1)); err == nil {
		t.Fatal("drained stream survived")
	}
	after := connect(1)
	n, e := after.Read(make([]byte, 2))
	after.Close()
	if n != 0 || e == nil {
		t.Fatal("drained core admitted another request")
	}
	replay, err := query(Protocol+"drain/"+nonce, false)
	if err != nil || !bytes.Equal(final, replay) {
		t.Fatal("final snapshot changed", err)
	}
	if _, err = query(Protocol+"drain/"+strings.Repeat("b", 64), false); err == nil {
		t.Fatal("conflicting nonce accepted")
	}
}
