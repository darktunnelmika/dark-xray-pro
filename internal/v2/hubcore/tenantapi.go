package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// The connected shop API, under /api/tenant/v1/. Every call carries the
// shop's key (Authorization: Bearer dt_… or X-API-Key: dt_…) and works only
// on that shop's clients. Answers are JSON; errors are
// {"error": "...", "code": "invalid|not_found|conflict|policy|unauthorized|unavailable"}.
// Every change is written to the panel's audit log as "tenant:<id>".

const tenantAPI = "/api/tenant/v1"

func tenantJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func tenantErr(w http.ResponseWriter, code int, kind, msg string) {
	tenantJSON(w, code, map[string]string{"error": msg, "code": kind})
}

// tenantFail maps a store error to an answer.
func tenantFail(w http.ResponseWriter, err error) {
	msg := strings.TrimPrefix(err.Error(), "invalid: ")
	switch {
	case errors.Is(err, ErrNotFound):
		tenantErr(w, http.StatusNotFound, "not_found", "client not found")
	case errors.Is(err, ErrConflict):
		tenantErr(w, http.StatusConflict, "conflict", strings.TrimPrefix(msg, "conflict: "))
	case errors.Is(err, ErrPolicy):
		tenantErr(w, http.StatusForbidden, "policy", strings.TrimPrefix(msg, "not allowed by policy: "))
	case errors.Is(err, ErrInvalid):
		tenantErr(w, http.StatusUnprocessableEntity, "invalid", msg)
	case errors.Is(err, ErrAuth):
		tenantErr(w, http.StatusUnauthorized, "unauthorized", "invalid key")
	default:
		tenantErr(w, http.StatusServiceUnavailable, "unavailable", "temporarily unavailable")
	}
}

// tenantDecode reads a JSON body (up to max bytes).
func tenantDecode(w http.ResponseWriter, r *http.Request, max int64, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, max))
	if err := dec.Decode(v); err != nil {
		tenantErr(w, http.StatusUnprocessableEntity, "invalid", "the body must be valid JSON: "+err.Error())
		return false
	}
	return true
}

// TenantInfo is what a shop reads about itself.
type TenantInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Inbounds   []string `json:"inbounds"`
	MaxClients int      `json:"max_clients"`
	MaxBytes   int64    `json:"max_bytes"`
	Clients    int      `json:"clients"`
	Volume     int64    `json:"volume"`
	Used       int64    `json:"used"`     // carried by this panel since the clients came
	Imported   int64    `json:"imported"` // brought from the panel they came from
	Online     int      `json:"online"`
	Rev        int64    `json:"rev"` // the panel's current revision (a starting cursor)
}

func (s *Store) tenantInfo(ctx context.Context, id string) (TenantInfo, error) {
	all, err := s.Tenants(ctx)
	if err != nil {
		return TenantInfo{}, err
	}
	for _, t := range all {
		if t.ID == id {
			info := TenantInfo{ID: t.ID, Name: t.Name, Enabled: t.Enabled, Inbounds: s.TenantInbounds(ctx, t.Tenant),
				MaxClients: t.MaxClients, MaxBytes: t.MaxBytes, Clients: t.Clients, Volume: t.Volume, Used: t.Used, Imported: t.Imported, Online: t.Online}
			_ = s.ro.QueryRowContext(ctx, `SELECT v FROM rev_seq WHERE id=1`).Scan(&info.Rev)
			return info, nil
		}
	}
	return TenantInfo{}, ErrNotFound
}

