package hubcore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
	"golang.org/x/crypto/scrypt"
)

func darkOldFixture(t *testing.T, path string) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	salt := []byte("0123456789abcdef")
	h, _ := scrypt.Key([]byte("rep-pass-123"), salt, 16384, 8, 1, 32)
	hash := "scrypt$16384$8$1$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(h)
	for _, q := range []string{
		`CREATE TABLE owners(id TEXT PRIMARY KEY, quota_bytes INTEGER DEFAULT 0, volume_credit_bytes INTEGER DEFAULT 0, unlimited_credit INTEGER DEFAULT 0, max_clients INTEGER DEFAULT 0, manual INTEGER DEFAULT 0, account_disabled INTEGER DEFAULT 0, period INTEGER DEFAULT 0, credit INTEGER DEFAULT 0, unlimited_spent INTEGER DEFAULT 0)`,
		`CREATE TABLE owner_profiles(id TEXT PRIMARY KEY,name TEXT NOT NULL,allowed TEXT DEFAULT '[]',prefix TEXT DEFAULT '',max_client_ips INTEGER DEFAULT 0,max_client_hwid INTEGER DEFAULT 0)`,
		`CREATE TABLE api_admins(id TEXT PRIMARY KEY, role TEXT NOT NULL, password_hash TEXT NOT NULL, permissions TEXT NOT NULL, disabled INTEGER DEFAULT 0)`,
		`CREATE TABLE clients(id TEXT PRIMARY KEY, owner TEXT NOT NULL, limit_ip INTEGER DEFAULT 0, quota_bytes INTEGER DEFAULT 0, used_bytes INTEGER DEFAULT 0, manual INTEGER DEFAULT 0, expires_at INTEGER DEFAULT 0, global_ip_block INTEGER DEFAULT 0, global_device_block INTEGER DEFAULT 0)`,
		`CREATE TABLE managed_clients(email TEXT PRIMARY KEY,desired TEXT,inbounds TEXT,public_token TEXT UNIQUE NOT NULL,op TEXT,op_id TEXT,state TEXT,error TEXT,last_up INTEGER,last_down INTEGER,initialized INTEGER,seq INTEGER,expected_enable INTEGER,external_disabled INTEGER DEFAULT 0,retry_at REAL,attempts INTEGER,created_at REAL NOT NULL,updated_at REAL NOT NULL)`,
		`CREATE TABLE core_clients(email TEXT PRIMARY KEY,body TEXT NOT NULL,inbounds TEXT NOT NULL,up INTEGER DEFAULT 0,down INTEGER DEFAULT 0)`,
		`CREATE TABLE unlimited_entitlements(client_id TEXT PRIMARY KEY, owner TEXT, state_json TEXT, revision INTEGER, updated_at REAL)`,
		`CREATE TABLE restore_subscriptions(id TEXT PRIMARY KEY, public_token TEXT, legacy_url TEXT, legacy_host TEXT, legacy_path TEXT, legacy_query TEXT DEFAULT '', core_email TEXT, inbound_ids TEXT, node_ids TEXT, legacy_upload INTEGER DEFAULT 0, legacy_download INTEGER DEFAULT 0, legacy_total INTEGER DEFAULT 0, legacy_expire INTEGER DEFAULT 0, scan_status TEXT, scan_error TEXT, enabled INTEGER DEFAULT 1, first_seen REAL, last_seen REAL, created_at REAL, updated_at REAL, group_id TEXT, node_mode TEXT, include_local INTEGER, promoted_owner TEXT, promoted_at REAL DEFAULT 0, deleted_at REAL DEFAULT 0, deleted_reason TEXT)`,
		`CREATE TABLE restore_usage(restore_id TEXT, scope TEXT, up INTEGER DEFAULT 0, down INTEGER DEFAULT 0, raw_up INTEGER, raw_down INTEGER, updated_at REAL, activity_at REAL)`,
		`CREATE TABLE commerce_orders(id TEXT PRIMARY KEY, client_id TEXT, duration_days INTEGER, status TEXT)`,
		`CREATE TABLE export_bot_tokens(owner TEXT PRIMARY KEY, token TEXT NOT NULL, enabled INTEGER, admin_telegram_id INTEGER, bot_username TEXT)`,
		`CREATE TABLE export_meta(k TEXT PRIMARY KEY, v TEXT)`,
		`INSERT INTO export_meta VALUES('public_origin','https://steven.example.info:2087')`,
		`INSERT INTO owners(id,volume_credit_bytes,unlimited_credit) VALUES('boss',0,0),('ali',107374182400,5)`,
		`INSERT INTO owner_profiles(id,name) VALUES('ali','Ali Shop')`,
		`INSERT INTO api_admins VALUES('boss','owner','` + hash + `','{}',0),('ali','reseller','` + hash + `','{}',0)`,
		`INSERT INTO clients(id,owner,limit_ip,quota_bytes,expires_at) VALUES('u1','ali',2,53687091200,1900000000),('u2','boss',1,0,0),('u3','ali',1,1073741824,0)`,
		`INSERT INTO managed_clients(email,public_token,state,created_at,updated_at,external_disabled) VALUES('u1','-11QabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLM','applied',1700000000,1700000000,0),('u2','tok2-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL','applied',1700000001,1700000001,1),('u3','tok3-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL','applied',1700000002,1700000002,0)`,
		`INSERT INTO core_clients VALUES('u1','{"id":"11111111-2222-3333-4444-555555555555","password":"pw1","enable":true,"group":"vip","comment":"Ali friend","tgId":"12345","limitHwid":2}','[]',100,200),('u2','{"id":"22222222-2222-3333-4444-555555555555","enable":true}','[]',0,0),('u3','{"id":"33333333-2222-3333-4444-555555555555","enable":true}','[]',0,0),('r1','{"id":"44444444-2222-3333-4444-555555555555","enable":true}','[]',0,0)`,
		`INSERT INTO unlimited_entitlements VALUES('u2','boss','{"segments":[[1,2,1]],"units":1}',1,0)`,
		`INSERT INTO commerce_orders VALUES('o1','u3',30,'provisioned_waiting_activation')`,
		`INSERT INTO restore_subscriptions(id,public_token,legacy_url,legacy_host,legacy_path,core_email,legacy_upload,legacy_download,legacy_total,legacy_expire,enabled,created_at) VALUES('rs1','x','https://hex.example.info:2096/sub/00lb0xrdtbgqxvk5','hex.example.info','/sub/00lb0xrdtbgqxvk5','r1',10,20,53687091200,1794601800000,1,1700000000)`,
		`INSERT INTO restore_usage(restore_id,scope,up,down) VALUES('rs1','local',5,5)`,
		`CREATE TABLE commerce_gateways(id TEXT, owner TEXT, label TEXT, kind TEXT, enabled INTEGER, instructions TEXT, plugin TEXT, secret_enc TEXT, updated_at REAL, card_number TEXT, card_holder TEXT, bank_name TEXT)`,
		`INSERT INTO commerce_gateways VALUES('g1','ali','card','manual',1,'','','',1,'6219-8610-1234-5678','Ali R','Saman')`,
		`INSERT INTO export_bot_tokens VALUES('ali','123456789:AAbbccddeeffgghhiijjkkllmmnnooppqq',1,777,'alibot'),('boss','987654321:AAbbccddeeffgghhiijjkkllmmnnooppqq',1,1,'bossbot')`,
		`CREATE TABLE customer_wallets(owner TEXT, telegram_id INTEGER, currency TEXT, balance_minor INTEGER, updated_at REAL)`,
		`INSERT INTO customer_wallets VALUES('ali',12345,'IRT',250000,1),('ali',555,'IRT',0,1)`,
		`CREATE TABLE commerce_products(id TEXT, owner TEXT, name TEXT, description TEXT, kind TEXT, active INTEGER, visible INTEGER, created_at REAL, updated_at REAL, category TEXT, sale_limit_per_user INTEGER DEFAULT 0, renewal_enabled INTEGER DEFAULT 1)`,
		`INSERT INTO commerce_products VALUES('p1','ali','Ali Plans','','volume',1,1,1,1,'VIP',2,0)`,
		`CREATE TABLE commerce_prices(id TEXT, owner TEXT, product_id TEXT, label TEXT, price_minor INTEGER, currency TEXT, duration_days INTEGER, volume_bytes INTEGER, unlimited_units INTEGER, device_limit INTEGER, inbound_ids TEXT, active INTEGER, created_at REAL, updated_at REAL, ip_limit INTEGER, hwid_limit INTEGER, activation_mode TEXT)`,
		`INSERT INTO commerce_prices VALUES('pr1','ali','p1','10 GB / 30D',38000,'IRT',30,10737418240,0,1,'[]',1,1,1,2,0,'first_connection')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q[:40], err)
		}
	}
}

func TestImportDarkOld(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "n1", "n1"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePreset(ctx, PresetRequest{Tag: "main", NodeID: "n1", Preset: "vless-reality-tcp", Port: 443, SNI: "www.speedtest.net",
		Endpoints: []sub.Endpoint{{Name: "Direct", Address: "1.2.3.4", Port: 443}}}); err != nil {
		t.Fatal(err)
	}
	darkOldFixture(t, s.darkOldPath())
	st := s.DarkOldStatus(ctx)
	if !st.Uploaded || st.Counts["clients"] != 3 || st.Counts["resellers"] != 1 || st.Origin == "" {
		t.Fatalf("%+v", st)
	}
	pre, err := s.ImportDarkOld(ctx, DarkOldOptions{Inbounds: []string{"main"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if pre.Clients != 3 || pre.Restore != 1 || pre.Reps != 1 || pre.Logins != 2 || len(pre.Domains) != 2 {
		t.Fatalf("%+v", pre)
	}
	if list, _ := s.Clients(ctx); len(list) != 0 {
		t.Fatal("a preview must not write")
	}
	var applied string
	SetTelegramControl(TelegramControl{Apply: func(_ context.Context, tok string, owners []int64) (string, error) {
		applied = tok
		return "bossbot", nil
	}})
	defer SetTelegramControl(TelegramControl{})
	res, err := s.ImportDarkOld(ctx, DarkOldOptions{Inbounds: []string{"main"}, OwnerBot: true})
	if err != nil || res.Clients != 3 || res.Telegram != 1 || res.Links != 4 {
		t.Fatalf("%+v %v", res, err)
	}
	if e, err := s.EmailBySubToken(ctx, "-11QabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLM"); err != nil || e != "u1" {
		t.Fatal("the old subscription link must keep working", e, err)
	}
	if e, err := s.EmailBySubToken(ctx, "00lb0xrdtbgqxvk5"); err != nil || e != "r1" {
		t.Fatal("the Restore link must keep working", e, err)
	}
	byEmail := map[string]ClientView{}
	list, _ := s.Clients(ctx)
	for _, c := range list {
		byEmail[c.Email] = c
	}
	u1, u2, u3, r1 := byEmail["u1"], byEmail["u2"], byEmail["u3"], byEmail["r1"]
	if u1.RepID != "ali" || u1.UUID != "11111111-2222-3333-4444-555555555555" || u1.MaxIPs != 2 || u1.ExpiresUnix != 1900000000 || u1.HWIDLimit != 2 || u1.Note != "Ali friend" {
		t.Fatalf("u1 %+v", u1)
	}
	if u2.RepID != "" || u2.VolumeBytes != -1 || u2.Enabled || u2.Plan != "unlimited" {
		t.Fatalf("u2 %+v", u2)
	}
	if u3.DurationSeconds != 30*86400 || u3.ExpiresUnix != 0 {
		t.Fatalf("u3 must start on first connection: %+v", u3)
	}
	if r1.ExpiresUnix != 1794601800 || r1.VolumeBytes != 53687091200 || r1.Usage.Up+r1.Usage.Down != 40 {
		t.Fatalf("r1 %+v", r1)
	}
	if _, err := s.Login(ctx, "ali", "rep-pass-123"); err != nil {
		t.Fatal("the reseller must sign in with the old password:", err)
	}
	if _, err := s.Login(ctx, "ali", "wrong"); err == nil {
		t.Fatal("a wrong password must fail")
	}
	if !strings.HasPrefix(applied, "987654321:") || res.MainBot != "@bossbot" {
		t.Fatal("the owner's old bot must become the main bot", applied, res.MainBot)
	}
	if res.Cards != 1 || res.Bots != 1 || res.Wallets != 1 || res.Products != 1 {
		t.Fatalf("shop: %+v", res)
	}
	if c := s.CardFor(ctx, "ali"); c.Number != "6219861012345678" || c.Bank != "Saman" {
		t.Fatalf("card %+v", c)
	}
	if b := s.RepBotOf(ctx, "ali"); !b.HasToken || !b.Enabled {
		t.Fatalf("bot %+v", b)
	}
	if bal := s.TomanBalance(ctx, "ali", 12345); bal != 250000 {
		t.Fatal("wallet", bal)
	}
	prods, _ := s.Products(ctx, Principal{Role: "owner"}, false)
	if len(prods) != 1 || prods[0].Seller != "ali" || prods[0].GB != 10 || prods[0].Price != 38000 || !prods[0].FirstUse || prods[0].Status != "published" ||
		prods[0].Category != "VIP" || !prods[0].NoRenew || prods[0].PerBuyer != 2 {
		t.Fatalf("products %+v", prods)
	}
	if again, _ := s.ImportDarkOld(ctx, DarkOldOptions{Inbounds: []string{"main"}, DryRun: true}); again.Clients != 0 {
		t.Fatal("a second import must not duplicate clients")
	}
}

// The routes main.go adds to the owner API must not clash with its own.
func TestDarkOldRoutesMount(t *testing.T) {
	s, _ := newStore(t)
	h := AdminHandlerWith(s, "tok", "https://hub.example", http.NotFoundHandler(), func(m *http.ServeMux) {
		mig := DarkOldHandler(s)
		m.Handle("GET /migrate/", mig)
		m.Handle("POST /migrate/", mig)
		m.Handle("GET /install/", InstallHandler(s, InstallConfig{}))
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/migrate/nope/export.py", nil))
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	cmd := s.DarkOldStart("https://hub.example")
	tok := strings.Split(strings.Split(cmd, "/migrate/")[1], "/")[0]
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/migrate/"+tok+"/export.py", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "export_bot_tokens") {
		t.Fatal(rec.Code)
	}
}
