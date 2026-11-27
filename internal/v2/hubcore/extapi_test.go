package hubcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// apiResp is one answer of the external API in a test.
type apiResp struct {
	Code   int
	Header http.Header
	Raw    []byte
	JSON   map[string]any
	List   []any
}

func (r apiResp) str(k string) string { v, _ := r.JSON[k].(string); return v }
func (r apiResp) num(k string) float64 {
	v, _ := r.JSON[k].(float64)
	return v
}

// apiCall sends one request; extra is header name/value pairs.
func apiCall(t *testing.T, base, method, path, key string, body any, extra ...string) apiResp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := apiResp{Code: resp.StatusCode, Header: resp.Header}
	out.Raw, _ = io.ReadAll(resp.Body)
	_ = json.Unmarshal(out.Raw, &out.JSON)
	_ = json.Unmarshal(out.Raw, &out.List)
	return out
}

func TestExtAPIIdempotency(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	k, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site", MaxGB: 100})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "Shop A", "credit_gb": 40}
	first := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, body, "Idempotency-Key", "order-1001")
	if first.Code != 200 || first.str("id") == "" || first.Header.Get("X-Request-ID") == "" {
		t.Fatalf("create %d %s", first.Code, first.Raw)
	}
	again := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, body, "Idempotency-Key", "order-1001")
	if again.Code != 200 || again.str("id") != first.str("id") || again.str("password") != first.str("password") || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a retry must get the first answer: %d %s", again.Code, again.Raw)
	}
	if kk, _ := s.partnerKey(ctx, k.ID); kk.Panels != 1 || kk.GivenGB != 40 {
		t.Fatalf("one panel, credit counted once: %+v", kk)
	}
	other := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Shop B"}, "Idempotency-Key", "order-1001")
	if other.Code != 422 || other.str("code") != codeIdemReused {
		t.Fatalf("the same key for another request: %d %s", other.Code, other.Raw)
	}
	// A refused request is not kept: fixed, it can go again with its key.
	over := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Big", "credit_gb": 500}, "Idempotency-Key", "order-1002")
	if over.Code != 403 || over.str("code") != codeCreditLimit || over.str("detail") == "" {
		t.Fatalf("over the limit: %d %s", over.Code, over.Raw)
	}
	if _, err := s.UpdatePartnerKey(ctx, k.ID, PartnerKeyIn{Name: "site", MaxGB: 1000}); err != nil {
		t.Fatal(err)
	}
	if ok := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Big", "credit_gb": 500}, "Idempotency-Key", "order-1002"); ok.Code != 200 {
		t.Fatalf("after the limit was raised: %d %s", ok.Code, ok.Raw)
	}
	// Two at once with one key: one runs, the other is told to wait or replays.
	var wg sync.WaitGroup
	codes := make([]int, 2)
	ids := make([]string, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Race", "credit_gb": 1}, "Idempotency-Key", "race-1")
			codes[i], ids[i] = r.Code, r.str("id")
		}(i)
	}
	wg.Wait()
	made := 0
	for i := range 2 {
		if codes[i] == 200 {
			made++
		} else if codes[i] != 409 {
			t.Fatalf("parallel: %v", codes)
		}
	}
	if made == 2 && ids[0] != ids[1] {
		t.Fatalf("two panels for one Idempotency-Key: %v", ids)
	}
	if bad := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, body, "Idempotency-Key", "has space"); bad.Code != 400 {
		t.Fatalf("a malformed Idempotency-Key: %d", bad.Code)
	}
}

