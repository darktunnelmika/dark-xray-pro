package hubcore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestPasarGuardAPI walks the calls Mirza makes to a PasarGuard panel.
func TestPasarGuardAPI(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	if err := s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, ""); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()

	call := func(method, path, tok string, body any) (int, map[string]any) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, _ := http.NewRequest(method, h.URL+path, rd)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	login := func(user, pass string) (int, string) {
		resp, err := http.PostForm(h.URL+"/api/admin/token", url.Values{"username": {user}, "password": {pass}})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		tok, _ := out["access_token"].(string)
		return resp.StatusCode, tok
	}
	credit := func() int64 {
		var v int64
		_ = s.db.QueryRow(`SELECT volume_credit FROM reps WHERE id='ali'`).Scan(&v)
		return v
	}

	a, err := s.EnablePGAPI(ctx, "ali")
	if err != nil || a.Username != "mirza_ali" || a.Password == "" {
		t.Fatalf("%+v %v", a, err)
	}
	if code, _ := login("mirza_ali", "wrong"); code != 401 {
		t.Fatal("a wrong password must be refused", code)
	}
	code, tok := login("mirza_ali", a.Password)
	if code != 200 || tok == "" {
		t.Fatal("login", code)
	}
	if !s.MirzaMode(ctx, "ali") {
		t.Fatal("the reseller sells through Mirza now")
	}

	code, out := call("GET", "/api/groups", tok, nil)
	groups, _ := out["groups"].([]any)
	if code != 200 || len(groups) != 1 {
		t.Fatalf("groups %d %v", code, out)
	}
	gid := groups[0].(map[string]any)["id"]
	if code, out := call("GET", "/api/user/"+PGAllGroups, tok, nil); code != 200 || len(out["group_ids"].([]any)) != 1 {
		t.Fatalf("all groups %d %v", code, out)
	}

	exp := now.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	code, out = call("POST", "/api/user", tok, map[string]any{"username": "m1", "data_limit": 10 << 30, "expire": exp,
		"group_ids": []any{gid}, "note": "from mirza", "proxy_settings": map[string]any{}, "data_limit_reset_strategy": "no_reset"})
	if code != 200 || out["username"] != "m1" || out["status"] != "active" || !strings.Contains(out["subscription_url"].(string), "/sub/") {
		t.Fatalf("create %d %v", code, out)
	}
	if credit() != 90<<30 {
		t.Fatalf("credit after create %d", credit())
	}
	if _, ok := out["links"].([]any); !ok {
		t.Fatal("the new user's configs come back as a list")
	}
	if code, _ := call("POST", "/api/user", tok, map[string]any{"username": "m1", "data_limit": 1 << 30, "expire": exp}); code != 409 {
		t.Fatal("a second user with the same name", code)
	}
	if code, out := call("GET", "/api/user/alice", tok, nil); code != 404 || out["detail"] != "User not found" {
		t.Fatal("the owner's client is invisible to a reseller", code)
	}

	// A renewal the way Mirza sends it: Unix expiry, groups under "inbounds".
	code, out = call("PUT", "/api/user/m1", tok, map[string]any{"data_limit": 15 << 30, "expire": now.Add(60 * 24 * time.Hour).Unix(),
		"inbounds": []any{gid}, "proxy_settings": map[string]any{"vless": map[string]any{"id": "x"}}})
	if code != 200 || out["data_limit"].(float64) != float64(15<<30) || credit() != 85<<30 {
		t.Fatalf("modify %d %v credit %d", code, out, credit())
	}
	// Reset: the used part is charged.
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('m1',?,0,?,?)`, int64(1<<30), now.Unix(), now.Unix())
	code, out = call("POST", "/api/user/m1/reset", tok, nil)
	if code != 200 || out["used_traffic"].(float64) != 0 || credit() != 84<<30 {
		t.Fatalf("reset %d %v credit %d", code, out, credit())
	}
	before, _ := s.clientView(ctx, "m1")
	code, out = call("POST", "/api/user/m1/revoke_sub", tok, nil)
	after, _ := s.clientView(ctx, "m1")
	if code != 200 || after.SubToken == before.SubToken {
		t.Fatal("revoke_sub gives a new link", code)
	}
	resp, err := http.Get(out["subscription_url"].(string) + "/links")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("the plain list of configs", err)
	}
	resp.Body.Close()
	// Disable, as Mirza does: read, then PUT the status with proxy_settings.
	if code, out := call("PUT", "/api/user/m1", tok, map[string]any{"status": "disabled", "proxy_settings": map[string]any{}}); code != 200 || out["status"] != "disabled" {
		t.Fatalf("disable %d %v", code, out)
	}
	// On hold: the term starts at the first connection.
	code, out = call("POST", "/api/user", tok, map[string]any{"username": "m2", "data_limit": 5 << 30, "expire": 0, "status": "on_hold",
		"on_hold_expire_duration": 7 * 86400, "group_ids": []any{gid}})
	if code != 200 || out["status"] != "on_hold" || out["expire"] != nil {
		t.Fatalf("on hold %d %v", code, out)
	}
	code, out = call("GET", "/api/users?status=on_hold", tok, nil)
	if code != 200 || out["total"].(float64) != 1 {
		t.Fatalf("list %d %v", code, out)
	}
	code, out = call("GET", "/api/system", tok, nil)
	if code != 200 || out["total_user"].(float64) != 2 {
		t.Fatalf("system %d %v", code, out)
	}
	if code, out := call("DELETE", "/api/user/m2", tok, nil); code != 200 || out["detail"] != "User successfully deleted" {
		t.Fatalf("delete %d %v", code, out)
	}
	if code, _ := call("GET", "/api/users", "", nil); code != 401 {
		t.Fatal("no token, no answer", code)
	}
	// Turning it off ends the tokens and gives the Dark bot back.
	if err := s.DisablePGAPI(ctx, "ali"); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("GET", "/api/users", tok, nil); code != 401 || s.MirzaMode(ctx, "ali") {
		t.Fatal("a disabled access must stop at once", code)
	}

	// The owner's own access sees the owner's clients, not resellers'.
	o, _ := s.EnablePGAPI(ctx, "")
	_, otok := login("mirza", o.Password)
	if code, out := call("GET", "/api/user/alice", otok, nil); code != 200 || out["username"] != "alice" {
		t.Fatalf("owner read %d %v", code, out)
	}
	if code, _ := call("GET", "/api/user/m1", otok, nil); code != 404 {
		t.Fatal("a reseller's client is not the owner's", code)
	}
}
