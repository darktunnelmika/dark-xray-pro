package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// raw sends a request the way Mirza's CurlRequest does: an exact body
// string (or none), the headers Mirza sets, and the bearer token.
func auditRaw(t *testing.T, method, url, tok, ctype, body string) (int, map[string]any, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("accept", "application/json")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return resp.StatusCode, out, string(b)
}

func auditJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// TestAudit2MirzaReplay replays Mirza's PasarGuard driver call by call
// (panels/Api/Marzban.php, panels/Drivers/Marzban.php, panels.php) for the
// owner's token and a reseller's.
func TestAudit2MirzaReplay(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	if err := s.AdjustCredit(ctx, "owner", "ali", 500<<30, 50, ""); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	o, _ := s.EnablePGAPI(ctx, "")
	a, _ := s.EnablePGAPI(ctx, "ali")

	for _, who := range []struct{ user, pass, prefix string }{{"mirza", o.Password, "own"}, {"mirza_ali", a.Password, "rep"}} {
		t.Run(who.prefix, func(t *testing.T) {
			tok := c.login(who.user, who.pass) // form post, as token_panel()
			// Panel setup: inbounds from a user (admin.php:7088).
			code, ag, _ := auditRaw(t, "GET", h.URL+"/api/user/"+PGAllGroups, tok, "", "")
			if code != 200 || ag["group_ids"] == nil {
				t.Fatalf("all-groups %d %v", code, ag)
			}
			gids := ag["group_ids"]
			// Buy, offconecton: ISO expire with Tehran offset, a float limit.
			exp := time.Unix(now.Unix()+30*86400, 0).In(time.FixedZone("IRST", 12600)).Format(time.RFC3339)
			name := who.prefix + "buy1"
			body := fmt.Sprintf(`{"proxy_settings":{},"data_limit":10737418240.0,"username":%q,"note":"123 | @x | buy","data_limit_reset_strategy":"no_reset","group_ids":%s,"expire":%q}`,
				name, auditJSON(gids), exp)
			code, out, rawb := auditRaw(t, "POST", h.URL+"/api/user", tok, "application/json", body)
			if code >= 400 || out["detail"] != nil || out["username"] != name {
				t.Fatalf("buy %d %s", code, rawb)
			}
			subURL := out["subscription_url"].(string)
			resp, err := http.Get(subURL + "/links")
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("links %v %v", err, resp)
			}
			resp.Body.Close()
			// Buy on_hold and a test account.
			for _, n := range []string{who.prefix + "hold1", who.prefix + "test1"} {
				body := fmt.Sprintf(`{"proxy_settings":{},"data_limit":1073741824,"username":%q,"note":"n","data_limit_reset_strategy":"no_reset","group_ids":%s,"expire":0,"status":"on_hold","on_hold_expire_duration":86400}`,
					n, auditJSON(gids))
				if code, out, rawb := auditRaw(t, "POST", h.URL+"/api/user", tok, "application/json", body); code >= 400 || out["status"] != "on_hold" {
					t.Fatalf("on_hold buy %d %s", code, rawb)
				}
			}
			// Read: user, then sub_update?offset=0&limit=1.
			code, u, _ := auditRaw(t, "GET", h.URL+"/api/user/"+name, tok, "", "")
			if code != 200 || u["username"] != name || u["links"] == nil || u["expire"] == nil {
				t.Fatalf("read %d %v", code, u)
			}
			if code, su, _ := auditRaw(t, "GET", h.URL+"/api/user/"+name+"/sub_update?offset=0&limit=1", tok, "", ""); code != 200 || su["updates"] == nil {
				t.Fatalf("sub_update %d %v", code, su)
			}
			proxies := auditJSON(u["proxy_settings"])
			// Renew, resetVolumeTime: POST reset without a body, then PUT.
			if code, out, rawb := auditRaw(t, "POST", h.URL+"/api/user/"+name+"/reset", tok, "", ""); code != 200 || out["detail"] != nil {
				t.Fatalf("reset %d %s", code, rawb)
			}
			put := func(cfg string) map[string]any {
				t.Helper()
				code, out, rawb := auditRaw(t, "PUT", h.URL+"/api/user/"+name, tok, "application/json", cfg)
				if code == 500 || out["detail"] != nil {
					t.Fatalf("put %s: %d %s", cfg, code, rawb)
				}
				return out
			}
			newExp := now.Unix() + 30*86400
			out = put(fmt.Sprintf(`{"data_limit":10737418240,"expire":%d,"inbounds":%s,"proxy_settings":%s}`, newExp, auditJSON(gids), proxies))
			if out["data_limit"].(float64) != 10<<30 {
				t.Fatalf("renew %v", out)
			}
			// Renew, addTimeVolumeNextMonth: old + new.
			out = put(fmt.Sprintf(`{"data_limit":21474836480,"expire":%d,"inbounds":%s,"proxy_settings":%s}`, newExp+30*86400, auditJSON(gids), proxies))
			if out["data_limit"].(float64) != 20<<30 {
				t.Fatalf("renew add %v", out)
			}
			// Extra volume and extra time (inbounds is the panel's JSON, maybe "{}").
			out = put(fmt.Sprintf(`{"data_limit":32212254720,"inbounds":"{}","proxy_settings":%s}`, proxies))
			if out["data_limit"].(float64) != 30<<30 {
				t.Fatalf("extra volume %v", out)
			}
			out = put(fmt.Sprintf(`{"expire":%d,"inbounds":%s,"proxy_settings":%s}`, newExp+40*86400, auditJSON(gids), proxies))
			if out["status"] != "active" {
				t.Fatalf("extra time %v", out)
			}
			// Change status both ways.
			if out = put(fmt.Sprintf(`{"status":"disabled","proxy_settings":%s}`, proxies)); out["status"] != "disabled" {
				t.Fatalf("disable %v", out)
			}
			if out = put(fmt.Sprintf(`{"status":"active","proxy_settings":%s}`, proxies)); out["status"] != "active" {
				t.Fatalf("enable %v", out)
			}
			// Revoke, then read the new link.
			if code, _, rawb := auditRaw(t, "POST", h.URL+"/api/user/"+name+"/revoke_sub", tok, "", ""); code != 200 {
				t.Fatalf("revoke %d %s", code, rawb)
			}
			if _, u2, _ := auditRaw(t, "GET", h.URL+"/api/user/"+name, tok, "", ""); u2["subscription_url"] == subURL {
				t.Fatal("revoke kept the link")
			}
			// Users list for the on_hold cron.
			code, list, _ := auditRaw(t, "GET", h.URL+"/api/users?status=on_hold", tok, "", "")
			held := 0
			for _, x := range list["users"].([]any) {
				if strings.HasPrefix(x.(map[string]any)["username"].(string), who.prefix) {
					held++
				}
			}
			if code != 200 || held != 2 {
				t.Fatalf("on_hold list %d %d", code, held)
			}
			// Delete: exactly 200 and the Marzban detail.
			if code, out, rawb := auditRaw(t, "DELETE", h.URL+"/api/user/"+who.prefix+"test1", tok, "", ""); code != 200 || out["detail"] != "User successfully deleted" {
				t.Fatalf("delete %d %s", code, rawb)
			}
			// Nodes: list, info, rename, multiplier, IP, reconnect.
			code, _, nodesRaw := auditRaw(t, "GET", h.URL+"/api/nodes", tok, "", "")
			var nodes []map[string]any
			_ = json.Unmarshal([]byte(nodesRaw), &nodes)
			if code != 200 {
				t.Fatal("nodes", code)
			}
			if who.prefix == "rep" {
				if len(nodes) != 0 {
					t.Fatal("a reseller sees no node", nodes)
				}
				return
			}
			nid := fmt.Sprint(nodes[0]["id"])
			code, n, _ := auditRaw(t, "GET", h.URL+"/api/node/"+nid, tok, "", "")
			if code != 200 || n["uplink"] == nil || n["api_port"] == nil {
				t.Fatalf("node %d %v", code, n)
			}
			for _, cfg := range []string{`{"name":"Berlin","api_port":0}`, `{"usage_coefficient":1.5,"api_port":0}`, `{"address":"9.9.9.9","api_port":0}`} {
				if code, _, rawb := auditRaw(t, "PUT", h.URL+"/api/node/"+nid, tok, "application/json", cfg); code != 200 {
					t.Fatalf("node put %s %d %s", cfg, code, rawb)
				}
			}
			if code, _, _ := auditRaw(t, "POST", h.URL+"/api/node/"+nid+"/reconnect", tok, "", ""); code != 200 {
				t.Fatal("reconnect", code)
			}
		})
	}
}

