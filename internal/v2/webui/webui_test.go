package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestIndexInlineScriptMatchesCSP(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body, _ := io.ReadAll(rec.Body)
	m := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindSubmatch(body)
	if rec.Code != 200 || m == nil {
		t.Fatalf("index %d", rec.Code)
	}
	sum := sha256.Sum256(m[1])
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, want) {
		t.Fatalf("CSP %q lacks %s", csp, want)
	}
	for _, f := range []string{"/app.js", "/app.css"} {
		r := httptest.NewRecorder()
		Handler().ServeHTTP(r, httptest.NewRequest("GET", f, nil))
		if r.Code != 200 {
			t.Fatalf("%s: %d", f, r.Code)
		}
	}
}

func TestAssetsGzipAndRevalidate(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
	tag := r.Header().Get("ETag")
	if r.Code != 200 || r.Header().Get("Content-Encoding") != "gzip" || tag == "" || r.Body.Len() > 200_000 || !strings.Contains(r.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("app.js %d enc=%q etag=%q size=%d type=%q", r.Code, r.Header().Get("Content-Encoding"), tag, r.Body.Len(), r.Header().Get("Content-Type"))
	}
	again := httptest.NewRequest("GET", "/app.js", nil)
	again.Header.Set("If-None-Match", tag)
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, again)
	if r2.Code != 304 || r2.Body.Len() != 0 {
		t.Fatalf("revalidate %d", r2.Code)
	}
	plain := httptest.NewRecorder()
	h.ServeHTTP(plain, httptest.NewRequest("GET", "/app.css", nil))
	if plain.Header().Get("Content-Encoding") != "" || !strings.Contains(plain.Body.String(), "--cyan") {
		t.Fatal("plain css")
	}
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest("GET", "/nope.js", nil))
	if missing.Code != 404 {
		t.Fatal("missing file", missing.Code)
	}
}
