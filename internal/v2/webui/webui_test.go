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
