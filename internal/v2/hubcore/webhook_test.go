package hubcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWebhookDelivery(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	var mu sync.Mutex
	var got []WebhookItem
	down := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m := hmac.New(sha256.New, []byte("shh"))
		m.Write(b)
		if r.Header.Get("X-Dark-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) {
			t.Error("bad signature")
		}
		mu.Lock()
		defer mu.Unlock()
		if down {
			http.Error(w, "nope", 503)
			return
		}
		var in struct{ Events []WebhookItem }
		_ = json.Unmarshal(b, &in)
		got = append(got, in.Events...)
	}))
	defer srv.Close()
	// An old event must not be replayed when the webhook is turned on.
	if _, err := s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(1,'de-1','alice','blocked:volume')`); err != nil {
		t.Fatal(err)
	}
	if err := s.PutWebhook(ctx, WebhookSettings{Enabled: true, URL: "ftp://x"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad scheme accepted")
	}
	if err := s.PutWebhook(ctx, WebhookSettings{Enabled: true, URL: srv.URL, Secret: "shh", Kinds: []string{"client", "change", "node", "bogus"}}); err != nil {
		t.Fatal(err)
	}
	if st := s.Webhook(ctx); !st.HasSecret || st.Secret != "" || len(st.Kinds) != 3 {
		t.Fatalf("%+v", st)
	}
	_, _ = s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(?,'de-1','alice','blocked:expired')`, now.Unix())
	s.Audit(ctx, Principal{User: "boss", Role: "owner"}, httptest.NewRequest("PUT", "/api/v2/admin/clients/bob", nil), 200)
	s.Audit(ctx, Principal{User: "boss", Role: "owner"}, httptest.NewRequest("DELETE", "/api/v2/admin/clients/x", nil), 403)
	fails := 0
	nodes := s.webhookTick(ctx, nil, &fails) // receiver down: nothing moves
	if fails != 1 || s.Webhook(ctx).LastError == "" {
		t.Fatalf("fails %d", fails)
	}
	mu.Lock()
	down = false
	mu.Unlock()
	nodes = s.webhookTick(ctx, nodes, &fails)
	mu.Lock()
	if len(got) != 2 || got[0].Kind != "client" || got[0].Event != "blocked:expired" || got[1].Kind != "change" || got[1].Path != "/api/v2/admin/clients/bob" {
		t.Fatalf("%+v", got)
	}
	mu.Unlock()
	// The node was never seen; once it syncs it is reported online, then offline.
	_, _ = s.db.Exec(`UPDATE nodes SET last_seen_unix=? WHERE id='de-1'`, now.Unix())
	nodes = s.webhookTick(ctx, nodes, &fails)
	*now = now.Add(5 * time.Minute)
	s.webhookTick(ctx, nodes, &fails)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 4 || got[2].Event != "online" || got[3].Event != "offline" || got[3].Node != "de-1" {
		t.Fatalf("%+v", got)
	}
	if fails != 0 || s.Webhook(ctx).LastOK == 0 {
		t.Fatal("status not recorded")
	}
}
