package hubcore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func makeDB(t *testing.T, stmts ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "src.sqlite")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	return p
}

func TestImport3xui(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if _, err := s.db.Exec(`UPDATE nodes SET public_ip='5.6.7.8' WHERE id='de-1'`); err != nil {
		t.Fatal(err)
	}
	priv, pub, _ := RealityKeys()
	exp := (now.Unix() + 10*86400) * 1000
	src := makeDB(t,
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, user_id INTEGER, up INTEGER, down INTEGER, total INTEGER, remark TEXT, enable INTEGER, expiry_time INTEGER, listen TEXT, port INTEGER, protocol TEXT, settings TEXT, stream_settings TEXT, tag TEXT, sniffing TEXT)`,
		`CREATE TABLE client_traffics (id INTEGER PRIMARY KEY, inbound_id INTEGER, enable INTEGER, email TEXT, up INTEGER, down INTEGER, expiry_time INTEGER, total INTEGER, reset INTEGER, last_online INTEGER)`,
		`CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)`,
		`INSERT INTO settings(key,value) VALUES('subPath','/mysub/'),('subJsonPath','/json/')`,
		`INSERT INTO inbounds(remark,enable,listen,port,protocol,settings,stream_settings,tag,sniffing) VALUES('UK Reality',1,'',443,'vless',
'{"clients":[{"id":"11111111-2222-3333-4444-555555555555","flow":"xtls-rprx-vision","email":"ali","limitIp":2,"totalGB":53687091200,"expiryTime":`+strconv.FormatInt(exp, 10)+`,"enable":true,"tgId":"1906987468","subId":"abcdefgh12345678","comment":"vip"},
{"id":"99999999-2222-3333-4444-555555555555","email":"sara","limitIp":0,"totalGB":0,"expiryTime":-2592000000,"enable":false,"subId":"zzzzzzzz12345678"}],"decryption":"none","fallbacks":[]}',
'{"network":"tcp","security":"reality","realitySettings":{"show":false,"dest":"www.speedtest.net:443","serverNames":["www.speedtest.net"],"privateKey":"`+priv+`","shortIds":["ab12"],"settings":{"publicKey":"`+pub+`","fingerprint":"firefox","spiderX":"/"}}}',
'inbound-443','{"enabled":true,"destOverride":["http","tls"]}')`,
		`INSERT INTO inbounds(remark,enable,listen,port,protocol,settings,stream_settings,tag,sniffing) VALUES('UK Trojan',1,'',8443,'trojan',
'{"clients":[{"password":"trojan-pass-1","email":"ali-tr","limitIp":2,"totalGB":0,"expiryTime":0,"enable":true,"subId":"abcdefgh12345678"}],"fallbacks":[]}',
'{"network":"ws","security":"none","wsSettings":{"path":"/tr"}}','inbound-8443','{}')`,
		`INSERT INTO inbounds(remark,enable,port,protocol,settings,tag) VALUES('socks',1,1080,'socks','{}','inbound-1080')`,
		`INSERT INTO client_traffics(email,up,down,last_online) VALUES('ali',100,900,0),('ali-tr',10,90,0),('sara',0,0,0)`,
	)
	dry, err := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1", DryRun: true})
	if err != nil || dry.Kind != "3x-ui" || dry.Clients != 2 || len(dry.Inbounds) != 3 {
		t.Fatalf("%+v %v", dry, err)
	}
	if all, _ := s.Clients(ctx); len(all) != 2 {
		t.Fatal("a preview writes nothing")
	}
	got, err := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1"})
	if err != nil || got.Clients != 2 || got.Links != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	ali, err := s.ClientByEmail(ctx, "ali")
	if err != nil {
		t.Fatal(err)
	}
	if ali.UUID != "11111111-2222-3333-4444-555555555555" || ali.MaxIPs != 2 || ali.VolumeBytes != 50<<30 || ali.ExpiresUnix != exp/1000 ||
		ali.Note != "vip" || len(ali.Inbounds) != 2 || ali.Usage.Up+ali.Usage.Down != 1100 || ali.SubToken != "abcdefgh12345678" {
		t.Fatalf("%+v", ali)
	}
	var secret string
	_ = s.db.QueryRow(`SELECT secret FROM clients WHERE email='ali'`).Scan(&secret)
	if secret != "trojan-pass-1" {
		t.Fatal("the trojan password must be kept", secret)
	}
	sara, _ := s.ClientByEmail(ctx, "sara")
	if sara.Enabled || sara.DurationSeconds != 30*86400 || sara.VolumeBytes != -1 {
		t.Fatalf("%+v", sara.Client)
	}
	raw, err := s.InboundRaw(ctx, got.Inbounds[0])
	if err != nil || strings.Contains(string(raw.Template), `"settings":{"publicKey"`) || !strings.Contains(string(raw.Meta), pub) || !strings.Contains(string(raw.Template), "xtls-rprx-vision") {
		t.Fatalf("%s %s", raw.Template, raw.Meta)
	}
	links, _ := s.ClientLinks(ctx, "ali")
	found := false
	for _, l := range links {
		if strings.Contains(l.Link, "@5.6.7.8:443") && strings.Contains(l.Link, "fp=firefox") && strings.Contains(l.Link, "sid=ab12") {
			found = true
		}
	}
	if !found {
		t.Fatal("links must use the node address and the 3x-ui client settings", links)
	}
	if _, err := s.RenderNode(ctx, "de-1"); err != nil {
		t.Fatal(err)
	}
	// The old custom subscription paths still answer.
	h := httptest.NewServer(SubHandler(s))
	defer h.Close()
	for _, p := range []string{"/mysub/abcdefgh12345678", "/sub/abcdefgh12345678", "/json/abcdefgh12345678"} {
		resp, err := http.Get(h.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatal(p, err, resp.StatusCode)
		}
		resp.Body.Close()
	}
	if again, err := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1", DryRun: true}); err != nil || again.Clients != 0 {
		t.Fatalf("existing names are skipped: %+v %v", again, err)
	}
}

func marzbanTok(user, secret string) string {
	body := strings.TrimRight(base64.URLEncoding.EncodeToString([]byte(user+",1700000000")), "=")
	sum := sha256.Sum256([]byte(body + secret))
	return body + base64.URLEncoding.EncodeToString(sum[:])[:10]
}

func TestImportMarzbanAndPasarGuard(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	exp := now.Unix() + 5*86400
	mz := makeDB(t,
		`CREATE TABLE admins (id INTEGER PRIMARY KEY, username TEXT, hashed_password TEXT, is_sudo INTEGER)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, status TEXT, used_traffic INTEGER, data_limit INTEGER, expire INTEGER, note TEXT, admin_id INTEGER, data_limit_reset_strategy TEXT, on_hold_expire_duration INTEGER, created_at TEXT)`,
		`CREATE TABLE proxies (id INTEGER PRIMARY KEY, user_id INTEGER, type TEXT, settings TEXT)`,
		`CREATE TABLE jwt (id INTEGER PRIMARY KEY, secret_key TEXT)`,
		`INSERT INTO jwt(secret_key) VALUES('s3cr3t')`,
		`INSERT INTO admins VALUES(1,'boss','$2b$12$abcdefghijklmnopqrstuuJ8zD3wq6m6o6C4o0p2m0W7j1Zk2y1Ue',1),(2,'seller1','$2b$12$abcdefghijklmnopqrstuuJ8zD3wq6m6o6C4o0p2m0W7j1Zk2y1Ue',0)`,
		`INSERT INTO users VALUES(1,'reza','active',2000,10737418240,`+strconv.FormatInt(exp, 10)+`,'n1',2,'month',0,'2025-01-02 03:04:05'),
