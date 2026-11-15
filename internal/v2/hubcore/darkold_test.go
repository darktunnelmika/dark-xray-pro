package hubcore

import (
	"context"
	"database/sql"
	"encoding/hex"
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
	res, err := s.ImportDarkOld(ctx, DarkOldOptions{Inbounds: []string{"main"}})
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
	if again, _ := s.ImportDarkOld(ctx, DarkOldOptions{Inbounds: []string{"main"}, DryRun: true}); again.Clients != 0 {
		t.Fatal("a second import must not duplicate clients")
	}
}