func TestExtAPIKeyLimits(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	rate := 3
	k, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "slow", RatePerMin: &rate})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		r := apiCall(t, h.URL, "GET", "/api/v2/partner", k.Key, nil)
		if r.Code != 200 || r.Header.Get("X-RateLimit-Limit") != "3" || r.Header.Get("X-RateLimit-Remaining") != strconv.Itoa(2-i) {
			t.Fatalf("request %d: %d %v", i, r.Code, r.Header)
		}
	}
	r := apiCall(t, h.URL, "GET", "/api/v2/partner", k.Key, nil)
	if r.Code != 429 || r.str("code") != codeRateLimited || r.Header.Get("Retry-After") == "" {
		t.Fatalf("over the rate: %d %s", r.Code, r.Raw)
	}
	*clock = clock.Add(21 * time.Second) // one token back
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner", k.Key, nil); r.Code != 200 {
		t.Fatalf("after a wait: %d", r.Code)
	}

	// Allowed addresses.
	only := []string{"10.0.0.0/8", "2001:db8::1"}
	k2, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "office", AllowedIPs: &only})
	if err != nil || len(k2.AllowedIPs) != 2 || k2.AllowedIPs[1] != "2001:db8::1/128" {
		t.Fatalf("%+v %v", k2, err)
	}
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner", k2.Key, nil); r.Code != 403 || r.str("code") != codeIPNotAllowed {
		t.Fatalf("from an address not allowed: %d %s", r.Code, r.Raw)
	}
	local := []string{"127.0.0.1"}
	if _, err := s.UpdatePartnerKey(ctx, k2.ID, PartnerKeyIn{Name: "office", AllowedIPs: &local}); err != nil {
		t.Fatal(err)
	}
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner", k2.Key, nil); r.Code != 200 {
		t.Fatalf("from an allowed address: %d %s", r.Code, r.Raw)
	}
	bad := []string{"not-an-ip"}
	if _, err := s.UpdatePartnerKey(ctx, k2.ID, PartnerKeyIn{Name: "office", AllowedIPs: &bad}); err == nil {
		t.Fatal("a wrong address must be refused")
	}

	// Expiry.
	exp := clock.Unix() + 60
	k3, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "trial", Expires: &exp})
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner", k3.Key, nil); r.Code != 200 {
		t.Fatalf("before expiry: %d", r.Code)
	}
	*clock = clock.Add(2 * time.Minute)
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner", k3.Key, nil); r.Code != 401 || r.str("code") != codeKeyExpired {
		t.Fatalf("after expiry: %d %s", r.Code, r.Raw)
	}

	// Scopes: a read-only key cannot make panels; old keys have the default set.
	ro := []string{scopePanelsRead}
	k4, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "viewer", Scopes: &ro})
	if r := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k4.Key, map[string]any{"name": "X"}); r.Code != 403 || r.str("code") != codeScopeMissing {
		t.Fatalf("without panels:write: %d %s", r.Code, r.Raw)
	}
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner/reps", k4.Key, nil); r.Code != 200 {
		t.Fatalf("with panels:read: %d", r.Code)
	}
	if kk, _ := s.partnerKey(ctx, k.ID); strings.Join(kk.Scopes, ",") != strings.Join(partnerDefaultScopes, ",") {
		t.Fatalf("default scopes %v", kk.Scopes)
	}
	nope := []string{"everything"}
	if _, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "x", Scopes: &nope}); err == nil {
		t.Fatal("an unknown scope must be refused")
	}

	// An address that keeps trying wrong keys is slowed down; a right key
	// from the same address (another partner behind the same CDN or NAT)
	// still works. X-Request-ID is echoed.
	refused := 0 // the expired key above counted as one already
	for range badKeyRate {
		r := apiCall(t, h.URL, "GET", "/api/v2/partner", "dp_bad_key", nil)
		if r.Code == 429 {
			break
		}
		if r.Code != 401 || r.str("code") != codeUnauthorized {
			t.Fatalf("a wrong key: %d", r.Code)
		}
		refused++
	}
	if refused != badKeyRate-1 {
		t.Fatalf("wrong keys before the slowdown: %d", refused)
	}
	r = apiCall(t, h.URL, "GET", "/api/v2/partner", "dp_bad_key2", nil, "X-Request-ID", "req-12345678")
	if r.Code != 429 || r.str("code") != codeRateLimited || r.Header.Get("Retry-After") == "" || r.Header.Get("X-Request-ID") != "req-12345678" || r.str("request_id") != "req-12345678" {
		t.Fatalf("another wrong key after many: %d %v %s", r.Code, r.Header, r.Raw)
	}
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner", k4.Key, nil); r.Code != 200 {
		t.Fatalf("a right key from the same address: %d %s", r.Code, r.Raw)
	}
}

