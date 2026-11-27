package hubcore

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

// Audit probes (money and policy). Each test logs what it saw and fails
// when the suspected bug is present.

func auditCredit(s *Store, rep string) (vol, units int64) {
	_ = s.db.QueryRow(`SELECT volume_credit,unlimited_units FROM reps WHERE id=?`, rep).Scan(&vol, &units)
	return
}

func auditRep(t *testing.T, s *Store, id string, pol RepPolicy, vol, units int64) {
	t.Helper()
	ctx := context.Background()
	if err := s.PutRep(ctx, Rep{ID: id, Name: id, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRepPolicy(ctx, id, pol); err != nil {
		t.Fatal(err)
	}
	if vol > 0 || units > 0 {
		if err := s.AdjustCredit(ctx, "owner", id, vol, units, ""); err != nil {
			t.Fatal(err)
		}
	}
}

// auditLegacy makes an old user the way importLegacyLink does (no network).
func auditLegacy(t *testing.T, s *Store, rep, email string, vol, oldUsed, expires int64) {
	t.Helper()
	ctx := context.Background()
	now := s.now().Unix()
	plan := "volume"
	if vol < 0 {
		plan = "unlimited"
	}
	c := Client{Email: email, Enabled: true, VolumeBytes: vol, ExpiresUnix: expires, Inbounds: []string{"de-vless"}, SubToken: "tok" + email + "0123456789"}
	if _, err := s.ImportClients(ctx, []ClientRecord{{Client: c, Plan: plan, RepID: rep, Note: "old user", Usage: Usage{Up: oldUsed, FirstSeen: now}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO legacy_clients(email,rep_id,last_used,imported_unix) VALUES(?,?,?,?)`, email, rep, oldUsed, now); err != nil {
		t.Fatal(err)
	}
}

func auditUse(t *testing.T, s *Store, email string, used int64) {
	t.Helper()
	now := s.now().Unix()
	if _, err := s.db.Exec(`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,0,?,?)
ON CONFLICT(email) DO UPDATE SET up=excluded.up, down=0, first_seen_unix=CASE WHEN client_usage.first_seen_unix=0 THEN excluded.first_seen_unix ELSE client_usage.first_seen_unix END, last_seen_unix=excluded.last_seen_unix`,
		email, used, now, now); err != nil {
		t.Fatal(err)
	}
}

// A client sent with both an expiry and a first-use term is charged and
// checked by termDays (the term) but served until the expiry.
func TestAuditBothTermFields(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, MaxDays: 30}, 100<<30, 5)
	c := Client{Email: "long", Enabled: true, VolumeBytes: -1, ExpiresUnix: now.Unix() + 3650*86400, DurationSeconds: 86400, Inbounds: []string{"de-vless"}}
	err := s.RepPutClient(ctx, "ali", "ali", c, "unlimited")
	_, units := auditCredit(s, "ali")
	v, _ := s.clientView(ctx, "long")
	t.Logf("err=%v units charged=%d effective_until-now=%d days (MaxDays 30)", err, 5-units, (v.EffectiveUntil-now.Unix())/86400)
	if err == nil && v.EffectiveUntil-now.Unix() > 30*86400 {
		t.Errorf("BUG: 10 years of unlimited for %d unit(s), past MaxDays=30", 5-units)
	}
}

// Old users are paid by use only, yet the expiry job refunds their unused
// volume as credit.
func TestAuditLegacyExpiryRefund(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 1<<30, 0)
	auditLegacy(t, s, "ali", "old1", 50<<30, 3<<30, now.Unix()+86400)
	before, _ := auditCredit(s, "ali")
	*now = now.Add(2 * 24 * time.Hour)
	n := s.runExpiryRefunds(ctx)
	after, _ := auditCredit(s, "ali")
	t.Logf("refunded clients=%d credit %d -> %d (+%d GB)", n, before, after, (after-before)>>30)
	if after > before {
		t.Errorf("BUG: an old user's never-paid volume came back as %d GB of credit", (after-before)>>30)
	}
}

// The PasarGuard API's on/off turns on an old user that was switched off
// because the credit ran out; with no credit its use is then free.
func TestAuditLegacyReenableWithoutCredit(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 1<<30, 0)
	auditLegacy(t, s, "ali", "old1", -1, 0, now.Unix()+30*86400)
	auditUse(t, s, "old1", 2<<30) // uses more than the credit
	s.chargeLegacy(ctx)
	v, _ := s.clientView(ctx, "old1")
	cr, _ := auditCredit(s, "ali")
	t.Logf("after use: credit=%d enabled=%v", cr, v.Enabled)
	if v.Enabled {
		t.Fatal("setup: should be off for no credit")
	}
	// The panel route refuses (repPutLegacy needs credit) ...
	vc := v.Client
	vc.Enabled = true
	errPanel := s.RepPutClient(ctx, "ali", "ali", vc, "unlimited")
	// ... the PasarGuard API does not.
	errPG := s.pgSetEnabled(ctx, "ali", "old1", true)
	auditUse(t, s, "old1", 50<<30)
	s.chargeLegacy(ctx)
	s.chargeLegacy(ctx)
	v, _ = s.clientView(ctx, "old1")
	cr, _ = auditCredit(s, "ali")
	var billed int64
	_ = s.db.QueryRow(`SELECT billed FROM legacy_clients WHERE email='old1'`).Scan(&billed)
	t.Logf("panel err=%v pg err=%v; after 48 GB more use: credit=%d enabled=%v billed=%d GB", errPanel, errPG, cr, v.Enabled, billed>>30)
	if errPG == nil && v.Enabled {
		t.Errorf("BUG: old user back on with no credit and stays on; 48 GB used for free")
	}
}

// A reset through the API charges an old user's whole usage (its old
// panel's included), on top of the use billing.
func TestAuditLegacyResetCharge(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 20<<30, 0)
	auditLegacy(t, s, "ali", "old1", 50<<30, 10<<30, now.Unix()+30*86400) // 10 GB used at the old panel
	auditUse(t, s, "old1", 11<<30)                                        // 1 GB here
	s.chargeLegacy(ctx)
	mid, _ := auditCredit(s, "ali")
	err := s.pgResetRep(ctx, "ali", "old1")
	after, _ := auditCredit(s, "ali")
	t.Logf("use billing took %d GB; reset err=%v took %d GB more", (20<<30-mid)>>30, err, (mid-after)>>30)
	if err == nil && mid-after > 1<<30 {
		t.Errorf("BUG: reset of an old user charged %d GB for 1 GB used here (already billed)", (mid-after)>>30)
	}
}

// An overdue bill stops spending, but the credit can still be moved to a
// sub-seller, which is not locked.
func TestAuditLockedParentTransfers(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 100<<30, 0)
	if _, err := s.db.Exec(`INSERT INTO rep_bills(unix,rep_id,amount,due_unix) VALUES(?,?,?,?)`, now.Unix()-2*86400, "ali", 500000, now.Unix()-86400); err != nil {
		t.Fatal(err)
	}
	c := Client{Email: "x1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 30*86400, Inbounds: []string{"de-vless"}}
	errDirect := s.RepPutClient(ctx, "ali", "ali", c, "volume")
	if err := s.PutSeller(ctx, "ali", Rep{ID: "sub1", Name: "s", Enabled: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	errMove := s.TransferCredit(ctx, "ali", "ali", "sub1", 50<<30, 0, "")
	_ = s.PutProduct(ctx, Principal{Role: "owner"}, Product{ID: "p10", Seller: "sub1", Name: "10G", Plan: "volume", GB: 10, Days: 30, Status: "published"})
	c.Email = "x2"
	errSub := s.RepPutClient(ctx, "sub1", "sub1", c, "volume")
	t.Logf("locked=%v direct sale err=%v; transfer err=%v; sub-seller sale err=%v", s.RepLocked(ctx, "ali"), errDirect, errMove, errSub)
	if errDirect != nil && errMove == nil {
		t.Errorf("BUG: a locked reseller moves 50 GB to its sub-seller, which sells (err=%v)", errSub)
	}
}

// DenyRenew: a used first-connection term rewritten as an expiry of the
// same length looks like no renewal (termDays is equal) yet adds days.
func TestAuditDenyRenewFirstUse(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 100<<30, 0)
	c := Client{Email: "fu", Enabled: true, VolumeBytes: 10 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	auditUse(t, s, "fu", 1<<30)
	*now = now.Add(29 * 24 * time.Hour)
	_ = s.SetRepPolicy(ctx, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, DenyRenew: true})
	before, _ := s.clientView(ctx, "fu")
	cr0, _ := auditCredit(s, "ali")
	c.DurationSeconds, c.ExpiresUnix = 0, now.Unix()+30*86400
	err := s.RepPutClient(ctx, "ali", "ali", c, "volume")
	after, _ := s.clientView(ctx, "fu")
	cr1, _ := auditCredit(s, "ali")
	t.Logf("err=%v days left %d -> %d, charged %d", err, (before.EffectiveUntil-now.Unix())/86400, (after.EffectiveUntil-now.Unix())/86400, cr0-cr1)
	if err == nil && after.EffectiveUntil > before.EffectiveUntil+86400 {
		t.Errorf("BUG: DenyRenew reseller added %d days for free", (after.EffectiveUntil-before.EffectiveUntil)/86400)
	}
}

// The owner renews a reseller's client for nothing; the renewal clears the
// gift columns, so the reseller can cash the free package in.
func TestAuditOwnerRenewRefundable(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, RefundUnused: true}, 10<<30, 0)
	c := Client{Email: "v1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 30*86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	auditUse(t, s, "v1", 10<<30)
	if _, err := s.RenewClient(ctx, Principal{User: "owner", Role: "owner"}, "v1", "volume", 100, 30); err != nil {
		t.Fatal(err)
	}
	b, _, err := s.RepDeleteClient(ctx, "ali", "ali", "v1")
	cr, _ := auditCredit(s, "ali")
	t.Logf("delete err=%v refund=%d GB credit now %d GB (paid 10 GB in total)", err, b>>30, cr>>30)
	if b > 0 {
		t.Errorf("BUG: the owner's free 100 GB renewal came back as %d GB of credit", b>>30)
	}
}

// An unlimited gift switched to volume keeps gift_units; switched back to
// a paid unlimited plan, the paid units are not refunded.
func TestAuditGiftUnitsStaleAfterPlanSwitch(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, RefundUnused: true}, 100<<30, 10)
	c := Client{Email: "g1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 3600, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	if err := s.applyNextPlan(ctx, "g1", NextPlan{Days: 60}); err != nil { // owner's gift: 60 unlimited days
		t.Fatal(err)
	}
	c.VolumeBytes, c.ExpiresUnix = 5<<30, now.Unix()+60*86400
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	c.VolumeBytes, c.ExpiresUnix = -1, now.Unix()+90*86400
	_, u0 := auditCredit(s, "ali")
	if err := s.RepPutClient(ctx, "ali", "ali", c, "unlimited"); err != nil {
		t.Fatal(err)
	}
	_, u1 := auditCredit(s, "ali")
	var gu int64
	_ = s.db.QueryRow(`SELECT gift_units FROM clients WHERE email='g1'`).Scan(&gu)
	_, ru, err := s.RepDeleteClient(ctx, "ali", "ali", "g1")
	t.Logf("paid %d units for 90 days; gift_units=%d; delete err=%v refunded %d units", u0-u1, gu, err, ru)
	if ru < u0-u1-1 {
		t.Errorf("BUG: stale gift_units=%d keep %d paid unit(s) from the refund", gu, u0-u1-ru)
	}
}

// A reset charges everything used, even past the volume it gives back.
func TestAuditResetChargesOverVolume(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 100<<30, 0)
	c := Client{Email: "r1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 30*86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	auditUse(t, s, "r1", 13<<30) // node stats arrive late: 3 GB over
	cr0, _ := auditCredit(s, "ali")
	err := s.pgResetRep(ctx, "ali", "r1")
	cr1, _ := auditCredit(s, "ali")
	t.Logf("err=%v charged %d GB to get 10 GB back", err, (cr0-cr1)>>30)
	if cr0-cr1 > 10<<30 {
		t.Errorf("BUG: reset charged %d GB for a 10 GB volume", (cr0-cr1)>>30)
	}
}

// The owner moves an old user away; its use is still billed to the
// reseller that brought it.
func TestAuditLegacyTransferKeepsBilling(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 10<<30, 0)
	auditRep(t, s, "reza", RepPolicy{AllowUnlimited: true, CanDelete: true}, 10<<30, 0)
	auditLegacy(t, s, "ali", "old1", -1, 0, now.Unix()+30*86400)
	if err := s.TransferClient(ctx, "old1", "reza"); err != nil {
		t.Fatal(err)
	}
	auditUse(t, s, "old1", 4<<30)
	s.chargeLegacy(ctx)
	a, _ := auditCredit(s, "ali")
	r, _ := auditCredit(s, "reza")
	t.Logf("after 4 GB of reza's client: ali=%d GB reza=%d GB", a>>30, r>>30)
	if a < 10<<30 {
		t.Errorf("BUG: ali pays %d GB for a client now owned by reza", (10<<30-a)>>30)
	}
}

// Gift units are withheld whole from a delete refund even after the gift's
// days went by, so days the reseller paid for later are kept.
func TestAuditGiftDaysElapsed(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, RefundUnused: true}, 100<<30, 10)
	c := Client{Email: "g2", Enabled: true, VolumeBytes: -1, ExpiresUnix: now.Unix() + 3600, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "unlimited"); err != nil {
		t.Fatal(err)
	}
	if err := s.applyNextPlan(ctx, "g2", NextPlan{Days: 60}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(45 * 24 * time.Hour) // 15 gift days left
	v, _ := s.clientView(ctx, "g2")
	c.ExpiresUnix = v.ExpiresUnix + 60*86400
	_, u0 := auditCredit(s, "ali")
	if err := s.RepPutClient(ctx, "ali", "ali", c, "unlimited"); err != nil {
		t.Fatal(err)
	}
	_, u1 := auditCredit(s, "ali")
	_, ru, err := s.RepDeleteClient(ctx, "ali", "ali", "g2")
	t.Logf("paid %d units for 60 days added; deleted at once: err=%v refunded %d units", u0-u1, err, ru)
	if ru < u0-u1 {
		t.Errorf("BUG: %d paid, unused unit(s) withheld as 'gift' after the gift days went by", u0-u1-ru)
	}
}

// A next plan that starts because the term ran out replaces the package;
// the expiry refund skips clients with a next plan, so unused paid volume
// is never returned.
func TestAuditNextPlanEatsPaidVolume(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, RefundUnused: true}, 50<<30, 0)
	c := Client{Email: "n1", Enabled: true, VolumeBytes: 50 << 30, ExpiresUnix: now.Unix() + 86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	auditUse(t, s, "n1", 5<<30)
	if err := s.SetNextPlan(ctx, "n1", &NextPlan{GB: 10, Days: 30}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * 24 * time.Hour)
	s.runNextPlans(ctx)
	s.runExpiryRefunds(ctx)
	b, _, _ := s.RepDeleteClient(ctx, "ali", "ali", "n1")
	cr, _ := auditCredit(s, "ali")
	t.Logf("45 GB unused when the term ended; credit now %d GB (delete refund %d)", cr>>30, b>>30)
	if cr < 45<<30 {
		t.Errorf("BUG: %d GB of paid, unused volume vanished when the owner's next plan started", (45<<30-cr)>>30)
	}
}

// A deleted reseller's wallet and bills stay under its id; a new reseller
// given that id inherits them.
func TestAuditRepIDReuseInheritsWalletAndBills(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true}, 0, 0)
	if _, _, err := s.RepWalletAdjust(ctx, "owner", "ali", 2_000_000, "top-up"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO rep_bills(unix,rep_id,amount,due_unix) VALUES(?,?,?,?)`, now.Unix(), "ali", 9_000_000, now.Unix()+3600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRep(ctx, "ali", true); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Hour)
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Password: "0123456789ab", CreditGB: 10}); err != nil {
		t.Fatal(err)
	}
	c := Client{Email: "n1", Enabled: true, VolumeBytes: 1 << 30, ExpiresUnix: now.Unix() + 86400, Inbounds: []string{"de-vless"}}
	err := s.RepPutClient(ctx, "ali", "ali", c, "volume")
	t.Logf("new 'ali': wallet=%d locked=%v sale err=%v", s.RepWallet(ctx, "ali"), s.RepLocked(ctx, "ali"), err)
	if s.RepWallet(ctx, "ali") != 0 || s.RepLocked(ctx, "ali") {
		t.Errorf("BUG: a new reseller inherits the deleted one's wallet/overdue bill")
	}
}

// A first-connection term near MaxInt64 overflows termDays to a negative
// number: no unit is charged, MaxDays passes, and the term never ends.
func TestAuditDurationOverflow(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, MaxDays: 30}, 0, 1)
	const huge = int64(9223372036854775807)
	c := Client{Email: "forever", Enabled: true, VolumeBytes: -1, DurationSeconds: huge, Inbounds: []string{"de-vless"}}
	err := s.RepPutClient(ctx, "ali", "ali", c, "unlimited")
	_, units := auditCredit(s, "ali")
	auditUse(t, s, "forever", 1)
	v, _ := s.clientView(ctx, "forever")
	t.Logf("panel route: err=%v units left=%d (of 1); effective_until=%d", err, units, v.EffectiveUntil)

	// The same through the PasarGuard API (on_hold_expire_duration has no cap).
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	pc := pgClient{t, h.URL}
	a, _ := s.EnablePGAPI(ctx, "ali")
	tok := pc.login("mirza_ali", a.Password)
	code, out := pc.obj("POST", "/api/user", tok, map[string]any{"username": "forever2", "data_limit": 0, "status": "on_hold", "on_hold_expire_duration": huge})
	_, units2 := auditCredit(s, "ali")
	auditUse(t, s, "forever2", 1)
	v2, _ := s.clientView(ctx, "forever2")
	t.Logf("pg api: code=%d status=%v units left=%d effective_until=%d (now %d)", code, out["status"], units2, v2.EffectiveUntil, now.Unix())
	if err == nil && units == 1 && v.EffectiveUntil <= 0 {
		t.Errorf("BUG: unlimited client that never expires for 0 units (panel)")
	}
	if code == 201 && units2 == units && v2.EffectiveUntil <= 0 {
		t.Errorf("BUG: unlimited client that never expires for 0 units (PasarGuard API)")
	}
}

// DenyEdit covers on/off and the IP limit, but not for old users.
func TestAuditLegacyDenyEdit(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	auditRep(t, s, "ali", RepPolicy{AllowUnlimited: true, CanDelete: true, DenyEdit: true}, 10<<30, 0)
	auditLegacy(t, s, "ali", "old1", -1, 0, now.Unix()+30*86400)
	v, _ := s.clientView(ctx, "old1")
	c := v.Client
	c.MaxIPs = 50
	err := s.RepPutClient(ctx, "ali", "ali", c, "unlimited")
	v, _ = s.clientView(ctx, "old1")
	t.Logf("err=%v max_ips=%d", err, v.MaxIPs)
	if err == nil && v.MaxIPs == 50 {
		t.Errorf("BUG: DenyEdit reseller changed an old user's IP limit")
	}
}
