package business

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestSellerCatalogConcurrentUnlimitedConservation(t *testing.T) {
	for _, duplicate := range []bool{true, false} {
		t.Run(fmt.Sprintf("duplicate=%v", duplicate), func(t *testing.T) {
			ctx := context.Background()
			f := sellerOrdersFixtureV1(t)
			product := publishUnlimitedCatalogOrderV1(t, f, "concurrent-catalog-plan", 90, 20, 300000)
			second, err := Open(f.path)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			const callers = 12
			type result struct {
				receipt SellerOrderReceiptV1
				err     error
			}
			results := make(chan result, callers)
			start := make(chan struct{})
			for i := 0; i < callers; i++ {
				go func(i int) {
					s := f.s
					if i%2 == 1 {
						s = second
					}
					id := i
					if duplicate {
						id = 0
					}
					req := catalogSellerRequestV1(product, fmt.Sprintf("catalog-concurrent-event-%02d", id), fmt.Sprintf("catalog-concurrent-client-%02d", id))
					<-start
					var receipt SellerOrderReceiptV1
					err := refundRetrySQLiteBusy(func() error {
						var e error
						receipt, e = s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req)
						return e
					})
					results <- result{receipt, err}
				}(i)
			}
			close(start)
			applied, replayed, insufficient := 0, 0, 0
			for i := 0; i < callers; i++ {
				r := <-results
				if errors.Is(r.err, ErrInsufficient) {
					insufficient++
					continue
				}
				if r.err != nil {
					t.Fatal(r.err)
				}
				if r.receipt.Replayed {
					replayed++
				} else {
					applied++
				}
				if r.receipt.UnlimitedDebitUnits != 60 || r.receipt.ReservedUnitsAfter != 40 ||
					r.receipt.Catalog == nil || r.receipt.Catalog.Months != 3 ||
					r.receipt.Catalog.UserCount != 20 || r.receipt.Catalog.PaymentApplied {
					t.Fatalf("incorrect concurrent receipt: %+v", r.receipt)
				}
			}
			if applied != 1 || duplicate && (replayed != callers-1 || insufficient != 0) ||
				!duplicate && (replayed != 0 || insufficient != callers-1) {
				t.Fatalf("applied=%d replayed=%d insufficient=%d", applied, replayed, insufficient)
			}
			sellerOrderBalancesV1(t, f, 0, 1000, 40)
			for _, table := range []string{"clients", "seller_order_clients_v1", "seller_order_receipts_v1", "pro_usage_accounts"} {
				var n int
				if err := f.s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 1 {
					t.Fatalf("%s: count=%d error=%v", table, n, err)
				}
			}
		})
	}
}

func TestSellerCatalogReceiptFailureRollsBackUnlimitedDebit(t *testing.T) {
	ctx := context.Background()
	f := sellerOrdersFixtureV1(t)
	product := publishUnlimitedCatalogOrderV1(t, f, "catalog-rollback-plan", 60, 3, 250000)
	req := catalogSellerRequestV1(product, "catalog-rollback-event-0001", "catalog-rollback-client")
	if _, err := f.s.db.Exec(`CREATE TRIGGER reject_catalog_receipt BEFORE INSERT ON seller_order_receipts_v1 BEGIN SELECT RAISE(ABORT,'injected catalog receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req); err == nil {
		t.Fatal("failed receipt committed")
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 100)
	for _, table := range []string{"clients", "seller_order_clients_v1", "seller_order_receipts_v1", "pro_usage_accounts"} {
		var n int
		if err := f.s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("partial catalog order in %s: count=%d error=%v", table, n, err)
		}
	}
	var n int
	if err := f.s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE event_id=?", req.EventID).Scan(&n); err != nil || n != 0 {
		t.Fatal("order journal survived rollback", n, err)
	}
	if _, err := f.s.db.Exec("DROP TRIGGER reject_catalog_receipt"); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.s.CreateSellerClientFromCatalogV1(ctx, f.rep.Token, req)
	if err != nil || receipt.Replayed || receipt.UnlimitedDebitUnits != 6 {
		t.Fatal(receipt, err)
	}
	sellerOrderBalancesV1(t, f, 0, 1000, 94)
}
