// Package usageledger atomically commits cumulative usage, quota decisions and
// exact receipts in the NEW PRO business database. No paths or legacy import.
package usageledger

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrSchema         = errors.New("unsupported or uninitialized PRO usage schema")
	ErrAuthority      = errors.New("usage authority does not match registered installation")
	ErrMissing        = errors.New("usage account or epoch not registered")
	ErrConflict       = errors.New("usage identity or transaction conflict")
	ErrStorage        = errors.New("usage storage integrity or durability check failed")
	ErrReconciliation = errors.New("usage accounting requires reconciliation")
)

const SchemaVersion = 1

type Ledger struct {
	db         *sql.DB
	checkpoint func(string) error
}

// V7/v8 use ordinary accounting. Explicit v9/v10 opt-ins require the complete
// system-probe and, in v10, health schemas; altered policy is refused.
func Open(ctx context.Context, db *sql.DB) (*Ledger, error) {
	if db == nil {
		return nil, ErrSchema
	}
	l := &Ledger{db: db}
	err := l.write(ctx, func(c *ledgerConn) error {
		var version int
		if e := c.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
			return e
		}
		var schemaErr error
		version,schemaErr=underlyingBusinessSchema(ctx,c,version)
		if schemaErr!=nil{return schemaErr}
		if version != 7 && version != 8 && !HasProbeSchema(version) {
			return ErrSchema
		}
		if e := verifyHealthSchema(ctx, c, version); e != nil {
			return e
		}
		if e := verifyProbeSchema(ctx, c, version); e != nil {
			return e
		}
		for _, q := range []string{"SELECT id,uuid,representative_id,plan_kind,volume_bytes,archived,expires_unix,deployment_state FROM clients LIMIT 0", "SELECT id,enabled,volume_credit_bytes,unlimited_credit_units FROM representatives LIMIT 0", "SELECT value FROM native_config_generation WHERE id=1"} {
			rows, e := c.QueryContext(ctx, q)
			if e != nil {
				return ErrSchema
			}
			rows.Close()
		}
		var count int
		if e := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_config_generation WHERE id=1 AND value>=1 AND value<9007199254740991").Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrSchema
		}
		if e := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pro_usage_meta'").Scan(&count); e != nil {
			return e
		}
		if HasProbeSchema(version) && count != 1 {
			return ErrSchema
		}
		if count == 1 {
			var v int
			if e := c.QueryRowContext(ctx, "SELECT version FROM pro_usage_meta WHERE id=1").Scan(&v); e != nil || v != SchemaVersion {
				return ErrSchema
			}
			for _, q := range []string{"SELECT client_id,uplink,downlink,baseline_complete,holds FROM pro_usage_accounts LIMIT 0", "SELECT epoch_id,epoch_json,binding_sha,cursor_json,state FROM pro_usage_epochs LIMIT 0", "SELECT epoch_id,sequence,sha256,quiesced FROM pro_usage_receipts LIMIT 0", "SELECT client_id,eligible FROM pro_usage_eligibility LIMIT 0"} {
				r, e := c.QueryContext(ctx, q)
				if e != nil {
					return ErrSchema
				}
				r.Close()
			}
			return verifySchema(ctx, c)
		}
		if e := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_usage_*'").Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return ErrSchema
		}
		for _, q := range schema {
			if _, e := c.ExecContext(ctx, q); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return l, nil
}
func checkConnection(ctx context.Context, c *ledgerConn) error {
	var fk, sync, busy int
	var mode string
	if c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy) != nil || busy <= 0 || busy > 5000 {
		return ErrStorage
	}
	if c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk) != nil || fk != 1 {
		return ErrStorage
	}
	if c.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync) != nil || sync < 2 {
		return ErrStorage
	}
	if c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode) != nil || strings.ToLower(mode) != "wal" {
		return ErrStorage
	}
	return nil
}

// Lock before reading across independent connections/processes. Only COMMIT
// permits a receipt. Rollback failure discards the poisoned pooled connection.
func (l *Ledger) write(ctx context.Context, fn func(*ledgerConn) error) (err error) {
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
	if _, err = c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		// An unsuccessful BEGIN has not established a usable transaction.
		// Never return that connection to the pool with ambiguous state.
		_ = c.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, e := c.ExecContext(cleanup, "ROLLBACK"); e != nil {
				_ = c.Raw(func(any) error { return driver.ErrBadConn })
			}
		}
	}()
	if err = fn(c); err != nil {
		return err
	}
	if l.checkpoint != nil {
		if err = l.checkpoint("before_commit"); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = c.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	if l.checkpoint != nil {
		if err = l.checkpoint("after_commit"); err != nil {
			return err
		}
	}
	return nil
}

type sqlWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func invalidate(ctx context.Context, c sqlWriter) error {
	res, e := c.ExecContext(ctx, "UPDATE native_config_generation SET value=value+1 WHERE id=1 AND value<9007199254740991")
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrStorage
	}
	return nil
}

