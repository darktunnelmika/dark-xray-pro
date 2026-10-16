package usageledger

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var idRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var eventRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$`)

type Principal struct{ NodeID, InstallationID string }

// Only trusted creation/import can seed. Missing/unknown is never guessed zero.
type Seed struct {
	ClientID string `json:"client_id"`
	EventID  string `json:"event_id"`
	Uplink   int64  `json:"uplink_bytes,string"`
	Downlink int64  `json:"downlink_bytes,string"`
	Complete bool   `json:"complete"`
	Reason   string `json:"reason"`
}
type Account struct {
	ClientID         string `json:"client_id"`
	RepresentativeID string `json:"representative_id"`
	Uplink           int64  `json:"uplink_bytes,string"`
	Downlink         int64  `json:"downlink_bytes,string"`
	Total            int64  `json:"total_bytes,string"`
	Limit            int64  `json:"limit_bytes,string"`
	Plan             string `json:"plan_kind"`
	Known            bool   `json:"known"`
	Holds            int64  `json:"reconciliation_holds"`
	Exhausted        bool   `json:"exhausted"`
	Eligible         bool   `json:"eligible"`
}

func validReason(reason string) bool {
	if !utf8.ValidString(reason) || strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 200 {
		return false
	}
	for _, r := range reason {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func checkedAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, uc.ErrOverflow
	}
	return a + b, nil
}
func eligible(ctx context.Context, c *sql.Conn, id string) (bool, error) {
	var n int
	e := c.QueryRowContext(ctx, "SELECT eligible FROM pro_usage_eligibility WHERE client_id=?", id).Scan(&n)
	if errors.Is(e, sql.ErrNoRows) {
		return false, ErrMissing
	}
	return n == 1, e
}
func (l *Ledger) InitializeAccount(ctx context.Context, s Seed) error {
	return l.write(ctx, func(c *sql.Conn) error { return initializeAccount(ctx, c, s) })
}
func (l *Ledger) InitializeAccountTx(ctx context.Context, tx *sql.Tx, s Seed) error {
	if tx == nil {
		return ErrStorage
	}
	return initializeAccount(ctx, tx, s)
}
func initializeAccount(ctx context.Context, c sqlWriter, s Seed) error {
	if !idRE.MatchString(s.ClientID) || !eventRE.MatchString(s.EventID) || !validReason(s.Reason) {
		return uc.ErrInvalid
	}
	if _, e := checkedAdd(s.Uplink, s.Downlink); e != nil {
		return e
	}
	body, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var existing []byte
	e := c.QueryRowContext(ctx, "SELECT body_json FROM pro_usage_seeds WHERE client_id=? OR event_id=?", s.ClientID, s.EventID).Scan(&existing)
	if e == nil {
		if bytes.Equal(body, existing) {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var n int
	if e = c.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients WHERE id=?", s.ClientID).Scan(&n); e != nil {
		return e
	}
	if n != 1 {
		return ErrMissing
	}
	if _, e = c.ExecContext(ctx, "INSERT INTO pro_usage_accounts(client_id,uplink,downlink,baseline_complete) VALUES(?,?,?,?)", s.ClientID, s.Uplink, s.Downlink, s.Complete); e != nil {
		return e
	}
	if _, e = c.ExecContext(ctx, "INSERT INTO pro_usage_seeds(client_id,event_id,body_json,created_unix) VALUES(?,?,?,?)", s.ClientID, s.EventID, body, time.Now().Unix()); e != nil {
		return e
	}
	return invalidate(ctx, c)
}

// Epoch registration is INTERNAL, before admitting traffic, with server-derived
// bindings and an actual supervised process birth. Never decode it from a report.
func (l *Ledger) RegisterEpoch(ctx context.Context, authority uc.Epoch) error {
	return l.write(ctx, func(c *sql.Conn) error { return registerEpoch(ctx, c, authority) })
}
func (l *Ledger) RegisterEpochTx(ctx context.Context, tx *sql.Tx, authority uc.Epoch) error {
	if tx == nil {
		return ErrStorage
	}
	return registerEpoch(ctx, tx, authority)
}
func registerEpoch(ctx context.Context, c sqlWriter, authority uc.Epoch) error {
	e, sha, err := uc.CanonicalEpoch(authority)
	if err != nil {
		return err
	}
	for _, b := range e.Bindings {
		email, err := uc.CounterEmail(b.ClientID)
		if err != nil || b.Email != email {
			return uc.ErrInvalid
		}
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}

	var stored []byte
	err = c.QueryRowContext(ctx, "SELECT epoch_json FROM pro_usage_epochs WHERE epoch_id=?", e.ID).Scan(&stored)
	if err == nil {
		if bytes.Equal(stored, raw) {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	for _, b := range e.Bindings {
		var n int
		if err = c.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_accounts WHERE client_id=?", b.ClientID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrMissing
		}
	}
	var duplicate int
	if err = c.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=? AND installation_id=? AND kernel_boot_id=? AND pid=? AND start_ticks=?", e.NodeID, e.InstallationID, e.KernelBootID, e.PID, strconv.FormatUint(e.ProcessStartTicks, 10)).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate != 0 {
		return ErrConflict
	}
	_, err = c.ExecContext(ctx, `INSERT INTO pro_usage_epochs(epoch_id,node_id,installation_id,kernel_boot_id,pid,start_ticks,config_sha,binding_sha,epoch_json,cursor_json,state,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,'open',?)`, e.ID, e.NodeID, e.InstallationID, e.KernelBootID, e.PID, strconv.FormatUint(e.ProcessStartTicks, 10), e.ConfigSHA, sha, raw, []byte(`{}`), time.Now().Unix())
	return err
}
func (l *Ledger) Accept(ctx context.Context, principal Principal, r uc.Report) (uc.Receipt, error) {
	var receipt uc.Receipt
	err := l.write(ctx, func(c *sql.Conn) error {
		var epochJSON, cursorJSON []byte
		var node, installation, binding, state string
		e := c.QueryRowContext(ctx, "SELECT node_id,installation_id,binding_sha,epoch_json,cursor_json,state FROM pro_usage_epochs WHERE epoch_id=?", r.EpochID).Scan(&node, &installation, &binding, &epochJSON, &cursorJSON, &state)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrMissing
		}
		if e != nil {
			return e
		}
		if node != principal.NodeID || installation != principal.InstallationID {
			return ErrAuthority
		}
		var epoch uc.Epoch
		var cursor uc.Cursor
		if json.Unmarshal(epochJSON, &epoch) != nil || json.Unmarshal(cursorJSON, &cursor) != nil {
			return ErrStorage
		}
		canonical, computed, e := uc.CanonicalEpoch(epoch)
		if e != nil || computed != binding {
			return ErrStorage
		}
		if e = uc.VerifyReport(canonical, r); e != nil {
			return e
		}
		var priorSHA string
		e = c.QueryRowContext(ctx, "SELECT sha256 FROM pro_usage_receipts WHERE epoch_id=? AND sequence=?", r.EpochID, strconv.FormatUint(r.Sequence, 10)).Scan(&priorSHA)
		if e == nil {
			if priorSHA != r.SHA {
				return uc.ErrConflict
			}
			receipt = uc.Receipt{EpochID: r.EpochID, Sequence: r.Sequence, SHA: r.SHA}
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		next, charges, duplicate, e := uc.Calculate(canonical, cursor, r)
		if e != nil {
			return e
		}
		if duplicate {
			return ErrStorage
		}
		if state == "sealed" {
			return uc.ErrSealed
		}
		seq := strconv.FormatUint(r.Sequence, 10)
		if _, e = c.ExecContext(ctx, "INSERT INTO pro_usage_receipts(epoch_id,sequence,sha256,quiesced,received_unix) VALUES(?,?,?,?,?)", r.EpochID, seq, r.SHA, r.Quiesced, time.Now().Unix()); e != nil {
			return e
		}
		dirty := false
		for _, charge := range charges {
			before, e := eligible(ctx, c, charge.ClientID)
			if e != nil {
				return e
			}
			var up, down int64
			if e = c.QueryRowContext(ctx, "SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?", charge.ClientID).Scan(&up, &down); e != nil {
				return e
			}
			up, e = checkedAdd(up, charge.Uplink)
			if e != nil {
				return e
			}
			down, e = checkedAdd(down, charge.Downlink)
			if e != nil {
				return e
			}
			if _, e = checkedAdd(up, down); e != nil {
				return e
			}
			if _, e = c.ExecContext(ctx, "UPDATE pro_usage_accounts SET uplink=?,downlink=? WHERE client_id=?", up, down, charge.ClientID); e != nil {
				return e
			}
			if _, e = c.ExecContext(ctx, "INSERT INTO pro_usage_deltas(epoch_id,sequence,client_id,uplink,downlink) VALUES(?,?,?,?,?)", r.EpochID, seq, charge.ClientID, charge.Uplink, charge.Downlink); e != nil {
				return e
			}
			after, e := eligible(ctx, c, charge.ClientID)
			if e != nil {
				return e
			}
			dirty = dirty || before != after
		}
		if l.checkpoint != nil {
			if e = l.checkpoint("after_totals"); e != nil {
				return e
			}
		}
		encoded, e := json.Marshal(next)
		if e != nil {
			return e
		}
		if r.Quiesced && state != "uncertain" {
			state = "sealed"
		}
		if _, e = c.ExecContext(ctx, "UPDATE pro_usage_epochs SET cursor_json=?,state=? WHERE epoch_id=?", encoded, state, r.EpochID); e != nil {
			return e
		}
		if dirty {
			if e = invalidate(ctx, c); e != nil {
				return e
			}
		}
		receipt = uc.Receipt{EpochID: r.EpochID, Sequence: r.Sequence, SHA: r.SHA}
		return nil
	})
	if err != nil {
		return uc.Receipt{}, err
	}
	return receipt, nil
}

// Unknown final bytes are held for reconciliation, never fabricated or forgiven.
// A later known sample adds bytes but cannot silently release an uncertain hold.
func (l *Ledger) MarkEpochUncertain(ctx context.Context, principal Principal, epochID, reason string) error {
	if !validReason(reason) {
		return uc.ErrInvalid
	}
	return l.write(ctx, func(c *sql.Conn) error {
		var node, installation, state string
		var raw []byte
		e := c.QueryRowContext(ctx, "SELECT node_id,installation_id,state,epoch_json FROM pro_usage_epochs WHERE epoch_id=?", epochID).Scan(&node, &installation, &state, &raw)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrMissing
		}
		if e != nil {
			return e
		}
		if node != principal.NodeID || installation != principal.InstallationID {
			return ErrAuthority
		}
		if state == "uncertain" {
			return nil
		}
		if state == "sealed" {
			return ErrConflict
		}
		var epoch uc.Epoch
		if json.Unmarshal(raw, &epoch) != nil {
			return ErrStorage
		}
		dirty := false
		for _, b := range epoch.Bindings {
			if !b.Billable {
				continue
			}
			before, e := eligible(ctx, c, b.ClientID)
			if e != nil {
				return e
			}
			if _, e = c.ExecContext(ctx, "INSERT INTO pro_usage_holds(epoch_id,client_id,reason,created_unix) VALUES(?,?,?,?)", epochID, b.ClientID, reason, time.Now().Unix()); e != nil {
				return e
			}
			if _, e = c.ExecContext(ctx, "UPDATE pro_usage_accounts SET holds=holds+1 WHERE client_id=? AND holds<9223372036854775807", b.ClientID); e != nil {
				return e
			}
			dirty = dirty || before
		}
		if _, e = c.ExecContext(ctx, "UPDATE pro_usage_epochs SET state='uncertain' WHERE epoch_id=?", epochID); e != nil {
			return e
		}
		if dirty {
			return invalidate(ctx, c)
		}
		return nil
	})
}
func (l *Ledger) Account(ctx context.Context, clientID string) (Account, error) {
	var a Account
	var complete, allowed int
	err := l.db.QueryRowContext(ctx, `SELECT a.client_id,c.representative_id,a.uplink,a.downlink,a.baseline_complete,a.holds,c.plan_kind,c.volume_bytes,v.eligible FROM pro_usage_accounts a JOIN clients c ON c.id=a.client_id JOIN pro_usage_eligibility v ON v.client_id=a.client_id WHERE a.client_id=?`, clientID).Scan(&a.ClientID, &a.RepresentativeID, &a.Uplink, &a.Downlink, &complete, &a.Holds, &a.Plan, &a.Limit, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrMissing
	}
	if err != nil {
		return a, err
	}
	a.Total, err = checkedAdd(a.Uplink, a.Downlink)
	if err != nil {
		return Account{}, err
	}
	a.Known = complete == 1 && a.Holds == 0
	a.Eligible = allowed == 1
	a.Exhausted = a.Plan == "volume" && a.Total >= a.Limit
	return a, nil
}
