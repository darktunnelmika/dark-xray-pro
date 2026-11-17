package hubcore

import (
	"context"
	"database/sql"
	"fmt"
)

// Renaming a client. The name is the client's key everywhere (usage, history,
// orders, groups, Telegram links, the nodes' configs), so a rename moves all
// of it in one transaction. Credentials and the subscription link stay as
// they are: the customer's apps keep working without a new link.

const renameSchema = `CREATE TABLE IF NOT EXISTS client_renames (
  old TEXT PRIMARY KEY,
  new TEXT NOT NULL,
  unix INTEGER NOT NULL
);`

func (s *Store) ensureRenameSchema() error {
	_, err := s.db.Exec(renameSchema)
	return err
}

// RenameClient gives the client a new name and carries everything stored
// under the old one over to it.
func (s *Store) RenameClient(ctx context.Context, old, name string) error {
	if !emailRE.MatchString(name) {
		return fmt.Errorf("%w: the name may use only English letters, digits and . _ - @ +", ErrInvalid)
	}
	if old == name {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, old).Scan(&n); err != nil || n != 1 {
		return ErrNotFound
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=? COLLATE NOCASE`, name).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("%w: client already exists", ErrInvalid)
	}
	// Child rows are moved after the client itself; the checks wait for the commit.
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
		return err
	}
	// Every table that names a client, including ones added later.
	rows, err := tx.QueryContext(ctx, `SELECT m.name, p.name FROM sqlite_master m JOIN pragma_table_info(m.name) p
WHERE m.type='table' AND p.name IN ('email','client_email') ORDER BY m.name`)
	if err != nil {
		return err
	}
	var cols [][2]string
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			rows.Close()
			return err
		}
		cols = append(cols, [2]string{t, c})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET email=? WHERE email=?`, name, old); err != nil {
		return err
	}
	for _, tc := range cols {
		// The credit ledger is append-only: it keeps the name the client had
		// at the time, and client_renames says who that is today.
		if tc[0] == "clients" || tc[0] == "credit_ledger" {
			continue
		}
		// A leftover row of a deleted client that once had this name gives way.
		if _, err := tx.ExecContext(ctx, `UPDATE OR REPLACE "`+tc[0]+`" SET "`+tc[1]+`"=? WHERE "`+tc[1]+`"=?`, name, old); err != nil {
			return fmt.Errorf("rename in %s: %w", tc[0], err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE OR REPLACE settings SET key=? WHERE key=?`, "remind:"+name, "remind:"+old); err != nil {
		return err
	}
	// Usage the nodes counted under the old name before they got the new
	// config still arrives for a while: remember where it belongs.
	now := s.now().Unix()
	for _, q := range []string{
		`DELETE FROM client_renames WHERE old=?2`,
		`UPDATE client_renames SET new=?2 WHERE new=?1`,
		`INSERT OR REPLACE INTO client_renames(old,new,unix) VALUES(?1,?2,?3)`,
	} {
		if _, err := tx.ExecContext(ctx, q, old, name, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ledgerClient is the SQL for the current name of a ledger row's client.
const ledgerClient = `COALESCE((SELECT new FROM client_renames WHERE old=l.client_email), l.client_email)`

// renamedNow maps old client names to their current ones for names that no
// live client uses, so late usage reports land on the renamed client.
func renamedNow(ctx context.Context, tx *sql.Tx) map[string]string {
	rows, err := tx.QueryContext(ctx, `SELECT r.old, r.new FROM client_renames r
WHERE NOT EXISTS (SELECT 1 FROM clients c WHERE c.email=r.old) AND EXISTS (SELECT 1 FROM clients c WHERE c.email=r.new)`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var m map[string]string
	for rows.Next() {
		var o, n string
		if rows.Scan(&o, &n) == nil {
			if m == nil {
				m = map[string]string{}
			}
			m[o] = n
		}
	}
	return m
}
