package hubcore

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
)

// Regression tests for the external API review findings.
func TestExternalAPIFixes(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	call := func(method, path, key string, body any, extra ...string) apiResp {
		t.Helper()
		return apiCall(t, h.URL, method, path, key, body, extra...)
	}
	if err := s.PutRep(ctx, Rep{ID: "tpl", Name: "Template", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepPolicy(ctx, "tpl", RepPolicy{MaxClients: 50, MaxDays: 30, CanDelete: true, PriceGB: 900}); err != nil {
		t.Fatal(err)
	}
	// Package ids keep the letter case the owner typed.
	if err := s.PutRepSale(ctx, RepSale{Packages: []RepPackage{{ID: "Gold", Name: "Gold", GB: 100, Price: 1000}}}); err != nil {
		t.Fatal(err)
	}
	all := append([]string{}, partnerScopes...)
	gold := []string{"Gold"}
	k, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site", MaxGB: 1000, Template: "tpl", Scopes: &all, Packages: &gold})
	if err != nil || len(k.Packages) != 1 || k.Packages[0] != "Gold" {
		t.Fatalf("a package id with capitals: %+v %v", k, err)
	}
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": "gold-shop", "name": "G", "package": "Gold"}); r.Code != 200 || r.num("credit_gb") != 100 {
		t.Fatalf("selling it: %d %s", r.Code, r.Raw)
	}

	// A partner's policy change keeps what the owner changed since.
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": "shop", "name": "Shop", "credit_gb": 100}); r.Code != 200 {
		t.Fatalf("create %d %s", r.Code, r.Raw)
	}
	pol, _ := s.RepPolicyOf(ctx, "shop")
	pol.PriceGB, pol.MaxClients = 1200, 500 // the owner, in the panel
	if err := s.SetRepPolicy(ctx, "shop", pol); err != nil {
		t.Fatal(err)
	}
	if r := call("PATCH", "/api/v2/partner/reps/shop", k.Key, map[string]any{"policy": map[string]any{"deny_renew": true, "max_days": 90}}); r.Code != 200 {
		t.Fatalf("patch %d %s", r.Code, r.Raw)
	}
	got, _ := s.RepPolicyOf(ctx, "shop")
	if got.PriceGB != 1200 || got.MaxClients != 500 || !got.DenyRenew || got.MaxDays != 30 {
		t.Fatalf("owner's price and cap kept, sent fields clamped: %+v", got)
	}
	// Bad values are refused before anything changes.
	if r := call("PATCH", "/api/v2/partner/reps/shop", k.Key, map[string]any{"note": "x", "low_credit_gb": -1}); r.Code != 422 {
		t.Fatalf("bad low_credit_gb on change: %d", r.Code)
	}
	if p, _ := s.partnerPanel(ctx, k.ID, "shop"); p.Note != "" {
		t.Fatalf("nothing saved from a refused change: %+v", p)
	}
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "X", "low_credit_gb": 5000000}); r.Code != 422 {
		t.Fatalf("bad low_credit_gb on create: %d", r.Code)
	}

	// Credit that comes back never gives the key more than it gave.
	given := func() float64 { kk, _ := s.partnerKey(ctx, k.ID); return kk.GivenGB }
	if err := s.AdjustCredit(ctx, "admin", "shop", 400<<30, 0, "owner gift"); err != nil {
		t.Fatal(err)
	}
	before := given()
	if r := call("POST", "/api/v2/partner/reps/shop/credit/revoke", k.Key, map[string]any{"credit_gb": 300}); r.Code != 200 || r.num("credit_gb") != 200 {
		t.Fatalf("revoke %d %s", r.Code, r.Raw)
	}
	if given() != before-100 {
		t.Fatalf("only the key's 100 GB come back to it: %v → %v", before, given())
	}
	if r := call("POST", "/api/v2/partner/reps/shop/credit", k.Key, map[string]any{"credit_gb": 20}); r.Code != 200 {
		t.Fatalf("add %d", r.Code)
	}
	before = given()
	if r := call("DELETE", "/api/v2/partner/reps/shop", k.Key, nil); r.Code != 200 || r.num("released_gb") != 20 {
		t.Fatalf("delete releases what the key gave: %d %s", r.Code, r.Raw)
	}
	if given() != before-20 {
		t.Fatalf("delete %v → %v", before, given())
	}

	// external_ref is unique even for requests at the same moment.
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Race", "credit_gb": 1, "external_ref": "order-9"}).Code
		}(i)
	}
	wg.Wait()
	made := 0
	for _, c := range codes {
		switch c {
		case 200:
			made++
		case 409:
		default:
			t.Fatalf("parallel creates: %v", codes)
		}
	}
	if list, _, _ := s.partnerPanels(ctx, k.ID, panelQuery{ExternalRef: "order-9"}); made != 1 || len(list) != 1 {
		t.Fatalf("one panel per external_ref: %v, %d panels", codes, len(list))
	}
	if kk, _ := s.partnerKey(ctx, k.ID); kk.Panels != 2 { // gold-shop and the race winner
		t.Fatalf("the losers left nothing behind: %d", kk.Panels)
	}

	// A key's expiry can be taken off again.
	exp := clock.Unix() + 3600
	if _, err := s.UpdatePartnerKey(ctx, k.ID, PartnerKeyIn{Name: "site", MaxGB: 1000, Template: "tpl", Expires: &exp}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if kk, _ := s.UpdatePartnerKey(ctx, k.ID, PartnerKeyIn{Name: "site", MaxGB: 1000, Template: "tpl", Expires: &zero}); kk.Expires != 0 {
		t.Fatalf("never expires again: %d", kk.Expires)
	}

	// Quotes: exact amounts, and a taken name is refused.
	if err := s.CreateRep(ctx, "admin", RepCreate{ID: "res", Password: "res-password-1", CreditGB: 1}); err != nil {
		t.Fatal(err)
	}
	rk, _ := s.CreateResKey(ctx, "res", "test", ResKeyIn{Name: "bot"})
	if q := call("POST", "/api/v2/reseller/quote", rk.Key, map[string]any{"gb": 1.0001, "days": 30}); q.JSON["allowed"] != false || q.str("code") != codeCredit {
		t.Fatalf("a hair over the credit: %s", q.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", rk.Key, map[string]any{"gb": 1, "days": 30}); q.JSON["allowed"] != true {
		t.Fatalf("exactly the credit: %s", q.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", rk.Key, map[string]any{"name": "alice", "gb": 0.1, "days": 30}); q.JSON["allowed"] != false || q.str("code") != codeConflict {
		t.Fatalf("a taken name: %s", q.Raw)
	}
	if r := call("POST", "/api/v2/reseller/clients/bulk", rk.Key, map[string]any{"prefix": "q", "count": 3, "gb": 0.3334, "days": 30}); r.Code != 402 {
		t.Fatalf("bulk a hair over the credit: %d %s", r.Code, r.Raw)
	}
	if r := call("GET", "/api/v2/reseller/clients", rk.Key, nil); r.num("total") != 0 {
		t.Fatalf("nothing made: %s", r.Raw)
	}
}

// The partner_reps upgrade: what each key gave each panel comes from the
// ledger, and duplicate external_refs are cleared before the unique index.
func TestPartnerRepsUpgrade(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	all := append([]string{}, partnerScopes...)
	k, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site", Scopes: &all})
	for _, id := range []string{"a1", "a2"} {
		if r := apiCall(t, h.URL, "POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": id, "name": id, "credit_gb": 30}); r.Code != 200 {
			t.Fatal(r.Code, string(r.Raw))
		}
	}
	apiCall(t, h.URL, "POST", "/api/v2/partner/reps/a1/credit", k.Key, map[string]any{"credit_gb": 10})
	apiCall(t, h.URL, "POST", "/api/v2/partner/reps/a1/credit/revoke", k.Key, map[string]any{"credit_gb": 5})
	_ = s.AdjustCredit(ctx, "admin", "a1", 100<<30, 0, "owner gift")
	// Back to the old shape: no given columns, a plain index, two panels with one ref.
	for _, q := range []string{`DROP INDEX partner_reps_ref_u`, `ALTER TABLE partner_reps DROP COLUMN given_bytes`, `ALTER TABLE partner_reps DROP COLUMN given_units`,
		`UPDATE partner_reps SET external_ref='same'`, `INSERT INTO partner_reps(rep_id,key_id,created_unix) VALUES('gone','x',1)`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err := s.ensurePartnerSchema(); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.partnerGivenTo(ctx, k.ID, "a1"); b != 35<<30 {
		t.Fatalf("a1 given %d GB, want 35 (30 + 10 - 5, not the owner's gift)", b>>30)
	}
	if b, _ := s.partnerGivenTo(ctx, k.ID, "a2"); b != 30<<30 {
		t.Fatalf("a2 given %d GB", b>>30)
	}
	var refs, gone int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM partner_reps WHERE external_ref='same'`).Scan(&refs)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM partner_reps WHERE rep_id='gone'`).Scan(&gone)
	if refs != 1 || gone != 0 {
		t.Fatalf("one panel keeps the ref (%d), rows of deleted resellers go (%d)", refs, gone)
	}
	if err := s.ensurePartnerSchema(); err != nil { // and again, as on every start
		t.Fatal(err)
	}
}

// Rules that came with v2.52–v2.65 hold for the reseller API too: an overdue
// bill stops sales (and the quote says so), old users keep their inbounds and
// switch on only with credit, connected shops' clients are not resellers'.
func TestResellerAPIWithLaterRules(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	call := func(method, path, key string, body any) apiResp {
		t.Helper()
		return apiCall(t, h.URL, method, path, key, body)
	}
	if err := s.CreateRep(ctx, "admin", RepCreate{ID: "res", Password: "res-password-1", CreditGB: 10}); err != nil {
		t.Fatal(err)
	}
	k, _ := s.CreateResKey(ctx, "res", "test", ResKeyIn{Name: "bot"})

	// An overdue bill.
	if _, err := s.db.Exec(`INSERT INTO rep_bills(unix,rep_id,amount,due_unix) VALUES(?,?,?,?)`, clock.Unix(), "res", 1000, clock.Unix()-60); err != nil {
		t.Fatal(err)
	}
	if me := call("GET", "/api/v2/reseller/me", k.Key, nil); me.JSON["locked"] != true {
		t.Fatalf("me shows the lock: %s", me.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", k.Key, map[string]any{"gb": 1, "days": 30}); q.JSON["allowed"] != false || q.str("code") != codePolicy {
		t.Fatalf("quote under an overdue bill: %s", q.Raw)
	}
	if r := call("POST", "/api/v2/reseller/clients", k.Key, map[string]any{"gb": 1, "days": 30}); r.Code != 403 || r.str("code") != codePolicy {
		t.Fatalf("create under an overdue bill: %d %s", r.Code, r.Raw)
	}
	if _, err := s.db.Exec(`UPDATE rep_bills SET status='paid' WHERE rep_id='res'`); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/api/v2/reseller/clients", k.Key, map[string]any{"name": "old1", "gb": 1, "days": 30}); r.Code != 201 {
		t.Fatalf("create after paying: %d %s", r.Code, r.Raw)
	}

	// An old user: no renewal, no inbound change, on again only with credit.
	if _, err := s.db.Exec(`INSERT INTO legacy_clients(email,rep_id,credit_off) VALUES('old1','res',1)`); err != nil {
		t.Fatal(err)
	}
	if q := call("POST", "/api/v2/reseller/quote", k.Key, map[string]any{"action": "renew", "name": "old1", "gb": 1, "days": 30}); q.JSON["allowed"] != false || q.str("code") != codePolicy {
		t.Fatalf("renew quote for an old user: %s", q.Raw)
	}
	if r := call("PATCH", "/api/v2/reseller/clients/old1", k.Key, map[string]any{"inbounds": []string{"de-vless", "nope"}}); r.Code != 403 {
		t.Fatalf("old user's inbounds: %d %s", r.Code, r.Raw)
	}
	if r := call("POST", "/api/v2/reseller/clients/old1/disable", k.Key, nil); r.Code != 200 {
		t.Fatalf("disable %d", r.Code)
	}
	if err := s.AdjustCredit(ctx, "admin", "res", -9<<30, 0, "spent"); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/api/v2/reseller/clients/old1/enable", k.Key, nil); r.Code != 402 || r.str("code") != codeCredit {
		t.Fatalf("an old user on without credit: %d %s", r.Code, r.Raw)
	}
	if err := s.AdjustCredit(ctx, "admin", "res", 5<<30, 0, "top-up"); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/api/v2/reseller/clients/old1/enable", k.Key, nil); r.Code != 200 || r.JSON["enabled"] != true {
		t.Fatalf("an old user on with credit: %d %s", r.Code, r.Raw)
	}
	var off int
	_ = s.db.QueryRow(`SELECT credit_off FROM legacy_clients WHERE email='old1'`).Scan(&off)
	if off != 0 {
		t.Fatal("switched by hand, not for lack of credit")
	}

	// A connected shop's client is left to the shop's own feed.
	clientStateEvents(ctx, s)
	if _, err := s.db.Exec(`UPDATE clients SET rep_id='~shop1' WHERE email='alice'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, clock.Unix(), "", "alice", "reset:manual"); err != nil {
		t.Fatal(err)
	}
	hubLogEvents(ctx, s) // first run: starts at the end of the log
	if _, err := s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, clock.Unix(), "", "alice", "reset:manual"); err != nil {
		t.Fatal(err)
	}
	hubLogEvents(ctx, s)
	clientStateEvents(ctx, s)
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM api_events WHERE target LIKE 'rep:~%'`).Scan(&n)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM api_client_state WHERE rep_id LIKE '~%'`).Scan(&off)
	if n != 0 || off != 0 {
		t.Fatalf("connected shop's client in reseller events: %d events, %d states", n, off)
	}
	// Names are one space in any letter case.
	if r := call("POST", "/api/v2/reseller/clients", k.Key, map[string]any{"name": "OLD1", "gb": 1, "days": 30}); r.Code != 409 {
		t.Fatalf("a name differing only in letter case: %d %s", r.Code, r.Raw)
	}
	if q := call("POST", "/api/v2/reseller/quote", k.Key, map[string]any{"name": "Old1", "gb": 1, "days": 30}); q.str("code") != codeConflict {
		t.Fatalf("quote for a name differing only in letter case: %s", q.Raw)
	}
}
