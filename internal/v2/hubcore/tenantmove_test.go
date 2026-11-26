package hubcore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestImport3xuiIntoTenant(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_, _ = s.db.Exec(`UPDATE nodes SET public_ip='5.6.7.8' WHERE id='de-1'`)
	// The owner already has a client called "ali": the shop's "ali" is a
	// different customer and must not clash with it.
	if err := s.PutClient(ctx, Client{Email: "ali", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTenant(ctx, Tenant{ID: "tish", Name: "Tishtar", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	exp := (now.Unix() + 10*86400) * 1000
	src := makeDB(t,
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, user_id INTEGER, up INTEGER, down INTEGER, total INTEGER, remark TEXT, enable INTEGER, expiry_time INTEGER, listen TEXT, port INTEGER, protocol TEXT, settings TEXT, stream_settings TEXT, tag TEXT, sniffing TEXT)`,
		`CREATE TABLE client_traffics (id INTEGER PRIMARY KEY, inbound_id INTEGER, enable INTEGER, email TEXT, up INTEGER, down INTEGER, expiry_time INTEGER, total INTEGER, reset INTEGER, last_online INTEGER)`,
		`CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)`,
		`INSERT INTO inbounds(remark,enable,listen,port,protocol,settings,stream_settings,tag,sniffing) VALUES('UK',1,'',2053,'vless',
'{"clients":[{"id":"11111111-2222-3333-4444-555555555555","email":"ali","limitIp":2,"totalGB":53687091200,"expiryTime":`+strconv.FormatInt(exp, 10)+`,"enable":true,"tgId":"111222333","subId":"abcdefgh12345678"},
{"id":"22222222-2222-3333-4444-555555555555","email":"a+b","limitIp":0,"totalGB":0,"expiryTime":0,"enable":true,"subId":"bbbbbbbb12345678"}],"decryption":"none"}',
'{"network":"tcp","security":"none"}','inbound-2053','{}')`,
		`INSERT INTO client_traffics(email,up,down,last_online) VALUES('ali',100,900,0)`,
	)
	dry, err := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1", Tenant: "tish", DryRun: true})
	if err != nil || dry.Clients != 2 {
		t.Fatalf("dry %+v %v", dry, err)
	}
	if sum, _ := s.Tenants(ctx); sum[0].Clients != 0 {
		t.Fatal("a preview wrote clients")
	}
	got, err := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1", Tenant: "tish"})
	if err != nil || got.Clients != 2 || got.Links != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	c, err := s.TenantClient(ctx, "tish", "ali")
	if err != nil || c.UUID != "11111111-2222-3333-4444-555555555555" || c.SubToken != "abcdefgh12345678" || c.VolumeBytes != 50<<30 ||
		c.ExpiresUnix != exp/1000 || c.UsedBytes != 1000 || c.MaxIPs != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := s.TenantClient(ctx, "tish", "a-b"); err != nil {
		t.Fatalf("a name with + gets a shop-safe name: %v %+v", err, got.Notes)
	}
	owner, _ := s.ClientByEmail(ctx, "ali")
	if owner.UUID == c.UUID {
		t.Fatal("the owner's ali was overwritten")
	}
	var links int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tg_links WHERE email LIKE 'tish+%'`).Scan(&links)
	if links != 0 {
		t.Fatal("a shop's customer was linked to the panel bot")
	}
	// The old 3x-ui link keeps answering.
	h := httptest.NewServer(SubHandler(s))
	defer h.Close()
	resp, err := http.Get(h.URL + "/sub/abcdefgh12345678")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("old link %v %v", err, resp)
	}
	resp.Body.Close()
	if again, _ := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1", Tenant: "tish", DryRun: true}); again.Clients != 0 {
		t.Fatalf("a second import would duplicate: %+v", again)
	}
	if _, err := s.ImportForeign(ctx, src, ForeignOptions{Node: "de-1", Tenant: "nope", DryRun: true}); err == nil {
		t.Fatal("unknown shop")
	}
}

