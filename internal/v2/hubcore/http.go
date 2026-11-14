package hubcore

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// NodeHandler serves the Node-facing v2 API only. It shares no route with
// the owner panel, so a Node token can never reach owner data.
func NodeHandler(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+wire.PathSync, func(w http.ResponseWriter, r *http.Request) {
		id, ok := authNode(s, w, r)
		if !ok {
			return
		}
		var req wire.SyncRequest
		if !decode(w, r, &req) {
			return
		}
		s.NodeAddress(r.Context(), id, r.RemoteAddr)
		resp, err := s.Sync(r.Context(), id, req)
		reply(w, resp, err)
	})
	mux.HandleFunc("POST "+wire.PathUsage, func(w http.ResponseWriter, r *http.Request) {
		id, ok := authNode(s, w, r)
		if !ok {
			return
		}
		var b wire.UsageBatch
		if !decode(w, r, &b) {
			return
		}
		ack, err := s.ApplyUsage(r.Context(), id, b)
		reply(w, ack, err)
	})
	mux.HandleFunc("GET "+wire.PathDist+"{file}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authNode(s, w, r); !ok {
			return
		}
		p, ok := s.distFile(r.PathValue("file"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, p)
	})
	return mux
}

func authNode(s *Store, w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.Header.Get(wire.HeaderNodeID)
	tok, err := bearer(r.Header.Get("Authorization"))
	if err != nil || s.AuthNode(r.Context(), id, tok) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return "", false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 32<<20))
	if err := dec.Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any, err error) {
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	case errors.Is(err, ErrCredit):
		http.Error(w, "insufficient credit", http.StatusPaymentRequired)
	case errors.Is(err, ErrPolicy):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrAuth):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, ErrInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		log.Printf("v2 node api: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}
}
