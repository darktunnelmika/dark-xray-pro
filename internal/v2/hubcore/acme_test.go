package hubcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudflareClient(t *testing.T) {
	var added, deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.Write([]byte(`{"success":false,"errors":[{"message":"bad token"}]}`))
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/zones":
			if r.URL.Query().Get("name") == "example.com" {
				w.Write([]byte(`{"success":true,"result":[{"id":"z1"}]}`))
				return
			}
			w.Write([]byte(`{"success":true,"result":[]}`))
		case r.Method == "POST" && r.URL.Path == "/zones/z1/dns_records":
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			added = in["name"].(string) + "=" + in["content"].(string)
			w.Write([]byte(`{"success":true,"result":{"id":"r9"}}`))
		case r.Method == "DELETE" && r.URL.Path == "/zones/z1/dns_records/r9":
			deleted = "r9"
			w.Write([]byte(`{"success":true,"result":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	cf := &cloudflare{Token: "tok", Base: srv.URL}
	rec, err := cf.addTXT(ctx, "_acme-challenge.vpn.sub.example.com", "v")
	if err != nil || added != "_acme-challenge.vpn.sub.example.com=v" {
		t.Fatalf("add %v %q", err, added)
	}
	if err := cf.delete(ctx, rec); err != nil || deleted != "r9" {
		t.Fatalf("delete %v", err)
	}
	if _, err := (&cloudflare{Token: "tok", Base: srv.URL}).addTXT(ctx, "_acme-challenge.other.org", "v"); err == nil || !strings.Contains(err.Error(), "no Cloudflare zone") {
		t.Fatalf("missing zone: %v", err)
	}
	if _, err := (&cloudflare{Token: "nope", Base: srv.URL}).addTXT(ctx, "_acme-challenge.example.com", "v"); err == nil || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("bad token: %v", err)
	}
}

func TestACMEValidation(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.PutACMECert(ctx, "c1", []string{"vpn.example.com"}, ACMEHTTPNode, "de-1"); err == nil {
		t.Fatal("issued without an email")
	}
	if err := s.PutACMESettings(ctx, ACMESettings{Email: "me@example.com", Directory: "https://127.0.0.1:1/directory", CFToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	if a := s.ACMESettings(ctx); !a.CFSet || a.CFToken != "" {
		t.Fatalf("token must be write-only: %+v", a)
	}
	for _, bad := range []struct {
		d      []string
		method string
	}{
		{[]string{"*.example.com"}, ACMEHTTPNode},
		{[]string{"1.2.3.4"}, ACMEDNSCloudflare},
		{[]string{"bad_name.com"}, ACMEDNSCloudflare},
		{nil, ACMEDNSCloudflare},
		{[]string{"x.example.com"}, "ftp"},
	} {
		if err := s.PutACMECert(ctx, "c1", bad.d, bad.method, "de-1"); err == nil {
			t.Errorf("%v %s accepted", bad.d, bad.method)
		}
	}
	// A valid request is stored and its (unreachable) CA failure recorded.
	if err := s.PutACMECert(ctx, "c1", []string{"*.example.com", "example.com"}, ACMEDNSCloudflare, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		list, _ := s.ACMECerts(ctx)
		if len(list) == 1 && list[0].Status == "failed" {
			if list[0].LastError == "" || len(list[0].Domains) != 2 {
				t.Fatalf("%+v", list[0])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no failure recorded: %+v", list)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := s.PutACMESettings(ctx, ACMESettings{Email: "me@example.com", CFToken: "-"}); err != nil || s.ACMESettings(ctx).CFSet {
		t.Fatal("token not cleared")
	}
}
