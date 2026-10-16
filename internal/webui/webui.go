// Package webui serves the UI built with this HUB, not a separate application.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// The tracked README keeps ordinary Go checks buildable without Node.js.
// Release builds populate generated assets before compiling this package.
//
//go:embed dist
var bundled embed.FS

func Embedded() http.Handler {
	root, err := fs.Sub(bundled, "dist")
	if err != nil {
		panic(err)
	}
	return Handler(root)
}
func Directory(name string) http.Handler { return Handler(os.DirFS(name)) }

// Only extensionless application routes use the SPA document. Missing assets
// are real 404s; no directory listing, source map or private path is served.
func Handler(root fs.FS) http.Handler {
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", 405)
			return
		}
		raw := strings.TrimPrefix(r.URL.Path, "/")
		if raw == "" || (fs.ValidPath(raw) && raw != "assets" && path.Ext(raw) == "" && !strings.HasPrefix(raw, "assets/")) {
			body, err := fs.ReadFile(root, "index.html")
			if err != nil {
				http.Error(w, "Frontend not bundled; use the reviewed release builder", 503)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if r.Method == http.MethodGet {
				_, _ = w.Write(body)
			}
			return
		}
		if !fs.ValidPath(raw) || !(raw == "index.html" || strings.HasPrefix(raw, "assets/")) || strings.HasSuffix(raw, ".map") {
			http.NotFound(w, r)
			return
		}
		info, err := fs.Stat(root, raw)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
