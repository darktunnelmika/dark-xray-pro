package hubcore

import (
	"context"
	"fmt"
)

// DeleteRep removes a reseller and its sub-sellers: their logins, settings
// (bot, card, brand) and, when withClients is set, all their clients. Their
// products are archived so old orders still name them; the credit ledger and
// orders stay as history. Without withClients a reseller that still has
// clients is refused.
func (s *Store) DeleteRep(ctx context.Context, id string, withClients bool) (clients int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, id).Scan(&n); err != nil || n == 0 {
		return 0, ErrNotFound
	}
	ids := []string{id}
	for i := 0; i < len(ids) && i < 1000; i++ {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM reps WHERE parent_id=?`, ids[i])
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil {
				ids = append(ids, c)
			}
		}
		rows.Close()
	}
	var emails []string
	for _, r := range ids {
		rows, err := tx.QueryContext(ctx, `SELECT email FROM clients WHERE rep_id=?`, r)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var e string
			if rows.Scan(&e) == nil {
				emails = append(emails, e)
			}
		}
		rows.Close()
	}
	if len(emails) > 0 && !withClients {
		return len(emails), fmt.Errorf("%w: the reseller still has %d clients", ErrInvalid, len(emails))
	}
	for _, e := range emails {
		if _, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE email=?`, e); err != nil {
			return 0, err
		}
		if err := forgetClient(ctx, tx, e); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM client_group_members WHERE email=?`, e); err != nil {
			return 0, err
		}
	}
	for _, r := range ids {
		for _, q := range []string{
			`DELETE FROM admins WHERE rep_id=? AND role='rep'`,
			`UPDATE products SET status='archived' WHERE seller=?`,
			`DELETE FROM settings WHERE key IN ('rep_bot:'||?1,'pay_card_number:'||?1,'pay_card_holder:'||?1,'pay_card_bank:'||?1,'rep_brand:'||?1)`,
			`DELETE FROM reps WHERE id=?`,
		} {
			if _, err := tx.ExecContext(ctx, q, r); err != nil {
				return 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,note) VALUES(?,?,?,?,?)`, s.now().Unix(), r, "owner", "delete", "reseller deleted"); err != nil {
			return 0, err
		}
	}
	return len(emails), tx.Commit()
}
