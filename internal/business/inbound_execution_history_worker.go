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
// connection and cursor, and never uses the accounting writer pool.
type executionHistoryMaintainer struct {
	db     *sql.DB
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func openExecutionHistoryMaintainer(path string) (*executionHistoryMaintainer, error) {
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
	m := &executionHistoryMaintainer{db: db, cancel: cancel, done: make(chan struct{})}
	go m.run(ctx)
	return m, nil
}
func (m *executionHistoryMaintainer) run(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cursor := executionHistoryCursor{}
	var removed, scanned int
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			attempt, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			conn, err := m.db.Conn(attempt)
			if err != nil {
				cancel()
				continue
			}
			batch, err := pruneExecutionZeroBatch(attempt, conn, cursor)
			conn.Close()
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				continue
			} // original cursor retained; retry on the next tick
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
