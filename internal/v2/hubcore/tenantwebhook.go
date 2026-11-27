package hubcore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Each connected shop may have its own webhook: an address and a secret.
// It receives only its own clients' events — blocked:expired|volume|ips…,
// reset:*, next-plan and device-refused — the same way as the panel's
// webhook: up to 50 per POST as {"source","tenant","events":[...]}, signed
// with HMAC-SHA256 of the body in X-Dark-Signature: sha256=<hex>, and the
// cursor moves only after a 2xx answer (after 20 failed tries a batch is
// skipped and noted). Client names are the shop's own.

// TenantWebhook is a tenant's webhook settings and state (no secret).
type TenantWebhook struct {
	Enabled   bool   `json:"enabled"`
	URL       string `json:"url"`
	HasSecret bool   `json:"has_secret"`
	Secret    string `json:"secret,omitempty"` // only on input: "" keeps it, "-" removes it
	LastOK    int64  `json:"last_ok"`
	LastTry   int64  `json:"last_try"`
	LastError string `json:"last_error"`
}

// TenantWebhookItem is one event sent to a shop.
type TenantWebhookItem struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"` // client
	Unix   int64  `json:"unix"`
	Event  string `json:"event"`
	Client string `json:"client"`
	Node   string `json:"node,omitempty"`
}

// tenantEventKinds is the SQL condition for the events a shop receives.
const tenantEventKinds = `(kind LIKE 'blocked:%' OR kind LIKE 'reset:%' OR kind IN ('next-plan','device-refused'))`

// TenantWebhookOf reads a tenant's webhook.
func (s *Store) TenantWebhookOf(ctx context.Context, tenant string) (TenantWebhook, error) {
	var w TenantWebhook
	var secret string
	err := s.ro.QueryRowContext(ctx, `SELECT webhook_enabled,webhook_url,webhook_secret,webhook_last_ok,webhook_last_try,webhook_last_error FROM tenants WHERE id=?`, tenant).
		Scan(&w.Enabled, &w.URL, &secret, &w.LastOK, &w.LastTry, &w.LastError)
	if err != nil {
		return w, ErrNotFound
	}
	w.HasSecret = secret != ""
	return w, nil
}

// PutTenantWebhook saves a tenant's webhook. Turning it on starts from now,
// so it does not replay old events.
func (s *Store) PutTenantWebhook(ctx context.Context, tenant string, in TenantWebhook) (TenantWebhook, error) {
	in.URL = strings.TrimSpace(in.URL)
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return TenantWebhook{}, fmt.Errorf("%w: the webhook address must be an http(s) URL", ErrInvalid)
		}
	}
	if in.Enabled && in.URL == "" {
		return TenantWebhook{}, fmt.Errorf("%w: enter the address to send to", ErrInvalid)
	}
	if len(in.Secret) > 200 {
		return TenantWebhook{}, fmt.Errorf("%w: the secret is too long", ErrInvalid)
	}
	var wasOn bool
	var secret string
	if s.ro.QueryRowContext(ctx, `SELECT webhook_enabled,webhook_secret FROM tenants WHERE id=?`, tenant).Scan(&wasOn, &secret) != nil {
		return TenantWebhook{}, ErrNotFound
	}
	switch in.Secret {
	case "":
	case "-":
		secret = ""
	default:
		secret = in.Secret
	}
	q := `UPDATE tenants SET webhook_enabled=?, webhook_url=?, webhook_secret=? WHERE id=?`
	args := []any{in.Enabled, in.URL, secret, tenant}
	if in.Enabled && !wasOn {
		q = `UPDATE tenants SET webhook_enabled=?, webhook_url=?, webhook_secret=?, webhook_cursor=(SELECT COALESCE(MAX(id),0) FROM events), webhook_last_error='' WHERE id=?`
	}
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return TenantWebhook{}, err
	}
	return s.TenantWebhookOf(ctx, tenant)
}

// tenantWebhookBatch is the next batch for a tenant and the cursor it moves to.
func (s *Store) tenantWebhookBatch(ctx context.Context, tenant string, from int64) ([]TenantWebhookItem, int64, error) {
	var top int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM events`).Scan(&top)
	rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,node_id,email,kind FROM events WHERE tenant_id=? AND id>? AND id<=? AND `+tenantEventKinds+` ORDER BY id LIMIT 50`, tenant, from, top)
	if err != nil {
		return nil, from, err
	}
	defer rows.Close()
	var items []TenantWebhookItem
	to := from
	for rows.Next() {
		var it TenantWebhookItem
		var email string
		if err := rows.Scan(&it.ID, &it.Unix, &it.Node, &email, &it.Event); err != nil {
			return nil, from, err
		}
		it.Kind, it.Client, to = "client", tenantName(tenant, email), it.ID
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, from, err
	}
	if len(items) < 50 && top > to {
		to = top // nothing more of this shop up to top
	}
	return items, to, nil
}

