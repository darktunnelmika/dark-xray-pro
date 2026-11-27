package hubcore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPartnerAPIFull(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutInbound(ctx, "de-trojan", "de-1", []byte(strings.Replace(vlessTemplate, "18580", "18581", 1)), true); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	call := func(method, path, key string, body any, extra ...string) apiResp {
		t.Helper()
		return apiCall(t, h.URL, method, path, key, body, extra...)
	}

	// A template reseller caps every panel the key makes.
	if err := s.PutRep(ctx, Rep{ID: "tpl", Name: "Template", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepPolicy(ctx, "tpl", RepPolicy{MaxDays: 30, MaxClientGB: 50, Inbounds: []string{"de-vless"}, CanDelete: true, PriceGB: 900}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Packages: []RepPackage{
		{ID: "gold", Name: "Gold", GB: 500, Price: 5_000_000, AllowUnlimited: true, Units: 10, MaxClients: 200, CanDelete: true},
		{ID: "mini", Name: "Mini", GB: 50, Price: 600_000},
	}}); err != nil {
		t.Fatal(err)
	}
	all := append([]string{}, partnerScopes...)
	gold := []string{"gold"}
	k, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site", MaxGB: 2000, Template: "tpl", Scopes: &all, Packages: &gold})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "x", Packages: &[]string{"nope"}}); err == nil {
		t.Fatal("an unknown package must be refused")
	}
	me := call("GET", "/api/v2/partner", k.Key, nil)
	if me.num("remaining_gb") != 2000 || me.num("remaining_units") != -1 {
		t.Fatalf("me %s", me.Raw)
	}

	// Packages: only the allowed ones.
	pk := call("GET", "/api/v2/partner/packages", k.Key, nil)
	if pk.Code != 200 || len(pk.List) != 1 || pk.List[0].(map[string]any)["id"] != "gold" {
		t.Fatalf("packages %s", pk.Raw)
	}
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "M", "package": "mini"}); r.Code != 422 {
		t.Fatalf("a package the key may not sell: %d", r.Code)
	}
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "M", "package": "gold", "credit_gb": 5}); r.Code != 422 {
		t.Fatalf("package and credit: %d", r.Code)
	}
	g := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Gold Shop", "package": "gold", "external_ref": "order-77",
		"telegram_ids": []int64{111222333}, "note": "VIP", "low_credit_gb": 20})
	if g.Code != 200 || g.num("credit_gb") != 500 || g.num("units") != 10 || g.str("package") != "gold" || g.str("external_ref") != "order-77" || g.num("low_credit_gb") != 20 {
		t.Fatalf("package panel %d %s", g.Code, g.Raw)
	}
	gpol := g.JSON["policy"].(map[string]any)
	if gpol["max_clients"].(float64) != 200 || gpol["allow_unlimited"] != true {
		t.Fatalf("the package's rules %v", gpol)
	}
	if ids := g.JSON["telegram_ids"].([]any); len(ids) != 1 || ids[0].(float64) != 111222333 {
		t.Fatalf("telegram %v", ids)
	}
	gid := g.str("id")
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Again", "external_ref": "order-77"}); r.Code != 409 || r.str("code") != codeConflict {
		t.Fatalf("a second panel with the same external_ref: %d %s", r.Code, r.Raw)
	}

	// Rules: only tighter than the template.
	p := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": "shop-b", "name": "Shop B", "credit_gb": 100,
		"policy": map[string]any{"max_days": 90, "max_clients": 5, "inbounds": []string{"de-trojan"}, "refund_unused": true, "deny_renew": true}})
	if p.Code != 200 {
		t.Fatalf("create %d %s", p.Code, p.Raw)
	}
	pol := p.JSON["policy"].(map[string]any)
	if pol["max_days"].(float64) != 30 || pol["max_clients"].(float64) != 5 || pol["refund_unused"] != false || pol["deny_renew"] != true ||
		len(pol["inbounds"].([]any)) != 1 || pol["inbounds"].([]any)[0] != "de-vless" {
		t.Fatalf("clamped rules %v", pol)
	}
	if rp, _ := s.RepPolicyOf(ctx, "shop-b"); rp.PriceGB != 900 {
		t.Fatalf("the template's price stays: %+v", rp)
	}
	up := call("PATCH", "/api/v2/partner/reps/shop-b", k.Key, map[string]any{"policy": map[string]any{"max_days": 0, "deny_renew": false, "max_client_gb": 20},
		"name": "Shop Bee", "note": "moved", "external_ref": "order-78", "enabled": false, "telegram_ids": []int64{5, 6}, "low_credit_gb": 0})
	upol := up.JSON["policy"].(map[string]any)
	if up.Code != 200 || up.str("name") != "Shop Bee" || up.str("note") != "moved" || up.str("external_ref") != "order-78" || up.JSON["enabled"] != false ||
		upol["max_days"].(float64) != 30 || upol["deny_renew"] != false || upol["max_client_gb"].(float64) != 20 || up.num("low_credit_gb") != 0 ||
		len(up.JSON["telegram_ids"].([]any)) != 2 {
		t.Fatalf("patch %d %s", up.Code, up.Raw)
	}
	if r := call("PATCH", "/api/v2/partner/reps/shop-b", k.Key, map[string]any{"external_ref": "order-77"}); r.Code != 409 {
		t.Fatalf("taking another panel's external_ref: %d", r.Code)
	}
	if r := call("PATCH", "/api/v2/partner/reps/shop-b", k.Key, map[string]any{"enabled": true}); r.JSON["enabled"] != true {
		t.Fatalf("on again %s", r.Raw)
	}

	// Lists: filters, search, pages.
	for i := range 3 {
		if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Bulk " + string(rune('A'+i)), "credit_gb": 1}); r.Code != 200 {
			t.Fatal(r.Code)
		}
	}
	l := call("GET", "/api/v2/partner/reps?limit=2&offset=1", k.Key, nil)
	if l.Code != 200 || len(l.List) != 2 || l.Header.Get("X-Total-Count") != "5" {
		t.Fatalf("page %d %s %v", l.Code, l.Raw, l.Header)
	}
	if l := call("GET", "/api/v2/partner/reps", k.Key, nil); len(l.List) != 5 {
		t.Fatalf("no limit: every panel, as before: %d", len(l.List))
	}
	if l := call("GET", "/api/v2/partner/reps?external_ref=order-77", k.Key, nil); len(l.List) != 1 || l.List[0].(map[string]any)["id"] != gid {
		t.Fatalf("by external_ref %s", l.Raw)
	}
	if l := call("GET", "/api/v2/partner/reps?q=bee", k.Key, nil); len(l.List) != 1 {
		t.Fatalf("search %s", l.Raw)
	}
	if l := call("GET", "/api/v2/partner/reps?q=%25", k.Key, nil); len(l.List) != 0 {
		t.Fatalf("a %% is a letter, not everything: %s", l.Raw)
	}

	// Credit back: needs its scope, never more than is unused.
	if r := call("POST", "/api/v2/partner/reps/shop-b/credit/revoke", k.Key, map[string]any{"credit_gb": 500}); r.Code != 402 || r.str("code") != codeCredit {
		t.Fatalf("taking back more than is there: %d %s", r.Code, r.Raw)
	}
	given := func() float64 { kk, _ := s.partnerKey(ctx, k.ID); return kk.GivenGB }
	before := given()
	if r := call("POST", "/api/v2/partner/reps/shop-b/credit/revoke", k.Key, map[string]any{"credit_gb": 40, "note": "refund"}); r.Code != 200 || r.num("credit_gb") != 60 {
		t.Fatalf("revoke %d %s", r.Code, r.Raw)
	}
	if given() != before-40 {
		t.Fatalf("taken-back credit can be handed out again: %v → %v", before, given())
	}
	if r := call("POST", "/api/v2/partner/reps/shop-b/credit", k.Key, map[string]any{"credit_gb": 15, "note": "top-up"}); r.Code != 200 || r.num("credit_gb") != 75 {
		t.Fatalf("add %d %s", r.Code, r.Raw)
	}
	if err := s.AdjustCredit(ctx, "admin", "shop-b", 1<<30, 0, "gift"); err != nil {
		t.Fatal(err)
	}
	led := call("GET", "/api/v2/partner/reps/shop-b/ledger", k.Key, nil)
	rows := led.JSON["items"].([]any)
	if led.Code != 200 || len(rows) != 4 {
		t.Fatalf("ledger %s", led.Raw)
	}
	if first := rows[0].(map[string]any); first["actor"] != "owner" || first["volume_gb"].(float64) != 1 {
		t.Fatalf("the owner's own name stays private: %v", first)
	}
	if second := rows[1].(map[string]any); second["actor"] != "partner:site" || second["volume_gb"].(float64) != 15 || !strings.Contains(second["note"].(string), "top-up") {
		t.Fatalf("%v", second)
	}
	kl := call("GET", "/api/v2/partner/ledger?limit=2", k.Key, nil)
	if items := kl.JSON["items"].([]any); len(items) != 2 || items[0].(map[string]any)["panel"] != "shop-b" || kl.num("next_before") == 0 {
		t.Fatalf("key ledger %s", kl.Raw)
	}

	// A customer, the numbers, and reading customers.
	if err := s.RepPutClient(ctx, "shop-b", "shop-b", Client{Email: "cust1", Enabled: true, VolumeBytes: 10 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	st := call("GET", "/api/v2/partner/stats", k.Key, nil)
	if st.Code != 200 || st.num("panels") != 5 || st.num("clients") != 1 || st.num("sold_gb") != 10 || st.num("credit_revoked_gb") != 40 || st.num("new_panels") != 5 {
		t.Fatalf("stats %s", st.Raw)
	}
	ov := call("GET", "/api/v2/partner/reps/shop-b/overview", k.Key, nil)
	if ov.Code != 200 || ov.num("clients") != 1 || ov.num("credit_gb") != 66 || len(ov.JSON["days"].([]any)) == 0 {
		t.Fatalf("overview %s", ov.Raw)
	}
	cl := call("GET", "/api/v2/partner/reps/shop-b/clients?status=on_hold", k.Key, nil)
	items := cl.JSON["items"].([]any)
	if cl.Code != 200 || len(items) != 1 || items[0].(map[string]any)["name"] != "cust1" || items[0].(map[string]any)["sub_url"] != nil {
		t.Fatalf("clients %s", cl.Raw)
	}
	if cl := call("GET", "/api/v2/partner/reps/shop-b/clients?status=expired", k.Key, nil); cl.num("total") != 0 {
		t.Fatalf("filtered %s", cl.Raw)
	}

	// Passwords, sessions and one-time sign-in links.
	if r := call("POST", "/api/v2/partner/reps/shop-b/password", k.Key, map[string]any{"password": "short"}); r.Code != 422 {
		t.Fatalf("a short password: %d", r.Code)
	}
	pw := call("POST", "/api/v2/partner/reps/shop-b/password", k.Key, map[string]any{"password": "chosen-password-1"})
	if pw.Code != 200 || pw.str("password") != "chosen-password-1" || !strings.Contains(pw.str("message"), "chosen-password-1") {
		t.Fatalf("password %s", pw.Raw)
	}
	user := pw.str("username")
	if _, err := s.Login(ctx, user, "chosen-password-1"); err != nil {
		t.Fatal("the chosen password works", err)
	}
	if r := call("POST", "/api/v2/partner/reps/shop-b/sessions/revoke", k.Key, nil); r.Code != 200 || r.num("ended") != 1 {
		t.Fatalf("sessions %s", r.Raw)
	}
	link := call("POST", "/api/v2/partner/reps/shop-b/login-link", k.Key, nil)
	u := link.str("url")
	if link.Code != 200 || !strings.Contains(u, loginLinkPath) || link.num("expires_unix") == 0 {
		t.Fatalf("link %s", link.Raw)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	path := u[strings.Index(u, loginLinkPath):]
	resp, err := noFollow.Get(h.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var sess string
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			sess = c.Value
		}
	}
	if resp.StatusCode != 302 || sess == "" {
		t.Fatalf("using the link: %d", resp.StatusCode)
	}
	if got, err := s.SessionUser(ctx, sess); err != nil || got != user {
		t.Fatalf("the link signs in as the panel: %q %v", got, err)
	}
	resp, _ = noFollow.Get(h.URL + path)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("a link works once: %d", resp.StatusCode)
	}
	if _, err := s.db.Exec(`UPDATE admins SET totp_secret='JBSWY3DPEHPK3PXP' WHERE username=?`, user); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/api/v2/partner/reps/shop-b/login-link", k.Key, nil); r.Code != 422 || r.str("code") != codePolicy {
		t.Fatalf("no link past two-step sign-in: %d %s", r.Code, r.Raw)
	}

	// Deleting: refuses a panel with customers unless asked; credit left goes back.
	before = given()
	if r := call("DELETE", "/api/v2/partner/reps/shop-b", k.Key, nil); r.Code != 409 {
		t.Fatalf("delete with customers %d %s", r.Code, r.Raw)
	}
	del := call("DELETE", "/api/v2/partner/reps/shop-b?clients=1", k.Key, nil)
	if del.Code != 200 || del.num("clients_deleted") != 1 || del.num("released_gb") != 66 {
		t.Fatalf("delete %d %s", del.Code, del.Raw)
	}
	if given() != before-66 {
		t.Fatalf("released credit %v → %v", before, given())
	}
	if r := call("GET", "/api/v2/partner/reps/shop-b", k.Key, nil); r.Code != 404 {
		t.Fatalf("gone: %d", r.Code)
	}

	// The default scopes cannot delete, take back or read customers.
	k2, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "plain"})
	p2 := call("POST", "/api/v2/partner/reps", k2.Key, map[string]any{"name": "Plain", "credit_gb": 5})
	for _, c := range []struct{ method, path string }{
		{"DELETE", "/api/v2/partner/reps/" + p2.str("id")},
		{"POST", "/api/v2/partner/reps/" + p2.str("id") + "/credit/revoke"},
		{"GET", "/api/v2/partner/reps/" + p2.str("id") + "/clients"},
	} {
		if r := call(c.method, c.path, k2.Key, map[string]any{"credit_gb": 1}); r.Code != 403 || r.str("code") != codeScopeMissing {
			t.Fatalf("%s %s without its scope: %d", c.method, c.path, r.Code)
		}
	}
	if r := call("GET", "/api/v2/partner/reps/"+gid, k2.Key, nil); r.Code != 404 {
		t.Fatalf("another key's panel: %d", r.Code)
	}
	// Without a template any inbound can be picked, but it must exist.
	if r := call("POST", "/api/v2/partner/reps", k2.Key, map[string]any{"name": "Bad", "policy": map[string]any{"inbounds": []string{"nope"}}}); r.Code != 422 {
		t.Fatalf("an inbound that does not exist: %d", r.Code)
	}

	// Events: what happened, in order.
	ev := call("GET", "/api/v2/partner/events?limit=500", k.Key, nil)
	var kinds []string
	for _, e := range ev.JSON["items"].([]any) {
		kinds = append(kinds, e.(map[string]any)["event"].(string))
	}
	want := []string{"panel.created", "panel.created", "panel.updated", "panel.disabled", "panel.updated", "panel.enabled",
		"panel.created", "panel.created", "panel.created", "panel.credit_revoked", "panel.credit_added", "panel.password_reset", "panel.deleted"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("events\n got %v\nwant %v", kinds, want)
	}

	// Low credit: the first run only notes who is low already (the 1 GB
	// panels, under the default 50 GB); then a fall is told once to the
	// reseller and to its partner.
	lowCreditEvents(ctx, s)
	if list, _ := s.APIEvents(ctx, partnerTarget(k.ID), 0, 500, []string{"panel.credit_low"}); len(list) != 0 {
		t.Fatalf("no old warnings on the first run: %v", list)
	}
	if err := s.SetRepAlertGB(ctx, gid, 600); err != nil {
		t.Fatal(err)
	}
	lowCreditEvents(ctx, s)
	lowCreditEvents(ctx, s)
	ev = call("GET", "/api/v2/partner/events?types=panel.credit_low", k.Key, nil)
	low := 0
	for _, e := range ev.JSON["items"].([]any) {
		if d := e.(map[string]any)["data"].(map[string]any); d["id"] == gid {
			low++
			if d["threshold_gb"].(float64) != 600 || d["external_ref"] != "order-77" {
				t.Fatalf("low credit data %v", d)
			}
		}
	}
	if low != 1 || len(ev.JSON["items"].([]any)) != 1 {
		t.Fatalf("low credit %s", ev.Raw)
	}
	// A reseller that only ever had unlimited units is not warned about volume.
	if err := s.CreateRep(ctx, "admin", RepCreate{ID: "units-only", Password: "units-only-pass", Units: 5}); err != nil {
		t.Fatal(err)
	}
	lowCreditEvents(ctx, s)
	if list, _ := s.APIEvents(ctx, repTarget("units-only"), 0, 10, []string{"credit.low"}); len(list) != 0 {
		t.Fatalf("units-only reseller warned: %v", list)
	}
	if list, _ := s.APIEvents(ctx, repTarget(gid), 0, 10, []string{"credit.low"}); len(list) != 1 {
		t.Fatalf("the reseller's own event %v", list)
	}
}
