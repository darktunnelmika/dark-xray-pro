package hubcore

import (
	"context"
	"database/sql"
	"fmt"
)

// DeleteRep removes a reseller and its sub-sellers: their logins, settings
// (bot, card, brand) and, when withClients is set, all their clients. Their
// products are archived so old orders still name them; the credit ledger and
// orders stay as history. Without withClients a reseller that still has
// clients is refused. A sub-seller's unused credit goes back to its parent
// (it was the parent's), and a sold panel's unpaid bill is cancelled.
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
	if err := s.settleDeletedSeller(ctx, tx, id); err != nil {
		return 0, err
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
			`DELETE FROM pg_api WHERE seller=?`,
			`DELETE FROM pg_tokens WHERE seller=?`,
			`DELETE FROM rep_wallet WHERE rep_id=?`,
			`UPDATE rep_bills SET status='cancelled' WHERE rep_id=? AND status='open'`,
			`DELETE FROM rep_resale WHERE rep_id=?`,
			`DELETE FROM legacy_clients WHERE rep_id=?`,
			`DELETE FROM toman_wallet WHERE seller=?`,
			`DELETE FROM toman_topups WHERE seller=?`,
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

// settleDeletedSeller squares the accounts of a sub-seller being deleted: a
// seller the parent opened and funded hands its unused credit back to the
// parent; a panel the parent sold through its bot was paid for in toman, so
// its credit is not the parent's — only the parent's unpaid bill for it is
// cancelled, since the panel no longer exists.
func (s *Store) settleDeletedSeller(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE rep_bills SET status='cancelled', note=note||' — the panel was deleted'
WHERE status='open' AND order_id IN (SELECT id FROM rep_orders WHERE rep_id=? AND seller<>'' AND kind IN ('new','topup'))`, id); err != nil {
		return err
	}
	var parent string
	var vol, units int64
	if err := tx.QueryRowContext(ctx, `SELECT parent_id,volume_credit,unlimited_units FROM reps WHERE id=?`, id).Scan(&parent, &vol, &units); err != nil {
		return err
	}
	if parent == "" || (vol <= 0 && units <= 0) {
		return nil
	}
	var sold int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_orders WHERE rep_id=? AND seller=? AND kind='new' AND status='approved'`, id, parent).Scan(&sold)
	if sold > 0 {
		return nil
	}
	r, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+?, unlimited_units=unlimited_units+? WHERE id=?`, vol, units, parent)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return nil // the parent goes in the same delete
	}
	now := s.now().Unix()
	for _, row := range []struct {
		rep, note string
		v, u      int64
	}{{id, parent + " — seller deleted: unused credit returned", -vol, -units}, {parent, id + " — seller deleted: unused credit returned", vol, units}} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,?,?)`,
			now, row.rep, "owner", "transfer", row.v, row.u, row.note); err != nil {
			return err
		}
	}
	return nil
}