// TestAudit2BulkPartialNotAudited: a bulk action stopped part way leaves
// the users it already changed out of the audit log's name search, unlike
// every other bulk route (which note each user as it goes).
func TestAudit2BulkPartialNotAudited(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	gid := g["groups"].([]any)[0].(map[string]any)["id"]
	exp := now.Add(30 * 24 * time.Hour).Unix()
	var ids []any
	for _, n := range []string{"zfirst", "zsecond"} {
		code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": n, "data_limit": 10 << 30, "expire": exp, "group_ids": []any{gid}})
		if code != 201 {
			t.Fatal(code, out)
		}
		ids = append(ids, out["id"])
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('zfirst',?,0,?,?),('zsecond',?,0,?,?)`,
		int64(1<<30), now.Unix(), now.Unix(), int64(9<<30), now.Unix(), now.Unix())
	_, _ = s.db.Exec(`UPDATE reps SET volume_credit=? WHERE id='ali'`, int64(2<<30)) // enough for zfirst's reset, not zsecond's
	code, out := c.obj("POST", "/api/users/bulk/reset", tok, map[string]any{"ids": ids})
	t.Logf("bulk reset: %d %v", code, out)
	if v, _ := s.clientView(ctx, "zfirst"); v.Usage.Up != 0 {
		t.Fatal("zfirst was not reset; the test setup is wrong")
	}
	list, _ := s.AuditLog(ctx, "", "zfirst", 0, 100)
	for _, e := range list {
		t.Logf("audit row for zfirst: %s %s %d", e.Method, e.Path, e.Status)
	}
	if len(list) < 2 {
		t.Errorf("BUG: zfirst was reset (and charged) by the bulk call, but the audit log has %d row(s) naming it (create only)", len(list))
	}
}

// TestAudit2NodeDeleteDropsGroups: PasarGuard's DELETE /api/node/{id}
// removes the node and its usage rows only; here it removes the node's
// inbounds, which are the API's groups: every user loses them and the
// group ids a bot stored stop working.
func TestAudit2NodeDeleteDropsGroups(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	o, _ := s.EnablePGAPI(ctx, "")
	tok := c.login("mirza", o.Password)
	_, ag := c.obj("GET", "/api/user/"+PGAllGroups, tok, nil)
	gids := ag["group_ids"]
	if code, _ := c.call("DELETE", "/api/node/1", tok, nil); code != 409 {
		t.Fatal("a node holding the only copy of inbounds is not deleted through the API", code)
	}
	var inb int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM inbounds`).Scan(&inb)
	alice, _ := s.clientView(ctx, "alice")
	t.Logf("after DELETE /api/node/1: inbounds=%d alice.inbounds=%v", inb, alice.Inbounds)
	code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": "afterdel", "data_limit": 1 << 30,
		"expire": now.Add(24 * time.Hour).Unix(), "group_ids": gids})
	t.Logf("Mirza's next sale with its stored group_ids %v: %d %v", gids, code, out)
	if inb == 0 && len(alice.Inbounds) == 0 && code == 422 {
		t.Errorf("BUG: one node delete removed every inbound (group) from every user and broke the bot's sales")
	}
}

