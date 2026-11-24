package hubcore

import (
	"context"
	"database/sql"
)

// Traffic on this panel. A reseller that moved here from another panel
// brings its customers with the usage they had there, so its lifetime total
// (UsedBytes) mixes the old panel's traffic with ours. used_here counts only
// what its clients used on this panel's servers: every usage report adds
// to it as it is stored, so renewals, resets and deleted clients never
// shrink it.

func (s *Store) ensureUsedHereSchema() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('reps') WHERE name='used_here'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if err := s.addColumn("reps", "used_here", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// First start with the counter: rebuild it from the daily history kept
	// since the reseller was made here — its clients today and the ones it
	// deleted (the ledger keeps their names).
	_, err := s.db.Exec(`UPDATE reps SET used_here=COALESCE((SELECT SUM(d.up+d.down) FROM usage_daily d
WHERE d.day>=date(reps.created_unix,'unixepoch') AND (d.email IN (SELECT email FROM clients WHERE rep_id=reps.id)
OR d.email IN (SELECT client_email FROM credit_ledger WHERE rep_id=reps.id AND kind='delete' AND client_email<>''))),0)`)
	return err
}

// addUsedHere adds a client's newly reported traffic to its reseller.
func addUsedHere(ctx context.Context, st *sql.Stmt, email string, n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := st.ExecContext(ctx, n, email)
	return err
}

const usedHereSQL = `UPDATE reps SET used_here=used_here+? WHERE id=(SELECT rep_id FROM clients WHERE email=? AND rep_id<>'')`
