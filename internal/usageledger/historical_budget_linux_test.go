//go:build linux

package usageledger

import (
	"context"
	"errors"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestHistoricalBudgetLimitsRealNativeLedgerWithoutChangingCounters(t *testing.T) {
	ctx := context.Background()
	db, path := testDB(t)
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	account(t, db, l, "customer", true)
	if _, err = db.Exec(`UPDATE clients SET deployment_state='disabled' WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.EnableHistoricalBudgetsTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	reservation := HistoricalBudget{ClientID: "customer", HistoricalBytes: 700, ReceiptSHA: strings.Repeat("a", 64)}
	if err = l.ReserveHistoricalBudgetTx(ctx, tx, reservation); err != nil {
		t.Fatal(err)
	}
	if err = l.ReserveHistoricalBudgetTx(ctx, tx, reservation); err != nil {
		t.Fatal("replay", err)
	}
	changed := reservation
	changed.HistoricalBytes++
	if err = l.ReserveHistoricalBudgetTx(ctx, tx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting history accepted", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE clients SET deployment_state='pending_node_apply' WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	epoch, outbox := registered(t, l, strings.Repeat("b", 32), "budget-node", []string{"customer"}, true)
	report, err := outbox.Observe([]uc.Usage{{ClientID: "customer", Uplink: 299}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Accept(ctx, principal(epoch), report); err != nil {
		t.Fatal(err)
	}
	before, err := l.Account(ctx, "customer")
	if err != nil || !before.Eligible || before.Total != 299 || before.ReservedHistoricalBytes != 700 || !before.RemainingNativeBudgetKnown || before.RemainingNativeBudgetBytes != 1 {
		t.Fatal("historical debit not applied", before, err)
	}
	report, err = outbox.Observe([]uc.Usage{{ClientID: "customer", Uplink: 300}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = l.Accept(ctx, principal(epoch), report); err != nil {
		t.Fatal(err)
	}
	exhausted, err := l.Account(ctx, "customer")
	if err != nil || exhausted.Eligible || !exhausted.Exhausted || exhausted.Total != 300 || exhausted.Limit != 1000 || exhausted.RemainingNativeBudgetBytes != 0 {
		t.Fatal("reserved budget did not stop native eligibility", exhausted, err)
	}
	if _, err = db.Exec(`UPDATE clients SET volume_bytes=1200 WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	renewed, err := l.Account(ctx, "customer")
	if err != nil || !renewed.Eligible || renewed.Exhausted || renewed.Total != 300 || renewed.RemainingNativeBudgetBytes != 200 {
		t.Fatal("renewal changed history or counted twice", renewed, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := connectDB(t, path)
	reopened, err := Open(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.Account(ctx, "customer")
	if err != nil || recovered.ReservedHistoricalBytes != 700 || recovered.Total != 300 || recovered.RemainingNativeBudgetBytes != 200 {
		t.Fatal("reopen lost separate budget", recovered, err)
	}
	for _, query := range []string{`DELETE FROM pro_usage_historical_budgets`, `UPDATE pro_usage_historical_budgets SET historical_bytes=0`} {
		if _, err = fresh.Exec(query); err == nil {
			t.Fatal("historical budget audit was mutable")
		}
	}
	if _, err = fresh.Exec(`DROP VIEW pro_usage_eligibility`); err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.Exec(`CREATE VIEW pro_usage_eligibility AS SELECT id AS client_id,1 AS eligible FROM clients`); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, fresh); !errors.Is(err, ErrSchema) {
		t.Fatal("tampered native budget policy accepted", err)
	}
}

func TestHistoricalBudgetUpgradeAndReservationRollback(t *testing.T) {
	ctx := context.Background()
	db, _ := testDB(t)
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	account(t, db, l, "customer", false)
	if _, err = db.Exec(`UPDATE clients SET deployment_state='disabled' WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.EnableHistoricalBudgetsTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = l.ReserveHistoricalBudgetTx(ctx, tx, HistoricalBudget{ClientID: "customer", HistoricalBytes: 900, ReceiptSHA: strings.Repeat("c", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, db); err != nil {
		t.Fatal("rollback damaged original schema", err)
	}
	a, err := l.Account(ctx, "customer")
	if err != nil || a.Known || a.Eligible || a.ReservedHistoricalBytes != 0 || a.RemainingNativeBudgetKnown {
		t.Fatal("rollback fabricated native authority", a, err)
	}
}

func TestHistoricalBudgetRejectsMalformedAndUnlimitedReservation(t *testing.T) {
	ctx := context.Background()
	db, _ := testDB(t)
	l, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	account(t, db, l, "customer", true)
	if _, err = db.Exec(`UPDATE clients SET deployment_state='disabled' WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.EnableHistoricalBudgetsTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	for _, b := range []HistoricalBudget{
		{ClientID: "customer", HistoricalBytes: -1, ReceiptSHA: strings.Repeat("a", 64)},
		{ClientID: "customer", HistoricalBytes: 1, ReceiptSHA: strings.Repeat("g", 64)},
		{ClientID: "missing", HistoricalBytes: 1, ReceiptSHA: strings.Repeat("a", 64)},
	} {
		if err = l.ReserveHistoricalBudgetTx(ctx, tx, b); !errors.Is(err, ErrConflict) {
			t.Fatal("invalid reservation accepted", err)
		}
	}
	if _, err = tx.Exec(`UPDATE clients SET plan_kind='unlimited' WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	if err = l.ReserveHistoricalBudgetTx(ctx, tx, HistoricalBudget{ClientID: "customer", HistoricalBytes: 1, ReceiptSHA: strings.Repeat("a", 64)}); !errors.Is(err, ErrConflict) {
		t.Fatal("ambiguous unlimited reservation accepted", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE clients SET deployment_state='pending_node_apply' WHERE id='customer'`); err != nil {
		t.Fatal(err)
	}
	a, err := l.Account(ctx, "customer")
	if err != nil || !a.Eligible || a.ReservedHistoricalBytes != 0 || a.RemainingNativeBudgetKnown {
		t.Fatal("native unlimited was changed by failed historical reservations", a, err)
	}
}
