package nodeagent

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Per-user speed limit. Xray has none, so the HUB routes a limited user's
// traffic to a SOCKS5 outbound pointing here (127.0.0.1 only). The SOCKS
// username is the user's email and the password the limit in kbit/s; all
// connections of one user share one bucket per direction.

// LimiterAddr is where the limiter listens; the HUB writes the same address.
const LimiterAddr = "127.0.0.1:62051"

type bucket struct {
	mu     sync.Mutex
	rate   float64 // bytes per second
	tokens float64
	last   time.Time
	used   time.Time
}

// take spends n bytes and sleeps off any debt, so the long-run rate holds.
func (b *bucket) take(ctx context.Context, n int) {
	b.mu.Lock()
	now := time.Now()
	burst := b.rate / 2
	if burst < 32<<10 {
		burst = 32 << 10
	}
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last, b.used = now, now
	b.tokens -= float64(n)
	wait := time.Duration(0)
	if b.tokens < 0 {
		wait = time.Duration(-b.tokens / b.rate * float64(time.Second))
	}
	b.mu.Unlock()
	if wait > 0 {
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
		}
	}
}

type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket // email + "|up" / "|down"
}

func NewLimiter() *Limiter { return &Limiter{buckets: map[string]*bucket{}} }

func (l *Limiter) bucket(key string, kbps int) *bucket {
	l.mu.Lock()
	defer l.mu.Unlock()
	rate := float64(kbps) * 1000 / 8
	b := l.buckets[key]
	if b == nil {
		b = &bucket{last: time.Now(), tokens: rate / 2}
		l.buckets[key] = b
	}
	b.mu.Lock()
	b.rate = rate
	b.mu.Unlock()
	if len(l.buckets) > 64 {
		for k, x := range l.buckets {
			x.mu.Lock()
			idle := time.Since(x.used) > 10*time.Minute && x != b
			x.mu.Unlock()
			if idle {
				delete(l.buckets, k)
			}
		}
	}
	return b
}

// Serve accepts SOCKS5 connections until ctx ends.
func (l *Limiter) Serve(ctx context.Context, ln net.Listener) {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go l.handle(ctx, c)
	}
}

// RunLimiter listens on LimiterAddr; failures are logged and retried.
func RunLimiter(ctx context.Context, logf func(string, ...any)) {
	l := NewLimiter()
	for ctx.Err() == nil {
		ln, err := net.Listen("tcp", LimiterAddr)
		if err != nil {
			logf("speed limiter: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}
		l.Serve(ctx, ln)
	}
}

var errSocks = errors.New("socks: bad request")

func readAddr(r io.Reader, atyp byte) (string, error) {
	var host string
	switch atyp {
	case 1, 4:
		b := make([]byte, map[byte]int{1: 4, 4: 16}[atyp])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return "", err
		}
		b := make([]byte, n[0])
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errSocks
	}
	var p [2]byte
	if _, err := io.ReadFull(r, p[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(p[:])))), nil
}

func socksAddr(a net.Addr) []byte {
	var ip net.IP
	port := 0
	switch x := a.(type) {
	case *net.TCPAddr:
		ip, port = x.IP, x.Port
	case *net.UDPAddr:
		ip, port = x.IP, x.Port
	}
	out := []byte{1}
	if v4 := ip.To4(); v4 != nil {
		out = append(out, v4...)
	} else if ip != nil {
		out = append([]byte{4}, ip.To16()...)
	} else {
		out = append(out, 0, 0, 0, 0)
	}
	return binary.BigEndian.AppendUint16(out, uint16(port))
}

