package nodeagent

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestMTProtoStarts(t *testing.T) {
	mtpLogLevel = zerolog.InfoLevel
	defer func() { mtpLogLevel = zerolog.WarnLevel }()
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var in mtpInbound
	in.Tag, in.Listen, in.Port = "mtp1", "127.0.0.1", port
	in.Settings.Secret = "ee" + hex.EncodeToString(key) + hex.EncodeToString([]byte("www.google.com"))
	s, err := startMTP(in, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	// A visitor that is not a Telegram client is fronted to the cover site.
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", "127.0.0.1:"+strconv.Itoa(port), &tls.Config{ServerName: "www.google.com"})
	if err != nil {
		t.Skipf("fronting needs internet: %v", err)
	}
	defer c.Close()
	if cn := c.ConnectionState().PeerCertificates[0].Subject.CommonName; cn == "" {
		t.Fatal("no cover certificate")
	} else {
		t.Log("cover cert:", cn)
	}
}