func TestAPIWebhooks(t *testing.T) {
	webhookAllowPrivate = true
	defer func() { webhookAllowPrivate = false }()
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	k, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site"})
	target := partnerTarget(k.ID)

	var mu sync.Mutex
	var got [][]byte
	var sigs, stamps []string
	fail := false
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		got = append(got, b)
		sigs = append(sigs, r.Header.Get("X-Dark-Signature"))
		stamps = append(stamps, r.Header.Get("X-Dark-Timestamp"))
	}))
	defer recv.Close()

	s.emitAPIEvent(ctx, target, "panel.created", map[string]string{"id": "before"}) // before the webhook: not pushed
	if r := apiCall(t, h.URL, "PUT", "/api/v2/partner/webhook", k.Key, map[string]any{"url": "ftp://x"}); r.Code != 422 {
		t.Fatalf("a wrong address: %d", r.Code)
	}
	if r := apiCall(t, h.URL, "PUT", "/api/v2/partner/webhook", k.Key, map[string]any{"url": recv.URL, "events": []string{"nope"}}); r.Code != 422 {
		t.Fatalf("an unknown event: %d", r.Code)
	}
	r := apiCall(t, h.URL, "PUT", "/api/v2/partner/webhook", k.Key, map[string]any{"url": recv.URL, "events": []string{"panel.created", "panel.credit_added"}})
	secret := r.str("secret")
	if r.Code != 200 || !strings.HasPrefix(secret, "whsec_") || r.JSON["configured"] != true {
		t.Fatalf("webhook %d %s", r.Code, r.Raw)
	}
	if r := apiCall(t, h.URL, "GET", "/api/v2/partner/webhook", k.Key, nil); r.str("secret") != "" || r.str("url") != recv.URL {
		t.Fatalf("the secret is shown once: %s", r.Raw)
	}
	s.emitAPIEvent(ctx, target, "panel.created", map[string]string{"id": "p1"})
	s.emitAPIEvent(ctx, target, "panel.disabled", map[string]string{"id": "p1"}) // not asked for
	s.emitAPIEvent(ctx, target, "panel.credit_added", map[string]any{"id": "p1", "credit_gb": 10})
	s.emitAPIEvent(ctx, partnerTarget("someone-else"), "panel.created", nil)
	s.apiWebhookTick(ctx)
	mu.Lock()
	if len(got) != 1 {
		mu.Unlock()
		t.Fatalf("one batch: %d", len(got))
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(stamps[0] + "."))
	m.Write(got[0])
	if sigs[0] != "sha256="+hex.EncodeToString(m.Sum(nil)) {
		t.Fatal("bad signature")
	}
	var batch struct {
		Target string     `json:"target"`
		Events []APIEvent `json:"events"`
	}
	_ = json.Unmarshal(got[0], &batch)
	mu.Unlock()
	if batch.Target != target || len(batch.Events) != 2 || batch.Events[0].Event != "panel.created" || batch.Events[1].Event != "panel.credit_added" {
		t.Fatalf("batch %+v", batch)
	}
	s.apiWebhookTick(ctx) // nothing new
	if wh := s.APIWebhookOf(ctx, target); wh.Pending != 0 || wh.LastOK == 0 {
		t.Fatalf("%+v", wh)
	}

	// The event list holds everything, the webhook or not.
	ev := apiCall(t, h.URL, "GET", "/api/v2/partner/events?after=0", k.Key, nil)
	items, _ := ev.JSON["items"].([]any)
	if ev.Code != 200 || len(items) != 4 || ev.num("next_after") == 0 {
		t.Fatalf("events %d %s", ev.Code, ev.Raw)
	}
	ev = apiCall(t, h.URL, "GET", "/api/v2/partner/events?types=panel.disabled", k.Key, nil)
	if items, _ := ev.JSON["items"].([]any); len(items) != 1 {
		t.Fatalf("filtered events %s", ev.Raw)
	}

	// A receiver that is down: tried again later, nothing lost.
	mu.Lock()
	fail = true
	mu.Unlock()
	s.emitAPIEvent(ctx, target, "panel.created", map[string]string{"id": "p2"})
	s.apiWebhookTick(ctx)
	wh := s.APIWebhookOf(ctx, target)
	if wh.Fails != 1 || wh.NextTry <= clock.Unix() || wh.Pending != 1 || wh.LastError == "" {
		t.Fatalf("after a failure %+v", wh)
	}
	s.apiWebhookTick(ctx) // too early for the next try
	mu.Lock()
	fail = false
	mu.Unlock()
	*clock = clock.Add(time.Minute)
	s.apiWebhookTick(ctx)
	if wh := s.APIWebhookOf(ctx, target); wh.Fails != 0 || wh.Pending != 0 {
		t.Fatalf("after the receiver came back %+v", wh)
	}
	d := apiCall(t, h.URL, "GET", "/api/v2/partner/webhook/deliveries", k.Key, nil)
	if list, _ := d.JSON["items"].([]any); len(list) != 3 {
		t.Fatalf("deliveries %s", d.Raw)
	}
	if r := apiCall(t, h.URL, "POST", "/api/v2/partner/webhook/test", k.Key, nil); r.Code != 200 || r.num("status") != 200 {
		t.Fatalf("test %d %s", r.Code, r.Raw)
	}
	rot := apiCall(t, h.URL, "PUT", "/api/v2/partner/webhook", k.Key, map[string]any{"url": recv.URL, "rotate_secret": true})
	if rot.str("secret") == "" || rot.str("secret") == secret || len(rot.JSON["events"].([]any)) != 2 {
		t.Fatalf("rotate keeps the events and makes a new secret: %s", rot.Raw)
	}
	if r := apiCall(t, h.URL, "DELETE", "/api/v2/partner/webhook", k.Key, nil); r.Code != 200 {
		t.Fatalf("delete %d", r.Code)
	}

	// Private addresses are refused outside tests.
	webhookAllowPrivate = false
	for _, u := range []string{"http://127.0.0.1:8080/x", "http://10.1.2.3/x", "http://localhost/x", "http://[::1]/x", "http://100.64.1.1/x"} {
		if _, err := checkWebhookURL(u); err == nil {
			t.Fatalf("%s must be refused", u)
		}
	}
	if _, err := checkWebhookURL("https://bot.example.com/hook?x=1"); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimiter(t *testing.T) {
	var l rateLimiter
	now := time.Unix(1000, 0)
	for range 5 {
		if ok, _, _ := l.take("a", 5, now); !ok {
			t.Fatal("within the burst")
		}
	}
	ok, _, wait := l.take("a", 5, now)
	if ok || wait <= 0 || wait > 12*time.Second {
		t.Fatalf("over: %v %v", ok, wait)
	}
	if ok, _, _ := l.take("b", 5, now); !ok {
		t.Fatal("buckets are separate")
	}
	if ok, _, _ := l.take("a", 5, now.Add(12*time.Second)); !ok {
		t.Fatal("refilled")
	}
}
