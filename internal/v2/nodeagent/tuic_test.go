package nodeagent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
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

func pemCert(t *testing.T) (certLines, keyLines []string) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "www.bing.com"}, DNSNames: []string{"www.bing.com"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	c := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	kk := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))
	return strings.Split(strings.TrimSpace(c), "\n"), strings.Split(strings.TrimSpace(kk), "\n")
}

func TestTUICEndToEnd(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 40000))) }))
	defer web.Close()
	webAddr := strings.TrimPrefix(web.URL, "http://")
	oldDial := forwardDial
	forwardDial = func(network, addr string) (net.Conn, error) { return net.Dial(network, webAddr) }
	defer func() { forwardDial = oldDial }()
	echo, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer echo.Close()
	go func() {
		b := make([]byte, 2048)
		for {
			n, a, err := echo.ReadFrom(b)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(b[:n], a)
		}
	}()
	oldUDP := udpTarget
	udpTarget = func(netip.AddrPort) netip.AddrPort { return echo.LocalAddr().(*net.UDPAddr).AddrPort() }
	defer func() { udpTarget = oldUDP }()

	l, _ := net.ListenPacket("udp", "127.0.0.1:0")
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	cl, kl := pemCert(t)
	var in tuicInbound
	in.Tag, in.Listen, in.Port = "tuic1", "127.0.0.1", port
	in.Settings.Clients = []tuicUser{{Email: "ali", UUID: "1e49a458-7c7c-4ef4-94c8-f320ef6a9436", Password: "pw-ali"}}
	in.StreamSettings.TLSSettings.ServerName = "www.bing.com"
	in.StreamSettings.TLSSettings.Certificates = append(in.StreamSettings.TLSSettings.Certificates, struct {
		Certificate []string `json:"certificate"`
		Key         []string `json:"key"`
	}{cl, kl})
	srv, err := startTUIC(in, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()

	id, _ := parseUUID("1e49a458-7c7c-4ef4-94c8-f320ef6a9436")
	cli, err := tuic.NewClient(tuic.ClientOptions{Context: context.Background(), Dialer: N.SystemDialer,
		ServerAddress: M.ParseSocksaddr("127.0.0.1:" + itoa(port)),
		TLSConfig:     &clientTLS{cfg: &tls.Config{ServerName: "www.bing.com", InsecureSkipVerify: true, NextProtos: []string{"h3"}}},
		UUID:          id, Password: "pw-ali", CongestionControl: "bbr"})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.CloseWithError(nil)
	hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		return cli.DialConn(ctx, M.ParseSocksaddr(a))
	}}}
	resp, err := hc.Get("http://203.0.113.10:8080/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 40000 {
		t.Fatalf("got %d bytes", len(body))
	}
	// UDP (DNS and games): an echo through the relay.
	pc, err := cli.ListenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.WriteTo([]byte("ping-udp"), M.ParseSocksaddr("203.0.113.10:53").UDPAddr()); err != nil {
		t.Fatal(err)
	}
	_ = pc.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, 64)
	n, _, err := pc.ReadFrom(b)
	if err != nil || string(b[:n]) != "ping-udp" {
		t.Fatalf("udp echo %q %v", b[:n], err)
	}
	pc.Close()
	time.Sleep(200 * time.Millisecond)
	users := srv.take()
	if len(users) != 1 || users[0].email != "ali" || users[0].down < 40000 || users[0].up == 0 {
		t.Fatalf("usage %+v", users)
	}
	if on := srv.online(time.Now()); len(on["ali"]) != 1 || on["ali"][0] != "127.0.0.1" {
		t.Fatalf("online %v", on)
	}
	// A private destination is refused.
	if _, err := hc.Get("http://10.1.2.3:80/"); err == nil {
		t.Fatal("private destination must be refused")
	}
	// Removed (disabled) user: the already-open session can no longer be used.
	srv.setUsers(nil)
	hc.Timeout = 3 * time.Second
	if _, err := hc.Get("http://203.0.113.10:8080/again"); err == nil {
		t.Fatal("a removed user must not get through")
	}
}

func itoa(n int) string { return big.NewInt(int64(n)).String() }
