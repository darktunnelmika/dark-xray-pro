package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BulkCreate makes many clients of one plan at once (e.g. shop-1 … shop-50).
type BulkCreate struct {
	Prefix     string   `json:"prefix"`
	Start      int      `json:"start"`
	Count      int      `json:"count"`
	Pad        int      `json:"pad"` // zero-pad numbers to this width
	Suffix     string   `json:"suffix"`
	Volume     int64    `json:"volume_bytes"` // -1 = unlimited
	Days       int64    `json:"days"`
	FirstUse   bool     `json:"first_use"`    // the term starts at the first connection
	OnHoldDays int64    `json:"on_hold_days"` // with FirstUse: start anyway after this many days
	MaxIPs     int      `json:"max_ips"`
	Inbounds   []string `json:"inbounds"`
	Group      string   `json:"group"`
	Note       string   `json:"note"`
}

// BulkCreateClients creates the clients in one transaction (all or none)
// and returns their emails. Existing names are refused, never overwritten.
func (s *Store) BulkCreateClients(ctx context.Context, b BulkCreate) ([]string, error) {
	if b.Count < 1 || b.Count > 1000 || b.Start < 0 || b.Pad < 0 || b.Pad > 8 || b.Days < 0 || b.Days > 3650 || b.OnHoldDays < 0 || b.OnHoldDays > 365 {
		return nil, fmt.Errorf("%w: count 1-1000, days 0-3650, on-hold 0-365", ErrInvalid)
	}
	if b.Volume < -1 {
		return nil, ErrInvalid
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	emails := make([]string, 0, b.Count)
	for i := 0; i < b.Count; i++ {
		n := strconv.Itoa(b.Start + i)
		if len(n) < b.Pad {
			n = strings.Repeat("0", b.Pad-len(n)) + n
		}
		email := b.Prefix + n + b.Suffix
		if !emailRE.MatchString(email) {
			return nil, fmt.Errorf("%w: %q is not a valid client name", ErrInvalid, email)
		}
		var x string
		if err := tx.QueryRowContext(ctx, `SELECT email FROM clients WHERE email=? COLLATE NOCASE`, email).Scan(&x); err == nil {
			return nil, fmt.Errorf("%w: %s already exists", ErrInvalid, email)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		c := Client{Email: email, Enabled: true, VolumeBytes: b.Volume, MaxIPs: b.MaxIPs, Inbounds: b.Inbounds}
		if b.Days > 0 {
			if b.FirstUse {
				c.DurationSeconds = b.Days * 86400
			} else {
				c.ExpiresUnix = now + b.Days*86400
			}
		}
		if err := putClientTx(ctx, tx, c, now); err != nil {
			return nil, fmt.Errorf("%s: %w", email, err)
		}
		if b.FirstUse && b.OnHoldDays > 0 && b.Days > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE clients SET start_by_unix=? WHERE email=?`, now+b.OnHoldDays*86400, email); err != nil {
				return nil, err
			}
		}
		if b.Note != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE clients SET note=? WHERE email=?`, b.Note, email); err != nil {
				return nil, err
			}
		}
		if b.Group != "" {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO client_group_members(group_id,email) SELECT id, ? FROM client_groups WHERE id=?`, email, b.Group); err != nil {
				return nil, err
			}
		}
		emails = append(emails, email)
	}
	return emails, tx.Commit()
}

// SetOnHold sets how long an unused first-connection client waits before
// its term starts anyway (0 days = wait for the first connection forever).
func (s *Store) SetOnHold(ctx context.Context, email string, days int64) error {
	if days < 0 || days > 365 {
		return ErrInvalid
	}
	var by int64
	if days > 0 {
		by = s.now().Unix() + days*86400
	}
	res, err := s.db.ExecContext(ctx, `UPDATE clients SET start_by_unix=? WHERE email=?`, by, email)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// runOnHold starts the term of first-connection clients whose on-hold
// window ran out without a connection: the term counts from start_by.
func (s *Store) runOnHold(ctx context.Context) int {
	now := s.now().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE clients SET expires_unix=start_by_unix+duration_seconds, start_by_unix=0
WHERE start_by_unix>0 AND start_by_unix<=? AND expires_unix=0 AND duration_seconds>0
AND NOT EXISTS(SELECT 1 FROM client_usage u WHERE u.email=clients.email AND u.first_seen_unix>0)`, now)
	if err != nil {
		return 0
	}
	// Clients that connected in time no longer need the deadline.
	_, _ = s.db.ExecContext(ctx, `UPDATE clients SET start_by_unix=0 WHERE start_by_unix>0
AND EXISTS(SELECT 1 FROM client_usage u WHERE u.email=clients.email AND u.first_seen_unix>0)`)
	n, _ := res.RowsAffected()
	return int(n)
}
