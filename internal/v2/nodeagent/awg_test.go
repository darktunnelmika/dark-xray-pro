package nodeagent

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
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
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

// clientTun is a minimal user-space client end for the test.
type clientTun struct {
	ep     *channel.Endpoint
	s      *stack.Stack
	events chan tun.Event
	in     chan *buffer.View
	closed chan struct{}
	once   sync.Once
}

func newClientTun(t *testing.T, addr string) *clientTun {
	s := stack.New(stack.Options{NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol}, TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol}})
	c := &clientTun{ep: channel.New(256, 1280, ""), s: s, events: make(chan tun.Event, 2), in: make(chan *buffer.View, 256), closed: make(chan struct{})}
	c.ep.AddNotify(c)
	if err := s.CreateNIC(1, c.ep); err != nil {
		t.Fatal(err)
	}
	a := netip.MustParseAddr(addr)
	_ = s.AddProtocolAddress(1, tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddrFromSlice(a.AsSlice()).WithPrefix()}, stack.AddressProperties{})
	s.AddRoute(tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: 1})
	c.events <- tun.EventUp
	return c
}
func (c *clientTun) WriteNotify() {
	if p := c.ep.Read(); p != nil {
		select {
		case c.in <- p.ToView():
		default:
		}
		p.DecRef()
	}
}
func (c *clientTun) Name() (string, error)    { return "client", nil }
func (c *clientTun) File() *os.File           { return nil }
func (c *clientTun) Events() <-chan tun.Event { return c.events }
func (c *clientTun) MTU() (int, error)        { return 1280, nil }
func (c *clientTun) BatchSize() int           { return 1 }
func (c *clientTun) Close() error {
	c.once.Do(func() { close(c.closed); c.s.RemoveNIC(1); c.ep.Close(); close(c.events) })
	return nil
}
func (c *clientTun) Read(b [][]byte, sz []int, off int) (int, error) {
	var v *buffer.View
	select {
	case v = <-c.in:
	case <-c.closed:
		return 0, os.ErrClosed
	}
	n, err := v.Read(b[0][off:])
	sz[0] = n
	return 1, err
}
func (c *clientTun) Write(b [][]byte, off int) (int, error) {
	for _, x := range b {
		pb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(x[off:])})
		c.ep.InjectInbound(header.IPv4ProtocolNumber, pb)
		pb.DecRef()
	}
	return len(b), nil
}
func (c *clientTun) dial(ctx context.Context, addr string) (net.Conn, error) {
	ap := netip.MustParseAddrPort(addr)
	return gonet.DialContextTCP(ctx, c.s, tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFromSlice(ap.Addr().AsSlice()), Port: ap.Port()}, ipv4.ProtocolNumber)
}

func wgKey(t *testing.T) (priv, pub string) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
}

func hexKey(b64 string) string {
	raw, _ := base64.StdEncoding.DecodeString(b64)
	return hex.EncodeToString(raw)
}

func TestSplitSide(t *testing.T) {
	raw := []byte(`{"inbounds":[{"tag":"api","protocol":"dokodemo-door"},{"tag":"a","protocol":"amneziawg","port":1},{"tag":"m","protocol":"mtproto","port":2},{"tag":"v","protocol":"vless"}],"outbounds":[]}`)
	x, side, err := SplitSide(raw)
	if err != nil || len(side) != 2 || strings.Contains(string(x), "amneziawg") || strings.Contains(string(x), "mtproto") || !strings.Contains(string(x), `"vless"`) {
		t.Fatal(string(x), len(side), err)
	}
	same, none, _ := SplitSide([]byte(`{"inbounds":[{"tag":"v","protocol":"vless"}]}`))
	if none != nil || !strings.Contains(string(same), "vless") {
		t.Fatal("configs without side inbounds pass unchanged")
	}
}

// A real AmneziaWG client (user-space) reaches a web server through the
// agent's AmneziaWG server; the bytes are counted for its email and a
// removed peer stops working.
func TestAWGEndToEnd(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 50000))) }))
	defer web.Close()
	// The client asks for a public test address; the node side reaches the local web server.
	webAddr := strings.TrimPrefix(web.URL, "http://")
	old := forwardDial
	forwardDial = func(network, addr string) (net.Conn, error) { return net.Dial(network, webAddr) }
	defer func() { forwardDial = old }()
	sPriv, sPub := wgKey(t)
	cPriv, cPub := wgKey(t)
	l, _ := net.ListenPacket("udp", "127.0.0.1:0")
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	obf := map[string]any{"jc": 4, "jmin": 40, "jmax": 70, "s1": 33, "s2": 77, "h1": 123456, "h2": 234567, "h3": 345678, "h4": 456789}
	set := map[string]any{"secretKey": sPriv, "mtu": 1280, "peers": []any{map[string]any{"publicKey": cPub, "allowedIPs": []string{"10.67.0.2/32"}, "email": "ali"}}}
	for k, v := range obf {
		set[k] = v
	}
	in, _ := json.Marshal(map[string]any{"tag": "awg1", "protocol": "amneziawg", "listen": "127.0.0.1", "port": port, "settings": set})
	side := NewSideServers(t.Logf)
	defer side.Close()
	side.Apply([]json.RawMessage{in})
	if side.awg["awg1"] == nil {
		t.Fatal("server not started")
	}
	// Client.
	ctun := newClientTun(t, "10.67.0.2")
	cdev := device.NewDevice(ctun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "client: "))
	defer cdev.Close()
	conf := fmt.Sprintf("private_key=%s\njc=4\njmin=40\njmax=70\ns1=33\ns2=77\nh1=123456\nh2=234567\nh3=345678\nh4=456789\npublic_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\npersistent_keepalive_interval=5\n",
		hexKey(cPriv), hexKey(sPub), port)
	if err := cdev.IpcSet(conf); err != nil {
		t.Fatal(err)
	}
	if err := cdev.Up(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) { return ctun.dial(ctx, a) }}}
	resp, err := client.Get("http://203.0.113.10:8080/")
	if err != nil {
		t.Fatal("through the tunnel:", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 50000 {
		t.Fatal("body", len(body))
	}
	users, inbounds := side.Take()
	if users["ali"][1] < 50000 || inbounds["awg1"][1] < 50000 {
		t.Fatalf("traffic not counted: %v %v", users, inbounds)
	}
	if on := side.Online(time.Now()); len(on["ali"]) != 1 {
		t.Fatal("online", on)
	}
	// Peer removed (client disabled): the next request fails.
	set["peers"] = []any{}
	in2, _ := json.Marshal(map[string]any{"tag": "awg1", "protocol": "amneziawg", "listen": "127.0.0.1", "port": port, "settings": set})
	side.Apply([]json.RawMessage{in2})
	client.Timeout = 3 * time.Second
	if _, err := client.Get("http://203.0.113.10:8080/again"); err == nil {
		t.Fatal("a removed peer must not get through")
	}
}
