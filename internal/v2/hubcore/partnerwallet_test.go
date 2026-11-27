package hubcore

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPartnerWallet(t *testing.T) {
	s, clock := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	call := func(method, path, key string, body any, extra ...string) apiResp {
		t.Helper()
		return apiCall(t, h.URL, method, path, key, body, extra...)
	}
	if err := s.PutRepSale(ctx, RepSale{Packages: []RepPackage{{ID: "month", Name: "Monthly", GB: 100, Price: 1_000_000, Days: 30}}}); err != nil {
		t.Fatal(err)
	}
	wallet, gb, unit, month, disc := "wallet", int64(5000), int64(30000), int64(200000), 20
	k, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "shop", Billing: &wallet, PriceGB: &gb, PriceUnit: &unit, PriceMonth: &month, PackageDiscount: &disc})
	if err != nil || k.Billing != "wallet" || k.PriceGB != 5000 || k.PackageDiscount != 20 || k.Balance != 0 {
		t.Fatalf("%+v %v", k, err)
	}
	bad := "barter"
	if _, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "x", Billing: &bad}); err == nil {
		t.Fatal("a wrong billing")
	}

	// Nothing is made without money.
	r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "A", "credit_gb": 10})
	if r.Code != 402 || r.str("code") != codeFunds {
		t.Fatalf("an empty wallet: %d %s", r.Code, r.Raw)
	}
	if kk, _ := s.partnerKey(ctx, k.ID); kk.Panels != 0 || kk.GivenGB != 0 {
		t.Fatalf("nothing left behind: %+v", kk)
	}
	if bal, err := s.PartnerWalletAdjust(ctx, "admin", k.ID, 2_000_000, "card #12"); err != nil || bal != 2_000_000 {
		t.Fatalf("top-up %d %v", bal, err)
	}
	if _, err := s.PartnerWalletAdjust(ctx, "admin", k.ID, -5_000_000, ""); err == nil {
		t.Fatal("taking more than is there")
	}

	// Quotes: credit, term and packages (less the discount).
	q := call("POST", "/api/v2/partner/quote", k.Key, map[string]any{"action": "create", "credit_gb": 10.5, "units": 2, "days": 31})
	if q.Code != 200 || q.num("cost") != 10.5*5000+2*30000+2*200000 || q.JSON["allowed"] != true || q.num("balance_after") != 2_000_000-q.num("cost") {
		t.Fatalf("quote %s", q.Raw)
	}
	if q := call("POST", "/api/v2/partner/quote", k.Key, map[string]any{"action": "renew", "package": "month"}); q.num("cost") != 800_000 || q.num("term_days") != 30 || q.num("credit_gb") != 100 {
		t.Fatalf("package quote %s", q.Raw)
	}
	if q := call("POST", "/api/v2/partner/quote", k.Key, map[string]any{"action": "credit", "credit_gb": 1000}); q.JSON["allowed"] != false || q.str("code") != codeFunds {
		t.Fatalf("over the balance %s", q.Raw)
	}

	// Making, adding credit and renewing take the quoted price; once, even on a retry.
	p := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": "a", "name": "A", "credit_gb": 10, "term_days": 30}, "Idempotency-Key", "o1")
	call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": "a", "name": "A", "credit_gb": 10, "term_days": 30}, "Idempotency-Key", "o1")
	if p.Code != 200 {
		t.Fatalf("create %d %s", p.Code, p.Raw)
	}
	bal := func() int64 { return s.PartnerBalance(ctx, k.ID) }
	if bal() != 2_000_000-50_000-200_000 {
		t.Fatalf("charged once for 10 GB and a month: %d", bal())
	}
	if r := call("POST", "/api/v2/partner/reps/a/credit", k.Key, map[string]any{"credit_gb": 20}); r.Code != 200 || bal() != 1_750_000-100_000 {
		t.Fatalf("credit %d %d", r.Code, bal())
	}
	if r := call("POST", "/api/v2/partner/reps/a/renew", k.Key, map[string]any{"package": "month"}); r.Code != 200 || bal() != 1_650_000-800_000 || r.num("credit_gb") != 130 {
		t.Fatalf("renew with a package %d %d %s", r.Code, bal(), r.Raw)
	}
	if r := call("POST", "/api/v2/partner/reps/a/renew", k.Key, map[string]any{"days": 150}); r.Code != 402 || bal() != 850_000 {
		t.Fatalf("a renewal the wallet cannot pay: %d %d", r.Code, bal())
	}
	end := int64(call("GET", "/api/v2/partner/reps/a", k.Key, nil).JSON["term"].(map[string]any)["expires_unix"].(float64))
	if end != clock.Unix()+60*86400 {
		t.Fatalf("the refused renewal changed nothing: %d", end)
	}

	// A charge whose action fails comes back.
	if r := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"id": "b", "name": "B", "credit_gb": 1, "password": "short"}); r.Code != 422 {
		t.Fatalf("a panel that cannot be made: %d %s", r.Code, r.Raw)
	}
	if bal() != 850_000 {
		t.Fatalf("refunded: %d", bal())
	}
	// Taking credit back and deleting pay nothing back in money.
	all := append([]string{}, partnerScopes...)
	if _, err := s.UpdatePartnerKey(ctx, k.ID, PartnerKeyIn{Name: "shop", Scopes: &all}); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/api/v2/partner/reps/a/credit/revoke", k.Key, map[string]any{"credit_gb": 5}); r.Code != 200 || bal() != 850_000 {
		t.Fatalf("revoke %d %d", r.Code, bal())
	}

	// The wallet's history, for the partner and the owner.
	wl := call("GET", "/api/v2/partner/wallet", k.Key, nil)
	items := wl.JSON["items"].([]any)
	if wl.num("balance") != 850_000 || wl.str("billing") != "wallet" || len(items) != 6 {
		t.Fatalf("wallet %s", wl.Raw)
	}
	kinds := []string{}
	for _, it := range items {
		kinds = append(kinds, it.(map[string]any)["kind"].(string))
	}
	if strings.Join(kinds, ",") != "refund,charge,charge,charge,charge,topup" {
		t.Fatalf("history %v", kinds)
	}
	if aw := call("GET", "/api/v2/admin/partner-keys/"+k.ID+"/wallet", "admintok", nil); aw.num("balance") != 850_000 {
		t.Fatalf("owner view %s", aw.Raw)
	}
	if top := call("POST", "/api/v2/admin/partner-keys/"+k.ID+"/wallet", "admintok", map[string]any{"amount": 150_000, "note": "card"}); top.num("balance") != 1_000_000 {
		t.Fatalf("owner top-up %s", top.Raw)
	}
	ev := eventKinds(t, s, partnerTarget(k.ID))
	if countOf(ev, "wallet.topped_up") != 2 || countOf(ev, "wallet.charged") != 4 || countOf(ev, "wallet.refunded") != 1 {
		t.Fatalf("wallet events %v", ev)
	}
	if _, err := s.db.Exec(`DELETE FROM partner_wallet WHERE key_id=?`, k.ID); err == nil {
		t.Fatal("the wallet is append-only")
	}

	// A cap key pays nothing; switching a key to cap keeps its history.
	capKey, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "trusted"})
	if q := call("POST", "/api/v2/partner/quote", capKey.Key, map[string]any{"credit_gb": 100}); q.num("cost") != 0 || q.JSON["allowed"] != true {
		t.Fatalf("cap quote %s", q.Raw)
	}
	if r := call("POST", "/api/v2/partner/reps", capKey.Key, map[string]any{"name": "B", "credit_gb": 100, "term_days": 30}); r.Code != 200 {
		t.Fatalf("cap create %d", r.Code)
	}
}
