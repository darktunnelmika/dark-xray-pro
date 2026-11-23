package hubcore

import (
	"context"
	"log"
)

// expiryRefundGrace leaves a little time after expiry for an automatic or
// last-minute renewal before the unused volume goes back.
const expiryRefundGrace = 10 * 60

// runExpiryRefunds gives representatives back the unused volume of their
// volume-plan clients that have expired. The client's volume is lowered to
// what it used, so the same volume can never be refunded twice, and a later
// renewal has to add (and pay for) new volume. It returns how many clients
// were refunded.
func (s *Store) runExpiryRefunds(ctx context.Context) int {
	now := s.now().Unix()
	rows, err := s.ro.QueryContext(ctx, `SELECT c.email,c.rep_id,c.volume_bytes,COALESCE(u.up+u.down,0)+c.gift_bytes
FROM clients c LEFT JOIN client_usage u ON u.email=c.email
WHERE c.rep_id<>'' AND c.plan='volume' AND c.volume_bytes>0 AND c.expires_unix>0 AND c.expires_unix<=? AND c.next_plan=''`, now-expiryRefundGrace)
	if err != nil {
		return 0
	}
	type due struct {
		email, rep string
		vol, used  int64
	}
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.email, &d.rep, &d.vol, &d.used) == nil {
			d.used = max(d.used, 1) // a volume of 0 would read as "no limit" in places
		}
		if d.vol > d.used {
			list = append(list, d)
		}
	}
	rows.Close()
	n := 0
	for _, d := range list {
		if err := s.expiryRefund(ctx, d.email, d.rep, d.vol, d.used, now); err != nil {
			log.Printf("expiry refund %s: %v", d.email, err)
			continue
		}
		n++
	}
	return n
}

func (s *Store) expiryRefund(ctx context.Context, email, repID string, vol, used, now int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Only if nothing changed since the scan (a renewal in between wins).
	r, err := tx.ExecContext(ctx, `UPDATE clients SET volume_bytes=? WHERE email=? AND rep_id=? AND plan='volume' AND volume_bytes=? AND expires_unix>0 AND expires_unix<=? AND next_plan=''`,
		used, email, repID, vol, now-expiryRefundGrace)
	if err != nil {
		return err
	}
	if k, _ := r.RowsAffected(); k != 1 {
		return nil
	}
	back := vol - used
	if _, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=volume_credit+? WHERE id=?`, back, repID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,volume_delta,note) VALUES(?,?,?,?,?,?,?)`,
		now, repID, "system", "refund", email, back, "expired: unused volume"); err != nil {
		return err
	}
	return tx.Commit()
}
