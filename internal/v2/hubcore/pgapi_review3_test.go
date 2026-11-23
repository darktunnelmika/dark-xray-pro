package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPasarGuardReviewThreeMoney holds the reseller money and permission
// cases the third review found.
func TestPasarGuardReviewThreeMoney(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutInbound(ctx, "de-two", "de-1", []byte(strings.Replace(vlessTemplate, "18580", "9000", 1)), true); err != nil {
		t.Fatal(err)
	}
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	if err := s.AdjustCredit(ctx, "owner", "ali", 50<<30, 2, ""); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	credit := func() (vol, units int64) {
		_ = s.db.QueryRow(`SELECT volume_credit,unlimited_units FROM reps WHERE id='ali'`).Scan(&vol, &units)
		return
	}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	g1 := g["groups"].([]any)[0].(map[string]any)["id"]
	g2 := g["groups"].([]any)[1].(map[string]any)["id"]

	// An unlimited user topped up before it runs out pays for the days added.
	if code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": "unl1", "data_limit": 0, "expire": now.Add(30 * 24 * time.Hour).Unix(), "group_ids": []any{g1}}); code != 201 {
		t.Fatalf("create %d %v", code, out)
	}
	*now = now.Add(29 * 24 * time.Hour)
	tok = c.login("mirza_ali", a.Password)
	if code, out := c.obj("PUT", "/api/user/unl1", tok, map[string]any{"expire": now.Add(30 * 24 * time.Hour).Unix()}); code != 200 {
		t.Fatalf("top-up %d %v", code, out)
	}
	if _, u := credit(); u != 0 {
		t.Fatalf("29 days added must cost a unit: %d left of 2", u)
	}
	*now = now.Add(29 * 24 * time.Hour)
	tok = c.login("mirza_ali", a.Password)
	if code, _ := c.obj("PUT", "/api/user/unl1", tok, map[string]any{"expire": now.Add(30 * 24 * time.Hour).Unix()}); code != 400 {
		t.Fatal("no units left: the top-up is refused", code)
	}
	// Echoing a user back costs nothing.
	_, u := c.obj("GET", "/api/user/unl1", tok, nil)
	if code, out := c.obj("PUT", "/api/user/unl1", tok, u); code != 200 {
		t.Fatalf("echo %d %v", code, out)
	}

	// A reset needs the right to renew, and is written to the ledger.
	if code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": "vol1", "data_limit": 10 << 30, "expire": now.Add(30 * 24 * time.Hour).Unix(), "group_ids": []any{g1}}); code != 201 {
		t.Fatalf("create %d %v", code, out)
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('vol1',?,0,?,?)`, int64(3<<30), now.Unix(), now.Unix())
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, DenyRenew: true})
	if code, _ := c.obj("POST", "/api/user/vol1/reset", tok, nil); code != 403 {
		t.Fatal("reset with DenyRenew", code)
	}
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	before, _ := credit()
	if code, out := c.obj("POST", "/api/user/vol1/reset", tok, nil); code != 200 || out["used_traffic"].(float64) != 0 {
		t.Fatalf("reset %d %v", code, out)
	}
	var ledger int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(-volume_delta),0) FROM credit_ledger WHERE rep_id='ali' AND client_email='vol1' AND kind='charge'`).Scan(&ledger)
	if after, _ := credit(); before-after != 3<<30 || ledger != 13<<30 {
		t.Fatalf("reset charged %d, ledger charges %d", before-after, ledger)
	}

	// The owner's next plan is applied early only by the owner.
	_ = s.SetNextPlan(ctx, "vol1", &NextPlan{GB: 50, Days: 30})
	if code, _ := c.obj("POST", "/api/user/vol1/active_next", tok, nil); code != 403 {
		t.Fatal("a reseller cannot apply the owner's gift early", code)
	}

	// DenyEdit covers the on-hold timeout and the groups.
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, DenyEdit: true})
	if code, _ := c.obj("PUT", "/api/user/vol1", tok, map[string]any{"on_hold_timeout": 10 * 86400}); code != 403 {
		t.Fatal("on_hold_timeout under DenyEdit", code)
	}
	if code, _ := c.obj("POST", "/api/groups/bulk/add", tok, map[string]any{"group_ids": []any{g2}}); code != 403 {
		t.Fatal("groups under DenyEdit", code)
	}
	if v, _ := s.clientView(ctx, "vol1"); len(v.Inbounds) != 1 {
		t.Fatal("the groups stay", v.Inbounds)
	}
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	if code, _ := c.obj("PUT", "/api/user/vol1", tok, map[string]any{"on_hold_timeout": 400 * 86400}); code != 422 {
		t.Fatal("on_hold_timeout beyond 365 days", code)
	}

	// A deleted reseller's access does not pass to a new one with the same id.
	if _, err := s.DeleteRep(ctx, "ali", true); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Another Ali", Enabled: true})
	if code, _ := c.obj("GET", "/api/users", tok, nil); code != 401 {
		t.Fatal("the old token on the new reseller", code)
	}
	if s.MirzaMode(ctx, "ali") {
		t.Fatal("the new reseller's Dark bot stays on")
	}
	var stale int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pg_api WHERE seller='ali'`).Scan(&stale)
	if stale != 0 {
		t.Fatal("the access is deleted with the reseller")
	}
	// A leftover row from before this fix does not log in either.
	_, _ = s.db.Exec(`INSERT INTO pg_api(seller,username,pass_hash,enabled,created_unix) SELECT 'ali','mirza_ali',pass_hash,1,0 FROM pg_api WHERE seller='' UNION ALL SELECT 'ali','mirza_ali','x',1,0 LIMIT 1`)
	resp, _ := http.PostForm(h.URL+"/api/admin/token", map[string][]string{"username": {"mirza_ali"}, "password": {a.Password}})
	if resp.StatusCode != 401 {
		t.Fatal("a leftover access", resp.StatusCode)
	}
	resp.Body.Close()
	if a2, err := s.EnablePGAPI(ctx, "ali"); err != nil || c.login("mirza_ali", a2.Password) == "" {
		t.Fatal("the new reseller turns its own access on", err)
	}
}

// TestPasarGuardReviewThreeAPI holds the routing, edge and PasarGuard
// compatibility cases the third review found.
func TestPasarGuardReviewThreeAPI(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutInbound(ctx, "de-two", "de-1", []byte(strings.Replace(vlessTemplate, "18580", "9000", 1)), true); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	o, _ := s.EnablePGAPI(ctx, "")
	tok := c.login("mirza", o.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	g1 := g["groups"].([]any)[0].(map[string]any)["id"]
	g2 := g["groups"].([]any)[1].(map[string]any)["id"]
	exp := now.Add(30 * 24 * time.Hour).Unix()
	code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": "carol", "data_limit": 1073741824.0, "expire": exp, "group_ids": []any{g2}, "data_limit_reset_strategy": "year"})
	if code != 201 || out["data_limit"].(float64) != 1<<30 || out["data_limit_reset_strategy"] != "year" {
		t.Fatalf("create (float limit, yearly reset) %d %v", code, out)
	}
	id := out["id"].(float64)

	// PUT follows PasarGuard: Mirza's "inbounds" is ignored, null and [] change nothing.
	if code, out := c.obj("PUT", "/api/user/carol", tok, map[string]any{"data_limit": 2 << 30, "inbounds": []any{g1}, "expire": nil, "group_ids": []any{}}); code != 200 ||
		out["group_ids"].([]any)[0] != g2 || out["expire"] == nil {
		t.Fatalf("put %d %v", code, out)
	}
	if code, _ := c.obj("PUT", "/api/user/carol", tok, map[string]any{"data_limit": -5}); code != 422 {
		t.Fatal("a negative limit", code)
	}
	for _, v := range []any{1e300, -100, 1.8e12} {
		if code, _ := c.obj("POST", "/api/user", tok, map[string]any{"username": "edge1", "expire": v, "group_ids": []any{g1}}); code != 422 {
			t.Fatalf("expire %v: %d", v, code)
		}
	}
	// username= is exact; search finds parts.
	_ = s.PutClient(ctx, Client{Email: "carol2", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}})
	if _, out := c.obj("GET", "/api/users?username=carol", tok, nil); out["total"].(float64) != 1 {
		t.Fatalf("exact %v", out["total"])
	}
	if _, out := c.obj("GET", "/api/users?search=carol", tok, nil); out["total"].(float64) != 2 {
		t.Fatalf("search %v", out["total"])
	}
	// Paging never panics; junk sort keys are dropped.
	for _, p := range []string{"/api/users?offset=1&limit=9223372036854775807", "/api/users/simple?offset=1&limit=9223372036854775807",
		"/api/groups/simple?offset=1&limit=9223372036854775807", "/api/users?sort=x,x,-x,username,username"} {
		if code, _ := c.call("GET", p, tok, nil); code != 200 {
			t.Fatal(p, code)
		}
	}
	// Bulk expire never wraps an expiry to "no expiry".
	if code, _ := c.obj("POST", "/api/users/bulk/expire", tok, map[string]any{"amount": int64(1) << 62}); code != 422 {
		t.Fatal("a huge amount", code)
	}
	if code, _ := c.obj("POST", "/api/users/bulk/expire", tok, map[string]any{"amount": -(exp + 3600), "users": []any{id}}); code != 200 {
		t.Fatal("a large negative amount", code)
	}
	if v, _ := s.clientView(ctx, "carol"); v.ExpiresUnix != 1 {
		t.Fatal("it ends at once", v.ExpiresUnix)
	}
	// Node numbers agree between /api/nodes and the usage charts.
	_, _ = s.CreateNode(ctx, "aa-2", "A")
	_, _ = s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('alice','aa-2',?,100,0)`, now.UTC().Format("2006-01-02"))
	_, nodes := c.call("GET", "/api/nodes", tok, nil)
	var aaID any
	for _, n := range nodes.([]any) {
		if n.(map[string]any)["name"] == "A" {
			aaID = n.(map[string]any)["id"]
		}
	}
	_, us := c.obj("GET", fmt.Sprintf("/api/users/usage?node_id=%v", aaID), tok, nil)
	if st := us["stats"].(map[string]any)["-1"].([]any); len(st) != 1 || st[0].(map[string]any)["total_traffic"].(float64) != 100 {
		t.Fatalf("node %v usage %v", aaID, us)
	}
	// A HEAD is not a change.
	req, _ := http.NewRequest("HEAD", h.URL+"/api/user/alice", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	var audits int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE method='HEAD'`).Scan(&audits)
	if audits != 0 {
		t.Fatal("HEAD was audited")
	}
	// sub_update pages and counts all.
	for i := 0; i < 3; i++ {
		_ = s.CheckDevice(ctx, "alice", fmt.Sprintf("dev%d", i), "", "", "ua")
	}
	if _, out := c.obj("GET", "/api/user/alice/sub_update?offset=0&limit=1", tok, nil); out["count"].(float64) != 3 || len(out["updates"].([]any)) != 1 {
		t.Fatalf("sub_update %v", out)
	}
	// Bulk template delete with an id twice.
	code, out = c.obj("POST", "/api/user_template", tok, map[string]any{"name": "T", "data_limit": 1 << 30, "expire_duration": 86400 * 30, "group_ids": []any{g1}})
	if code != 201 {
		t.Fatalf("template %d %v", code, out)
	}
	if code, out := c.obj("POST", "/api/user_templates/bulk/delete", tok, map[string]any{"ids": []any{out["id"], out["id"]}}); code != 200 || out["count"].(float64) != 1 {
		t.Fatalf("dup delete %d %v", code, out)
	}

	// Links moved from Marzban answer on every form; PasarGuard's trailing slash too.
	secret := "sekret"
	_ = s.SetSetting(ctx, "legacy_marzban_secret", secret)
	body := strings.TrimRight(base64.URLEncoding.EncodeToString([]byte("alice,1700000000")), "=")
	sum := sha256.Sum256([]byte(body + secret))
	mz := body + base64.URLEncoding.EncodeToString(sum[:])[:10]
	if _, err := s.db.Exec(`INSERT INTO legacy_sub_tokens(token_sha,kind,email) VALUES(?,?,?)`, legacyTokenSHA("marzban-user", "alice"), "marzban-user", "alice"); err != nil {
		t.Fatal(err)
	}
	v, _ := s.clientView(ctx, "alice")
	for _, p := range []string{"/sub/" + mz + "/links", "/sub/" + mz + "/v2ray", "/sub/" + mz + "/sing-box", "/sub/" + v.SubToken + "/", "/sub/" + v.SubToken + "/raw"} {
		resp, err := http.Get(h.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(strings.Replace(p, mz, "<marzban>", 1), resp.StatusCode)
		}
	}
}

// TestRestoreKeepsRowIDMarks: a restored older backup does not give out
// again the ids of users made after it.
func TestRestoreKeepsRowIDMarks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	if err := s.SnapshotTo(ctx, path+".pending"); err != nil {
		t.Fatal(err)
	}
	_ = s.PutClient(ctx, Client{Email: "carol", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}})
	var carol int64
	_ = s.db.QueryRow(`SELECT rowid FROM clients WHERE email='carol'`).Scan(&carol)
	s.Close()
	if ok, err := ApplyPendingRestore(path); !ok || err != nil {
		t.Fatal(ok, err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_ = r.PutClient(ctx, Client{Email: "dave", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}})
	var dave int64
	_ = r.db.QueryRow(`SELECT rowid FROM clients WHERE email='dave'`).Scan(&dave)
	if dave <= carol {
		t.Fatalf("dave got id %d, carol had %d", dave, carol)
	}
}

// TestPasarGuardReviewFour holds the four items left after the third
// review: an expired reseller user, node management, the owner's gift and
// names in the audit log.
func TestPasarGuardReviewFour(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, RefundUnused: true})
	if err := s.AdjustCredit(ctx, "owner", "ali", 100<<30, 5, ""); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	credit := func() (vol, units int64) {
		_ = s.db.QueryRow(`SELECT volume_credit,unlimited_units FROM reps WHERE id='ali'`).Scan(&vol, &units)
		return
	}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	gid := g["groups"].([]any)[0].(map[string]any)["id"]
	exp := now.Add(30 * 24 * time.Hour).Unix()
	mk := func(name string, gb int64) float64 {
		t.Helper()
		code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": name, "data_limit": gb << 30, "expire": exp, "group_ids": []any{gid}})
		if code != 201 {
			t.Fatalf("create %s %d %v", name, code, out)
		}
		return out["id"].(float64)
	}

	// 1. Extra volume on an expired user, the way Mirza sells it.
	mk("old1", 10)
	_, _ = s.db.Exec(`UPDATE clients SET expires_unix=? WHERE email='old1'`, now.Add(-time.Hour).Unix())
	before, _ := credit()
	if code, out := c.obj("PUT", "/api/user/old1", tok, map[string]any{"data_limit": 15 << 30, "proxy_settings": map[string]any{}}); code != 200 || out["data_limit"].(float64) != 15<<30 {
		t.Fatalf("extra volume on an expired user %d %v", code, out)
	}
	if after, _ := credit(); before-after != 5<<30 {
		t.Fatalf("the 5 GB added are charged: %d", before-after)
	}
	if code, _ := c.obj("POST", "/api/user", tok, map[string]any{"username": "past1", "data_limit": 1 << 30, "expire": now.Add(-time.Hour).Unix(), "group_ids": []any{gid}}); code != 422 {
		t.Fatal("a new user already expired", code)
	}
	if code, _ := c.obj("PUT", "/api/user/old1", tok, map[string]any{"expire": 0}); code != 422 {
		t.Fatal("a reseller's user without any term", code)
	}

	// 3. The owner's next plan is a gift: renewals, deletes and the expiry
	// refund never pay it out.
	mk("gift1", 10) // paid 10 GB
	_ = s.SetNextPlan(ctx, "gift1", &NextPlan{GB: 50, Days: 30})
	v, _ := s.clientView(ctx, "gift1")
	if err := s.applyNextPlan(ctx, "gift1", *v.NextPlan); err != nil {
		t.Fatal(err)
	}
	vol0, _ := credit()
	if _, err := s.RenewClient(ctx, Principal{User: "ali", Role: "rep", RepID: "ali"}, "gift1", "volume", 1, 30); err != nil {
		t.Fatal(err)
	}
	if vol1, _ := credit(); vol0-vol1 != 1<<30 {
		t.Fatalf("a renewal of a gift pays 1 GB and gets nothing back: %d", vol0-vol1)
	}
	mk("gift2", 10)
	_ = s.SetNextPlan(ctx, "gift2", &NextPlan{GB: 50, Days: 30})
	v, _ = s.clientView(ctx, "gift2")
	_ = s.applyNextPlan(ctx, "gift2", *v.NextPlan)
	if code, _ := c.obj("PUT", "/api/user/gift2", tok, map[string]any{"data_limit": 60 << 30}); code != 200 {
		t.Fatal("paid volume on top of a gift", code)
	}
	vol0, _ = credit()
	if code, _ := c.obj("DELETE", "/api/user/gift2", tok, nil); code != 200 {
		t.Fatal("delete", code)
	}
	if vol1, _ := credit(); vol1-vol0 != 10<<30 {
		t.Fatalf("only the paid 10 GB comes back, not the gift: %d", vol1-vol0)
	}
	mk("gift3", 10)
	_ = s.SetNextPlan(ctx, "gift3", &NextPlan{GB: 0, Days: 60}) // unlimited, two units
	v, _ = s.clientView(ctx, "gift3")
	_ = s.applyNextPlan(ctx, "gift3", *v.NextPlan)
	_, u0 := credit()
	if code, _ := c.obj("DELETE", "/api/user/gift3", tok, nil); code != 200 {
		t.Fatal("delete", code)
	}
	if _, u1 := credit(); u1 != u0 {
		t.Fatalf("a gifted unlimited term gives no units back: %d -> %d", u0, u1)
	}
	mk("gift4", 10)
	_ = s.SetNextPlan(ctx, "gift4", &NextPlan{GB: 50, Days: 30})
	v, _ = s.clientView(ctx, "gift4")
	_ = s.applyNextPlan(ctx, "gift4", *v.NextPlan)
	_, _ = s.db.Exec(`UPDATE clients SET expires_unix=? WHERE email='gift4'`, now.Add(-time.Hour).Unix())
	s.runExpiryRefunds(ctx)
	var giftBack, paidBack int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(volume_delta),0) FROM credit_ledger WHERE client_email='gift4' AND kind='refund' AND note='expired: unused volume'`).Scan(&giftBack)
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(volume_delta),0) FROM credit_ledger WHERE client_email='old1' AND kind='refund'`).Scan(&paidBack)
	if giftBack != 0 || paidBack != 15<<30-1 {
		t.Fatalf("the expiry refund pays no gift (%d) but does pay unused paid volume (%d)", giftBack, paidBack)
	}

	// 4. Changes by id or in bulk are found by the user's name in the audit log.
	id1, id2 := mk("audit1", 1), mk("audit2", 1)
	if code, _ := c.obj("PUT", fmt.Sprintf("/api/user/by-id/%d/disabled", int64(id1)), tok, map[string]any{"disabled": true}); code != 200 {
		t.Fatal("disable", code)
	}
	if code, _ := c.obj("POST", "/api/users/bulk/revoke_sub", tok, map[string]any{"ids": []any{id1, id2}}); code != 200 {
		t.Fatal("bulk", code)
	}
	for name, want := range map[string]int{"audit1": 3, "audit2": 2} { // create + changes
		if list, _ := s.AuditLog(ctx, "", name, 0, 100); len(list) != want {
			t.Fatalf("audit for %s: %d entries, want %d: %+v", name, len(list), want, list)
		}
	}

	// 2. Node management, the owner's only.
	if code, _ := c.obj("GET", "/api/node/1", tok, nil); code != 403 {
		t.Fatal("a reseller does not manage nodes", code)
	}
	o, _ := s.EnablePGAPI(ctx, "")
	otok := c.login("mirza", o.Password)
	_, _ = s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('alice','de-1',?,100,300)`, now.UTC().Format("2006-01-02"))
	code, node := c.obj("GET", "/api/node/1", otok, nil)
	if code != 200 || node["name"] != "Germany" || node["uplink"].(float64) != 100 || node["downlink"].(float64) != 300 || node["usage_coefficient"].(float64) != 1 {
		t.Fatalf("node %d %v", code, node)
	}
	code, node = c.obj("PUT", "/api/node/1", otok, map[string]any{"name": "Frankfurt", "usage_coefficient": 1.5, "address": "8.8.4.4"})
	if code != 200 || node["name"] != "Frankfurt" || node["usage_coefficient"].(float64) != 1.5 || node["address"] != "8.8.4.4" {
		t.Fatalf("modify node %d %v", code, node)
	}
	if code, _ := c.obj("PUT", "/api/node/1", otok, map[string]any{"name": "X", "usage_coefficient": 50}); code != 422 {
		t.Fatal("a bad multiplier", code)
	}
	if _, nv := c.obj("GET", "/api/node/1", otok, nil); nv["name"] != "Frankfurt" {
		t.Fatal("a refused change leaves the node as it was", nv["name"])
	}
	if code, _ := c.obj("POST", "/api/node/1/reconnect", otok, nil); code != 200 {
		t.Fatal("reconnect", code)
	}
	var tasks int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM node_tasks WHERE node_id='de-1'`).Scan(&tasks)
	if tasks != 1 {
		t.Fatal("reconnect restarts the node's Xray", tasks)
	}
	if _, us := c.obj("GET", "/api/nodes/usage", otok, nil); len(us["usages"].([]any)) != 1 || us["usages"].([]any)[0].(map[string]any)["node_id"].(float64) != 1 {
		t.Fatalf("nodes usage %v", us)
	}
	if code, _ := c.obj("GET", "/api/node/99", otok, nil); code != 404 {
		t.Fatal("unknown node", code)
	}
	// A node holding the only copy of inbounds (the groups bots sell) is not
	// deleted through the API; an empty one is.
	if code, _ := c.call("DELETE", "/api/node/1", otok, nil); code != 409 {
		t.Fatal("delete a node with inbounds", code)
	}
	if _, err := s.CreateNode(ctx, "spare", "Spare"); err != nil {
		t.Fatal(err)
	}
	var spare int64
	_ = s.db.QueryRow(`SELECT rowid FROM nodes WHERE id='spare'`).Scan(&spare)
	if code, _ := c.call("DELETE", fmt.Sprintf("/api/node/%d", spare), otok, nil); code != 204 {
		t.Fatal("delete an empty node", code)
	}
	if nodes, _ := s.Nodes(ctx); len(nodes) != 1 {
		t.Fatal("only the empty node is gone", len(nodes))
	}
}

// TestOverdueBillStopsRenewAndReset: a reseller with a bill past its due
// time sells nothing, renewals and the API's usage reset included.
func TestOverdueBillStopsRenewAndReset(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 50<<30, 0, "")
	if err := s.RepPutClient(ctx, "ali", "ali", Client{Email: "cust1", Enabled: true, VolumeBytes: 5 << 30, ExpiresUnix: now.Add(30 * 24 * time.Hour).Unix(), Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('cust1',?,0,?,?)`, int64(1<<30), now.Unix(), now.Unix())
	_, _ = s.db.Exec(`INSERT INTO rep_bills(unix,rep_id,amount,due_unix) VALUES(?,?,?,?)`, now.Unix(), "ali", 1000, now.Add(-time.Hour).Unix())
	if !s.RepLocked(ctx, "ali") {
		t.Fatal("the bill is overdue")
	}
	if _, err := s.RenewClient(ctx, Principal{User: "ali", Role: "rep", RepID: "ali"}, "cust1", "volume", 5, 30); err == nil {
		t.Fatal("a renewal is a sale")
	}
	if err := s.pgResetRep(ctx, "ali", "cust1"); err == nil {
		t.Fatal("a reset gives the volume again")
	}
}

