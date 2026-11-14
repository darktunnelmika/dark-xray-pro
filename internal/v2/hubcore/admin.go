package hubcore

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// AdminHandler serves the owner API under /api/v2/admin/ and public
// subscriptions under /sub/<token>. adminToken guards the owner API until the
// panel's session login is attached in front of it.
func AdminHandler(s *Store, adminToken string) http.Handler {
	mux := http.NewServeMux()
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			tok, err := bearer(r.Header.Get("Authorization"))
			if err != nil || adminToken == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(adminToken)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/v2/admin/nodes", guard(func(w http.ResponseWriter, r *http.Request) {
		nodes, err := s.Nodes(r.Context())
		type view struct {
			NodeStatus
			Online bool `json:"online"`
		}
		out := make([]view, 0, len(nodes))
		now := time.Now()
		for _, n := range nodes {
			out = append(out, view{n, n.Online(now)})
		}
		reply(w, out, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID, Name string }
		if !decode(w, r, &in) {
			return
		}
		tok, err := s.CreateNode(r.Context(), in.ID, in.Name)
		reply(w, map[string]string{"id": in.ID, "token": tok}, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/inbounds/{tag}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			NodeID   string          `json:"node_id"`
			Template json.RawMessage `json:"template"`
			Meta     json.RawMessage `json:"meta"`
			Enabled  *bool           `json:"enabled"`
		}
		if !decode(w, r, &in) {
			return
		}
		enabled := in.Enabled == nil || *in.Enabled
		err := s.PutInboundMeta(r.Context(), r.PathValue("tag"), in.NodeID, in.Template, in.Meta, enabled)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Clients(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}", guard(func(w http.ResponseWriter, r *http.Request) {
		var c Client
		if !decode(w, r, &c) {
			return
		}
		c.Email = r.PathValue("email")
		err := s.PutClient(r.Context(), c)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/clients/{email}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteClient(r.Context(), r.PathValue("email"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.Handle("GET /sub/{token}", SubHandler(s))
	return mux
}

func b64(s string) string { return sub.Base64([]string{s}) }

// SubHandler serves only public subscriptions, so it can listen on a public
// address without exposing the owner API.
func SubHandler(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sub/{token}", func(w http.ResponseWriter, r *http.Request) {
		email, err := s.EmailBySubToken(r.Context(), r.PathValue("token"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		links, info, err := s.Subscription(r.Context(), email)
		if err != nil {
			reply(w, nil, err)
			return
		}
		w.Header().Set("Subscription-Userinfo", info)
		w.Header().Set("Profile-Update-Interval", "12")
		w.Header().Set("Profile-Title", "base64:"+b64(email))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if strings.Contains(r.URL.RawQuery, "raw") {
			_, _ = w.Write([]byte(strings.Join(links, "\n")))
			return
		}
		_, _ = w.Write([]byte(sub.Base64(links)))
	})
	return mux
}
