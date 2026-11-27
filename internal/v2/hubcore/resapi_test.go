package hubcore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func eventKinds(t *testing.T, s *Store, target string) []string {
	t.Helper()
	list, err := s.APIEvents(context.Background(), target, 0, 500, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		out = append(out, e.Event)
	}
	return out
}

func countOf(list []string, v string) int {
	n := 0
	for _, x := range list {
		if x == v {
			n++
		}
	}
	return n
}

func TestResellerAPI(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutInbound(ctx, "de-trojan", "de-1", []byte(strings.Replace(vlessTemplate, "18580", "18581", 1)), true); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "https://sub.example.com", nil))
	defer h.Close()
	call := func(method, path, key string, body any, extra ...string) apiResp {
		t.Helper()
		return apiCall(t, h.URL, method, path, key, body, extra...)
	}
	pol := RepPolicy{MaxDays: 60, Inbounds: []string{"de-vless"}, AllowUnlimited: true, CanDelete: true, RefundUnused: true}
	if err := s.CreateRep(ctx, "admin", RepCreate{ID: "shop", Name: "Shop", Password: "shop-password-1", CreditGB: 100, Units: 5, Policy: &pol}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRep(ctx, "admin", RepCreate{ID: "other", Name: "Other", Password: "other-password", CreditGB: 10}); err != nil {
		t.Fatal(err)
	}
	k, err := s.CreateResKey(ctx, "shop", "test", ResKeyIn{Name: "bot"})
	if err != nil || !strings.HasPrefix(k.Key, "dr_") || strings.Join(k.Scopes, ",") != strings.Join(resellerDefaultScopes, ",") {
		t.Fatalf("%+v %v", k, err)
	}
	ok2, _ := s.CreateResKey(ctx, "other", "test", ResKeyIn{Name: "bot"})
	key := k.Key

	if r := call("GET", "/api/v2/reseller/me", "dr_nope_x", nil); r.Code != 401 {
		t.Fatalf("wrong key %d", r.Code)
	}
	me := call("GET", "/api/v2/reseller/me", key, nil)
	if me.Code != 200 || me.num("credit_gb") != 100 || me.num("units") != 5 || me.JSON["sub_seller"] != false ||
		me.JSON["policy"].(map[string]any)["max_days"].(float64) != 60 || me.str("sub_base") != "https://sub.example.com/sub/" {
		t.Fatalf("me %s", me.Raw)
	}
	if ib := call("GET", "/api/v2/reseller/inbounds", key, nil); len(ib.List) != 1 || ib.List[0].(map[string]any)["tag"] != "de-vless" {
		t.Fatalf("inbounds %s", ib.Raw)
	}

	// Quotes change nothing.
	q := call("POST", "/api/v2/reseller/quote", key, map[string]any{"action": "create", "gb": 10, "days": 30})
	if q.Code != 200 || q.JSON["allowed"] != true || q.num("charge_gb") != 10 || q.num("credit_after_gb") != 90 || q.JSON["start_on_first_use"] != true {
		t.Fatalf("quote %s", q.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", key, map[string]any{"gb": 10, "days": 90}); q.JSON["allowed"] != false || q.str("code") != codePolicy {
		t.Fatalf("quote over the longest term %s", q.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", key, map[string]any{"plan": "unlimited", "days": 31}); q.num("charge_units") != 2 || q.num("units_after") != 3 {
		t.Fatalf("quote unlimited %s", q.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", key, map[string]any{"gb": 500, "days": 30}); q.JSON["allowed"] != false || q.str("code") != codeCredit {
		t.Fatalf("quote over credit %s", q.Raw)
	}

	// Making customers.
	c1 := call("POST", "/api/v2/reseller/clients", key, map[string]any{"name": "c1", "gb": 10, "days": 30, "note": "first", "device_limit": 2}, "Idempotency-Key", "buy-1")
	cl := c1.JSON["client"].(map[string]any)
	if c1.Code != 201 || cl["status"] != "on_hold" || cl["term_days"].(float64) != 30 || !strings.HasPrefix(cl["sub_url"].(string), "https://sub.example.com/sub/") ||
		cl["note"] != "first" || cl["device_limit"].(float64) != 2 || c1.JSON["charge"].(map[string]any)["gb"].(float64) != 10 ||
		c1.JSON["balance"].(map[string]any)["credit_gb"].(float64) != 90 || len(cl["inbounds"].([]any)) != 1 {
		t.Fatalf("create %d %s", c1.Code, c1.Raw)
	}
	again := call("POST", "/api/v2/reseller/clients", key, map[string]any{"name": "c1", "gb": 10, "days": 30, "note": "first", "device_limit": 2}, "Idempotency-Key", "buy-1")
	if again.Code != 201 || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a retry %d %s", again.Code, again.Raw)
	}
	if me := call("GET", "/api/v2/reseller/me", key, nil); me.num("credit_gb") != 90 {
		t.Fatalf("charged once %s", me.Raw)
	}
	for _, name := range []string{"c1", "alice"} {
		if r := call("POST", "/api/v2/reseller/clients", key, map[string]any{"name": name, "gb": 1, "days": 30}); r.Code != 409 || r.str("code") != codeConflict {
			t.Fatalf("a taken name %s: %d %s", name, r.Code, r.Raw)
		}
	}
	if r := call("POST", "/api/v2/reseller/clients", key, map[string]any{"name": "c9", "gb": 1, "days": 30, "inbounds": []string{"de-trojan"}}); r.Code != 403 || r.str("code") != codePolicy {
		t.Fatalf("an inbound not allowed %d %s", r.Code, r.Raw)
	}
	c2 := call("POST", "/api/v2/reseller/clients", key, map[string]any{"name": "c2", "gb": 5, "days": 10, "start_on_first_use": false})
	if cl := c2.JSON["client"].(map[string]any); c2.Code != 201 || cl["status"] != "active" || int64(cl["expires_unix"].(float64)) != clock.Unix()+10*86400 {
		t.Fatalf("starting now %s", c2.Raw)
	}
	u1 := call("POST", "/api/v2/reseller/clients", key, map[string]any{"plan": "unlimited", "days": 30})
	if cl := u1.JSON["client"].(map[string]any); u1.Code != 201 || cl["volume_gb"].(float64) != -1 || !strings.HasPrefix(cl["name"].(string), "u") ||
		u1.JSON["charge"].(map[string]any)["units"].(float64) != 1 {
		t.Fatalf("unlimited %s", u1.Raw)
	}

	// Reading customers: only its own.
	l := call("GET", "/api/v2/reseller/clients", key, nil)
	if l.Code != 200 || l.num("total") != 3 {
		t.Fatalf("list %s", l.Raw)
	}
	if l := call("GET", "/api/v2/reseller/clients?status=active&q=c", key, nil); l.num("total") != 1 {
		t.Fatalf("filtered %s", l.Raw)
	}
	for _, path := range []string{"/api/v2/reseller/clients/alice", "/api/v2/reseller/clients/nobody"} {
		if r := call("GET", path, key, nil); r.Code != 404 {
			t.Fatalf("%s: %d", path, r.Code)
		}
	}
	if r := call("GET", "/api/v2/reseller/clients/c1", ok2.Key, nil); r.Code != 404 {
		t.Fatalf("another reseller's customer: %d", r.Code)
	}

	// Changing a customer.
	p := call("PATCH", "/api/v2/reseller/clients/c1", key, map[string]any{"max_ips": 3, "note": "vip", "device_limit": 1, "enabled": false})
	if p.Code != 200 || p.num("max_ips") != 3 || p.str("note") != "vip" || p.num("device_limit") != 1 || p.str("status") != "disabled" {
		t.Fatalf("patch %d %s", p.Code, p.Raw)
	}
	if r := call("POST", "/api/v2/reseller/clients/c1/enable", key, nil); r.str("status") != "on_hold" {
		t.Fatalf("enable %s", r.Raw)
	}
	if r := call("PATCH", "/api/v2/reseller/clients/c1", key, map[string]any{"inbounds": []string{"de-trojan"}}); r.Code != 403 {
		t.Fatalf("inbound not allowed %d", r.Code)
	}

	// Renew: the new package is charged, unused volume of the old comes back.
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('c1',?,0,?,?)`, int64(4<<30), clock.Unix(), clock.Unix()); err != nil {
		t.Fatal(err)
	}
	rq := call("POST", "/api/v2/reseller/quote", key, map[string]any{"action": "renew", "name": "c1", "gb": 20, "days": 30})
	if rq.JSON["allowed"] != true || rq.num("charge_gb") != 20 || rq.num("refund_gb") != 6 || rq.num("credit_after_gb") != 71 {
		t.Fatalf("renew quote %s", rq.Raw)
	}
	rn := call("POST", "/api/v2/reseller/clients/c1/renew", key, map[string]any{"gb": 20, "days": 30})
	if rn.Code != 200 || rn.JSON["charge"].(map[string]any)["gb"].(float64) != 20 || rn.JSON["refund"].(map[string]any)["gb"].(float64) != 6 ||
		rn.JSON["balance"].(map[string]any)["credit_gb"].(float64) != 71 || rn.JSON["client"].(map[string]any)["volume_gb"].(float64) != 20 {
		t.Fatalf("renew %d %s", rn.Code, rn.Raw)
	}

	// Reset: the used part is paid, on the ledger.
	if _, err := s.db.Exec(`UPDATE client_usage SET up=? WHERE email='c1'`, int64(2<<30)); err != nil {
		t.Fatal(err)
	}
	rs := call("POST", "/api/v2/reseller/clients/c1/reset", key, nil)
	if rs.Code != 200 || rs.JSON["charge"].(map[string]any)["gb"].(float64) != 2 || rs.JSON["client"].(map[string]any)["used_gb"].(float64) != 0 ||
		rs.JSON["balance"].(map[string]any)["credit_gb"].(float64) != 69 {
		t.Fatalf("reset %d %s", rs.Code, rs.Raw)
	}

	// A new link.
	before := call("GET", "/api/v2/reseller/clients/c1", key, nil).str("sub_url")
	if after := call("POST", "/api/v2/reseller/clients/c1/revoke", key, nil).str("sub_url"); after == before || after == "" {
		t.Fatal("revoke must change the link")
	}
	lk := call("GET", "/api/v2/reseller/clients/c1/links", key, nil)
	if lk.Code != 200 || !strings.HasPrefix(lk.str("sub_url"), "https://sub.example.com/sub/") || !strings.HasSuffix(lk.str("links_url"), "/links") {
		t.Fatalf("links %s", lk.Raw)
	}
	req, _ := http.NewRequest("GET", h.URL+"/api/v2/reseller/clients/c1/qr.png", nil)
	req.Header.Set("X-API-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	png, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || len(png) < 100 {
		t.Fatalf("qr %d", resp.StatusCode)
	}
	if u := call("GET", "/api/v2/reseller/clients/c1/usage?days=7", key, nil); len(u.JSON["days"].([]any)) != 7 {
		t.Fatalf("usage %s", u.Raw)
	}
	for _, path := range []string{"/devices", "/ips"} {
		if r := call("GET", "/api/v2/reseller/clients/c1"+path, key, nil); r.Code != 200 || r.JSON["items"] == nil {
			t.Fatalf("%s %s", path, r.Raw)
		}
	}
	if r := call("GET", "/api/v2/reseller/online", key, nil); r.Code != 200 {
		t.Fatalf("online %d", r.Code)
	}

	// Delete: unused volume comes back under this policy.
	d := call("DELETE", "/api/v2/reseller/clients/c2", key, nil)
	if d.Code != 200 || d.JSON["refund"].(map[string]any)["gb"].(float64) != 5 || d.JSON["balance"].(map[string]any)["credit_gb"].(float64) != 74 {
		t.Fatalf("delete %d %s", d.Code, d.Raw)
	}

	// Bulk: every name free and the credit covers all, or nothing is made.
	b := call("POST", "/api/v2/reseller/clients/bulk", key, map[string]any{"prefix": "b", "count": 3, "gb": 2, "days": 30})
	if b.Code != 201 || len(b.JSON["created"].([]any)) != 3 || b.JSON["charge"].(map[string]any)["gb"].(float64) != 6 || b.JSON["stopped"] != nil {
		t.Fatalf("bulk %d %s", b.Code, b.Raw)
	}
	if r := call("POST", "/api/v2/reseller/clients/bulk", key, map[string]any{"prefix": "b", "count": 3, "gb": 2, "days": 30}); r.Code != 409 {
		t.Fatalf("bulk with taken names %d", r.Code)
	}
	if r := call("POST", "/api/v2/reseller/clients/bulk", key, map[string]any{"prefix": "big", "count": 100, "gb": 10, "days": 30}); r.Code != 402 {
		t.Fatalf("bulk over credit %d %s", r.Code, r.Raw)
	}

	// The ledger, as the reseller sees it.
	if err := s.AdjustCredit(ctx, "admin", "shop", 1<<30, 0, "gift"); err != nil {
		t.Fatal(err)
	}
	led := call("GET", "/api/v2/reseller/ledger?limit=500", key, nil)
	rows := led.JSON["items"].([]any)
	if top := rows[0].(map[string]any); top["actor"] != "owner" || top["kind"] != "adjust" {
		t.Fatalf("ledger top %v", top)
	}
	keyRows := 0
	for _, row := range rows {
		if row.(map[string]any)["actor"] == "key:bot" {
			keyRows++
		}
	}
	if keyRows < 6 {
		t.Fatalf("the key's charges are on the ledger under its name: %d %s", keyRows, led.Raw)
	}
	if ov := call("GET", "/api/v2/reseller/overview", key, nil); ov.Code != 200 || ov.num("clients") != 5 {
		t.Fatalf("overview %s", ov.Raw)
	}

	// Policy: deny_edit and deny_renew hold for keys too.
	pol.DenyEdit, pol.DenyRenew = true, true
	if err := s.SetRepPolicy(ctx, "shop", pol); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"PATCH", "/api/v2/reseller/clients/c1", map[string]any{"max_ips": 9}},
		{"POST", "/api/v2/reseller/clients/c1/renew", map[string]any{"gb": 1, "days": 30}},
		{"POST", "/api/v2/reseller/clients/c1/reset", nil},
	} {
		if r := call(c.method, c.path, key, c.body); r.Code != 403 || r.str("code") != codePolicy {
			t.Fatalf("%s %s under deny: %d %s", c.method, c.path, r.Code, r.Raw)
		}
	}
	pol.DenyEdit, pol.DenyRenew = false, false
	_ = s.SetRepPolicy(ctx, "shop", pol)

	// Sub-sellers: a scope of their own.
	if r := call("GET", "/api/v2/reseller/sellers", key, nil); r.Code != 403 || r.str("code") != codeScopeMissing {
		t.Fatalf("sellers without scope %d", r.Code)
	}
	sc := append([]string{}, resellerScopes...)
	sk, _ := s.CreateResKey(ctx, "shop", "test", ResKeyIn{Name: "admin-bot", Scopes: &sc})
	sl := call("POST", "/api/v2/reseller/sellers", sk.Key, map[string]any{"id": "sub1", "name": "Sub", "credit_gb": 5, "policy": map[string]any{"max_days": 90, "max_clients": 3}})
	spol := sl.JSON["policy"].(map[string]any)
	if sl.Code != 201 || sl.num("credit_gb") != 5 || sl.str("password") == "" || spol["max_days"].(float64) != 60 || spol["max_clients"].(float64) != 3 {
		t.Fatalf("seller %d %s", sl.Code, sl.Raw)
	}
	if r := call("POST", "/api/v2/reseller/sellers/sub1/credit", sk.Key, map[string]any{"credit_gb": 2}); r.Code != 200 || r.JSON["seller"].(map[string]any)["credit_gb"].(float64) != 7 {
		t.Fatalf("seller credit %s", r.Raw)
	}
	if r := call("POST", "/api/v2/reseller/sellers/sub1/credit/revoke", sk.Key, map[string]any{"credit_gb": 3}); r.Code != 200 || r.JSON["seller"].(map[string]any)["credit_gb"].(float64) != 4 {
		t.Fatalf("seller take back %s", r.Raw)
	}
	if r := call("PATCH", "/api/v2/reseller/sellers/sub1", sk.Key, map[string]any{"enabled": false, "name": "Sub One"}); r.JSON["enabled"] != false || r.str("name") != "Sub One" {
		t.Fatalf("seller patch %s", r.Raw)
	}
	if r := call("GET", "/api/v2/reseller/sellers", ok2.Key, nil); r.Code != 403 {
		t.Fatalf("other's key %d", r.Code)
	}
	subKey, _ := s.CreateResKey(ctx, "sub1", "test", ResKeyIn{Name: "sub", Scopes: &sc})
	for _, x := range subKey.Scopes {
		if x == scopeSellers {
			t.Fatal("a sub-seller's key never gets the sellers scope")
		}
	}
	if r := call("GET", "/api/v2/reseller/me", subKey.Key, nil); r.Code != 403 || r.str("code") != codeDisabled {
		t.Fatalf("a turned-off seller's key %d %s", r.Code, r.Raw)
	}

	// Keys are managed by the owner (and the reseller in its panel).
	adm := call("POST", "/api/v2/admin/reps/shop/apikeys", "admintok", map[string]any{"name": "made-by-owner", "days": 30, "scopes": []string{"clients:read"}})
	if adm.Code != 200 || !strings.HasPrefix(adm.str("key"), "dr_") {
		t.Fatalf("owner makes a key %d %s", adm.Code, adm.Raw)
	}
	if r := call("POST", "/api/v2/reseller/clients", adm.str("key"), map[string]any{"gb": 1, "days": 1}); r.Code != 403 {
		t.Fatalf("a read-only key %d", r.Code)
	}
	if r := call("DELETE", "/api/v2/admin/reps/shop/apikeys/"+adm.str("id"), "admintok", nil); r.Code != 200 {
		t.Fatalf("owner deletes %d", r.Code)
	}
	if r := call("GET", "/api/v2/reseller/me", adm.str("key"), nil); r.Code != 401 {
		t.Fatalf("a deleted key %d", r.Code)
	}

	// Events: from the API and from the panel alike.
	if err := s.RepPutClient(ctx, "shop", "shop", Client{Email: "panel-made", Enabled: true, VolumeBytes: 1 << 30, DurationSeconds: 86400, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	kinds := eventKinds(t, s, repTarget("shop"))
	// credit.added: the first credit, the owner's gift, credit taken back from the seller;
	// credit.removed: the two moves to the seller.
	for kind, want := range map[string]int{"client.created": 7, "client.updated": 1, "client.disabled": 1, "client.enabled": 1, "client.revoked": 1,
		"client.deleted": 1, "credit.added": 3, "credit.removed": 2} {
		if got := countOf(kinds, kind); got != want {
			t.Fatalf("%s: %d, want %d\n%v", kind, got, want, kinds)
		}
	}

	// Renewals and resets come from the HUB's log, whoever made them.
	hubLogEvents(ctx, s) // the first run starts at the end of the log
	if _, err := s.RenewClient(ctx, Principal{User: "shop", Role: "rep", RepID: "shop"}, "b-1", "volume", 3, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetUsage(ctx, "b-2", "manual"); err != nil {
		t.Fatal(err)
	}
	hubLogEvents(ctx, s)
	hubLogEvents(ctx, s)
	kinds = eventKinds(t, s, repTarget("shop"))
	if countOf(kinds, "client.renewed") != 1 || countOf(kinds, "client.usage_reset") != 1 {
		t.Fatalf("hub log events %v", kinds)
	}

	// State changes: first seen is remembered, then changes are told once.
	clientStateEvents(ctx, s)
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('b-3',?,0,?,?)`, int64(1900<<20), clock.Unix(), clock.Unix()); err != nil {
		t.Fatal(err)
	}
	clientStateEvents(ctx, s) // b-3: on hold → active, and 1.86 of 2 GB used
	*clock = clock.Add(28 * 24 * time.Hour)
	clientStateEvents(ctx, s) // b-1 (renewed today+30): 2 days left
	clientStateEvents(ctx, s)
	*clock = clock.Add(3 * 24 * time.Hour)
	clientStateEvents(ctx, s) // b-1 ended
	kinds = eventKinds(t, s, repTarget("shop"))
	for kind, want := range map[string]int{"client.first_used": 1, "client.volume_low": 1, "client.expiring": 1, "client.expired": 1} {
		if got := countOf(kinds, kind); got < want {
			t.Fatalf("%s: %d, want %d\n%v", kind, got, want, kinds)
		}
	}
	if countOf(kinds, "client.expiring") > 2 {
		t.Fatalf("expiring is told once per period %v", kinds)
	}

	// The reseller's webhook routes are its own.
	if r := call("GET", "/api/v2/reseller/events?types=client.deleted", key, nil); len(r.JSON["items"].([]any)) != 1 {
		t.Fatalf("events %s", r.Raw)
	}
	if r := call("GET", "/api/v2/reseller/events", ok2.Key, nil); len(r.JSON["items"].([]any)) != 1 || strings.Contains(string(r.Raw), "client.") {
		t.Fatalf("another reseller sees only its own first credit %s", r.Raw)
	}

	// A partner can make keys for its panels.
	pk, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site"})
	pp := call("POST", "/api/v2/partner/reps", pk.Key, map[string]any{"name": "Sold", "credit_gb": 5})
	mk := call("POST", "/api/v2/partner/reps/"+pp.str("id")+"/apikeys", pk.Key, map[string]any{"name": "bundle"})
	if mk.Code != 201 || !strings.HasPrefix(mk.str("key"), "dr_") {
		t.Fatalf("partner key %d %s", mk.Code, mk.Raw)
	}
	if r := call("GET", "/api/v2/reseller/me", mk.str("key"), nil); r.str("id") != pp.str("id") {
		t.Fatalf("the bundle key works for its panel %s", r.Raw)
	}
	if r := call("GET", "/api/v2/partner/reps/shop/apikeys", pk.Key, nil); r.Code != 404 {
		t.Fatalf("a panel the partner did not make %d", r.Code)
	}

	// A turned-off reseller's keys answer 403.
	if err := s.PutRep(ctx, Rep{ID: "shop", Name: "Shop", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if r := call("GET", "/api/v2/reseller/me", key, nil); r.Code != 403 || r.str("code") != codeDisabled {
		t.Fatalf("turned off %d %s", r.Code, r.Raw)
	}
}
