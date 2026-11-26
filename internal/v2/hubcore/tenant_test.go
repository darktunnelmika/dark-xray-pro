package hubcore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func i64(v int64) *int64    { return &v }
func strp(v string) *string { return &v }

// tenantStore is a store with the seed clients (alice, bob — the owner's),
// a reseller "r1" with client "r1c", and tenant "tish" with client "u1".
func tenantStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u1", VolumeBytes: i64(5 << 30), Days: i64(30), ExternalRef: strp("order-1")}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRep(ctx, Rep{ID: "r1", Name: "R1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutClient(ctx, Client{Email: "r1c", Enabled: true, VolumeBytes: 1 << 30, Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.TransferClient(ctx, "r1c", "r1"); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func TestTenantClientIsStoredInItsOwnSpace(t *testing.T) {
	s, ctx := tenantStore(t)
	var rep, email string
	var rev int64
	if err := s.db.QueryRow(`SELECT email,rep_id,rev FROM clients WHERE external_ref='order-1'`).Scan(&email, &rep, &rev); err != nil {
		t.Fatal(err)
	}
	if email != "tish+u1" || rep != "~tish" || rev == 0 {
		t.Fatalf("stored as %q %q rev %d", email, rep, rev)
	}
	c, err := s.TenantClient(ctx, "tish", "u1")
	if err != nil || c.Name != "u1" || c.ExternalRef != "order-1" || c.VolumeBytes != 5<<30 || c.Status != "active" || len(c.Inbounds) != 1 || c.UUID == "" || c.SubToken == "" {
		t.Fatalf("%+v %v", c, err)
	}
	if got, err := s.TenantClientByRef(ctx, "tish", "order-1"); err != nil || got.Name != "u1" {
		t.Fatalf("by ref %+v %v", got, err)
	}
	if s.TenantOfClient(ctx, "tish+u1") != "tish" || s.TenantOfClient(ctx, "alice") != "" {
		t.Fatal("TenantOfClient")
	}
}

func TestTenantClientsStayOutOfTheOwnersViews(t *testing.T) {
	s, ctx := tenantStore(t)
	owner := Principal{User: "admin", Role: "owner"}
	list, _ := s.Clients(ctx)
	for _, c := range list {
		if strings.HasPrefix(c.Email, "tish+") {
			t.Fatal("tenant client in Clients()")
		}
	}
	if len(list) != 3 {
		t.Fatalf("owner world has %d clients, want 3", len(list))
	}
	if _, err := s.ClientByEmail(ctx, "tish+u1"); err == nil {
		t.Fatal("ClientByEmail (bots) must not find a tenant client")
	}
	if a, err := s.Analytics(ctx, owner, ""); err != nil || a.Total != 3 {
		t.Fatalf("analytics total %d %v", a.Total, err)
	}
	if recs, _ := s.ExportClients(ctx); len(recs) != 3 {
		t.Fatalf("export has %d", len(recs))
	}
	mine, _ := s.scopedClients(ctx, Principal{Role: "rep", RepID: "r1"})
	if len(mine) != 1 || mine[0].Email != "r1c" {
		t.Fatalf("reseller sees %+v", mine)
	}
	now := s.now().Unix()
	day := s.now().UTC().Format("2006-01-02")
	_, _ = s.db.Exec(`INSERT INTO online(email,node_id,ips,seen_unix) VALUES('tish+u1','de-1','["1.1.1.1"]',?),('alice','de-1','["2.2.2.2"]',?)`, now, now)
	_, _ = s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('tish+u1','de-1',?,500,500),('alice','de-1',?,1,1)`, day, day)
	_, _ = s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(?,'de-1','tish+u1','blocked:volume')`, now)
	on, _ := s.Online(ctx)
	if len(on) != 1 || on[0].Email != "alice" {
		t.Fatalf("online %+v", on)
	}
	in, _ := s.Insights(ctx, 7)
	for _, u := range in.Top {
		if u.Email == "tish+u1" {
			t.Fatal("tenant client in the top list")
		}
	}
	if len(in.Events) != 0 {
		t.Fatalf("tenant event in insights: %+v", in.Events)
	}
	if err := s.TgLink(ctx, 42, "tish+u1"); err == nil {
		t.Fatal("a tenant client must not be linked to the panel bot")
	}
	sum, _ := s.Tenants(ctx)
	if len(sum) != 1 || sum[0].Clients != 1 || sum[0].Online != 1 || sum[0].Volume != 5<<30 {
		t.Fatalf("summary %+v", sum)
	}
}

func TestTenantEventsAndRevisions(t *testing.T) {
	s, ctx := tenantStore(t)
	_, _ = s.db.Exec(`INSERT INTO events(unix,node_id,email,kind) VALUES(1,'de-1','tish+u1','blocked:volume'),(2,'de-1','alice','blocked:volume')`)
	var tid string
	_ = s.db.QueryRow(`SELECT tenant_id FROM events WHERE email='tish+u1'`).Scan(&tid)
	if tid != "tish" {
		t.Fatalf("event tenant %q", tid)
	}
	w := WebhookSettings{Enabled: true, URL: "https://x", Kinds: []string{"client"}}
	items, _, _, _, err := s.webhookBatch(ctx, w, nil)
	if err != nil || len(items) != 1 || items[0].Client != "alice" {
		t.Fatalf("panel webhook got %+v %v", items, err)
	}
	before, _ := s.TenantClient(ctx, "tish", "u1")
	if _, err := s.TenantUpdate(ctx, "tish", "u1", TenantClientIn{Enabled: new(bool)}); err != nil {
		t.Fatal(err)
	}
	after, _ := s.TenantClient(ctx, "tish", "u1")
	if after.Rev <= before.Rev || after.Enabled {
		t.Fatalf("rev %d -> %d enabled %v", before.Rev, after.Rev, after.Enabled)
	}
	// A usage batch stamps the batch's revision on the usage row.
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1, FromUnix: 1, ToUnix: 2, Traffic: []wire.UserTraffic{{Email: "tish+u1", Up: 10, Down: 20}}}); err != nil {
		t.Fatal(err)
	}
	used, _ := s.TenantClient(ctx, "tish", "u1")
	if used.Rev <= after.Rev || used.UsedBytes != 30 {
		t.Fatalf("usage rev %d used %d", used.Rev, used.UsedBytes)
	}
	if err := s.TenantDelete(ctx, "tish", "u1"); err != nil {
		t.Fatal(err)
	}
	var name string
	var rev int64
	if err := s.db.QueryRow(`SELECT name,rev FROM tenant_tombstones WHERE tenant_id='tish'`).Scan(&name, &rev); err != nil || name != "tish+u1" || rev <= used.Rev {
		t.Fatalf("tombstone %q %d %v", name, rev, err)
	}
	// The owner's clients do not move the sequence.
	var seq int64
	_ = s.db.QueryRow(`SELECT v FROM rev_seq`).Scan(&seq)
	_ = s.PutClient(ctx, Client{Email: "carol", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}})
	var seq2 int64
	_ = s.db.QueryRow(`SELECT v FROM rev_seq`).Scan(&seq2)
	if seq2 != seq {
		t.Fatalf("owner client moved the sequence %d -> %d", seq, seq2)
	}
}