// TestAudit2RenewSmallerOvercharges: Mirza's default renewal (reset, then
// PUT the new package) charges a reseller more than the panel's own
// renewal of the same package when the new package is smaller.
func TestAudit2RenewSmallerOvercharges(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	gid := g["groups"].([]any)[0].(map[string]any)["id"]
	credit := func() int64 {
		var v int64
		_ = s.db.QueryRow(`SELECT volume_credit FROM reps WHERE id='ali'`).Scan(&v)
		return v
	}
	exp := now.Add(30 * 24 * time.Hour).Unix()
	for _, n := range []string{"rmirza", "rpanel"} {
		if code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": n, "data_limit": 10 << 30, "expire": exp, "group_ids": []any{gid}}); code != 201 {
			t.Fatal(code, out)
		}
		_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,0,?,?)`, n, int64(8<<30), now.Unix(), now.Unix())
	}
	// Mirza, resetVolumeTime with a 5 GB product.
	before := credit()
	if code, _ := c.obj("POST", "/api/user/rmirza/reset", tok, nil); code != 200 {
		t.Fatal("reset", code)
	}
	if code, out := c.obj("PUT", "/api/user/rmirza", tok, map[string]any{"data_limit": 5 << 30, "expire": now.Add(30 * 24 * time.Hour).Unix(), "inbounds": []any{gid}}); code != 200 {
		t.Fatal("put", code, out)
	}
	viaAPI := before - credit()
	// The panel's renewal of the same package.
	before = credit()
	if _, err := s.RenewClient(ctx, Principal{User: "ali", Role: "rep", RepID: "ali"}, "rpanel", "volume", 5, 30); err != nil {
		t.Fatal(err)
	}
	viaPanel := before - credit()
	t.Logf("renew to 5 GB after 8 of 10 GB used: Mirza path charged %.1f GB, panel renewal charged %.1f GB", float64(viaAPI)/(1<<30), float64(viaPanel)/(1<<30))
	for _, n := range []string{"rmirza", "rpanel"} {
		v, _ := s.clientView(ctx, n)
		t.Logf("%s: volume %.1f GB used %d", n, float64(v.VolumeBytes)/(1<<30), v.Usage.Up+v.Usage.Down)
	}
	if viaAPI != viaPanel {
		t.Errorf("BUG: the same renewal costs %d bytes more through Mirza's API calls", viaAPI-viaPanel)
	}
}

// TestAudit2OnHoldDurationIgnored: PasarGuard's PUT applies
// on_hold_expire_duration on its own (crud/user.py modify_user); here it is
// read only together with status "on_hold", and dropped silently otherwise.
func TestAudit2OnHoldDurationIgnored(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	o, _ := s.EnablePGAPI(ctx, "")
	tok := c.login("mirza", o.Password)
	// bob is on hold for one day (seed).
	code, out := c.obj("PUT", "/api/user/bob", tok, map[string]any{"on_hold_expire_duration": 7 * 86400})
	t.Logf("PUT on_hold_expire_duration=7d: %d status=%v on_hold_expire_duration=%v", code, out["status"], out["on_hold_expire_duration"])
	if code == 200 && out["on_hold_expire_duration"].(float64) == 86400 {
		t.Errorf("BUG: 200 OK but the on-hold duration is unchanged")
	}
}

// TestAudit2ExtraTimeAfterRefund: Mirza's "extra time" on a reseller's
// volume user that expired more than 10 minutes ago gives a limited user:
// the expiry job took the unused volume back.
func TestAudit2ExtraTimeAfterRefund(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	gid := g["groups"].([]any)[0].(map[string]any)["id"]
	if code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": "xt1", "data_limit": 10 << 30, "expire": now.Add(24 * time.Hour).Unix(), "group_ids": []any{gid}}); code != 201 {
		t.Fatal(code, out)
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('xt1',?,0,?,?)`, int64(2<<30), now.Unix(), now.Unix())
	*now = now.Add(25 * time.Hour)
	s.runExpiryRefunds(ctx)
	tok = c.login("mirza_ali", a.Password)
	_, u := c.obj("GET", "/api/user/xt1", tok, nil)
	t.Logf("before extra time: status=%v data_limit=%v used=%v", u["status"], u["data_limit"], u["used_traffic"])
	// extra_time(): expire = max(now, old) + 7 days, inbounds from the panel.
	code, out := c.obj("PUT", "/api/user/xt1", tok, map[string]any{"expire": now.Add(7 * 24 * time.Hour).Unix(), "inbounds": []any{gid}, "proxy_settings": u["proxy_settings"]})
	t.Logf("after extra time: %d status=%v data_limit=%v", code, out["status"], out["data_limit"])
	if code == 200 && out["status"] == "limited" {
		t.Errorf("BUG (design): Mirza reports the extra time sold, the user is 'limited' and cannot connect")
	}
}

