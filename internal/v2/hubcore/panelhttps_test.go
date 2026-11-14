package hubcore

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"
)

func TestPanelHTTPS(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	for _, c := range [][3]string{{"*.example.com", "panel.example.com", "y"}, {"*.example.com", "a.b.example.com", ""}, {"example.com", "EXAMPLE.com", "y"}, {"*.example.com", "example.com", ""}} {
		if nameMatches(c[0], c[1]) != (c[2] == "y") {
			t.Fatal(c)
		}
	}
	if err := s.PutPanelHTTPS(ctx, PanelHTTPS{Port: 2096, Domain: "1.2.3.4"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("IP as domain")
	}
	if err := s.PutPanelHTTPS(ctx, PanelHTTPS{Port: 41001}); !errors.Is(err, ErrInvalid) {
		t.Fatal("reserved port")
	}
	if err := s.PutPanelHTTPS(ctx, PanelHTTPS{Port: 2096, Domain: "Panel.Example.com."}); err != nil {
		t.Fatal(err)
	}
	if st := s.PanelHTTPSStatus(ctx); st.CertID != "" || st.URL != "https://panel.example.com:2096/" {
		t.Fatalf("%+v", st)
	}
	cert, key := testCert(t, "*.example.com", now.Add(80*24*time.Hour))
	if err := s.PutCert(ctx, "wild", cert, key); err != nil {
		t.Fatal(err)
	}
	if st := s.PanelHTTPSStatus(ctx); st.CertID != "wild" || st.DaysLeft != 80 {
		t.Fatalf("%+v", st)
	}
	fbCert, fbKey := testCert(t, "hub.local", now.Add(80*24*time.Hour))
	fb, _ := tls.X509KeyPair([]byte(fbCert), []byte(fbKey))
	get := s.PanelCertificate(fb)
	c, err := get(&tls.ClientHelloInfo{ServerName: "panel.example.com"})
	if err != nil || c == nil || bytes.Equal(c.Certificate[0], fb.Certificate[0]) {
		t.Fatal(err)
	}
	if c2, _ := get(&tls.ClientHelloInfo{ServerName: "panel.example.com"}); c2 != c {
		t.Fatal("certificate should be cached")
	}
	if c3, _ := get(&tls.ClientHelloInfo{ServerName: "other.org"}); !bytes.Equal(c3.Certificate[0], fb.Certificate[0]) {
		t.Fatal("unknown name must get the fallback")
	}
}
