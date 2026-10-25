//go:build linux

package corebarrierv2_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
)

type ownedDNSServer struct {
	port    uint16
	held    chan struct{}
	release chan struct{}
	once    sync.Once
}

// The upstream server is owned by this test. It supplies real A/AAAA/TXT wire
// answers and can withhold an answer across the worker's final fence.
func dnsTarget(t *testing.T, address string, requestedPort uint16) *ownedDNSServer {
	t.Helper()
	port := requestedPort
	if port == 0 {
		port = freePort(t)
	}
	tcp, err := net.Listen("tcp", net.JoinHostPort(address, strconvPort(port)))
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", net.JoinHostPort(address, strconvPort(port)))
	if err != nil {
		_ = tcp.Close()
		t.Fatal(err)
	}
	s := &ownedDNSServer{port: port, held: make(chan struct{}), release: make(chan struct{})}
	var clients sync.Map
	var jobs sync.WaitGroup
	acceptDone := make(chan struct{})
	answer := func(raw []byte) []byte {
		name, kind, end, err := dnsQuestion(raw)
		if err != nil {
			return nil
		}
		if name == "held.darkpro.test." {
			s.once.Do(func() { close(s.held) })
			<-s.release
		}
		body := dnsAnswerData(kind)
		if body == nil {
			return nil
		}
		out := make([]byte, 12)
		copy(out[:2], raw[:2])
		binary.BigEndian.PutUint16(out[2:4], 0x8180)
		binary.BigEndian.PutUint16(out[4:6], 1)
		binary.BigEndian.PutUint16(out[6:8], 1)
		out = append(out, raw[12:end]...)
		out = append(out, 0xc0, 0x0c, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 30, byte(len(body)>>8), byte(len(body)))
		return append(out, body...)
	}
	jobs.Add(2)
	go func() {
		defer jobs.Done()
		defer close(acceptDone)
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			clients.Store(c, true)
			jobs.Add(1)
			go func() {
				defer jobs.Done()
				defer clients.Delete(c)
				defer c.Close()
				for {
					var n [2]byte
					if _, err := io.ReadFull(c, n[:]); err != nil {
						return
					}
					raw := make([]byte, binary.BigEndian.Uint16(n[:]))
					if _, err := io.ReadFull(c, raw); err != nil {
						return
					}
					out := answer(raw)
					binary.BigEndian.PutUint16(n[:], uint16(len(out)))
					if _, err := c.Write(append(n[:], out...)); err != nil {
						return
					}
				}
			}()
		}
	}()
	go func() {
		defer jobs.Done()
		for {
			buf := make([]byte, 4096)
			n, peer, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			jobs.Add(1)
			go func(raw []byte, peer net.Addr) {
				defer jobs.Done()
				if out := answer(raw); out != nil {
					_, _ = udp.WriteTo(out, peer)
				}
			}(buf[:n], peer)
		}
	}()
	t.Cleanup(func() {
		_ = tcp.Close()
		_ = udp.Close()
		<-acceptDone
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		clients.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
		jobs.Wait()
	})
	return s
}

func strconvPort(p uint16) string {
	// net.JoinHostPort requires a decimal string; keep fixture construction
	// separate from the production compiler's signed configuration.
	return fmt.Sprintf("%d", p)
}

func dnsWireQuery(network, address, name string, kind uint16, timeout time.Duration) (dnsWireAnswer, error) {
	c, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return dnsWireAnswer{}, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	raw := []byte{0x13, 0x36, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if len(label) == 0 || len(label) > 63 {
			return dnsWireAnswer{}, fmt.Errorf("invalid fixture DNS label")
		}
		raw = append(raw, byte(len(label)))
		raw = append(raw, label...)
	}
	raw = append(raw, 0, byte(kind>>8), byte(kind), 0, 1)
	if strings.HasPrefix(network, "tcp") {
		var n [2]byte
		binary.BigEndian.PutUint16(n[:], uint16(len(raw)))
		if _, err = c.Write(append(n[:], raw...)); err != nil {
			return dnsWireAnswer{}, err
		}
		if _, err = io.ReadFull(c, n[:]); err != nil {
			return dnsWireAnswer{}, err
		}
		raw = make([]byte, binary.BigEndian.Uint16(n[:]))
		if _, err = io.ReadFull(c, raw); err != nil {
			return dnsWireAnswer{}, err
		}
	} else {
		if _, err = c.Write(raw); err != nil {
			return dnsWireAnswer{}, err
		}
		raw = make([]byte, 4096)
		n, e := c.Read(raw)
		if e != nil {
			return dnsWireAnswer{}, e
		}
		raw = raw[:n]
	}
	response, err := parseDNSAnswer(raw)
	if err != nil {
		return response, err
	}
	if response.ID != 0x1336 || !response.Response || response.Name != name || response.Kind != kind {
		return response, fmt.Errorf("DNS response identity mismatch")
	}
	return response, nil
}

