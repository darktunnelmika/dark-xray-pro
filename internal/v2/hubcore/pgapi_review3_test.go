package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
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
	if code, _ := c.obj("PUT", "/api/user/vol1", tok, map[string]any{"on_hold_timeout": now.Add(10 * 24 * time.Hour).Unix()}); code != 403 {
		t.Fatal("on_hold_timeout under DenyEdit", code)
	}
	if code, _ := c.obj("POST", "/api/groups/bulk/add", tok, map[string]any{"group_ids": []any{g2}}); code != 403 {
		t.Fatal("groups under DenyEdit", code)
	}
	if v, _ := s.clientView(ctx, "vol1"); len(v.Inbounds) != 1 {
		t.Fatal("the groups stay", v.Inbounds)
	}
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true})
	if code, _ := c.obj("PUT", "/api/user/vol1", tok, map[string]any{"on_hold_timeout": now.Add(400 * 24 * time.Hour).Unix()}); code != 422 {
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
