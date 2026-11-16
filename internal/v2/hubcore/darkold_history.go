package hubcore

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
)

// darkOldCredit finishes the moved resellers' credit: the expired clients'
// unused volume, a ledger row explaining the opening balance, the old
// panel's credit history (as notes, balances are already set) and the
// lifetime traffic of clients deleted in the old panel.
func (s *Store) darkOldCredit(ctx context.Context, tx *sql.Tx, old *sql.DB, fresh map[string]*DarkOldCredit, repOf map[string]string, now int64, rep *DarkOldReport) error {
	ids := make([]string, 0, len(fresh))
	for id := range fresh {
		ids = append(ids, id)
	}
	sortStrings(ids)
	for _, id := range ids {
		cr := fresh[id]
		cr.Credit += cr.Expired
		var inClients int64
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(u.up+u.down),0) FROM client_usage u JOIN clients c ON c.email=u.email WHERE c.rep_id=?`, id).Scan(&inClients)
		base := max(cr.Lifetime-inClients, 0)
		if _, err := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=?, used_base=? WHERE id=?`, cr.Credit, base, id); err != nil {
			return err
		}
		note := fmt.Sprintf("moved from the old panel: capacity %s, in use %s, unused volume of expired clients %s", gib(cr.Cap), gib(cr.InUse), gib(cr.Expired))
		if cr.OldUnits > 0 {
			note += fmt.Sprintf("; %d unlimited units left there were not moved", cr.OldUnits)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,note) VALUES(?,?,?,?,?,?)`,
			now, id, "import", "adjust", cr.Credit, note); err != nil {
			return err
		}
		rep.Credit = append(rep.Credit, *cr)
	}
	if !tableExists(ctx, old, "resource_credit_ledger") {
		return nil
	}
	rows, err := old.QueryContext(ctx, `SELECT owner,volume_bytes,unlimited_units,kind,reference,at FROM resource_credit_ledger ORDER BY at`)
	if err != nil {
		return err
	}
	type row struct {
		owner, kind, ref string
		vol, units       int64
		at               float64
	}
	var list []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.owner, &r.vol, &r.units, &r.kind, &r.ref, &r.at) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	for _, r := range list {
		id, ok := repOf[r.owner]
		if !ok || fresh[id] == nil {
			continue
		}
		note := fmt.Sprintf("old panel: %s", r.kind)
		if r.vol != 0 {
			note += " · volume " + gib(r.vol)
		}
		if r.units != 0 {
			note += fmt.Sprintf(" · unlimited %+d", r.units)
		}
		if r.ref != "" {
			note += " · " + r.ref
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,client_email,note) VALUES(?,?,?,?,?,?)`,
			int64(r.at), id, "old panel", "adjust", "", note); err != nil {
			return err
		}
		rep.LedgerRows++
	}
	return nil
}

func gib(b int64) string {
	return fmt.Sprintf("%.0f GB", math.Round(float64(b)/(1<<30)))
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// darkOldWallets moves customers' toman wallets with every history row; a
// last row makes up any difference so the balance matches the old panel.
func (s *Store) darkOldWallets(ctx context.Context, tx *sql.Tx, old *sql.DB, repOf map[string]string, rep *DarkOldReport) error {
	if !tableExists(ctx, old, "customer_wallets") {
		return nil
	}
	rows, err := old.QueryContext(ctx, `SELECT owner,telegram_id,balance_minor FROM customer_wallets`)
	if err != nil {
		return err
	}
	type w struct {
		owner string
		chat  int64
		bal   int64
	}
	var ws []w
	for rows.Next() {
		var x w
		if rows.Scan(&x.owner, &x.chat, &x.bal) == nil {
			ws = append(ws, x)
		}
	}
	rows.Close()
	hasLedger := tableExists(ctx, old, "customer_wallet_ledger")
	for _, x := range ws {
		sl, ok := repOf[x.owner]
		if !ok || x.chat == 0 {
			continue
		}
		var n int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM toman_wallet WHERE seller=? AND chat_id=? AND kind='import'`, sl, x.chat).Scan(&n)
		if n > 0 {
			continue
		}
		var sum int64
		if hasLedger {
			lr, err := old.QueryContext(ctx, `SELECT delta_minor,kind,COALESCE(detail,''),created_at FROM customer_wallet_ledger WHERE owner=? AND telegram_id=? AND currency='IRT' ORDER BY created_at,id`, x.owner, x.chat)
			if err != nil {
				return err
			}
			type l struct {
				delta      int64
				kind, note string
				at         float64
			}
			var ls []l
			for lr.Next() {
				var y l
				if lr.Scan(&y.delta, &y.kind, &y.note, &y.at) == nil {
					ls = append(ls, y)
				}
			}
			lr.Close()
			for _, y := range ls {
				note := "old panel: " + y.kind
				if d := strings.TrimSpace(y.note); d != "" && len(d) < 200 {
					note += " · " + d
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,note) VALUES(?,?,?,?,?,?)`, sl, x.chat, int64(y.at), y.delta, "import", note); err != nil {
					return err
				}
				sum += y.delta
				rep.WalletRows++
			}
		}
		if d := x.bal - sum; d != 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,note) VALUES(?,?,?,?,?,?)`, sl, x.chat, s.now().Unix(), d, "import", "old panel balance"); err != nil {
				return err
			}
		}
		if x.bal > 0 {
			rep.Wallets++
		}
	}
	return nil
}

// darkOldHistory keeps the old orders (marked as already announced, so
// the bots never tell buyers about them again) and wallet top-ups as history. Orders
// and top-ups still waiting for a decision in the old panel were settled by
// hand by the sellers, so they are left out (counted in Unsettled).
func (s *Store) darkOldHistory(ctx context.Context, tx *sql.Tx, old *sql.DB, repOf map[string]string, rep *DarkOldReport) error {
	if tableExists(ctx, old, "commerce_orders") {
		rows, err := old.QueryContext(ctx, `SELECT id,owner,buyer_telegram_id,price_id,amount_minor,currency,status,COALESCE(gateway_id,''),created_at,updated_at,COALESCE(client_id,''),COALESCE(order_type,'')
