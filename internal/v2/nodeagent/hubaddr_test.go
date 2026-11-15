package nodeagent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetHub(t *testing.T) {
	dir := t.TempDir()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ca"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	caFile := filepath.Join(dir, "ca.pem")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"dark-hub2"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	task := SetHubTask(dir, caFile)
	bad, _ := json.Marshal(HubAddr{URL: "https://127.0.0.1:" + port, Name: "other-name"})
	if _, err := task(context.Background(), bad); err == nil {
		t.Fatal("a certificate for another name must be refused")
	}
	if _, ok := LoadHubAddr(dir); ok {
		t.Fatal("nothing may be saved after a failed check")
	}
	good, _ := json.Marshal(HubAddr{URL: "https://127.0.0.1:" + port + "/", Name: "dark-hub2"})
	res, err := task(context.Background(), good)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.(Restart); !ok {
		t.Fatal("the agent must restart onto the new address")
	}
	a, ok := LoadHubAddr(dir)
	if !ok || a.URL != "https://127.0.0.1:"+port || a.Name != "dark-hub2" {
		t.Fatalf("%+v", a)
	}
}
