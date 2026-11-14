package hubcore

import (
	"context"
	"testing"
)

func TestSystemOnlineNodeTraffic(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	si := s.System(ctx)
	if si.Version != HubVersion || si.Cores == 0 || si.DBSize == 0 {
		t.Fatalf("%+v", si)
	}
	day := now.UTC().Format("2006-01-02")
	if _, err := s.db.Exec(`INSERT INTO online(email,node_id,ips,seen_unix) VALUES('alice','de-1','["1.2.3.4"]',?),('bob','de-1','[]',?)`, now.Unix(), now.Unix()-3600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO usage_daily(email,node_id,day,up,down) VALUES('alice','de-1',?,10,90),('bob','de-1',?,1,9)`, day, day); err != nil {
		t.Fatal(err)
	}
	on, err := s.Online(ctx)
	if err != nil || len(on) != 1 || on[0].Email != "alice" || on[0].Today != 100 || on[0].IPs[0] != "1.2.3.4" {
		t.Fatal("only recently seen clients are online", on, err)
	}
	tr, err := s.NodeTraffic(ctx, 7)
	if err != nil || len(tr) != 1 || tr[0].Up != 11 || tr[0].Down != 99 {
		t.Fatal(tr, err)
	}
}
