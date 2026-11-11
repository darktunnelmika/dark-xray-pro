package business

import (
	"context"
	"database/sql"
	"log"
	"net/url"
	"sync"
	"time"
)

// This worker is created only after explicit runtime enablement. It owns its
// connection and cursor. It reserves (but never executes SQL on) the sole
// accounting connection while pruning, so a deferred accounting transaction
// cannot lose its read snapshot to a concurrent maintenance commit.
type executionHistoryMaintainer struct {
	db     *sql.DB
	writer *sql.DB
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func openExecutionHistoryMaintainer(path string, writer *sql.DB) (*executionHistoryMaintainer, error) {
	if writer == nil || writer.Stats().MaxOpenConnections != 1 {
		return nil, ErrUnavailable
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_pragma=busy_timeout(0)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_pragma=trusted_schema(OFF)&_pragma=wal_autocheckpoint(0)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err = verifyInboundExecutionSchema(ctx, db)
	cancel()
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel = context.WithCancel(context.Background())
	m := &executionHistoryMaintainer{db: db, writer: writer, cancel: cancel, done: make(chan struct{})}
	go m.run(ctx)
	return m, nil
}
func (m *executionHistoryMaintainer) run(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cursor := executionHistoryCursor{}
	var removed, scanned, failures int
	var lastFailureLog time.Time
	recordFailure := func(err error) {
		failures++
		if lastFailureLog.IsZero() || time.Since(lastFailureLog) >= time.Minute {
			log.Printf("PRO usage history maintenance deferred attempts=%d error=%v", failures, err)
			lastFailureLog = time.Now()
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			attempt, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			// Reserve the Store writer before beginning maintenance. Merely
			// testing InUse would race with a transaction acquiring its snapshot.
			// SQL stays on the dedicated maintenance connection; release the
			// reservation only after commit/rollback and connection cleanup.
			reservation, err := m.writer.Conn(attempt)
			if err != nil {
				cancel()
				if ctx.Err() != nil {
					return
				}
				recordFailure(err)
				continue
			}
			conn, err := m.db.Conn(attempt)
			if err != nil {
				reservation.Close()
				cancel()
				if ctx.Err() != nil {
					return
				}
				recordFailure(err)
				continue
			}
			batch, err := pruneExecutionZeroBatch(attempt, conn, cursor)
			conn.Close()
			reservation.Close()
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				recordFailure(err)
				continue
			} // original cursor retained; retry on the next tick
			if failures > 0 {
				log.Printf("PRO usage history maintenance resumed deferred_attempts=%d", failures)
				failures = 0
				lastFailureLog = time.Time{}
			}
			cursor = batch.Next
			removed += batch.Removed
			scanned += batch.Scanned
			if batch.Done {
				log.Printf("PRO usage history sweep complete scanned=%d removed_zero_deltas=%d", scanned, removed)
				cursor = executionHistoryCursor{}
				removed = 0
				scanned = 0
				// A completed sweep waits an hour; shutdown still cancels immediately.
				wait := time.NewTimer(time.Hour)
				select {
				case <-ctx.Done():
					wait.Stop()
					return
				case <-wait.C:
				}
			}
		}
	}
}
func (m *executionHistoryMaintainer) close() error {
	if m == nil {
		return nil
	}
	m.once.Do(m.cancel)
	<-m.done
	return m.db.Close()
}