func (l *Limiter) handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	h := make([]byte, 2)
	if _, err := io.ReadFull(c, h); err != nil || h[0] != 5 {
		return
	}
	methods := make([]byte, h[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	_, _ = c.Write([]byte{5, 2}) // username/password only
	// RFC 1929
	if _, err := io.ReadFull(c, h[:2]); err != nil || h[0] != 1 {
		return
	}
	user := make([]byte, h[1])
	if _, err := io.ReadFull(c, user); err != nil {
		return
	}
	if _, err := io.ReadFull(c, h[:1]); err != nil {
		return
	}
	pass := make([]byte, h[0])
	if _, err := io.ReadFull(c, pass); err != nil {
		return
	}
	kbps, err := strconv.Atoi(string(pass))
	if err != nil || kbps < 8 || len(user) == 0 {
		_, _ = c.Write([]byte{1, 1})
		return
	}
	_, _ = c.Write([]byte{1, 0})
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != 5 {
		return
	}
	dst, err := readAddr(c, req[3])
	if err != nil {
		return
	}
	up := l.bucket(string(user)+"|up", kbps)
	down := l.bucket(string(user)+"|down", kbps)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	switch req[1] {
	case 1: // CONNECT
		var d net.Dialer
		dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := d.DialContext(dctx, "tcp", dst)
		dcancel()
		if err != nil {
			_, _ = c.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		defer out.Close()
		_, _ = c.Write(append([]byte{5, 0, 0}, socksAddr(out.LocalAddr())...))
		_ = c.SetDeadline(time.Time{})
		done := make(chan struct{}, 2)
		go func() { limitedCopy(ctx, out, c, up); closeWrite(out); done <- struct{}{} }()
		go func() { limitedCopy(ctx, c, out, down); closeWrite(c); done <- struct{}{} }()
		<-done
		// Give the other side a moment to drain, then stop both.
		select {
		case <-done:
		case <-time.After(30 * time.Second):
		}
	case 3: // UDP ASSOCIATE
		l.udp(ctx, c, up, down)
	default:
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
	}
}

func closeWrite(c net.Conn) {
	if t, ok := c.(*net.TCPConn); ok {
		_ = t.CloseWrite()
		return
	}
	_ = c.Close()
}

func limitedCopy(ctx context.Context, dst io.Writer, src io.Reader, b *bucket) {
	buf := make([]byte, 16<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			b.take(ctx, n)
			if ctx.Err() != nil {
				return
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (l *Limiter) udp(ctx context.Context, c net.Conn, up, down *bucket) {
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_, _ = c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer local.Close()
	remote, err := net.ListenUDP("udp", nil)
	if err != nil {
		_, _ = c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	_, _ = c.Write(append([]byte{5, 0, 0}, socksAddr(local.LocalAddr())...))
	_ = c.SetDeadline(time.Time{})
	// The association lives as long as its TCP connection.
	go func() { _, _ = io.Copy(io.Discard, c); local.Close(); remote.Close() }()
	var mu sync.Mutex
	var client *net.UDPAddr
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, from, err := remote.ReadFromUDP(buf)
			if err != nil {
				return
			}
			mu.Lock()
			to := client
			mu.Unlock()
			if to == nil {
				continue
			}
			down.take(ctx, n)
			pkt := append([]byte{0, 0, 0}, socksAddr(from)...)
			_, _ = local.WriteToUDP(append(pkt, buf[:n]...), to)
		}
	}()
	buf := make([]byte, 64<<10)
	for {
		n, from, err := local.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 4 || buf[2] != 0 { // fragments are not supported
			continue
		}
		mu.Lock()
		client = from
		mu.Unlock()
		r := &sliceReader{b: buf[4:n]}
		dst, err := readAddr(r, buf[3])
		if err != nil {
			continue
		}
		ua, err := net.ResolveUDPAddr("udp", dst)
		if err != nil {
			continue
		}
		up.take(ctx, len(r.b))
		_, _ = remote.WriteToUDP(r.b, ua)
	}
}

// sliceReader reads the address after the ATYP byte, leaving the payload.
type sliceReader struct{ b []byte }

func (s *sliceReader) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.b)
	s.b = s.b[n:]
	return n, nil
}
