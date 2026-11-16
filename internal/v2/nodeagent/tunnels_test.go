package nodeagent

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
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
