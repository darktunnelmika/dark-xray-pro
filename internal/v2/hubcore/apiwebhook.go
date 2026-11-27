package hubcore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Events and webhooks of the external API. Each target (a partner key,
// "partner:<key id>", or a reseller, "rep:<reseller id>") has its own event
// list, kept 30 days, which it can read (GET …/events?after=<id>) or have
// pushed to its webhook:
//
//	POST <url>
//	Content-Type: application/json
//	X-Dark-Timestamp: <unix seconds>
//	X-Dark-Signature: sha256=<hex HMAC-SHA256 of "<timestamp>.<body>" with the secret>
//	X-Dark-Delivery: <delivery id>
//
//	{"target":"partner:ab12cd34","events":[{"id":41,"event":"panel.created","unix":1767225600,"data":{…}}]}
//
// Up to 50 events per POST, oldest first. Any 2xx answer moves past them;
// anything else is tried again after 10 s, 20 s, 40 s … up to an hour; after
// 25 failed tries the batch is skipped (and stays in the event list).
// Webhook addresses must be public: the HUB never posts into its own or a
// private network.

const apiWebhookSchema = `
CREATE TABLE IF NOT EXISTS api_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  target TEXT NOT NULL,
  event TEXT NOT NULL,
  data TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS api_events_target ON api_events(target, id);
CREATE TABLE IF NOT EXISTS api_webhooks (
  target TEXT PRIMARY KEY,
  url TEXT NOT NULL,
  secret TEXT NOT NULL,
  events TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  cursor INTEGER NOT NULL DEFAULT 0,
  fails INTEGER NOT NULL DEFAULT 0,
  next_try INTEGER NOT NULL DEFAULT 0,
  last_ok INTEGER NOT NULL DEFAULT 0,
  last_try INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS api_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  target TEXT NOT NULL,
  unix INTEGER NOT NULL,
  first_event INTEGER NOT NULL DEFAULT 0,
  last_event INTEGER NOT NULL DEFAULT 0,
  count INTEGER NOT NULL DEFAULT 0,
  status INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS api_deliveries_target ON api_deliveries(target, id);
`

func (s *Store) ensureAPIWebhookSchema() error {
	_, err := s.db.Exec(apiWebhookSchema)
	return err
}

const (
	apiEventsKeep    = 30 * 24 * time.Hour
	apiDeliveryKeep  = 7 * 24 * time.Hour
	apiWebhookBatch  = 50
	apiWebhookTries  = 25
	apiWebhookMaxGap = time.Hour
)

func partnerTarget(keyID string) string { return "partner:" + keyID }
func repTarget(repID string) string     { return "rep:" + repID }

// APIEvent is one event of a target.
type APIEvent struct {
	ID    int64           `json:"id"`
	Event string          `json:"event"`
	Unix  int64           `json:"unix"`
	Data  json.RawMessage `json:"data"`
}

// emitAPIEvent adds an event to a target's list. It never fails the change
// that caused it.
func (s *Store) emitAPIEvent(ctx context.Context, target, event string, data any) {
	if target == "" {
		return
	}
	b, err := json.Marshal(data)
	if err != nil || data == nil {
		b = []byte("{}")
	}
	if _, err := s.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO api_events(unix,target,event,data) VALUES(?,?,?,?)`,
		s.now().Unix(), target, event, string(b)); err != nil {
		log.Printf("api event %s %s: %v", target, event, err)
	}
}

// APIEvents lists a target's events after an id, oldest first.
func (s *Store) APIEvents(ctx context.Context, target string, after int64, limit int, kinds []string) ([]APIEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id,event,unix,data FROM api_events WHERE target=? AND id>?`
	args := []any{target, after}
	if len(kinds) > 0 {
		q += ` AND event IN (` + strings.TrimSuffix(strings.Repeat("?,", len(kinds)), ",") + `)`
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	rows, err := s.ro.QueryContext(ctx, q+` ORDER BY id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIEvent{}
	for rows.Next() {
		var e APIEvent
		var d string
		if err := rows.Scan(&e.ID, &e.Event, &e.Unix, &d); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

// APIWebhook is a target's webhook as its owner sees it.
type APIWebhook struct {
	Configured bool     `json:"configured"`
	URL        string   `json:"url"`
	Events     []string `json:"events"` // empty: every event
	Enabled    bool     `json:"enabled"`
	LastOK     int64    `json:"last_ok"`
	LastTry    int64    `json:"last_try"`
	LastError  string   `json:"last_error"`
	Fails      int      `json:"fails"`
	NextTry    int64    `json:"next_try"`
	Pending    int      `json:"pending"` // events not delivered yet
	// Secret is only set when it was just made.
	Secret string `json:"secret,omitempty"`
}

// APIWebhookIn is what a target sets.
type APIWebhookIn struct {
	URL          string    `json:"url"`
	Events       *[]string `json:"events"`
	Enabled      *bool     `json:"enabled"`
	RotateSecret bool      `json:"rotate_secret"`
}

func (s *Store) APIWebhookOf(ctx context.Context, target string) APIWebhook {
	w := APIWebhook{Events: []string{}}
	var ev string
	var cursor int64
	err := s.ro.QueryRowContext(ctx, `SELECT url,events,enabled,last_ok,last_try,last_error,fails,next_try,cursor FROM api_webhooks WHERE target=?`, target).
		Scan(&w.URL, &ev, &w.Enabled, &w.LastOK, &w.LastTry, &w.LastError, &w.Fails, &w.NextTry, &cursor)
	if err != nil {
		return w
	}
	w.Configured = true
	w.Events = splitTags(ev)
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_events WHERE target=? AND id>?`, target, cursor).Scan(&w.Pending)
	return w
}

// checkWebhookURL accepts a public http(s) address.
func checkWebhookURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || len(raw) > 500 || u.User != nil {
		return "", fmt.Errorf("%w: the webhook address must be an http(s) URL", ErrInvalid)
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicIP(ip) && !webhookAllowPrivate {
		return "", fmt.Errorf("%w: the webhook address must be on the public internet", ErrInvalid)
	}
	if h := strings.ToLower(u.Hostname()); (h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local")) && !webhookAllowPrivate {
		return "", fmt.Errorf("%w: the webhook address must be on the public internet", ErrInvalid)
	}
	return u.String(), nil
}

// PutAPIWebhook creates or changes a target's webhook. A new webhook gets a
// secret (returned once) and starts with the events that come after it.
func (s *Store) PutAPIWebhook(ctx context.Context, target string, in APIWebhookIn, known []string) (APIWebhook, error) {
	u, err := checkWebhookURL(in.URL)
	if err != nil {
		return APIWebhook{}, err
	}
	cur := s.APIWebhookOf(ctx, target)
	events := cur.Events
	if in.Events != nil {
		if events, err = normScopes(*in.Events, known); err != nil {
			return APIWebhook{}, err
		}
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	} else if cur.Configured {
		enabled = cur.Enabled
	}
	secret := ""
	if !cur.Configured || in.RotateSecret {
		secret = "whsec_" + randomHex(24)
	}
	now := s.now().Unix()
	if !cur.Configured {
		var last int64
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM api_events WHERE target=?`, target).Scan(&last)
		if _, err := s.db.ExecContext(ctx, `INSERT INTO api_webhooks(target,url,secret,events,enabled,cursor,created_unix) VALUES(?,?,?,?,?,?,?)`,
			target, u, secret, strings.Join(events, ","), enabled, last, now); err != nil {
			return APIWebhook{}, err
		}
	} else {
		if _, err := s.db.ExecContext(ctx, `UPDATE api_webhooks SET url=?, events=?, enabled=?, fails=0, next_try=0,
secret=CASE WHEN ?<>'' THEN ? ELSE secret END WHERE target=?`, u, strings.Join(events, ","), enabled, secret, secret, target); err != nil {
			return APIWebhook{}, err
		}
	}
	w := s.APIWebhookOf(ctx, target)
	w.Secret = secret
	return w, nil
}

// DeleteAPIWebhook removes a target's webhook; its events stay readable.
func (s *Store) DeleteAPIWebhook(ctx context.Context, target string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM api_webhooks WHERE target=?`, target)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// APIDelivery is one try to deliver a batch.
type APIDelivery struct {
	ID         int64  `json:"id"`
	Unix       int64  `json:"unix"`
	FirstEvent int64  `json:"first_event"`
	LastEvent  int64  `json:"last_event"`
	Count      int    `json:"count"`
	Status     int    `json:"status"` // the receiver's HTTP status; 0 = no answer
	Error      string `json:"error"`
	MS         int64  `json:"ms"`
}

func (s *Store) APIDeliveries(ctx context.Context, target string, limit int) ([]APIDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,first_event,last_event,count,status,error,ms FROM api_deliveries WHERE target=? ORDER BY id DESC LIMIT ?`, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIDelivery{}
	for rows.Next() {
		var d APIDelivery
		if err := rows.Scan(&d.ID, &d.Unix, &d.FirstEvent, &d.LastEvent, &d.Count, &d.Status, &d.Error, &d.MS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---- delivery ----

// webhookAllowPrivate lets tests post to a local server.
var webhookAllowPrivate = false

var errPrivateAddr = errors.New("the webhook address resolves to a private network")

// apiHookClient checks every address it connects to, so a name that
// resolves (or later re-resolves) to a private address is refused too.
var apiHookClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			Control: func(network, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				if ip := net.ParseIP(host); ip == nil || (!publicIP(ip) && !webhookAllowPrivate) {
					return errPrivateAddr
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       60 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func signWebhook(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// postWebhook sends one batch and returns the receiver's status.
func (s *Store) postWebhook(ctx context.Context, target, u, secret string, deliveryID int64, events []APIEvent) (int, error) {
	body, _ := json.Marshal(map[string]any{"target": target, "events": events})
	ts := s.now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DarkXrayPro-Webhook/1")
	req.Header.Set("X-Dark-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Dark-Signature", signWebhook(secret, ts, body))
	req.Header.Set("X-Dark-Delivery", strconv.FormatInt(deliveryID, 10))
	resp, err := apiHookClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("the receiver answered %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func webhookBackoff(fails int) time.Duration {
	d := 10 * time.Second
	for i := 1; i < fails && d < apiWebhookMaxGap; i++ {
		d *= 2
	}
	return min(d, apiWebhookMaxGap)
}

type dueWebhook struct {
	target, url, secret, events string
	cursor                      int64
	fails                       int
}

// deliverWebhook sends a target's next batch.
func (s *Store) deliverWebhook(ctx context.Context, w dueWebhook) {
	events, err := s.APIEvents(ctx, w.target, w.cursor, apiWebhookBatch, nil)
	if err != nil || len(events) == 0 {
		return
	}
	last := events[len(events)-1].ID
	if want := scopeSet(w.events); len(want) > 0 {
		kept := events[:0]
		for _, e := range events {
			if want[e.Event] {
				kept = append(kept, e)
			}
		}
		events = kept
	}
	now := s.now().Unix()
	if len(events) == 0 { // nothing it asked for: move on quietly
		_, _ = s.db.ExecContext(ctx, `UPDATE api_webhooks SET cursor=? WHERE target=? AND cursor=?`, last, w.target, w.cursor)
		return
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_deliveries(target,unix,first_event,last_event,count) VALUES(?,?,?,?,?)`,
		w.target, now, events[0].ID, events[len(events)-1].ID, len(events))
	if err != nil {
		return
	}
	did, _ := res.LastInsertId()
	start := time.Now()
	status, err := s.postWebhook(ctx, w.target, w.url, w.secret, did, events)
	ms := time.Since(start).Milliseconds()
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE api_deliveries SET status=?, ms=? WHERE id=?`, status, ms, did)
		_, _ = s.db.ExecContext(ctx, `UPDATE api_webhooks SET cursor=?, fails=0, next_try=0, last_ok=?, last_try=?, last_error='' WHERE target=? AND cursor=?`,
			last, now, now, w.target, w.cursor)
		return
	}
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	fails := w.fails + 1
	if fails >= apiWebhookTries {
		msg = fmt.Sprintf("skipped after %d failed tries: %s", fails, msg)
		_, _ = s.db.ExecContext(ctx, `UPDATE api_deliveries SET status=?, error=?, ms=? WHERE id=?`, status, msg, ms, did)
		_, _ = s.db.ExecContext(ctx, `UPDATE api_webhooks SET cursor=?, fails=0, next_try=0, last_try=?, last_error=? WHERE target=? AND cursor=?`,
			last, now, msg, w.target, w.cursor)
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_deliveries SET status=?, error=?, ms=? WHERE id=?`, status, msg, ms, did)
	_, _ = s.db.ExecContext(ctx, `UPDATE api_webhooks SET fails=?, next_try=?, last_try=?, last_error=? WHERE target=?`,
		fails, now+int64(webhookBackoff(fails).Seconds()), now, msg, w.target)
}

// apiWebhookTick delivers every due webhook once, a few at a time.
func (s *Store) apiWebhookTick(ctx context.Context) {
	rows, err := s.ro.QueryContext(ctx, `SELECT w.target,w.url,w.secret,w.events,w.cursor,w.fails FROM api_webhooks w
WHERE w.enabled=1 AND w.next_try<=? AND EXISTS(SELECT 1 FROM api_events e WHERE e.target=w.target AND e.id>w.cursor)`, s.now().Unix())
	if err != nil {
		if !noTable(err) {
			log.Printf("api webhooks: %v", err)
		}
		return
	}
	var due []dueWebhook
	for rows.Next() {
		var d dueWebhook
		if rows.Scan(&d.target, &d.url, &d.secret, &d.events, &d.cursor, &d.fails) == nil {
			due = append(due, d)
		}
	}
	rows.Close()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, d := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(d dueWebhook) {
			defer func() { <-sem; wg.Done() }()
			s.deliverWebhook(ctx, d)
		}(d)
	}
	wg.Wait()
}

