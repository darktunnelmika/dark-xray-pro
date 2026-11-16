package hubcore

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGzipJSON(t *testing.T) {
	h := gzipJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG"))
			return
		}
		if r.URL.Path == "/err" {
			http.Error(w, "nope", 404)
			return
		}
		reply(w, map[string]int{"a": 1}, nil)
	}))
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("json not gzipped: %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]int
	if b, _ := io.ReadAll(zr); json.Unmarshal(b, &m) != nil || m["a"] != 1 {
		t.Fatalf("bad body")
	}
	for _, p := range []string{"/png", "/err"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s should not be gzipped", p)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() == "" {
		t.Fatal("plain client got gzip")
	}
}
