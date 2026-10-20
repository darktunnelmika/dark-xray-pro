package usageledger

import (
	"context"
	"database/sql"
	"database/sql/driver"
)

// The pinned modernc driver can lose the rows handle if a context is canceled
// just as QueryContext returns. The leaked SQLite statement can keep that
// connection permanently interrupted; merely discarding the connection can
// also leave its transaction locked until the missing statement is finalized.
//
// Honor request cancellation BETWEEN ledger statements, never by interrupting
// one in flight. SQL here is fixed, bounded ledger/schema work; checkConnection
// additionally requires a positive SQLite busy timeout of at most five seconds.
// write checks cancellation again before COMMIT and rolls back independently.
// A cancel concurrent with COMMIT remains an ordinary lost-receipt ambiguity,
// resolved by the existing exact replay protocol, never by guessing a total.
type ledgerConn struct{ *sql.Conn }

func (l *Ledger) read(ctx context.Context, fn func(*ledgerConn) error) error {
	raw, err := l.db.Conn(ctx)
	if err != nil {
		return err
	}
	c := &ledgerConn{raw}
	defer c.Close()
	if err = checkConnection(ctx, c); err != nil {
		_ = c.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	if err = fn(c); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *ledgerConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.ExecContext(context.WithoutCancel(ctx), query, args...)
}

func (c *ledgerConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.QueryContext(context.WithoutCancel(ctx), query, args...)
}

func (c *ledgerConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if ctx.Err() != nil {
		// An ALREADY canceled context is rejected before the driver's statement
		// or interrupt goroutine starts. Scan retains the original context error.
		return c.Conn.QueryRowContext(ctx, query, args...)
	}
	return c.Conn.QueryRowContext(context.WithoutCancel(ctx), query, args...)
}
