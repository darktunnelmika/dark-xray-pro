package nodeagent

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-quic/tuic"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
)

// TUIC v5 (QUIC) inside the agent, on sing-box's own engine (sing-quic).
// Xray has no TUIC, so it runs beside Xray like AmneziaWG. Every client is
// a TUIC user (uuid + password); the agent sees which user opened each
// stream, so traffic, online IPs and disabling work per client.

type tuicInbound struct {
	Tag      string `json:"tag"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	Settings struct {
		Clients           []tuicUser `json:"clients"`
		CongestionControl string     `json:"congestion_control"`
	} `json:"settings"`
	StreamSettings struct {
		TLSSettings struct {
			ServerName   string   `json:"serverName"`
			ALPN         []string `json:"alpn"`
			Certificates []struct {
				Certificate []string `json:"certificate"`
				Key         []string `json:"key"`
			} `json:"certificates"`
		} `json:"tlsSettings"`
	} `json:"streamSettings"`
}

type tuicUser struct {
	Email    string `json:"email"`
	UUID     string `json:"uuid"`
	Password string `json:"password"`
}

type tuicCount struct{ up, down atomic.Uint64 }

type tuicServer struct {
	svc  *tuic.Service[string]
	conn net.PacketConn

	mu      sync.Mutex
	allowed map[string]bool             // current users
	counts  map[string]*tuicCount       // per user
	seen    map[string]map[string]int64 // user -> ip -> last unix
	live    map[string]map[*liveConn]bool
	logf    func(string, ...any)
}

type liveConn struct{ close func() }

// tuicBase is what needs a restart when it changes (users do not).
func tuicBase(in tuicInbound) string {
	t := in.StreamSettings.TLSSettings
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|%s|%s|%v", in.Listen, in.Port, in.Settings.CongestionControl, t.ServerName, t.ALPN)
	for _, c := range t.Certificates {
		b.WriteString(strings.Join(c.Certificate, ""))
		b.WriteString(strings.Join(c.Key, ""))
	}
	return b.String()
}

func parseUUID(s string) ([16]byte, error) {
	var u [16]byte
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		return u, fmt.Errorf("bad uuid %q", s)
	}
	copy(u[:], b)
	return u, nil
}

func startTUIC(in tuicInbound, logf func(string, ...any)) (*tuicServer, error) {
	t := in.StreamSettings.TLSSettings
	if len(t.Certificates) == 0 {
		return nil, fmt.Errorf("tuic %s: no certificate", in.Tag)
	}
	cert, err := tls.X509KeyPair([]byte(strings.Join(t.Certificates[0].Certificate, "\n")), []byte(strings.Join(t.Certificates[0].Key, "\n")))
	if err != nil {
		return nil, fmt.Errorf("tuic %s: certificate: %w", in.Tag, err)
	}
	alpn := t.ALPN
	if len(alpn) == 0 {
		alpn = []string{"h3"}
	}
	cc := in.Settings.CongestionControl
	if cc == "" {
		cc = "bbr"
	}
	s := &tuicServer{allowed: map[string]bool{}, counts: map[string]*tuicCount{}, seen: map[string]map[string]int64{}, live: map[string]map[*liveConn]bool{}, logf: logf}
	svc, err := tuic.NewService[string](tuic.ServiceOptions{
		Context: context.Background(), Logger: logger.NOP(),
		TLSConfig:         &tuicTLS{cfg: &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: alpn, MinVersion: tls.VersionTLS13}},
		CongestionControl: cc, AuthTimeout: 3 * time.Second, Heartbeat: 10 * time.Second, UDPTimeout: 2 * time.Minute,
		Handler: s,
	})
	if err != nil {
		return nil, err
	}
	s.svc = svc
	s.setUsers(in.Settings.Clients)
	listen := in.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	pc, err := net.ListenPacket("udp", net.JoinHostPort(listen, strconv.Itoa(in.Port)))
	if err != nil {
		return nil, err
	}
	if err := svc.Start(pc); err != nil {
		pc.Close()
		return nil, err
	}
	s.conn = pc
	return s, nil
}

// setUsers swaps the user list and cuts every open stream of a user who
// is no longer on it (an authenticated QUIC session would otherwise keep
// working).
func (s *tuicServer) setUsers(list []tuicUser) {
	var names, pws []string
	var ids [][16]byte
	allowed := map[string]bool{}
	for _, u := range list {
		id, err := parseUUID(u.UUID)
		if err != nil || u.Email == "" || u.Password == "" {
			continue
		}
		names, ids, pws = append(names, u.Email), append(ids, id), append(pws, u.Password)
		allowed[u.Email] = true
	}
	s.mu.Lock()
	s.allowed = allowed
	var cut []*liveConn
	for email, set := range s.live {
		if !allowed[email] {
			for c := range set {
				cut = append(cut, c)
			}
			delete(s.live, email)
			delete(s.seen, email)
		}
	}
	s.mu.Unlock()
	s.svc.UpdateUsers(names, ids, pws)
	for _, c := range cut {
		c.close()
	}
}

// admit checks the stream's user, records its address and returns its
// counter and a handle to forget the stream when it ends.
func (s *tuicServer) admit(ctx context.Context, src M.Socksaddr, closer func()) (string, *tuicCount, *liveConn, bool) {
	email, _ := auth.UserFromContext[string](ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.allowed[email] {
		return "", nil, nil, false
	}
	c := s.counts[email]
	if c == nil {
		c = &tuicCount{}
		s.counts[email] = c
	}
	if s.seen[email] == nil {
		s.seen[email] = map[string]int64{}
	}
	s.seen[email][src.AddrString()] = time.Now().Unix()
	lc := &liveConn{close: closer}
	if s.live[email] == nil {
		s.live[email] = map[*liveConn]bool{}
	}
	s.live[email][lc] = true
	return email, c, lc, true
}

func (s *tuicServer) forget(email string, lc *liveConn) {
	s.mu.Lock()
	delete(s.live[email], lc)
	if t := s.seen[email]; t != nil {
		// a stream that ends refreshes the user's last-seen time
		for ip := range t {
			t[ip] = time.Now().Unix()
		}
	}
	s.mu.Unlock()
}

// resolveDest turns a stream's destination into an address the node may
// reach (never the node's own or private networks).
func resolveDest(d M.Socksaddr) (netip.AddrPort, error) {
	var a netip.Addr
	if d.IsFqdn() {
		ips, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", d.Fqdn)
		if err != nil || len(ips) == 0 {
			return netip.AddrPort{}, fmt.Errorf("resolve %s: %v", d.Fqdn, err)
		}
		a = ips[0]
	} else {
		a = d.Addr
	}
	a = a.Unmap()
	if blockedDest(a) {
		return netip.AddrPort{}, fmt.Errorf("destination %s is not allowed", a)
	}
	return netip.AddrPortFrom(a, d.Port), nil
}

func (s *tuicServer) NewConnectionEx(ctx context.Context, conn net.Conn, src, dst M.Socksaddr, onClose N.CloseHandlerFunc) {
	email, cnt, lc, ok := s.admit(ctx, src, func() { conn.Close() })
	if !ok {
		conn.Close()
		return
	}
	defer s.forget(email, lc)
	ap, err := resolveDest(dst)
	if err != nil {
		conn.Close()
		return
	}
	out, err := forwardDial("tcp", ap.String())
	if err != nil {
		conn.Close()
		return
	}
	_ = bufio.CopyConn(ctx, &countConn{Conn: conn, read: &cnt.up, write: &cnt.down}, out)
}

func (s *tuicServer) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, src, dst M.Socksaddr, onClose N.CloseHandlerFunc) {
	email, cnt, lc, ok := s.admit(ctx, src, func() { conn.Close() })
	if !ok {
		conn.Close()
		return
	}
	defer s.forget(email, lc)
	pc, err := net.ListenPacket("udp", "")
	if err != nil {
		conn.Close()
		return
	}
	out := &guardPacket{PacketConn: bufio.NewPacketConn(pc)}
	_ = bufio.CopyPacketConn(ctx, &countPacket{PacketConn: conn, cnt: cnt}, out)
}

func (s *tuicServer) take() (users []userBytes) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for email, c := range s.counts {
		up, down := c.up.Swap(0), c.down.Swap(0)
		if up+down > 0 {
			users = append(users, userBytes{email: email, up: up, down: down})
		}
		if !s.allowed[email] && up+down == 0 {
			delete(s.counts, email)
		}
	}
	return users
}

func (s *tuicServer) online(now time.Time) map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]string{}
	for email, ips := range s.seen {
		for ip, at := range ips {
			if len(s.live[email]) > 0 || now.Unix()-at <= 180 {
				out[email] = append(out[email], ip)
			} else if now.Unix()-at > 3600 {
				delete(ips, ip)
			}
		}
		sort.Strings(out[email])
	}
	return out
}

func (s *tuicServer) close() {
	s.svc.Close()
	s.conn.Close()
	s.mu.Lock()
	var cut []*liveConn
	for _, set := range s.live {
		for c := range set {
			cut = append(cut, c)
		}
	}
	s.mu.Unlock()
	for _, c := range cut {
		c.close()
	}
}

// countConn counts a client stream: reads are upload, writes download.
type countConn struct {
	net.Conn
	read, write *atomic.Uint64
}

func (c *countConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Add(uint64(n))
	return n, err
}

func (c *countConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.write.Add(uint64(n))
	return n, err
}

type countPacket struct {
	N.PacketConn
	cnt *tuicCount
}

func (c *countPacket) ReadPacket(b *buf.Buffer) (M.Socksaddr, error) {
	d, err := c.PacketConn.ReadPacket(b)
	if err == nil {
		c.cnt.up.Add(uint64(b.Len()))
	}
	return d, err
}

func (c *countPacket) WritePacket(b *buf.Buffer, d M.Socksaddr) error {
	n := b.Len()
	err := c.PacketConn.WritePacket(b, d)
	if err == nil {
		c.cnt.down.Add(uint64(n))
	}
	return err
}

// guardPacket is the node side of a UDP relay: it resolves names and drops
// packets to private or local addresses.
type guardPacket struct {
	N.PacketConn
}

func (g *guardPacket) WritePacket(b *buf.Buffer, d M.Socksaddr) error {
	ap, err := resolveDest(d)
	if err != nil {
		b.Release()
		return nil // dropped, the relay goes on
	}
	return g.PacketConn.WritePacket(b, M.SocksaddrFromNetIP(udpTarget(ap)))
}

// udpTarget lets tests point UDP relays at a local server.
var udpTarget = func(ap netip.AddrPort) netip.AddrPort { return ap }

// tuicTLS hands sing-quic a plain crypto/tls server config.
type tuicTLS struct{ cfg *tls.Config }

func (t *tuicTLS) ServerName() string                  { return t.cfg.ServerName }
func (t *tuicTLS) SetServerName(n string)              { t.cfg.ServerName = n }
func (t *tuicTLS) NextProtos() []string                { return t.cfg.NextProtos }
func (t *tuicTLS) SetNextProtos(p []string)            { t.cfg.NextProtos = p }
func (t *tuicTLS) HandshakeTimeout() time.Duration     { return 10 * time.Second }
func (t *tuicTLS) SetHandshakeTimeout(time.Duration)   {}
func (t *tuicTLS) STDConfig() (*aTLS.STDConfig, error) { return t.cfg, nil }
func (t *tuicTLS) Client(net.Conn) (aTLS.Conn, error)  { return nil, fmt.Errorf("server only") }
func (t *tuicTLS) Clone() aTLS.Config                  { return &tuicTLS{cfg: t.cfg.Clone()} }
func (t *tuicTLS) Start() error                        { return nil }
func (t *tuicTLS) Close() error                        { return nil }
func (t *tuicTLS) Server(c net.Conn) (aTLS.Conn, error) {
	return tls.Server(c, t.cfg), nil
}
