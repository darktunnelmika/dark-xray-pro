package nodeagent

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// AmneziaWG server inside the agent. It runs entirely in user space: the
// tunnel ends in a gVisor network stack whose TCP and UDP flows are opened
// again from the node as ordinary sockets — no kernel module, no TUN device
// and no NAT rules, so it works on any VPS (like Xray's own WireGuard).
// Every client is a peer named by its email: the node counts its bytes
// and drops it when the HUB's limits say so (peers are filtered like
// clients).

type awgPeer struct {
	PublicKey  string   `json:"publicKey"`
	AllowedIPs []string `json:"allowedIPs"`
	Email      string   `json:"email"`
}

type awgSettings struct {
	SecretKey string    `json:"secretKey"`
	MTU       int       `json:"mtu"`
	Jc        int       `json:"jc"`
	Jmin      int       `json:"jmin"`
	Jmax      int       `json:"jmax"`
	S1        int       `json:"s1"`
	S2        int       `json:"s2"`
	H1        uint32    `json:"h1"`
	H2        uint32    `json:"h2"`
	H3        uint32    `json:"h3"`
	H4        uint32    `json:"h4"`
	Peers     []awgPeer `json:"peers"`
}

type awgInbound struct {
	Tag      string      `json:"tag"`
	Listen   string      `json:"listen"`
	Port     int         `json:"port"`
	Settings awgSettings `json:"settings"`
}

func b64ToHex(k string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k))
	if err != nil || len(raw) != 32 {
		return "", errors.New("bad key")
	}
	return hex.EncodeToString(raw), nil
}

// awgBase is the interface part of the UAPI config (no peers).
func awgBase(in awgInbound) (string, error) {
	priv, err := b64ToHex(in.Settings.SecretKey)
	if err != nil {
		return "", fmt.Errorf("amneziawg %s: server key: %w", in.Tag, err)
	}
	s := in.Settings
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\nlisten_port=%d\n", priv, in.Port)
	for _, kv := range []struct {
		k string
		v uint64
	}{{"jc", uint64(s.Jc)}, {"jmin", uint64(s.Jmin)}, {"jmax", uint64(s.Jmax)}, {"s1", uint64(s.S1)}, {"s2", uint64(s.S2)},
		{"h1", uint64(s.H1)}, {"h2", uint64(s.H2)}, {"h3", uint64(s.H3)}, {"h4", uint64(s.H4)}} {
		if kv.v > 0 {
			fmt.Fprintf(&b, "%s=%d\n", kv.k, kv.v)
		}
	}
	return b.String(), nil
}

func awgPeerConf(p awgPeer) (string, error) {
	pub, err := b64ToHex(p.PublicKey)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "public_key=%s\nreplace_allowed_ips=true\n", pub)
	for _, ip := range p.AllowedIPs {
		fmt.Fprintf(&b, "allowed_ip=%s\n", ip)
	}
	return b.String(), nil
}

type awgServer struct {
	tag   string
	base  string // interface config the device was started with
	dev   *device.Device
	tun   *awgTun
	peers map[string]awgPeer // public key (base64) -> peer
	last  map[string][2]uint64
	logf  func(string, ...any)
}