// TestAudit2NodeEditsMirzaAllows: Mirza accepts any multiplier >= 0 and any
// address (admin.php:5617, 5664) and never reads PUT /api/node's answer.
func TestAudit2NodeEditsMirzaAllows(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	o, _ := s.EnablePGAPI(ctx, "")
	tok := c.login("mirza", o.Password)
	for _, cfg := range []string{`{"usage_coefficient":0,"api_port":0}`, `{"usage_coefficient":20,"api_port":0}`, `{"address":"node1.example.com","api_port":0}`} {
		code, _, rawb := auditRaw(t, "PUT", h.URL+"/api/node/1", tok, "application/json", cfg)
		t.Logf("PUT /api/node/1 %s -> %d %s", cfg, code, strings.TrimSpace(rawb))
	}
	_, _, nodes := auditRaw(t, "GET", h.URL+"/api/nodes", tok, "", "")
	t.Logf("GET /api/nodes -> %s", nodes[:min(len(nodes), 60)])
}

// TestAudit2OwnerModeSwitch: the owner's access takes a mode it ignores.
func TestAudit2OwnerModeSwitch(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	_, _ = s.EnablePGAPI(ctx, "")
	code, out, rawb := auditRaw(t, "PUT", h.URL+"/api/v2/admin/pg-api", "admintok", "application/json", `{"mode":"mirza"}`)
	t.Logf("owner PUT mode=mirza -> %d mode=%v (%s)", code, out["mode"], strings.TrimSpace(rawb))
	var botOff int
	_ = s.db.QueryRow(`SELECT bot_off FROM pg_api WHERE seller=''`).Scan(&botOff)
	t.Logf("stored bot_off for the owner: %d", botOff)
}

