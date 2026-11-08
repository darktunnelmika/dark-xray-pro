package business

import (
	"context"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"strings"
	"testing"
)

func TestExecutionSubscriptionUserInfoEntitlement(t *testing.T) {
	for _, kind := range []string{"volume", "unlimited", "historical", "held", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			f := newExecutionFixture(t)
			ctx := context.Background()
			tx, err := f.s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec("UPDATE pro_usage_accounts SET uplink=123,downlink=456 WHERE client_id=?", f.client.ID); err != nil {
				t.Fatal(err)
			}
			if kind == "unlimited" {
				if _, err = tx.Exec("UPDATE clients SET plan_kind='unlimited',volume_bytes=0 WHERE id=?", f.client.ID); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "historical" {
				if _, err = tx.Exec("UPDATE clients SET deployment_state='disabled' WHERE id=?", f.client.ID); err != nil {
					t.Fatal(err)
				}
				if err = f.s.usage.EnableHistoricalBudgetsTx(ctx, tx); err != nil {
					t.Fatal(err)
				}
				if err = f.s.usage.ReserveHistoricalBudgetTx(ctx, tx, usageledger.HistoricalBudget{ClientID: f.client.ID, HistoricalBytes: 700, ReceiptSHA: strings.Repeat("a", 64)}); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec("UPDATE clients SET deployment_state='pending_node_apply' WHERE id=?", f.client.ID); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "held" {
				if _, err = tx.Exec("UPDATE pro_usage_accounts SET holds=1 WHERE client_id=?", f.client.ID); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "unknown" {
				if _, err = tx.Exec("UPDATE pro_usage_accounts SET baseline_complete=0 WHERE client_id=?", f.client.ID); err != nil {
					t.Fatal(err)
				}
			}
			info, err := f.s.executionSubscriptionUserInfo(ctx, tx, f.client.ID)
			if kind == "held" || kind == "unknown" {
				if err == nil || info != nil {
					t.Fatal("unknown usage advertised", info, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			total, download := f.client.VolumeBytes, int64(456)
			if kind == "unlimited" {
				total = 0
			}
			if kind == "historical" {
				download += 700
			}
			if info.Upload != 123 || info.Download != download || info.Total != total || info.Expire != f.client.ExpiresUnix {
				t.Fatal("incorrect entitlement", info)
			}
			var up, down int64
			if err = tx.QueryRow("SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != 123 || down != 456 {
				t.Fatal("metadata changed ledger", up, down, err)
			}
		})
	}
}
