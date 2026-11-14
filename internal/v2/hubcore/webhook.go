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
	"strconv"
	"strings"
	"time"
)

// Webhooks push what happens in the panel to another system (a billing
// site, a Discord/Slack relay, a CRM). Three kinds of items are sent:
//
//   - client: client events the HUB records (blocked:expired|volume|ips…,
//     reset:*, next-plan, device-refused);
//   - change: every successful change in the change log (who did what);
//   - node:   a node going offline or coming back.
//
// Items are batched (up to 50 per POST) as {"events":[...]} and signed with
// HMAC-SHA256 of the body in X-Dark-Signature: sha256=<hex>. A cursor per
// source only moves after a 2xx reply, so nothing is lost while the
// receiver is down; after 20 failed tries a batch is skipped and noted.

type WebhookSettings struct {
	Enabled bool     `json:"enabled"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret,omitempty"`
	Kinds   []string `json:"kinds"` // client | change | node
}

type WebhookStatus struct {
	WebhookSettings
	HasSecret bool   `json:"has_secret"`
	LastOK    int64  `json:"last_ok"`
	LastError string `json:"last_error"`
	LastTry   int64  `json:"last_try"`
}

type WebhookItem struct {
	Kind   string `json:"kind"` // client | change | node
	Unix   int64  `json:"unix"`
	Event  string `json:"event"`
	Client string `json:"client,omitempty"`
	Node   string `json:"node,omitempty"`
	Actor  string `json:"actor,omitempty"`
	Path   string `json:"path,omitempty"`
}

func (s *Store) webhookSettings(ctx context.Context) WebhookSettings {
	var w WebhookSettings
	_ = json.Unmarshal([]byte(s.Setting(ctx, "webhook")), &w)
	return w
}

// Webhook returns the settings without the secret.
func (s *Store) Webhook(ctx context.Context) WebhookStatus {
	w := s.webhookSettings(ctx)
	st := WebhookStatus{WebhookSettings: w, HasSecret: w.Secret != ""}
	st.Secret = ""
	st.LastOK, _ = strconv.ParseInt(s.Setting(ctx, "webhook_last_ok"), 10, 64)
	st.LastTry, _ = strconv.ParseInt(s.Setting(ctx, "webhook_last_try"), 10, 64)
	st.LastError = s.Setting(ctx, "webhook_last_error")
	return st
}

// PutWebhook saves settings; an empty secret keeps the old one, "-" removes it.
func (s *Store) PutWebhook(ctx context.Context, w WebhookSettings) error {
	w.URL = strings.TrimSpace(w.URL)
	if w.URL != "" {
		u, err := url.Parse(w.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%w: the webhook address must be an http(s) URL", ErrInvalid)
		}
	}
	if w.Enabled && w.URL == "" {
		return fmt.Errorf("%w: enter the address to send to", ErrInvalid)
	}
	kinds := []string{}
	for _, k := range w.Kinds {
		if k == "client" || k == "change" || k == "node" {
			kinds = append(kinds, k)
		}
	}
	w.Kinds = kinds
	old := s.webhookSettings(ctx)
	switch w.Secret {
	case "":
		w.Secret = old.Secret
	case "-":
		w.Secret = ""
	}
	if w.Enabled && !old.Enabled {
		// Start from now: turning it on must not replay the whole history.
		if err := s.webhookResetCursors(ctx); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(w)
	return s.SetSetting(ctx, "webhook", string(b))
}

func (s *Store) webhookResetCursors(ctx context.Context) error {
	var ev, au int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM events`).Scan(&ev)
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM audit_log`).Scan(&au)
	if err := s.SetSetting(ctx, "webhook_cursor_events", strconv.FormatInt(ev, 10)); err != nil {
		return err
	}
	return s.SetSetting(ctx, "webhook_cursor_audit", strconv.FormatInt(au, 10))
}

func wants(w WebhookSettings, kind string) bool {
	for _, k := range w.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// webhookBatch collects pending items and the cursors they would advance to.
func (s *Store) webhookBatch(ctx context.Context, w WebhookSettings, nodes map[string]bool) (items []WebhookItem, evTo, auTo int64, nodeNow map[string]bool, err error) {
	evFrom, _ := strconv.ParseInt(s.Setting(ctx, "webhook_cursor_events"), 10, 64)
	auFrom, _ := strconv.ParseInt(s.Setting(ctx, "webhook_cursor_audit"), 10, 64)
	evTo, auTo = evFrom, auFrom
	if wants(w, "client") {
		rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,node_id,email,kind FROM events WHERE id>? ORDER BY id LIMIT 50`, evFrom)
		if err != nil {
			return nil, 0, 0, nil, err
		}
		for rows.Next() {
			var it WebhookItem
			var id int64
			if err := rows.Scan(&id, &it.Unix, &it.Node, &it.Client, &it.Event); err != nil {
				rows.Close()
				return nil, 0, 0, nil, err
			}
			it.Kind = "client"
			items, evTo = append(items, it), id
		}
		rows.Close()
	}
	if wants(w, "change") && len(items) < 50 {
		rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,actor,method,path FROM audit_log WHERE id>? AND status<400 AND path<>'/api/v2/auth/login' ORDER BY id LIMIT ?`, auFrom, 50-len(items))
		if err != nil {
			return nil, 0, 0, nil, err
		}
		for rows.Next() {
			var it WebhookItem
			var id int64
			var method string
			if err := rows.Scan(&id, &it.Unix, &it.Actor, &method, &it.Path); err != nil {
				rows.Close()
				return nil, 0, 0, nil, err
			}
			it.Kind, it.Event = "change", method
			items, auTo = append(items, it), id
		}
		rows.Close()
	}
	nodeNow = map[string]bool{}
	if wants(w, "node") {
		now := s.now().Unix()
		rows, err := s.ro.QueryContext(ctx, `SELECT id,last_seen_unix FROM nodes`)
		if err != nil {
			return nil, 0, 0, nil, err
		}
		for rows.Next() {
			var id string
			var seen int64
			if err := rows.Scan(&id, &seen); err != nil {
				rows.Close()
				return nil, 0, 0, nil, err
			}
			online := seen > 0 && now-seen < 90
			nodeNow[id] = online
			if was, known := nodes[id]; known && was != online {
				ev := "offline"
				if online {
					ev = "online"
				}
				items = append(items, WebhookItem{Kind: "node", Unix: now, Event: ev, Node: id})
			}
		}
		rows.Close()
	}
	return items, evTo, auTo, nodeNow, nil
}

