//go:build linux

package usageledger

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUsageLedgerCancelledStatementsDoNotPoisonPool(t *testing.T) {
	db, _ := testDB(t)
	l, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	var initial int
	if err := db.QueryRow("SELECT value FROM native_config_generation WHERE id=1").Scan(&initial); err != nil {
		t.Fatal(err)
	}
	committed, canceled := 0, 0
	const attempts = 1000
	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { time.Sleep(time.Duration(10+i%200) * time.Microsecond); cancel(); close(done) }()
		operation := func(c *ledgerConn) error {
			var n int
			if e := c.QueryRowContext(ctx, "SELECT value FROM native_config_generation WHERE id=1").Scan(&n); e != nil {
				return e
			}
			if i%2 == 1 {
				return nil
			}
			_, e := c.ExecContext(ctx, "UPDATE native_config_generation SET value=value+1 WHERE id=1")
			return e
		}
		var interrupted error
		if i%2 == 0 {
			interrupted = l.write(ctx, operation)
		} else {
			interrupted = l.read(ctx, operation)
		}
		<-done
		if interrupted == nil && i%2 == 0 {
			committed++
		} else if interrupted != nil {
			canceled++
		}
		fresh, stop := context.WithTimeout(context.Background(), time.Second)
		err := l.write(fresh, func(c *ledgerConn) error {
			_, e := c.ExecContext(fresh, "UPDATE native_config_generation SET value=value+1 WHERE id=1")
			return e
		})
		stop()
		if err != nil {
			for _, q := range []string{"PRAGMA foreign_keys", "PRAGMA synchronous", "PRAGMA journal_mode", "SELECT value FROM native_config_generation WHERE id=1"} {
				var value any
				e := db.QueryRow(q).Scan(&value)
				t.Logf("diagnostic %s value=%v err=%v", q, value, e)
			}
			t.Fatalf("iteration=%d interrupted=%v recovery=%v", i, interrupted, err)
		}
	}
	var final int
	if err := db.QueryRow("SELECT value FROM native_config_generation WHERE id=1").Scan(&final); err != nil {
		t.Fatal(err)
	}
	if canceled == 0 || final != initial+committed+attempts {
		t.Fatalf("canceled=%d committed=%d value=%d expected=%d", canceled, committed, final, initial+committed+attempts)
	}
}

func TestUsageLedgerCancellationBeforeCommitRollsBack(t *testing.T) {
	db, _ := testDB(t)
	l, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.checkpoint = func(stage string) error {
		if stage == "before_commit" {
			cancel()
		}
		return nil
	}
	err = l.write(ctx, func(c *ledgerConn) error {
		_, e := c.ExecContext(ctx, "UPDATE native_config_generation SET value=value+1 WHERE id=1")
		return e
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled transaction was acknowledged", err)
	}
	l.checkpoint = nil
	var value int
	if err = db.QueryRow("SELECT value FROM native_config_generation WHERE id=1").Scan(&value); err != nil || value != 1 {
		t.Fatal("canceled transaction partly committed", value, err)
	}
	if err = l.write(context.Background(), func(c *ledgerConn) error {
		_, e := c.ExecContext(context.Background(), "UPDATE native_config_generation SET value=value+1 WHERE id=1")
		return e
	}); err != nil {
		t.Fatal("canceled request poisoned next transaction", err)
	}
}
