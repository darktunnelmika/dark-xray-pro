package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// oldFixture builds a small database in the old panel's format.
func oldFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "business.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	salt := []byte("0123456789abcdef")
	hash := argon2.IDKey([]byte("old-owner-password"), salt, 3, 32*1024, 2, 32)
	stmts := []string{
		`CREATE TABLE representatives (id TEXT PRIMARY KEY, display_name TEXT, kind TEXT, enabled INTEGER, volume_credit_bytes INTEGER, unlimited_credit_units INTEGER, created_unix INTEGER)`,
		`CREATE TABLE clients (id TEXT PRIMARY KEY, representative_id TEXT, display_name TEXT, uuid TEXT, plan_kind TEXT, ip_count INTEGER, volume_bytes INTEGER, duration_days INTEGER, charged_volume_bytes INTEGER, charged_unlimited_units INTEGER, created_unix INTEGER, expires_unix INTEGER, deployment_state TEXT, archived INTEGER)`,
		`CREATE TABLE auth_users (id TEXT PRIMARY KEY, username TEXT, representative_id TEXT, role TEXT, password_salt BLOB, password_hash BLOB, disabled INTEGER, created_unix INTEGER)`,
		`CREATE TABLE pro_usage_accounts (client_id TEXT PRIMARY KEY, uplink INTEGER, downlink INTEGER, baseline_complete INTEGER, holds INTEGER)`,
		`CREATE TABLE client_groups (id TEXT PRIMARY KEY, representative_id TEXT, name TEXT, color TEXT, created_unix INTEGER, updated_unix INTEGER)`,
		`CREATE TABLE client_group_members (client_id TEXT, group_id TEXT, assigned_unix INTEGER)`,
		`CREATE TABLE representative_controls (representative_id TEXT, revision INTEGER, login_suspended INTEGER)`,
		`CREATE TABLE representative_sellers_v2 (id TEXT, representative_id TEXT, name TEXT, telegram_user_id INTEGER, enabled INTEGER, reserved_bytes INTEGER, reserved_units INTEGER, created_unix INTEGER)`,
		`CREATE TABLE inbound_execution_subscription_credentials (id TEXT, client_id TEXT, generation INTEGER, token_sha TEXT, token_enc BLOB, created INTEGER)`,
		`CREATE TABLE inbound_execution_subscription_heads (client_id TEXT, generation INTEGER, state TEXT, credential_id TEXT, event_id TEXT)`,
		`INSERT INTO representatives VALUES ('owner','Primary owner','owner',1,0,0,1),('Ali Shop','Ali shop','representative',1,5368709120,4,1),('frozen','Frozen','representative',1,0,0,1)`,
		`INSERT INTO representative_controls VALUES ('frozen',1,1)`,
		`INSERT INTO representative_sellers_v2 VALUES ('sub1','Ali Shop','Sub seller',0,1,1073741824,1,1)`,
		`INSERT INTO clients VALUES
		 ('reza phone','Ali Shop','Reza','8B1A4C2D-1111-4222-8333-944455556666','volume',2,32212254720,30,0,0,1000,1900000000,'active',0),
		 ('mina','owner','mina','not-a-uuid','unlimited',1,0,30,0,0,1000,0,'pending_node_apply',0),
		 ('old-gone','owner','gone','','volume',1,1,30,0,0,1000,0,'disabled',1)`,
		`INSERT INTO pro_usage_accounts VALUES ('reza phone',100,900,1,0)`,
		`INSERT INTO client_groups VALUES ('g1','Ali Shop','VIP','',1,1)`,
		`INSERT INTO client_group_members VALUES ('reza phone','g1',1),('old-gone','g1',1)`,
		`INSERT INTO inbound_execution_subscription_credentials VALUES ('c1','reza phone',1,'` + legacyTokenSHA("managed", strings.Repeat("A", 43)) + `',x'00',1)`,
		`INSERT INTO inbound_execution_subscription_heads VALUES ('reza phone',1,'active','c1','e1')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO auth_users VALUES ('owner','darkpro','owner','owner',?,?,0,1),('r1','ali','Ali Shop','representative',?,?,0,1),('r2','gone','frozen','representative',?,?,1,1)`,
		salt, hash, salt, hash, salt, hash); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrateFromOld(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	old := oldFixture(t)
	if _, err := s.MigrateFromOld(ctx, old, MigrateOptions{Inbounds: []string{"nope"}}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown inbound accepted")
	}
	// Preview changes nothing.
	pre, err := s.MigrateFromOld(ctx, old, MigrateOptions{Inbounds: []string{"de-vless"}, DryRun: true})
	if err != nil || pre.Clients != 2 || pre.Reps != 2 || pre.Sellers != 1 || pre.Logins != 2 || pre.Links != 1 || pre.Usage != 1 {
		t.Fatalf("%+v %v", pre, err)
	}
	if _, err := s.ClientByEmail(ctx, "reza-phone"); !errors.Is(err, ErrNotFound) {
		t.Fatal("dry run must not write")
	}
	rep, err := s.MigrateFromOld(ctx, old, MigrateOptions{Inbounds: []string{"de-vless"}})
	if err != nil || rep.Clients != 2 || len(rep.Skipped) == 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	reza, err := s.ClientByEmail(ctx, "reza-phone")
	if err != nil || reza.UUID != "8b1a4c2d-1111-4222-8333-944455556666" || reza.RepID != "ali-shop" || reza.MaxIPs != 2 || reza.VolumeBytes != 30<<30 ||
		reza.ExpiresUnix != 1900000000 || reza.Usage.Up+reza.Usage.Down != 1000 || reza.Note != "Reza" || len(reza.Inbounds) != 1 {
		t.Fatalf("%+v", reza)
	}
	mina, _ := s.ClientByEmail(ctx, "mina")
	if mina.VolumeBytes != -1 || mina.DurationSeconds != 30*86400 || mina.UUID == "not-a-uuid" || mina.RepID != "" || mina.Plan != "unlimited" {
		t.Fatalf("%+v", mina.Client)
	}
	reps, _ := s.Reps(ctx)
	var shop, frozen, sub *Rep
	for i := range reps {
		switch reps[i].ID {
		case "ali-shop":
			shop = &reps[i]
		case "frozen":
			frozen = &reps[i]
		case "sub1":
			sub = &reps[i]
		}
	}
	if shop == nil || shop.VolumeCredit != 5<<30 || shop.UnlimitedUnits != 4 || frozen == nil || frozen.Enabled || sub == nil || sub.ParentID != "ali-shop" {
		t.Fatalf("%+v %+v %+v", shop, frozen, sub)
	}
	// Old passwords work once, then are bcrypt.
	if _, err := s.Login(ctx, "darkpro", "old-owner-password"); err != nil {
		t.Fatal("old password", err)
	}
	var h string
	_ = s.ro.QueryRow(`SELECT pass_hash FROM admins WHERE username='darkpro'`).Scan(&h)
	if strings.HasPrefix(h, "argon2id:") {
		t.Fatal("hash not upgraded")
	}
	if _, err := s.Login(ctx, "darkpro", "old-owner-password"); err != nil {
		t.Fatal("upgraded password", err)
	}
	if _, err := s.Login(ctx, "ali", "wrong"); err == nil {
		t.Fatal("wrong password")
	}
	// The old subscription URL answers with the client's v2 subscription.
	srv := httptest.NewServer(SubHandler(s))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/inbound-subscriptions/" + strings.Repeat("A", 43) + "?format=links")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Subscription-Userinfo"), "total=") || len(body) == 0 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if r2, _ := http.Get(srv.URL + "/api/v1/inbound-subscriptions/" + strings.Repeat("B", 43)); r2.StatusCode != 404 {
		t.Fatal("unknown old token")
	}
	// A second import skips everything that already exists.
	again, err := s.MigrateFromOld(ctx, old, MigrateOptions{Inbounds: []string{"de-vless"}})
	if err != nil || again.Clients != 0 || again.Reps != 0 {
		t.Fatalf("%+v %v", again, err)
	}
}