func startAWG(in awgInbound, logf func(string, ...any)) (*awgServer, error) {
	base, err := awgBase(in)
	if err != nil {
		return nil, err
	}
	mtu := in.Settings.MTU
	if mtu <= 0 {
		mtu = 1280
	}
	t, err := newAWGTun(mtu, logf)
	if err != nil {
		return nil, err
	}
	logger := &device.Logger{Verbosef: device.DiscardLogf, Errorf: func(f string, a ...any) { logf("amneziawg %s: "+f, append([]any{in.Tag}, a...)...) }}
	dev := device.NewDevice(t, conn.NewDefaultBind(), logger)
	if err := dev.IpcSet(base); err != nil {
		dev.Close()
		return nil, fmt.Errorf("amneziawg %s: %w", in.Tag, err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("amneziawg %s: %w", in.Tag, err)
	}
	s := &awgServer{tag: in.Tag, base: base, dev: dev, tun: t, peers: map[string]awgPeer{}, last: map[string][2]uint64{}, logf: logf}
	s.setPeers(in.Settings.Peers)
	return s, nil
}

// setPeers adds, changes and removes peers without touching the others.
func (s *awgServer) setPeers(list []awgPeer) {
	want := map[string]awgPeer{}
	for _, p := range list {
		if p.Email != "" && p.PublicKey != "" {
			want[p.PublicKey] = p
		}
	}
	var b strings.Builder
	for k := range s.peers {
		if _, ok := want[k]; !ok {
			if h, err := b64ToHex(k); err == nil {
				fmt.Fprintf(&b, "public_key=%s\nremove=true\n", h)
			}
		}
	}
	for k, p := range want {
		old, ok := s.peers[k]
		if ok && strings.Join(old.AllowedIPs, ",") == strings.Join(p.AllowedIPs, ",") && old.Email == p.Email {
			continue
		}
		if c, err := awgPeerConf(p); err == nil {
			b.WriteString(c)
		}
	}
	if b.Len() > 0 {
		if err := s.dev.IpcSet(b.String()); err != nil {
			s.logf("amneziawg %s: peers: %v", s.tag, err)
		}
	}
	s.peers = want
}

func (s *awgServer) close() {
	s.dev.Close()
}

// stats reads per-peer byte counters and the last handshake/endpoint.
type awgPeerStat struct {
	rx, tx    uint64
	handshake int64
	endpoint  string
}

func (s *awgServer) stats() map[string]awgPeerStat {
	raw, err := s.dev.IpcGet()
	if err != nil {
		return nil
	}
	out := map[string]awgPeerStat{}
	var cur string
	var st awgPeerStat
	flush := func() {
		if cur != "" {
			out[cur] = st
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "public_key":
			flush()
			cur, st = "", awgPeerStat{}
			if b, err := hex.DecodeString(v); err == nil {
				cur = base64.StdEncoding.EncodeToString(b)
			}
		case "rx_bytes":
			st.rx, _ = strconv.ParseUint(v, 10, 64)
		case "tx_bytes":
			st.tx, _ = strconv.ParseUint(v, 10, 64)
		case "last_handshake_time_sec":
			st.handshake, _ = strconv.ParseInt(v, 10, 64)
		case "endpoint":
			st.endpoint = v
		}
	}
	flush()
	return out
}

// take returns traffic since the last call per email (server rx = client
// upload) and the inbound's total.
func (s *awgServer) take() (users []userBytes, total uint64) {
	for k, st := range s.stats() {
		p, ok := s.peers[k]
		if !ok {
			continue
		}
		prev := s.last[k]
		up, down := st.rx-min(prev[0], st.rx), st.tx-min(prev[1], st.tx)
		s.last[k] = [2]uint64{st.rx, st.tx}
		if up+down > 0 {
			users = append(users, userBytes{email: p.Email, up: up, down: down})
			total += up + down
		}
	}
	return users, total
}

type userBytes struct {
	email    string
	up, down uint64
}

// online lists peers with a handshake in the last three minutes and their
// source address (for IP limits).
func (s *awgServer) online(now time.Time) map[string][]string {
	out := map[string][]string{}
	for k, st := range s.stats() {
		p, ok := s.peers[k]
		if !ok || st.handshake == 0 || now.Unix()-st.handshake > 180 {
			continue
		}
		ip := st.endpoint
		if h, _, err := net.SplitHostPort(ip); err == nil {
			ip = h
		}
		out[p.Email] = append(out[p.Email], ip)
	}
	return out
}

// ---------- user-space network stack ----------

type awgTun struct {
	ep       *channel.Endpoint
	stack    *stack.Stack
	events   chan tun.Event
	incoming chan *buffer.View
	mtu      int
	closed   chan struct{}
	once     sync.Once
}

func newAWGTun(mtu int, logf func(string, ...any)) (*awgTun, error) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})
	t := &awgTun{ep: channel.New(1024, uint32(mtu), ""), stack: s, events: make(chan tun.Event, 4), incoming: make(chan *buffer.View, 256), mtu: mtu, closed: make(chan struct{})}
	sack := tcpip.TCPSACKEnabled(true)
	_ = s.SetTransportProtocolOption(tcp.ProtocolNumber, &sack)
	t.ep.AddNotify(t)
	if err := s.CreateNIC(1, t.ep); err != nil {
		return nil, fmt.Errorf("netstack: %v", err)
	}
	// Accept packets for every destination: this stack is the exit.
	_ = s.SetPromiscuousMode(1, true)
	_ = s.SetSpoofing(1, true)
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}, {Destination: header.IPv6EmptySubnet, NIC: 1}})
	tf := tcp.NewForwarder(s, 0, 4096, func(r *tcp.ForwarderRequest) {
		id := r.ID()
		dst := netip.AddrFrom16(id.LocalAddress.As16()).Unmap()
		if id.LocalAddress.Len() == 4 {
			dst = netip.AddrFrom4(id.LocalAddress.As4())
		}
		if blockedDest(dst) {
			r.Complete(true)
			return
		}
		out, err := net.DialTimeout("tcp", netip.AddrPortFrom(dst, id.LocalPort).String(), 10*time.Second)
		if err != nil {
			r.Complete(true)
			return
		}
		var wq waiter.Queue
		ep, terr := r.CreateEndpoint(&wq)
		if terr != nil {
			r.Complete(true)
			out.Close()
			return
		}
		r.Complete(false)
		go pipe(gonet.NewTCPConn(&wq, ep), out)
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tf.HandlePacket)
	uf := udp.NewForwarder(s, func(r *udp.ForwarderRequest) {
		id := r.ID()
		dst := netip.AddrFrom16(id.LocalAddress.As16()).Unmap()
		if id.LocalAddress.Len() == 4 {
			dst = netip.AddrFrom4(id.LocalAddress.As4())
		}
		if blockedDest(dst) {
			return
		}
		var wq waiter.Queue
		ep, terr := r.CreateEndpoint(&wq)
		if terr != nil {
			return
		}
		in := gonet.NewUDPConn(&wq, ep)
		out, err := net.Dial("udp", netip.AddrPortFrom(dst, id.LocalPort).String())
		if err != nil {
			in.Close()
			return
		}
		go pipeUDP(in, out)
	})
	s.SetTransportProtocolHandler(udp.ProtocolNumber, uf.HandlePacket)
	t.events <- tun.EventUp
	return t, nil
}

