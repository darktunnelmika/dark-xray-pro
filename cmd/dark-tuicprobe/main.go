// dark-tuicprobe: a TUIC v5 client for checking a node from outside. It
// takes a tuic:// share link, fetches a page by IP over a TCP stream and
// sends one DNS query over UDP.
package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sagernet/sing-quic/tuic"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
)

type clientTLS struct{ cfg *tls.Config }

func (t *clientTLS) ServerName() string                  { return t.cfg.ServerName }
func (t *clientTLS) SetServerName(n string)              { t.cfg.ServerName = n }
func (t *clientTLS) NextProtos() []string                { return t.cfg.NextProtos }
func (t *clientTLS) SetNextProtos(p []string)            { t.cfg.NextProtos = p }
func (t *clientTLS) HandshakeTimeout() time.Duration     { return 10 * time.Second }
func (t *clientTLS) SetHandshakeTimeout(time.Duration)   {}
func (t *clientTLS) STDConfig() (*aTLS.STDConfig, error) { return t.cfg, nil }
func (t *clientTLS) Client(c net.Conn) (aTLS.Conn, error) {
	return tls.Client(c, t.cfg), nil
}
func (t *clientTLS) Clone() aTLS.Config { return &clientTLS{cfg: t.cfg.Clone()} }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: dark-tuicprobe 'tuic://uuid:password@host:port?sni=...'")
		os.Exit(2)
	}
	u, err := url.Parse(os.Args[1])
	if err != nil || u.Scheme != "tuic" || u.User == nil {
		fmt.Println("FAIL: not a tuic:// link")
		os.Exit(2)
	}
	pw, _ := u.User.Password()
	raw, err := hex.DecodeString(strings.ReplaceAll(u.User.Username(), "-", ""))
	if err != nil || len(raw) != 16 {
		fmt.Println("FAIL: bad uuid")
		os.Exit(2)
	}
	var id [16]byte
	copy(id[:], raw)
	q := u.Query()
	insecure := q.Get("allow_insecure") == "1" || q.Get("insecure") == "1"
	cc := q.Get("congestion_control")
	if cc == "" {
		cc = "bbr"
	}
	cli, err := tuic.NewClient(tuic.ClientOptions{Context: context.Background(), Dialer: N.SystemDialer,
		ServerAddress: M.ParseSocksaddr(u.Host),
		TLSConfig:     &clientTLS{cfg: &tls.Config{ServerName: q.Get("sni"), InsecureSkipVerify: insecure, NextProtos: []string{"h3"}}},
		UUID:          id, Password: pw, CongestionControl: cc})
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
	defer cli.CloseWithError(nil)
	hc := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		return cli.DialConn(ctx, M.ParseSocksaddr(a))
	}}}
	resp, err := hc.Get("http://1.1.1.1/cdn-cgi/trace")
	if err != nil {
		fmt.Println("FAIL tcp:", err)
		os.Exit(1)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, l := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(l, "ip=") || strings.HasPrefix(l, "loc=") {
			fmt.Println("tcp", l)
		}
	}
	// UDP: one DNS query (A example.com) to 1.1.1.1.
	pc, err := cli.ListenPacket(context.Background())
	if err != nil {
		fmt.Println("FAIL udp:", err)
		os.Exit(1)
	}
	defer pc.Close()
	msg := []byte{0x12, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, part := range []string{"example", "com"} {
		msg = append(msg, byte(len(part)))
		msg = append(msg, part...)
	}
	msg = append(msg, 0, 0, 1, 0, 1)
	if _, err := pc.WriteTo(msg, M.ParseSocksaddr("1.1.1.1:53").UDPAddr()); err != nil {
		fmt.Println("FAIL udp:", err)
		os.Exit(1)
	}
	_ = pc.SetReadDeadline(time.Now().Add(8 * time.Second))
	b := make([]byte, 1500)
	n, _, err := pc.ReadFrom(b)
	if err != nil || n < 12 || b[0] != 0x12 || b[1] != 0x34 {
		fmt.Println("FAIL udp: no DNS answer", err)
		os.Exit(1)
	}
	fmt.Printf("udp dns answers=%d bytes=%d\n", binary.BigEndian.Uint16(b[6:8]), n)
}