// apiEventSources are checks that turn panel state into events (low
// credit, …), run about once a minute by RunAPIWebhooks.
var apiEventSources []func(context.Context, *Store)

// RunAPIWebhooks delivers external API webhooks until ctx ends.
func (s *Store) RunAPIWebhooks(ctx context.Context) {
	var lastSources time.Time
	for {
		if time.Since(lastSources) >= time.Minute {
			lastSources = time.Now()
			for _, f := range apiEventSources {
				f(ctx, s)
			}
		}
		s.apiWebhookTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// TestAPIWebhook sends one "ping" event now (not through the list).
func (s *Store) TestAPIWebhook(ctx context.Context, target string) (APIDelivery, error) {
	var u, secret string
	if err := s.ro.QueryRowContext(ctx, `SELECT url,secret FROM api_webhooks WHERE target=?`, target).Scan(&u, &secret); err != nil {
		return APIDelivery{}, ErrNotFound
	}
	now := s.now().Unix()
	ping := APIEvent{ID: 0, Event: "ping", Unix: now, Data: json.RawMessage(`{"message":"webhook test"}`)}
	res, err := s.db.ExecContext(ctx, `INSERT INTO api_deliveries(target,unix,count) VALUES(?,?,1)`, target, now)
	if err != nil {
		return APIDelivery{}, err
	}
	did, _ := res.LastInsertId()
	start := time.Now()
	status, err := s.postWebhook(ctx, target, u, secret, did, []APIEvent{ping})
	d := APIDelivery{ID: did, Unix: now, Count: 1, Status: status, MS: time.Since(start).Milliseconds()}
	if err != nil {
		d.Error = err.Error()
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_deliveries SET status=?, error=?, ms=? WHERE id=?`, d.Status, d.Error, d.MS, did)
	return d, nil
}

func (s *Store) pruneAPIWebhooks(ctx context.Context) error {
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM api_events WHERE unix<?`, now.Add(-apiEventsKeep).Unix()); err != nil && !noTable(err) {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM api_deliveries WHERE unix<?`, now.Add(-apiDeliveryKeep).Unix()); err != nil && !noTable(err) {
		return err
	}
	return nil
}

// ---- HTTP: the same routes for every kind of target ----

// eventRoutes serves a target's events and webhook under prefix
// (/api/v2/partner, /api/v2/reseller), behind the "events" scope.
func (s *Store) eventRoutes(mux routeMux, prefix string, auth extAuthFunc, target func(apiCaller) string, known []string) {
	mux.HandleFunc("GET "+prefix+"/events", s.ext(auth, "events", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		q := r.URL.Query()
		after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		var kinds []string
		if t := strings.TrimSpace(q.Get("types")); t != "" {
			kinds = splitTags(t)
		}
		list, err := s.APIEvents(r.Context(), target(c), after, limit, kinds)
		if err != nil {
			apiFail(w, err)
			return
		}
		var next int64 = after
		if len(list) > 0 {
			next = list[len(list)-1].ID
		}
		apiJSON(w, http.StatusOK, map[string]any{"items": list, "next_after": next})
	}))
	mux.HandleFunc("GET "+prefix+"/webhook", s.ext(auth, "events", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		apiJSON(w, http.StatusOK, s.APIWebhookOf(r.Context(), target(c)))
	}))
	mux.HandleFunc("PUT "+prefix+"/webhook", s.ext(auth, "events", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		var in APIWebhookIn
		if !readJSON(w, r, &in) {
			return
		}
		wh, err := s.PutAPIWebhook(r.Context(), target(c), in, known)
		if err != nil {
			apiFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, wh)
	}))
	mux.HandleFunc("DELETE "+prefix+"/webhook", s.ext(auth, "events", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		if err := s.DeleteAPIWebhook(r.Context(), target(c)); err != nil {
			apiFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST "+prefix+"/webhook/test", s.ext(auth, "events", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		d, err := s.TestAPIWebhook(r.Context(), target(c))
		if err != nil {
			apiFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, d)
	}))
	mux.HandleFunc("GET "+prefix+"/webhook/deliveries", s.ext(auth, "events", func(w http.ResponseWriter, r *http.Request, c apiCaller) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := s.APIDeliveries(r.Context(), target(c), limit)
		if err != nil {
			apiFail(w, err)
			return
		}
		apiJSON(w, http.StatusOK, map[string]any{"items": list})
	}))
}
