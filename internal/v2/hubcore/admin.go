package hubcore

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// AdminHandler serves the owner API under /api/v2/admin/ and public
// subscriptions under /sub/<token>. adminToken guards the owner API until the
// panel's session login is attached in front of it.
func AdminHandler(s *Store, adminToken string) http.Handler {
	return AdminHandlerWith(s, adminToken, "", nil)
}

// AdminHandlerWith also serves the panel UI at / and fills each client's
// public subscription URL from subBase (e.g. https://sub.example.com).
func AdminHandlerWith(s *Store, adminToken, subBase string, ui http.Handler, extra ...func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()
	for _, f := range extra {
		f(mux)
	}
	if ui != nil {
		mux.Handle("GET /", ui)
	}
	// member lets owners and representatives in; handlers read the caller
	// with who(r). guard is owner-only and is the default for everything else.
	member := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p, ok := authorize(s, adminToken, r)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
		}
	}
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return member(func(w http.ResponseWriter, r *http.Request) {
			if who(r).Role != "owner" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		})
	}
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		if !decode(w, r, &in) {
			return
		}
		tok, err := s.Login(r.Context(), in.Username, in.Password)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
			return
		}
		setSessionCookie(w, r, tok, int(sessionTTL.Seconds()))
		reply(w, map[string]string{"username": in.Username}, nil)
	})
	mux.HandleFunc("POST /api/v2/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			_ = s.Logout(r.Context(), c.Value)
		}
		setSessionCookie(w, r, "", -1)
		reply(w, map[string]bool{"ok": true}, nil)
	})
	mux.HandleFunc("GET /api/v2/auth/me", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		out := map[string]any{"user": p.User, "role": p.Role, "rep_id": p.RepID}
		if p.Role == "rep" {
			reps, _ := s.Reps(r.Context())
			for _, rp := range reps {
				if rp.ID == p.RepID {
					out["volume_credit"], out["unlimited_units"] = rp.VolumeCredit, rp.UnlimitedUnits
				}
			}
		}
		reply(w, out, nil)
	}))
	mux.HandleFunc("POST /api/v2/auth/password", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Old, New string }
		if !decode(w, r, &in) {
			return
		}
		err := s.ChangePassword(r.Context(), who(r).User, in.Old, in.New)
		if err == nil {
			setSessionCookie(w, r, "", -1)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/reps", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Reps(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name     string
			Enabled  bool
			Username string
			Password string
		}
		if !decode(w, r, &in) {
			return
		}
		id := r.PathValue("id")
		err := s.PutRep(r.Context(), Rep{ID: id, Name: in.Name, Enabled: in.Enabled})
		if err == nil && in.Username != "" && in.Password != "" {
			err = s.SetRepLogin(r.Context(), in.Username, in.Password, id)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/reps/{id}/credit", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			VolumeGB float64
			Units    int64
			Note     string
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.AdjustCredit(r.Context(), who(r).User, r.PathValue("id"), int64(in.VolumeGB*(1<<30)), in.Units, in.Note)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/overview", guard(func(w http.ResponseWriter, r *http.Request) {
		o, err := s.Overview(r.Context())
		reply(w, o, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/inbounds", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Inbounds(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/presets", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, Presets, nil)
	}))
	mux.HandleFunc("POST /api/v2/admin/inbounds/preset", guard(func(w http.ResponseWriter, r *http.Request) {
		var p PresetRequest
		if !decode(w, r, &p) {
			return
		}
		err := s.CreatePreset(r.Context(), p)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/inbounds/{tag}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteInbound(r.Context(), r.PathValue("tag"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
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
		tok, err := s.CreateNodeInstall(r.Context(), in.ID, in.Name)
		reply(w, map[string]string{"id": in.ID, "install": "curl -fsSL " + publicBase(subBase, r) + "/install/" + tok + " | bash"}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/nodes/{id}/install", guard(func(w http.ResponseWriter, r *http.Request) {
		tok, err := s.NewInstallToken(r.Context(), r.PathValue("id"))
		reply(w, map[string]string{"install": "curl -fsSL " + publicBase(subBase, r) + "/install/" + tok + " | bash"}, err)
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
	mux.HandleFunc("GET /api/v2/admin/clients", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.scopedClients(r.Context(), who(r))
		type view struct {
			ClientView
			SubURL string
		}
		out := make([]view, 0, len(list))
		for _, c := range list {
			out = append(out, view{c, subURL(subBase, r, c.SubToken)})
		}
		reply(w, out, err)
	}))
	mux.HandleFunc("GET /api/v2/admin/clients/{email}/qr.png", member(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.scopedClients(r.Context(), who(r))
		if err != nil {
			reply(w, nil, err)
			return
		}
		for _, c := range list {
			if c.Email == r.PathValue("email") {
				png, err := qrcode.Encode(subURL(subBase, r, c.SubToken), qrcode.Medium, 320)
				if err != nil {
					reply(w, nil, err)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(png)
				return
			}
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("PUT /api/v2/admin/clients/{email}", member(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Client
			Plan string
		}
		if !decode(w, r, &in) {
			return
		}
		c := in.Client
		c.Email = r.PathValue("email")
		p := who(r)
		var err error
		if p.Role == "rep" {
			c.UUID, c.Secret, c.SubToken = "", "", "" // representatives never set credentials
			err = s.RepPutClient(r.Context(), p.User, p.RepID, c, in.Plan)
		} else {
			err = s.PutClient(r.Context(), c)
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE /api/v2/admin/clients/{email}", member(func(w http.ResponseWriter, r *http.Request) {
		p := who(r)
		if p.Role == "rep" && !s.ownsClient(r.Context(), p.RepID, r.PathValue("email")) {
			http.NotFound(w, r)
			return
		}
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

func publicBase(base string, r *http.Request) string {
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return strings.TrimRight(base, "/")
}

func subURL(base string, r *http.Request, tok string) string {
	return publicBase(base, r) + "/sub/" + tok
}

type principalKey struct{}

func who(r *http.Request) Principal {
	p, _ := r.Context().Value(principalKey{}).(Principal)
	return p
}

// scopedClients: owners see everyone, a representative only its own.
func (s *Store) scopedClients(ctx context.Context, p Principal) ([]ClientView, error) {
	all, err := s.Clients(ctx)
	if err != nil || p.Role == "owner" {
		return all, err
	}
	out := all[:0]
	for _, c := range all {
		if c.RepID == p.RepID && p.RepID != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Store) ownsClient(ctx context.Context, repID, email string) bool {
	var got string
	return repID != "" && s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, email).Scan(&got) == nil && got == repID
}
