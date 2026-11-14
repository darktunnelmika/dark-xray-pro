// dark-awgprobe: an AmneziaWG client for checking a node from outside. It reads a wg-quick
// conf (with Jc..H4), connects in user space and fetches a URL by IP.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
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

type ctun struct {
	ep     *channel.Endpoint
	s      *stack.Stack
	events chan tun.Event
	in     chan *buffer.View
	closed chan struct{}
	once   sync.Once
}

func (c *ctun) WriteNotify() {
	if p := c.ep.Read(); p != nil {
		select {
		case c.in <- p.ToView():
		default:
		}
		p.DecRef()
	}
}
func (c *ctun) Name() (string, error)    { return "awgc", nil }
func (c *ctun) File() *os.File           { return nil }
func (c *ctun) Events() <-chan tun.Event { return c.events }
func (c *ctun) MTU() (int, error)        { return 1280, nil }
func (c *ctun) BatchSize() int           { return 1 }
func (c *ctun) Close() error {
	c.once.Do(func() { close(c.closed); c.s.RemoveNIC(1); c.ep.Close(); close(c.events) })
	return nil
}
func (c *ctun) Read(b [][]byte, sz []int, off int) (int, error) {
	select {
	case v := <-c.in:
		n, err := v.Read(b[0][off:])
		sz[0] = n
		return 1, err
	case <-c.closed:
		return 0, os.ErrClosed
	}
}
func (c *ctun) Write(b [][]byte, off int) (int, error) {
	for _, x := range b {
		pb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(x[off:])})
		c.ep.InjectInbound(header.IPv4ProtocolNumber, pb)
		pb.DecRef()
	}
	return len(b), nil
}

func b64hex(s string) string {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	addr := netip.MustParsePrefix(kv["Address"]).Addr()
	s := stack.New(stack.Options{NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol}, TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol}})
	c := &ctun{ep: channel.New(256, 1280, ""), s: s, events: make(chan tun.Event, 2), in: make(chan *buffer.View, 256), closed: make(chan struct{})}
	c.ep.AddNotify(c)
	s.CreateNIC(1, c.ep)
	s.AddProtocolAddress(1, tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddrFromSlice(addr.AsSlice()).WithPrefix()}, stack.AddressProperties{})
	s.AddRoute(tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: 1})
	c.events <- tun.EventUp
	dev := device.NewDevice(c, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "awgc "))
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", b64hex(kv["PrivateKey"]))
	for _, k := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "H1", "H2", "H3", "H4"} {
		if v := kv[k]; v != "" {
			fmt.Fprintf(&b, "%s=%s\n", strings.ToLower(k), v)
		}
	}
	fmt.Fprintf(&b, "public_key=%s\nendpoint=%s\nallowed_ip=0.0.0.0/0\npersistent_keepalive_interval=25\n", b64hex(kv["PublicKey"]), kv["Endpoint"])
	if err := dev.IpcSet(b.String()); err != nil {
		panic(err)
	}
	dev.Up()
	defer dev.Close()
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		ap := netip.MustParseAddrPort(a)
		return gonet.DialContextTCP(ctx, s, tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFromSlice(ap.Addr().AsSlice()), Port: ap.Port()}, ipv4.ProtocolNumber)
	}}}
	url := "http://1.1.1.1/cdn-cgi/trace"
	if len(os.Args) > 2 {
		url = os.Args[2]
	}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, l := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(l, "ip=") || strings.HasPrefix(l, "loc=") || strings.HasPrefix(l, "colo=") {
			fmt.Println(l)
		}
	}
	fmt.Println("bytes:", len(body))
}
