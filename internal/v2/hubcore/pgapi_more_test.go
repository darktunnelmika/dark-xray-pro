package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type pgClient struct {
	t   *testing.T
	url string
}

func (c pgClient) call(method, path, tok string, body any) (int, any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, c.url+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (c pgClient) obj(method, path, tok string, body any) (int, map[string]any) {
	c.t.Helper()
	code, out := c.call(method, path, tok, body)
	m, _ := out.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return code, m
}

func (c pgClient) login(user, pass string) string {
	c.t.Helper()
	resp, err := http.PostForm(c.url+"/api/admin/token", url.Values{"username": {user}, "password": {pass}})
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	tok, _ := out["access_token"].(string)
	if tok == "" {
		c.t.Fatal("login", user, resp.StatusCode)
	}
	return tok
}

// TestPasarGuardFullUserAPI walks the rest of PasarGuard's user API for a
// reseller token and the owner's, with the reseller's credit rules.
func TestPasarGuardFullUserAPI(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if err := s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, ""); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	credit := func() int64 {
		var v int64
		_ = s.db.QueryRow(`SELECT volume_credit FROM reps WHERE id='ali'`).Scan(&v)
		return v
	}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	gid := g["groups"].([]any)[0].(map[string]any)["id"]
	exp := now.Add(30 * 24 * time.Hour).Unix()

	mk := func(name string, gb int64) float64 {
		t.Helper()
		code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": name, "data_limit": gb << 30, "expire": exp, "group_ids": []any{gid}})
		if code != 200 {
			t.Fatalf("create %s %d %v", name, code, out)
		}
		return out["id"].(float64)
	}
	id1 := mk("usr1", 10)
	id2 := mk("usr2", 10)
	if credit() != 80<<30 {
		t.Fatal("credit", credit())
	}

	// By id and by username.
	if code, out := c.obj("GET", fmt.Sprintf("/api/user/by-id/%d", int64(id1)), tok, nil); code != 200 || out["username"] != "usr1" {
		t.Fatalf("by-id %d %v", code, out)
	}
	if code, out := c.obj("GET", "/api/user/by-username/usr2", tok, nil); code != 200 || out["id"].(float64) != id2 {
		t.Fatalf("by-username %d %v", code, out)
	}
	if code, _ := c.obj("GET", "/api/user/by-id/abc", tok, nil); code != 404 {
		t.Fatal("a bad id", code)
	}
	if code, _ := c.obj("GET", "/api/user/usr1/nothing", tok, nil); code != 404 {
		t.Fatal("an unknown action", code)
	}
	var aliceID int64
	_ = s.db.QueryRow(`SELECT rowid FROM clients WHERE email='alice'`).Scan(&aliceID)
	if code, _ := c.obj("GET", fmt.Sprintf("/api/user/by-id/%d", aliceID), tok, nil); code != 404 {
		t.Fatal("the owner's client is invisible by id too", code)
	}

	// On and off, free of charge.
	if code, out := c.obj("PUT", "/api/user/usr1/disabled", tok, map[string]any{"disabled": true}); code != 200 || out["status"] != "disabled" {
		t.Fatalf("disable %d %v", code, out)
	}
	if code, out := c.obj("PUT", fmt.Sprintf("/api/user/by-id/%d/disabled", int64(id1)), tok, map[string]any{"disabled": false}); code != 200 || out["status"] != "active" {
		t.Fatalf("enable %d %v", code, out)
	}
	if credit() != 80<<30 {
		t.Fatal("on/off must not charge", credit())
	}

	// What a reseller may and may not set.
	if code, _ := c.obj("PUT", "/api/user/usr1", tok, map[string]any{"data_limit_reset_strategy": "month"}); code != 403 {
		t.Fatal("a periodic reset gives unpaid volume", code)
	}
	if code, _ := c.obj("PUT", "/api/user/usr1", tok, map[string]any{"next_plan": map[string]any{"data_limit": 1 << 30}}); code != 403 {
		t.Fatal("a next plan gives unpaid service", code)
	}
	if code, out := c.obj("PUT", "/api/user/usr1", tok, map[string]any{"hwid_limit": 3}); code != 200 || out["hwid_limit"].(float64) != 3 {
		t.Fatalf("hwid %d %v", code, out)
	}
	if code, _ := c.obj("PUT", "/api/user/usr1/set_owner?admin_username=owner", tok, nil); code != 403 {
		t.Fatal("only the owner moves users", code)
	}
	if code, _ := c.obj("POST", "/api/users/reset", tok, nil); code != 403 {
		t.Fatal("resetting everyone is the owner's", code)
	}

	// Lists.
	if code, out := c.obj("GET", "/api/users/simple?sort=-username", tok, nil); code != 200 || out["total"].(float64) != 2 ||
		out["users"].([]any)[0].(map[string]any)["username"] != "usr2" {
		t.Fatalf("simple %d %v", code, out)
	}
	if code, out := c.obj("GET", "/api/users?sort=username&limit=1&offset=1", tok, nil); code != 200 || out["total"].(float64) != 2 ||
		out["users"].([]any)[0].(map[string]any)["username"] != "usr2" {
		t.Fatalf("sorted page %d %v", code, out)
	}
	if code, out := c.obj("GET", "/api/users?search=usr1", tok, nil); code != 200 || out["total"].(float64) != 1 {
		t.Fatalf("search %d %v", code, out)
	}
	if code, out := c.obj("GET", "/api/users?status=active&status=disabled", tok, nil); code != 200 || out["total"].(float64) != 2 {
		t.Fatalf("statuses %d %v", code, out)
	}

	// Usage per day.
	day := now.UTC().Format("2006-01-02")
	_, _ = s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('usr1','de-1',?,100,200)`, day)
	_, out := c.obj("GET", "/api/user/usr1/usage", tok, nil)
	series, _ := out["stats"].(map[string]any)["-1"].([]any)
	if len(series) != 1 || series[0].(map[string]any)["total_traffic"].(float64) != 300 {
		t.Fatalf("usage %v", out)
	}
	_, out = c.obj("GET", "/api/users/usage?group_by_node=true", tok, nil)
	if len(out["stats"].(map[string]any)) != 1 || out["stats"].(map[string]any)["-1"] != nil {
		t.Fatalf("usage by node %v", out)
	}
	if code, out := c.obj("GET", "/api/users/counts/online", tok, nil); code != 200 || out["count_during_period"].(float64) != 1 {
		t.Fatalf("online count %d %v", code, out)
	}

	// Bulk limits: a dry run changes nothing, the real one is charged.
	if code, out := c.obj("POST", "/api/users/bulk/data_limit", tok, map[string]any{"amount": 1 << 30, "dry_run": true}); code != 200 || out["affected_users"].(float64) != 2 {
		t.Fatalf("dry run %d %v", code, out)
	}
	if code, out := c.obj("POST", "/api/users/bulk/data_limit", tok, map[string]any{"amount": 1 << 30, "users": []any{id1}}); code != 200 || credit() != 79<<30 {
		t.Fatalf("bulk data limit %d %v credit %d", code, out, credit())
	}
	if code, out := c.obj("POST", "/api/users/bulk/expire", tok, map[string]any{"amount": 86400}); code != 200 {
		t.Fatalf("bulk expire %d %v", code, out)
	}
	if v, _ := s.clientView(ctx, "usr2"); v.ExpiresUnix != exp+86400 {
		t.Fatal("bulk expire adds the seconds", v.ExpiresUnix)
	}
	// Bulk by ids.
	if code, out := c.obj("POST", "/api/users/bulk/disable", tok, map[string]any{"ids": []any{id1, id2}}); code != 200 || out["count"].(float64) != 2 {
		t.Fatalf("bulk disable %d %v", code, out)
	}
	if code, _ := c.obj("POST", "/api/users/bulk/enable", tok, map[string]any{"ids": []any{id1, aliceID}}); code != 404 {
		t.Fatal("someone else's id stops the whole call", code)
	}
	if v, _ := s.clientView(ctx, "usr1"); v.Enabled {
		t.Fatal("nothing changes when one id is not the seller's")
	}
	if code, out := c.obj("POST", "/api/users/bulk/enable", tok, map[string]any{"ids": []any{id1, id2}}); code != 200 || out["count"].(float64) != 2 {
		t.Fatalf("bulk enable %d %v", code, out)
	}

	// Templates are the reseller's shop products, under the same permission.
	_ = s.SetManageProducts(ctx, "ali", false)
	if code, _ := c.obj("POST", "/api/user_template", tok, map[string]any{"name": "5G", "data_limit": 5 << 30, "expire_duration": 30 * 86400, "group_ids": []any{gid}}); code != 403 {
		t.Fatal("making products needs the owner's permission", code)
	}
	_ = s.SetManageProducts(ctx, "ali", true)
	code, out := c.obj("POST", "/api/user_template", tok, map[string]any{"name": "5G", "data_limit": 5 << 30, "expire_duration": 30 * 86400, "group_ids": []any{gid}})
	if code != 201 || out["data_limit"].(float64) != 5<<30 || out["expire_duration"].(float64) != 30*86400 {
		t.Fatalf("template %d %v", code, out)
	}
	tid := out["id"]
	if code, list := c.call("GET", "/api/user_templates", tok, nil); code != 200 || len(list.([]any)) != 1 {
		t.Fatalf("templates %d %v", code, list)
	}
	code, out = c.obj("POST", "/api/user/from_template", tok, map[string]any{"user_template_id": tid, "username": "tpl1"})
	if code != 201 || out["data_limit"].(float64) != 5<<30 || credit() != 74<<30 {
		t.Fatalf("from template %d %v credit %d", code, out, credit())
	}
	code, out = c.obj("PUT", "/api/user/from_template/tpl1", tok, map[string]any{"user_template_id": tid})
	if code != 200 || out["data_limit"].(float64) != 5<<30 || credit() != 74<<30 {
		t.Fatalf("renew from template (unused volume comes back) %d %v credit %d", code, out, credit())
	}
	if code, out := c.obj("PUT", fmt.Sprintf("/api/user_template/%v", tid), tok, map[string]any{"is_disabled": true}); code != 200 || out["is_disabled"] != true {
		t.Fatalf("disable template %d %v", code, out)
	}
	if code, _ := c.obj("POST", "/api/user/from_template", tok, map[string]any{"user_template_id": tid, "username": "tpl2"}); code != 403 {
		t.Fatal("a disabled template sells nothing", code)
	}
	if code, _ := c.obj("POST", "/api/user/from_template", tok, map[string]any{"user_template_id": 999, "username": "tpl2"}); code != 404 {
		t.Fatal("unknown template", code)
	}
	_, _ = c.obj("POST", "/api/user_templates/bulk/enable", tok, map[string]any{"ids": []any{tid}})
	code, out = c.obj("POST", "/api/users/bulk/from_template", tok, map[string]any{"user_template_id": tid, "username": "bulk", "strategy": "sequence", "count": 2})
	if code != 200 || out["created"].(float64) != 2 || credit() != 64<<30 {
		t.Fatalf("bulk from template %d %v credit %d", code, out, credit())
	}
	if _, err := s.clientView(ctx, "bulk2"); err != nil {
		t.Fatal("sequence names", err)
	}

	// Groups.
	if code, out := c.obj("GET", fmt.Sprintf("/api/group/%v", gid), tok, nil); code != 200 || out["name"] != "de-vless" {
		t.Fatalf("group %d %v", code, out)
	}
	if code, list := c.call("GET", "/api/inbounds", tok, nil); code != 200 || len(list.([]any)) != 1 {
		t.Fatalf("inbounds %d %v", code, list)
	}

	// Subscription by id, and the shared info page shows no credentials.
	if code, _ := c.call("GET", fmt.Sprintf("/api/user/%d/subscription/links", int64(id1)), tok, nil); code != 200 {
		t.Fatal("subscription by id", code)
	}
	v1, _ := s.clientView(ctx, "usr1")
	hs := httptest.NewServer(SubHandler(s))
	defer hs.Close()
	resp, err := http.Get(hs.URL + "/sub/" + v1.SubToken + "/info")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("sub info", err)
	}
	var info map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()
	if info["username"] != "usr1" || info["proxy_settings"] != nil || info["subscription_url"] != nil {
		t.Fatalf("sub info %v", info)
	}

	// Cleanup and bulk delete go through the reseller's refund rules.
	_, _ = s.db.Exec(`UPDATE clients SET expires_unix=? WHERE email='usr2'`, now.Add(-time.Hour).Unix())
	if code, list := c.call("GET", "/api/users/expired", tok, nil); code != 200 || len(list.([]any)) != 1 || list.([]any)[0] != "usr2" {
		t.Fatalf("expired %d %v", code, list)
	}
	if code, out := c.obj("DELETE", "/api/users/expired?dry_run=true", tok, nil); code != 200 || out["count"].(float64) != 1 {
		t.Fatalf("expired dry run %d %v", code, out)
	}
	if _, err := s.clientView(ctx, "usr2"); err != nil {
		t.Fatal("a dry run deletes nothing")
	}
	if code, out := c.obj("POST", "/api/users/bulk/delete", tok, map[string]any{"ids": []any{id1}}); code != 200 || out["count"].(float64) != 1 {
		t.Fatalf("bulk delete %d %v", code, out)
	}
	if _, err := s.clientView(ctx, "usr1"); err == nil {
		t.Fatal("bulk delete deletes")
	}

	// The owner: next plans, moving users, admins.
	o, _ := s.EnablePGAPI(ctx, "")
	otok := c.login("mirza", o.Password)
	code, out = c.obj("PUT", "/api/user/alice", otok, map[string]any{"next_plan": map[string]any{"data_limit": 2 << 30, "expire": 10 * 86400}})
	if code != 200 || out["next_plan"].(map[string]any)["data_limit"].(float64) != 2<<30 {
		t.Fatalf("next plan %d %v", code, out)
	}
	if code, out := c.obj("POST", "/api/user/alice/active_next", otok, nil); code != 200 || out["data_limit"].(float64) != 2<<30 || out["next_plan"] != nil {
		t.Fatalf("active next %d %v", code, out)
	}
	if code, _ := c.obj("POST", "/api/user/alice/active_next", otok, nil); code != 404 {
		t.Fatal("no next plan left", code)
	}
	if code, out := c.obj("PUT", "/api/user/alice", otok, map[string]any{"data_limit_reset_strategy": "month"}); code != 200 || out["data_limit_reset_strategy"] != "month" {
		t.Fatalf("owner reset strategy %d %v", code, out)
	}
	if code, out := c.obj("GET", "/api/admins", otok, nil); code != 200 || out["total"].(float64) != 1 {
		t.Fatalf("admins %d %v", code, out)
	}
	if code, out := c.obj("PUT", "/api/user/alice/set_owner?admin_username=mirza_ali", otok, nil); code != 200 || out["admin"].(map[string]any)["username"] != "ali" {
		t.Fatalf("set owner %d %v", code, out)
	}
	if code, _ := c.obj("GET", "/api/user/alice", tok, nil); code != 200 {
		t.Fatal("the reseller sees the user it was given", code)
	}
}

// TestPasarGuardReviewFixes holds the cases a review of the API found.
func TestPasarGuardReviewFixes(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutInbound(ctx, "de-two", "de-1", []byte(strings.Replace(vlessTemplate, "18580", "9000", 1)), true); err != nil {
		t.Fatal(err)
	}
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if err := s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, ""); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	c := pgClient{t, h.URL}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := c.login("mirza_ali", a.Password)
	_, g := c.obj("GET", "/api/groups", tok, nil)
	groups := g["groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("groups %v", g)
	}
	g1, g2 := groups[0].(map[string]any)["id"], groups[1].(map[string]any)["id"]
	exp := now.Add(30 * 24 * time.Hour).Unix()
	for name, gid := range map[string]any{"one1": g1, "two2": g2} {
		if code, out := c.obj("POST", "/api/user", tok, map[string]any{"username": name, "data_limit": 1 << 30, "expire": exp, "group_ids": []any{gid}}); code != 200 {
			t.Fatalf("create %d %v", code, out)
		}
	}
	vol := func(e string) int64 { v, _ := s.clientView(ctx, e); return v.VolumeBytes }

	// group_ids narrows a bulk change to the users in those groups.
	if code, out := c.obj("POST", "/api/users/bulk/data_limit", tok, map[string]any{"amount": 1 << 30, "group_ids": []any{g2}, "dry_run": true}); code != 200 || out["affected_users"].(float64) != 1 {
		t.Fatalf("group filter dry run %d %v", code, out)
	}
	if code, _ := c.obj("POST", "/api/users/bulk/data_limit", tok, map[string]any{"amount": 1 << 30, "group_ids": []any{g2}}); code != 200 || vol("one1") != 1<<30 || vol("two2") != 2<<30 {
		t.Fatalf("group filter %d one1=%d two2=%d", code, vol("one1"), vol("two2"))
	}
	// admins: another admin's id reaches none of this seller's users; its own id does.
	var aliID int64
	_ = s.db.QueryRow(`SELECT rowid FROM reps WHERE id='ali'`).Scan(&aliID)
	if code, out := c.obj("POST", "/api/users/bulk/expire", tok, map[string]any{"amount": 86400, "admins": []any{aliID + 100}, "dry_run": true}); code != 200 || out["affected_users"].(float64) != 0 {
		t.Fatalf("other admin %d %v", code, out)
	}
	if code, out := c.obj("POST", "/api/users/bulk/expire", tok, map[string]any{"amount": 86400, "admins": []any{aliID}, "dry_run": true}); code != 200 || out["affected_users"].(float64) != 2 {
		t.Fatalf("own admin %d %v", code, out)
	}

	// A bot that reads a user and sends it back whole must not be refused
	// for what the owner set on it.
	_ = s.SetResetPeriod(ctx, "one1", "month")
	_ = s.SetNextPlan(ctx, "one1", &NextPlan{GB: 1, Days: 30})
	_, u := c.obj("GET", "/api/user/one1", tok, nil)
	u["note"] = "echoed"
	if code, out := c.obj("PUT", "/api/user/one1", tok, u); code != 200 || out["data_limit_reset_strategy"] != "month" || out["next_plan"] == nil || out["note"] != "echoed" {
		t.Fatalf("echo back %d %v", code, out)
	}
	if code, _ := c.obj("PUT", "/api/user/one1", tok, map[string]any{"next_plan": map[string]any{"data_limit": 5 << 30}}); code != 403 {
		t.Fatal("a changed next plan is still the owner's", code)
	}

	// The public client-type links keep the device limit.
	_ = s.SetHWIDLimit(ctx, "one1", 1)
	v, _ := s.clientView(ctx, "one1")
	hs := httptest.NewServer(SubHandler(s))
	defer hs.Close()
	get := func(path, hwid string) int {
		req, _ := http.NewRequest("GET", hs.URL+path, nil)
		req.Header.Set("X-Hwid", hwid)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("/sub/"+v.SubToken, "phone-1"); code != 200 {
		t.Fatal("first device", code)
	}
	if code := get("/sub/"+v.SubToken+"/sing_box", "phone-2"); code != 403 {
		t.Fatal("a second device must be refused on the client-type link too", code)
	}

	// A missing template is reported as a template.
	if code, out := c.obj("PUT", "/api/user/from_template/one1", tok, map[string]any{"user_template_id": 999}); code != 404 || out["detail"] != "User Template not found" {
		t.Fatalf("missing template %d %v", code, out)
	}
}