func TestTenantNamesAreReserved(t *testing.T) {
	s, ctx := tenantStore(t)
	if err := s.PutClient(ctx, Client{Email: "tish+u2", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}}); err == nil {
		t.Fatal("the owner made a name in the tenant's space")
	}
	if err := s.PutClient(ctx, Client{Email: "tish+u1", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}}); err == nil {
		t.Fatal("the owner overwrote a tenant client")
	}
	if err := s.RenameClient(ctx, "alice", "tish+alice"); err == nil {
		t.Fatal("renamed into the tenant's space")
	}
	if err := s.RenameClient(ctx, "tish+u1", "stolen"); err == nil {
		t.Fatal("renamed a tenant client out of its space")
	}
	if err := s.RepPutClient(ctx, "r", "r1", Client{Email: "tish+u1", Enabled: true, VolumeBytes: 1 << 30, ExpiresUnix: s.now().Unix() + 86400, Inbounds: []string{"de-vless"}}, "volume"); err == nil {
		t.Fatal("a reseller wrote a tenant client")
	}
	_ = s.PutClient(ctx, Client{Email: "shop+x", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}})
	if err := s.PutTenant(ctx, Tenant{ID: "shop", Name: "Shop", Enabled: true}); err == nil {
		t.Fatal("a tenant took a name space that is in use")
	}
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u1"}); err == nil {
		t.Fatal("duplicate name")
	}
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u3", ExternalRef: strp("order-1")}); err == nil {
		t.Fatal("duplicate external_ref")
	}
	c, _ := s.TenantClient(ctx, "tish", "u1")
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u4", UUID: c.UUID}); err == nil {
		t.Fatal("duplicate uuid")
	}
}

