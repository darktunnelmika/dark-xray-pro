package business

import (
	"context"
	"database/sql"
	"log"
	"net/url"
	"sync"
	"time"
)

// Checkpoints must not occupy the accounting writer's only connection.
// FULL durability is retained on both connections. Only PASSIVE checkpoints
// run here: no writer lock escalation, truncate, or ledger mutation.
type businessCheckpointer struct {
	db           *sql.DB
	cancel       context.CancelFunc
	done         chan struct{}
	once         sync.Once
	pageBytes    int64
	pendingSince time.Time
}

func openBusinessCheckpointer(path string) (*businessCheckpointer, error) {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_pragma=busy_timeout(0)&_pragma=synchronous(FULL)&_pragma=wal_autocheckpoint(0)&_pragma=trusted_schema(OFF)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var pageBytes int64
	err = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageBytes)
	cancel()
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel = context.WithCancel(context.Background())
	c := &businessCheckpointer{db: db, cancel: cancel, done: make(chan struct{}), pageBytes: pageBytes}
	go c.run(ctx)
	return c, nil
}

func (c *businessCheckpointer) checkpoint(ctx context.Context) (int, int, int, error) {
	var busy, frames, done int
	err := c.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &frames, &done)
	return busy, frames, done, err
}

// NOOP reports the logical backlog without copying or syncing WAL frames.
// The physical WAL allocation is retained for reuse and is not a backlog.
// Small batches age out after a minute; large batches retain the 5s cadence.
// This scheduling state belongs exclusively to the maintenance goroutine.
func (c *businessCheckpointer) checkpointIfNeeded(ctx context.Context, now time.Time) (attempted bool, busy, frames, done int, err error) {
	err = c.db.QueryRowContext(ctx, "PRAGMA main.wal_checkpoint(NOOP)").Scan(&busy, &frames, &done)
	if err != nil || busy != 0 {
		return
	}
	if frames <= done || frames < 0 || done < 0 {
		c.pendingSince = time.Time{}
		return
	}
	if c.pendingSince.IsZero() {
		c.pendingSince = now
	}
	if int64(frames-done)*c.pageBytes < 1<<20 && now.Sub(c.pendingSince) < time.Minute {
		return
	}
	attempted = true
	busy, frames, done, err = c.checkpoint(ctx)
	if err == nil && busy == 0 && frames >= 0 && done >= frames {
		c.pendingSince = time.Time{}
	}
	return
}

func (c *businessCheckpointer) run(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	failed := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
			started := time.Now()
			attempted, _, frames, done, err := c.checkpointIfNeeded(attempt, started)
			elapsed := time.Since(started)
			deadlineExceeded := attempt.Err() == context.DeadlineExceeded
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if !failed || elapsed > time.Second {
					log.Printf("PRO background checkpoint unavailable elapsed_ms=%d attempted=%t deadline_exceeded=%t; durable WAL retained for retry", elapsed.Milliseconds(), attempted, deadlineExceeded)
				}
				failed = true
				continue
			}
			if failed {
				log.Print("PRO background checkpoint recovered")
				failed = false
			}
			if attempted && elapsed > time.Second {
				log.Printf("PRO background checkpoint slow elapsed_ms=%d frames=%d checkpointed=%d", elapsed.Milliseconds(), frames, done)
			}
		}
	}
}

func (c *businessCheckpointer) close() error {
	if c == nil {
		return nil
	}
	c.once.Do(c.cancel)
	<-c.done
	return c.db.Close()
}
