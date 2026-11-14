package nodeagent

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestServeACME(t *testing.T) {
	if _, err := serveACME(json.RawMessage(`{"path":"/x","body":"b"}`), "127.0.0.1:0"); err == nil {
		t.Fatal("non-challenge path accepted")
	}
	v, err := serveACME(json.RawMessage(`{"path":"/.well-known/acme-challenge/tok","body":"tok.key","seconds":5}`), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := v.(map[string]any)["listening"].(string)
	resp, err := http.Get("http://" + addr + "/.well-known/acme-challenge/tok")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "tok.key" {
		t.Fatalf("body %q", b)
	}
	resp, _ = http.Get("http://" + addr + "/other")
	if resp.StatusCode != 404 {
		t.Fatalf("other path: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