func TestTenantCapsAndState(t *testing.T) {
	s, ctx := tenantStore(t)
	_ = s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: true, MaxClients: 2, MaxBytes: 8 << 30})
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u2"}); err == nil {
		t.Fatal("unlimited client under a volume cap")
	}
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u2", VolumeBytes: i64(4 << 30)}); err == nil {
		t.Fatal("over the volume cap")
	}
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u2", VolumeBytes: i64(3 << 30)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u3", VolumeBytes: i64(1)}); err == nil {
		t.Fatal("over the client cap")
	}
	_ = s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: false})
	if _, err := s.TenantUpdate(ctx, "tish", "u1", TenantClientIn{MaxIPs: new(int)}); err == nil {
		t.Fatal("a switched-off tenant made a change")
	}
}

func TestTenantRenew(t *testing.T) {
	s, ctx := tenantStore(t)
	now := s.now().Unix()
	c, err := s.TenantRenew(ctx, "tish", "u1", TenantRenewIn{AddBytes: 1 << 30, AddDays: 10})
	if err != nil || c.VolumeBytes != 6<<30 || c.ExpiresUnix != now+40*86400 {
		t.Fatalf("%+v %v", c, err)
	}
	_, _ = s.TenantCreate(ctx, "tish", TenantClientIn{Name: "hold", VolumeBytes: i64(1 << 30), OnHoldDays: i64(30)})
	h, err := s.TenantRenew(ctx, "tish", "hold", TenantRenewIn{AddDays: 5})
	if err != nil || h.ExpiresUnix != 0 || h.DurationSeconds != 35*86400 || h.Status != "on_hold" {
		t.Fatalf("on hold %+v %v", h, err)
	}
}