// blockedDest keeps tunnel users away from the node itself and private
// networks (the same as the panel's "block private networks" default).
var allowLocalDest = false // tests only

func blockedDest(a netip.Addr) bool {
	if allowLocalDest && a.IsLoopback() {
		return false
	}
	return !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsMulticast() || a.IsUnspecified() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(a)
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
	a.Close()
	b.Close()
}

func pipeUDP(a, b net.Conn) {
	idle := 2 * time.Minute
	stop := make(chan struct{})
	var once sync.Once
	end := func() { once.Do(func() { close(stop); a.Close(); b.Close() }) }
	cp := func(dst, src net.Conn) {
		buf := make([]byte, 65535)
		for {
			_ = src.SetReadDeadline(time.Now().Add(idle))
			n, err := src.Read(buf)
			if err != nil {
				end()
				return
			}
			if _, err := dst.Write(buf[:n]); err != nil {
				end()
				return
			}
		}
	}
	go cp(a, b)
	go cp(b, a)
	<-stop
}

func (t *awgTun) Name() (string, error)    { return "dark-awg", nil }
func (t *awgTun) File() *os.File           { return nil }
func (t *awgTun) Events() <-chan tun.Event { return t.events }
func (t *awgTun) MTU() (int, error)        { return t.mtu, nil }
func (t *awgTun) BatchSize() int           { return 1 }

func (t *awgTun) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	select {
	case v, ok := <-t.incoming:
		if !ok {
			return 0, os.ErrClosed
		}
		n, err := v.Read(bufs[0][offset:])
		v.Release()
		if err != nil {
			return 0, err
		}
		sizes[0] = n
		return 1, nil
	case <-t.closed:
		return 0, os.ErrClosed
	}
}

func (t *awgTun) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		pkt := b[offset:]
		if len(pkt) == 0 {
			continue
		}
		pb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(pkt)})
		switch pkt[0] >> 4 {
		case 4:
			t.ep.InjectInbound(header.IPv4ProtocolNumber, pb)
		case 6:
			t.ep.InjectInbound(header.IPv6ProtocolNumber, pb)
		default:
			pb.DecRef()
			return 0, syscall.EAFNOSUPPORT
		}
		pb.DecRef()
	}
	return len(bufs), nil
}

// WriteNotify is called by the channel endpoint when the stack sends a
// packet back toward the clients.
func (t *awgTun) WriteNotify() {
	pkt := t.ep.Read()
	if pkt.IsNil() {
		return
	}
	v := pkt.ToView()
	pkt.DecRef()
	select {
	case t.incoming <- v:
	case <-t.closed:
		v.Release()
	}
}

func (t *awgTun) Close() error {
	t.once.Do(func() {
		close(t.closed)
		t.stack.RemoveNIC(1)
		t.ep.Close()
		close(t.events)
	})
	return nil
}

// ---------- side servers next to Xray ----------

// SideServers runs the inbounds Xray cannot: AmneziaWG and MTProto. The
// HUB renders them in the same config; the agent takes them out before
// Xray sees it.
type SideServers struct {
	mu     sync.Mutex
	awg    map[string]*awgServer
	awgKey map[string]string
	mtp    map[string]*mtpServer
	mtpKey map[string]string
	logf   func(string, ...any)
}

