//go:build linux

package usageledger

import (
	"context"
	"errors"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestMeteredHealthSchemaAtomicMigrationAndProbeAccounting(t *testing.T) {
	ctx := context.Background()
	db, _, l, p, e := probeTestLedger(t)
	interrupted := errors.New("migration reply interrupted")
	l.checkpoint = func(at string) error {
		if at == "before_commit" {
			return interrupted
		}
		return nil
	}
	if err := l.EnableHealthChallenges(ctx); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	l.checkpoint = nil
	var version, n int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 9 {
		t.Fatal("partial migration", version, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_health_*'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial policy", n, err)
	}
	l.checkpoint = func(at string) error {
		if at == "after_commit" {
			return interrupted
		}
		return nil
	}
	if err := l.EnableHealthChallenges(ctx); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	l.checkpoint = nil
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal("committed migration lost", err)
	}
	if err = l.EnableHealthChallenges(ctx); err != nil {
		t.Fatal("migration retry", err)
	}
	if err = l.RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	r := probeTestReport(t, e, 1, false, uc.Usage{ClientID: "customer"}, uc.Usage{ClientID: p.ID})
	if _, err = l.Accept(ctx, principal(e), r); err != nil {
		t.Fatal(err)
	}
	r = probeTestReport(t, e, 2, true, uc.Usage{ClientID: "customer", Uplink: 3}, uc.Usage{ClientID: p.ID, Uplink: 7, Downlink: 11})
	if _, err = l.Accept(ctx, principal(e), r); err != nil {
		t.Fatal(err)
	}
	a, err := l.ProbeUsage(ctx, p.ID)
	if err != nil || a.Total != 18 {
		t.Fatal("v10 lost system accounting", a.Total, err)
	}
	financial, err := l.Account(ctx, "customer")
	if err != nil || financial.Uplink != 3 {
		t.Fatal("v10 charged probe bytes", err)
	}
	if _, err = db.Exec(`PRAGMA user_version=9`); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatal("health journal accepted with older marker", err)
	}
}

func TestMeteredHealthSchemaRequiresProbeOptIn(t *testing.T) {
	ctx := context.Background()
	db, _ := testDB(t)
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.EnableHealthChallenges(ctx); !errors.Is(err, ErrSchema) {
		t.Fatal("ordinary schema silently opted in", err)
	}
}
