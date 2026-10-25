//go:build linux

package corebarrierv2_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	barrier "github.com/darktunnelmika/dark-xray-pro/internal/corebarrierv2"
)

func isolatedResolverCommand(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("DXP_TUN_DNS_TOOLS"), "resolvectl"), args...)
	cmd.Env = append(os.Environ(), "SYSTEMD_PAGER=cat", "LC_ALL=C")
	return cmd.CombinedOutput()
}

func assertIsolatedDNSNamespace(t *testing.T) {
	t.Helper()
	if os.Getenv("DXP_TUN_DNS_NAMESPACE") != "1" {
		t.Skip("run the privileged private D-Bus/resolved TUN acceptance harness")
	}
	if os.Geteuid() != 0 {
		t.Fatal("owned namespace administrator required")
	}
	for _, kind := range []string{"net", "mnt"} {
		current, err := os.Readlink("/proc/self/ns/" + kind)
		outer := os.Getenv("DXP_TUN_OUTER_" + strings.ToUpper(kind) + "NS")
		if err != nil || outer == "" || current == outer {
			t.Fatal("refusing host namespace", kind)
		}
		pid, err := strconv.Atoi(os.Getenv("DXP_TUN_DNS_RESOLVED_PID"))
		if err != nil || pid < 2 {
			t.Fatal("owned resolver process identity missing")
		}
		child, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/%s", pid, kind))
		if err != nil || child != current {
			t.Fatal("resolver escaped the private namespace", kind)
		}
	}
	if info, err := os.Stat("/run/dbus/system_bus_socket"); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatal("private system bus unavailable")
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if iface.Name != "lo" {
			t.Fatal("namespace contains an unowned interface", iface.Name)
		}
	}
	if out, err := isolatedResolverCommand(t, "dns"); err != nil || bytes.Contains(out, []byte("10.78.")) {
		t.Fatal("private resolver baseline", string(out), err)
	}
}

func tunDNSConfiguration(iface, tag string, api uint16) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "none"}, "stats": map[string]any{},
		"policy": map[string]any{"system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}},
		"api":    map[string]any{"tag": "private-api", "services": []string{"StatsService"}},
		"dns":    map[string]any{"servers": []string{"198.18.0.53"}, "tag": barrier.DNSTag, "queryStrategy": "UseIP", "useSystemHosts": false},
		"inbounds": []any{
			map[string]any{"tag": tag, "protocol": "tun", "settings": map[string]any{"name": iface, "mtu": 1500, "gateway": []string{"10.78.0.1/30"}, "dns": []string{"198.18.0.53"}, "autoSystemDnsToGateway": true}},
			map[string]any{"tag": barrier.ControlTag, "listen": "127.0.0.1", "port": api, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}},
		},
		"outbounds": []any{
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "dark-pro-inbound-dns", "protocol": "dns", "settings": map[string]any{"rewriteAddress": "198.18.0.53", "rewritePort": 53, "rules": []any{map[string]any{"action": "hijack", "qType": "1,28"}, map[string]any{"action": "direct"}}}},
		},
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "inboundTag": []string{tag}, "ip": []string{"10.78.0.2/32"}, "port": "53", "network": "tcp,udp", "outboundTag": "dark-pro-inbound-dns"},
			map[string]any{"type": "field", "inboundTag": []string{barrier.ControlTag}, "outboundTag": "private-api"},
		}},
	}
}

func assertResolverReleased(t *testing.T, iface string) {
	t.Helper()
	var dns, domains []byte
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		_, linkErr := net.InterfaceByName(iface)
		var dnsErr, domainErr error
		dns, dnsErr = isolatedResolverCommand(t, "dns")
		domains, domainErr = isolatedResolverCommand(t, "domain")
		if linkErr != nil && dnsErr == nil && domainErr == nil && !bytes.Contains(dns, []byte("10.78.0.2")) && !bytes.Contains(dns, []byte("fd78:44::2")) && !bytes.Contains(dns, []byte(iface)) && !bytes.Contains(domains, []byte("~.")) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("owned TUN or resolved per-link state survived cleanup: DNS=%s domains=%s", dns, domains)
}