// TestPGAPIModes: a reseller's access is for Mirza (its Dark bot off) or
// for any PasarGuard client (its Dark bot on), switched from its panel
// without a new password.
func TestPGAPIModes(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if err := s.SetRepLogin(ctx, "ali", "pass-12345678", "ali"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	resp, err := http.Post(h.URL+"/api/v2/auth/login", "application/json", strings.NewReader(`{"Username":"ali","Password":"pass-12345678"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("login", err)
	}
	cookie := resp.Cookies()[0]
	resp.Body.Close()
	panel := func(method, body string) (int, map[string]any) {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(method, h.URL+"/api/v2/rep/pg-api", rd)
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
	c := pgClient{t, h.URL}

	// The plain API: the Dark bot stays on.
	code, a := panel("POST", `{"mode":"api"}`)
	if code != 200 || a["mode"] != "api" || a["password"] == "" || s.MirzaMode(ctx, "ali") {
		t.Fatalf("api mode %d %v mirza=%v", code, a, s.MirzaMode(ctx, "ali"))
	}
	pass := a["password"].(string)
	tok := c.login("mirza_ali", pass)
	if code, _ := c.obj("GET", "/api/users", tok, nil); code != 200 {
		t.Fatal("the API works", code)
	}
	// Switching to Mirza keeps the password and the sessions; the Dark bot goes off.
	if code, a := panel("PUT", `{"mode":"mirza"}`); code != 200 || a["mode"] != "mirza" || a["password"] != nil || !s.MirzaMode(ctx, "ali") {
		t.Fatalf("mirza mode %d %v", code, a)
	}
	if code, _ := c.obj("GET", "/api/users", tok, nil); code != 200 {
		t.Fatal("the session survives a switch", code)
	}
	c.login("mirza_ali", pass)
	if code, _ := panel("PUT", `{"mode":"other"}`); code != 400 {
		t.Fatal("a bad mode", code)
	}
	// A new password without a mode keeps the mode; a new access is Mirza's.
	if code, a := panel("POST", ""); code != 200 || a["mode"] != "mirza" {
		t.Fatalf("new password %d %v", code, a)
	}
	if code, _ := panel("DELETE", ""); code != 200 || s.MirzaMode(ctx, "ali") {
		t.Fatal("off", code)
	}
	if a := s.PGAPIOf(ctx, ""); a.Mode != "" {
		t.Fatal("the owner's access was never made", a.Mode)
	}
	o, _ := s.EnablePGAPI(ctx, "")
	if a := s.PGAPIOf(ctx, ""); a.Mode != "api" || o.Password == "" {
		t.Fatal("the owner's access never turns a bot off", a.Mode)
	}
	if err := s.SetPGAPIMode(ctx, "", "mirza"); err == nil {
		t.Fatal("the owner's access has no mode")
	}
	// The owner's card sends the API mode with "Create": it is accepted.
	oh := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer oh.Close()
	req, _ := http.NewRequest("POST", oh.URL+"/api/v2/admin/pg-api", strings.NewReader(`{"mode":"api"}`))
	req.Header.Set("Authorization", "Bearer admintok")
	req.Header.Set("Content-Type", "application/json")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 200 {
		t.Fatal("owner create with a mode", err, r.StatusCode)
	} else {
		r.Body.Close()
	}
}

// TestYearlyResetSafeForOlderPanels: a yearly reset is kept out of
// reset_period, which a panel older than v2.54 (after a rollback) would read
// as due every minute; values stored as "year" before move over.
func TestYearlyResetSafeForOlderPanels(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.SetResetPeriod(ctx, "alice", "year"); err != nil {
		t.Fatal(err)
	}
	var col string
	var year bool
	_ = s.db.QueryRow(`SELECT reset_period,reset_year FROM clients WHERE email='alice'`).Scan(&col, &year)
	if col != "" || !year {
		t.Fatalf("stored as %q/%v", col, year)
	}
	if v, _ := s.clientView(ctx, "alice"); v.ResetPeriod != "year" {
		t.Fatal("shown as", v.ResetPeriod)
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('alice',500,0,?,?)`, now.Unix(), now.Unix())
	*now = now.Add(30 * 24 * time.Hour)
	if n := s.runDueResets(ctx); n != 0 {
		t.Fatal("not due within the year", n)
	}
	*now = now.Add(340 * 24 * time.Hour)
	if n := s.runDueResets(ctx); n != 1 {
		t.Fatal("due after a year", n)
	}
	if err := s.SetResetPeriod(ctx, "alice", "month"); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRow(`SELECT reset_period,reset_year FROM clients WHERE email='alice'`).Scan(&col, &year)
	if col != "month" || year {
		t.Fatalf("back to monthly: %q/%v", col, year)
	}
	// A value written by v2.54.0/v2.54.1 moves at start.
	_, _ = s.db.Exec(`UPDATE clients SET reset_period='year', reset_year=0 WHERE email='bob'`)
	if err := s.ensureResetSchema(); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRow(`SELECT reset_period,reset_year FROM clients WHERE email='bob'`).Scan(&col, &year)
	if col != "" || !year {
		t.Fatalf("moved: %q/%v", col, year)
	}
}

// TestStaleAccessShownOff: an access left by a deleted reseller (before
// v2.54.1 deleted it) is not shown as on and does not keep the new
// reseller's Dark bot off.
func TestStaleAccessShownOff(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if _, err := s.EnablePGAPI(ctx, "ali"); err != nil {
		t.Fatal(err)
	}
	// As v2.53.3 left it: the reseller deleted (without the access) and made again.
	_, _ = s.db.Exec(`DELETE FROM reps WHERE id='ali'`)
	*now = now.Add(time.Hour)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "New Ali", Enabled: true})
	if a := s.PGAPIOf(ctx, "ali"); a.Enabled || a.Mode != "" {
		t.Fatalf("shown as %+v", a)
	}
	if s.MirzaMode(ctx, "ali") {
		t.Fatal("the new reseller's Dark bot stays on")
	}
	if a, err := s.EnablePGAPI(ctx, "ali"); err != nil || !a.Enabled || a.Mode != "mirza" || a.Password == "" {
		t.Fatalf("a fresh access %+v %v", a, err)
	}
}