// TestAudit2OnHoldTimeoutSeconds: PasarGuard reads an integer
// on_hold_timeout as seconds from now (validators.py
// validator_on_hold_timeout); here it is a Unix time, so "7 days" is 1970.
func TestAudit2OnHoldTimeoutSeconds(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	o, _ := s.EnablePGAPI(ctx, "")
	tok := c.login("mirza", o.Password)
	code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": "hold7", "data_limit": 1 << 30, "expire": 0,
		"status": "on_hold", "on_hold_expire_duration": 30 * 86400, "on_hold_timeout": 7 * 86400})
	t.Logf("create on_hold with on_hold_timeout=604800: %d status=%v on_hold_timeout=%v", code, out["status"], out["on_hold_timeout"])
	s.runOnHold(ctx)
	_, out = c.obj("GET", "/api/user/hold7", tok, nil)
	t.Logf("after the on-hold job: status=%v expire=%v", out["status"], out["expire"])
	if out["status"] == "expired" {
		t.Errorf("BUG: a 7-day on-hold window made a brand-new user expired")
	}
}

// TestAudit2LinksSkipsDeviceLimit: /sub/{token}/links serves the configs
// without the device check that /sub/{token} and /sub/{token}/{type} do.
func TestAudit2LinksSkipsDeviceLimit(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	_ = s.SetHWIDLimit(ctx, "alice", 1)
	v, _ := s.clientView(ctx, "alice")
	get := func(path, hwid string) int {
		req, _ := http.NewRequest("GET", h.URL+path, nil)
		req.Header.Set("X-Hwid", hwid)
		req.Header.Set("User-Agent", "v2rayNG/1.8")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	t.Logf("dev1 /sub/tok: %d", get("/sub/"+v.SubToken, "dev1"))
	a, b, cc := get("/sub/"+v.SubToken, "dev2"), get("/sub/"+v.SubToken+"/v2ray", "dev2"), get("/sub/"+v.SubToken+"/links", "dev2")
	t.Logf("dev2: /sub/tok %d, /sub/tok/v2ray %d, /sub/tok/links %d", a, b, cc)
	if a == 403 && cc == 200 {
		t.Errorf("BUG (pre-existing): /links bypasses the device limit")
	}
}

// TestAudit2ModeSwitchDuplicateBot: a reseller in Mirza mode drops out of
// RepBotsAll, so PutRepBot's "one bot, one seller" check lets another
// reseller take the same token; switching back to "api" runs it twice.
func TestAudit2ModeSwitchDuplicateBot(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.PutRep(ctx, Rep{ID: "reza", Name: "Reza", Enabled: true})
	tok := "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: tok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnablePGAPI(ctx, "ali"); err != nil { // Mirza mode: ali's bot is off
		t.Fatal(err)
	}
	err := s.PutRepBot(ctx, "reza", RepBot{Token: tok, Enabled: true})
	t.Logf("reza takes ali's token while ali is in Mirza mode: err=%v", err)
	_ = s.SetPGAPIMode(ctx, "ali", PGModeAPI)
	n := 0
	for _, b := range s.RepBotsAll(ctx) {
		if b.Token == tok {
			n++
		}
	}
	t.Logf("bots running token after ali switches to api mode: %d", n)
	if n > 1 {
		t.Errorf("BUG: two resellers poll the same Telegram bot")
	}
}