var webhookClient = &http.Client{Timeout: 15 * time.Second}

// sendWebhook posts one batch and reports the receiver's answer.
func sendWebhook(ctx context.Context, w WebhookSettings, items []WebhookItem) error {
	body, _ := json.Marshal(map[string]any{"source": "dark-xray-pro", "events": items})
	// Chat webhooks (Discord, Slack) want readable text, not our JSON.
	switch {
	case strings.Contains(w.URL, "discord.com/api/webhooks/") || strings.Contains(w.URL, "discordapp.com/api/webhooks/"):
		body, _ = json.Marshal(map[string]any{"username": "Dark VPN", "content": chatText(items, 1900)})
	case strings.Contains(w.URL, "hooks.slack.com/"):
		body, _ = json.Marshal(map[string]any{"text": chatText(items, 3500)})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DarkXrayPro-Webhook/1")
	if w.Secret != "" {
		m := hmac.New(sha256.New, []byte(w.Secret))
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

// TestWebhook sends one sample item to the saved address.
func (s *Store) TestWebhook(ctx context.Context) error {
	w := s.webhookSettings(ctx)
	if w.URL == "" {
		return fmt.Errorf("%w: save an address first", ErrInvalid)
	}
	err := sendWebhook(ctx, w, []WebhookItem{{Kind: "test", Unix: s.now().Unix(), Event: "test"}})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

// webhookTick sends what is pending once; returns the node states to carry.
func (s *Store) webhookTick(ctx context.Context, nodes map[string]bool, fails *int) map[string]bool {
	w := s.webhookSettings(ctx)
	if !w.Enabled || w.URL == "" {
		return nil
	}
	items, evTo, auTo, nodeNow, err := s.webhookBatch(ctx, w, nodes)
	if err != nil {
		log.Printf("webhook: %v", err)
		return nodes
	}
	if len(items) == 0 {
		return nodeNow
	}
	now := strconv.FormatInt(s.now().Unix(), 10)
	_ = s.SetSetting(ctx, "webhook_last_try", now)
	if err := sendWebhook(ctx, w, items); err != nil {
		*fails++
		_ = s.SetSetting(ctx, "webhook_last_error", err.Error())
		if *fails < 20 {
			return nodes // retry the same batch (node changes included) next time
		}
		log.Printf("webhook: giving up on %d items after %d tries: %v", len(items), *fails, err)
		_ = s.SetSetting(ctx, "webhook_last_error", fmt.Sprintf("skipped %d items after %d failed tries: %v", len(items), *fails, err))
	} else {
		_ = s.SetSetting(ctx, "webhook_last_ok", now)
		_ = s.SetSetting(ctx, "webhook_last_error", "")
	}
	*fails = 0
	_ = s.SetSetting(ctx, "webhook_cursor_events", strconv.FormatInt(evTo, 10))
	_ = s.SetSetting(ctx, "webhook_cursor_audit", strconv.FormatInt(auTo, 10))
	return nodeNow
}

// RunWebhooks delivers pending items every few seconds until ctx ends.
func (s *Store) RunWebhooks(ctx context.Context) {
	var nodes map[string]bool
	fails := 0
	for {
		nodes = s.webhookTick(ctx, nodes, &fails)
		wait := 5 * time.Second
		if fails > 0 {
			wait = time.Duration(min(fails, 12)) * 10 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// chatText renders items as short lines for chat webhooks.
func chatText(items []WebhookItem, max int) string {
	var b strings.Builder
	for _, it := range items {
		var line string
		switch it.Kind {
		case "client":
			line = fmt.Sprintf("👤 %s — %s", it.Client, it.Event)
		case "change":
			line = fmt.Sprintf("✏️ %s %s %s", it.Actor, it.Event, strings.TrimPrefix(it.Path, "/api/v2/admin/"))
		case "node":
			icon := "🟢"
			if it.Event == "offline" {
				icon = "🔴"
			}
			line = fmt.Sprintf("%s node %s %s", icon, it.Node, it.Event)
		default:
			line = "✅ Dark VPN webhook test"
		}
		if b.Len()+len(line)+1 > max {
			b.WriteString("…")
			break
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
