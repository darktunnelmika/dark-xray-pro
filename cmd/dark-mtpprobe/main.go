// dark-mtpprobe: an MTProto proxy client for checking a node from outside
// without a Telegram account. It takes a tg://proxy link (fake-TLS "ee"
// secret), does the fake-TLS and obfuscated2 handshakes like a Telegram
// app, then sends req_pq_multi to a Telegram DC through the proxy. A resPQ
// that echoes our nonce proves the proxy really reaches Telegram.
package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/9seconds/mtg/v2/mtglib/obfuscation"
)

// tlsConn wraps writes in TLS application records and unwraps reads.
type tlsConn struct {
	*net.TCPConn
	r   *bufio.Reader
	buf bytes.Buffer
}

func (c *tlsConn) Write(p []byte) (int, error) {
	for off := 0; off < len(p); {
		n := min(len(p)-off, 16000)
		rec := append([]byte{0x17, 3, 3, byte(n >> 8), byte(n)}, p[off:off+n]...)
		if _, err := c.TCPConn.Write(rec); err != nil {
			return off, err
		}
		off += n
	}
	return len(p), nil
}

func (c *tlsConn) Read(p []byte) (int, error) {
	for c.buf.Len() == 0 {
		typ, body, err := readRecord(c.r)
		if err != nil {
			return 0, err
		}
		if typ == 0x17 {
			c.buf.Write(body)
		}
	}
	return c.buf.Read(p)
}

func readRecord(r io.Reader) (byte, []byte, error) {
	h := make([]byte, 5)
	if _, err := io.ReadFull(r, h); err != nil {
		return 0, nil, err
	}
	body := make([]byte, int(h[3])<<8|int(h[4]))
	_, err := io.ReadFull(r, body)
	return h[0], body, err
}

func clientHello(key []byte, host string) []byte {
	ext := func(typ uint16, data []byte) []byte {
		return append(binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, typ), uint16(len(data))), data...)
	}
	sni := append([]byte{0}, binary.BigEndian.AppendUint16(nil, uint16(len(host)))...)
	sni = append(sni, host...)
	sni = append(binary.BigEndian.AppendUint16(nil, uint16(len(sni))), sni...)
	var exts []byte
	exts = append(exts, ext(0, sni)...)
	exts = append(exts, ext(0x2b, []byte{4, 3, 4, 3, 3})...)          // supported_versions
	exts = append(exts, ext(0x0a, []byte{0, 4, 0, 0x1d, 0, 0x17})...) // groups
	exts = append(exts, ext(0x0d, []byte{0, 4, 4, 3, 8, 4})...)       // signature algorithms
	ks := make([]byte, 32)
	_, _ = rand.Read(ks)
	exts = append(exts, ext(0x33, append([]byte{0, 36, 0, 0x1d, 0, 32}, ks...))...) // key_share x25519
	sid := make([]byte, 32)
	_, _ = rand.Read(sid)
	var hs []byte
	hs = append(hs, 3, 3)
	hs = append(hs, make([]byte, 32)...) // random, filled below
	hs = append(hs, 32)
	hs = append(hs, sid...)
	hs = append(hs, 0, 6, 0x13, 0x01, 0x13, 0x02, 0x13, 0x03)
	hs = append(hs, 1, 0)
	hs = append(hs, binary.BigEndian.AppendUint16(nil, uint16(len(exts)))...)
	hs = append(hs, exts...)
	msg := append([]byte{1, byte(len(hs) >> 16), byte(len(hs) >> 8), byte(len(hs))}, hs...)
	rec := append([]byte{0x16, 3, 1, byte(len(msg) >> 8), byte(len(msg))}, msg...)
	mac := hmac.New(sha256.New, key)
	mac.Write(rec)
	random := mac.Sum(nil)
	ts := make([]byte, 4)
	binary.LittleEndian.PutUint32(ts, uint32(time.Now().Unix()))
	for i := 0; i < 4; i++ {
		random[28+i] ^= ts[i]
	}
	copy(rec[11:43], random)
	return rec
}