var schema = []string{
	`CREATE TABLE pro_usage_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL)`,
	`INSERT INTO pro_usage_meta VALUES(1,1)`,
	`CREATE TABLE pro_usage_accounts(client_id TEXT PRIMARY KEY REFERENCES clients(id) ON DELETE RESTRICT,uplink INTEGER NOT NULL CHECK(typeof(uplink)='integer' AND uplink>=0),downlink INTEGER NOT NULL CHECK(typeof(downlink)='integer' AND downlink>=0),baseline_complete INTEGER NOT NULL CHECK(baseline_complete IN(0,1)),holds INTEGER NOT NULL DEFAULT 0 CHECK(holds>=0),CHECK(uplink<=9223372036854775807-downlink))`,
	`CREATE TABLE pro_usage_seeds(client_id TEXT PRIMARY KEY REFERENCES pro_usage_accounts(client_id) ON DELETE RESTRICT,event_id TEXT NOT NULL UNIQUE,body_json BLOB NOT NULL,created_unix INTEGER NOT NULL)`,
	`CREATE TABLE pro_usage_epochs(epoch_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,installation_id TEXT NOT NULL,kernel_boot_id TEXT NOT NULL,pid INTEGER NOT NULL,start_ticks TEXT NOT NULL,config_sha TEXT NOT NULL,binding_sha TEXT NOT NULL,epoch_json BLOB NOT NULL,cursor_json BLOB NOT NULL,state TEXT NOT NULL CHECK(state IN('open','sealed','uncertain')),created_unix INTEGER NOT NULL,UNIQUE(node_id,installation_id,kernel_boot_id,pid,start_ticks))`,
	`CREATE TABLE pro_usage_receipts(epoch_id TEXT NOT NULL REFERENCES pro_usage_epochs(epoch_id) ON DELETE RESTRICT,sequence TEXT NOT NULL,sha256 TEXT NOT NULL,quiesced INTEGER NOT NULL CHECK(quiesced IN(0,1)),received_unix INTEGER NOT NULL,PRIMARY KEY(epoch_id,sequence))`,
	`CREATE TABLE pro_usage_deltas(epoch_id TEXT NOT NULL,sequence TEXT NOT NULL,client_id TEXT NOT NULL REFERENCES pro_usage_accounts(client_id) ON DELETE RESTRICT,uplink INTEGER NOT NULL CHECK(typeof(uplink)='integer' AND uplink>=0),downlink INTEGER NOT NULL CHECK(typeof(downlink)='integer' AND downlink>=0),PRIMARY KEY(epoch_id,sequence,client_id),FOREIGN KEY(epoch_id,sequence) REFERENCES pro_usage_receipts(epoch_id,sequence) ON DELETE RESTRICT)`,
	`CREATE INDEX pro_usage_deltas_client ON pro_usage_deltas(client_id,epoch_id,sequence)`,
	`CREATE TABLE pro_usage_holds(epoch_id TEXT NOT NULL REFERENCES pro_usage_epochs(epoch_id),client_id TEXT NOT NULL REFERENCES pro_usage_accounts(client_id),reason TEXT NOT NULL,created_unix INTEGER NOT NULL,PRIMARY KEY(epoch_id,client_id))`,
	`CREATE TRIGGER pro_usage_quota_changed AFTER UPDATE OF volume_bytes,plan_kind ON clients WHEN NEW.volume_bytes!=OLD.volume_bytes OR NEW.plan_kind!=OLD.plan_kind BEGIN UPDATE native_config_generation SET value=value+1 WHERE id=1; END`,
	// Unknown baseline, missing account or uncertain crash tail is NOT zero.
	`CREATE VIEW pro_usage_eligibility AS SELECT c.id AS client_id,CASE WHEN a.client_id IS NOT NULL AND a.baseline_complete=1 AND a.holds=0 AND c.archived=0 AND c.deployment_state!='disabled' AND r.enabled=1 AND c.expires_unix>CAST(strftime('%s','now') AS INTEGER) AND (c.plan_kind='unlimited' OR (c.plan_kind='volume' AND a.uplink+a.downlink<c.volume_bytes)) THEN 1 ELSE 0 END AS eligible FROM clients c JOIN representatives r ON r.id=c.representative_id LEFT JOIN pro_usage_accounts a ON a.client_id=c.id`,
}

func init() {
	for _, table := range []string{"pro_usage_seeds", "pro_usage_receipts", "pro_usage_deltas", "pro_usage_holds"} {
		for _, action := range []string{"UPDATE", "DELETE"} {
			schema = append(schema, fmt.Sprintf("CREATE TRIGGER %s_no_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'usage audit is append-only'); END", table, strings.ToLower(action), action, table))
		}
	}
}
