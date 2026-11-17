package hubcore

import (
	"context"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestRenameClientCarriesEverything(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	c := Client{Email: "old1", Enabled: true, VolumeBytes: 30 << 30, ExpiresUnix: now.Unix() + 20*86400, Inbounds: []string{"de-vless"}}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ClientByEmail(ctx, "old1")
	for _, q := range []string{
		`INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES('old1',5,7,1,1)`,
		`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('old1','de-1','2026-11-01',5,7)`,
		`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('new1','de-1','2026-11-01',1,1)`, // a deleted client's leftover
		`INSERT INTO tg_links(chat_id,email) VALUES(42,'old1')`,
		`INSERT INTO settings(key,value) VALUES('remind:old1','9')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err := s.PutGroup(ctx, Group{ID: "g1", Name: "G", Members: []string{"old1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameClient(ctx, "old1", "bad name"); err == nil {
		t.Fatal("a name with a space must be refused")
	}
	if err := s.RenameClient(ctx, "old1", "new1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClientByEmail(ctx, "old1"); err == nil {
		t.Fatal("the old name must be gone")
	}
	got, err := s.ClientByEmail(ctx, "new1")
	if err != nil {
		t.Fatal(err)
	}
	if got.UUID != before.UUID || got.SubToken != before.SubToken || got.RepID != "ali" || len(got.Inbounds) != 1 {
		t.Fatalf("credentials, link, owner and inbounds stay: %+v", got)
	}
	if got.Usage.Up != 5 || got.Usage.Down != 7 {
		t.Fatalf("usage moves: %+v", got.Usage)
	}
	for q, want := range map[string]int{
		`SELECT COUNT(*) FROM usage_daily WHERE email='new1' AND up=5`:             1,
		`SELECT COUNT(*) FROM usage_daily WHERE email='old1'`:                      0,
		`SELECT COUNT(*) FROM tg_links WHERE email='new1'`:                         1,
		`SELECT COUNT(*) FROM client_group_members WHERE email='new1'`:             1,
		`SELECT COUNT(*) FROM credit_ledger l WHERE ` + ledgerClient + `='new1'`: 1,
		`SELECT COUNT(*) FROM settings WHERE key='remind:new1'`:                    1,
		`SELECT COUNT(*) FROM clients c JOIN client_inbounds i ON i.email=c.email WHERE c.email='new1'`: 1,
		`SELECT COUNT(*) FROM pragma_foreign_key_check`:                            0,
	} {
		var n int
		if err := s.db.QueryRow(q).Scan(&n); err != nil || n != want {
			t.Fatalf("%s = %d (%v), want %d", q, n, err, want)
		}
	}
	// A node that has not picked up the new config yet still reports the old name.
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{Seq: 1, ToUnix: now.Unix(), Traffic: []wire.UserTraffic{{Email: "old1", Up: 10, Down: 20}}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ClientByEmail(ctx, "new1")
	if got.Usage.Up != 15 || got.Usage.Down != 27 {
		t.Fatalf("late usage lands on the renamed client: %+v", got.Usage)
	}
	// Taken names are refused, whatever the letter case.
	_ = s.PutClient(ctx, Client{Email: "other", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-vless"}})
	if err := s.RenameClient(ctx, "other", "NEW1"); err == nil {
		t.Fatal("a name in use must be refused")
	}
	// Renaming twice follows the chain.
	if err := s.RenameClient(ctx, "new1", "new2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{Seq: 2, ToUnix: now.Unix(), Traffic: []wire.UserTraffic{{Email: "old1", Up: 1, Down: 1}}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ClientByEmail(ctx, "new2")
	if got.Usage.Up != 16 {
		t.Fatalf("chain: %+v", got.Usage)
	}
}
