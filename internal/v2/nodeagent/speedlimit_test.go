package nodeagent

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func socksDial(t *testing.T, proxy, user, pass, dst string) (net.Conn, bool) {
	c, err := net.Dial("tcp", proxy)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte{5, 1, 2})
	r := make([]byte, 2)
	io.ReadFull(c, r)
	_, _ = c.Write(append(append(append([]byte{1, byte(len(user))}, user...), byte(len(pass))), pass...))
	io.ReadFull(c, r)
	if r[1] != 0 {
		c.Close()
		return nil, false
	}
	host, port, _ := net.SplitHostPort(dst)
	ip := net.ParseIP(host).To4()
	var p int
	for _, ch := range port {
		p = p*10 + int(ch-'0')
	}
	req := append([]byte{5, 1, 0, 1}, ip...)
	req = binary.BigEndian.AppendUint16(req, uint16(p))
	_, _ = c.Write(req)
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0 {
		t.Fatalf("connect failed %v %v", rep, err)
	}
	return c, true
}

func TestSpeedLimiter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go NewLimiter().Serve(ctx, ln)
	// A server that sends 250 KB to whoever connects.
	srv, _ := net.Listen("tcp", "127.0.0.1:0")
	defer srv.Close()
	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			go func() { c.Write(make([]byte, 250<<10)); c.Close() }()
		}
	}()
	if _, ok := socksDial(t, ln.Addr().String(), "a@b", "x", srv.Addr().String()); ok {
		t.Fatal("a bad limit must be refused")
	}
	c, _ := socksDial(t, ln.Addr().String(), "a@b", "800", srv.Addr().String()) // 100 KB/s
	start := time.Now()
	n, _ := io.Copy(io.Discard, c)
	el := time.Since(start)
	if n != 250<<10 {
		t.Fatal("got", n)
	}
	// 250 KB at 100 KB/s with a 50 KB burst: about 2 s.
	if el < 1500*time.Millisecond || el > 4*time.Second {
		t.Fatal("took", el)
	}
	// Unlimited for comparison: a high limit is fast.
	c2, _ := socksDial(t, ln.Addr().String(), "c@d", "1000000", srv.Addr().String())
	start = time.Now()
	io.Copy(io.Discard, c2)
	if time.Since(start) > time.Second {
		t.Fatal("a high limit should not slow down")
	}
}
