package hubcore

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// gzipJSON compresses JSON answers for clients that accept gzip. The client
// list alone is ~400 KB raw and ~40 KB compressed, which matters on slow
// mobile links in Iran. Other content types pass through untouched (the UI
// assets carry their own precompressed copies).
func gzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

var gzPool = sync.Pool{New: func() any { z, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed); return z }}

type gzWriter struct {
	http.ResponseWriter
	decided bool
	zw      *gzip.Writer
}

func (w *gzWriter) decide(code int) {
	if w.decided {
		return
	}
	w.decided = true
	h := w.Header()
	h.Add("Vary", "Accept-Encoding")
	if code == http.StatusOK && h.Get("Content-Encoding") == "" && strings.Contains(h.Get("Content-Type"), "json") {
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		z := gzPool.Get().(*gzip.Writer)
		z.Reset(w.ResponseWriter)
		w.zw = z
	}
}

func (w *gzWriter) WriteHeader(code int) {
	w.decide(code)
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzWriter) Write(b []byte) (int, error) {
	if !w.decided {
		w.WriteHeader(http.StatusOK)
	}
	if w.zw != nil {
		return w.zw.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzWriter) Flush() {
	if w.zw != nil {
		_ = w.zw.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzWriter) close() {
	if w.zw != nil {
		_ = w.zw.Close()
		gzPool.Put(w.zw)
		w.zw = nil
	}
}
