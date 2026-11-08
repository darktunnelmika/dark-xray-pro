package business

import (
	"context"
	"database/sql"
	"log"
	"net/url"
	"os"
	"sync"
	"time"
)

// Checkpoints must not occupy the accounting writer's only connection.
// FULL durability is retained on both connections. Only PASSIVE checkpoints
// run here: no writer lock escalation, truncate, or ledger mutation.
type businessCheckpointer struct {
	db     *sql.DB
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
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
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel = context.WithCancel(context.Background())
	c := &businessCheckpointer{db: db, cancel: cancel, done: make(chan struct{})}
	go c.run(ctx, path)
	return c, nil
}

func (c *businessCheckpointer) checkpoint(ctx context.Context) (int, int, int, error) {
	var busy, frames, done int
	err := c.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &frames, &done)
	return busy, frames, done, err
}

func (c *businessCheckpointer) run(ctx context.Context, path string) {
	defer close(c.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	failed := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path + "-wal")
			if os.IsNotExist(err) {
				continue
			}
			if err == nil && info.Size() < 1<<20 {
				continue
			}
			if err != nil {
				if !failed {
					log.Print("PRO checkpoint WAL inspection unavailable; accounting remains FULL durable")
				}
				failed = true
				continue
			}
			attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
			started := time.Now()
			_, frames, done, err := c.checkpoint(attempt)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if !failed {
					log.Print("PRO background checkpoint unavailable; durable WAL retained for retry")
				}
				failed = true
				continue
			}
			if failed {
				log.Print("PRO background checkpoint recovered")
				failed = false
			}
			if time.Since(started) > time.Second {
				log.Printf("PRO background checkpoint slow elapsed_ms=%d frames=%d checkpointed=%d", time.Since(started).Milliseconds(), frames, done)
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