// TestAudit2StaffMintsOwnerPGAccess: a staff "admin" cannot make API keys
// (owner-only), but can make the owner's PasarGuard password, which is an
// owner-scope credential not tied to the staff login.
func TestAudit2StaffMintsOwnerPGAccess(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutStaff(ctx, "helper", "admin", "helper-pass-123"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	resp, err := http.Post(h.URL+"/api/v2/auth/login", "application/json", strings.NewReader(`{"Username":"helper","Password":"helper-pass-123"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("login", err, resp.StatusCode)
	}
	cookie := resp.Cookies()[0]
	resp.Body.Close()
	call := func(method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, h.URL+path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("X-Dark-Request", "1")
		req.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out
	}
	k, _ := call("POST", "/api/v2/admin/apikeys", `{"name":"x","level":"admin"}`)
	code, out := call("POST", "/api/v2/admin/pg-api", `{}`)
	t.Logf("staff admin: POST apikeys -> %d; POST pg-api -> %d password=%v", k, code, out["password"] != nil)
	if k == 403 && code == 200 && out["password"] != nil {
		c := pgClient{t, h.URL}
		tok := c.login("mirza", out["password"].(string))
		if code, _ := c.call("DELETE", "/api/node/1", tok, nil); code == 204 {
			t.Errorf("BUG: a staff admin minted an owner-scope API credential (and the node delete is audited as 'mirza')")
		}
	}
}