// TestPGAPIKey: an API key as PasarGuard's (pg_key_..., in X-Api-Key or
// "Authorization: ApiKey"), shown once, renewed apart from the password.
func TestPGAPIKey(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 10<<30, 0, "")
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	get := func(header, value string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", h.URL+"/api/users", nil)
		if header != "" {
			req.Header.Set(header, value)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&out)
		return r.StatusCode, out
	}
	a, err := s.EnablePGAPI(ctx, "ali")
	if err != nil || !a.HasKey || !strings.HasPrefix(a.APIKey, "pg_key_") || a.Password == "" {
		t.Fatalf("first access: %+v %v", a, err)
	}
	key := a.APIKey
	if code, _ := get("X-Api-Key", key); code != 200 {
		t.Fatal("X-Api-Key", code)
	}
	if code, _ := get("Authorization", "ApiKey "+key); code != 200 {
		t.Fatal("Authorization: ApiKey", code)
	}
	if code, _ := get("X-Api-Key", "pg_key_wrong"); code != 401 {
		t.Fatal("a wrong key", code)
	}
	if code, _ := get("Authorization", "Bearer "+key); code != 401 {
		t.Fatal("a key is not a bearer token", code)
	}
	if a := s.PGAPIOf(ctx, "ali"); a.APIKey != "" || !a.HasKey {
		t.Fatal("the key is shown only once", a)
	}
	// A reseller's key reaches only its users; it makes them on its credit.
	pc := pgClient{t, h.URL}
	req, _ := http.NewRequest("POST", h.URL+"/api/user", strings.NewReader(fmt.Sprintf(`{"username":"keyuser","data_limit":%d,"expire":%d}`, 1<<30, now.Add(24*time.Hour).Unix())))
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Content-Type", "application/json")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 201 {
		t.Fatal("create with the key", err, r.StatusCode)
	}
	if _, out := get("X-Api-Key", key); out["total"].(float64) != 1 {
		t.Fatal("only its own users", out["total"])
	}
	// A new password keeps the key; a new key ends the old one and keeps the password.
	b, _ := s.EnablePGAPI(ctx, "ali")
	if b.APIKey != "" {
		t.Fatal("a new password does not make a new key")
	}
	if code, _ := get("X-Api-Key", key); code != 200 {
		t.Fatal("the key outlives a new password", code)
	}
	key2, err := s.NewPGAPIKey(ctx, "ali")
	if err != nil || key2 == key {
		t.Fatal("new key", err)
	}
	if code, _ := get("X-Api-Key", key); code != 401 {
		t.Fatal("the old key stops", code)
	}
	if code, _ := get("X-Api-Key", key2); code != 200 {
		t.Fatal("the new key works", code)
	}
	pc.login("mirza_ali", b.Password)
	// Off: the key stops; a deleted reseller's key never works for a new one.
	_ = s.DisablePGAPI(ctx, "ali")
	if code, _ := get("X-Api-Key", key2); code != 401 {
		t.Fatal("off", code)
	}
	if _, err := s.NewPGAPIKey(ctx, "ali"); err == nil {
		t.Fatal("no key while the access is off")
	}
}