// tenantRoutes serves the connected shop API.
func tenantRoutes(s *Store, mux *docMux, subBase string) {
	auth := func(h func(w http.ResponseWriter, r *http.Request, tenant string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			tenant, key, why := s.tenantAuth(r)
			if tenant == "" {
				w.Header().Set("WWW-Authenticate", "Bearer")
				tenantErr(w, http.StatusUnauthorized, "unauthorized", why)
				return
			}
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == tenantAPI+"/bulk/links" { // reads: not in the audit log
				h(w, r, tenant)
				return
			}
			sw := &statusWriter{ResponseWriter: w}
			h(sw, r, tenant)
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			s.Audit(r.Context(), Principal{User: "tenant:" + tenant + " (key " + key + ")", Role: "tenant"}, r, status)
		}
	}
	base := func(r *http.Request, tenant string) string {
		if b := s.TenantBrand(r.Context(), tenant).BaseURL; b != "" {
			return b
		}
		return subBase
	}
	withURL := func(r *http.Request, tenant string, c TenantClient) TenantClient {
		c.SubURL = subURL(base(r, tenant), r, c.SubToken)
		return c
	}
	one := func(w http.ResponseWriter, r *http.Request, tenant string, c TenantClient, err error, code int) {
		if err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, code, withURL(r, tenant, c))
	}
	name := func(r *http.Request) string { return r.PathValue("name") }

	mux.HandleFunc("GET "+tenantAPI+"/me", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		info, err := s.tenantInfo(r.Context(), tenant)
		if err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, http.StatusOK, info)
	}))
	mux.HandleFunc("GET "+tenantAPI+"/clients", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		q := r.URL.Query()
		if ref := q.Get("external_ref"); ref != "" {
			c, err := s.TenantClientByRef(r.Context(), tenant, ref)
			if err != nil {
				tenantFail(w, err)
				return
			}
			tenantJSON(w, http.StatusOK, map[string]any{"clients": []TenantClient{withURL(r, tenant, c)}, "next": ""})
			return
		}
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		list, err := s.TenantClientPage(r.Context(), tenant, q.Get("after"), limit)
		if err != nil {
			tenantFail(w, err)
			return
		}
		for i := range list {
			list[i] = withURL(r, tenant, list[i])
		}
		next := ""
		if len(list) == limit {
			next = list[len(list)-1].Name
		}
		tenantJSON(w, http.StatusOK, map[string]any{"clients": list, "next": next})
	}))
	mux.HandleFunc("POST "+tenantAPI+"/clients", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in TenantClientIn
		if !tenantDecode(w, r, 1<<16, &in) {
			return
		}
		c, err := s.TenantCreate(r.Context(), tenant, in)
		one(w, r, tenant, c, err, http.StatusCreated)
	}))
	mux.HandleFunc("GET "+tenantAPI+"/clients/{name}", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		c, err := s.TenantClient(r.Context(), tenant, name(r))
		one(w, r, tenant, c, err, http.StatusOK)
	}))
	update := auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in TenantClientIn
		if !tenantDecode(w, r, 1<<16, &in) {
			return
		}
		c, err := s.TenantUpdate(r.Context(), tenant, name(r), in)
		one(w, r, tenant, c, err, http.StatusOK)
	})
	mux.HandleFunc("PATCH "+tenantAPI+"/clients/{name}", update)
	mux.HandleFunc("PUT "+tenantAPI+"/clients/{name}", update)
	mux.HandleFunc("DELETE "+tenantAPI+"/clients/{name}", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		if err := s.TenantDelete(r.Context(), tenant, name(r)); err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}))
	for _, st := range []struct {
		path string
		on   bool
	}{{"enable", true}, {"disable", false}} {
		on := st.on
		mux.HandleFunc("POST "+tenantAPI+"/clients/{name}/"+st.path, auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
			c, err := s.TenantUpdate(r.Context(), tenant, name(r), TenantClientIn{Enabled: &on})
			one(w, r, tenant, c, err, http.StatusOK)
		}))
	}
	mux.HandleFunc("POST "+tenantAPI+"/clients/{name}/reset", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		c, err := s.TenantResetUsage(r.Context(), tenant, name(r))
		one(w, r, tenant, c, err, http.StatusOK)
	}))
	mux.HandleFunc("POST "+tenantAPI+"/clients/{name}/renew", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in TenantRenewIn
		if !tenantDecode(w, r, 4096, &in) {
			return
		}
		c, err := s.TenantRenew(r.Context(), tenant, name(r), in)
		one(w, r, tenant, c, err, http.StatusOK)
	}))
	mux.HandleFunc("POST "+tenantAPI+"/clients/{name}/rotate", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		in := struct {
			Link        *bool `json:"link"`
			Credentials bool  `json:"credentials"`
		}{}
		if r.ContentLength != 0 && !tenantDecode(w, r, 4096, &in) {
			return
		}
		link := in.Link == nil || *in.Link
		c, err := s.TenantRotate(r.Context(), tenant, name(r), in.Credentials, link)
		one(w, r, tenant, c, err, http.StatusOK)
	}))
	mux.HandleFunc("GET "+tenantAPI+"/clients/{name}/links", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		c, err := s.TenantClient(r.Context(), tenant, name(r))
		if err != nil {
			tenantFail(w, err)
			return
		}
		links, err := s.ClientLinks(r.Context(), tenantEmail(tenant, c.Name))
		if err != nil {
			tenantFail(w, err)
			return
		}
		if links == nil {
			links = []ClientLink{}
		}
		u := subURL(base(r, tenant), r, c.SubToken)
		tenantJSON(w, http.StatusOK, map[string]any{"subscription_url": u, "formats": map[string]string{
			"base64": u, "singbox": u + "?format=singbox", "clash": u + "?format=clash", "xray": u + "?format=xray"}, "links": links})
	}))
	tenantBulkRoutes(s, mux, auth, base)
	tenantUsageRoutes(s, mux, auth)
	tenantWebhookRoutes(s, mux, auth)
}