func preparedTUNDNS(t *testing.T, bin, iface, tag string) (*coreProcess, uint16) {
	t.Helper()
	api := freePort(t)
	p := startCore(t, bin, tunDNSConfiguration(iface, tag, api), true)
	waitCore(t, bin, api, p)
	if _, err := net.InterfaceByName(iface); err != nil {
		t.Fatal("ready TUN device missing", err)
	}
	dns, err := isolatedResolverCommand(t, "dns", iface)
	if err != nil || bytes.Contains(dns, []byte("10.78.0.2")) {
		t.Fatal("host DNS changed before durable zero/activation", string(dns), err)
	}
	domains, err := isolatedResolverCommand(t, "domain", iface)
	if err != nil || bytes.Contains(domains, []byte("~.")) {
		t.Fatal("host DNS routing changed before activation", string(domains), err)
	}
	zero, err := query(bin, api, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range zero {
		if value != 0 {
			t.Fatal("prepared TUN DNS did not start at zero", zero)
		}
	}
	return p, api
}

func activateTUNDNS(t *testing.T, bin, iface string, api uint16) {
	t.Helper()
	ack, err := query(bin, api, barrier.Protocol+"activate/"+strings.Repeat("a", 64), false)
	if err != nil || ack[barrier.Protocol+"activated/"+strings.Repeat("a", 64)] != 1 {
		t.Fatal("TUN DNS activation", ack, err)
	}
	for _, check := range []struct{ action, want string }{{"dns", "10.78.0.2"}, {"domain", "~."}, {"default-route", "yes"}} {
		out, err := isolatedResolverCommand(t, check.action, iface)
		if err != nil || !bytes.Contains(out, []byte(check.want)) {
			t.Fatal("resolver activation not acknowledged", check.action, string(out), err)
		}
	}
}

func TestPinnedNativeTUNDNSNamespaceLifecycle(t *testing.T) {
	assertIsolatedDNSNamespace(t)
	bin := nativeBinary(t)
	_ = dnsTarget(t, "198.18.0.53", 53)
	t.Run("activate_final", func(t *testing.T) {
		const iface, tag = "dxp-dns-test", "customer-tun-dns"
		_, api := preparedTUNDNS(t, bin, iface, tag)
		activateTUNDNS(t, bin, iface, api)
		for _, network := range []string{"udp4", "tcp4"} {
			for _, kind := range []uint16{dnsTypeA, dnsTypeAAAA, dnsTypeTXT} {
				answer, err := dnsWireQuery(network, "10.78.0.2:53", "direct-tun.darkpro.test.", kind, 3*time.Second)
				if err != nil || len(answer.Answers) != 1 || !bytes.Equal(answer.Answers[0].data, dnsAnswerData(kind)) {
					t.Fatal("real TUN DNS payload", network, kind, answer, err)
				}
			}
		}
		// This is a real lookup through the private resolved service and its
		// D-Bus API, then the kernel TUN, core DNS outbound and owned upstream.
		out, err := isolatedResolverCommand(t, "--cache=no", "--type=A", "query", "via-resolved.darkpro.test")
		if err != nil || !bytes.Contains(out, []byte("203.0.113.41")) {
			t.Fatal("actual private resolved lookup", string(out), err)
		}
		stub, err := dnsWireQuery("udp4", "127.0.0.53:53", "via-stub.darkpro.test.", dnsTypeTXT, 3*time.Second)
		if err != nil || len(stub.Answers) != 1 || !bytes.Equal(stub.Answers[0].data, dnsAnswerData(dnsTypeTXT)) {
			t.Fatal("private system DNS stub through TUN", stub, err)
		}
		finalNonce := strings.Repeat("b", 64)
		final, err := query(bin, api, barrier.Protocol+"final/"+finalNonce, false)
		if err != nil || final[barrier.Protocol+"final/"+finalNonce] != 1 {
			t.Fatal("TUN DNS final cleanup did not complete", final, err)
		}
		assertResolverReleased(t, iface)
		ordinary, err := query(bin, api, "", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, direction := range []string{"uplink", "downlink"} {
			name := "inbound>>>" + tag + ">>>traffic>>>" + direction
			if final[name] == 0 || ordinary[name] != final[name] {
				t.Fatal("TUN DNS final owner accounting", final, ordinary)
			}
		}
		for name, value := range ordinary {
			if value != 0 && !strings.HasPrefix(name, "inbound>>>"+tag+">>>traffic>>>") {
				t.Fatal("unexpected DNS financial identity", ordinary)
			}
		}
	})
	t.Run("crash_cleanup", func(t *testing.T) {
		const iface = "dxp-dns-crash"
		p, api := preparedTUNDNS(t, bin, iface, "customer-tun-dns-crash")
		activateTUNDNS(t, bin, iface, api)
		out, err := isolatedResolverCommand(t, "--cache=no", "--type=A", "query", "before-crash.darkpro.test")
		if err != nil || !bytes.Contains(out, []byte("203.0.113.41")) {
			t.Fatal("resolver did not route through crash fixture", string(out), err)
		}
		if err = p.cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			t.Fatal("owned core did not exit after SIGKILL")
		}
		assertResolverReleased(t, iface)
		// SIGKILL gives no accounting final receipt. This only proves the
		// kernel-owned interface removes resolver state on lease/crash kill.
	})
	t.Run("ipv6_gateway", func(t *testing.T) {
		const iface, tag = "dxp-dns-v6", "customer-tun-dns-v6"
		api := freePort(t)
		config := tunDNSConfiguration(iface, tag, api)
		config["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["gateway"] = []string{"fd78:44::1/126"}
		config["routing"].(map[string]any)["rules"].([]any)[0].(map[string]any)["ip"] = []string{"fd78:44::2/128"}
		p := startCore(t, bin, config, true)
		waitCore(t, bin, api, p)
		out, err := isolatedResolverCommand(t, "dns")
		if err != nil || bytes.Contains(out, []byte("fd78:44::2")) {
			t.Fatal("IPv6 resolver changed before activation", string(out), err)
		}
		zero, err := query(bin, api, "", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range zero {
			if value != 0 {
				t.Fatal("IPv6 preparation counters", zero)
			}
		}
		if _, err = query(bin, api, barrier.Protocol+"activate/"+strings.Repeat("a", 64), false); err != nil {
			t.Fatal(err)
		}
		out, err = isolatedResolverCommand(t, "dns", iface)
		if err != nil || !bytes.Contains(out, []byte("fd78:44::2")) {
			t.Fatal("IPv6 gateway-next resolver missing", string(out), err)
		}
		for _, network := range []string{"udp6", "tcp6"} {
			answer, err := dnsWireQuery(network, "[fd78:44::2]:53", "ipv6-tun.darkpro.test.", dnsTypeAAAA, 4*time.Second)
			if err != nil || len(answer.Answers) != 1 || !bytes.Equal(answer.Answers[0].data, dnsAnswerData(dnsTypeAAAA)) {
				t.Fatal("actual IPv6 TUN DNS", network, answer, err)
			}
		}
		out, err = isolatedResolverCommand(t, "--cache=no", "--type=A", "query", "via-ipv6-resolved.darkpro.test")
		if err != nil || !bytes.Contains(out, []byte("203.0.113.41")) {
			t.Fatal("private resolver over IPv6 TUN", string(out), err)
		}
		if _, err = query(bin, api, barrier.Protocol+"final/"+strings.Repeat("b", 64), false); err != nil {
			t.Fatal(err)
		}
		assertResolverReleased(t, iface)
	})
	t.Run("exclusive_device", func(t *testing.T) {
		const iface = "dxp-dns-owned"
		if out, err := exec.Command("ip", "tuntap", "add", "dev", iface, "mode", "tun").CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
		t.Cleanup(func() { _ = exec.Command("ip", "link", "delete", iface).Run() })
		if out, err := exec.Command("ip", "link", "set", "dev", iface, "mtu", "1560").CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
		p := startCore(t, bin, tunDNSConfiguration(iface, "customer-tun-dns-existing", freePort(t)), true)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			t.Fatal("managed worker attached to an existing TUN")
		}
		link, err := net.InterfaceByName(iface)
		if err != nil || link.MTU != 1560 {
			t.Fatal("rejected worker modified an existing interface", link, err)
		}
		out, err := isolatedResolverCommand(t, "dns")
		if err != nil || bytes.Contains(out, []byte("10.78.0.2")) {
			t.Fatal("existing interface rejection changed resolver", string(out), err)
		}
	})
	t.Log("KERNEL_TUN_PRIVATE_RESOLVED_ACTIVATION_FINAL_CRASH_PASS")
}