func TestMoveClientsBetweenWorlds(t *testing.T) {
	s, ctx := tenantStore(t)
	// Some history under the owner's names.
	_, _ = s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1, FromUnix: 1, ToUnix: 2, Traffic: []wire.UserTraffic{{Email: "alice", Up: 10, Down: 20}}})
	_ = s.SetClientNote(ctx, "alice", "vip")
	_ = s.TgLink(ctx, 42, "alice")
	before, _ := s.ClientByEmail(ctx, "alice")

	dry, err := s.MoveClients(ctx, MoveRequest{From: MoveSide{Kind: "owner"}, To: MoveSide{Kind: "tenant", ID: "tish"}, Names: []string{"alice", "nobody"}, DryRun: true})
	if err != nil || dry.Moved != 1 || len(dry.Skipped) != 1 {
		t.Fatalf("dry %+v %v", dry, err)
	}
	if s.TenantOfClient(ctx, "alice") != "" {
		t.Fatal("a dry run moved")
	}
	rep, err := s.MoveClients(ctx, MoveRequest{From: MoveSide{Kind: "owner"}, To: MoveSide{Kind: "tenant", ID: "tish"}, Names: []string{"alice"}})
	if err != nil || rep.Moved != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	c, err := s.TenantClient(ctx, "tish", "alice")
	if err != nil || c.UUID != before.UUID || c.SubToken != before.SubToken || c.UsedBytes != 30 || c.Note != "vip" {
		t.Fatalf("moved %+v %v", c, err)
	}
	if _, err := s.ClientByEmail(ctx, "alice"); err == nil {
		t.Fatal("still in the owner's world")
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tg_links WHERE email IN ('alice','tish+alice')`).Scan(&n)
	if n != 0 {
		t.Fatal("the bot link went along into the shop")
	}
	// Late usage under the old name lands on the moved client.
	_, _ = s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 2, FromUnix: 2, ToUnix: 3, Traffic: []wire.UserTraffic{{Email: "alice", Up: 1, Down: 1}}})
	if c, _ := s.TenantClient(ctx, "tish", "alice"); c.UsedBytes != 32 {
		t.Fatalf("late usage %d", c.UsedBytes)
	}
	// The single transfer refuses a shop's client.
	if err := s.TransferClient(ctx, "tish+alice", ""); err == nil {
		t.Fatal("TransferClient took a client out of a shop")
	}
	// All of the shop's clients to a reseller, in one call.
	rep, err = s.MoveClients(ctx, MoveRequest{From: MoveSide{Kind: "tenant", ID: "tish"}, To: MoveSide{Kind: "rep", ID: "r1"}})
	if err != nil || rep.Moved != 2 {
		t.Fatalf("%+v %v", rep, err)
	}
	mine, _ := s.scopedClients(ctx, Principal{Role: "rep", RepID: "r1"})
	names := []string{}
	for _, c := range mine {
		names = append(names, c.Email)
	}
	if strings.Join(names, ",") != "alice,r1c,u1" && len(names) != 3 {
		t.Fatalf("reseller has %v", names)
	}
	var ref string
	_ = s.db.QueryRow(`SELECT external_ref FROM clients WHERE email='u1'`).Scan(&ref)
	if ref != "" {
		t.Fatal("the shop's reference left with the client")
	}
	if sum, _ := s.Tenants(ctx); sum[0].Clients != 0 {
		t.Fatalf("shop still has %d", sum[0].Clients)
	}
	// The shop's feed shows them as gone.
	ch, _, _, _ := s.TenantChanges(ctx, "tish", "", 100)
	gone := 0
	for _, c := range ch {
		if c.Deleted {
			gone++
		}
	}
	if gone < 2 {
		t.Fatalf("feed %+v", ch)
	}
	// A name already taken on the other side is skipped, not overwritten.
	_, _ = s.TenantCreate(ctx, "tish", TenantClientIn{Name: "bob"})
	rep, err = s.MoveClients(ctx, MoveRequest{From: MoveSide{Kind: "owner"}, To: MoveSide{Kind: "tenant", ID: "tish"}, Names: []string{"bob"}})
	if err != nil || rep.Moved != 0 || len(rep.Skipped) != 1 {
		t.Fatalf("clash %+v %v", rep, err)
	}
	if _, err := s.MoveClients(ctx, MoveRequest{From: MoveSide{Kind: "owner"}, To: MoveSide{Kind: "owner"}}); err == nil {
		t.Fatal("a move to the same place")
	}
}

func TestMoveIsOwnerOnly(t *testing.T) {
	s, _ := tenantStore(t)
	h := httptest.NewServer(AdminHandler(s, "admintok"))
	defer h.Close()
	req, _ := http.NewRequest("POST", h.URL+"/api/v2/admin/clients/move", strings.NewReader(`{"from":{"kind":"owner"},"to":{"kind":"tenant","id":"tish"},"names":["bob"]}`))
	req.Header.Set("Authorization", "Bearer admintok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("owner move %v %v", err, resp.StatusCode)
	}
	resp.Body.Close()
	if !isOwnerOnly("/api/v2/admin/clients/move") || !isOwnerOnly("/api/v2/admin/tenants/tish/keys") {
		t.Fatal("staff could manage connected shops")
	}
}