// tenantAdminRoutes lets the owner manage tenants, their keys, look and
// webhook (owner-only paths, see ownerOnly).
func tenantAdminRoutes(s *Store, mux *docMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	const p = "/api/v2/admin/tenants"
	mux.HandleFunc("GET "+p, guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Tenants(r.Context())
		reply(w, list, err)
	}))
	mux.HandleFunc("PUT "+p+"/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		var t Tenant
		if !decode(w, r, &t) {
			return
		}
		t.ID = r.PathValue("id")
		if err := s.PutTenant(r.Context(), t); err != nil {
			reply(w, nil, err)
			return
		}
		got, err := s.TenantByID(r.Context(), t.ID)
		reply(w, got, err)
	}))
	mux.HandleFunc("DELETE "+p+"/{id}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteTenant(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET "+p+"/{id}/traffic", guard(func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		list, err := s.TenantNodeTraffic(r.Context(), r.PathValue("id"), days)
		reply(w, list, err)
	}))
	mux.HandleFunc("GET "+p+"/{id}/keys", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.TenantKeys(r.Context(), r.PathValue("id"))
		reply(w, list, err)
	}))
	mux.HandleFunc("POST "+p+"/{id}/keys", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name     string   `json:"name"`
			AllowIPs []string `json:"allow_ips"`
			Days     int      `json:"days"`
		}
		if !decode(w, r, &in) {
			return
		}
		key, k, err := s.CreateTenantKey(r.Context(), r.PathValue("id"), in.Name, in.AllowIPs, in.Days)
		reply(w, map[string]any{"key": key, "info": k}, err)
	}))
	mux.HandleFunc("DELETE "+p+"/{id}/keys/{key}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteTenantKey(r.Context(), r.PathValue("id"), r.PathValue("key"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("GET "+p+"/{id}/brand", guard(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.TenantByID(r.Context(), r.PathValue("id")); err != nil {
			reply(w, nil, err)
			return
		}
		reply(w, s.TenantBrand(r.Context(), r.PathValue("id")), nil)
	}))
	mux.HandleFunc("PUT "+p+"/{id}/brand", guard(func(w http.ResponseWriter, r *http.Request) {
		var b RepBrand
		if !decode(w, r, &b) {
			return
		}
		err := s.PutTenantBrand(r.Context(), r.PathValue("id"), b)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	tenantWebhookAdminRoutes(s, mux, guard)
	tenantMoveRoutes(s, mux, guard)
	// The shop's own subscription domains (tenantsubdomain.go).
	mux.HandleFunc("GET "+p+"/{id}/sub-domains", guard(func(w http.ResponseWriter, r *http.Request) {
		list, err := s.TenantSubDomains(r.Context(), r.PathValue("id"))
		reply(w, map[string]any{"domains": list, "hub_ips": HubIPs()}, err)
	}))
	mux.HandleFunc("PUT "+p+"/{id}/sub-domains/{domain}", guard(func(w http.ResponseWriter, r *http.Request) {
		var d TenantSubDomain
		if !decode(w, r, &d) {
			return
		}
		d.Domain = r.PathValue("domain")
		err := s.PutTenantSubDomain(r.Context(), r.PathValue("id"), d)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("DELETE "+p+"/{id}/sub-domains/{domain}", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.DeleteTenantSubDomain(r.Context(), r.PathValue("id"), r.PathValue("domain"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST "+p+"/{id}/sub-domains/{domain}/check", guard(func(w http.ResponseWriter, r *http.Request) {
		ok, why, err := s.CheckTenantSubDomain(r.Context(), r.PathValue("id"), r.PathValue("domain"))
		reply(w, map[string]any{"ok": ok, "reason": why}, err)
	}))
	mux.HandleFunc("GET "+p+"/{id}/sub-fetches", guard(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		rep, err := s.TenantSubFetches(r.Context(), r.PathValue("id"), since, q.Get("list"), q.Get("after"), limit)
		reply(w, rep, err)
	}))
}
