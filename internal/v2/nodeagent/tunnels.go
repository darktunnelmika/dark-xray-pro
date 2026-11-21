package nodeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// TunnelMonitor finds the tunnels that end on this node and pings the far
// end of each through the tunnel every 30 seconds. It only reads: tunnel
// configuration files and interface addresses; it never changes a tunnel.
// Found: dark-gre tunnels (/etc/dark-gre/tunnels/*/meta.conf), any other
// interface holding a private /30 or /31 (the usual point-to-point link),
// and Max Dark Tunnel tunnels that have no interface (from their status file).
type TunnelMonitor struct {
	Logf     func(string, ...any)
	Interval time.Duration
	GREDir   string
	MDTRun   string // Max Dark Tunnel status files (/run/max-dark-tunnel)
	MDTConf  string // Max Dark Tunnel tunnel files (/etc/max-dark-tunnel/tunnels)
	SysNet   string // /sys/class/net
	Ping     func(ctx context.Context, iface, peer string) (rttMS, loss float64, err error)

	mu    sync.Mutex
	state map[string]*tunState
	mdt   map[string]mdtStatus // by tunnel name, as of the last discover
}

// mdtStatus is what Max Dark Tunnel writes about one tunnel every few
// seconds. Its TCP and SSH tunnels have no interface to ping, so this file
// is how such a tunnel is seen at all.
type mdtStatus struct {
	State     string  `json:"state"`
	Peer      string  `json:"peer"`
	Transport string  `json:"transport"`
	Loss      float64 `json:"loss"` // percent, -1 = not measured
	Updated   int64   `json:"updated"`
}

const mdtPrefix = "mdt:"

type tunState struct {
	t     wire.Tunnel
	fails int
}

// NewTunnelMonitor returns a monitor using the system ping.
func NewTunnelMonitor(logf func(string, ...any)) *TunnelMonitor {
	return &TunnelMonitor{Logf: logf, Interval: 30 * time.Second, GREDir: "/etc/dark-gre/tunnels", MDTRun: "/run/max-dark-tunnel", MDTConf: "/etc/max-dark-tunnel/tunnels", Ping: systemPing, state: map[string]*tunState{}}
}

// Run probes until ctx ends.
func (m *TunnelMonitor) Run(ctx context.Context) {
	for {
		m.Probe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.Interval):
		}
	}
}

// Snapshot is the latest result of every tunnel, sorted by name.
func (m *TunnelMonitor) Snapshot() []wire.Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]wire.Tunnel, 0, len(m.state))
	for _, s := range m.state {
		out = append(out, s.t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Probe finds the tunnels and pings each once; a tunnel counts as down
// after two probes in a row with no answer.
func (m *TunnelMonitor) Probe(ctx context.Context) {
	found := m.discover()
	mdtNow := m.mdt
	now := time.Now().Unix()
	seen := map[string]bool{}
	var wg sync.WaitGroup
	type res struct {
		key       string
		rtt, loss float64
		err       error
	}
	results := make(chan res, len(found))
	for _, t := range found {
		key := t.Iface + "|" + t.Peer
		seen[key] = true
		wg.Add(1)
		go func(t wire.Tunnel, key string) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			if name, ok := strings.CutPrefix(t.Iface, mdtPrefix); ok {
				// No interface: the tunnel's own word on whether it is connected.
				st := mdtNow[name]
				if st.State != "connected" {
					results <- res{key, 0, 100, errors.New("not connected")}
					return
				}
				results <- res{key, 0, max(st.Loss, 0), nil}
				return
			}
			rtt, loss, err := m.Ping(pctx, t.Iface, t.Peer)
			results <- res{key, rtt, loss, err}
		}(t, key)
	}
	wg.Wait()
	close(results)
	byKey := map[string]wire.Tunnel{}
	for _, t := range found {
		byKey[t.Iface+"|"+t.Peer] = t
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.state {
		if !seen[k] {
			delete(m.state, k)
		}
	}
	for r := range results {
		t := byKey[r.key]
		s := m.state[r.key]
		fresh := s == nil
		if fresh {
			s = &tunState{t: t}
			s.t.Since = now
			m.state[r.key] = s
		}
		s.t.Name, s.t.Remote = t.Name, t.Remote
		s.t.RTTms, s.t.Loss = r.rtt, r.loss
		if r.err != nil || r.loss >= 100 {
			s.fails++
			s.t.RTTms, s.t.Loss = 0, 100
		} else {
			s.fails = 0
		}
		up := s.fails < 2
		if fresh {
			// The first probe decides: no "up, 0ms" for a tunnel that is down.
			s.t.Up = s.fails == 0
			continue
		}
		if up != s.t.Up {
			s.t.Up, s.t.Since = up, now
			if m.Logf != nil {
				m.Logf("tunnel %s (%s → %s) up=%v", s.t.Name, s.t.Iface, s.t.Peer, up)
			}
		}
	}
}

// discover lists the tunnels: dark-gre first (named), then other
// point-to-point interfaces.
func (m *TunnelMonitor) discover() []wire.Tunnel {
	var out []wire.Tunnel
	have := map[string]bool{}
	metas, _ := filepath.Glob(filepath.Join(m.GREDir, "*", "meta.conf"))
	sort.Strings(metas)
	for _, f := range metas {
		kv := readKV(f)
		if kv["IFNAME"] == "" || net.ParseIP(kv["REMOTE_TUN"]) == nil {
			continue
		}
		name := kv["NAME"]
		if name == "" {
			name = kv["IFNAME"]
		}
		out = append(out, wire.Tunnel{Name: name, Iface: kv["IFNAME"], Peer: kv["REMOTE_TUN"], Remote: kv["REMOTE_PUBLIC"]})
		have[kv["IFNAME"]] = true
	}
	// Max Dark Tunnel: a tunnel with an interface (tun mode) is pinged like
	// any other and only takes its name from here; one without (tcp, ssh)
	// is listed from its status file.
	mdtIface := map[string]string{}
	m.mdt = map[string]mdtStatus{}
	runs, _ := filepath.Glob(filepath.Join(m.MDTRun, "*.json"))
	sort.Strings(runs)
	for _, f := range runs {
		var st mdtStatus
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &st) != nil || time.Now().Unix()-st.Updated > 30 {
			continue // unreadable, or left behind by a tunnel that no longer runs
		}
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		if strings.HasPrefix(st.Transport, "tun") {
			var c struct {
				TunDev string `json:"tun_dev"`
				Subnet int    `json:"subnet"`
			}
			if b, err := os.ReadFile(filepath.Join(m.MDTConf, name+".json")); err == nil {
				_ = json.Unmarshal(b, &c)
			}
			if c.TunDev == "" {
				c.TunDev = "mdt" + strconv.Itoa(c.Subnet)
			}
			mdtIface[c.TunDev] = name
			continue
		}
		m.mdt[name] = st
		out = append(out, wire.Tunnel{Name: name, Iface: mdtPrefix + name, Peer: st.Peer, Remote: st.Peer})
	}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if have[ifc.Name] || ifc.Flags&net.FlagLoopback != 0 || skipIface.MatchString(ifc.Name) {
			continue
		}
		addrs, _ := ifc.Addrs()
		_, err := os.Stat(filepath.Join(m.sysNet(), ifc.Name, "tun_flags"))
		isTun := err == nil
		for _, a := range addrs {
			peer := p2pPeer(a)
			if peer == "" && isTun {
				peer = tunPeer(a) // Dark Tunnel style TUN: a private /24, far end .1
			}
			if peer != "" {
				name := ifc.Name
				if n := mdtIface[ifc.Name]; n != "" {
					name = n
				}
				out = append(out, wire.Tunnel{Name: name, Iface: ifc.Name, Peer: peer})
				break
			}
		}
	}
	return out
}

