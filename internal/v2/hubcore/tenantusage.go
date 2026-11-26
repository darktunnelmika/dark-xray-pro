package hubcore

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
)

// Use a shop's clients had elsewhere.
//
// A shop moving its customers here from another panel brings what each one
// already used (used_bytes on create), and for the days its customers' apps
// still reach the old servers it adds the use those servers report
// (POST /bulk/add-usage). The use counts against the volume like use here
// and shows in the subscription's userinfo; it is not in the per-node or
// per-day history, which keep what this panel's nodes carried.

// maxUsageAdd caps one addition (1 PB): a typo must not be a client's volume
// a thousand times over.
const maxUsageAdd = int64(1) << 50

// addTenantUsage adds downloaded bytes to a client's total; rev is the
// revision to stamp (0 = a new one).
func addTenantUsage(ctx context.Context, tx *sql.Tx, email string, n, rev int64) error {
	if rev == 0 {
		if err := tx.QueryRowContext(ctx, `UPDATE rev_seq SET v=v+1 WHERE id=1 RETURNING v`).Scan(&rev); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix,rev,ext_bytes) VALUES(?,0,?,0,0,?,?)
ON CONFLICT(email) DO UPDATE SET down=down+excluded.down, rev=excluded.rev, ext_bytes=ext_bytes+excluded.ext_bytes`, email, n, rev, n)
	return err
}

// TenantUsageAdd is one item of /bulk/add-usage.
type TenantUsageAdd struct {
	Name     string `json:"name"`
	AddBytes int64  `json:"add_bytes"`
}

// TenantBulkAddUsage adds use to up to 500 of the shop's clients.
func (s *Store) TenantBulkAddUsage(ctx context.Context, tenant string, items []TenantUsageAdd, atomic bool) ([]TenantBulkResult, bool, error) {
	names := make([]string, len(items))
	for i := range items {
		names[i] = items[i].Name
	}
	return s.tenantBulk(ctx, tenant, names, atomic, func(tx *sql.Tx, t Tenant, i int) error {
		it := items[i]
		if it.AddBytes < 0 || it.AddBytes > maxUsageAdd {
			return fmt.Errorf("%w: add_bytes is 0 to 2^50", ErrInvalid)
		}
		c, err := s.tenantOwnTx(ctx, tx, t.ID, it.Name)
		if err != nil {
			return err
		}
		if it.AddBytes == 0 {
			return nil
		}
		return addTenantUsage(ctx, tx, c.Email, it.AddBytes, 0)
	})
}

func tenantUsageRoutes(s *Store, mux *docMux, auth func(func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc) {
	mux.HandleFunc("POST "+tenantAPI+"/bulk/add-usage", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in struct {
			Clients []TenantUsageAdd `json:"clients"`
			Atomic  bool             `json:"atomic"`
		}
		if !tenantDecode(w, r, 1<<20, &in) {
			return
		}
		res, committed, err := s.TenantBulkAddUsage(r.Context(), tenant, in.Clients, in.Atomic)
		if err != nil {
			tenantFail(w, err)
			return
		}
		ok := 0
		for i := range res {
			if res[i].OK {
				ok++
			}
		}
		code := http.StatusOK
		if !committed {
			code = http.StatusUnprocessableEntity
		}
		tenantJSON(w, code, map[string]any{"committed": committed, "ok": ok, "failed": len(res) - ok, "results": res})
	}))
}

// TenantNodeTraffic is NodeTraffic for one connected shop's clients. The
// history is kept per client, so a deleted client's days leave with it.
func (s *Store) TenantNodeTraffic(ctx context.Context, tenant string, days int) ([]NodeDay, error) {
	if _, err := s.TenantByID(ctx, tenant); err != nil {
		return nil, err
	}
	if days <= 0 || days > 400 {
		days = 30
	}
	cut := s.now().AddDate(0, 0, -days+1).UTC().Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT u.day,u.node_id,SUM(u.up),SUM(u.down) FROM clients c JOIN usage_daily u ON u.email=c.email
WHERE c.rep_id=? AND u.day>=? GROUP BY u.day,u.node_id ORDER BY u.day,u.node_id`, "~"+tenant, cut)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeDay{}
	for rows.Next() {
		var d NodeDay
		if err := rows.Scan(&d.Day, &d.NodeID, &d.Up, &d.Down); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
