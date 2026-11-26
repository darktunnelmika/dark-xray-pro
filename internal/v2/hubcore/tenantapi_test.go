package hubcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

type tenantClientT struct {
	t   *testing.T
	url string
	key string
	ip  string
}

func (c tenantClientT) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, c.url+tenantAPI+path, rd)
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.ip != "" {
		req.Header.Set("X-Forwarded-For", c.ip)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{"raw": string(b)}
	}
	return resp.StatusCode, out
}

// twoTenants is tenantStore plus a second shop "other" (client "u1" too, the
// same customer name) and a key for each.
func twoTenants(t *testing.T) (*Store, *httptest.Server, tenantClientT, tenantClientT) {
	t.Helper()
	s, ctx := tenantStore(t)
	if err := s.PutTenant(ctx, Tenant{ID: "other", Name: "Other", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TenantCreate(ctx, "other", TenantClientIn{Name: "u1", VolumeBytes: i64(1 << 30)}); err != nil {
		t.Fatal(err)
	}
	ka, _, err := s.CreateTenantKey(ctx, "tish", "site", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	kb, _, _ := s.CreateTenantKey(ctx, "other", "site", nil, 0)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "https://sub.example.com", nil))
	t.Cleanup(h.Close)
	return s, h, tenantClientT{t: t, url: h.URL, key: ka}, tenantClientT{t: t, url: h.URL, key: kb}
}

func TestTenantKeys(t *testing.T) {
	s, h, a, _ := twoTenants(t)
	ctx := context.Background()
	if code, _ := (tenantClientT{t: t, url: h.URL}).do("GET", "/me", nil); code != 401 {
		t.Fatalf("no key: %d", code)
	}
	if code, _ := (tenantClientT{t: t, url: h.URL, key: "dt_x_y"}).do("GET", "/me", nil); code != 401 {
		t.Fatalf("bad key: %d", code)
	}
	if code, _ := (tenantClientT{t: t, url: h.URL, key: "admintok"}).do("GET", "/me", nil); code != 401 {
		t.Fatal("the owner's token is not a shop key")
	}
	if code, me := a.do("GET", "/me", nil); code != 200 || me["id"] != "tish" || me["clients"].(float64) != 1 {
		t.Fatalf("me %d %v", code, me)
	}
	// The key's secret is never stored.
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tenant_keys WHERE secret_sha256 LIKE '%' || ? || '%'`, strings.SplitN(a.key, "_", 3)[2]).Scan(&n)
	if n != 0 {
		t.Fatal("secret stored in clear")
	}
	key, k, err := s.CreateTenantKey(ctx, "tish", "office", []string{"10.0.0.0/8", "203.0.113.7"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ok := tenantClientT{t: t, url: h.URL, key: key, ip: "10.9.8.7"}
	if code, _ := ok.do("GET", "/me", nil); code != 200 {
		t.Fatalf("allowed range: %d", code)
	}
	ok.ip = "203.0.113.7"
	if code, _ := ok.do("GET", "/me", nil); code != 200 {
		t.Fatalf("allowed address: %d", code)
	}
	ok.ip = "8.8.8.8"
	if code, body := ok.do("GET", "/me", nil); code != 401 || !strings.Contains(fmt.Sprint(body["error"]), "8.8.8.8") {
		t.Fatalf("outside the list: %d %v", code, body)
	}
	s.SetClock(func() time.Time { return time.Unix(1_800_000_000+2*86400, 0) })
	ok.ip = "10.9.8.7"
	if code, _ := ok.do("GET", "/me", nil); code != 401 {
		t.Fatalf("expired key: %d", code)
	}
	s.SetClock(func() time.Time { return time.Unix(1_800_000_000, 0) })
	if err := s.DeleteTenantKey(ctx, "tish", k.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := ok.do("GET", "/me", nil); code != 401 {
		t.Fatalf("revoked key: %d", code)
	}
	if _, _, err := s.CreateTenantKey(ctx, "tish", "x", []string{"not-an-ip"}, 0); err == nil {
		t.Fatal("bad address accepted")
	}
	// A switched-off shop can read but not change.
	_ = s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: false})
	if code, _ := a.do("PATCH", "/clients/u1", map[string]any{"max_ips": 2}); code != 403 {
		t.Fatalf("switched-off shop changed a client: %d", code)
	}
}

func TestTenantAPIClientLifecycle(t *testing.T) {
	s, _, a, _ := twoTenants(t)
	ctx := context.Background()
	uuid := "1b2c3d4e-0000-4000-8000-0000000000aa"
	code, c := a.do("POST", "/clients", map[string]any{"name": "moved", "uuid": uuid, "trojan_password": "oldpass123", "sub_token": "OldSubId123",
		"external_ref": "rep7/order9", "volume_bytes": 10 << 30, "days": 30, "max_ips": 2, "max_devices": 3, "note": "from 3x-ui"})
	if code != 201 || c["uuid"] != uuid || c["trojan_password"] != "oldpass123" || c["sub_token"] != "OldSubId123" || c["max_devices"].(float64) != 3 ||
		c["subscription_url"] != "https://sub.example.com/sub/OldSubId123" || c["name"] != "moved" {
		t.Fatalf("create %d %v", code, c)
	}
	if code, _ := a.do("POST", "/clients", map[string]any{"name": "moved"}); code != 409 {
		t.Fatalf("duplicate %d", code)
	}
	if code, body := a.do("POST", "/clients", map[string]any{"name": "x2", "inbounds": []string{"nope"}}); code != 422 {
		t.Fatalf("unknown inbound %d %v", code, body)
	}
	if code, l := a.do("GET", "/clients?external_ref=rep7/order9", nil); code != 200 || len(l["clients"].([]any)) != 1 {
		t.Fatalf("by ref %d %v", code, l)
	}
	if code, c := a.do("PATCH", "/clients/moved", map[string]any{"volume_bytes": 20 << 30, "max_ips": 0}); code != 200 || c["volume_bytes"].(float64) != 20<<30 || c["uuid"] != uuid {
		t.Fatalf("patch %d %v", code, c)
	}
	if code, body := a.do("PATCH", "/clients/moved", map[string]any{"uuid": "1b2c3d4e-0000-4000-8000-0000000000bb"}); code != 422 {
		t.Fatalf("credentials changed by patch: %d %v", code, body)
	}
	if code, c := a.do("POST", "/clients/moved/disable", nil); code != 200 || c["status"] != "disabled" {
		t.Fatalf("disable %d %v", code, c)
	}
	if code, c := a.do("POST", "/clients/moved/enable", nil); code != 200 || c["status"] != "active" {
		t.Fatalf("enable %d %v", code, c)
	}
	_, _ = s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1, FromUnix: 1, ToUnix: 2, Traffic: []wire.UserTraffic{{Email: "tish+moved", Up: 100, Down: 900}}})
	if code, c := a.do("GET", "/clients/moved", nil); code != 200 || c["used_bytes"].(float64) != 1000 {
		t.Fatalf("usage %d %v", code, c)
	}
	if code, c := a.do("POST", "/clients/moved/reset", nil); code != 200 || c["used_bytes"].(float64) != 0 {
		t.Fatalf("reset %d %v", code, c)
	}
	if code, c := a.do("POST", "/clients/moved/renew", map[string]any{"add_bytes": 1 << 30, "add_days": 5}); code != 200 || c["volume_bytes"].(float64) != 21<<30 {
		t.Fatalf("renew %d %v", code, c)
	}
	if code, c := a.do("POST", "/clients/moved/rotate", nil); code != 200 || c["sub_token"] == "OldSubId123" || c["uuid"] != uuid {
		t.Fatalf("rotate link %d %v", code, c)
	}
	if code, l := a.do("GET", "/clients/moved/links", nil); code != 200 || !strings.HasPrefix(fmt.Sprint(l["subscription_url"]), "https://sub.example.com/sub/") || len(l["links"].([]any)) != 1 {
		t.Fatalf("links %d %v", code, l)
	}
	if code, _ := a.do("DELETE", "/clients/moved", nil); code != 200 {
		t.Fatalf("delete %d", code)
	}
	if code, _ := a.do("GET", "/clients/moved", nil); code != 404 {
		t.Fatalf("deleted still there %d", code)
	}
	log, _ := s.AuditLog(ctx, "", "/api/tenant/v1/clients", 0, 100)
	if len(log) < 8 || !strings.HasPrefix(log[0].Actor, "tenant:tish") {
		t.Fatalf("changes not logged with the shop's name: %+v", log)
	}
}

func TestTenantAPIIsolation(t *testing.T) {
	s, _, a, b := twoTenants(t)
	ctx := context.Background()
	// Both shops have a customer "u1"; each key sees its own.
	_, ca := a.do("GET", "/clients/u1", nil)
	_, cb := b.do("GET", "/clients/u1", nil)
	if ca["uuid"] == cb["uuid"] || ca["volume_bytes"].(float64) != 5<<30 || cb["volume_bytes"].(float64) != 1<<30 {
		t.Fatalf("a %v b %v", ca, cb)
	}
	// Neither reaches the owner's, a reseller's or the other shop's clients.
	for _, name := range []string{"alice", "bob", "r1c", "other+u1", "tish+u1", "..%2Falice"} {
		for _, k := range []tenantClientT{a, b} {
			if code, _ := k.do("GET", "/clients/"+name, nil); code != 404 {
				t.Errorf("GET %s: %d", name, code)
			}
			if code, _ := k.do("PATCH", "/clients/"+name, map[string]any{"enabled": false}); code != 404 && code != 422 {
				t.Errorf("PATCH %s: %d", name, code)
			}
			if code, _ := k.do("DELETE", "/clients/"+name, nil); code != 404 {
				t.Errorf("DELETE %s: %d", name, code)
			}
		}
	}
	_, l := a.do("GET", "/clients", nil)
	if list := l["clients"].([]any); len(list) != 1 || list[0].(map[string]any)["name"] != "u1" {
		t.Fatalf("list %v", l)
	}
	_, res := b.do("POST", "/bulk/update", map[string]any{"clients": []map[string]any{{"name": "alice", "enabled": false}, {"name": "u1", "max_ips": 3}}})
	if res["ok"].(float64) != 1 || res["failed"].(float64) != 1 {
		t.Fatalf("bulk %v", res)
	}
	alice, _ := s.ClientByEmail(ctx, "alice")
	tish, _ := s.TenantClient(ctx, "tish", "u1")
	if !alice.Enabled || tish.MaxIPs != 0 {
		t.Fatal("another world was changed")
	}
	_, ch := a.do("GET", "/changes", nil)
	for _, c := range ch["changes"].([]any) {
		if c.(map[string]any)["name"] != "u1" {
			t.Fatalf("feed has %v", c)
		}
	}
	if len(ch["changes"].([]any)) != 1 {
		t.Fatalf("feed %v", ch)
	}
	// A shop never reaches the owner API with its key.
	req, _ := http.NewRequest("GET", a.url+"/api/v2/admin/clients", nil)
	req.Header.Set("Authorization", "Bearer "+a.key)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("shop key on the owner API: %d", resp.StatusCode)
	}
	// Nor the PasarGuard API.
	req, _ = http.NewRequest("GET", a.url+"/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+a.key)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("shop key on the PasarGuard API: %d", resp.StatusCode)
	}
}

func TestTenantBulk(t *testing.T) {
	s, _, a, _ := twoTenants(t)
	ctx := context.Background()
	items := []map[string]any{{"name": "b1", "volume_bytes": 1 << 30, "days": 30}, {"name": "u1"}, {"name": "b2", "external_ref": "r2"}}
	code, res := a.do("POST", "/bulk/create", map[string]any{"clients": items})
	if code != 200 || res["committed"] != true || res["ok"].(float64) != 2 {
		t.Fatalf("bulk create %d %v", code, res)
	}
	r0 := res["results"].([]any)[0].(map[string]any)
	if r0["uuid"] == "" || !strings.HasPrefix(fmt.Sprint(r0["subscription_url"]), "https://sub.example.com/sub/") {
		t.Fatalf("result %v", r0)
	}
	if r1 := res["results"].([]any)[1].(map[string]any); r1["code"] != "conflict" {
		t.Fatalf("duplicate %v", r1)
	}
	code, res = a.do("POST", "/bulk/create", map[string]any{"atomic": true, "clients": []map[string]any{{"name": "c1"}, {"name": "b1"}, {"name": "c2"}}})
	if code != 422 || res["committed"] != false {
		t.Fatalf("atomic %d %v", code, res)
	}
	if _, err := s.TenantClient(ctx, "tish", "c1"); err == nil {
		t.Fatal("an atomic call that failed kept its first item")
	}
	big := make([]map[string]any, 501)
	for i := range big {
		big[i] = map[string]any{"name": fmt.Sprintf("x%d", i)}
	}
	if code, _ := a.do("POST", "/bulk/create", map[string]any{"clients": big}); code != 422 {
		t.Fatalf("501 items: %d", code)
	}
	code, res = a.do("POST", "/bulk/renew", map[string]any{"clients": []map[string]any{{"name": "b1", "add_bytes": 1 << 30, "add_days": 10}, {"name": "nobody", "add_days": 1}}})
	if code != 200 || res["ok"].(float64) != 1 {
		t.Fatalf("bulk renew %d %v", code, res)
	}
	if c, _ := s.TenantClient(ctx, "tish", "b1"); c.VolumeBytes != 2<<30 || c.ExpiresUnix != s.now().Unix()+40*86400 {
		t.Fatalf("renewed %+v", c)
	}
	code, res = a.do("POST", "/bulk/delete", map[string]any{"names": []string{"b1", "b2", "alice"}})
	if code != 200 || res["ok"].(float64) != 2 {
		t.Fatalf("bulk delete %d %v", code, res)
	}
	if _, err := s.ClientByEmail(ctx, "alice"); err != nil {
		t.Fatal("the owner's client was deleted by a shop")
	}
}

func TestTenantChangeFeed(t *testing.T) {
	s, _, a, _ := twoTenants(t)
	ctx := context.Background()
	for _, n := range []string{"f1", "f2", "f3"} {
		if code, _ := a.do("POST", "/clients", map[string]any{"name": n}); code != 201 {
			t.Fatal(n)
		}
	}
	code, page := a.do("GET", "/changes?limit=2", nil)
	if code != 200 || len(page["changes"].([]any)) != 2 || page["more"] != true {
		t.Fatalf("page 1 %d %v", code, page)
	}
	_, page2 := a.do("GET", "/changes?limit=10&cursor="+page["cursor"].(string), nil)
	if len(page2["changes"].([]any)) != 2 || page2["more"] != false {
		t.Fatalf("page 2 %v", page2)
	}
	cur := page2["cursor"].(string)
	if _, again := a.do("GET", "/changes?cursor="+cur, nil); len(again["changes"].([]any)) != 0 || again["cursor"] != cur {
		t.Fatalf("nothing new %v", again)
	}
	// Usage on two clients (and on the owner's), a change and a delete.
	_, _ = s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1, FromUnix: 1, ToUnix: 2, Traffic: []wire.UserTraffic{
		{Email: "tish+f1", Up: 1, Down: 2}, {Email: "tish+f3", Up: 3, Down: 4}, {Email: "alice", Up: 5, Down: 5}}})
	a.do("POST", "/clients/f2/disable", nil)
	a.do("DELETE", "/clients/u1", nil)
	_, news := a.do("GET", "/changes?cursor="+cur, nil)
	got := []string{}
	for _, c := range news["changes"].([]any) {
		m := c.(map[string]any)
		got = append(got, fmt.Sprintf("%s:%v:%v", m["name"], m["used_bytes"], m["deleted"] == true))
	}
	want := "f1:3:false f3:7:false f2:0:false u1:<nil>:true"
	if strings.Join(got, " ") != want {
		t.Fatalf("feed %q, want %q", strings.Join(got, " "), want)
	}
	if code, _ := a.do("GET", "/changes?cursor=@@bad", nil); code != 422 {
		t.Fatalf("bad cursor %d", code)
	}
}

func TestTenantWebhookGetsOnlyItsOwnEvents(t *testing.T) {
	s, _, a, b := twoTenants(t)
	ctx := context.Background()
	var mu sync.Mutex
	got := map[string][]string{}
	status := 200
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m := hmac.New(sha256.New, []byte("s3cret-"+r.URL.Path[1:]))
		m.Write(body)
		if r.Header.Get("X-Dark-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) {
			t.Errorf("bad signature for %s", r.URL.Path)
		}
		var in struct {
			Tenant string `json:"tenant"`
			Events []struct{ Client, Event string }
		}
		_ = json.Unmarshal(body, &in)
		mu.Lock()
		defer mu.Unlock()
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		for _, e := range in.Events {
			got[in.Tenant] = append(got[in.Tenant], e.Client+":"+e.Event)
		}
	}))
	defer recv.Close()
	if code, w := a.do("PUT", "/webhook", map[string]any{"enabled": true, "url": recv.URL + "/tish", "secret": "s3cret-tish"}); code != 200 || w["has_secret"] != true {
		t.Fatalf("set %d %v", code, w)
	}
	if _, err := s.PutTenantWebhook(ctx, "other", TenantWebhook{Enabled: true, URL: recv.URL + "/other", Secret: "s3cret-other"}); err != nil {
		t.Fatal(err)
	}
	_ = b
	_, _ = s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES
(1,'de-1','tish+u1','blocked:volume'),(2,'de-1','other+u1','blocked:expired'),(3,'de-1','alice','blocked:volume'),
(4,'','tish+u1','renew'),(5,'','tish+u1','reset:api'),(6,'de-1','r1c','device-refused')`)
	fails, next := map[string]int{}, map[string]time.Time{}
	mu.Lock()
	status = 500
	mu.Unlock()
	s.tenantWebhookTick(ctx, fails, next)
	if w, _ := s.TenantWebhookOf(ctx, "tish"); w.LastError == "" {
		t.Fatal("a failed delivery was not noted")
	}
	mu.Lock()
	status = 200
	mu.Unlock()
	for k := range next {
		delete(next, k)
	}
	s.tenantWebhookTick(ctx, fails, next)
	s.tenantWebhookTick(ctx, fails, next) // nothing new: nothing sent twice
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got["tish"], ",") != "u1:blocked:volume,u1:reset:api" || strings.Join(got["other"], ",") != "u1:blocked:expired" || len(got) != 2 {
		t.Fatalf("delivered %v", got)
	}
}