func sendTenantWebhook(ctx context.Context, rawURL, secret, tenant string, items []TenantWebhookItem) error {
	body, _ := json.Marshal(map[string]any{"source": "dark-xray-pro", "tenant": tenant, "events": items})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DarkXrayPro-Webhook/1")
	if secret != "" {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write(body)
		req.Header.Set("X-Dark-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	resp, err := webhookClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("receiver answered %s", resp.Status)
	}
	return nil
}

// TestTenantWebhook sends one sample event to the saved address.
func (s *Store) TestTenantWebhook(ctx context.Context, tenant string) error {
	var u, secret string
	if s.ro.QueryRowContext(ctx, `SELECT webhook_url,webhook_secret FROM tenants WHERE id=?`, tenant).Scan(&u, &secret) != nil {
		return ErrNotFound
	}
	if u == "" {
		return fmt.Errorf("%w: save an address first", ErrInvalid)
	}
	if err := sendTenantWebhook(ctx, u, secret, tenant, []TenantWebhookItem{{Kind: "test", Unix: s.now().Unix(), Event: "test"}}); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

// tenantWebhookTick sends one pending batch per tenant. fails counts failed
// tries per tenant; next says when a failing tenant is tried again.
func (s *Store) tenantWebhookTick(ctx context.Context, fails map[string]int, next map[string]time.Time) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,webhook_url,webhook_secret,webhook_cursor FROM tenants WHERE enabled=1 AND webhook_enabled=1 AND webhook_url<>''`)
	if err != nil {
		return
	}
	type hook struct {
		id, url, secret string
		cursor          int64
	}
	var hooks []hook
	for rows.Next() {
		var h hook
		if rows.Scan(&h.id, &h.url, &h.secret, &h.cursor) == nil {
			hooks = append(hooks, h)
		}
	}
	rows.Close()
	now := s.now()
	for _, h := range hooks {
		if t, ok := next[h.id]; ok && now.Before(t) {
			continue
		}
		items, to, err := s.tenantWebhookBatch(ctx, h.id, h.cursor)
		if err != nil {
			log.Printf("tenant webhook %s: %v", h.id, err)
			continue
		}
		if len(items) == 0 {
			if to != h.cursor {
				_, _ = s.db.ExecContext(ctx, `UPDATE tenants SET webhook_cursor=? WHERE id=? AND webhook_cursor=?`, to, h.id, h.cursor)
			}
			continue
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE tenants SET webhook_last_try=? WHERE id=?`, now.Unix(), h.id)
		if err := sendTenantWebhook(ctx, h.url, h.secret, h.id, items); err != nil {
			fails[h.id]++
			next[h.id] = now.Add(time.Duration(min(fails[h.id], 12)) * 10 * time.Second)
			msg := err.Error()
			if fails[h.id] < 20 {
				_, _ = s.db.ExecContext(ctx, `UPDATE tenants SET webhook_last_error=? WHERE id=?`, msg, h.id)
				continue // the same batch next time
			}
			log.Printf("tenant webhook %s: giving up on %d items after %d tries: %v", h.id, len(items), fails[h.id], err)
			_, _ = s.db.ExecContext(ctx, `UPDATE tenants SET webhook_last_error=? WHERE id=?`,
				fmt.Sprintf("skipped %d items after %d failed tries: %v", len(items), fails[h.id], err), h.id)
		} else {
			_, _ = s.db.ExecContext(ctx, `UPDATE tenants SET webhook_last_ok=?, webhook_last_error='' WHERE id=?`, now.Unix(), h.id)
		}
		delete(fails, h.id)
		delete(next, h.id)
		// Only from where it was read: a change of settings in between wins.
		_, _ = s.db.ExecContext(ctx, `UPDATE tenants SET webhook_cursor=? WHERE id=? AND webhook_cursor=?`, to, h.id, h.cursor)
	}
}

// RunTenantWebhooks delivers the shops' webhooks every few seconds and
// prunes old deletion marks once a day, until ctx ends.
func (s *Store) RunTenantWebhooks(ctx context.Context) {
	fails, next := map[string]int{}, map[string]time.Time{}
	var pruned time.Time
	for {
		s.tenantWebhookTick(ctx, fails, next)
		if time.Since(pruned) > 24*time.Hour {
			s.pruneTombstones(ctx)
			pruned = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func tenantWebhookAdminRoutes(s *Store, mux *docMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	const p = "/api/v2/admin/tenants/{id}/webhook"
	mux.HandleFunc("GET "+p, guard(func(w http.ResponseWriter, r *http.Request) {
		h, err := s.TenantWebhookOf(r.Context(), r.PathValue("id"))
		reply(w, h, err)
	}))
	mux.HandleFunc("PUT "+p, guard(func(w http.ResponseWriter, r *http.Request) {
		var in TenantWebhook
		if !decode(w, r, &in) {
			return
		}
		h, err := s.PutTenantWebhook(r.Context(), r.PathValue("id"), in)
		reply(w, h, err)
	}))
	mux.HandleFunc("POST "+p+"/test", guard(func(w http.ResponseWriter, r *http.Request) {
		err := s.TestTenantWebhook(r.Context(), r.PathValue("id"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
}

// tenantWebhookRoutes let a shop set and test its own webhook with its key.
func tenantWebhookRoutes(s *Store, mux *docMux, auth func(func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc) {
	mux.HandleFunc("GET "+tenantAPI+"/webhook", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		h, err := s.TenantWebhookOf(r.Context(), tenant)
		if err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, http.StatusOK, h)
	}))
	mux.HandleFunc("PUT "+tenantAPI+"/webhook", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in TenantWebhook
		if !tenantDecode(w, r, 4096, &in) {
			return
		}
		h, err := s.PutTenantWebhook(r.Context(), tenant, in)
		if err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, http.StatusOK, h)
	}))
	mux.HandleFunc("POST "+tenantAPI+"/webhook/test", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		if err := s.TestTenantWebhook(r.Context(), tenant); err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
}