func run(link string, dc int) (time.Duration, error) {
	u, err := url.Parse(link)
	if err != nil {
		return 0, err
	}
	q := u.Query()
	sec, err := hex.DecodeString(q.Get("secret"))
	if err != nil || len(sec) < 18 || sec[0] != 0xee {
		return 0, fmt.Errorf("need a fake-TLS (ee) hex secret")
	}
	key, host := sec[1:17], string(sec[17:])
	addr := net.JoinHostPort(q.Get("server"), q.Get("port"))
	raw, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return 0, err
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(20 * time.Second))
	tcp := raw.(*net.TCPConn)
	if _, err := tcp.Write(clientHello(key, host)); err != nil {
		return 0, err
	}
	br := bufio.NewReader(tcp)
	// ServerHello, ChangeCipherSpec, then one application record of noise.
	for _, want := range []byte{0x16, 0x14, 0x17} {
		typ, _, err := readRecord(br)
		if err != nil {
			return 0, fmt.Errorf("fake-TLS reply: %w", err)
		}
		if typ != want {
			return 0, fmt.Errorf("fake-TLS reply: record type %#x, want %#x (wrong secret or not an MTProto proxy)", typ, want)
		}
	}
	_, _ = tcp.Write([]byte{0x14, 3, 3, 0, 1, 1})
	tc := &tlsConn{TCPConn: tcp, r: br}
	obfs, err := obfuscation.Obfuscator{Secret: key}.SendHandshake(tc, dc)
	if err != nil {
		return 0, err
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	body := binary.LittleEndian.AppendUint32(nil, 0xbe7e8ef1)
	body = append(body, nonce...)
	msg := make([]byte, 20, 20+len(body))
	now := time.Now()
	binary.LittleEndian.PutUint64(msg[8:], (uint64(now.Unix())<<32|uint64(now.Nanosecond())<<2)&^3)
	binary.LittleEndian.PutUint32(msg[16:], uint32(len(body)))
	msg = append(msg, body...)
	frame := binary.LittleEndian.AppendUint32(nil, uint32(len(msg)))
	start := time.Now()
	if _, err := obfs.Write(append(frame, msg...)); err != nil {
		return 0, err
	}
	lb := make([]byte, 4)
	if _, err := io.ReadFull(obfs, lb); err != nil {
		return 0, fmt.Errorf("no reply from Telegram: %w", err)
	}
	n := binary.LittleEndian.Uint32(lb)
	if n < 40 || n > 1024 {
		return 0, fmt.Errorf("odd reply length %d", n)
	}
	resp := make([]byte, n)
	if _, err := io.ReadFull(obfs, resp); err != nil {
		return 0, err
	}
	rtt := time.Since(start)
	if binary.LittleEndian.Uint32(resp[20:24]) != 0x05162463 || !bytes.Equal(resp[24:40], nonce) {
		return rtt, fmt.Errorf("the reply is not Telegram's resPQ")
	}
	return rtt, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: dark-mtpprobe 'tg://proxy?server=..&port=..&secret=ee..' [dc]")
		os.Exit(2)
	}
	link := strings.Replace(os.Args[1], "https://t.me/proxy", "tg://proxy", 1)
	dcs := []int{1, 2, 3, 4, 5}
	if len(os.Args) > 2 {
		d, _ := strconv.Atoi(os.Args[2])
		dcs = []int{d}
	}
	bad := 0
	for _, dc := range dcs {
		rtt, err := run(link, dc)
		if err != nil {
			bad++
			fmt.Printf("DC%d FAIL: %v\n", dc, err)
			continue
		}
		fmt.Printf("DC%d OK: Telegram answered through the proxy in %s\n", dc, rtt.Round(time.Millisecond))
	}
	if bad > 0 {
		os.Exit(1)
	}
}
