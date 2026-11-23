package hubcore

import (
	"context"
	"fmt"
)

// Everyday bulk actions: zero the usage of every client on an inbound,
// clean out expired or used-up clients, and copy a client.

// ResetInboundUsage zeroes the usage of every client on an inbound and
// returns how many were reset.
func (s *Store) ResetInboundUsage(ctx context.Context, tag string) (int, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT email FROM client_inbounds WHERE tag=?`, tag)
	if err != nil {
		return 0, err
	}
	var emails []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			emails = append(emails, e)
		}
	}
	rows.Close()
	n := 0
	for _, e := range emails {
		if err := s.ResetUsage(ctx, e, "inbound:"+tag); err == nil {
			n++
		}
	}
	return n, nil
}

// CleanupRequest picks clients to delete.
type CleanupRequest struct {
	Expired  bool    `json:"expired"`  // the term ended
	UsedUp   bool    `json:"used_up"`  // no volume left
	Disabled bool    `json:"disabled"` // switched off by hand
	MinDays  int64   `json:"min_days"` // ...at least this many days ago (expired) / not seen for this long (others)
	DryRun   bool    `json:"dry_run"`  // only count and list
	RepID    *string `json:"rep_id"`   // limit to one seller ("" = owner's own)
}

// CleanupClients deletes (or, with DryRun, lists) the matching clients.
func (s *Store) CleanupClients(ctx context.Context, r CleanupRequest) ([]string, error) {
	if !r.Expired && !r.UsedUp && !r.Disabled {
		return nil, fmt.Errorf("%w: choose what to clean", ErrInvalid)
	}
	if r.MinDays < 0 || r.MinDays > 3650 {
		return nil, ErrInvalid
	}
	all, err := s.Clients(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	cut := now - r.MinDays*86400
	var hit []string
	for _, c := range all {
		if r.RepID != nil && c.RepID != *r.RepID {
			continue
		}
		last := c.Usage.LastSeen
		if last == 0 {
			last = c.EffectiveUntil
		}
		quiet := r.MinDays == 0 || last <= cut
		switch {
		case r.Expired && c.EffectiveUntil > 0 && c.EffectiveUntil <= cut:
		case r.UsedUp && c.Remaining == 0 && c.VolumeBytes > 0 && quiet:
		case r.Disabled && !c.Enabled && quiet:
		default:
			continue
		}
		hit = append(hit, c.Email)
	}
	if r.DryRun {
		return hit, nil
	}
	var done []string
	for _, e := range hit {
		if err := s.DeleteClient(ctx, e); err == nil {
			done = append(done, e)
		}
	}
	return done, nil
}

// CloneClient makes a new client with the same plan, inbounds, seller,
// groups and settings, and its own credentials and link. A first-connection
// term starts fresh; usage starts at zero.
func (s *Store) CloneClient(ctx context.Context, from, to string) error {
	c, err := s.ClientByEmail(ctx, from)
	if err != nil {
		return err
	}
	var n int
	if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, to).Scan(&n) == nil && n > 0 {
		return fmt.Errorf("%w: %s already exists", ErrInvalid, to)
	}
	nc := c.Client
	nc.Email, nc.UUID, nc.Secret, nc.SubToken = to, "", "", ""
	if nc.DurationSeconds > 0 {
		nc.ExpiresUnix = 0
	}
	if err := s.PutClient(ctx, nc); err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE clients SET rep_id=(SELECT rep_id FROM clients WHERE email=?), plan=(SELECT plan FROM clients WHERE email=?),
note=(SELECT note FROM clients WHERE email=?), reset_period=(SELECT reset_period FROM clients WHERE email=?), reset_year=(SELECT reset_year FROM clients WHERE email=?),
hwid_limit=(SELECT hwid_limit FROM clients WHERE email=?) WHERE email=?`,
		from, from, from, from, from, from, to)
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO client_group_members(group_id,email) SELECT group_id,? FROM client_group_members WHERE email=?`, to, from)
	return nil
}
