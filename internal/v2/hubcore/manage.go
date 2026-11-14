package hubcore

import (
	"context"
	"database/sql"
	"errors"
)

const manageSchema = `
CREATE TABLE IF NOT EXISTS client_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS node_history (
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  bucket INTEGER NOT NULL,
  cpu REAL NOT NULL, ram_pct REAL NOT NULL, rx_rate REAL NOT NULL, tx_rate REAL NOT NULL, online INTEGER NOT NULL,
  PRIMARY KEY(node_id, bucket)
);
CREATE TABLE IF NOT EXISTS client_group_members (
  group_id TEXT NOT NULL REFERENCES client_groups(id) ON DELETE CASCADE,
  email TEXT NOT NULL REFERENCES clients(email) ON DELETE CASCADE,
  PRIMARY KEY(group_id, email)
);
`

func (s *Store) ensureManageSchema() error {
	if _, err := s.db.Exec(manageSchema); err != nil {
		return err
	}
	if err := s.addColumn("nodes", "host_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.addColumn("clients", "note", "TEXT NOT NULL DEFAULT ''")
}

// RenameNode changes a node's display name.
func (s *Store) RenameNode(ctx context.Context, id, name string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE nodes SET name=? WHERE id=?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// DeleteNode removes a node and its inbounds. Clients and their usage stay;
// they simply lose the inbounds that lived on that node.
func (s *Store) DeleteNode(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// SetClientNote stores free-form contact details (phone, Telegram, notes).
func (s *Store) SetClientNote(ctx context.Context, email, note string) error {
	if len(note) > 2000 {
		return ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET note=? WHERE email=?`, note, email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// BulkRequest extends many clients at once. AddDays extends a fixed expiry
// (or an unstarted first-use term); AddBytes raises volume plans.
type BulkRequest struct {
	Emails   []string
	Group    string
	AddDays  int64
	AddBytes int64
	Enable   *bool
}

// BulkExtend applies a renewal to every selected client in one transaction
// and returns how many changed. Owner only.
func (s *Store) BulkExtend(ctx context.Context, req BulkRequest) (int, error) {
	if req.AddDays < 0 || req.AddBytes < 0 {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	emails := append([]string(nil), req.Emails...)
	if req.Group != "" {
		rows, err := tx.QueryContext(ctx, `SELECT email FROM client_group_members WHERE group_id=?`, req.Group)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var e string
			if err := rows.Scan(&e); err != nil {
				rows.Close()
				return 0, err
			}
			emails = append(emails, e)
		}
		rows.Close()
	}
	now := s.now().Unix()
	seen := map[string]bool{}
	changed := 0
	for _, e := range emails {
		if seen[e] {
			continue
		}
		seen[e] = true
		var exp, dur, vol int64
		if err := tx.QueryRowContext(ctx, `SELECT expires_unix,duration_seconds,volume_bytes FROM clients WHERE email=?`, e).Scan(&exp, &dur, &vol); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return 0, err
		}
		add := req.AddDays * 86400
		switch {
		case add == 0:
		case exp > 0 && exp > now:
			exp += add
		case exp > 0:
			exp = now + add // already expired: renew from today
		default:
			dur += add // first-use term not started (or no expiry)
		}
		if vol >= 0 {
			vol += req.AddBytes
		}
		q := `UPDATE clients SET expires_unix=?, duration_seconds=?, volume_bytes=? WHERE email=?`
		args := []any{exp, dur, vol, e}
		if req.Enable != nil {
			q = `UPDATE clients SET expires_unix=?, duration_seconds=?, volume_bytes=?, enabled=? WHERE email=?`
			args = []any{exp, dur, vol, *req.Enable, e}
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return 0, err
		}
		changed++
	}
	return changed, tx.Commit()
}

// TransferClient moves a client to another representative ("" = owner).
// Credit is not moved automatically; adjust it explicitly if needed.
func (s *Store) TransferClient(ctx context.Context, email, toRep string) error {
	if toRep != "" {
		var n int
		if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, toRep).Scan(&n); err != nil || n != 1 {
			return ErrNotFound
		}
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET rep_id=? WHERE email=?`, toRep, email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// Group is a named set of clients.
type Group struct {
	ID      string
	Name    string
	Members []string
}

func (s *Store) PutGroup(ctx context.Context, g Group) error {
	if !idRE.MatchString(g.ID) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO client_groups(id,name,created_unix) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name`,
		g.ID, g.Name, s.now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM client_group_members WHERE group_id=?`, g.ID); err != nil {
		return err
	}
	for _, e := range g.Members {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO client_group_members(group_id,email) SELECT ?, email FROM clients WHERE email=?`, g.ID, e); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM client_groups WHERE id=?`, id)
	return err
}

func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT g.id,g.name,COALESCE(m.email,'') FROM client_groups g
LEFT JOIN client_group_members m ON m.group_id=g.id ORDER BY g.id, m.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var id, name, email string
		if err := rows.Scan(&id, &name, &email); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, Group{ID: id, Name: name, Members: []string{}})
		}
		if email != "" {
			out[len(out)-1].Members = append(out[len(out)-1].Members, email)
		}
	}
	return out, rows.Err()
}