func (m *TunnelMonitor) sysNet() string {
	if m.SysNet != "" {
		return m.SysNet
	}
	return "/sys/class/net"
}

// tunPeer: on a TUN interface with a private IPv4 subnet, the far end is
// the subnet's first host (.1), or the second when this side is .1.
func tunPeer(a net.Addr) string {
	n, ok := a.(*net.IPNet)
	if !ok {
		return ""
	}
	ip := n.IP.To4()
	ones, bits := n.Mask.Size()
	if ip == nil || bits != 32 || ones < 16 || ones > 30 || !ip.IsPrivate() && !cgnat.Contains(ip) {
		return ""
	}
	first := append(net.IP{}, ip.Mask(n.Mask)...)
	first[3]++
	if ip.Equal(first) {
		first[3]++
	}
	return first.String()
}

var skipIface = regexp.MustCompile(`^(docker|br-|veth|virbr|cni|flannel|kube|lxc|tailscale|zt)`)

// p2pPeer: for a private IPv4 /30 or /31, the other host on the link.
func p2pPeer(a net.Addr) string {
	n, ok := a.(*net.IPNet)
	if !ok {
		return ""
	}
	ip := n.IP.To4()
	ones, bits := n.Mask.Size()
	if ip == nil || bits != 32 || (ones != 30 && ones != 31) || !ip.IsPrivate() && !cgnat.Contains(ip) {
		return ""
	}
	base := ip.Mask(n.Mask)
	var peer net.IP
	if ones == 31 {
		peer = append(net.IP{}, base...)
		if ip.Equal(base) {
			peer[3]++
		}
	} else {
		first, second := append(net.IP{}, base...), append(net.IP{}, base...)
		first[3]++
		second[3] += 2
		peer = first
		if ip.Equal(first) {
			peer = second
		}
	}
	return peer.String()
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0).To4(), Mask: net.CIDRMask(10, 32)}

func readKV(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

var (
	lossRE = regexp.MustCompile(`([\d.]+)% packet loss`)
	rttRE  = regexp.MustCompile(`= [\d.]+/([\d.]+)/`)
)

func systemPing(ctx context.Context, iface, peer string) (float64, float64, error) {
	out, err := exec.CommandContext(ctx, "ping", "-n", "-q", "-c", "3", "-i", "0.3", "-W", "2", "-I", iface, peer).CombinedOutput()
	rtt, loss, perr := parsePing(string(out))
	if perr == nil {
		return rtt, loss, nil
	}
	if err != nil {
		return 0, 100, err
	}
	return 0, 100, perr
}

// parsePing reads loss and average round trip from ping's summary.
func parsePing(out string) (rttMS, loss float64, err error) {
	m := lossRE.FindStringSubmatch(out)
	if m == nil {
		return 0, 100, os.ErrInvalid
	}
	loss, _ = strconv.ParseFloat(m[1], 64)
	if r := rttRE.FindStringSubmatch(out); r != nil {
		rttMS, _ = strconv.ParseFloat(r[1], 64)
	}
	return rttMS, loss, nil
}
