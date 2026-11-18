package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Renewing starts a new period, the way resellers expect from the old
// panel: whoever renews has usually run out of volume, so the term starts
// again today (or at the first connection, for a service never used), usage
// goes back to zero and the volume becomes the new package. Nothing is added
// on top of what was left. A reseller pays for the new package; volume left
// unused from the old one goes back to their credit, so they never pay twice.

// RenewResult is what a renewal charged and gave back.
type RenewResult struct {
	ChargedBytes int64 `json:"charged_bytes"`
	RefundBytes  int64 `json:"refund_bytes"`
	ChargedUnits int64 `json:"charged_units"`
}

// RenewClient renews a client for the caller (owner, admin or reseller).
// plan is "volume" or "unlimited", or "" to keep the client's own.
func (s *Store) RenewClient(ctx context.Context, p Principal, email, plan string, gb float64, days int64) (RenewResult, error) {
	repID := ""
	if p.Role == "rep" {
		repID = p.RepID
		if err := s.subSellerPlan(ctx, repID, plan, int64(gb*(1<<30)), days, -1, true); err != nil {
			return RenewResult{}, err
		}
	}
	return s.renewPeriod(ctx, p.User, repID, email, plan, int64(gb*(1<<30)), days, true)
}

// renewPeriod does the work. repID "" renews without charging anyone.
// firstUse lets a service that was never used start at its first connection.
func (s *Store) renewPeriod(ctx context.Context, actor, repID, email, plan string, volBytes, days int64, firstUse bool) (RenewResult, error) {
	var res RenewResult
	if days < 0 || days > 3650 || volBytes < 0 {
		return res, ErrInvalid
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	var owner, oldPlan string
	var old Client
	err = tx.QueryRowContext(ctx, `SELECT rep_id,plan,volume_bytes,expires_unix,duration_seconds,max_ips,enabled FROM clients WHERE email=?`, email).
		Scan(&owner, &oldPlan, &old.VolumeBytes, &old.ExpiresUnix, &old.DurationSeconds, &old.MaxIPs, &old.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return res, ErrNotFound
	}
	if err != nil {
		return res, err
	}
	if repID != "" && owner != repID {
		return res, ErrNotFound // another seller's (or the owner's) client is invisible
	}
	var used, firstSeen int64
	_ = tx.QueryRowContext(ctx, `SELECT up+down,first_seen_unix FROM client_usage WHERE email=?`, email).Scan(&used, &firstSeen)

	if plan == "" {
		plan = "volume"
		if old.VolumeBytes < 0 || oldPlan == "unlimited" {
			plan = "unlimited"
		}
	}
	if plan != "volume" && plan != "unlimited" {
		return res, ErrInvalid
	}
	c := Client{Email: email, Enabled: true, MaxIPs: old.MaxIPs, VolumeBytes: -1}
	if plan == "volume" {
		if volBytes <= 0 {
			return res, fmt.Errorf("%w: a volume plan needs a volume", ErrInvalid)
		}
		c.VolumeBytes = volBytes
	}
	// Only a first-connection service still waiting for its first use keeps
	// waiting; anything else starts today.
	never := firstSeen == 0 && used == 0 && old.ExpiresUnix == 0 && old.DurationSeconds > 0
	if days > 0 {
		if firstUse && never {
			c.DurationSeconds = days * 86400
		} else {
			c.ExpiresUnix = now + days*86400
		}
	}

	if repID != "" {
		var enabled bool
		if err := tx.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, repID).Scan(&enabled); err != nil || !enabled {
			return res, ErrAuth
		}
		if days == 0 {
			return res, fmt.Errorf("%w: representatives sell a fixed term", ErrInvalid)
		}
		pol, err := scanPolicy(tx.QueryRowContext(ctx, `SELECT `+policyCols+` FROM reps WHERE id=?`, repID))
		if err != nil {
			return res, err
		}
		if pol.DenyRenew {
			return res, fmt.Errorf("%w: renewing clients is not allowed for this representative", ErrPolicy)
		}
		if err := checkPolicy(ctx, tx, repID, c, plan, false, now); err != nil {
			return res, err
		}
		if plan == "volume" {
			res.ChargedBytes = c.VolumeBytes
		} else {
			res.ChargedUnits = unitsFor(days)
		}
		// Volume left unused from the old package goes back (the expiry job
		// has already returned it for services that ran out of time).
		if oldPlan != "unlimited" && old.VolumeBytes > 0 && old.VolumeBytes > used {
			res.RefundBytes = old.VolumeBytes - used
		}
		net := res.ChargedBytes - res.RefundBytes
		r, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit-?, unlimited_units=unlimited_units-?
WHERE id=? AND volume_credit>=? AND unlimited_units>=?`, net, res.ChargedUnits, repID, max(net, 0), res.ChargedUnits)
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return res, ErrCredit
		}
		if res.RefundBytes > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,units_delta,note) VALUES(?,?,?,?,?,?,?,?)`,
				now, repID, actor, "refund", email, res.RefundBytes, 0, "renew: unused volume"); err != nil {
				return res, err
			}
		}
		if res.ChargedBytes > 0 || res.ChargedUnits > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,units_delta,note) VALUES(?,?,?,?,?,?,?,?)`,
				now, repID, actor, "charge", email, -res.ChargedBytes, -res.ChargedUnits, "renew"); err != nil {
				return res, err
			}
		}
	}

	// New period: usage starts from zero (kept in the reset log and in the
	// reseller's lifetime total) and the term and volume are replaced.
	if err := keepRepUsage(ctx, tx, email); err != nil {
		return res, err
	}
	// Every renewal is logged (with zero use too): reports count renewals here.
	var up, down int64
	_ = tx.QueryRowContext(ctx, `SELECT up,down FROM client_usage WHERE email=?`, email).Scan(&up, &down)
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_resets(email,unix,up,down,reason) VALUES(?,?,?,?,?)`, email, now, up, down, "renew"); err != nil {
		return res, err
	}
	// A renewed trial is a paying client from here on.
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET trial=0, note=CASE WHEN note='free trial' THEN '' ELSE note END WHERE email=? AND trial=1`, email); err != nil {
		return res, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE client_usage SET up=0, down=0 WHERE email=?`, email); err != nil {
		return res, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET enabled=1, volume_bytes=?, expires_unix=?, duration_seconds=?, plan=?, last_reset_unix=?,
start_by_unix=CASE WHEN ? THEN start_by_unix ELSE 0 END WHERE email=?`,
		c.VolumeBytes, c.ExpiresUnix, c.DurationSeconds, plan, now, c.DurationSeconds > 0, email); err != nil {
		return res, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, now, "", email, "renew"); err != nil {
		return res, err
	}
	return res, tx.Commit()
}
