package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestParsePing(t *testing.T) {
	rtt, loss, err := parsePing("3 packets transmitted, 3 received, 0% packet loss, time 602ms\nrtt min/avg/max/mdev = 41.2/42.75/44.1/1.2 ms\n")
	if err != nil || rtt != 42.75 || loss != 0 {
		t.Fatal(rtt, loss, err)
	}
	if _, loss, err := parsePing("3 packets transmitted, 0 received, 100% packet loss, time 2050ms\n"); err != nil || loss != 100 {
		t.Fatal(loss, err)
	}
	if _, _, err := parsePing("ping: SO_BINDTODEVICE: No such device"); err == nil {
		t.Fatal("no summary = error")
	}
}

func TestP2PPeer(t *testing.T) {
	cases := map[string]string{"10.77.10.2/30": "10.77.10.1", "10.77.10.1/30": "10.77.10.2", "172.20.0.0/31": "172.20.0.1", "172.20.0.1/31": "172.20.0.0",
		"100.64.3.6/30": "100.64.3.5", "172.16.0.2/32": "", "10.0.0.5/24": "", "82.47.63.165/30": ""}
	for in, want := range cases {
		ip, n, _ := net.ParseCIDR(in)
		n.IP = ip
		if got := p2pPeer(n); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}

func TestTunPeer(t *testing.T) {
	cases := map[string]string{"10.10.66.2/24": "10.10.66.1", "10.10.66.1/24": "10.10.66.2", "172.16.0.2/32": "", "8.8.8.2/24": ""}
	for in, want := range cases {
		ip, n, _ := net.ParseCIDR(in)
		n.IP = ip
		if got := tunPeer(n); got != want {
			t.Errorf("%s: %q want %q", in, got, want)
		}
	}
}

func TestTunnelMonitorDownAfterTwoMisses(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "poland"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "poland", "meta.conf"), []byte("NAME=poland\nIFNAME=dgr1\nREMOTE_TUN=10.77.10.1\nREMOTE_PUBLIC=\"5.6.7.8\"\n"), 0o600)
	answer := true
	m := &TunnelMonitor{GREDir: dir, state: map[string]*tunState{}, Ping: func(ctx context.Context, iface, peer string) (float64, float64, error) {
		if iface != "dgr1" || peer != "10.77.10.1" {
			return 0, 0, nil // other interfaces of the test machine
		}
		if answer {
			return 40, 0, nil
		}
		return 0, 100, errors.New("timeout")
	}}
	get := func() (out struct {
		up   bool
		rtt  float64
		seen bool
	}) {
		for _, x := range m.Snapshot() {
			if x.Name == "poland" {
				out.up, out.rtt, out.seen = x.Up, x.RTTms, true
				if x.Remote != "5.6.7.8" || x.Iface != "dgr1" {
					t.Fatalf("meta: %+v", x)
				}
			}
		}
		return
	}
	m.Probe(context.Background())
	if g := get(); !g.seen || !g.up || g.rtt != 40 {
		t.Fatalf("first: %+v", g)
	}
	answer = false
	m.Probe(context.Background())
	if g := get(); !g.up {
		t.Fatal("one miss is not down")
	}
	m.Probe(context.Background())
	if g := get(); g.up {
		t.Fatal("two misses = down")
	}
	answer = true
	m.Probe(context.Background())
	if g := get(); !g.up || g.rtt != 40 {
		t.Fatalf("back: %+v", g)
	}
}

func TestTunnelFirstProbeDecides(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "x"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "x", "meta.conf"), []byte("NAME=x\nIFNAME=dgrx\nREMOTE_TUN=10.77.10.1\n"), 0o600)
	m := &TunnelMonitor{GREDir: dir, state: map[string]*tunState{}, Ping: func(ctx context.Context, iface, peer string) (float64, float64, error) {
		if iface == "dgrx" {
			return 0, 100, errors.New("timeout")
		}
		return 0, 0, nil
	}}
	m.Probe(context.Background())
	for _, x := range m.Snapshot() {
		if x.Name == "x" && x.Up {
			t.Fatalf("a tunnel down from the start shows up: %+v", x)
		}
	}
}

// A Max Dark Tunnel TCP tunnel has no interface: it is listed from its
// status file, up while that says connected, gone once the file goes stale.
func TestTunnelMonitorMDTStatus(t *testing.T) {
	run, conf := t.TempDir(), t.TempDir()
	write := func(name, state, transport string, age int64) {
		_ = os.WriteFile(filepath.Join(run, name+".json"), []byte(fmt.Sprintf(
			`{"loss":-1,"peer":"5.6.7.8","sessions":4,"state":%q,"transport":%q,"updated":%d}`, state, transport, time.Now().Unix()-age)), 0o600)
	}
	_ = os.WriteFile(filepath.Join(conf, "tu.json"), []byte(`{"name":"tu","psk":"x","transport":"tcp"}`), 0o600)
	m := &TunnelMonitor{GREDir: t.TempDir(), MDTRun: run, MDTConf: conf, state: map[string]*tunState{},
		Ping: func(ctx context.Context, iface, peer string) (float64, float64, error) {
			if strings.HasPrefix(iface, "mdt:") {
				t.Fatalf("a tunnel without an interface is never pinged: %s", iface)
			}
			return 1, 0, nil
		}}
	find := func(name string) (wire.Tunnel, bool) {
		for _, x := range m.Snapshot() {
			if x.Name == name {
				return x, true
			}
		}
		return wire.Tunnel{}, false
	}
	write("tu", "connected", "tcp", 2)
	write("old", "connecting", "tun/icmp", 900) // its service was stopped long ago
	m.Probe(context.Background())
	x, ok := find("tu")
	if !ok || !x.Up || x.Remote != "5.6.7.8" || x.Iface != "mdt:tu" || x.Loss != 0 {
		t.Fatalf("%+v %v", x, ok)
	}
	if _, ok := find("old"); ok {
		t.Fatal("a stale status file is not a tunnel")
	}
	write("tu", "connecting", "tcp", 1)
	m.Probe(context.Background())
	m.Probe(context.Background())
	if x, _ := find("tu"); x.Up {
		t.Fatal("not connected twice in a row = down")
	}
	write("tu", "connected", "tcp", 1)
	m.Probe(context.Background())
	if x, _ := find("tu"); !x.Up {
		t.Fatal("back up")
	}
	write("tu", "connected", "tcp", 600)
	m.Probe(context.Background())
	if _, ok := find("tu"); ok {
		t.Fatal("gone once its status stops being written")
	}
}
