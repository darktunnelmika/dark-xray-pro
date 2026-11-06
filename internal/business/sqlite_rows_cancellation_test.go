package business

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite "modernc.org/sqlite"
)

var sqliteRowsCancellationSerial atomic.Uint64

type sqliteRowsCancellationGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *sqliteRowsCancellationGate) unblock() { g.once.Do(func() { close(g.release) }) }

// This is the public-API scenario from modernc SQLite MR81, adapted to a real
// temporary WAL database and a channel rendezvous instead of a startup sleep.
// Cancellation is known to occur inside the UDF. The exact driver return/defer
// race remains scheduler-dependent, so use eight bounded attempts. No internal
// driver hook, production database, key, namespace, or service is involved.
// https://gitlab.com/cznic/sqlite/-/merge_requests/81/diffs
func TestSQLiteCanceledRowsKeepConnectionUsable(t *testing.T) {
	const attempts = 8
	whole, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	var gateMu sync.Mutex
	var active *sqliteRowsCancellationGate
	function := "dark_pro_cancel_rows_" + strconv.FormatUint(sqliteRowsCancellationSerial.Add(1), 10)
	if err := sqlite.RegisterScalarFunction(function, 0,
		func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
			gateMu.Lock()
			g := active
			gateMu.Unlock()
			if g == nil {
				return nil, errors.New("cancellation_gate_missing")
			}
			close(g.entered) // The query has exactly one input row.
			select {
			case <-g.release:
				return int64(42), nil
			case <-whole.Done():
				return nil, whole.Err()
			}
		}); err != nil {
		t.Fatal("register synthetic cancellation function:", sqliteRowsErrorClass(err))
	}

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "synthetic-cancellation.sqlite"))
	if err != nil {
		t.Fatal("open synthetic database:", sqliteRowsErrorClass(err))
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	// Retaining this public sql.Conn prevents replacement of a poisoned driver
	// connection from making the recovery checks appear successful.
	conn, err := db.Conn(whole)
	if err != nil {
		t.Fatal("acquire synthetic connection:", sqliteRowsErrorClass(err))
	}
	defer conn.Close()
	var mode string
	if err = conn.QueryRowContext(whole, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || mode != "wal" {
		t.Fatal("enable synthetic WAL:", sqliteRowsErrorClass(err))
	}
	for _, statement := range []string{
		"PRAGMA busy_timeout=100",
		"PRAGMA synchronous=FULL",
		"PRAGMA wal_autocheckpoint=0",
		"CREATE TABLE synthetic_account (value INTEGER NOT NULL)",
		"INSERT INTO synthetic_account VALUES (100)",
	} {
		if _, err = conn.ExecContext(whole, statement); err != nil {
			t.Fatal("initialize synthetic database:", sqliteRowsErrorClass(err))
		}
	}

	canceledReturns, successfulRaces := 0, 0
	for attempt := 0; attempt < attempts; attempt++ {
		g := &sqliteRowsCancellationGate{entered: make(chan struct{}), release: make(chan struct{})}
		defer g.unblock()
		gateMu.Lock()
		active = g
		gateMu.Unlock()
		request, cancel := context.WithCancel(whole)
		defer cancel()
		type queryResult struct {
			err          error
			closeErr     error
			returnedRows bool
		}
		joined := make(chan queryResult, 1)
		go func() {
			rows, queryErr := conn.QueryContext(request, "SELECT value, "+function+"() FROM synthetic_account")
			hadRows := rows != nil
			var closeErr error
			if rows != nil {
				closeErr = rows.Close()
			}
			joined <- queryResult{queryErr, closeErr, hadRows}
		}()
		select {
		case <-g.entered:
		case <-whole.Done():
			t.Fatal("synthetic UDF entry deadline")
		}
		cancel()
		// Let the driver's cancellation watcher run while the actual SQLite UDF
		// remains blocked. This is a scheduling opportunity, not an internal-state
		// assertion; the subsequent same-connection checks are the regression.
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-timer.C:
		case <-whole.Done():
			timer.Stop()
			t.Fatal("synthetic cancellation deadline")
		}
		g.unblock()
		var observed queryResult
		select {
		case observed = <-joined:
		case <-whole.Done():
			t.Fatal("synthetic canceled query did not return")
		}
		queryAccepted := errors.Is(request.Err(), context.Canceled)
		switch {
		case observed.err == nil && observed.returnedRows:
			// A public Rows result can win the cancellation return race. It has
			// already been closed above; do not misclassify this as a failure or
			// count it as exercising the canceled-before-Rows-return branch.
			successfulRaces++
		case sqliteRowsCancellationError(observed.err):
			canceledReturns++
		default:
			queryAccepted = false
		}
		closeAccepted := observed.closeErr == nil || sqliteRowsCancellationError(observed.closeErr)

		// The query canceled above was read-only. Verify the account before the
		// sole intentional increment, then verify its exact value afterward.
		var one, before, after int64
		selectErr := conn.QueryRowContext(whole, "SELECT 1").Scan(&one)
		beforeErr := conn.QueryRowContext(whole, "SELECT value FROM synthetic_account").Scan(&before)
		result, writeErr := conn.ExecContext(whole, "UPDATE synthetic_account SET value=value+1")
		var affected int64
		if writeErr == nil {
			affected, writeErr = result.RowsAffected()
		}
		afterErr := conn.QueryRowContext(whole, "SELECT value FROM synthetic_account").Scan(&after)
		var busy, logFrames, checkpointed int
		checkpointErr := conn.QueryRowContext(whole, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
		if !queryAccepted || !closeAccepted || selectErr != nil || beforeErr != nil || writeErr != nil || afterErr != nil || checkpointErr != nil ||
			one != 1 || before != int64(100+attempt) || affected != 1 || after != int64(101+attempt) ||
			busy != 0 || logFrames != 0 || checkpointed != 0 {
			t.Fatalf("attempt=%d query=%s close=%s rows=%t request=%s recovery select=%s before=%s write=%s after=%s checkpoint=%s values=%d/%d affected=%d checkpoint_tuple=%d/%d/%d",
				attempt, sqliteRowsErrorClass(observed.err), sqliteRowsErrorClass(observed.closeErr), observed.returnedRows, sqliteRowsErrorClass(request.Err()),
				sqliteRowsErrorClass(selectErr), sqliteRowsErrorClass(beforeErr), sqliteRowsErrorClass(writeErr),
				sqliteRowsErrorClass(afterErr), sqliteRowsErrorClass(checkpointErr), before, after, affected, busy, logFrames, checkpointed)
		}
	}
	if canceledReturns == 0 {
		t.Fatal("no canceled QueryContext return witnessed within bounded attempts")
	}
	if err = conn.Close(); err != nil || db.Stats().InUse != 0 {
		t.Fatal("synthetic connection did not return to pool:", sqliteRowsErrorClass(err))
	}
	t.Logf("canceled_query_returns=%d successful_rows_races=%d same_connection_recoveries=%d exact_synthetic_increments=%d", canceledReturns, successfulRaces, attempts, attempts)
}

// The matching request must also report context.Canceled. Some driver return
// races expose SQLite's native interruption result instead of ctx.Err(); other
// SQL errors and deadlines do not count as cancellation coverage.
func sqliteRowsCancellationError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	var coded interface{ Code() int }
	return errors.As(err, &coded) && coded.Code()&255 == 9 // SQLITE_INTERRUPT
}

func sqliteRowsErrorClass(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		return "sqlite_" + strconv.Itoa(coded.Code())
	}
	return "other"
}
