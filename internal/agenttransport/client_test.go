package agenttransport

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func labTLS(t *testing.T, handler http.Handler) (*httptest.Server, []byte, string) {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	der := s.Certificate().Raw
	sum := sha256.Sum256(der)
	pemData := pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE", Bytes: der})
	return s, pemData, hex.EncodeToString(sum[:])
}

func TestVerifiedTLS13AndCertificatePin(t *testing.T) {
	server, ca, pin := labTLS(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 {
			t.Error("insecure incoming Agent transport")
		}
		if r.URL.Path != "/api/v1/agent/health" { t.Errorf("wrong path: %s", r.URL.Path) }
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, endpoint, err := New(Config{Endpoint:server.URL,PinSHA256:pin,CABundle:ca})
	if err != nil { t.Fatal(err) }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Probe(ctx, client, endpoint);err != nil { t.Fatal(err) }
	client.CloseIdleConnections()

	wrong := strings.Repeat("0",64)
	if wrong == pin {wrong = strings.Repeat("1",64)}
	bad, uri, err := New(Config{Endpoint:server.URL,PinSHA256:wrong,CABundle:ca})
	if err != nil {t.Fatal(err)}
	if err := Probe(ctx,bad,uri); !errors.Is(err,ErrCertificatePin) {
		t.Fatalf("mismatched certificate pin accepted: %v",err)
	}
	bad.CloseIdleConnections()
}

func TestPlaintextRemoteAndUnpinnedTLSRefused(t *testing.T) {
	valid := []Config{{Endpoint:"http://127.0.0.1:2087"}}
	for _, cfg := range valid {
		cli,_,err:=New(cfg)
		if err != nil { t.Fatalf("valid loopback rejected: %v",err) }
		cli.CloseIdleConnections()
	}
	invalid := []Config{
		{Endpoint:"http://127.0.0.1:2088"},
		{Endpoint:"http://localhost:2087"},
		{Endpoint:"http://example.net:2087"},
		{Endpoint:"https://example.net:8443"},
		{Endpoint:"https://example.net"},
		{Endpoint:"https://user:pass@example.net:8443",PinSHA256:strings.Repeat("a",64)},
		{Endpoint:"https://example.net:8443/path",PinSHA256:strings.Repeat("a",64)},
		{Endpoint:"https://example.net:8443?token=x",PinSHA256:strings.Repeat("a",64)},
		{Endpoint:"https://example.net:8443#fragment",PinSHA256:strings.Repeat("a",64)},
		{Endpoint:"https://example.net:8443",PinSHA256:"nope"},
		{Endpoint:"ftp://example.net:8443",PinSHA256:strings.Repeat("a",64)},
	}
	for _, cfg := range invalid {
		if c,_,err:=New(cfg);err==nil {
			c.CloseIdleConnections()
			t.Fatalf("unsafe endpoint accepted: %+v",cfg)
		}
	}
}

func TestUntrustedCertificateRefusedEvenWithCorrectPin(t *testing.T) {
	s, _, pin := labTLS(t,http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request){
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()
	c,uri,err:=New(Config{Endpoint:s.URL,PinSHA256:pin})
	if err!=nil {t.Fatal(err)}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	if err:=Probe(ctx,c,uri);err==nil {
		t.Fatal("pin alone bypassed the public certificate trust chain")
	}
	c.CloseIdleConnections()
}

func TestTLS12AndRedirectRefused(t *testing.T) {
	s:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request){
		http.Redirect(w,&http.Request{},"https://example.com/leak",http.StatusTemporaryRedirect)
	}))
	s.TLS=&tls.Config{MinVersion:tls.VersionTLS12,MaxVersion:tls.VersionTLS12}
	s.StartTLS()
	defer s.Close()
	sum:=sha256.Sum256(s.Certificate().Raw)
	pin:=hex.EncodeToString(sum[:])
	ca:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:s.Certificate().Raw})
	c,uri,err:=New(Config{Endpoint:s.URL,PinSHA256:pin,CABundle:ca})
	if err!=nil {t.Fatal(err)}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	if err:=Probe(ctx,c,uri);err==nil {t.Fatal("TLS 1.2 accepted")}
	c.CloseIdleConnections()
	// Redirect refusal is independently checked on an eligible TLS1.3 server.
	redirect,root,p:=labTLS(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		http.Redirect(w,r,"https://example.com/leak",http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client,endpoint,err:=New(Config{Endpoint:redirect.URL,PinSHA256:p,CABundle:root})
	if err!=nil {t.Fatal(err)}
	if err:=Probe(ctx,client,endpoint);err==nil || !strings.Contains(err.Error(),"HTTP 307") {
		t.Fatalf("followed Agent redirect or reported success: %v",err)
	}
	client.CloseIdleConnections()
}