func dnsConfig(tag string, upstream uint16) (map[string]any, map[string]any) {
	feature := map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": upstream}}, "tag": barrier.DNSTag, "queryStrategy": "UseIP", "useSystemHosts": false, "disableCache": true}
	outbound := map[string]any{"tag": "dark-pro-inbound-dns", "protocol": "dns", "settings": map[string]any{"rewriteAddress": "127.0.0.1", "rewritePort": upstream, "rules": []any{map[string]any{"action": "hijack", "qType": "1,28"}, map[string]any{"action": "direct"}}}}
	return feature, outbound
}

func TestPinnedNativeDNSQueriesAndFinalReceipt(t *testing.T) {
	bin := nativeBinary(t)
	upstream := dnsTarget(t, "127.0.0.1", 0)
	api, listen := freePort(t), freePort(t)
	const tag = "customer-dns"
	feature, outbound := dnsConfig(tag, upstream.port)
	server := map[string]any{
		"log": map[string]any{"loglevel": "none"}, "dns": feature, "stats": map[string]any{},
		"policy": map[string]any{"system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}},
		"api":    map[string]any{"tag": "private-api", "services": []string{"StatsService"}},
		"inbounds": []any{
			map[string]any{"tag": tag, "listen": "127.0.0.1", "port": listen, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1", "port": 53, "network": "tcp,udp"}},
			map[string]any{"tag": barrier.ControlTag, "listen": "127.0.0.1", "port": api, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}},
		},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}, outbound},
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "inboundTag": []string{barrier.ControlTag}, "outboundTag": "private-api"},
			map[string]any{"type": "field", "inboundTag": []string{tag}, "outboundTag": "dark-pro-inbound-dns"},
		}},
	}
	p := startCore(t, bin, server, true)
	waitCore(t, bin, api, p)
	if _, err := dnsWireQuery("udp4", endpoint(listen), "before.darkpro.test.", dnsTypeA, 150*time.Millisecond); err == nil {
		t.Fatal("DNS passed before activation")
	}
	zero, err := query(bin, api, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range zero {
		if value != 0 {
			t.Fatal("preactivation DNS produced counters", zero)
		}
	}
	nonce := strings.Repeat("a", 64)
	if _, err = query(bin, api, barrier.Protocol+"activate/"+nonce, false); err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"udp4", "tcp4"} {
		for _, kind := range []uint16{dnsTypeA, dnsTypeAAAA, dnsTypeTXT} {
			response, err := dnsWireQuery(network, endpoint(listen), "positive.darkpro.test.", kind, 3*time.Second)
			if err != nil || response.RCode != 0 || len(response.Answers) != 1 || response.Answers[0].kind != kind {
				t.Fatalf("real DNS %s/%v response %+v: %v", network, kind, response, err)
			}
			if !bytes.Equal(response.Answers[0].data, dnsAnswerData(kind)) {
				t.Fatal("non-IP DNS answer changed")
			}
		}
	}
	live, err := query(bin, api, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range live {
		if value != 0 && !strings.HasPrefix(name, "inbound>>>"+tag+">>>traffic>>>") {
			t.Fatal("DNS created an unknown financial identity", live)
		}
	}
	for _, direction := range []string{"uplink", "downlink"} {
		if live["inbound>>>"+tag+">>>traffic>>>"+direction] == 0 {
			t.Fatal("DNS owner accounting missing", live)
		}
	}
	heldDone := make(chan error, 1)
	go func() {
		_, err := dnsWireQuery("udp4", endpoint(listen), "held.darkpro.test.", dnsTypeA, 3*time.Second)
		heldDone <- err
	}()
	select {
	case <-upstream.held:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream query did not enter the held real request")
	}
	finalNonce := strings.Repeat("b", 64)
	final, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
	if err != nil || final[barrier.Protocol+"final/"+finalNonce] != 1 {
		t.Fatal("DNS descendant barrier", final, err)
	}
	close(upstream.release)
	if err := <-heldDone; err == nil {
		t.Fatal("held DNS request escaped the final fence")
	}
	replay, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := query(bin, api, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"uplink", "downlink"} {
		name := "inbound>>>" + tag + ">>>traffic>>>" + direction
		if final[name] < live[name] || replay[name] != final[name] || ordinary[name] != final[name] {
			t.Fatal("DNS final changed after withheld upstream response", live, final, replay, ordinary)
		}
	}
	if _, err = query(bin, api, barrier.Protocol+"activate/"+nonce, false); err == nil {
		t.Fatal("DNS reopened after final")
	}
}