func TestTenantSubscriptionShowsTheShop(t *testing.T) {
	s, ctx := tenantStore(t)
	_ = s.PutSubSettings(ctx, SubSettings{Title: "Dark Panel", SupportURL: "https://t.me/darkpanel", InfoLines: true})
	if err := s.PutTenantBrand(ctx, "tish", RepBrand{Title: "Tishtar VPN", Telegram: "tishtar_support"}); err != nil {
		t.Fatal(err)
	}
	set, _ := s.brandFor(ctx, "tish+u1")
	if set.Title != "Tishtar VPN" || set.SupportURL != "https://t.me/tishtar_support" {
		t.Fatalf("brand %+v", set)
	}
	c, _ := s.TenantClient(ctx, "tish", "u1")
	acc, _, _, err := s.subscriptionData(ctx, "tish+u1")
	if err != nil || acc.Email != "u1" || len(acc.InfoLines) != 1 || strings.Contains(acc.InfoLines[0], "tish+") {
		t.Fatalf("account %+v %v", acc, err)
	}
	h := httptest.NewServer(AdminHandler(s, "admintok"))
	defer h.Close()
	req, _ := http.NewRequest("GET", h.URL+"/sub/"+c.SubToken, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(b)
	for bad, want := range map[string]bool{"Dark Panel": false, "tish+u1": false, "t.me/darkpanel": false, "Tishtar VPN": true} {
		if strings.Contains(page, bad) != want {
			i := max(strings.Index(page, bad)-200, 0)
			t.Errorf("page %d: %q shown=%v, want %v; near: %s", resp.StatusCode, bad, !want, want, page[i:min(i+400, len(page))])
		}
	}
}

func TestOwnerAccessToTenantClients(t *testing.T) {
	s, ctx := tenantStore(t)
	h := httptest.NewServer(AdminHandler(s, "admintok"))
	defer h.Close()
	do := func(method, path, body string) int {
		req, _ := http.NewRequest(method, h.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admintok")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := do("GET", "/api/v2/admin/clients/tish+u1/links", ""); code != 403 {
		t.Fatalf("summary access: look got %d", code)
	}
	if code := do("GET", "/api/v2/admin/clients/alice/links", ""); code != 200 {
		t.Fatalf("own client %d", code)
	}
	_ = s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: true, OwnerAccess: "read"})
	if code := do("GET", "/api/v2/admin/clients/tish+u1/links", ""); code != 200 {
		t.Fatalf("read access: look got %d", code)
	}
	if code := do("PUT", "/api/v2/admin/clients/tish+u1/note", `{"note":"x"}`); code != 403 {
		t.Fatalf("read access: change got %d", code)
	}
	log, _ := s.AuditLog(ctx, "", "tish+u1", 0, 50)
	looked := false
	for _, e := range log {
		if e.Method == "GET" && strings.Contains(e.Actor, "[tenant:tish]") && e.Status == 200 {
			looked = true
		}
	}
	if !looked {
		t.Fatalf("the owner's look was not logged: %+v", log)
	}
	_ = s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: true, OwnerAccess: "write"})
	if code := do("PUT", "/api/v2/admin/clients/tish+u1/note", `{"note":"x"}`); code != 200 {
		t.Fatalf("write access: change got %d", code)
	}
	if code := do("PUT", "/api/v2/admin/clients/tish+u1", `{"Enabled":true,"VolumeBytes":-1,"Inbounds":["de-vless"]}`); code != 200 {
		t.Fatalf("write access: edit got %d", code)
	}
	if s.TenantOfClient(ctx, "tish+u1") != "tish" {
		t.Fatal("an owner edit moved the client out of its shop")
	}
}

