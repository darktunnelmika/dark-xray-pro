package usageledger

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// Opt-in only. Older binaries refuse v9 instead of misreading system counters
// as financial customers. The ordinary business schema remains v8 by default.
const ProbeBusinessSchemaVersion = 9

type ProbeIdentity struct {
	ID             string `json:"id"`
	UUID           string `json:"-"`
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	InboundID      uint64 `json:"inbound_id"`
	EventID        string `json:"event_id"`
}

type ProbeAccount struct {
	ID       string `json:"id"`
	Uplink   int64  `json:"uplink_bytes,string"`
	Downlink int64  `json:"downlink_bytes,string"`
	Total    int64  `json:"total_bytes,string"`
	Holds    int64  `json:"reconciliation_holds"`
}

var probeUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
var probeInstall = regexp.MustCompile(`^[a-f0-9]{32}$`)

var probeSchema = []string{
	`CREATE TABLE pro_probe_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`INSERT INTO pro_probe_meta VALUES(1,1)`,
	`CREATE TABLE pro_probe_accounts(probe_id TEXT PRIMARY KEY,uuid TEXT NOT NULL UNIQUE,node_id TEXT NOT NULL,installation_id TEXT NOT NULL,inbound_id INTEGER NOT NULL CHECK(inbound_id>0),event_id TEXT NOT NULL UNIQUE,uplink INTEGER NOT NULL DEFAULT 0 CHECK(typeof(uplink)='integer' AND uplink>=0),downlink INTEGER NOT NULL DEFAULT 0 CHECK(typeof(downlink)='integer' AND downlink>=0),holds INTEGER NOT NULL DEFAULT 0 CHECK(typeof(holds)='integer' AND holds>=0),created_unix INTEGER NOT NULL,CHECK(uplink<=9223372036854775807-downlink),UNIQUE(node_id,installation_id,inbound_id))`,
	`CREATE TABLE pro_probe_deltas(epoch_id TEXT NOT NULL,sequence TEXT NOT NULL,probe_id TEXT NOT NULL REFERENCES pro_probe_accounts(probe_id) ON DELETE RESTRICT,uplink INTEGER NOT NULL CHECK(typeof(uplink)='integer' AND uplink>=0),downlink INTEGER NOT NULL CHECK(typeof(downlink)='integer' AND downlink>=0),PRIMARY KEY(epoch_id,sequence,probe_id),FOREIGN KEY(epoch_id,sequence) REFERENCES pro_usage_receipts(epoch_id,sequence) ON DELETE RESTRICT)`,
	`CREATE TABLE pro_probe_holds(epoch_id TEXT NOT NULL REFERENCES pro_usage_epochs(epoch_id),probe_id TEXT NOT NULL REFERENCES pro_probe_accounts(probe_id),reason TEXT NOT NULL,created_unix INTEGER NOT NULL,PRIMARY KEY(epoch_id,probe_id))`,
	`CREATE TRIGGER pro_probe_identity_immutable BEFORE UPDATE OF probe_id,uuid,node_id,installation_id,inbound_id,event_id,created_unix ON pro_probe_accounts BEGIN SELECT RAISE(ABORT,'system probe identity immutable'); END`,
	`CREATE TRIGGER pro_probe_identity_no_delete BEFORE DELETE ON pro_probe_accounts BEGIN SELECT RAISE(ABORT,'system probe identity retained'); END`,
	`CREATE TRIGGER pro_probe_not_customer BEFORE INSERT ON pro_probe_accounts WHEN EXISTS(SELECT 1 FROM clients WHERE id=NEW.probe_id OR lower(uuid)=lower(NEW.uuid)) BEGIN SELECT RAISE(ABORT,'system probe collides with customer'); END`,
	`CREATE TRIGGER pro_probe_customer_insert BEFORE INSERT ON clients WHEN EXISTS(SELECT 1 FROM pro_probe_accounts WHERE probe_id=NEW.id OR lower(uuid)=lower(NEW.uuid)) BEGIN SELECT RAISE(ABORT,'customer collides with system probe'); END`,
	`CREATE TRIGGER pro_probe_customer_update BEFORE UPDATE OF id,uuid ON clients WHEN EXISTS(SELECT 1 FROM pro_probe_accounts WHERE probe_id=NEW.id OR lower(uuid)=lower(NEW.uuid)) BEGIN SELECT RAISE(ABORT,'customer collides with system probe'); END`,
	`CREATE TRIGGER pro_probe_deltas_no_update BEFORE UPDATE ON pro_probe_deltas BEGIN SELECT RAISE(ABORT,'system probe audit immutable'); END`,
	`CREATE TRIGGER pro_probe_deltas_no_delete BEFORE DELETE ON pro_probe_deltas BEGIN SELECT RAISE(ABORT,'system probe audit immutable'); END`,
	`CREATE TRIGGER pro_probe_holds_no_update BEFORE UPDATE ON pro_probe_holds BEGIN SELECT RAISE(ABORT,'system probe audit immutable'); END`,
	`CREATE TRIGGER pro_probe_holds_no_delete BEFORE DELETE ON pro_probe_holds BEGIN SELECT RAISE(ABORT,'system probe audit immutable'); END`,
}

func verifyProbeSchema(ctx context.Context, c *ledgerConn, version int) error {
	var n int
	if err := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_probe_*'").Scan(&n); err != nil {
		return err
	}
	if version != ProbeBusinessSchemaVersion {
		if n != 0 {
			return ErrSchema
		}
		return nil
	}
	if err := verifySchemaObjects(ctx, c, probeSchema, "pro_probe_*"); err != nil {
		return err
	}
	if err := c.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_probe_meta WHERE id=1 AND version=1").Scan(&n); err != nil || n != 1 {
		return ErrSchema
	}
	return nil
}

func probeAccountingEnabled(ctx context.Context, c sqlWriter) (bool, error) {
	var version int
	if err := c.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return false, err
	}
	return version == ProbeBusinessSchemaVersion, nil
}

// EnableProbeAccounting is a reviewed startup migration, never an Agent or
// browser action. It creates no identity and activates no runtime.
func (l *Ledger) EnableProbeAccounting(ctx context.Context) error {
	return l.write(ctx, func(c *ledgerConn) error {
		var version int
		if err := c.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version == ProbeBusinessSchemaVersion {
			return verifyProbeSchema(ctx, c, version)
		}
		if version != 8 {
			return ErrSchema
		}
		if err := verifySchema(ctx, c); err != nil {
			return err
		}
		if err := verifyProbeSchema(ctx, c, version); err != nil {
			return err
		}
		for _, ddl := range probeSchema {
			if _, err := c.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, "PRAGMA user_version=9")
		return err
	})
}

func readProbeIdentity(ctx context.Context, c sqlWriter, query string, args ...any) (ProbeIdentity, int64, error) {
	var p ProbeIdentity
	var holds int64
	err := c.QueryRowContext(ctx, "SELECT probe_id,uuid,node_id,installation_id,inbound_id,event_id,holds FROM pro_probe_accounts WHERE "+query, args...).Scan(&p.ID, &p.UUID, &p.NodeID, &p.InstallationID, &p.InboundID, &p.EventID, &holds)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrMissing
	}
	return p, holds, err
}

// ProbeIdentityTx returns private credential material only to trusted rendering
// and authority code. UUID is omitted from every JSON serialization.
func ProbeIdentityTx(ctx context.Context, tx *sql.Tx, node, installation string, inbound uint64) (ProbeIdentity, error) {
	if tx == nil {
		return ProbeIdentity{}, ErrStorage
	}
	enabled, err := probeAccountingEnabled(ctx, tx)
	if err != nil {
		return ProbeIdentity{}, err
	}
	if !enabled {
		return ProbeIdentity{}, ErrMissing
	}
	p, holds, err := readProbeIdentity(ctx, tx, "node_id=? AND installation_id=? AND inbound_id=?", node, installation, inbound)
	if err == nil && holds != 0 {
		return ProbeIdentity{}, ErrReconciliation
	}
	return p, err
}

func (l *Ledger) CreateProbeIdentityTx(ctx context.Context, tx *sql.Tx, p ProbeIdentity) error {
	if tx == nil {
		return ErrStorage
	}
	if !idRE.MatchString(p.ID) || !probeUUID.MatchString(p.UUID) || !idRE.MatchString(p.NodeID) || !probeInstall.MatchString(p.InstallationID) || p.InboundID == 0 || p.InboundID > 1<<53-1 || !eventRE.MatchString(p.EventID) {
		return uc.ErrInvalid
	}
	enabled, err := probeAccountingEnabled(ctx, tx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrSchema
	}
	prior, _, err := readProbeIdentity(ctx, tx, "probe_id=? OR event_id=? OR (node_id=? AND installation_id=? AND inbound_id=?)", p.ID, p.EventID, p.NodeID, p.InstallationID, p.InboundID)
	if err == nil {
		if prior == p {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, ErrMissing) {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO pro_probe_accounts(probe_id,uuid,node_id,installation_id,inbound_id,event_id,created_unix) VALUES(?,?,?,?,?,?,?)", p.ID, p.UUID, p.NodeID, p.InstallationID, p.InboundID, p.EventID, time.Now().Unix()); err != nil {
		return err
	}
	return invalidate(ctx, tx)
}

func probeBinding(ctx context.Context, c sqlWriter, e uc.Epoch, b uc.Binding, enabled bool) (bool, int64, error) {
	if !enabled {
		return false, 0, nil
	}
	p, holds, err := readProbeIdentity(ctx, c, "probe_id=?", b.ClientID)
	if errors.Is(err, ErrMissing) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if b.Billable || p.NodeID != e.NodeID || p.InstallationID != e.InstallationID {
		return false, 0, ErrAuthority
	}
	return true, holds, nil
}

func recordProbeDeltas(ctx context.Context, c sqlWriter, e uc.Epoch, previous uc.Cursor, r uc.Report, enabled bool) error {
	if !enabled {
		return nil
	}
	for i, b := range e.Bindings {
		if b.Billable {
			continue
		}
		found, _, err := probeBinding(ctx, c, e, b, enabled)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		up, down := r.Users[i].Uplink, r.Users[i].Downlink
		if previous.Sequence != 0 {
			up -= previous.Users[i].Uplink
			down -= previous.Users[i].Downlink
		}
		var oldUp, oldDown int64
		if err = c.QueryRowContext(ctx, "SELECT uplink,downlink FROM pro_probe_accounts WHERE probe_id=?", b.ClientID).Scan(&oldUp, &oldDown); err != nil {
			return err
		}
		newUp, err := checkedAdd(oldUp, up)
		if err != nil {
			return err
		}
		newDown, err := checkedAdd(oldDown, down)
		if err != nil {
			return err
		}
		if _, err = checkedAdd(newUp, newDown); err != nil {
			return err
		}
		if _, err = c.ExecContext(ctx, "UPDATE pro_probe_accounts SET uplink=?,downlink=? WHERE probe_id=?", newUp, newDown, b.ClientID); err != nil {
			return err
		}
		if _, err = c.ExecContext(ctx, "INSERT INTO pro_probe_deltas(epoch_id,sequence,probe_id,uplink,downlink) VALUES(?,?,?,?,?)", e.ID, strconv.FormatUint(r.Sequence, 10), b.ClientID, up, down); err != nil {
			return err
		}
	}
	return nil
}

func (l *Ledger) ProbeUsage(ctx context.Context, id string) (ProbeAccount, error) {
	var a ProbeAccount
	err := l.read(ctx, func(c *ledgerConn) error {
		enabled, err := probeAccountingEnabled(ctx, c)
		if err != nil {
			return err
		}
		if !enabled {
			return ErrMissing
		}
		return c.QueryRowContext(ctx, "SELECT probe_id,uplink,downlink,holds FROM pro_probe_accounts WHERE probe_id=?", id).Scan(&a.ID, &a.Uplink, &a.Downlink, &a.Holds)
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrMissing
	}
	if err != nil {
		return ProbeAccount{}, err
	}
	var e error
	a.Total, e = checkedAdd(a.Uplink, a.Downlink)
	return a, e
}

func holdProbe(ctx context.Context, c sqlWriter, e uc.Epoch, b uc.Binding, reason string, enabled bool) error {
	found, holds, err := probeBinding(ctx, c, e, b, enabled)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if _, err = checkedAdd(holds, 1); err != nil {
		return err
	}
	if _, err = c.ExecContext(ctx, "INSERT INTO pro_probe_holds(epoch_id,probe_id,reason,created_unix) VALUES(?,?,?,?)", e.ID, b.ClientID, reason, time.Now().Unix()); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "UPDATE pro_probe_accounts SET holds=holds+1 WHERE probe_id=?", b.ClientID)
	return err
}