func NewSideServers(logf func(string, ...any)) *SideServers {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &SideServers{awg: map[string]*awgServer{}, awgKey: map[string]string{}, mtp: map[string]*mtpServer{}, mtpKey: map[string]string{}, logf: logf}
}

var sideProtocols = map[string]bool{"amneziawg": true, "mtproto": true}

// SplitSide removes AmneziaWG and MTProto inbounds from an Xray config and
// returns them separately.
func SplitSide(raw []byte) (xray []byte, side []json.RawMessage, err error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, err
	}
	var ins []json.RawMessage
	if err := json.Unmarshal(cfg["inbounds"], &ins); err != nil {
		return raw, nil, nil
	}
	kept := ins[:0]
	for _, in := range ins {
		var p struct {
			Protocol string `json:"protocol"`
		}
		_ = json.Unmarshal(in, &p)
		if sideProtocols[p.Protocol] {
			side = append(side, in)
			continue
		}
		kept = append(kept, in)
	}
	if len(side) == 0 {
		return raw, nil, nil
	}
	b, _ := json.Marshal(kept)
	cfg["inbounds"] = b
	out, err := json.Marshal(cfg)
	return out, side, err
}

// Apply starts, changes and stops side servers to match the config.
func (m *SideServers) Apply(side []json.RawMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seenA, seenM := map[string]bool{}, map[string]bool{}
	for _, raw := range side {
		var p struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
		}
		_ = json.Unmarshal(raw, &p)
		switch p.Protocol {
		case "amneziawg":
			var in awgInbound
			if json.Unmarshal(raw, &in) != nil || in.Port <= 0 {
				continue
			}
			seenA[in.Tag] = true
			base, err := awgBase(in)
			if err != nil {
				m.logf("%v", err)
				continue
			}
			if s := m.awg[in.Tag]; s != nil && m.awgKey[in.Tag] == base {
				s.setPeers(in.Settings.Peers)
				continue
			}
			if s := m.awg[in.Tag]; s != nil {
				s.close()
				delete(m.awg, in.Tag)
			}
			s, err := startAWG(in, m.logf)
			if err != nil {
				m.logf("amneziawg %s not started: %v", in.Tag, err)
				continue
			}
			m.awg[in.Tag], m.awgKey[in.Tag] = s, base
		case "mtproto":
			var in mtpInbound
			if json.Unmarshal(raw, &in) != nil || in.Port <= 0 {
				continue
			}
			seenM[in.Tag] = true
			key := fmt.Sprintf("%s|%d|%s", in.Listen, in.Port, in.Settings.Secret)
			if m.mtp[in.Tag] != nil && m.mtpKey[in.Tag] == key {
				continue
			}
			if s := m.mtp[in.Tag]; s != nil {
				s.close()
				delete(m.mtp, in.Tag)
			}
			s, err := startMTP(in, m.logf)
			if err != nil {
				m.logf("mtproto %s not started: %v", in.Tag, err)
				continue
			}
			m.mtp[in.Tag], m.mtpKey[in.Tag] = s, key
		}
	}
	for tag, s := range m.awg {
		if !seenA[tag] {
			s.close()
			delete(m.awg, tag)
			delete(m.awgKey, tag)
		}
	}
	for tag, s := range m.mtp {
		if !seenM[tag] {
			s.close()
			delete(m.mtp, tag)
			delete(m.mtpKey, tag)
		}
	}
}

// Take returns user traffic (AmneziaWG peers) and per-inbound traffic.
func (m *SideServers) Take() (users map[string][2]int64, inbounds map[string][2]int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	users, inbounds = map[string][2]int64{}, map[string][2]int64{}
	for tag, s := range m.awg {
		list, _ := s.take()
		for _, u := range list {
			x := users[u.email]
			users[u.email] = [2]int64{x[0] + int64(u.up), x[1] + int64(u.down)}
			y := inbounds[tag]
			inbounds[tag] = [2]int64{y[0] + int64(u.up), y[1] + int64(u.down)}
		}
	}
	for tag, s := range m.mtp {
		up, down := s.take()
		if up+down > 0 {
			inbounds[tag] = [2]int64{int64(up), int64(down)}
		}
	}
	return users, inbounds
}

// Online lists AmneziaWG peers connected now with their source address.
func (m *SideServers) Online(now time.Time) map[string][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string][]string{}
	for _, s := range m.awg {
		for e, ips := range s.online(now) {
			out[e] = append(out[e], ips...)
		}
	}
	for e := range out {
		sort.Strings(out[e])
	}
	return out
}

// Close stops every side server.
func (m *SideServers) Close() { m.Apply(nil) }