(2,'mina','on_hold',0,0,NULL,'',1,'no_reset',604800,'2025-01-02 03:04:05'),(3,'old','disabled',0,0,NULL,'',1,'no_reset',0,'2025-01-02 03:04:05')`,
		`INSERT INTO proxies(user_id,type,settings) VALUES(1,'VLESS','{"id":"aaaaaaaa-2222-3333-4444-555555555555","flow":""}'),(1,'Trojan','{"password":"tp-1"}'),(2,'VMess','{"id":"bbbbbbbb-2222-3333-4444-555555555555"}')`,
	)
	got, err := s.ImportForeign(ctx, mz, ForeignOptions{Inbounds: []string{"de-vless"}})
	if err != nil || got.Kind != "marzban" || got.Clients != 3 || got.Reps != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	reza, _ := s.ClientByEmail(ctx, "reza")
	if reza.UUID != "aaaaaaaa-2222-3333-4444-555555555555" || reza.ExpiresUnix != exp || reza.VolumeBytes != 10<<30 || reza.RepID != "mz-seller1" || reza.Usage.Down != 2000 {
		t.Fatalf("%+v", reza)
	}
	mina, _ := s.ClientByEmail(ctx, "mina")
	if mina.DurationSeconds != 604800 || mina.ExpiresUnix != 0 || !mina.Enabled {
		t.Fatalf("on hold: %+v", mina.Client)
	}
	if old, _ := s.ClientByEmail(ctx, "old"); old.Enabled {
		t.Fatal("disabled stays disabled")
	}
	var role string
	_ = s.db.QueryRow(`SELECT role FROM admins WHERE username='boss'`).Scan(&role)
	if role != "admin" {
		t.Fatal("a sudo admin becomes a staff admin, never an owner", role)
	}
	h := httptest.NewServer(SubHandler(s))
	defer h.Close()
	resp, err := http.Get(h.URL + "/sub/" + marzbanTok("reza", "s3cr3t"))
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("old Marzban link", err, resp.StatusCode)
	}
	resp.Body.Close()
	if resp, _ := http.Get(h.URL + "/sub/" + marzbanTok("reza", "wrong")); resp.StatusCode != 404 {
		t.Fatal("a forged link must not answer")
	}

	pg := makeDB(t,
		`CREATE TABLE admins (id INTEGER PRIMARY KEY, username TEXT, hashed_password TEXT, is_sudo INTEGER)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, status TEXT, used_traffic INTEGER, data_limit INTEGER, expire TEXT, note TEXT, admin_id INTEGER, data_limit_reset_strategy TEXT, proxy_settings TEXT, created_at TEXT)`,
		`CREATE TABLE core_configs (id INTEGER PRIMARY KEY, name TEXT, config TEXT)`,
		`INSERT INTO users VALUES(1,'pguser','active',0,0,'2030-01-01 00:00:00','',0,'no_reset','{"vless":{"id":"cccccccc-2222-3333-4444-555555555555","flow":"xtls-rprx-vision"},"trojan":{"password":"pgp"}}','2025-01-02T03:04:05')`,
		`INSERT INTO core_configs(name,config) VALUES('main','{"inbounds":[{"tag":"VLESS TCP","protocol":"vless","port":9443,"settings":{"clients":[],"decryption":"none"},"streamSettings":{"network":"tcp","security":"none"}},{"tag":"api","protocol":"dokodemo-door","port":62789}]}')`,
	)
	if _, err := s.db.Exec(`UPDATE nodes SET public_ip='5.6.7.8' WHERE id='de-1'`); err != nil {
		t.Fatal(err)
	}
	got, err = s.ImportForeign(ctx, pg, ForeignOptions{Node: "de-1"})
	if err != nil || got.Kind != "pasarguard" || got.Clients != 1 || len(got.Inbounds) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	pu, _ := s.ClientByEmail(ctx, "pguser")
	if pu.UUID != "cccccccc-2222-3333-4444-555555555555" || pu.ExpiresUnix != 1893456000 || len(pu.Inbounds) != 1 {
		t.Fatalf("%+v", pu)
	}
}
