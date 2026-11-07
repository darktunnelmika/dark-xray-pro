//go:build linux

package corebarrierv2_test

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

// The target deliberately withholds its handshake until the test has observed
// closed admission. This exercises real detector startup without timing a race
// against a fast local target or relying on a certificate-verification failure.
type heldRealityTarget struct {
	net.Listener
	released chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func (l *heldRealityTarget) Accept() (net.Conn, error) {
	select {
	case <-l.released:
		return l.Listener.Accept()
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *heldRealityTarget) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.Listener.Close()
}

func TestPinnedNativeRealityStartupReadiness(t *testing.T) {
	bin := nativeBinary(t)
	for _, network := range []string{"tcp", "grpc", "xhttp"} {
		t.Run(network, func(t *testing.T) {
			pair := fixtureCertificate(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			held := &heldRealityTarget{Listener: server.Listener, released: make(chan struct{}), closed: make(chan struct{})}
			server.Listener = held
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.EnableHTTP2 = true
			server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
			server.StartTLS()
			t.Cleanup(server.Close)
			trust := filepath.Join(t.TempDir(), "fixture-ca.pem")
			if err := os.WriteFile(trust, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}), 0600); err != nil {
				t.Fatal(err)
			}
			spec := fixtures(t)["vless"]
			spec.Network, spec.Security = network, "reality"
			spec.Reality = map[string]any{
				"target":      server.Listener.Addr().String(),
				"privateKey":  base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x12}, 32)),
				"serverNames": []any{"fixture.example.test"}, "shortIds": []any{"1122334455667788"},
			}
			spec.Client = map[string]any{"fingerprint": "chrome"}
			host := targetAddress(t)
			target, listen, api, clientPort := echoTarget(t, host), freePort(t), freePort(t), freePort(t)
			user := inboundspec.User{ID: 1, UUID: "11111111-1111-4111-8111-111111111111", Email: "startup@example.test"}
			rendered, err := inboundspec.Render(spec, "127.0.0.1", listen, "customer", []inboundspec.User{user})
			if err != nil {
				t.Fatal(err)
			}
			config := map[string]any{
				"log": map[string]any{"loglevel": "none"}, "stats": map[string]any{},
				"policy":    map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}},
				"api":       map[string]any{"tag": "private-api", "services": []string{"StatsService"}},
				"inbounds":  []any{rendered.Inbound, map[string]any{"tag": barrier.ControlTag, "listen": "127.0.0.1", "port": api, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}},
				"outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{host + "/32"}, "port": strconv.Itoa(int(target))}}}}},
				"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{barrier.ControlTag}, "outboundTag": "private-api"}}},
			}
			p := startCoreWithEnv(t, bin, config, true, []string{"SSL_CERT_FILE=" + trust})
			var health map[string]int64
			for until := time.Now().Add(8 * time.Second); time.Now().Before(until); {
				health, err = query(bin, api, barrier.Protocol+"health", false)
				if err == nil {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if err != nil {
				t.Fatal("private API unavailable during startup", err)
			}
			nonce := strings.Repeat("a", 64)
			for i := 0; i < 3; i++ {
				if health[barrier.Protocol+"api-ready"] != 1 || health[barrier.Protocol+"engine-ready"] != 0 || health[barrier.Protocol+"admission-open"] != 0 {
					t.Fatal("REALITY was declared ready before its target initialization finished", health)
				}
				if _, err := query(bin, api, barrier.Protocol+"activate/"+nonce, false); err == nil {
					t.Fatal("early activation bypassed REALITY initialization")
				}
				time.Sleep(50 * time.Millisecond)
				health, err = query(bin, api, barrier.Protocol+"health", false)
				if err != nil {
					t.Fatal(err)
				}
			}
			zero, err := query(bin, api, "", false)
			if err != nil {
				t.Fatal(err)
			}
			for name, n := range zero {
				if strings.HasPrefix(name, "user>>>") && n != 0 {
					t.Fatal("startup produced customer usage", zero)
				}
			}
			close(held.released)
			waitCore(t, bin, api, p)
			if _, err := query(bin, api, barrier.Protocol+"activate/"+nonce, false); err != nil {
				t.Fatal("same nonce could not activate after startup completed", err)
			}
			proxyClient(t, bin, spec, user, listen, clientPort, false)
			firstStarted := time.Now()
			c, _ := socks(t, clientPort, target, host, 1)
			// No artificial warmup follows readiness. SOCKS negotiation and the
			// first payload share the original four-second acceptance budget.
			_ = c.SetDeadline(firstStarted.Add(4 * time.Second))
			payload := bytes.Repeat([]byte("ready-first-payload/"), 64)
			if _, err := c.Write(payload); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(c, got); err != nil || !bytes.Equal(got, payload) {
				t.Fatal("first ready connection failed", err)
			}
			_ = c.Close()
			finalNonce := strings.Repeat("b", 64)
			final, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, direction := range []string{"uplink", "downlink"} {
				if final["user>>>startup@example.test>>>traffic>>>"+direction] < int64(len(payload)) {
					t.Fatal("final receipt omitted the first payload", final)
				}
			}
			replayed, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
			if err != nil || !reflect.DeepEqual(final, replayed) {
				t.Fatal("final receipt changed on replay", err)
			}
		})
	}
}
