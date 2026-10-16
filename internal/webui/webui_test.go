package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestApplicationRoutesUseSameBundledUIWithoutLeakingOtherFiles(t *testing.T) {
	root := fstest.MapFS{
		"index.html":        &fstest.MapFile{Data: []byte("<html>one UI</html>")},
		"assets/app.js":     &fstest.MapFile{Data: []byte("window.ready=true")},
		"assets/app.js.map": &fstest.MapFile{Data: []byte("private build map")},
		"private.env":       &fstest.MapFile{Data: []byte("private-test-data")},
	}
	h := Handler(root)
	for _, path := range []string{"/", "/login", "/clients", "/groups", "/representatives", "/account"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != "<html>one UI</html>" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/private.env", "/assets/missing.js", "/assets/", "/assets/app.js.map", "/../private.env"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), "private-test-data") {
			t.Fatal("leaked file or fake SPA", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/app.js", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "window.ready") {
		t.Fatal("asset unavailable")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", "/login", nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD body")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/login", nil))
	if w.Code != 405 {
		t.Fatal("static mutation allowed")
	}
	w = httptest.NewRecorder()
	Handler(fstest.MapFS{}).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal("missing compiled UI pretended ready")
	}
}
