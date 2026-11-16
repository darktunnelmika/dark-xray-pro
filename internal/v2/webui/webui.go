// Package webui is the v2 owner panel: a dependency-free single page served
// from the HUB binary.
package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".woff", "font/woff")
}

//go:embed static
var files embed.FS

// asset is one embedded file, prepared once: its ETag and, for text, a
// gzip copy (the panel script shrinks from ~370 KB to under 100 KB).
type asset struct {
	body, gz []byte
	etag     string
	ctype    string
}

func load(sub fs.FS) map[string]*asset {
	out := map[string]*asset{}
	_ = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		a := &asset{body: b, etag: `"` + hex.EncodeToString(sum[:8]) + `"`, ctype: mime.TypeByExtension(path.Ext(p))}
		if a.ctype == "" {
			a.ctype = http.DetectContentType(b)
		}
		if strings.HasPrefix(a.ctype, "text/") || strings.Contains(a.ctype, "javascript") || strings.Contains(a.ctype, "json") || strings.Contains(a.ctype, "svg") || strings.HasSuffix(p, ".webmanifest") {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(b)
			_ = zw.Close()
			if buf.Len() < len(b) {
				a.gz = buf.Bytes()
			}
		}
		out[p] = a
		return nil
	})
	return out
}

// Handler serves the panel files.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	assets := load(sub)
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self' 'sha256-n+aDuX5+DGbK82aepW7mvwcqwieMmw9XOltshzTrdLE='; frame-ancestors 'none'")
		// Always revalidate (a new HUB version shows at once), but an
		// unchanged file costs only a 304.
		w.Header().Set("Cache-Control", "no-cache")
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		a, ok := assets[p]
		if !ok || (r.Method != http.MethodGet && r.Method != http.MethodHead) || p == "index.html" && r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "index.html") {
			fileServer.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("ETag", a.etag)
		h.Set("Vary", "Accept-Encoding")
		h.Set("Content-Type", a.ctype)
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, a.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := a.body
		if a.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
			body = a.gz
		}
		h.Set("Content-Length", itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