func TestPGAPIOwnerDoesNotSeeTenantClients(t *testing.T) {
	s, ctx := tenantStore(t)
	a, err := s.EnablePGAPI(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Users != 2 {
		t.Fatalf("owner pg users %d, want 2", a.Users)
	}
	h := httptest.NewServer(AdminHandler(s, "admintok"))
	defer h.Close()
	resp, err := http.PostForm(h.URL+"/api/admin/token", map[string][]string{"username": {a.Username}, "password": {a.Password}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	tok := regexp.MustCompile(`"access_token":"([0-9a-f]+)"`).FindStringSubmatch(string(b))
	if tok == nil {
		t.Fatalf("token %s", b)
	}
	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", h.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+tok[1])
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(b)
	}
	if code, body := get("/api/users"); code != 200 || strings.Contains(body, "tish+") || !strings.Contains(body, `"total":2`) {
		t.Fatalf("users %d %s", code, body)
	}
	if code, _ := get("/api/user/tish+u1"); code != 404 {
		t.Fatalf("owner bot read a tenant client: %d", code)
	}
}

// TestClientQueriesAreReviewed is the guard for the tenant separation: every
// SQL statement that reads the clients table was checked for whether a
// connected shop's clients belong in it. A new one (or a removed one) fails
// here until it is checked too: add the scope (ownerWorld, rep_id=?) where
// the owner's or a reseller's world is meant, then update the count.
func TestClientQueriesAreReviewed(t *testing.T) {
	reviewed := map[string]int{
		"hubcore/admin.go": 1, "hubcore/analytics.go": 14, "hubcore/bulkcreate.go": 1, "hubcore/bulkops.go": 6,
		"hubcore/commerce.go": 4, "hubcore/darkold.go": 6, "hubcore/darkold_history.go": 1, "hubcore/expiryrefund.go": 1,
		"hubcore/export.go": 1, "hubcore/foreign.go": 4, "hubcore/inboundtools.go": 1, "hubcore/insights.go": 1,
		"hubcore/manage.go": 2, "hubcore/migrate.go": 3, "hubcore/pgapi.go": 5, "hubcore/plans.go": 2,
		"hubcore/referral.go": 2, "hubcore/rename.go": 4, "hubcore/render.go": 2, "hubcore/renew.go": 1,
		"hubcore/repbrand.go": 1, "hubcore/repctl.go": 4, "hubcore/repdelete.go": 2, "hubcore/repoverview.go": 1,
		"hubcore/reps.go": 3, "hubcore/resets.go": 2, "hubcore/restore.go": 1, "hubcore/sales.go": 1,
		"hubcore/shop.go": 3, "hubcore/store.go": 7, "hubcore/subpage.go": 2, "hubcore/system.go": 1,
		"hubcore/tg.go": 3, "hubcore/toman.go": 1,
	}
	re := regexp.MustCompile(`(FROM|JOIN) clients\b`)
	got := map[string]int{}
	root := ".."
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(filepath.Base(rel), "tenant") { // the tenant layer itself: scoped by design
			return nil
		}
		b, _ := os.ReadFile(p)
		if n := len(re.FindAll(b, -1)); n > 0 {
			got[rel] = n
		}
		return nil
	})
	for f, n := range got {
		if reviewed[f] != n {
			t.Errorf("%s reads the clients table %d times (reviewed: %d): check whether connected shops' clients belong in the new query, then update this list", f, n, reviewed[f])
		}
	}
	for f, n := range reviewed {
		if got[f] == 0 {
			t.Errorf("%s no longer reads the clients table (reviewed: %d): update this list", f, n)
		}
	}
}

// TestTenantSchemaUpgradesAnOlderDatabase opens a database made before the
// tenant layer (no tenant tables, columns, indexes or triggers): the upgrade
// adds them, keeps the data, and a second open changes nothing.
func TestTenantSchemaUpgradesAnOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	for _, q := range []string{
		`DROP TRIGGER clients_tenant_ins`, `DROP TRIGGER clients_tenant_upd`, `DROP TRIGGER clients_tenant_del`, `DROP TRIGGER events_tenant_ins`,
		`DROP INDEX clients_rep`, `DROP INDEX clients_ext_ref`, `DROP INDEX events_tenant`,
		`DROP INDEX clients_email_nocase`, `DROP INDEX clients_uuid`, `DROP INDEX clients_secret`,
		`DROP TABLE tenant_keys`, `DROP TABLE tenants`, `DROP TABLE tenant_tombstones`, `DROP TABLE rev_seq`,
		`ALTER TABLE clients DROP COLUMN external_ref`, `ALTER TABLE clients DROP COLUMN rev`,
		`ALTER TABLE client_usage DROP COLUMN rev`, `ALTER TABLE events DROP COLUMN tenant_id`,
		`INSERT INTO client_usage(email,up,down) VALUES('alice',5,6)`,
		`INSERT INTO events(unix,node_id,email,kind) VALUES(1,'de-1','alice','blocked:volume')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	s.Close()
	for i := 0; i < 2; i++ {
		s, err = Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		ctx := context.Background()
		if c, err := s.ClientByEmail(ctx, "alice"); err != nil || c.Usage.Up != 5 {
			t.Fatalf("data lost: %+v %v", c, err)
		}
		if i == 0 {
			if err := s.PutTenant(ctx, Tenant{ID: "tish", Name: "T", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TenantCreate(ctx, "tish", TenantClientIn{Name: "u1"}); err != nil {
				t.Fatal(err)
			}
		}
		if c, err := s.TenantClient(ctx, "tish", "u1"); err != nil || c.Rev == 0 {
			t.Fatalf("tenant after reopen: %+v %v", c, err)
		}
		s.Close()
	}
}
