package hubcore

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Client export/import keeps identity: UUID, secret and subscription token
// travel with the client, so imported users keep working with their old
// links. Usage totals and first/last seen are carried too. Import is one
// transaction: either every row applies or none does.

// ClientRecord is one exported client.
type ClientRecord struct {
	Client
	Plan  string `json:"plan"`
	RepID string `json:"rep_id"`
	Note  string `json:"note"`
	Usage Usage  `json:"usage"`
}

// ExportClients returns every client with credentials and usage. Owner only.
func (s *Store) ExportClients(ctx context.Context) ([]ClientRecord, error) {
	list, err := s.Clients(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ClientRecord, 0, len(list))
	for _, c := range list {
		var uuid, secret string
		if err := s.ro.QueryRowContext(ctx, `SELECT uuid,secret FROM clients WHERE email=?`, c.Email).Scan(&uuid, &secret); err != nil {
			return nil, err
		}
		cl := c.Client
		cl.UUID, cl.Secret = uuid, secret
		out = append(out, ClientRecord{Client: cl, Plan: c.Plan, RepID: c.RepID, Note: c.Note, Usage: c.Usage})
	}
	return out, nil
}

// ImportClients creates or replaces clients from an export. Unknown inbounds
// and representatives are refused before anything changes.
func (s *Store) ImportClients(ctx context.Context, recs []ClientRecord) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.now().Unix()
	for i, r := range recs {
		var x int
		for _, t := range r.Inbounds {
			if tx.QueryRowContext(ctx, `SELECT 1 FROM inbounds WHERE tag=?`, t).Scan(&x) != nil {
				return 0, fmt.Errorf("%w: row %d (%s): unknown inbound %q", ErrInvalid, i+1, r.Email, t)
			}
		}
		if r.RepID != "" && tx.QueryRowContext(ctx, `SELECT 1 FROM reps WHERE id=?`, r.RepID).Scan(&x) != nil {
			return 0, fmt.Errorf("%w: row %d (%s): unknown representative %q", ErrInvalid, i+1, r.Email, r.RepID)
		}
		if err := putClientTx(ctx, tx, r.Client, now); err != nil {
			return 0, fmt.Errorf("row %d (%s): %w", i+1, r.Email, err)
		}
		plan := r.Plan
		if plan != "unlimited" {
			plan = "volume"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=?, plan=?, note=? WHERE email=?`, r.RepID, plan, r.Note, r.Email); err != nil {
			return 0, err
		}
		if r.Usage.Up < 0 || r.Usage.Down < 0 {
			return 0, fmt.Errorf("%w: row %d: negative usage", ErrInvalid, i+1)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)
ON CONFLICT(email) DO UPDATE SET up=excluded.up, down=excluded.down, first_seen_unix=excluded.first_seen_unix, last_seen_unix=excluded.last_seen_unix`,
			r.Email, r.Usage.Up, r.Usage.Down, r.Usage.FirstSeen, r.Usage.LastSeen); err != nil {
			return 0, err
		}
	}
	return len(recs), tx.Commit()
}

// WriteClientsCSV writes a spreadsheet-friendly list (UTF-8 with BOM so
// Excel shows Persian names correctly). It carries no keys, only what an
// owner reads: volume, usage, expiry, devices and each subscription link.
func (s *Store) WriteClientsCSV(ctx context.Context, w io.Writer, base string) error {
	list, err := s.Clients(ctx)
	if err != nil {
		return err
	}
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"email", "enabled", "volume_gb", "used_gb", "remaining_gb", "expires", "days_left", "max_ips", "inbounds", "seller", "note", "last_seen", "subscription"})
	gb := func(b int64) string { return strconv.FormatFloat(float64(b)/(1<<30), 'f', 2, 64) }
	day := func(u int64) string {
		if u <= 0 {
			return ""
		}
		return time.Unix(u, 0).UTC().Format("2006-01-02")
	}
	now := s.now().Unix()
	for _, c := range list {
		used := c.Usage.Up + c.Usage.Down
		vol, rem := "unlimited", "unlimited"
		if c.VolumeBytes > 0 {
			vol, rem = gb(c.VolumeBytes), gb(max(0, c.VolumeBytes-used))
		}
		exp := EffectiveExpiry(c.Client, c.Usage)
		left := ""
		if exp > 0 {
			left = strconv.FormatInt(max(0, (exp-now+86399)/86400), 10)
		}
		sub := ""
		if c.SubToken != "" {
			sub = base + "/sub/" + c.SubToken
		}
		_ = cw.Write([]string{c.Email, strconv.FormatBool(c.Enabled), vol, gb(used), rem, day(exp), left, strconv.Itoa(c.MaxIPs),
			strings.Join(c.Inbounds, " "), c.RepID, c.Note, day(c.Usage.LastSeen), sub})
	}
	cw.Flush()
	return cw.Error()
}
