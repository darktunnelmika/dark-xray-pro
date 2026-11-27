package hubcore

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRepTerms(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	call := func(method, path, key string, body any) apiResp {
		t.Helper()
		return apiCall(t, h.URL, method, path, key, body)
	}
	day := int64(86400)

	// A partner panel with a 30-day term whose customers go off after 2 days of grace.
	pk, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site"})
	p := call("POST", "/api/v2/partner/reps", pk.Key, map[string]any{"id": "shop", "name": "Shop", "credit_gb": 50, "term_days": 30, "on_expiry": "suspend", "grace_days": 2})
	term := p.JSON["term"].(map[string]any)
	if p.Code != 200 || int64(term["expires_unix"].(float64)) != clock.Unix()+30*day || term["on_expiry"] != "suspend" || term["state"] != "active" {
		t.Fatalf("create with a term: %d %s", p.Code, p.Raw)
	}
	if r := call("POST", "/api/v2/partner/reps", pk.Key, map[string]any{"name": "X", "on_expiry": "explode"}); r.Code != 422 {
		t.Fatalf("a wrong on_expiry: %d", r.Code)
	}
	if err := s.CreateSeller(ctx, "shop", "shop", RepCreate{ID: "sub", Password: "sub-password-1", CreditGB: 5}); err != nil {
		t.Fatal(err)
	}
	rk, _ := s.CreateResKey(ctx, "shop", "test", ResKeyIn{Name: "bot"})
	if r := call("POST", "/api/v2/reseller/clients", rk.Key, map[string]any{"name": "c1", "gb": 5, "days": 30}); r.Code != 201 {
		t.Fatalf("selling while the term runs: %d %s", r.Code, r.Raw)
	}
	if _, err := s.db.Exec(`INSERT INTO clients(email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips,sub_token,created_unix,rep_id,plan) VALUES('s1','u-s1','x',1,1073741824,0,2592000,0,'tok-s1',1,'sub','volume')`); err != nil {
		t.Fatal(err)
	}

	// Reminders: three days and one day before, each once.
	*clock = clock.Add(28 * 24 * time.Hour)
	repTermTick(ctx, s)
	repTermTick(ctx, s)
	*clock = clock.Add(36 * time.Hour)
	repTermTick(ctx, s)
	kinds := eventKinds(t, s, repTarget("shop"))
	if countOf(kinds, "term.expiring") != 2 {
		t.Fatalf("reminders %v", kinds)
	}
	if me := call("GET", "/api/v2/reseller/me", rk.Key, nil); me.JSON["term"].(map[string]any)["state"] != "expiring" {
		t.Fatalf("me %s", me.Raw)
	}

	// The end: no more sales, for the panel and its sub-seller; customers keep working during the grace.
	*clock = clock.Add(13 * time.Hour)
	repTermTick(ctx, s)
	r := call("POST", "/api/v2/reseller/clients", rk.Key, map[string]any{"gb": 1, "days": 30})
	if r.Code != 403 || r.str("code") != codePolicy || !strings.Contains(r.str("detail"), "term ended") {
		t.Fatalf("selling after the end: %d %s", r.Code, r.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", rk.Key, map[string]any{"gb": 1, "days": 30}); q.JSON["allowed"] != false {
		t.Fatalf("quote after the end: %s", q.Raw)
	}
	if err := repStopQ(ctx, s.ro, "sub", clock.Unix()); err == nil || !strings.Contains(err.Error(), "above this panel") {
		t.Fatalf("the sub-seller stops too: %v", err)
	}
	if err := s.TransferCredit(ctx, "shop", "shop", "sub", 1<<30, 0, ""); err == nil {
		t.Fatal("no credit moves to sub-sellers after the end")
	}
	if _, err := s.RenewClient(ctx, Principal{User: "shop", Role: "rep", RepID: "shop"}, "c1", "volume", 1, 30); err == nil {
		t.Fatal("no renewals after the end")
	}
	if me := call("GET", "/api/v2/reseller/me", rk.Key, nil); me.JSON["locked"] != true || me.JSON["term"].(map[string]any)["state"] != "expired" {
		t.Fatalf("me after the end %s", me.Raw)
	}
	enabled := func(email string) bool {
		var on bool
		_ = s.db.QueryRow(`SELECT enabled FROM clients WHERE email=?`, email).Scan(&on)
		return on
	}
	if !enabled("c1") || !enabled("s1") {
		t.Fatal("customers keep working during the grace")
	}
	// After the grace, its customers and its sub-seller's go off; one already off stays out of it.
	if err := s.RepPutClient(ctx, "shop", "shop", Client{Email: "c1", Enabled: false, VolumeBytes: 5 << 30, DurationSeconds: 30 * day, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE clients SET enabled=1 WHERE email='c1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO clients(email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips,sub_token,created_unix,rep_id,plan) VALUES('off1','u-off1','x',0,1,0,86400,0,'tok-off1',1,'shop','volume')`); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(49 * time.Hour)
	repTermTick(ctx, s)
	repTermTick(ctx, s)
	if enabled("c1") || enabled("s1") || enabled("off1") {
		t.Fatal("suspended after the grace")
	}
	if tm, _ := s.RepTermOf(ctx, "shop"); tm.State != "suspended" {
		t.Fatalf("state %+v", tm)
	}
	kinds = eventKinds(t, s, repTarget("shop"))
	if countOf(kinds, "term.expired") != 1 || countOf(kinds, "term.suspended") != 1 {
		t.Fatalf("events %v", kinds)
	}
	pev := eventKinds(t, s, partnerTarget(pk.ID))
	if countOf(pev, "panel.expiring") != 2 || countOf(pev, "panel.expired") != 1 || countOf(pev, "panel.suspended") != 1 {
		t.Fatalf("partner events %v", pev)
	}

	// Renewing brings back exactly the customers the term switched off.
	rn := call("POST", "/api/v2/partner/reps/shop/renew", pk.Key, map[string]any{"days": 30})
	if rn.Code != 200 || rn.JSON["term"].(map[string]any)["state"] != "active" || int64(rn.JSON["term"].(map[string]any)["expires_unix"].(float64)) != clock.Unix()+30*day {
		t.Fatalf("renew %d %s", rn.Code, rn.Raw)
	}
	if !enabled("c1") || !enabled("s1") || enabled("off1") {
		t.Fatal("back on: the two switched off by the term, not the one off before")
	}
	if r := call("POST", "/api/v2/reseller/clients", rk.Key, map[string]any{"gb": 1, "days": 30}); r.Code != 201 {
		t.Fatalf("selling after renewal: %d %s", r.Code, r.Raw)
	}
	// A renewal before the end adds to the end.
	end := clock.Unix() + 30*day
	rn = call("POST", "/api/v2/partner/reps/shop/renew", pk.Key, map[string]any{"days": 10, "on_expiry": "block"})
	if tm := rn.JSON["term"].(map[string]any); int64(tm["expires_unix"].(float64)) != end+10*day || tm["on_expiry"] != "block" {
		t.Fatalf("early renewal %s", rn.Raw)
	}
	if ev := eventKinds(t, s, repTarget("shop")); countOf(ev, "term.renewed") != 3 { // the start (previous_unix 0) and two renewals
		t.Fatalf("renewed events %v", ev)
	}
	led := call("GET", "/api/v2/partner/reps/shop/ledger", pk.Key, nil)
	if !strings.Contains(string(led.Raw), `"kind":"term"`) {
		t.Fatalf("terms are on the ledger %s", led.Raw)
	}

	// "block": customers are never switched off.
	if _, err := s.SetRepTerm(ctx, "admin", "shop", clock.Unix()-10*day, "", -1); err != nil {
		t.Fatal(err)
	}
	repTermTick(ctx, s)
	if !enabled("c1") {
		t.Fatal("block keeps customers on")
	}

	// The owner's endpoints.
	put := call("PUT", "/api/v2/admin/reps/shop/term", "admintok", map[string]any{"add_days": 30, "on_expiry": "suspend", "grace_days": 3})
	if put.Code != 200 || put.str("state") != "active" || put.num("grace_days") != 3 {
		t.Fatalf("owner extends %d %s", put.Code, put.Raw)
	}
	if l := call("GET", "/api/v2/admin/rep-terms", "admintok", nil); len(l.List) != 1 || l.List[0].(map[string]any)["rep_id"] != "shop" {
		t.Fatalf("terms list %s", l.Raw)
	}
	if put := call("PUT", "/api/v2/admin/reps/shop/term", "admintok", map[string]any{"expires_unix": 0}); put.str("state") != "none" {
		t.Fatalf("no term %s", put.Raw)
	}

	// Packages with days: the Telegram sale and the partner API.
	if err := s.PutRepSale(ctx, RepSale{Packages: []RepPackage{{ID: "month", Name: "Monthly", GB: 100, Price: 1000, Days: 30, OnExpiry: "suspend", GraceDays: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Packages: []RepPackage{{ID: "bad", Name: "Bad", GB: 1, Price: 1, OnExpiry: "nope"}}}); err == nil {
		t.Fatal("a wrong on_expiry in a package")
	}
	mp := call("POST", "/api/v2/partner/reps", pk.Key, map[string]any{"name": "Monthly", "package": "month"})
	if tm := mp.JSON["term"].(map[string]any); mp.Code != 200 || tm["on_expiry"] != "suspend" || int64(tm["expires_unix"].(float64)) != clock.Unix()+30*day {
		t.Fatalf("package term %d %s", mp.Code, mp.Raw)
	}
	rp := call("POST", "/api/v2/partner/reps/"+mp.str("id")+"/renew", pk.Key, map[string]any{"package": "month"})
	if tm := rp.JSON["term"].(map[string]any); rp.Code != 200 || rp.num("credit_gb") != 200 || int64(tm["expires_unix"].(float64)) != clock.Unix()+60*day {
		t.Fatalf("renew with a package: credit and days %d %s", rp.Code, rp.Raw)
	}
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, Packages: s.RepSale(ctx).Packages}); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateRepOrder(ctx, "", 777, "Buyer", "", "tgshop", "month")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE rep_orders SET status='pending' WHERE id=?`, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveRepOrder(ctx, "admin", "", o.ID); err != nil {
		t.Fatal(err)
	}
	if tm, _ := s.RepTermOf(ctx, "tgshop"); tm.Until != clock.Unix()+30*day || tm.OnExpiry != "suspend" || tm.GraceDays != 1 {
		t.Fatalf("bought in the bot: %+v", tm)
	}
}
