//go:build linux

package usageledger

import (
	"context"
	"errors"
	"testing"
)

func TestMeteredHealthQueueSchemaAtomicUpgradeAndDowngradeGuard(t *testing.T) {
	ctx := context.Background()
	db, _, l, _, _ := probeTestLedger(t)
	if err := l.EnableHealthChallenges(ctx); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("interrupted queue migration")
	l.checkpoint = func(at string) error {
		if at == "before_commit" {
			return failure
		}
		return nil
	}
	if err := l.EnableHealthQueue(ctx); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	l.checkpoint = nil
	var v, n int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 10 {
		t.Fatal("partial version", v, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_health_intents*'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial queue", n, err)
	}
	l.checkpoint = func(at string) error {
		if at == "after_commit" {
			return failure
		}
		return nil
	}
	if err := l.EnableHealthQueue(ctx); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	l.checkpoint = nil
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.EnableHealthQueue(ctx); err != nil {
		t.Fatal(err)
	}
	if err = l.EnableHealthChallenges(ctx); err != nil {
		t.Fatal(err)
	}
	if err = l.EnableProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != 11 {
		t.Fatal("migration replay downgraded queue", v, err)
	}
	if _, err = db.Exec(`PRAGMA user_version=10`); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatal("older marker ignored intent queue", err)
	}
}
