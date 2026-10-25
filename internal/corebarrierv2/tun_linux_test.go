//go:build linux

package corebarrierv2_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

// Run only through scripts/ci/test-inbound-tun.sh. The script supplies separate
// network, mount and PID namespaces, a private /dev and no external interface.
// The single benchmark-address route and kernel TUN belong to that namespace.
func TestPinnedNativeTUNNamespaceTrafficAndFinalReceipt(t *testing.T) {
	if os.Getenv("DXP_INBOUND_TUN_NAMESPACE") != "1" {
		t.Skip("run the privileged isolated TUN acceptance harness")
	}
	for _, namespace := range []string{"net", "mnt"} {
		outer := os.Getenv("DXP_TUN_OUTER_" + strings.ToUpper(namespace) + "NS")
		current, err := os.Readlink("/proc/self/ns/" + namespace)
		if err != nil || outer == "" || current == outer {
			t.Fatalf("TUN test requires its own %s namespace", namespace)
		}
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated TUN test requires namespace administrator privileges")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Name != "lo" {
			t.Fatal("refusing a namespace with an existing non-loopback interface")
		}
	}
	if info, err := os.Stat("/dev/net/tun"); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		t.Fatal("private /dev/net/tun is unavailable")
	}
	bin := nativeBinary(t)
	target, api := echoTarget(t, "127.0.0.1"), freePort(t)
	const ifaceName, tag, virtualIP = "dxp-v2-test", "customer-tun", "198.18.0.2"
	spec := inboundspec.Spec{Version: 1, Protocol: "tun", Network: "tun", Security: "none", Settings: map[string]any{
		"name": ifaceName, "mtu": 1500, "gateway": []any{"10.77.0.1/30"}, "dns": []any{},
		"autoSystemRoutingTable": []any{virtualIP + "/32"},
	}}
	rendered, err := inboundspec.Render(spec, "", 0, tag, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := map[string]any{
		"log": map[string]any{"loglevel": "none"}, "stats": map[string]any{},
		"policy":   map[string]any{"system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}},
		"api":      map[string]any{"tag": "private-api", "services": []string{"StatsService"}},
		"inbounds": []any{rendered.Inbound, map[string]any{"tag": barrier.ControlTag, "listen": "127.0.0.1", "port": api, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}},
		// Redirect only this disposable synthetic target to the namespace's own
		// echo server. Neither default routes nor DNS configuration are changed.
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom", "settings": map[string]any{
			"redirect": endpoint(target), "finalRules": []any{map[string]any{"action": "allow", "network": "tcp,udp", "ip": []string{"127.0.0.1/32"}, "port": strconv.Itoa(int(target))}},
		}}},
		"routing": map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{barrier.ControlTag}, "outboundTag": "private-api"}}},
	}
	p := startCore(t, bin, server, true)
	waitCore(t, bin, api, p)
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		t.Fatal("TUN reported ready without an active kernel interface")
	}
	routes, err := exec.Command("ip", "-j", "route", "show", virtualIP+"/32").Output()
	var rows []struct {
		Dev string `json:"dev"`
	}
	if err != nil || json.Unmarshal(routes, &rows) != nil || len(rows) != 1 || rows[0].Dev != ifaceName {
		t.Fatalf("synthetic destination does not use the owned TUN: %s, %v", routes, err)
	}
	virtualTarget := net.JoinHostPort(virtualIP, strconv.Itoa(int(target)))
	if c, err := net.DialTimeout("tcp4", virtualTarget, time.Second); err == nil {
		_ = c.SetDeadline(time.Now().Add(300 * time.Millisecond))
		_, _ = c.Write([]byte("before-activation"))
		var b [1]byte
		if n, _ := c.Read(b[:]); n > 0 {
			t.Fatal("TUN passed payload before activation")
		}
		_ = c.Close()
	}
	zero, err := query(bin, api, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range zero {
		if strings.HasPrefix(name, "inbound>>>"+tag+">>>") && value != 0 {
			t.Fatal("TUN charged traffic before activation", zero)
		}
	}
	nonce := strings.Repeat("a", 64)
	activation, err := query(bin, api, barrier.Protocol+"activate/"+nonce, false)
	if err != nil || activation[barrier.Protocol+"activated/"+nonce] != 1 {
		t.Fatal(activation, err)
	}
	tcp, err := net.DialTimeout("tcp4", virtualTarget, 3*time.Second)
	if err != nil {
		t.Fatal("actual kernel TUN TCP connection: ", err)
	}
	defer tcp.Close()
	payload := bytes.Repeat([]byte("kernel-tun-owner-payload/"), 191)
	sendTCP(t, tcp, payload)
	udp, err := net.Dial("udp4", virtualTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(5 * time.Second))
	datagram := []byte("kernel-tun-owner-datagram")
	if _, err = udp.Write(datagram); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 1024)
	n, err := udp.Read(b)
	if err != nil || !bytes.Equal(b[:n], datagram) {
		t.Fatalf("actual kernel TUN UDP payload: %v", err)
	}
	live, err := query(bin, api, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"uplink", "downlink"} {
		if live["inbound>>>"+tag+">>>traffic>>>"+direction] < int64(len(payload)+len(datagram)) {
			t.Fatal("missing owner aggregate TUN accounting", live)
		}
	}
	for name := range live {
		if strings.HasPrefix(name, "user>>>") {
			t.Fatal("TUN invented a per-user identity", live)
		}
	}
	finalNonce := strings.Repeat("b", 64)
	final, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
	if err != nil || final[barrier.Protocol+"final/"+finalNonce] != 1 {
		t.Fatal("kernel TUN final barrier: ", final, err)
	}
	_ = tcp.SetDeadline(time.Now().Add(time.Second))
	_, _ = tcp.Write([]byte("after-final"))
	if n, _ := io.ReadFull(tcp, make([]byte, 11)); n > 0 {
		t.Fatal("kernel TUN TCP traffic survived finalization")
	}
	for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
		if _, err = net.InterfaceByName(ifaceName); err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = net.InterfaceByName(ifaceName); err == nil {
		t.Fatal("final receipt left the owned TUN interface behind")
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
			t.Fatal("TUN owner counters changed or were lost after finalization")
		}
	}
	if _, err = query(bin, api, barrier.Protocol+"activate/"+nonce, false); err == nil {
		t.Fatal("finalized kernel TUN was reactivated")
	}
	t.Log("KERNEL_TUN_TCP_UDP_ACCOUNTING_FINAL_PASS")
}
