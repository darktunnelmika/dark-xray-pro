package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// serveACME answers one ACME HTTP-01 challenge on addr for a few minutes.
// It returns as soon as the port is open so the HUB can ask the CA to
// validate; everything except the challenge path gets 404.
func serveACME(raw json.RawMessage, addr string) (any, error) {
	var a struct {
		Path    string `json:"path"`
		Body    string `json:"body"`
		Seconds int    `json:"seconds"`
	}
	if err := json.Unmarshal(raw, &a); err != nil || !strings.HasPrefix(a.Path, "/.well-known/acme-challenge/") || a.Body == "" {
		return nil, errors.New("bad acme arguments")
	}
	if a.Seconds <= 0 || a.Seconds > 600 {
		a.Seconds = 240
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != a.Path {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(a.Body))
	})}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		time.Sleep(time.Duration(a.Seconds) * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	return map[string]any{"listening": ln.Addr().String(), "seconds": a.Seconds}, nil
}
