package business

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Exercise database/sql's context-triggered Tx cleanup through public APIs.
// No manual Rollback runs before the fresh pooled read: that read must wait for
// awaitDone to finish releasing the only connection. The driver may retain or
// discard that connection; either policy must preserve the synthetic account.
func TestSQLiteCanceledTransactionRestoresPool(t *testing.T) {
	const attempts = 3
	whole, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	location := &url.URL{Scheme: "file", Path: filepath.Join(t.TempDir(), "synthetic-tx-cancellation.sqlite")}
	options := url.Values{}
	for _, pragma := range []string{"journal_mode(WAL)", "synchronous(FULL)", "busy_timeout(100)", "wal_autocheckpoint(0)"} {
		options.Add("_pragma", pragma)
	}
	location.RawQuery = options.Encode()
	db, err := sql.Open("sqlite", location.String())
	if err != nil {
		t.Fatal("open synthetic transaction database:", sqliteTxCancellationErrorClass(err))
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var mode string
	if err = db.QueryRowContext(whole, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatal("synthetic transaction WAL mode:", sqliteTxCancellationErrorClass(err))
	}
	for _, statement := range []string{
		"CREATE TABLE synthetic_tx_account (value INTEGER NOT NULL)",
		"INSERT INTO synthetic_tx_account VALUES (100)",
	} {
		if _, err = db.ExecContext(whole, statement); err != nil {
			t.Fatal("initialize synthetic transaction account:", sqliteTxCancellationErrorClass(err))
		}
	}

	for attempt := 0; attempt < attempts; attempt++ {
		expected := int64(100 + attempt)
		request, cancel := context.WithCancel(whole)
		defer cancel()
		tx, beginErr := db.BeginTx(request, nil)
		if beginErr != nil {
			t.Fatalf("attempt=%d begin=%s", attempt, sqliteTxCancellationErrorClass(beginErr))
		}
		defer tx.Rollback() // Failure cleanup only; runs after the checks below.
		result, writeErr := tx.ExecContext(request, "UPDATE synthetic_tx_account SET value=value+7")
		var affected, uncommitted int64
		if writeErr == nil {
			affected, writeErr = result.RowsAffected()
		}
		readErr := tx.QueryRowContext(request, "SELECT value FROM synthetic_tx_account").Scan(&uncommitted)
		if writeErr != nil || readErr != nil || affected != 1 || uncommitted != expected+7 {
			t.Fatalf("attempt=%d uncommitted update=%s read=%s affected=%d value=%d", attempt,
				sqliteTxCancellationErrorClass(writeErr), sqliteTxCancellationErrorClass(readErr), affected, uncommitted)
		}
		cancel()

		// This context is independent of the canceled transaction's context.
		fresh, stop := context.WithTimeout(whole, 2*time.Second)
		defer stop()
		var restored int64
		readErr = db.QueryRowContext(fresh, "SELECT value FROM synthetic_tx_account").Scan(&restored)
		if readErr != nil || restored != expected || db.Stats().InUse != 0 {
			t.Fatalf("attempt=%d automatic rollback read=%s value=%d expected=%d in_use=%d", attempt,
				sqliteTxCancellationErrorClass(readErr), restored, expected, db.Stats().InUse)
		}
		commitErr := tx.Commit()
		if !errors.Is(commitErr, sql.ErrTxDone) && !errors.Is(commitErr, context.Canceled) {
			t.Fatalf("attempt=%d canceled transaction commit=%s", attempt, sqliteTxCancellationErrorClass(commitErr))
		}

		recovery, beginErr := db.BeginTx(fresh, nil)
		if beginErr != nil {
			t.Fatalf("attempt=%d recovery begin=%s", attempt, sqliteTxCancellationErrorClass(beginErr))
		}
		defer recovery.Rollback()
		result, writeErr = recovery.ExecContext(fresh, "UPDATE synthetic_tx_account SET value=value+1")
		affected = 0
		if writeErr == nil {
			affected, writeErr = result.RowsAffected()
		}
		if writeErr != nil || affected != 1 {
			t.Fatalf("attempt=%d recovery update=%s affected=%d", attempt, sqliteTxCancellationErrorClass(writeErr), affected)
		}
		if commitErr = recovery.Commit(); commitErr != nil {
			t.Fatalf("attempt=%d recovery commit=%s", attempt, sqliteTxCancellationErrorClass(commitErr))
		}
		var committed int64
		readErr = db.QueryRowContext(fresh, "SELECT value FROM synthetic_tx_account").Scan(&committed)
		var busy, logFrames, checkpointed int
		checkpointErr := db.QueryRowContext(fresh, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
		if readErr != nil || committed != expected+1 || checkpointErr != nil ||
			busy != 0 || logFrames != 0 || checkpointed != 0 || db.Stats().InUse != 0 {
			t.Fatalf("attempt=%d committed read=%s value=%d expected=%d checkpoint=%s tuple=%d/%d/%d in_use=%d", attempt,
				sqliteTxCancellationErrorClass(readErr), committed, expected+1, sqliteTxCancellationErrorClass(checkpointErr),
				busy, logFrames, checkpointed, db.Stats().InUse)
		}
		stop()
	}
	t.Logf("automatic_rollbacks=%d unchanged_accounts=%d subsequent_exact_commits=%d", attempts, attempts, attempts)
}

func sqliteTxCancellationErrorClass(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, sql.ErrTxDone):
		return "transaction_done"
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		return "sqlite_" + strconv.Itoa(coded.Code())
	}
	return "other"
}