FROM commerce_orders ORDER BY created_at`)
		if err != nil {
			rep.note("old orders not read: %v", err)
			rows = nil
		}
		type o struct {
			id, owner, price, cur, status, gw, client, typ string
			buyer, amount                                  int64
			created, updated                               float64
		}
		var list []o
		for rows != nil && rows.Next() {
			var x o
			if rows.Scan(&x.id, &x.owner, &x.buyer, &x.price, &x.amount, &x.cur, &x.status, &x.gw, &x.created, &x.updated, &x.client, &x.typ) == nil {
				list = append(list, x)
			}
		}
		if rows != nil {
			rows.Close()
		}
		for _, x := range list {
			sl, ok := repOf[x.owner]
			if !ok {
				continue
			}
			note := "old:" + x.id
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE note=?`, note).Scan(&n)
			if n > 0 {
				continue
			}
			status := "cancelled"
			switch x.status {
			case "provisioned", "provisioned_waiting_activation", "renewed":
				status = "fulfilled"
			case "paid", "payment_review":
				// Settled by hand by the sellers: left out.
				rep.Unsettled++
				continue
			default:
				note += " · " + x.status
			}
			method := "manual"
			if x.gw == "wallet" {
				method = "wallet"
			}
			email := ""
			if x.client != "" {
				email = v2Email(x.client)
			}
			done := int64(0)
			if status != "cancelled" || x.updated > x.created {
				done = int64(x.updated)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO orders(product_id,seller,buyer,client_email,renew,amount,status,note,created_unix,done_unix,done_by,method,tg_owner_notified,tg_buyer_notified) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,1,1)`,
				v2ID("old-"+x.owner+"-"+x.price), sl, TgBuyer(x.buyer), email, strings.HasPrefix(x.typ, "renew"), max(x.amount, 0), status, note, int64(x.created), done, "old panel", method); err != nil {
				return fmt.Errorf("order %s: %w", x.id, err)
			}
			rep.Orders++
		}
	}
	if tableExists(ctx, old, "customer_topups") {
		rows, err := old.QueryContext(ctx, `SELECT owner,telegram_id,amount_minor,COALESCE(receipt_ref,''),status,created_at,updated_at FROM customer_topups WHERE currency='IRT' ORDER BY created_at`)
		if err != nil {
			return err
		}
		type t struct {
			owner, receipt, status string
			chat, amount           int64
			created, updated       float64
		}
		var list []t
		for rows.Next() {
			var x t
			if rows.Scan(&x.owner, &x.chat, &x.amount, &x.receipt, &x.status, &x.created, &x.updated) == nil {
				list = append(list, x)
			}
		}
		rows.Close()
		var n int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM toman_topups WHERE done_by LIKE 'old panel%'`).Scan(&n)
		if n > 0 {
			return nil
		}
		for _, x := range list {
			sl, ok := repOf[x.owner]
			if !ok || x.chat == 0 || x.amount <= 0 {
				continue
			}
			status, by := "rejected", "old panel"
			switch x.status {
			case "approved":
				status = "approved"
			case "review":
				rep.Unsettled++
				continue // settled by hand: left out
			case "awaiting_receipt":
				by = "old panel: no receipt sent"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO toman_topups(seller,chat_id,amount,receipt,status,created_unix,done_unix,done_by) VALUES(?,?,?,?,?,?,?,?)`,
				sl, x.chat, x.amount, x.receipt, status, int64(x.created), int64(x.updated), by); err != nil {
				return err
			}
			rep.Topups++
		}
	}
	return nil
}
