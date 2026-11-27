package hubcore

import (
	"context"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func repByID(t *testing.T, s *Store, id string) Rep {
	t.Helper()
	list, err := s.Reps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no reseller %s", id)
	return Rep{}
}

// A reseller that moved here with old usage: the lifetime total keeps the
// old panel's traffic, used_here counts only this panel's, and neither a
// reset nor a delete takes anything off it.
func TestUsedHereCountsOnlyThisPanel(t *testing.T) {
	s, now := newStore(t)
	seed(t, s)
	ctx := context.Background()
	_ = s.PutRep(ctx, Rep{ID: "saman", Name: "Saman", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "saman", 100<<30, 0, "")
	for _, e := range []string{"old1", "old2"} {
		if err := s.RepPutClient(ctx, "saman", "saman", Client{Email: e, Enabled: true, VolumeBytes: 10 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
			t.Fatal(err)
		}
	}
	// The old panel's usage, as an import brings it, and its lifetime base.
	_, _ = s.db.Exec(`INSERT INTO client_usage(email,up,down) VALUES('old1',0,?) ON CONFLICT(email) DO UPDATE SET down=excluded.down`, int64(7<<30))
	_, _ = s.db.Exec(`UPDATE reps SET used_base=? WHERE id='saman'`, int64(50<<30))
	if r := repByID(t, s, "saman"); r.UsedHere != 0 || r.UsedBytes != 57<<30 {
		t.Fatalf("before any traffic here: here %d total %d", r.UsedHere, r.UsedBytes)
	}
	seq := uint64(0)
	use := func(email string, n int64) {
		seq++
		*now = now.Add(60e9)
		if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{Seq: seq, ToUnix: now.Unix(), Traffic: []wire.UserTraffic{{Email: email, Down: n}}}); err != nil {
			t.Fatal(err)
		}
	}
	use("old1", 1<<30)
	use("old2", 2<<30)
	use("alice", 5<<30) // the owner's own client: not the reseller's
	if r := repByID(t, s, "saman"); r.UsedHere != 3<<30 || r.UsedBytes != 60<<30 {
		t.Fatalf("after traffic: here %d total %d", r.UsedHere, r.UsedBytes)
	}
	if err := s.ResetUsage(ctx, "old1", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteClient(ctx, "old2"); err != nil {
		t.Fatal(err)
	}
	if r := repByID(t, s, "saman"); r.UsedHere != 3<<30 || r.UsedBytes != 60<<30 {
		t.Fatalf("after reset and delete: here %d total %d", r.UsedHere, r.UsedBytes)
	}
}

// A sub-seller the owner deletes hands its unused credit back to its parent.
func TestDeletedSubSellerReturnsCredit(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	_ = s.PutRep(ctx, Rep{ID: "saman", Name: "Saman", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "saman", 2000<<30, 3, "")
	if err := s.PutSeller(ctx, "saman", Rep{ID: "parham", Name: "Parham", Enabled: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.TransferCredit(ctx, "saman", "saman", "parham", 1000<<30, 2, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProduct(ctx, Principal{User: "saman", Role: "rep", RepID: "saman"}, Product{ID: "p100", Seller: "parham", Name: "100GB", Plan: "volume", GB: 100, Days: 30, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RepPutClient(ctx, "parham", "parham", Client{Email: "p1", Enabled: true, VolumeBytes: 100 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRep(ctx, "parham", true); err != nil {
		t.Fatal(err)
	}
	r := repByID(t, s, "saman")
	if r.VolumeCredit != 1900<<30 || r.UnlimitedUnits != 3 {
		t.Fatalf("parent credit %d GB, %d units; want 1900 GB, 3 units", r.VolumeCredit>>30, r.UnlimitedUnits)
	}
	var sum int64
	_ = s.db.QueryRow(`SELECT SUM(volume_delta) FROM credit_ledger WHERE rep_id='saman'`).Scan(&sum)
	if sum != r.VolumeCredit {
		t.Fatalf("ledger %d does not match the credit %d", sum, r.VolumeCredit)
	}
	// A top-level reseller's credit simply goes with it.
	if _, err := s.DeleteRep(ctx, "saman", true); err != nil {
		t.Fatal(err)
	}
}
