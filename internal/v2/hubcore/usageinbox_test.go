package hubcore

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// A big batch is applied in chunks; a stop in the middle resumes where it
// stopped, and a repeat of the batch is never counted twice.
func TestUsageInboxResumesWithoutCountingTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	ctx := context.Background()
	n := usageChunk*2 + 7
	ts := time.Now().Unix()
	b := wire.UsageBatch{NodeID: "de-1", Seq: 1, FromUnix: ts - 5, ToUnix: ts, Online: []wire.UserOnline{{Email: "alice", IPs: []string{"1.1.1.1"}}},
		Disconnects: []wire.Disconnect{{Unix: 5, Email: "alice", Reason: "volume"}}}
	for i := 0; i < n; i++ {
		b.Traffic = append(b.Traffic, wire.UserTraffic{Email: fmt.Sprintf("x%05d", i), Up: 1, Down: 1})
	}
	b.Traffic = append(b.Traffic, wire.UserTraffic{Email: "alice", Up: 10, Down: 20})
	// Accept, apply one chunk, then stop.
	if _, err := s.acceptUsage(ctx, "de-1", b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.applyUsageChunk(ctx, "de-1", b, ts, 100, 0); err != nil {
		t.Fatal(err)
	}
	resp, err := s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1"})
	if err != nil || resp.UsageSeq != 0 {
		t.Fatalf("a half-applied batch was reported as applied: %d %v", resp.UsageSeq, err)
	}
	s.Close()
	// The restart finishes it.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check := func(when string) {
		t.Helper()
		var rows, total int64
		_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(up+down),0) FROM client_usage WHERE email LIKE 'x%'`).Scan(&rows, &total)
		if rows != int64(n) || total != int64(2*n) {
			t.Fatalf("%s: %d rows, %d bytes; want %d, %d", when, rows, total, n, 2*n)
		}
		u, _ := s.ClientUsage(ctx, "alice")
		if u.Up+u.Down != 30 {
			t.Fatalf("%s: alice %d", when, u.Up+u.Down)
		}
		var ev, left int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE email='alice' AND kind='blocked:volume'`).Scan(&ev)
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM usage_inbox`).Scan(&left)
		if ev != 1 || left != 0 {
			t.Fatalf("%s: %d events, %d left in the inbox", when, ev, left)
		}
	}
	check("after restart")
	// The node did not get its answer and sends the batch again.
	ack, err := s.ApplyUsage(ctx, "de-1", b)
	if err != nil || ack.Seq != 1 {
		t.Fatalf("repeat %+v %v", ack, err)
	}
	check("after the repeat")
	resp, _ = s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1"})
	if resp.UsageSeq != 1 {
		t.Fatalf("applied seq %d", resp.UsageSeq)
	}
	on, _ := s.Online(ctx)
	if len(on) != 1 || on[0].Email != "alice" {
		t.Fatalf("online %+v", on)
	}
	// An empty online snapshot clears the node's list.
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 2, Online: []wire.UserOnline{}}); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.Online(ctx); len(on) != 0 {
		t.Fatalf("empty snapshot kept %+v", on)
	}
	// A bad row still rejects the whole batch before anything is stored.
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 3, Traffic: []wire.UserTraffic{{Email: "bad name", Up: 1}}}); err == nil {
		t.Fatal("a bad row was accepted")
	}
	var seq uint64
	_ = s.db.QueryRow(`SELECT usage_seq FROM nodes WHERE id='de-1'`).Scan(&seq)
	if seq != 2 {
		t.Fatalf("a rejected batch moved the sequence to %d", seq)
	}
}

// An older database (before the inbox) starts with every accepted batch
// counted as applied.
func TestUsageInboxUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, s)
	for _, q := range []string{`DROP TABLE usage_inbox`, `ALTER TABLE nodes DROP COLUMN applied_seq`, `UPDATE nodes SET usage_seq=41`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resp, err := s.Sync(context.Background(), "de-1", wire.SyncRequest{NodeID: "de-1"})
	if err != nil || resp.UsageSeq != 41 {
		t.Fatalf("%d %v", resp.UsageSeq, err)
	}
}
